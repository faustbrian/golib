package prompts_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestFormRunsTypedFieldsAndConditionalFollowUp(t *testing.T) {
	t.Parallel()

	name := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	advanced, err := prompts.NewConfirm(prompts.ConfirmConfig{ID: "advanced", Label: "Advanced"})
	if err != nil {
		t.Fatalf("NewConfirm() error = %v", err)
	}
	note := newTextPrompt(t, prompts.TextConfig{ID: "note", Label: "Note"})
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "setup",
		Fields: []prompts.FormField{
			prompts.AsField(name), prompts.AsField(advanced),
			prompts.When(prompts.AsField(note), func(result prompts.FormResult) bool {
				value, present := prompts.FormValue[bool](result, "advanced")
				return present && value
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(40, 8)
	terminal.Push(prompts.PasteEvent("Brian"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.PasteEvent("yes"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.PasteEvent("details"), prompts.KeyEvent(prompts.KeyEnter))
	result, err := prompts.RunForm(context.Background(), form, interactiveExecution(terminal))
	if err != nil {
		t.Fatalf("RunForm() error = %v", err)
	}
	if value, ok := prompts.FormValue[string](result, "name"); !ok || value != "Brian" {
		t.Fatalf("name = %q, %v", value, ok)
	}
	if value, ok := prompts.FormValue[string](result, "note"); !ok || value != "details" {
		t.Fatalf("note = %q, %v", value, ok)
	}
	if got := result.IDs(); fmt.Sprint(got) != "[name advanced note]" {
		t.Fatalf("IDs() = %v", got)
	}
}

func TestInteractiveFormBackNavigationRetainsDraftsAndReevaluatesConditions(t *testing.T) {
	t.Parallel()

	name := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	advanced, err := prompts.NewConfirm(prompts.ConfirmConfig{ID: "advanced", Label: "Advanced"})
	if err != nil {
		t.Fatal(err)
	}
	details := newTextPrompt(t, prompts.TextConfig{ID: "details", Label: "Details"})
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "navigation",
		Fields: []prompts.FormField{
			prompts.AsField(name), prompts.AsField(advanced),
			prompts.When(prompts.AsField(details), func(result prompts.FormResult) bool {
				value, ok := prompts.FormValue[bool](result, "advanced")
				return ok && value
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.PasteEvent("Ada"), prompts.KeyEvent(prompts.KeyTab),
		prompts.PasteEvent("yes"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.PasteEvent("retained draft"), prompts.KeyEvent(prompts.KeyShiftTab),
		prompts.KeyEvent(prompts.KeyHome), prompts.KeyEvent(prompts.KeyDelete),
		prompts.KeyEvent(prompts.KeyDelete), prompts.KeyEvent(prompts.KeyDelete),
		prompts.PasteEvent("no"), prompts.KeyEvent(prompts.KeyEnter),
	)
	terminal.CloseInput()
	result, err := prompts.RunForm(context.Background(), form, interactiveExecution(terminal))
	if err != nil {
		t.Fatalf("RunForm() error = %v", err)
	}
	if name, ok := prompts.FormValue[string](result, "name"); !ok || name != "Ada" {
		t.Fatalf("name = %q, %v", name, ok)
	}
	if advanced, ok := prompts.FormValue[bool](result, "advanced"); !ok || advanced {
		t.Fatalf("advanced = %v, %v", advanced, ok)
	}
	if result.Has("details") {
		t.Fatalf("conditional result survived back navigation: %v", result.IDs())
	}
}

func TestInteractiveFormRetainsTextSelectionAndByteSecretDrafts(t *testing.T) {
	t.Parallel()

	option, err := prompts.NewOption(prompts.OptionConfig[string]{
		ID: "prod", Label: "Production", Value: "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "environment", Label: "Environment", Options: []prompts.Option[string]{option},
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	note := newTextPrompt(t, prompts.TextConfig{ID: "note", Label: "Note"})
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "drafts", Fields: []prompts.FormField{
			prompts.AsField(selection), prompts.AsField(secret), prompts.AsField(note),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.KeyEvent(prompts.KeyShiftTab), prompts.KeyEvent(prompts.KeyTab),
		prompts.PasteBytesEvent([]byte("secret")), prompts.KeyEvent(prompts.KeyEnter),
		prompts.PasteEvent("draft"), prompts.KeyEvent(prompts.KeyShiftTab),
		prompts.KeyEvent(prompts.KeyShiftTab),
		prompts.KeyEvent(prompts.KeyTab),
		prompts.KeyEvent(prompts.KeyTab),
		prompts.KeyEvent(prompts.KeyEnter),
	)
	terminal.CloseInput()
	result, err := prompts.RunForm(context.Background(), form, interactiveExecution(terminal))
	if err != nil {
		t.Fatalf("RunForm() error = %v", err)
	}
	if environment, ok := prompts.FormValue[string](result, "environment"); !ok || environment != "production" {
		t.Fatalf("environment = %q, %v", environment, ok)
	}
	token, ok := prompts.FormValue[*prompts.SecretBytes](result, "token")
	if !ok || string(token.Reveal()) != "secret" {
		t.Fatalf("token = %v, %v", token, ok)
	}
	token.Destroy()
	if note, ok := prompts.FormValue[string](result, "note"); !ok || note != "draft" {
		t.Fatalf("note = %q, %v", note, ok)
	}
	result.DestroySecrets()
}

func TestInteractiveFormBackNavigationSkipsInactiveFields(t *testing.T) {
	t.Parallel()

	first := newTextPrompt(t, prompts.TextConfig{ID: "first", Label: "First"})
	hidden := newTextPrompt(t, prompts.TextConfig{ID: "hidden", Label: "Hidden"})
	last := newTextPrompt(t, prompts.TextConfig{ID: "last", Label: "Last"})
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "skip-inactive", Fields: []prompts.FormField{
			prompts.AsField(first),
			prompts.When(prompts.AsField(hidden), func(prompts.FormResult) bool { return false }),
			prompts.AsField(last),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.PasteEvent("one"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.PasteEvent("three"), prompts.KeyEvent(prompts.KeyShiftTab),
		prompts.KeyEvent(prompts.KeyHome), prompts.KeyEvent(prompts.KeyDelete),
		prompts.KeyEvent(prompts.KeyDelete), prompts.KeyEvent(prompts.KeyDelete),
		prompts.PasteEvent("updated"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.KeyEvent(prompts.KeyEnter),
	)
	terminal.CloseInput()
	result, err := prompts.RunForm(context.Background(), form, interactiveExecution(terminal))
	if err != nil || result.Has("hidden") {
		t.Fatalf("RunForm() = %v, %v", result.IDs(), err)
	}
	if first, _ := prompts.FormValue[string](result, "first"); first != "updated" {
		t.Fatalf("first = %q", first)
	}
	if last, _ := prompts.FormValue[string](result, "last"); last != "three" {
		t.Fatalf("last = %q", last)
	}
}

func TestFormSkipsFalseConditionAndDefensivelyCopiesResults(t *testing.T) {
	t.Parallel()

	optionA, err := prompts.NewOption(prompts.OptionConfig[string]{ID: "a", Label: "A", Value: "a"})
	if err != nil {
		t.Fatalf("NewOption() error = %v", err)
	}
	selectMany, err := prompts.NewMultiSelect(prompts.MultiSelectConfig[string]{
		ID: "many", Label: "Many", Options: []prompts.Option[string]{optionA},
		FallbackIDs: prompts.Some([]string{"a"}), Headless: prompts.HeadlessUseFallback,
	})
	if err != nil {
		t.Fatalf("NewMultiSelect() error = %v", err)
	}
	skipped := newTextPrompt(t, prompts.TextConfig{ID: "skipped", Label: "Skipped"})
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "copy",
		Fields: []prompts.FormField{
			prompts.AsField(selectMany),
			prompts.When(prompts.AsField(skipped), func(prompts.FormResult) bool { return false }),
		},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	result, err := prompts.RunForm(context.Background(), form, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil {
		t.Fatalf("RunForm() error = %v", err)
	}
	first, ok := prompts.FormValue[[]string](result, "many")
	if !ok || len(first) != 1 || result.Has("skipped") {
		t.Fatalf("result = %#v, %v", first, result.IDs())
	}
	first[0] = "changed"
	second, _ := prompts.FormValue[[]string](result, "many")
	if second[0] != "a" {
		t.Fatal("FormValue() exposed stored slice memory")
	}
	ids := result.IDs()
	ids[0] = "changed"
	if result.IDs()[0] != "many" {
		t.Fatal("IDs() exposed internal order")
	}
	if _, ok := prompts.FormValue[int](result, "many"); ok {
		t.Fatal("FormValue() accepted the wrong type")
	}
}

func TestFormValidationIdentifiesFieldsAndRedactsSecrets(t *testing.T) {
	t.Parallel()

	secret, err := prompts.NewSecret(prompts.SecretConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
		Fallback: prompts.Some(prompts.NewSecretValue(secretCanary)),
		Headless: prompts.HeadlessUseFallback,
	})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "secure", Fields: []prompts.FormField{prompts.AsField(secret)},
		Validate: []prompts.FormValidator{func(context.Context, prompts.FormResult, prompts.ValidationContext) error {
			return prompts.NewValidationIssue("mismatch", secretCanary, "token")
		}},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	_, err = prompts.RunForm(context.Background(), form, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if !errors.Is(err, prompts.ErrValidationExhausted) || strings.Contains(fmt.Sprintf("%+v", err), secretCanary) {
		t.Fatalf("form validation error = %v", err)
	}
	var issue *prompts.ValidationIssue
	if !errors.As(err, &issue) || issue.Code() != "form_validation" || fmt.Sprint(issue.Fields()) != "[token]" {
		t.Fatalf("form validation issue = %#v", issue)
	}
}

func TestFormRejectsInvalidDefinitionsAndCallbackPanic(t *testing.T) {
	t.Parallel()

	_, err := prompts.NewForm(prompts.FormConfig{})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("empty form error = %v", err)
	}
	if prompts.When(nil, func(prompts.FormResult) bool { return true }) != nil {
		t.Fatal("When() did not preserve a nil field")
	}
	_, err = prompts.NewForm(prompts.FormConfig{ID: "nil", Fields: []prompts.FormField{nil}})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil form field error = %v", err)
	}
	field := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Fallback: prompts.Some("value"),
		Headless: prompts.HeadlessUseFallback,
	})
	_, err = prompts.NewForm(prompts.FormConfig{
		ID: "duplicate", Fields: []prompts.FormField{prompts.AsField(field), prompts.AsField(field)},
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("duplicate form field error = %v", err)
	}
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "panic",
		Fields: []prompts.FormField{prompts.When(prompts.AsField(field), func(prompts.FormResult) bool {
			panic("condition")
		})},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	_, err = prompts.RunForm(context.Background(), form, prompts.Execution{Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly}})
	if !errors.Is(err, prompts.ErrAdapter) {
		t.Fatalf("condition panic error = %v", err)
	}
}

func TestRunFormPropagatesFieldAndMidExecutionCancellation(t *testing.T) {
	t.Parallel()

	field := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	form, err := prompts.NewForm(prompts.FormConfig{ID: "failure", Fields: []prompts.FormField{prompts.AsField(field)}})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	_, err = prompts.RunForm(context.Background(), form, prompts.Execution{Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly}})
	if !errors.Is(err, prompts.ErrInteractionNotPermitted) {
		t.Fatalf("field error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	conditional, err := prompts.NewForm(prompts.FormConfig{
		ID: "conditional",
		Fields: []prompts.FormField{prompts.When(prompts.AsField(field), func(prompts.FormResult) bool {
			cancel()
			return false
		})},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	_, err = prompts.RunForm(ctx, conditional, prompts.Execution{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("condition cancellation = %v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	validated, err := prompts.NewForm(prompts.FormConfig{
		ID: "validated",
		Fields: []prompts.FormField{prompts.AsField(newTextPrompt(t, prompts.TextConfig{
			ID: "value", Label: "Value", Fallback: prompts.Some("ok"), Headless: prompts.HeadlessUseFallback,
		}))},
		Validate: []prompts.FormValidator{func(context.Context, prompts.FormResult, prompts.ValidationContext) error {
			cancel()
			return nil
		}},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	_, err = prompts.RunForm(ctx, validated, prompts.Execution{Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("validator cancellation = %v", err)
	}
}

func TestFormValidationHandlesSafeErrorsAndByteSecrets(t *testing.T) {
	t.Parallel()

	secret := prompts.NewSecretBytes([]byte(secretCanary))
	prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
		Fallback: prompts.Some(secret), Headless: prompts.HeadlessUseFallback,
	})
	if err != nil {
		t.Fatalf("NewSecretBytesPrompt() error = %v", err)
	}
	form, err := prompts.NewForm(prompts.FormConfig{
		ID: "bytes", Fields: []prompts.FormField{prompts.AsField(prompt)},
		Validate: []prompts.FormValidator{func(context.Context, prompts.FormResult, prompts.ValidationContext) error {
			return errors.New(secretCanary)
		}},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	_, err = prompts.RunForm(context.Background(), form, prompts.Execution{Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly}})
	if !errors.Is(err, prompts.ErrValidationExhausted) || strings.Contains(fmt.Sprintf("%+v", err), secretCanary) {
		t.Fatalf("byte secret validation error = %v", err)
	}
	ownedForm, err := prompts.NewForm(prompts.FormConfig{
		ID: "owned", Fields: []prompts.FormField{prompts.AsField(prompt)},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	owned, err := prompts.RunForm(context.Background(), ownedForm, prompts.Execution{Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly}})
	if err != nil {
		t.Fatalf("RunForm() error = %v", err)
	}
	copyBeforeDestroy, ok := prompts.FormValue[*prompts.SecretBytes](owned, "token")
	if !ok || string(copyBeforeDestroy.Reveal()) != secretCanary {
		t.Fatal("FormValue() did not clone its owned byte secret")
	}
	owned.DestroySecrets()
	destroyed, ok := prompts.FormValue[*prompts.SecretBytes](owned, "token")
	if !ok || destroyed.Len() != 0 || copyBeforeDestroy.Destroyed() {
		t.Fatal("DestroySecrets() did not isolate caller copies")
	}

	safe := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Fallback: prompts.Some("value"), Headless: prompts.HeadlessUseFallback,
	})
	safeForm, err := prompts.NewForm(prompts.FormConfig{
		ID: "safe", Fields: []prompts.FormField{prompts.AsField(safe)},
		Validate: []prompts.FormValidator{func(context.Context, prompts.FormResult, prompts.ValidationContext) error {
			return prompts.NewValidationIssue("cross_field", "Values disagree", "name")
		}},
	})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	_, err = prompts.RunForm(context.Background(), safeForm, prompts.Execution{Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly}})
	var issue *prompts.ValidationIssue
	if !errors.As(err, &issue) || issue.Code() != "cross_field" || issue.Message() != "Values disagree" {
		t.Fatalf("safe form validation error = %v", err)
	}
}

func TestRunFormRejectsNilAndCanceledContexts(t *testing.T) {
	t.Parallel()

	field := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	form, err := prompts.NewForm(prompts.FormConfig{ID: "setup", Fields: []prompts.FormField{prompts.AsField(field)}})
	if err != nil {
		t.Fatalf("NewForm() error = %v", err)
	}
	//lint:ignore SA1012 Nil context behavior is part of the public contract.
	_, err = prompts.RunForm(nil, form, prompts.Execution{}) //nolint:staticcheck // contract test
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = prompts.RunForm(ctx, form, prompts.Execution{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	if _, ok := prompts.FormValue[string](prompts.FormResult{}, "missing"); ok {
		t.Fatal("FormValue() found a missing field")
	}
}
