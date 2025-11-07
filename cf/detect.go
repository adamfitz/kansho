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

	match := false

	// ---------------------------
	// Status-based detection
	// ---------------------------
	if resp.StatusCode == 403 {
		info.Indicators = append(info.Indicators, "403 Forbidden")
		match = true
	}
	if resp.StatusCode == 503 {
		info.Indicators = append(info.Indicators, "503 Service Unavailable")
		match = true
	}
	if resp.StatusCode == 429 {
		info.Indicators = append(info.Indicators, "429 Rate limit")
	}

	// Identify cf Server Header
	if strings.Contains(strings.ToLower(info.ServerHeader), "cf") {
		info.Indicators = append(info.Indicators, "cf server header")
		match = true
	}

	// Ray ID (cf always includes this)
	if ray := resp.Header.Get("CF-Ray"); ray != "" {
		info.RayID = ray
		info.Indicators = append(info.Indicators, "cf Ray ID")
		match = true
	}

	// ---------------------------
	// Body content checks
	// ---------------------------

	checks := map[string]string{
		"cf-browser-verification":      "JS browser verification challenge",
		"challenge-form":               "cf challenge form",
		"/cdn-cgi/challenge-platform/": "cf challenge JS",
		"cf-chl-":                      "cf challenge token",
		"attention required":           "cf BIC",
	}

	for subs, reason := range checks {
		if strings.Contains(body, subs) {
			info.Indicators = append(info.Indicators, reason)
			match = true
		}
	}

	// Detect BIC (Browser Integrity Check)
	if strings.Contains(body, "verify you are human") {
		info.IsBIC = true
	}

	// Extract cf_chl_* tokens
	chlTokenRe := regexp.MustCompile(`cf_chl_[a-zA-Z0-9_-]+`)
	info.CHLTokens = chlTokenRe.FindAllString(body, -1)

	// Extract JS challenge URLs
	jsRe := regexp.MustCompile(`/cdn-cgi/challenge-platform/[^"']+`)
	info.JSChallenges = jsRe.FindAllString(body, -1)

	// Extract challenge form action
	formRe := regexp.MustCompile(`<form[^>]+id="challenge-form"[^>]+action="([^"]+)"`)
	if m := formRe.FindStringSubmatch(body); len(m) > 1 {
		info.FormAction = m[1]
		info.Indicators = append(info.Indicators, "cf challenge form detected")
	}

	// Extract meta redirect
	metaRedirectRe := regexp.MustCompile(`<meta[^>]+url=([^">]+)`)
	if m := metaRedirectRe.FindStringSubmatch(body); len(m) > 1 {
		info.MetaRedirect = m[1]
		info.Indicators = append(info.Indicators, "Meta redirect present")
	}

	// Detect Turnstile CAPTCHA
	if strings.Contains(body, "cf-turnstile") {
		info.Turnstile = true
		info.Indicators = append(info.Indicators, "Turnstile CAPTCHA")
		match = true
	}

	// ---------------------------
	// Final match?
	// ---------------------------
	if match {
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
