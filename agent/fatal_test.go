package agent

import (
	"errors"
	"fmt"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestClassifyMarksOnlyUnrecoverableErrorsFatal(t *testing.T) {
	type fataler interface{ Fatal() bool }
	for _, msg := range []string{
		`429 Too Many Requests {"code": "credit_balance_exhausted"}`,
		`429 {"code":"insufficient_quota"}`,
		`401 Unauthorized`,
	} {
		var f fataler
		if !errors.As(classify(errors.New(msg)), &f) || !f.Fatal() {
			t.Errorf("%q must be fatal", msg)
		}
	}
	var f fataler
	if errors.As(classify(errors.New("429 Too Many Requests: rate limit reached for requests")), &f) {
		t.Error("an ordinary rate limit is retryable, not fatal")
	}
	if classify(nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestClassifyReadsTheStructuredCodeNotTheMessage(t *testing.T) {
	type fataler interface{ Fatal() bool }
	var f fataler
	// what the SDK really returns: a bland message, the reason only in the fields
	noCredit := &openai.Error{Code: "credit_balance_exhausted", StatusCode: 429}
	if !errors.As(classify(fmt.Errorf("openai tools complete: %w", noCredit)), &f) {
		t.Fatal("empty credits must be fatal even when the message does not say so")
	}
	rate := &openai.Error{Code: "rate_limit_exceeded", StatusCode: 429}
	if errors.As(classify(fmt.Errorf("x: %w", rate)), &f) {
		t.Fatal("a plain rate limit is not fatal")
	}
	if !errors.As(classify(&openai.Error{StatusCode: 401}), &f) {
		t.Fatal("401 is fatal")
	}
}

// Groq-style answers: a per-minute limit is retried, a per-day limit stops the run, as does an unknown model.
func TestDailyQuotaAndUnknownModelAreFatal(t *testing.T) {
	perMin := fmt.Errorf(`429 Too Many Requests {"error":{"message":"Rate limit reached for model in organization on tokens per minute (TPM): Limit 8000, Used 7900","code":"rate_limit_exceeded"}}`)
	perDay := fmt.Errorf(`429 Too Many Requests {"error":{"message":"Rate limit reached for model in organization on requests per day (RPD): Limit 1000, Used 1000","code":"rate_limit_exceeded"}}`)
	noModel := fmt.Errorf(`404 Not Found {"error":{"code":"model_not_found"}}`)
	if isFatal(classify(perMin)) {
		t.Fatal("a per-minute limit must be retried, not stop the run")
	}
	if !isFatal(classify(perDay)) || !isFatal(classify(noModel)) {
		t.Fatal("a daily quota or an unknown model must stop the run")
	}
}

func isFatal(err error) bool {
	f, ok := err.(interface{ Fatal() bool })
	return ok && f.Fatal()
}
