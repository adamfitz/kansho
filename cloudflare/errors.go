package cloudflare

import "fmt"

// CloudflareChallengeError is returned when a Cloudflare challenge
// is detected and the browser has been opened for the user to solve it
type CloudflareChallengeError struct {
	URL        string
	StatusCode int
	Indicators []string
}

func (e *CloudflareChallengeError) Error() string {
	return fmt.Sprintf("cloudflare_challenge_opened: status=%d url=%s", e.StatusCode, e.URL)
}

// IsCloudflareChallenge checks if an error is a CloudflareChallengeError
func IsCloudflareChallenge(err error) (*CloudflareChallengeError, bool) {
	if err == nil {
		return nil, false
	}
	cfErr, ok := err.(*CloudflareChallengeError)
	return cfErr, ok
}
