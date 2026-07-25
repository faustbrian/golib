package prompts_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestRunAppliesDocumentedValidationAndTransformationOrder(t *testing.T) {
	var calls []string
	prompt := newTextPrompt(t, prompts.TextConfig{
		ID:       "name",
		Label:    "Name",
		Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("  brian  "),
		PreValidate: []prompts.Validator[string]{func(_ context.Context, value string, validation prompts.ValidationContext) error {
			calls = append(calls, "pre:"+value)
			if validation.Dependencies != "dependency" {
				return prompts.NewValidationIssue("dependency", "dependency was not provided", "name")
			}

			return nil
		}},
		Transform: []prompts.Transformer[string]{
			func(_ context.Context, value string, _ prompts.ValidationContext) (string, error) {
				calls = append(calls, "trim:"+value)

				return strings.TrimSpace(value), nil
			},
			func(_ context.Context, value string, _ prompts.ValidationContext) (string, error) {
				calls = append(calls, "upper:"+value)

				return strings.ToUpper(value), nil
			},
		},
		PostValidate: []prompts.Validator[string]{func(_ context.Context, value string, _ prompts.ValidationContext) error {
			calls = append(calls, "post:"+value)

			return nil
		}},
	})

	got, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy:       prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
		Dependencies: "dependency",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got != "BRIAN" {
		t.Fatalf("Run() = %q, want BRIAN", got)
	}

	wantCalls := []string{"pre:  brian  ", "trim:  brian  ", "upper:brian", "post:BRIAN"}
	if strings.Join(calls, "|") != strings.Join(wantCalls, "|") {
		t.Fatalf("pipeline calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestPromptDefensivelyCopiesValidationPipeline(t *testing.T) {
	t.Parallel()

	validators := []prompts.Validator[string]{func(context.Context, string, prompts.ValidationContext) error {
		return nil
	}}
	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("safe"), PostValidate: validators,
	})
	validators[0] = func(context.Context, string, prompts.ValidationContext) error {
		return prompts.NewValidationIssue("mutated", "mutated validator ran", "name")
	}

	got, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil || got != "safe" {
		t.Fatalf("Run() = %q, %v; definition retained caller slice", got, err)
	}
}

func TestValidationFailureIsStableSafeAndFieldAddressable(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("bad"),
		PostValidate: []prompts.Validator[string]{func(context.Context, string, prompts.ValidationContext) error {
			return prompts.NewValidationIssue("reserved", "reserved\x1b name", "name")
		}},
	})

	_, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if !errors.Is(err, prompts.ErrValidationExhausted) {
		t.Fatalf("Run() error = %v, want ErrValidationExhausted", err)
	}

	var issue *prompts.ValidationIssue
	if !errors.As(err, &issue) {
		t.Fatalf("Run() error = %T, want wrapped ValidationIssue", err)
	}
	if issue.Code() != "reserved" || issue.Message() != "reserved� name" {
		t.Fatalf("issue = %q %q", issue.Code(), issue.Message())
	}
	fields := issue.Fields()
	fields[0] = "mutated"
	if issue.Fields()[0] != "name" {
		t.Fatal("ValidationIssue.Fields exposed mutable internal state")
	}
}

func TestValidationPipelineRejectsNilCallbacks(t *testing.T) {
	t.Parallel()

	tests := []prompts.TextConfig{
		{ID: "pre", Label: "Pre", PreValidate: []prompts.Validator[string]{nil}},
		{ID: "transform", Label: "Transform", Transform: []prompts.Transformer[string]{nil}},
		{ID: "post", Label: "Post", PostValidate: []prompts.Validator[string]{nil}},
	}
	for _, config := range tests {
		_, err := prompts.NewText(config)
		if !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewText(%q) error = %v, want ErrInvalidDefinition", config.ID, err)
		}
	}
}

func TestValidationPanicBecomesSafeAdapterFailure(t *testing.T) {
	t.Parallel()

	const canary = "secret-canary-must-not-escape"
	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("value"),
		PostValidate: []prompts.Validator[string]{func(context.Context, string, prompts.ValidationContext) error {
			panic(canary)
		}},
	})

	_, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("Run() error = %v, want ErrAdapter", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("Run() error disclosed panic canary: %v", err)
	}
}

