package prompts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestTypedPromptParsersAndResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"text", testTextParsing},
		{"multiline", testMultilineParsing},
		{"integer", testIntegerParsing},
		{"decimal", testDecimalParsing},
		{"duration", testDurationParsing},
		{"date", testDateParsing},
		{"time", testTimeParsing},
		{"path", testPathParsing},
		{"confirmation", testConfirmationParsing},
	}

	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func testTextParsing(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	if got := parseValue(t, prompt, "Brian"); got != "Brian" {
		t.Fatalf("Parse() = %q", got)
	}
	assertInvalidSubmission(t, prompt, "first\nsecond")
}

func testMultilineParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewMultiline(prompts.MultilineConfig{ID: "bio", Label: "Biography"})
	if err != nil {
		t.Fatalf("NewMultiline() error = %v", err)
	}
	if got := parseValue(t, prompt, "first\nsecond"); got != "first\nsecond" {
		t.Fatalf("Parse() = %q", got)
	}
}

func testIntegerParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewInteger(prompts.IntegerConfig{ID: "count", Label: "Count"})
	if err != nil {
		t.Fatalf("NewInteger() error = %v", err)
	}
	if got := parseValue(t, prompt, "-42"); got != -42 {
		t.Fatalf("Parse() = %d", got)
	}
	assertInvalidSubmission(t, prompt, "4.2")
}

func testDecimalParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewDecimal(prompts.DecimalConfig{ID: "rate", Label: "Rate"})
	if err != nil {
		t.Fatalf("NewDecimal() error = %v", err)
	}
	value := parseValue(t, prompt, "-001.2300")
	if value.String() != "-1.23" || value.Scale() != 2 {
		t.Fatalf("Parse() = %q scale %d", value.String(), value.Scale())
	}
	assertInvalidSubmission(t, prompt, "NaN")
}

func testDurationParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewDuration(prompts.DurationConfig{ID: "timeout", Label: "Timeout"})
	if err != nil {
		t.Fatalf("NewDuration() error = %v", err)
	}
	if got := parseValue(t, prompt, "1h30m"); got != 90*time.Minute {
		t.Fatalf("Parse() = %s", got)
	}
	assertInvalidSubmission(t, prompt, "one hour")
}

func testDateParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewDate(prompts.DateConfig{ID: "day", Label: "Day"})
	if err != nil {
		t.Fatalf("NewDate() error = %v", err)
	}
	value := parseValue(t, prompt, "2024-02-29")
	if value.String() != "2024-02-29" || value.Year() != 2024 || value.Month() != time.February || value.Day() != 29 {
		t.Fatalf("Parse() = %v", value)
	}
	assertInvalidSubmission(t, prompt, "2023-02-29")
}

func testTimeParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewTime(prompts.TimeConfig{ID: "at", Label: "At"})
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	value := parseValue(t, prompt, "23:59:58.123")
	if value.String() != "23:59:58.123" || value.Hour() != 23 || value.Minute() != 59 || value.Second() != 58 || value.Nanosecond() != 123_000_000 {
		t.Fatalf("Parse() = %v", value)
	}
	assertInvalidSubmission(t, prompt, "24:00")
}

func testPathParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewPath(prompts.PathConfig{ID: "file", Label: "File", Kind: prompts.PathFile})
	if err != nil {
		t.Fatalf("NewPath() error = %v", err)
	}
	value := parseValue(t, prompt, "/definitely/not/created/by/prompts")
	if value.String() != "/definitely/not/created/by/prompts" || value.Kind() != prompts.PathFile {
		t.Fatalf("Parse() = %v", value)
	}
	assertInvalidSubmission(t, prompt, "bad\x00path")
}

func testConfirmationParsing(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewConfirm(prompts.ConfirmConfig{
		ID: "continue", Label: "Continue?", Accept: []string{"ja"}, Reject: []string{"nej"},
	})
	if err != nil {
		t.Fatalf("NewConfirm() error = %v", err)
	}
	if !parseValue(t, prompt, "JA") {
		t.Fatal("localized affirmative did not parse")
	}
	if parseValue(t, prompt, "nej") {
		t.Fatal("localized negative parsed as true")
	}
	assertInvalidSubmission(t, prompt, "yes")
}

