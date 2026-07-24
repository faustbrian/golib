package expression

import (
	"errors"
	"testing"
)

func TestEvaluateRejectsUnknownPrivateMessageSource(t *testing.T) {
	t.Parallel()

	value := Expression{kind: Request}
	if _, err := value.Evaluate(Context{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Evaluate() error = %v", err)
	}
}
