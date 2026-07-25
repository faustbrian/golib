package prompts_test

import (
	"context"
	"errors"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestErrorClassificationAndSafeFormatting(t *testing.T) {
	t.Parallel()

	cause := errors.New("unsafe cause containing a secret")
	err := &prompts.Error{
		Kind:      prompts.ErrorReader,
		Operation: "read\x1b input",
		PromptID:  "name\rforged",
		Cause:     cause,
	}

	if got, want := err.Error(), "read� input: reader_failure (prompt \"name�forged\")"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if errors.Is(err, prompts.ErrWriter) {
		t.Fatal("reader failure matched ErrWriter")
	}
	if !errors.Is(err, &prompts.Error{Kind: prompts.ErrorReader}) {
		t.Fatal("reader failure did not match another ErrorReader")
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrapped cause is unavailable through errors.Is")
	}
	if !errors.Is(err.Unwrap(), cause) {
		t.Fatal("Unwrap() did not preserve the cause")
	}
	if errors.Is(err, nil) {
		t.Fatal("Error matched nil")
	}

	var nilError *prompts.Error
	if got := nilError.Error(); got != "prompt failure" {
		t.Fatalf("nil Error() = %q", got)
	}
	if nilError.Is(prompts.ErrReader) {
		t.Fatal("nil Error matched a sentinel")
	}
	if nilError.Unwrap() != nil {
		t.Fatal("nil Error unwrapped to a cause")
	}
}

func TestStableErrorSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind     prompts.ErrorKind
		sentinel error
	}{
		{prompts.ErrorInteractionNotPermitted, prompts.ErrInteractionNotPermitted},
		{prompts.ErrorTerminalUnavailable, prompts.ErrTerminalUnavailable},
		{prompts.ErrorCanceled, prompts.ErrCanceled},
		{prompts.ErrorDeadlineExceeded, prompts.ErrDeadlineExceeded},
		{prompts.ErrorEndOfInput, prompts.ErrEndOfInput},
		{prompts.ErrorTerminalDetached, prompts.ErrTerminalDetached},
		{prompts.ErrorInvalidDefinition, prompts.ErrInvalidDefinition},
		{prompts.ErrorUnsupported, prompts.ErrUnsupported},
		{prompts.ErrorValidationExhausted, prompts.ErrValidationExhausted},
		{prompts.ErrorRenderer, prompts.ErrRenderer},
		{prompts.ErrorReader, prompts.ErrReader},
		{prompts.ErrorWriter, prompts.ErrWriter},
		{prompts.ErrorTerminalControl, prompts.ErrTerminalControl},
		{prompts.ErrorAdapter, prompts.ErrAdapter},
	}

	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			t.Parallel()

			if got := test.sentinel.Error(); got != string(test.kind) {
				t.Fatalf("sentinel Error() = %q, want %q", got, test.kind)
			}
			if !errors.Is(&prompts.Error{Kind: test.kind}, test.sentinel) {
				t.Fatalf("Error kind %q did not match its sentinel", test.kind)
			}
		})
	}
}

func TestCanceledContextPreservesStandardAndPromptErrors(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := prompts.Run(ctx, prompt, prompts.Execution{})
	if !errors.Is(err, prompts.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want prompt and context cancellation", err)
	}
}
