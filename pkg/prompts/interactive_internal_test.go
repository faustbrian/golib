package prompts

import (
	"errors"
	"testing"
)

func TestValidationMessageHasGenericFallback(t *testing.T) {
	t.Parallel()

	if got := validationMessage(errors.New("internal")); got != "Value was rejected" {
		t.Fatalf("validationMessage() = %q", got)
	}
}
