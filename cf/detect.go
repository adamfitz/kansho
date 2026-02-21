package cf

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gocolly/colly"
)

type CfInfo struct {
	StatusCode int
	Reason     string
	Indicators []string
	Body       string

	// Extracted fields
	RayID        string
	MetaRedirect string
	JSChallenges []string
	CHLTokens    []string
	FormAction   string
	Turnstile    bool
	ServerHeader string
	IsBIC        bool // Browser Integrity Check
}

// Detectcf inspects the HTTP response and determines
// whether CF is blocking or challenging the request.
func Detectcf(resp *http.Response) (bool, *CfInfo, error) {
	if resp == nil {
		return false, nil, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, nil, err
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	body := strings.ToLower(string(bodyBytes))

	info := &CfInfo{
		StatusCode:   resp.StatusCode,
		Indicators:   []string{},
		Body:         string(bodyBytes),
		ServerHeader: resp.Header.Get("Server"),
	}

	// Log response details
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	bodyPreview := string(bodyBytes)
	if len(bodyPreview) > 500 {
		bodyPreview = bodyPreview[:500]
	}
	LogCFResponse(resp.StatusCode, len(bodyBytes), headers, bodyPreview)

	match := false

	// ---------------------------
	// Status-based detection
	// ---------------------------
	if resp.StatusCode == 403 {
		info.Indicators = append(info.Indicators, "403 Forbidden")
		match = true
		logCF("  Indicator: 403 Forbidden")
	}
	if resp.StatusCode == 503 {
		info.Indicators = append(info.Indicators, "503 Service Unavailable")
		match = true
		logCF("  Indicator: 503 Service Unavailable")
	}
	if resp.StatusCode == 429 {
		info.Indicators = append(info.Indicators, "429 Rate limit")
		logCF("  Indicator: 429 Rate Limit")
	}

	// Identify Cloudflare Server Header
	if strings.Contains(strings.ToLower(info.ServerHeader), "cloudflare") {
		info.Indicators = append(info.Indicators, "Cloudflare server header")
		// informational only — DO NOT mark as challenge (removed match = true)
		//match = true
		logCF("  Indicator: Cloudflare server header detected (info only - asuracomics.net serves ALL pages from CF (not only images))")
	}

	// Ray ID (informational only)
	if ray := resp.Header.Get("CF-Ray"); ray != "" {
		info.RayID = ray
		logCF("  CF-Ray present (informational): %s", ray)
	}

	// Check for Set-Cookie with new cf_clearance
	setCookies := resp.Header["Set-Cookie"]
	for _, cookie := range setCookies {
		if strings.Contains(cookie, "cf_clearance") {
			info.Indicators = append(info.Indicators, "New cf_clearance cookie in response")
			match = true
			logCF("  Indicator: New cf_clearance cookie issued by server")
			logCF("    Cookie value: %s", cookie[:min(100, len(cookie))])
		}
	}

	// ---------------------------
	// Body content checks
	// ---------------------------
	// NOTE: /cdn-cgi/challenge-platform/ alone is NOT sufficient to declare a challenge.
	// Asura (and some other Next.js sites behind CF) embed CF's JSD script on EVERY page
	// for bot-scoring purposes — it appears in normal successful responses too.
	// We only treat it as a challenge indicator when combined with other strong signals
	// (403/503 status, challenge-form, just a moment, etc.).
	// The map below is split into "strong" indicators (each alone = challenge) and
	// "weak" indicators (only count when a strong indicator is also present).
	strongChecks := map[string]string{
		"cloudflare-browser-verification": "JS browser verification challenge",
		"challenge-form":                  "Cloudflare challenge form",
		"cf-chl-":                         "Cloudflare challenge token",
		"attention required":              "Cloudflare BIC",
		"checking your browser":           "Cloudflare browser check",
		"verify you are human":            "Cloudflare human verification",
	}
	weakChecks := map[string]string{
		// Present on normal Asura pages — only a challenge when combined with something strong
		"/cdn-cgi/challenge-platform/": "Cloudflare challenge JS",
	}

	strongMatch := false
	for substr, reason := range strongChecks {
		if strings.Contains(body, substr) {
			info.Indicators = append(info.Indicators, reason)
			match = true
			strongMatch = true
			logCF("  Indicator (strong): Found '%s' (%s)", substr, reason)
			if idx := strings.Index(body, substr); idx >= 0 {
				start := max(0, idx-100)
				end := min(len(body), idx+200)
				logCF("    Context: %s", body[start:end])
			}
		}
	}

	// "just a moment" must ONLY match inside <title> — Asura chapter pages contain
	// user comments with this phrase (e.g. "i was at 19 just a moment ago") which
	// caused false positives when matching anywhere in the body.
	// Real CF challenge pages always have: <title>Just a moment...</title>
	justAMomentRe := regexp.MustCompile(`(?i)<title[^>]*>[^<]*just a moment[^<]*</title>`)
	if justAMomentRe.MatchString(body) {
		info.Indicators = append(info.Indicators, "Cloudflare challenge page")
		match = true
		strongMatch = true
		logCF("  Indicator (strong): Found 'just a moment' in <title> (Cloudflare challenge page)")
	} else if strings.Contains(body, "just a moment") {
		logCF("  Skipping 'just a moment' — present in body but NOT in <title> (user comment, not a CF challenge)")
	}

	for substr, reason := range weakChecks {
		if strings.Contains(body, substr) {
			if strongMatch {
				// Only flag as a challenge when there's already a strong indicator
				info.Indicators = append(info.Indicators, reason)
				match = true
				logCF("  Indicator (weak, confirmed by strong signal): Found '%s' (%s)", substr, reason)
			} else {
				logCF("  Skipping weak indicator '%s' — no strong challenge signals present (normal CF-proxied page)", substr)
			}
			if idx := strings.Index(body, substr); idx >= 0 {
				start := max(0, idx-100)
				end := min(len(body), idx+200)
				logCF("    Context: %s", body[start:end])
			}
		}
	}

	// Detect BIC (Browser Integrity Check)
	if strings.Contains(body, "verify you are human") {
		info.IsBIC = true
		logCF("  Browser Integrity Check detected")
	}

	// Extract cf_chl_* tokens
	chlTokenRe := regexp.MustCompile(`cf_chl_[a-zA-Z0-9_-]+`)
	info.CHLTokens = chlTokenRe.FindAllString(body, -1)
	if len(info.CHLTokens) > 0 {
		logCF("  Found %d CF challenge tokens: %v", len(info.CHLTokens), info.CHLTokens)
	}

	// Extract JS challenge URLs
	jsRe := regexp.MustCompile(`/cdn-cgi/challenge-platform/[^"']+`)
	info.JSChallenges = jsRe.FindAllString(body, -1)
	if len(info.JSChallenges) > 0 {
		logCF("  Found %d JS challenge URLs", len(info.JSChallenges))
		for i, url := range info.JSChallenges {
			logCF("    [%d] %s", i+1, url)
		}
	}

	// Extract challenge form action
	formRe := regexp.MustCompile(`<form[^>]+id="challenge-form"[^>]+action="([^"]+)"`)
	if m := formRe.FindStringSubmatch(body); len(m) > 1 {
		info.FormAction = m[1]
		info.Indicators = append(info.Indicators, "Cloudflare challenge form detected")
		logCF("  Challenge form action: %s", info.FormAction)
	}

	// Extract meta redirect
	metaRedirectRe := regexp.MustCompile(`<meta[^>]+url=([^">]+)`)
	if m := metaRedirectRe.FindStringSubmatch(body); len(m) > 1 {
		info.MetaRedirect = m[1]
		info.Indicators = append(info.Indicators, "Meta redirect present")
		logCF("  Meta redirect: %s", info.MetaRedirect)
	}

	// Detect Turnstile CAPTCHA
	if strings.Contains(body, "cf-turnstile") {
		info.Turnstile = true
		info.Indicators = append(info.Indicators, "Turnstile CAPTCHA")
		match = true
		logCF("  Indicator: Turnstile CAPTCHA detected")
	}

	// ---------------------------
	// Final detection result
	// ---------------------------
	if match {
		info.Reason = "Cloudflare anti-bot challenge detected"
		LogCFDetection(true, info.Indicators, info)
		return true, info, nil
	}

	LogCFDetection(false, nil, nil)
	return false, nil, nil
}

// Wraps Detectcf so it can be used directly with Colly scrapers
func DetectFromColly(r *colly.Response) (bool, *CfInfo, error) {
	if r == nil {
		return false, nil, nil
	}

	// Convert *colly.Response → *http.Response
	httpResp := &http.Response{
		StatusCode: r.StatusCode,
		Body:       io.NopCloser(bytes.NewReader(r.Body)),
		Header:     make(http.Header),
	}

	return Detectcf(httpResp)
}
