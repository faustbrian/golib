package safeerror_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/config/internal/safeerror"
)

type customCause struct{}

func (*customCause) Error() string { return "canary-secret-value" }

func TestRedactPreservesIdentityWithoutExposingCause(t *testing.T) {
	t.Parallel()

	cause := &customCause{}
	err := safeerror.Redact(cause, "safe message")
	if err.Error() != "safe message" || !errors.Is(err, cause) {
		t.Fatalf("Redact() = %T %v", err, err)
	}
	var exposed *customCause
	if errors.As(err, &exposed) || errors.Unwrap(err) != nil {
		t.Fatalf("Redact() exposed cause: %#v", exposed)
	}
	if got := safeerror.Redact(err, "replacement"); got.Error() != "safe message" {
		t.Fatalf("Redact(existing) = %q", got)
	}
	if safeerror.Redact(nil, "safe message") != nil {
		t.Fatal("Redact(nil) != nil")
	}
}
