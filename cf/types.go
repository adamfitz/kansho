package cf

import "time"

// ProtectionType indicates which cf protection is active
type ProtectionType string

const (
	ProtectionNone      ProtectionType = "none"
	ProtectionCookie    ProtectionType = "cookie"    // cf_clearance based
	ProtectionTurnstile ProtectionType = "turnstile" // Turnstile token based
	ProtectionUnknown   ProtectionType = "unknown"
)

// Cookie represents a browser cookie
type Cookie struct {
	Name           string  `json:"name"`
	Value          string  `json:"value"`
	Domain         string  `json:"domain"`
	Path           string  `json:"path"`
	Secure         bool    `json:"secure"`
	HTTPOnly       bool    `json:"httpOnly"`
	SameSite       string  `json:"sameSite"`
	ExpirationDate float64 `json:"expirationDate"` // Unix timestamp
}

// Entropy represents browser fingerprint data
type Entropy struct {
	UserAgent           string           `json:"userAgent"`
	Language            string           `json:"language"`
	Languages           []string         `json:"languages"`
	Platform            string           `json:"platform"`
	HardwareConcurrency int              `json:"hardwareConcurrency"`
	DeviceMemory        float64          `json:"deviceMemory"`
	ScreenResolution    ScreenResolution `json:"screenResolution"`
	Timezone            string           `json:"timezone"`
	TimezoneOffset      int              `json:"timezoneOffset"`
	WebGL               *WebGLInfo       `json:"webgl"`
}

// ScreenResolution represents screen properties
type ScreenResolution struct {
	Width      int `json:"width"`
	Height     int `json:"height"`
	ColorDepth int `json:"colorDepth"`
	PixelDepth int `json:"pixelDepth"`
}

// WebGLInfo represents GPU information
type WebGLInfo struct {
	Vendor   string `json:"vendor"`
	Renderer string `json:"renderer"`
}

// BypassData contains all possible bypass methods
// Only the relevant fields will be populated based on ProtectionType
type BypassData struct {
	Type       ProtectionType `json:"type"`
	CapturedAt string         `json:"capturedAt"`
	URL        string         `json:"url"`
	Domain     string         `json:"domain"`

	// Cookie-based protection (existing)
	Cookies    []Cookie `json:"cookies,omitempty"`
	AllCookies []Cookie `json:"allCookies,omitempty"`

	// Turnstile-based protection (new)
	TurnstileToken    string            `json:"turnstileToken,omitempty"`
	TurnstileFormData map[string]string `json:"turnstileFormData,omitempty"`
	ChallengeToken    string            `json:"challengeToken,omitempty"` // __cf_chl_tk from URL

	// Browser fingerprint (used by all)
	Entropy Entropy           `json:"entropy"`
	Headers map[string]string `json:"headers"`

	// --- new cf cfClearance fields ---
	CfClearance           string             `json:"cfClearance,omitempty"`    // convenience string
	CfClearanceRaw        string             `json:"cfClearanceRaw,omitempty"` // full raw string from headers
	CfClearanceUrl        string             `json:"cfClearanceUrl,omitempty"`
	CfClearanceCapturedAt time.Time          `json:"cfClearanceCapturedAt"`
	CfClearanceStruct     *CfClearanceCookie `json:"cfClearanceStruct,omitempty"` // structured cf_clearance
}

// IsExpired checks if the bypass data is too old
func (b *BypassData) IsExpired(maxAge time.Duration) bool {
	capturedTime, err := time.Parse(time.RFC3339, b.CapturedAt)
	if err != nil {
		return true
	}
	return time.Since(capturedTime) > maxAge
}

// HasCookies returns true if cookie-based bypass data exists
func (b *BypassData) HasCookies() bool {
	return len(b.AllCookies) > 0
}

// HasTurnstile returns true if Turnstile bypass data exists
func (b *BypassData) HasTurnstile() bool {
	return b.TurnstileToken != "" && len(b.TurnstileFormData) > 0
}

// DetermineProtectionType analyzes the data and determines protection type
func (b *BypassData) DetermineProtectionType() ProtectionType {
	if b.HasTurnstile() {
		return ProtectionTurnstile
	}
	if b.HasCookies() {
		return ProtectionCookie
	}
	return ProtectionNone
}
