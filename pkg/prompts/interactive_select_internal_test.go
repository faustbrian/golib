package prompts

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInteractiveSelectionContainsOwnedParserFailure(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{{id: "one", label: "One"}}, maximum: 1,
	}
	prompt := Prompt[string]{definition: definition[string]{
		kind: KindSelect, id: "choice", label: "Choice",
		retry: RetryPolicy{MaxAttempts: 1}, selection: &details,
		parse: func(string) (string, error) { return "", errors.New("owned parser failed") },
	}}
	terminal := NewVirtualTerminal(40, 8)
	terminal.Push(KeyEvent(KeyEnter))
	_, err := Run(context.Background(), prompt, Execution{
		Output: terminal, Events: terminal, Terminal: terminal,
		Capabilities: Capabilities{InputTerminal: true, OutputTerminal: true},
		Policy:       InteractionPolicy{Mode: InteractiveRequired, PermitInteraction: true},
	})
	if !errors.Is(err, ErrValidationExhausted) || !terminal.Released() {
		t.Fatalf("Run() error = %v, released %v", err, terminal.Released())
	}
}

func TestSelectionStateEmptyAndDisabledOperationsAreStable(t *testing.T) {
	t.Parallel()

	empty := selectionState{details: selectionDetails{maximum: 1}, selected: map[string]bool{}}
	empty.ensureEnabled(1)
	empty.move(1)
	empty.focusLast()
	empty.toggle()
	if input, ok := empty.submission(); ok || input != "" || empty.message != "No selectable options" {
		t.Fatalf("empty submission = %q, %v, message %q", input, ok, empty.message)
	}

	disabled := selectionState{
		details: selectionDetails{
			options: []selectionOption{{id: "off", label: "Off", disabled: true}},
			maximum: 1,
		},
		visible: []int{0}, selected: map[string]bool{},
	}
	disabled.ensureEnabled(1)
	disabled.toggle()
	if input, ok := disabled.submission(); ok || input != "" {
		t.Fatalf("disabled submission = %q, %v", input, ok)
	}
}

func TestSelectionStateFocusToggleAndRanking(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{
			{id: "alpha", label: "Alpha", description: "first token"},
			{id: "beta", label: "Beta", description: "second match"},
		},
		initialIDs: []string{"alpha"}, multiple: true, maximum: 2,
		searchPolicy: SearchPolicy{MaxOptions: 2, MaxResults: 1, MaxQueryRunes: 10},
	}
	state := newSelectionState(details, 20, 4)
	state.focusLast()
	state.toggle()
	state.toggle()
	state.focusFirst()
	state.toggle()
	if state.selected["alpha"] {
		t.Fatal("toggle did not remove an initial selection")
	}

	state.query = lineEditor{cells: splitGraphemes("beta"), cursor: 4, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 || state.details.options[state.visible[0]].id != "beta" {
		t.Fatalf("exact filter = %#v", state.visible)
	}
	state.query = lineEditor{cells: splitGraphemes("second ma"), cursor: 9, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 || state.details.options[state.visible[0]].id != "beta" {
		t.Fatalf("prefix-token filter = %#v", state.visible)
	}
	state.query = lineEditor{cells: splitGraphemes("cond at"), cursor: 7, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 || state.details.options[state.visible[0]].id != "beta" {
		t.Fatalf("contains-token filter = %#v", state.visible)
	}

	state.query = lineEditor{cells: splitGraphemes("a"), cursor: 1, maxBytes: 40}
	state.filter()
	if len(state.visible) != 1 {
		t.Fatalf("result limit = %#v", state.visible)
	}
	if !strings.Contains(state.details.options[state.visible[0]].label, "Alpha") {
		t.Fatalf("stable limited match = %#v", state.visible)
	}
}

func TestSelectionStateAppliesFormReplayDefensively(t *testing.T) {
	t.Parallel()

	details := selectionDetails{
		options: []selectionOption{
			{id: "disabled", label: "Disabled", disabled: true},
			{id: "active", label: "Active"},
		},
		multiple: true, maximum: 2,
		searchPolicy: SearchPolicy{MaxOptions: 2, MaxResults: 2, MaxQueryRunes: 10},
	}
	state := newSelectionState(details, 20, 4)
	state.applyReplay(selectionReplay{
		selected: []string{"missing", "disabled", "active"},
		focusID:  "active", query: "act",
	})
	if len(state.selected) != 1 || !state.selected["active"] || state.replay().focusID != "active" {
		t.Fatalf("replayed state = %#v, %#v", state.selected, state.replay())
	}
}
