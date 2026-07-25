package prompts_test

import (
	"errors"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestTextDefinitionRequiresStableIdentityAndLabel(t *testing.T) {
	t.Parallel()

	_, err := prompts.NewText(prompts.TextConfig{Label: "Name"})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("empty identity error = %v", err)
	}

	_, err = prompts.NewText(prompts.TextConfig{ID: "name"})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("empty label error = %v", err)
	}
}

func TestTextDefinitionHasStableIdentityAndExplicitOptionalValues(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID:       "name",
		Label:    "Name",
		Default:  prompts.Some(""),
		Fallback: prompts.Some("batch-name"),
	})
	if prompt.ID() != "name" {
		t.Fatalf("ID() = %q", prompt.ID())
	}

	value, ok := prompts.Some(0).Get()
	if !ok || value != 0 {
		t.Fatalf("Some(0).Get() = %d, %v", value, ok)
	}
	_, ok = (prompts.Optional[int]{}).Get()
	if ok {
		t.Fatal("zero Optional reported a present value")
	}
}

func newTextPrompt(t *testing.T, config prompts.TextConfig) prompts.Prompt[string] {
	t.Helper()

	prompt, err := prompts.NewText(config)
	if err != nil {
		t.Fatalf("NewText() error = %v", err)
	}

	return prompt
}