func TestValidationIssueZeroAndDefensiveBehavior(t *testing.T) {
	t.Parallel()

	var nilIssue *prompts.ValidationIssue
	if nilIssue.Error() != "validation failed" || nilIssue.Code() != "" || nilIssue.Message() != "" || nilIssue.Fields() != nil {
		t.Fatal("nil ValidationIssue did not use safe zero behavior")
	}
	if got := prompts.NewValidationIssue("code", "").Error(); got != "code" {
		t.Fatalf("code-only issue Error() = %q", got)
	}
	if got := prompts.NewValidationIssue("", "").Error(); got != "validation failed" {
		t.Fatalf("empty issue Error() = %q", got)
	}
	issue := prompts.NewValidationIssue("bad\r", "message\x7f", "field\n")
	if issue.Code() != "bad�" || issue.Message() != "message�" || issue.Fields()[0] != "field�" {
		t.Fatalf("issue content was not neutralized: %q %q %#v", issue.Code(), issue.Message(), issue.Fields())
	}
	if issue.Error() != "message�" {
		t.Fatalf("message issue Error() = %q", issue.Error())
	}
}

func TestEachPipelineStageCanRejectAValue(t *testing.T) {
	t.Parallel()

	genericFailure := errors.New(" unsafe\x1b failure ")
	tests := []struct {
		name   string
		config prompts.TextConfig
	}{
		{
			name: "pre-validation",
			config: prompts.TextConfig{PreValidate: []prompts.Validator[string]{
				func(context.Context, string, prompts.ValidationContext) error { return genericFailure },
			}},
		},
		{
			name: "transformation",
			config: prompts.TextConfig{Transform: []prompts.Transformer[string]{
				func(context.Context, string, prompts.ValidationContext) (string, error) {
					return "", genericFailure
				},
			}},
		},
		{
			name: "post-validation",
			config: prompts.TextConfig{PostValidate: []prompts.Validator[string]{
				func(context.Context, string, prompts.ValidationContext) error { return genericFailure },
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			test.config.ID = test.name
			test.config.Label = test.name
			test.config.Headless = prompts.HeadlessUseFallback
			test.config.Fallback = prompts.Some("value")
			prompt := newTextPrompt(t, test.config)
			_, err := prompts.Run(context.Background(), prompt, prompts.Execution{
				Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
			})

			var issue *prompts.ValidationIssue
			if !errors.As(err, &issue) {
				t.Fatalf("Run() error = %v, want ValidationIssue", err)
			}
			if issue.Code() != "validation" || issue.Message() != "unsafe� failure" {
				t.Fatalf("normalized issue = %q %q", issue.Code(), issue.Message())
			}
			if issue.Fields()[0] != test.name {
				t.Fatalf("issue fields = %#v", issue.Fields())
			}
		})
	}
}

type stagedContext struct {
	calls int
	err   error
}

func (ctx *stagedContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *stagedContext) Done() <-chan struct{}       { return nil }
func (ctx *stagedContext) Value(any) any               { return nil }
func (ctx *stagedContext) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return ctx.err
	}

	return nil
}

func TestPipelineObservesContextAfterEveryCallbackKind(t *testing.T) {
	tests := []struct {
		name       string
		contextErr error
		config     prompts.TextConfig
		want       error
	}{
		{
			name: "pre canceled", contextErr: context.Canceled, want: prompts.ErrCanceled,
			config: prompts.TextConfig{PreValidate: []prompts.Validator[string]{
				func(context.Context, string, prompts.ValidationContext) error { return nil },
			}},
		},
		{
			name: "transform deadline", contextErr: context.DeadlineExceeded, want: prompts.ErrDeadlineExceeded,
			config: prompts.TextConfig{Transform: []prompts.Transformer[string]{
				func(_ context.Context, value string, _ prompts.ValidationContext) (string, error) { return value, nil },
			}},
		},
		{
			name: "post canceled", contextErr: context.Canceled, want: prompts.ErrCanceled,
			config: prompts.TextConfig{PostValidate: []prompts.Validator[string]{
				func(context.Context, string, prompts.ValidationContext) error { return nil },
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.config.ID = test.name
			test.config.Label = test.name
			test.config.Headless = prompts.HeadlessUseFallback
			test.config.Fallback = prompts.Some("value")
			prompt := newTextPrompt(t, test.config)
			ctx := &stagedContext{err: test.contextErr}

			_, err := prompts.Run(ctx, prompt, prompts.Execution{
				Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
			})
			if !errors.Is(err, test.want) || !errors.Is(err, test.contextErr) {
				t.Fatalf("Run() error = %v, want %v and %v", err, test.want, test.contextErr)
			}
		})
	}
}

type emptyValidationError struct{}

func (emptyValidationError) Error() string { return "" }

func TestEmptyValidationMessageUsesStableFallback(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some("value"),
		PostValidate: []prompts.Validator[string]{
			func(context.Context, string, prompts.ValidationContext) error { return emptyValidationError{} },
		},
	})
	_, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})

	var issue *prompts.ValidationIssue
	if !errors.As(err, &issue) || issue.Message() != "validation failed" {
		t.Fatalf("Run() issue = %#v", issue)
	}
}