func TestPromptDescriptorContainsExecutionContract(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Description: "Account holder", Placeholder: "Ada",
		Hint: "Public name", Help: "Shown on invoices", Headless: prompts.HeadlessUseFallback,
		Retry: prompts.RetryPolicy{MaxAttempts: 5}, Cancel: prompts.CancelUseDefault,
		EndOfInput: prompts.EOFUseFallback, Secret: prompts.SecretNone,
		Accessibility: prompts.Accessibility{Label: "Account holder name", Description: "Public", TextualHint: "Enter text"},
	})
	descriptor := prompt.Describe()
	if descriptor.Kind != prompts.KindText || descriptor.ID != "name" || descriptor.Label != "Name" || descriptor.Description != "Account holder" || descriptor.Placeholder != "Ada" || descriptor.Hint != "Public name" || descriptor.Help != "Shown on invoices" {
		t.Fatalf("descriptor metadata = %#v", descriptor)
	}
	if descriptor.Retry.MaxAttempts != 5 || descriptor.Cancel != prompts.CancelUseDefault || descriptor.EndOfInput != prompts.EOFUseFallback || descriptor.Headless != prompts.HeadlessUseFallback || descriptor.Secret != prompts.SecretNone {
		t.Fatalf("descriptor behavior = %#v", descriptor)
	}
	if descriptor.Accessibility.Label != "Account holder name" {
		t.Fatalf("descriptor accessibility = %#v", descriptor.Accessibility)
	}
}

func TestInvalidTypedDefinitionsAreRejected(t *testing.T) {
	t.Parallel()

	_, err := prompts.NewText(prompts.TextConfig{
		ID: "name", Label: "Name", Retry: prompts.RetryPolicy{Unlimited: true, MaxAttempts: 1},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("conflicting retry policy error = %v", err)
	}
	_, err = prompts.NewConfirm(prompts.ConfirmConfig{
		ID: "continue", Label: "Continue?", Accept: []string{"same"}, Reject: []string{"SAME"},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("overlapping confirm values error = %v", err)
	}
	_, err = prompts.NewPath(prompts.PathConfig{ID: "path", Label: "Path", Kind: prompts.PathKind(200)})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid path kind error = %v", err)
	}
	_, err = prompts.NewConfirm(prompts.ConfirmConfig{
		ID: "continue", Label: "Continue?", Accept: []string{"yes", "YES"}, Reject: []string{"no"},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("duplicate confirmation value error = %v", err)
	}
}

func TestTypedConstructorValidationAndDefensiveCopies(t *testing.T) {
	t.Parallel()

	_, err := prompts.NewInteger(prompts.IntegerConfig{Label: "Count"})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("missing integer identity error = %v", err)
	}

	invalidPre := prompts.IntegerConfig{ID: "count", Label: "Count", PreValidate: []prompts.Validator[int64]{nil}}
	invalidTransform := prompts.IntegerConfig{ID: "count", Label: "Count", Transform: []prompts.Transformer[int64]{nil}}
	invalidPost := prompts.IntegerConfig{ID: "count", Label: "Count", PostValidate: []prompts.Validator[int64]{nil}}
	invalidCancel := prompts.IntegerConfig{ID: "count", Label: "Count", Cancel: prompts.CancelBehavior(200)}
	invalidEOF := prompts.IntegerConfig{ID: "count", Label: "Count", EndOfInput: prompts.EOFBehavior(200)}
	for _, config := range []prompts.IntegerConfig{invalidPre, invalidTransform, invalidPost, invalidCancel, invalidEOF} {
		_, err := prompts.NewInteger(config)
		if !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("invalid integer config error = %v", err)
		}
	}

	_, err = prompts.NewMultiline(prompts.MultilineConfig{
		ID: "bio", Label: "Biography", Transform: []prompts.Transformer[string]{nil},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil multiline callback error = %v", err)
	}

	accept := []string{"yes"}
	reject := []string{"no"}
	prompt, err := prompts.NewConfirm(prompts.ConfirmConfig{ID: "continue", Label: "Continue?", Accept: accept, Reject: reject})
	if err != nil {
		t.Fatalf("NewConfirm() error = %v", err)
	}
	accept[0] = "mutated"
	reject[0] = "mutated"
	if !parseValue(t, prompt, "yes") || parseValue(t, prompt, "no") {
		t.Fatal("confirmation retained caller slices")
	}
}

func TestConfirmationDefaultsAndInvalidVocabulary(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewConfirm(prompts.ConfirmConfig{ID: "continue", Label: "Continue?"})
	if err != nil {
		t.Fatalf("NewConfirm() error = %v", err)
	}
	if !parseValue(t, prompt, " yes ") || parseValue(t, prompt, "FALSE") {
		t.Fatal("default confirmation vocabulary did not parse")
	}

	for _, config := range []prompts.ConfirmConfig{
		{ID: "empty-accept", Label: "Continue?", Accept: []string{" "}, Reject: []string{"no"}},
		{ID: "empty-reject", Label: "Continue?", Accept: []string{"yes"}, Reject: []string{" "}},
	} {
		_, err := prompts.NewConfirm(config)
		if !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewConfirm(%q) error = %v", config.ID, err)
		}
	}
}

