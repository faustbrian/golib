package prompts

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestByteLineEditorCoversEditingAndBounds(t *testing.T) {
	t.Parallel()

	editor := byteLineEditor{maxBytes: 64}
	if editor.renderValue() != "" {
		t.Fatal("empty editor rendered a value")
	}
	if err := editor.insert([]byte{0xff}); !errors.Is(err, ErrReader) {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
	if err := editor.insert([]byte("line\n")); !errors.Is(err, ErrReader) {
		t.Fatalf("newline error = %v", err)
	}
	if err := editor.insert([]byte("one\x01\u202e two👩‍💻")); err != nil {
		t.Fatalf("insert() error = %v", err)
	}
	if got := string(editor.bytes()); got != "one two👩‍💻" {
		t.Fatalf("bytes() = %q", got)
	}
	if editor.renderValue() != "secret entered" {
		t.Fatal("populated editor did not render safe state")
	}
	for _, key := range []Key{KeyHome, KeyRight, KeyDelete, KeyBackspace, KeyEnd,
		KeyLeft, KeyWordLeft, KeyWordRight, KeyLeft, KeyRight, KeyTab, KeyShiftTab,
		KeyUp, KeyDown, KeyPageUp, KeyPageDown, KeyIgnored} {
		if err := editor.applyKey(KeyEvent(key)); err != nil {
			t.Fatalf("applyKey(%v) error = %v", key, err)
		}
	}
	if err := editor.applyKey(RuneEvent('!')); err != nil {
		t.Fatalf("rune error = %v", err)
	}
	if err := editor.applyKey(RuneEvent('\x01')); !errors.Is(err, ErrReader) {
		t.Fatalf("control rune error = %v", err)
	}
	if err := editor.applyKey(RuneEvent('\u202e')); !errors.Is(err, ErrReader) {
		t.Fatalf("bidi rune error = %v", err)
	}
	if err := editor.applyKey(KeyEvent(KeyEnter)); !errors.Is(err, ErrReader) {
		t.Fatalf("unsupported key error = %v", err)
	}
	editor.destroy()
	if editor.size != 0 || editor.cells != nil {
		t.Fatal("destroy() retained editor state")
	}

	limited := byteLineEditor{maxBytes: 1}
	if err := limited.insert([]byte("ab")); !errors.Is(err, ErrReader) {
		t.Fatalf("limit error = %v", err)
	}
	words := byteLineEditor{maxBytes: 16}
	if err := words.insert([]byte("one two")); err != nil {
		t.Fatal(err)
	}
	words.wordLeft()
	words.wordLeft()
	words.wordRight()
	words.wordRight()
	if words.cursor != len(words.cells) {
		t.Fatalf("word cursor = %d", words.cursor)
	}
}

func TestHandleSecretByteEventCoversSemanticEvents(t *testing.T) {
	t.Parallel()

	var nilEvent *InputEvent
	nilEvent.Destroy()
	limits := InputLimits{MaxPasteBytes: 4, MaxInputBytes: 16}
	for name, test := range map[string]struct {
		event  InputEvent
		action secretInputAction
		err    error
	}{
		"eof":              {event: InputEvent{Kind: EventEOF}, action: secretEOF},
		"detached":         {event: InputEvent{Kind: EventDetached}, err: ErrTerminalDetached},
		"resize":           {event: ResizeEvent(40, 10), action: secretContinue},
		"invalid resize":   {event: ResizeEvent(-1, 10), err: ErrReader},
		"large bytes":      {event: PasteBytesEvent([]byte("large")), err: ErrReader},
		"large text":       {event: PasteEvent("large"), err: ErrReader},
		"text":             {event: PasteEvent("ok"), action: secretContinue},
		"escape":           {event: KeyEvent(KeyEscape), action: secretCancel},
		"control c":        {event: KeyEvent(KeyCtrlC), action: secretCancel},
		"control d":        {event: KeyEvent(KeyCtrlD), action: secretEOF},
		"enter":            {event: KeyEvent(KeyEnter), action: secretSubmit},
		"ignored":          {event: KeyEvent(KeyIgnored), action: secretContinue},
		"unknown event":    {event: InputEvent{Kind: EventKind(200)}, err: ErrReader},
		"invalid byte key": {event: KeyEvent(Key(200)), err: ErrReader},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			event := test.event
			editor := byteLineEditor{maxBytes: limits.MaxInputBytes}
			width := 80
			action, err := handleSecretByteEvent(&event, &editor, KeyMap{}, &width, limits)
			if action != test.action || !errors.Is(err, test.err) {
				t.Fatalf("handle() = %v, %v", action, err)
			}
			if name == "resize" && width != 40 {
				t.Fatalf("width = %d", width)
			}
		})
	}
}

