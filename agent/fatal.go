package agent

import (
	"errors"
	"strings"

	"github.com/openai/openai-go/v3"
)

// fatalAPIError marks an API failure that no retry will fix: no credits, no quota, a bad key.
// The selection gate stops a run on it instead of recording one failed task per call and
// continuing (which wastes time and, in an improver, would "learn" from a run of failures).
type fatalAPIError struct{ err error }

func (e fatalAPIError) Error() string { return e.err.Error() }
func (e fatalAPIError) Unwrap() error { return e.err }

// Fatal reports true; selectiongate looks for this method.
func (e fatalAPIError) Fatal() bool { return true }

// fatalCodes are OpenAI error codes that mean the account cannot make calls at all.
var fatalCodes = map[string]bool{
	"credit_balance_exhausted": true, "insufficient_quota": true, "invalid_api_key": true, "account_deactivated": true,
}

// classify wraps err as fatal when it says no call can succeed. The SDK's error text does not
// include the response body ("OpenAI API error: 429 Too Many Requests" is all it says, for both a
// rate limit and empty credits), so the structured code and status are read, not the message.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		if fatalCodes[apiErr.Code] || apiErr.StatusCode == 401 {
			return fatalAPIError{err}
		}
		return err
	}
	// not an SDK error (for example a stub or gateway in front of the API): fall back to the text
	msg := err.Error()
	if strings.Contains(msg, "401 Unauthorized") {
		return fatalAPIError{err}
	}
	for code := range fatalCodes {
		if strings.Contains(msg, code) {
			return fatalAPIError{err}
		}
	}
	return err
}
