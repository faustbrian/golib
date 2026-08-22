package prompts

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInteractiveRequiresEachRuntimeDependency(t *testing.T) {
	t.Parallel()

	prompt := Prompt[string]{definition: definition[string]{id: "name", label: "Name"}}
	for name, omit := range
		map[string]func(
			*Execution,
		){
			"events": func(execution *Execution) {
				execution.Events = nil
			},
			"terminal": func(execution *Execution) {
				execution.Terminal = nil
			},
			"output": func(execution *Execution) {
				execution.Output = nil
			},
		} {
		t.Run(
			name,
			func(t *testing.T) {
				t.Parallel()

				terminal := NewVirtualTerminal(80, 24)
				execution := Execution{
					Output: terminal,
					Events: terminal,
					Terminal: terminal,
				}
				omit(&execution)
				_, err := runInteractive(context.Background(), prompt, execution)
				if !errors.Is(err, ErrTerminalUnavailable) || terminal.Acquired() {
					t.Fatalf(
						"runInteractive() error = %v, acquired = %t",
						err,
						terminal.Acquired(),
					)
				}
			},
		)
	}
}

func TestValidationMessageFallsBackForNonValidationErrors(t *testing.T) {
	t.Parallel()

	if got := validationMessage(errors.New("opaque validator failure"));
		got != "Value was rejected" {
		t.Fatalf("validationMessage() = %q", got)
	}
}

func TestInteractiveIgnoresReplayFromAnotherPromptKind(t *testing.T) {
	t.Parallel()

	terminal := NewVirtualTerminal(80, 24)
	if err := terminal.Push(PasteEvent("fresh"), KeyEvent(KeyEnter)); err != nil {
		t.Fatal(err)
	}
	interaction := &formInteraction{initial: &formReplay{kind: formReplayBytes, text: "stale"}}
	ctx := context.WithValue(context.Background(), formNavigationContextKey{}, interaction)
	prompt := Prompt[string]{
		definition: definition[string]{
			id: "name",
			label: "Name",
			kind: KindText,
			retry: RetryPolicy{MaxAttempts: 1},
			parse: func(value string) (string, error) {
				return value, nil
			},
		},
	}
	value, err := runInteractive(
		ctx,
		prompt,
		Execution{
			Output: terminal,
			Events: terminal,
			Terminal: terminal,
			Capabilities: Capabilities{InputTerminal: true, OutputTerminal: true},
		},
	)
	if err != nil || value != "fresh" {
		t.Fatalf("runInteractive() = %q, %v", value, err)
	}
}

func TestInteractiveTextRejectsMultilineReplayAndPaste(t *testing.T) {
	t.Parallel()

	prompt := Prompt[string]{
		definition: definition[string]{
			id: "name",
			label: "Name",
			kind: KindText,
			retry: RetryPolicy{MaxAttempts: 1},
			parse: func(value string) (string, error) {
				return value, nil
			},
		},
	}

	replayTerminal := NewVirtualTerminal(80, 24)
	if err := replayTerminal.Push(PasteEvent("fresh"), KeyEvent(KeyEnter)); err != nil {
		t.Fatal(err)
	}
	interaction := &formInteraction{initial: &formReplay{kind: formReplayText, text: "stale\n"}}
	ctx := context.WithValue(context.Background(), formNavigationContextKey{}, interaction)
	value, err := runInteractive(
		ctx,
		prompt,
		Execution{
			Output: replayTerminal,
			Events: replayTerminal,
			Terminal: replayTerminal,
			Capabilities: Capabilities{InputTerminal: true, OutputTerminal: true},
		},
	)
	if err != nil || value != "fresh" {
		t.Fatalf("multiline replay run = %q, %v", value, err)
	}

	pasteTerminal := NewVirtualTerminal(80, 24)
	if err := pasteTerminal.Push(PasteEvent("bad\n"), KeyEvent(KeyEnter)); err != nil {
		t.Fatal(err)
	}
	_, err = runInteractive(
		context.Background(),
		prompt,
		Execution{
			Output: pasteTerminal,
			Events: pasteTerminal,
			Terminal: pasteTerminal,
			Capabilities: Capabilities{InputTerminal: true, OutputTerminal: true},
		},
	)
	if !errors.Is(err, ErrReader) {
		t.Fatalf("multiline paste error = %v", err)
	}
}

func TestInteractiveSecretEmptyStateAndCapabilityBoundaries(t *testing.T) {
	t.Parallel()

	terminal := NewVirtualTerminal(80, 24)
	if err := writeInteractive(
		Execution{Output: terminal},
		definition[string]{id: "token", label: "Token", secret: SecretToken},
		"",
		"",
		80,
	);
		err != nil {
		t.Fatal(err)
	}
	if strings.Contains(terminal.Output(), "secret entered") {
		t.Fatalf("empty secret output = %q", terminal.Output())
	}

	execution := Execution{}
	width, height := -1, -1
	capabilities := Capabilities{
		InputTerminal: true,
		OutputTerminal: true,
		Color: ColorTrueColor,
	}
	if err := applyCapabilityChange(&execution, capabilities, &width, &height); err != nil {
		t.Fatalf("exact-bound capability error = %v", err)
	}
	if width != 0 || height != 0 || execution.Capabilities != capabilities {
		t.Fatalf("capability state = %#v, %d x %d", execution.Capabilities, width, height)
	}
	for name, detached := range
		map[string]Capabilities{
			"input": {OutputTerminal: true},
			"output": {InputTerminal: true},
		} {
		if err := applyCapabilityChange(&execution, detached, &width, &height);
			!errors.Is(err, ErrTerminalDetached) {
			t.Fatalf("missing %s terminal error = %v", name, err)
		}
	}
}

func TestLineEditorExactBoundsAndBoundaryNavigation(t *testing.T) {
	t.Parallel()

	editor := lineEditor{maxBytes: 4}
	if err := editor.insert("ab", false); err != nil {
		t.Fatal(err)
	}
	if err := editor.insert("cd", false); err != nil || editor.text() != "abcd" {
		t.Fatalf("exact-bound insert = %q, %v", editor.text(), err)
	}
	if err := editor.insert("e", false); !errors.Is(err, ErrReader) {
		t.Fatalf("overflow insert error = %v", err)
	}

	empty := lineEditor{maxBytes: 4}
	if err := empty.applyKey(KeyEvent(KeyLeft)); err != nil || empty.cursor != 0 {
		t.Fatalf("left boundary = %d, %v", empty.cursor, err)
	}
	for _, event := range []InputEvent{RuneEvent('\n'), RuneEvent('\u202e')} {
		if err := empty.applyKey(event); !errors.Is(err, ErrReader) {
			t.Fatalf("unsafe rune %#U error = %v", event.Rune, err)
		}
	}
}
