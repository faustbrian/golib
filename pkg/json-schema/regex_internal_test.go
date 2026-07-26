package jsonschema

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPatternMatchingDeadlineIsBounded(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRegexBacktracking = 100_000_000
	limits.MaxRegexMatchMilliseconds = 1
	pattern, err := compilePatternWithLimits(`(a+)+$`, limits)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pattern.matchString(strings.Repeat("a", 10_000) + "!")
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want ErrLimitExceeded", err)
	}
	var limitError *LimitError
	if !errors.As(err, &limitError) || limitError.Resource != "regular expression match milliseconds" {
		t.Fatalf("got %#v, want match duration limit", err)
	}
}

func TestRegexFormatPreservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (boundedRegexFormat{limits: DefaultLimits()}).Valid(ctx, "valid")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
}
