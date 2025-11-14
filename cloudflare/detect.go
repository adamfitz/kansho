package cloudflare

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gocolly/colly"
)

type CloudflareInfo struct {
	StatusCode int
	Reason     string
	Indicators []string
	Body       string
}

// DetectCloudflare inspects the HTTP response and determines
// whether Cloudflare is blocking or challenging the request.
func DetectCloudflare(resp *http.Response) (bool, *CloudflareInfo, error) {
	if resp == nil {
		return false, nil, nil
	}

	// Read the body safely (without consuming response for caller)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, nil, err
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	body := strings.ToLower(string(bodyBytes))

	indicators := []string{}
	match := false

	// Status-based
	if resp.StatusCode == 403 {
		indicators = append(indicators, "403 Forbidden")
		match = true
	}
	if resp.StatusCode == 503 {
		indicators = append(indicators, "503 Service Unavailable")
		match = true
	}
	if resp.StatusCode == 429 {
		indicators = append(indicators, "429 Rate limit")
	}

	// Body-based detection
	checks := map[string]string{
		"cf-browser-verification":      "JS browser verification challenge",
		"cloudflare":                   "Contains 'cloudflare'",
		"challenge-form":               "Cloudflare challenge form",
		"/cdn-cgi/challenge-platform/": "Cloudflare challenge JS",
		"cf-chl-":                      "Cloudflare challenge token",
	}

	for subs, reason := range checks {
		if strings.Contains(body, subs) {
			indicators = append(indicators, reason)
			match = true
		}
	}

	// Meta refresh redirect check
	metaRedirect := regexp.MustCompile(`<meta[^>]+url=([^">]+)`)
	if metaRedirect.Match(bodyBytes) {
		indicators = append(indicators, "Meta redirect present")
		match = true
	}

	if match {
		return true, &CloudflareInfo{
			StatusCode: resp.StatusCode,
			Reason:     "Cloudflare anti-bot challenge detected",
			Indicators: indicators,
			Body:       string(bodyBytes),
		}, nil
	}

	return false, nil, nil
}

// Wraps DetectCloudflare so it can be used directly
// with Colly scrapers without duplicating conversion code.
func DetectFromColly(r *colly.Response) (bool, *CloudflareInfo, error) {
	if r == nil {
		return false, nil, nil
	}

	// Convert *colly.Response → *http.Response
	httpResp := &http.Response{
		StatusCode: r.StatusCode,
		Body:       io.NopCloser(bytes.NewReader(r.Body)),
		Header:     make(http.Header),
	}

	return DetectCloudflare(httpResp)
}
