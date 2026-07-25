package prompts_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

type rejectingReader struct {
	t *testing.T
}

func (reader rejectingReader) Read(_ []byte) (int, error) {
	reader.t.Helper()
	reader.t.Fatal("headless execution attempted to read input")

	return 0, io.ErrUnexpectedEOF
}

func TestRunRefusesForbiddenHeadlessPromptWithoutReading(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewText(prompts.TextConfig{
		ID:       "account-name",
		Label:    "Account name",
		Headless: prompts.HeadlessForbidden,
	})
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}

	var output bytes.Buffer
	var errorOutput bytes.Buffer

	_, err = prompts.Run(context.Background(), prompt, prompts.Execution{
		Input:  rejectingReader{t: t},
		Output: &output,
		Error:  &errorOutput,
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if !errors.Is(err, prompts.ErrInteractionNotPermitted) {
		t.Fatalf("Run() error = %v, want ErrInteractionNotPermitted", err)
	}

	var promptError *prompts.Error
	if !errors.As(err, &promptError) {
		t.Fatalf("Run() error type = %T, want *prompts.Error", err)
	}
	if promptError.PromptID != "account-name" {
		t.Fatalf("PromptID = %q, want account-name", promptError.PromptID)
	}
	if output.Len() != 0 || errorOutput.Len() != 0 {
		t.Fatalf("headless refusal wrote output %q or error output %q", output.String(), errorOutput.String())
	}
}