func TestSecretByteExecutionCoversReaderAndParserFailures(t *testing.T) {
	t.Parallel()

	prompt, err := NewSecretBytesPrompt(SecretBytesConfig{
		ID: "secret", Label: "Secret", Class: SecretToken,
		Retry: RetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]EventSource{
		"reader": eventSourceFunc(func(context.Context) (InputEvent, error) {
			return InputEvent{}, errors.New("read failure")
		}),
		"eof": eventSourceFunc(func(context.Context) (InputEvent, error) {
			return InputEvent{}, io.EOF
		}),
		"semantic eof": eventSourceFunc(func(context.Context) (InputEvent, error) {
			return InputEvent{Kind: EventEOF}, nil
		}),
		"detached": eventSourceFunc(func(context.Context) (InputEvent, error) {
			return InputEvent{Kind: EventDetached}, nil
		}),
		"invalid event": eventSourceFunc(func(context.Context) (InputEvent, error) {
			return ResizeEvent(-1, 10), nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			terminal := NewVirtualTerminal(80, 24)
			execution := secretInteractiveExecution(terminal)
			execution.Events = source
			_, err := Run(context.Background(), prompt, execution)
			if err == nil {
				t.Fatal("Run() returned nil error")
			}
			if name == "detached" && !errors.Is(err, ErrTerminalDetached) {
				t.Fatalf("detached error = %v", err)
			}
		})
	}

	parserFailure := errors.New("parser failure")
	broken := prompt
	broken.definition.parseBytes = func([]byte) (*SecretBytes, error) {
		return nil, parserFailure
	}
	terminal := NewVirtualTerminal(80, 24)
	terminal.Push(KeyEvent(KeyEnter))
	_, err = Run(context.Background(), broken, secretInteractiveExecution(terminal))
	if !errors.Is(err, ErrValidationExhausted) {
		t.Fatalf("parser error = %v", err)
	}
	_, err = ParseBytes(context.Background(), broken, []byte("secret"), nil)
	if !errors.Is(err, ErrValidationExhausted) {
		t.Fatalf("ParseBytes() parser error = %v", err)
	}
}

func TestSecretByteExecutionCoversRenderingRetryAndCallbackFailures(t *testing.T) {
	t.Parallel()

	prompt, err := NewSecretBytesPrompt(SecretBytesConfig{
		ID: "secret", Label: "Secret", Class: SecretToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := NewVirtualTerminal(80, 24)
	execution := secretInteractiveExecution(terminal)
	execution.Output = errorWriter{err: io.ErrClosedPipe}
	if _, err := Run(context.Background(), prompt, execution); !errors.Is(err, ErrWriter) {
		t.Fatalf("initial writer error = %v", err)
	}

	terminal = NewVirtualTerminal(80, 24)
	terminal.Push(RuneEvent('a'))
	execution = secretInteractiveExecution(terminal)
	execution.Output = &countingErrorWriter{failAt: 2}
	if _, err := Run(context.Background(), prompt, execution); !errors.Is(err, ErrWriter) {
		t.Fatalf("event writer error = %v", err)
	}

	attempts := 0
	retryPrompt, err := NewSecretBytesPrompt(SecretBytesConfig{
		ID: "retry", Label: "Retry", Class: SecretToken,
		PostValidate: []Validator[*SecretBytes]{func(
			context.Context, *SecretBytes, ValidationContext,
		) error {
			attempts++
			if attempts == 1 {
				return errors.New("rejected")
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal = NewVirtualTerminal(80, 24)
	terminal.Push(PasteBytesEvent([]byte("secret")), KeyEvent(KeyEnter), KeyEvent(KeyEnter))
	result, err := Run(context.Background(), retryPrompt, secretInteractiveExecution(terminal))
	if err != nil || string(result.Reveal()) != "secret" || attempts != 2 {
		t.Fatalf("retry Run() = %v, %v, attempts %d", result, err, attempts)
	}
	result.Destroy()

	attempts = 0
	terminal = NewVirtualTerminal(80, 24)
	terminal.Push(PasteBytesEvent([]byte("secret")), KeyEvent(KeyEnter))
	execution = secretInteractiveExecution(terminal)
	execution.Output = &countingErrorWriter{failAt: 3}
	if _, err := Run(context.Background(), retryPrompt, execution); !errors.Is(err, ErrWriter) {
		t.Fatalf("retry writer error = %v", err)
	}

	panicPrompt, err := NewSecretBytesPrompt(SecretBytesConfig{
		ID: "panic", Label: "Panic", Class: SecretToken,
		Transform: []Transformer[*SecretBytes]{func(
			context.Context, *SecretBytes, ValidationContext,
		) (*SecretBytes, error) {
			panic("secret callback")
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal = NewVirtualTerminal(80, 24)
	terminal.Push(KeyEvent(KeyEnter))
	if _, err := Run(context.Background(), panicPrompt, secretInteractiveExecution(terminal)); !errors.Is(err, ErrAdapter) {
		t.Fatalf("callback error = %v", err)
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	terminal = NewVirtualTerminal(80, 24)
	execution = secretInteractiveExecution(terminal)
	execution.Events = eventSourceFunc(func(context.Context) (InputEvent, error) {
		cancel()
		return InputEvent{}, context.Canceled
	})
	if _, err := Run(cancelContext, prompt, execution); !errors.Is(err, context.Canceled) {
		t.Fatalf("context reader error = %v", err)
	}
}

func TestSecretByteExecutionAppliesCapabilityChanges(t *testing.T) {
	t.Parallel()

	prompt, err := NewSecretBytesPrompt(SecretBytesConfig{
		ID: "secret", Label: "Secret", Class: SecretToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := NewVirtualTerminal(80, 24)
	terminal.Push(
		CapabilityEvent(Capabilities{
			InputTerminal: true, OutputTerminal: true, Width: 20, Height: 3,
		}),
		RuneEvent('x'), KeyEvent(KeyEnter),
	)
	result, err := Run(context.Background(), prompt, secretInteractiveExecution(terminal))
	if err != nil || string(result.Reveal()) != "x" {
		t.Fatalf("Run() = %v, %v", result, err)
	}
	result.Destroy()

	for name, capabilities := range map[string]Capabilities{
		"detached": {},
		"invalid": {
			InputTerminal: true, OutputTerminal: true, Width: -1,
		},
	} {
		terminal := NewVirtualTerminal(80, 24)
		terminal.Push(CapabilityEvent(capabilities))
		_, runErr := Run(context.Background(), prompt, secretInteractiveExecution(terminal))
		want := ErrReader
		if name == "detached" {
			want = ErrTerminalDetached
		}
		if !errors.Is(runErr, want) {
			t.Fatalf("%s capability error = %v", name, runErr)
		}
	}

	terminal = NewVirtualTerminal(80, 24)
	terminal.Push(CapabilityEvent(Capabilities{
		InputTerminal: true, OutputTerminal: true, Width: 20, Height: 3,
	}))
	execution := secretInteractiveExecution(terminal)
	execution.Output = &secretNthWriter{failAt: 2}
	if _, err := Run(context.Background(), prompt, execution); !errors.Is(err, ErrWriter) {
		t.Fatalf("capability render error = %v", err)
	}
}

type secretNthWriter struct {
	calls  int
	failAt int
}

func (writer *secretNthWriter) Write(content []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failAt {
		return 0, io.ErrClosedPipe
	}

	return len(content), nil
}

type eventSourceFunc func(context.Context) (InputEvent, error)

func (source eventSourceFunc) Next(ctx context.Context) (InputEvent, error) {
	return source(ctx)
}

func secretInteractiveExecution(terminal *VirtualTerminal) Execution {
	return Execution{
		Output: terminal, Events: terminal, Terminal: terminal,
		Capabilities: Capabilities{
			InputTerminal: true, OutputTerminal: true, Width: 80, Height: 24,
		},
		Policy: InteractionPolicy{Mode: InteractiveRequired, PermitInteraction: true},
	}
}

type errorWriter struct{ err error }

func (writer errorWriter) Write([]byte) (int, error) { return 0, writer.err }

type countingErrorWriter struct {
	writes int
	failAt int
}

func (writer *countingErrorWriter) Write(value []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.failAt {
		return 0, io.ErrClosedPipe
	}
	return len(value), nil
}