func TestValueTypeCanonicalBoundaries(t *testing.T) {
	t.Parallel()

	decimalPrompt, err := prompts.NewDecimal(prompts.DecimalConfig{ID: "decimal", Label: "Decimal"})
	if err != nil {
		t.Fatalf("NewDecimal() error = %v", err)
	}
	for input, want := range map[string]string{
		"0": "0", "-0.000": "0", "+12": "12", "0.00120": "0.0012",
	} {
		if got := parseValue(t, decimalPrompt, input).String(); got != want {
			t.Fatalf("Parse(%q) = %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{"", "+", "-", ".", ".1", "1.", "1.2.3", "1e3"} {
		assertInvalidSubmission(t, decimalPrompt, input)
	}
	if got := (prompts.Decimal{}).String(); got != "0" {
		t.Fatalf("zero Decimal.String() = %q", got)
	}

	timePrompt, err := prompts.NewTime(prompts.TimeConfig{ID: "time", Label: "Time"})
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	if got := parseValue(t, timePrompt, "12:34").String(); got != "12:34:00" {
		t.Fatalf("minute TimeOfDay.String() = %q", got)
	}

	pathPrompt, err := prompts.NewPath(prompts.PathConfig{ID: "path", Label: "Path"})
	if err != nil {
		t.Fatalf("NewPath() error = %v", err)
	}
	assertInvalidSubmission(t, pathPrompt, "")
}

type alwaysCanceledContext struct{ err error }

func (ctx alwaysCanceledContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx alwaysCanceledContext) Done() <-chan struct{}       { return nil }
func (ctx alwaysCanceledContext) Err() error                  { return ctx.err }
func (ctx alwaysCanceledContext) Value(any) any               { return nil }

func TestParseRejectsInvalidContextAndUnsupportedZeroPrompt(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	var nilContext context.Context
	_, err := prompts.Parse(nilContext, prompt, "value", nil)
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil context error = %v", err)
	}
	_, err = prompts.Parse(alwaysCanceledContext{err: context.Canceled}, prompt, "value", nil)
	if !errors.Is(err, prompts.ErrCanceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	_, err = prompts.Parse(context.Background(), prompts.Prompt[string]{}, "value", nil)
	if !errors.Is(err, prompts.ErrUnsupported) {
		t.Fatalf("zero prompt error = %v", err)
	}
}

func parseValue[T any](t *testing.T, prompt prompts.Prompt[T], input string) T {
	t.Helper()

	value, err := prompts.Parse(context.Background(), prompt, input, nil)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", input, err)
	}

	return value
}

func assertInvalidSubmission[T any](t *testing.T, prompt prompts.Prompt[T], input string) {
	t.Helper()

	_, err := prompts.Parse(context.Background(), prompt, input, nil)
	if !errors.Is(err, prompts.ErrValidationExhausted) {
		t.Fatalf("Parse(%q) error = %v, want ErrValidationExhausted", input, err)
	}
}
