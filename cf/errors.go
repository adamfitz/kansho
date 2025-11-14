package cf

import "fmt"

// cfChallengeError is returned when a cf challenge
// is detected and the browser has been opened for the user to solve it
type CfChallengeError struct {
	URL        string
	StatusCode int
	Indicators []string
}

func (e *CfChallengeError) Error() string {
	return fmt.Sprintf("cf_challenge_opened: status=%d url=%s", e.StatusCode, e.URL)
}

// IscfChallenge checks if an error is a CfChallengeError
func IscfChallenge(err error) (*CfChallengeError, bool) {
	if err == nil {
		return nil, false
	}
	cfErr, ok := err.(*CfChallengeError)
	return cfErr, ok
}
