//go:build ignore

package main

import (
	"context"
	"errors"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestReviewNamePromptAllowsRepeatedCorrection(t *testing.T) {
	t.Parallel()

	prompt, err := reviewNamePrompt()
	if err != nil {
		t.Fatalf("reviewNamePrompt() error = %v", err)
	}
	if retry := prompt.Describe().Retry; !retry.Unlimited || retry.MaxAttempts != 0 {
		t.Fatalf("review retry = %#v", retry)
	}
}

func TestReviewMultiSelectAllowsRepeatedCorrection(t *testing.T) {
	t.Parallel()

	options, err := reviewOptions()
	if err != nil {
		t.Fatalf("reviewOptions() error = %v", err)
	}
	prompt, err := reviewMultiSelect(options)
	if err != nil {
		t.Fatalf("reviewMultiSelect() error = %v", err)
	}
	if retry := prompt.Describe().Retry; !retry.Unlimited || retry.MaxAttempts != 0 {
		t.Fatalf("review retry = %#v", retry)
	}
	_, err = prompts.Parse(context.Background(), prompt, "", nil)
	var issue *prompts.ValidationIssue
	if !errors.As(err, &issue) || issue.Message() != "Select exactly two choices with Space before pressing Enter" {
		t.Fatalf("empty selection error = %v", err)
	}
}
