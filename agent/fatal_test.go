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
