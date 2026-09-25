package cf

import (
	"strings"
	"testing"
)

// TestClearanceDomainMatches verifies the exact/parent domain matching used to
// reject a cf_clearance issued for a different site than the one it will be
// used against.
func TestClearanceDomainMatches(t *testing.T) {
	cases := []struct {
		name         string
		cookieDomain string
		targetDomain string
		wantMatch    bool
	}{
		{
			name:         "exact match",
			cookieDomain: "en-thunderscans.com",
			targetDomain: "en-thunderscans.com",
			wantMatch:    true,
		},
		{
			name:         "leading dot ignored",
			cookieDomain: ".en-thunderscans.com",
			targetDomain: "en-thunderscans.com",
			wantMatch:    true,
		},
		{
			name:         "apex cookie covers www subdomain",
			cookieDomain: "en-thunderscans.com",
			targetDomain: "www.en-thunderscans.com",
			wantMatch:    true,
		},
		{
			name:         "apex cookie covers deeper subdomain",
			cookieDomain: "thunderscans.com",
			targetDomain: "en.thunderscans.com",
			wantMatch:    true,
		},
		{
			name:         "different site is rejected",
			cookieDomain: "manhuaus.com",
			targetDomain: "en-thunderscans.com",
			wantMatch:    false,
		},
		{
			name:         "similar-looking unrelated apex is rejected",
			cookieDomain: "thunderscans.com",
			targetDomain: "en-thunderscans.com",
			wantMatch:    false,
		},
		{
			name:         "subdomain cookie cannot cover apex",
			cookieDomain: "www.en-thunderscans.com",
			targetDomain: "en-thunderscans.com",
			wantMatch:    false,
		},
		{
			name:         "subdomain cookie cannot cover sibling subdomain",
			cookieDomain: "a.en-thunderscans.com",
			targetDomain: "b.en-thunderscans.com",
			wantMatch:    false,
		},
	}

	for _, tc := range cases {
		if got := ClearanceDomainMatches(tc.cookieDomain, tc.targetDomain); got != tc.wantMatch {
			t.Errorf("%s: ClearanceDomainMatches(%q, %q) = %v, want %v",
				tc.name, tc.cookieDomain, tc.targetDomain, got, tc.wantMatch)
		}
	}
}

// TestValidateImportDataRejectsMismatchedClearance verifies that data whose
// cf_clearance was issued for another site is rejected and, critically, that the
// import pipeline reports an error so the data is never written to this site's
// file.
func TestValidateImportDataRejectsMismatchedClearance(t *testing.T) {
	data := &BypassData{
		Domain: "en-thunderscans.com",
		CfClearanceStruct: &CfClearanceCookie{
			Name:   "cf_clearance",
			Value:  "stale-token",
			Domain: "manhuaus.com",
		},
	}

	if err := ValidateImportData(data); err == nil {
		t.Fatal("expected mismatched clearance to be rejected")
	} else if !strings.Contains(err.Error(), "domain mismatch") {
		t.Fatalf("expected a domain mismatch error, got: %v", err)
	}
}

// TestValidateImportDataAllowsMatchingClearance verifies that a clearance issued
// for the captured domain (exact or via its apex domain) passes validation.
func TestValidateImportDataAllowsMatchingClearance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfDomain string
		domain   string
	}{
		{name: "exact match", cfDomain: "en-thunderscans.com", domain: "en-thunderscans.com"},
		{name: "apex cookie covers subdomain target", cfDomain: "thunderscans.com", domain: "en.thunderscans.com"},
	} {
		data := &BypassData{
			Domain: tc.domain,
			CfClearanceStruct: &CfClearanceCookie{
				Name:   "cf_clearance",
				Value:  "token",
				Domain: tc.cfDomain,
			},
		}
		if err := ValidateImportData(data); err != nil {
			t.Errorf("%s: expected validation to pass, got: %v", tc.name, err)
		}
	}
}

// TestValidateImportDataIgnoresMissingClearance verifies that data without a
// structured clearance (or with an empty clearance domain) is not blocked by the
// import guard, preserving the existing behaviour for cookie- and turnstile-based
// captures.
func TestValidateImportDataIgnoresMissingClearance(t *testing.T) {
	for _, tc := range []struct {
		name string
		data *BypassData
	}{
		{name: "no clearance struct", data: &BypassData{Domain: "en-thunderscans.com"}},
		{name: "empty clearance domain", data: &BypassData{
			Domain:            "en-thunderscans.com",
			CfClearanceStruct: &CfClearanceCookie{Name: "cf_clearance", Value: "token"},
		}},
		{name: "nil data is an error", data: nil},
	} {
		err := ValidateImportData(tc.data)
		if tc.name == "nil data is an error" {
			if err == nil {
				t.Error("nil data should be rejected")
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: expected validation to pass, got: %v", tc.name, err)
		}
	}
}

// TestParseCapturedDataToleratesEmptyClearanceTimestamp verifies that exported
// data with no cf_clearance (empty timestamp string) parses without error — this
// was the exact crash seen when importing a capture that contained no clearance.
func TestParseCapturedDataToleratesEmptyClearanceTimestamp(t *testing.T) {
	jsonData := `{
  "capturedAt": "2026-09-25T20:14:00Z",
  "url": "https://en-thunderscans.com/manga",
  "domain": "en-thunderscans.com",
  "type": "cookie",
  "allCookies": [
    {"name": "PHPSESSID", "value": "sess", "domain": "en-thunderscans.com", "path": "/"}
  ],
  "cfClearance": "",
  "cfClearanceCapturedAt": "",
  "cfClearanceUrl": "",
  "headers": {"userAgent": "Mozilla/5.0", "acceptLanguage": "en-US"}
}`

	data, err := ParseCapturedData(jsonData)
	if err != nil {
		t.Fatalf("expected parsing to succeed despite the empty timestamp, got: %v", err)
	}
	if !data.CfClearanceCapturedAt.IsZero() {
		t.Errorf("expected a zero capture time, got %v", data.CfClearanceCapturedAt)
	}
	if data.CfClearance != "" {
		t.Errorf("expected no clearance value, got %q", data.CfClearance)
	}
}

// TestImportFromClipboardStringRejectsMismatchedClearance feeds a complete
// captured-data JSON whose cf_clearance belongs to another site and verifies the
// whole pipeline rejects it before saving.
func TestImportFromClipboardStringRejectsMismatchedClearance(t *testing.T) {
	jsonData := `{
  "capturedAt": "2026-09-25T19:57:00Z",
  "url": "https://en-thunderscans.com/manga",
  "domain": "en-thunderscans.com",
  "type": "cookie",
  "allCookies": [
    {"name": "PHPSESSID", "value": "sess", "domain": "en-thunderscans.com", "path": "/"}
  ],
  "cfClearance": "cf_clearance=stale-token; Domain=manhuaus.com; Path=/; HttpOnly; Secure",
  "cfClearanceUrl": "https://manhuaus.com/",
  "headers": {"userAgent": "Mozilla/5.0", "acceptLanguage": "en-US"}
}`

	_, err := ImportFromClipboardString(jsonData)
	if err == nil {
		t.Fatal("expected the import of a mismatched clearance to fail")
	}
	if !strings.Contains(err.Error(), "domain mismatch") {
		t.Fatalf("expected a domain mismatch error, got: %v", err)
	}
}
