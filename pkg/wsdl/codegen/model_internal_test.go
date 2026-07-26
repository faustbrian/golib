package codegen

import (
	"errors"
	"testing"
)

func TestWithinAcceptsExactLimitAndRejectsOverflow(t *testing.T) {
	t.Parallel()

	if err := within("items", 1, 1); err != nil {
		t.Fatalf("within(exact limit) error = %v", err)
	}
	if err := within("items", 2, 1); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("within(over limit) error = %v, want ErrLimitExceeded", err)
	}
}
