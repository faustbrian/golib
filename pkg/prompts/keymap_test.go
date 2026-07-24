package prompts_test

import (
	"context"
	"errors"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestKeyMapRebindsEditingAndSubmission(t *testing.T) {
	t.Parallel()

	keys, err := prompts.NewKeyMap(
		prompts.KeyBinding{Input: prompts.KeyTab, Meaning: prompts.KeyEnter},
		prompts.KeyBinding{Input: prompts.KeyEnd, Meaning: prompts.KeyLeft},
	)
	if err != nil {
		t.Fatalf("NewKeyMap() error = %v", err)
	}
	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.PasteEvent("ab"), prompts.KeyEvent(prompts.KeyEnd), prompts.RuneEvent('X'),
		prompts.KeyEvent(prompts.KeyEnter), prompts.RuneEvent('Y'), prompts.KeyEvent(prompts.KeyTab),
	)
	execution := interactiveExecution(terminal)
	execution.Keys = keys

	value, err := prompts.Run(context.Background(), prompt, execution)
	if err != nil || value != "aXYb" {
		t.Fatalf("Run() = %q, %v", value, err)
	}
}

func TestKeyMapRebindsCancellationAndSelection(t *testing.T) {
	t.Parallel()

	cancelKeys, err := prompts.NewKeyMap(
		prompts.KeyBinding{Input: prompts.KeyTab, Meaning: prompts.KeyEscape},
	)
	if err != nil {
		t.Fatalf("NewKeyMap() error = %v", err)
	}
	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.KeyEvent(prompts.KeyEscape), prompts.PasteEvent("value"), prompts.KeyEvent(prompts.KeyTab))
	execution := interactiveExecution(terminal)
	execution.Keys = cancelKeys
	if _, err := prompts.Run(context.Background(), prompt, execution); !errors.Is(err, prompts.ErrCanceled) {
		t.Fatalf("Run() error = %v", err)
	}

	nextKeys, err := prompts.NewKeyMap(
		prompts.KeyBinding{Input: prompts.KeyEnd, Meaning: prompts.KeyDown},
	)
	if err != nil {
		t.Fatalf("NewKeyMap() error = %v", err)
	}
	selection, err := prompts.NewSelect(prompts.SelectConfig[string]{
		ID: "environment", Label: "Environment", Options: selectionOptions(t), InitialID: "dev",
	})
	if err != nil {
		t.Fatalf("NewSelect() error = %v", err)
	}
	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.KeyEvent(prompts.KeyEnd), prompts.KeyEvent(prompts.KeyEnter))
	execution = interactiveExecution(terminal)
	execution.Keys = nextKeys
	value, err := prompts.Run(context.Background(), selection, execution)
	if err != nil || value != "production" {
		t.Fatalf("selection Run() = %q, %v", value, err)
	}
}

func TestKeyMapValidatesBindings(t *testing.T) {
	t.Parallel()

	if _, err := prompts.NewKeyMap(); err != nil {
		t.Fatalf("default NewKeyMap() error = %v", err)
	}
	tests := map[string][]prompts.KeyBinding{
		"rune input": {
			{Input: prompts.KeyRune, Meaning: prompts.KeyEnter},
		},
		"rune meaning": {
			{Input: prompts.KeyTab, Meaning: prompts.KeyRune},
		},
		"unknown input": {
			{Input: prompts.Key(255), Meaning: prompts.KeyEnter},
		},
		"unknown meaning": {
			{Input: prompts.KeyTab, Meaning: prompts.Key(255)},
		},
		"ignored input": {
			{Input: prompts.KeyIgnored, Meaning: prompts.KeyEnter},
		},
		"ignored meaning": {
			{Input: prompts.KeyTab, Meaning: prompts.KeyIgnored},
		},
		"duplicate input": {
			{Input: prompts.KeyTab, Meaning: prompts.KeyEnter},
			{Input: prompts.KeyTab, Meaning: prompts.KeyEscape},
		},
	}
	for name, bindings := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := prompts.NewKeyMap(bindings...); !errors.Is(err, prompts.ErrInvalidDefinition) {
				t.Fatalf("NewKeyMap() error = %v", err)
			}
		})
	}
}
