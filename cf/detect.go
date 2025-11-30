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
// whether cf is blocking or challenging the request.
//
// IMPORTANT: This uses a scoring system to avoid false positives.
// CF-Ray header and cloudflare server header are present on ALL CF responses,
// so we need MULTIPLE challenge-specific indicators.
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

	// Use a scoring system - need multiple indicators
	challengeScore := 0

	// ---------------------------
	// Status-based detection
	// ---------------------------
	if resp.StatusCode == 403 {
		info.Indicators = append(info.Indicators, "403 Forbidden")
		challengeScore += 2 // Strong indicator
	}
	if resp.StatusCode == 503 {
		info.Indicators = append(info.Indicators, "503 Service Unavailable")
		challengeScore += 2 // Strong indicator
	}
	if resp.StatusCode == 429 {
		info.Indicators = append(info.Indicators, "429 Rate limit")
		challengeScore += 1 // Moderate indicator
	}

	// Server header - WEAK indicator (present on all CF-proxied sites)
	// Only log it, don't count it toward challenge detection
	if strings.Contains(strings.ToLower(info.ServerHeader), "cloudflare") {
		info.Indicators = append(info.Indicators, "cloudflare server (informational)")
		// DO NOT increment score - this is on ALL CF responses
	}

	// Ray ID (cf always includes this) - INFORMATIONAL ONLY
	if ray := resp.Header.Get("CF-Ray"); ray != "" {
		info.RayID = ray
		// DO NOT increment score - present on ALL Cloudflare responses
	}

	// ---------------------------
	// Body content checks - STRONG indicators
	// ---------------------------

	// These are definitive challenge indicators
	strongChecks := map[string]string{
		"cf-browser-verification":      "JS browser verification challenge",
		"challenge-form":               "cf challenge form",
		"/cdn-cgi/challenge-platform/": "cf challenge JS",
		"checking your browser":        "Browser check message",
		"verify you are human":         "Human verification",
	}

	for subs, reason := range strongChecks {
		if strings.Contains(body, subs) {
			info.Indicators = append(info.Indicators, reason)
			challengeScore += 3 // Very strong indicator
		}
	}

	// Challenge tokens - STRONG indicator
	if strings.Contains(body, "cf-chl-") || strings.Contains(body, "cf_chl_") {
		info.Indicators = append(info.Indicators, "cf challenge token")
		challengeScore += 3
	}

	// "Attention Required" - MODERATE indicator (could be other issues)
	if strings.Contains(body, "attention required") {
		info.Indicators = append(info.Indicators, "Attention Required")
		challengeScore += 2
	}

	// Detect BIC (Browser Integrity Check)
	if strings.Contains(body, "verify you are human") || strings.Contains(body, "checking your browser") {
		info.IsBIC = true
	}

	// Extract cf_chl_* tokens
	chlTokenRe := regexp.MustCompile(`cf_chl_[a-zA-Z0-9_-]+`)
	info.CHLTokens = chlTokenRe.FindAllString(body, -1)
	if len(info.CHLTokens) > 0 {
		// Already counted above, but log the count
		challengeScore += 2
	}

	// Extract JS challenge URLs
	jsRe := regexp.MustCompile(`/cdn-cgi/challenge-platform/[^"']+`)
	info.JSChallenges = jsRe.FindAllString(body, -1)
	if len(info.JSChallenges) > 0 {
		// Already counted in strongChecks
	}

	// Extract challenge form action
	formRe := regexp.MustCompile(`<form[^>]+id="challenge-form"[^>]+action="([^"]+)"`)
	if m := formRe.FindStringSubmatch(body); len(m) > 1 {
		info.FormAction = m[1]
		// Already counted in strongChecks
	}

	// Extract meta redirect
	metaRedirectRe := regexp.MustCompile(`<meta[^>]+url=([^">]+)`)
	if m := metaRedirectRe.FindStringSubmatch(body); len(m) > 1 {
		info.MetaRedirect = m[1]
		info.Indicators = append(info.Indicators, "Meta redirect present")
		// Don't count - meta redirects are common
	}

	// Detect Turnstile CAPTCHA
	if strings.Contains(body, "cf-turnstile") {
		info.Turnstile = true
		info.Indicators = append(info.Indicators, "Turnstile CAPTCHA")
		challengeScore += 3 // Definitive challenge
	}

	// ---------------------------
	// Final determination
	// ---------------------------
	// Require a minimum score to avoid false positives
	// Score >= 3 means we have at least one strong indicator
	// This prevents normal CF-proxied pages from being flagged
	if challengeScore >= 3 {
		info.Reason = "cf anti-bot challenge detected"
		return true, info, nil
	}

	return false, nil, nil
}

// Wraps Detectcf so it can be used directly
// with Colly scrapers without duplicating conversion code.
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
