package prompts_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestInteractiveTextEditingAndSubmission(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name", Hint: "Public value"})
	terminal := prompts.NewVirtualTerminal(24, 8)
	terminal.Push(
		prompts.RuneEvent('a'), prompts.RuneEvent('b'), prompts.KeyEvent(prompts.KeyLeft),
		prompts.RuneEvent('X'), prompts.KeyEvent(prompts.KeyEnd), prompts.KeyEvent(prompts.KeyBackspace),
		prompts.PasteEvent("👩‍💻é"), prompts.KeyEvent(prompts.KeyEnter),
	)
	terminal.CloseInput()

	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "aX👩‍💻é" {
		t.Fatalf("Run() = %q, %v", value, err)
	}
	if !terminal.Released() || !terminal.EchoEnabled() {
		t.Fatal("terminal was not restored after submission")
	}
	output := terminal.Output()
	if !strings.Contains(output, "Name") || !strings.Contains(output, "Public value") || !strings.Contains(output, value) {
		t.Fatalf("interactive output = %q", output)
	}
}

func TestInteractiveRenderingUsesAccessibilityMetadata(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Visual name", Description: "Visual description",
		Accessibility: prompts.Accessibility{
			Label: "Account holder name", Description: "Public account value",
			TextualHint: "Type the complete name",
		},
	})
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("Ada"), prompts.KeyEvent(prompts.KeyEnter))
	if _, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal)); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := terminal.Output()
	for _, text := range []string{
		"Account holder name", "Public account value", "Type the complete name",
	} {
		if !strings.Contains(output, text) {
			t.Fatalf("accessible output missing %q: %q", text, output)
		}
	}
	if strings.Contains(output, "Visual name") || strings.Contains(output, "Visual description") {
		t.Fatalf("accessible overrides were ignored: %q", output)
	}
}

func TestInteractiveEditorNavigationAndPasteLimits(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.PasteEvent("one two"), prompts.KeyEvent(prompts.KeyWordLeft),
		prompts.KeyEvent(prompts.KeyDelete), prompts.RuneEvent('T'), prompts.KeyEvent(prompts.KeyHome),
		prompts.KeyEvent(prompts.KeyDelete), prompts.KeyEvent(prompts.KeyEnd),
		prompts.KeyEvent(prompts.KeyEnter),
	)
	terminal.CloseInput()
	execution := interactiveExecution(terminal)
	execution.Limits = prompts.InputLimits{MaxPasteBytes: 64, MaxInputBytes: 64}

	value, err := prompts.Run(context.Background(), prompt, execution)
	if err != nil || value != "ne Two" {
		t.Fatalf("Run() = %q, %v", value, err)
	}

	limited := prompts.NewVirtualTerminal(80, 24)
	limited.Push(prompts.PasteEvent("too-long"))
	limited.CloseInput()
	execution = interactiveExecution(limited)
	execution.Limits = prompts.InputLimits{MaxPasteBytes: 3, MaxInputBytes: 64}
	_, err = prompts.Run(context.Background(), prompt, execution)
	if !errors.Is(err, prompts.ErrReader) || strings.Contains(err.Error(), "too-long") {
		t.Fatalf("oversized paste error = %v", err)
	}
}

func TestInteractiveRetryCancelEOFAndDetach(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Retry: prompts.RetryPolicy{MaxAttempts: 2},
		PostValidate: []prompts.Validator[string]{func(_ context.Context, value string, _ prompts.ValidationContext) error {
			if value != "valid" {
				return prompts.NewValidationIssue("invalid_name", "Choose another name", "name")
			}
			return nil
		}},
	})
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("bad"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.KeyEvent(prompts.KeyHome), prompts.KeyEvent(prompts.KeyDelete),
		prompts.KeyEvent(prompts.KeyDelete), prompts.KeyEvent(prompts.KeyDelete),
		prompts.PasteEvent("valid"), prompts.KeyEvent(prompts.KeyEnter))
	terminal.CloseInput()
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "valid" || !strings.Contains(terminal.Output(), "Choose another name") {
		t.Fatalf("retry Run() = %q, %v, output %q", value, err, terminal.Output())
	}

	tests := []struct {
		name  string
		event prompts.InputEvent
		want  error
	}{
		{"escape", prompts.KeyEvent(prompts.KeyEscape), prompts.ErrCanceled},
		{"control c", prompts.KeyEvent(prompts.KeyCtrlC), prompts.ErrCanceled},
		{"control d", prompts.KeyEvent(prompts.KeyCtrlD), prompts.ErrEndOfInput},
		{"end of stream", prompts.InputEvent{Kind: prompts.EventEOF}, prompts.ErrEndOfInput},
		{"detached", prompts.InputEvent{Kind: prompts.EventDetached}, prompts.ErrTerminalDetached},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			virtual := prompts.NewVirtualTerminal(80, 24)
			virtual.Push(test.event)
			_, runErr := prompts.Run(context.Background(), prompt, interactiveExecution(virtual))
			if !errors.Is(runErr, test.want) || !virtual.Released() {
				t.Fatalf("Run() error = %v, released %v", runErr, virtual.Released())
			}
		})
	}
}

func TestInteractiveCancelAndEOFModesUseOwnedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cancel prompts.CancelBehavior
		eof    prompts.EOFBehavior
		event  prompts.InputEvent
		want   string
	}{
		{"cancel default", prompts.CancelUseDefault, prompts.EOFReturnError, prompts.KeyEvent(prompts.KeyEscape), "default"},
		{"cancel fallback", prompts.CancelUseFallback, prompts.EOFReturnError, prompts.KeyEvent(prompts.KeyCtrlC), "fallback"},
		{"eof default", prompts.CancelReturnError, prompts.EOFUseDefault, prompts.KeyEvent(prompts.KeyCtrlD), "default"},
		{"eof fallback", prompts.CancelReturnError, prompts.EOFUseFallback, prompts.InputEvent{Kind: prompts.EventEOF}, "fallback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			prompt := newTextPrompt(t, prompts.TextConfig{
				ID: "name", Label: "Name", Default: prompts.Some("default"),
				Fallback: prompts.Some("fallback"), Cancel: test.cancel, EndOfInput: test.eof,
			})
			terminal := prompts.NewVirtualTerminal(80, 24)
			terminal.Push(test.event)
			got, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
			if err != nil || got != test.want {
				t.Fatalf("Run() = %q, %v", got, err)
			}
		})
	}
}

func TestInteractiveSecretNeverRendersAndRestoresEcho(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSecret(prompts.SecretConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
	})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent(secretCanary), prompts.KeyEvent(prompts.KeyEnter))
	terminal.CloseInput()
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value.Reveal() != secretCanary {
		t.Fatalf("Run() = %v, %v", value, err)
	}
	if strings.Contains(terminal.Output(), secretCanary) || !strings.Contains(terminal.Output(), "secret entered") {
		t.Fatalf("secret output = %q", terminal.Output())
	}
	if !terminal.EchoEnabled() || !terminal.Released() {
		t.Fatal("secret terminal state was not restored")
	}
}

func TestInteractiveTerminalFailuresAreTypedAndRestored(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	tests := []struct {
		name      string
		configure func(*prompts.VirtualTerminal)
		want      error
	}{
		{"acquire", func(v *prompts.VirtualTerminal) { v.FailAcquire(io.ErrClosedPipe) }, prompts.ErrTerminalControl},
		{"echo", func(v *prompts.VirtualTerminal) { v.FailEcho(io.ErrClosedPipe) }, prompts.ErrTerminalControl},
		{"release", func(v *prompts.VirtualTerminal) { v.FailRelease(io.ErrClosedPipe) }, prompts.ErrTerminalControl},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			terminal := prompts.NewVirtualTerminal(80, 24)
			terminal.Push(prompts.PasteEvent("value"), prompts.KeyEvent(prompts.KeyEnter))
			test.configure(terminal)
			_, runErr := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
			if !errors.Is(runErr, test.want) {
				t.Fatalf("Run() error = %v", runErr)
			}
		})
	}

	writer := &failingWriter{err: io.ErrClosedPipe}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.KeyEvent(prompts.KeyEnter))
	execution := interactiveExecution(terminal)
	execution.Output = writer
	_, err := prompts.Run(context.Background(), prompt, execution)
	if !errors.Is(err, prompts.ErrWriter) || !terminal.Released() {
		t.Fatalf("writer failure = %v, released %v", err, terminal.Released())
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.InputEvent{Kind: prompts.EventDetached})
	terminal.FailRelease(io.ErrClosedPipe)
	_, err = prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrTerminalControl) || !errors.Is(err, prompts.ErrTerminalDetached) {
		t.Fatalf("combined operation and restoration error = %v", err)
	}
}

func TestInteractiveAcquireCancellationIsTypedAsCancellation(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	ctx, cancel := context.WithCancel(context.Background())
	terminal := &cancelAcquireTerminal{cancel: cancel}
	execution := interactiveExecution(prompts.NewVirtualTerminal(80, 24))
	execution.Terminal = terminal
	_, err := prompts.Run(ctx, prompt, execution)
	if !errors.Is(err, prompts.ErrCanceled) || errors.Is(err, prompts.ErrTerminalControl) {
		t.Fatalf("Run() error = %v", err)
	}
}

func interactiveExecution(terminal *prompts.VirtualTerminal) prompts.Execution {
	return prompts.Execution{
		Output: terminal, Events: terminal, Terminal: terminal,
		Capabilities: prompts.Capabilities{
			InputTerminal: true, OutputTerminal: true, Width: terminal.Width(),
			Height: terminal.Height(), Unicode: true,
		},
		Policy: prompts.InteractionPolicy{Mode: prompts.InteractiveRequired, PermitInteraction: true},
	}
}

type failingWriter struct{ err error }

func (writer *failingWriter) Write([]byte) (int, error) { return 0, writer.err }

type nthFailWriter struct {
	calls, failAt int
}

func (writer *nthFailWriter) Write(value []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failAt {
		return 0, io.ErrClosedPipe
	}
	return len(value), nil
}

func TestInputEventConstructorsAndFormattingStaySafe(t *testing.T) {
	t.Parallel()

	capabilities := prompts.Capabilities{Width: 12, Height: 3, Unicode: true}
	if event := prompts.CapabilityEvent(capabilities); event.Kind != prompts.EventCapabilities ||
		event.Capabilities != capabilities {
		t.Fatalf("CapabilityEvent() = %#v", event)
	}
	if event := prompts.ResizeEvent(12, 3); event.Kind != prompts.EventResize || event.Width != 12 || event.Height != 3 {
		t.Fatalf("ResizeEvent() = %#v", event)
	}
	event := prompts.PasteEvent(secretCanary)
	text, err := event.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	for _, got := range []string{
		event.String(), event.GoString(), fmt.Sprint(event), fmt.Sprintf("%#v", event),
		string(text), string(encoded), event.LogValue().String(),
	} {
		if strings.Contains(got, secretCanary) || !strings.Contains(got, "INPUT EVENT") {
			t.Fatalf("event formatting exposed input = %q", got)
		}
	}
}

func TestInteractiveEventAndRendererFailures(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Description: "Description", Help: "Help",
		Placeholder: "Placeholder",
	})
	tests := []struct {
		name   string
		source prompts.EventSource
		ctx    context.Context
		want   error
		kind   prompts.ErrorKind
	}{
		{"source error", eventSourceFunc(func(context.Context) (prompts.InputEvent, error) { return prompts.InputEvent{}, io.ErrUnexpectedEOF }), context.Background(), prompts.ErrReader, prompts.ErrorReader},
		{"source detached", eventSourceFunc(func(context.Context) (prompts.InputEvent, error) {
			return prompts.InputEvent{}, prompts.ErrTerminalDetached
		}), context.Background(), prompts.ErrTerminalDetached, prompts.ErrorTerminalDetached},
		{"source eof", eventSourceFunc(func(context.Context) (prompts.InputEvent, error) { return prompts.InputEvent{}, io.EOF }), context.Background(), prompts.ErrEndOfInput, prompts.ErrorEndOfInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			terminal := prompts.NewVirtualTerminal(80, 24)
			execution := interactiveExecution(terminal)
			execution.Events = test.source
			_, err := prompts.Run(test.ctx, prompt, execution)
			var typed *prompts.Error
			if !errors.Is(err, test.want) || !errors.As(err, &typed) ||
				typed.Kind != test.kind || !terminal.Released() {
				t.Fatalf("Run() error = %v, released %v", err, terminal.Released())
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	terminal := prompts.NewVirtualTerminal(80, 24)
	execution := interactiveExecution(terminal)
	execution.Events = eventSourceFunc(func(context.Context) (prompts.InputEvent, error) {
		cancel()
		return prompts.InputEvent{}, context.Canceled
	})
	_, err := prompts.Run(ctx, prompt, execution)
	if !errors.Is(err, context.Canceled) || !terminal.Released() {
		t.Fatalf("event cancellation = %v, released %v", err, terminal.Released())
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.KeyEvent(prompts.KeyEnter))
	execution = interactiveExecution(terminal)
	execution.Renderer = rendererFunc(func(prompts.Frame, prompts.RenderOptions) (string, error) {
		return "", io.ErrClosedPipe
	})
	_, err = prompts.Run(context.Background(), prompt, execution)
	if !errors.Is(err, prompts.ErrRenderer) || !terminal.Released() {
		t.Fatalf("renderer failure = %v, released %v", err, terminal.Released())
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("value"))
	execution = interactiveExecution(terminal)
	execution.Output = &nthFailWriter{failAt: 2}
	_, err = prompts.Run(context.Background(), prompt, execution)
	if !errors.Is(err, prompts.ErrWriter) || !terminal.Released() {
		t.Fatalf("event render failure = %v, released %v", err, terminal.Released())
	}
}

func TestInteractiveResizeInvalidEventsAndParsing(t *testing.T) {
	t.Parallel()

	integer, err := prompts.NewInteger(prompts.IntegerConfig{
		ID: "count", Label: "Count", Retry: prompts.RetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("NewInteger() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.ResizeEvent(12, 3), prompts.PasteEvent("bad"), prompts.KeyEvent(prompts.KeyEnter))
	terminal.CloseInput()
	_, err = prompts.Run(context.Background(), integer, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrValidationExhausted) || terminal.Width() != 12 || terminal.Height() != 3 {
		t.Fatalf("integer Run() error = %v, size %dx%d", err, terminal.Width(), terminal.Height())
	}

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	badEvents := []prompts.InputEvent{
		prompts.ResizeEvent(-1, 2),
		{Kind: prompts.EventKind(200)},
		{Kind: prompts.EventKey, Key: prompts.Key(200)},
		prompts.RuneEvent('\n'),
		prompts.PasteEvent("line\nbreak"),
		prompts.PasteEvent(string([]byte{0xff})),
	}
	for _, event := range badEvents {
		virtual := prompts.NewVirtualTerminal(80, 24)
		virtual.Push(event)
		_, runErr := prompts.Run(context.Background(), prompt, interactiveExecution(virtual))
		if !errors.Is(runErr, prompts.ErrReader) {
			t.Fatalf("event %#v error = %v", event, runErr)
		}
	}

	tooLarge := prompts.NewVirtualTerminal(80, 24)
	tooLarge.Push(prompts.PasteEvent("four"))
	execution := interactiveExecution(tooLarge)
	execution.Limits = prompts.InputLimits{MaxPasteBytes: 8, MaxInputBytes: 3}
	_, err = prompts.Run(context.Background(), prompt, execution)
	if !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("input limit error = %v", err)
	}
}

func TestInteractiveCapabilityChangesUpdateFallbackAndDetectLoss(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.CapabilityEvent(prompts.Capabilities{
			InputTerminal: true, OutputTerminal: true, Width: 20, Height: 4,
		}),
		prompts.PasteEvent("界"), prompts.KeyEvent(prompts.KeyEnter),
	)
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "界" || !strings.Contains(terminal.Output(), "\\u{754C}") {
		t.Fatalf("Run() = %q, %v; output %q", value, err, terminal.Output())
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.CapabilityEvent(prompts.Capabilities{}))
	_, err = prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrTerminalDetached) || !terminal.Released() {
		t.Fatalf("terminal loss = %v, released %v", err, terminal.Released())
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.CapabilityEvent(prompts.Capabilities{
		InputTerminal: true, OutputTerminal: true, Width: -1,
	}))
	_, err = prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("invalid capability error = %v", err)
	}
}

func TestInteractiveNavigationNoOpsAndMultiline(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{ID: "name", Label: "Name"})
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.KeyEvent(prompts.KeyBackspace), prompts.KeyEvent(prompts.KeyDelete),
		prompts.KeyEvent(prompts.KeyLeft), prompts.KeyEvent(prompts.KeyRight),
		prompts.PasteEvent("one two"), prompts.KeyEvent(prompts.KeyHome),
		prompts.KeyEvent(prompts.KeyWordRight), prompts.KeyEvent(prompts.KeyRight),
		prompts.KeyEvent(prompts.KeyLeft), prompts.RuneEvent('X'),
		prompts.KeyEvent(prompts.KeyTab), prompts.KeyEvent(prompts.KeyShiftTab),
		prompts.KeyEvent(prompts.KeyUp), prompts.KeyEvent(prompts.KeyDown),
		prompts.KeyEvent(prompts.KeyPageUp), prompts.KeyEvent(prompts.KeyPageDown),
		prompts.KeyEvent(prompts.KeyNewline),
		prompts.KeyEvent(prompts.KeyEnter),
	)
	value, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "one Xtwo" {
		t.Fatalf("navigation Run() = %q, %v", value, err)
	}
	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("one "), prompts.KeyEvent(prompts.KeyWordLeft),
		prompts.RuneEvent('X'), prompts.KeyEvent(prompts.KeyEnter))
	value, err = prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil || value != "Xone " {
		t.Fatalf("whitespace navigation Run() = %q, %v", value, err)
	}

	multiline, err := prompts.NewMultiline(prompts.MultilineConfig{ID: "body", Label: "Body"})
	if err != nil {
		t.Fatalf("NewMultiline() error = %v", err)
	}
	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(
		prompts.PasteEvent("first"), prompts.KeyEvent(prompts.KeyNewline),
		prompts.PasteEvent("second\tline"), prompts.KeyEvent(prompts.KeyEnter),
	)
	value, err = prompts.Run(context.Background(), multiline, interactiveExecution(terminal))
	if err != nil || value != "first\nsecondline" {
		t.Fatalf("multiline Run() = %q, %v", value, err)
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.RuneEvent('x'), prompts.KeyEvent(prompts.KeyNewline))
	execution := interactiveExecution(terminal)
	execution.Limits = prompts.InputLimits{MaxPasteBytes: 1, MaxInputBytes: 1}
	if _, err := prompts.Run(context.Background(), multiline, execution); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("multiline limit error = %v", err)
	}
}

func TestInteractiveUnlimitedRetryRequiresAuthority(t *testing.T) {
	t.Parallel()

	prompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name", Retry: prompts.RetryPolicy{Unlimited: true},
		PostValidate: []prompts.Validator[string]{func(_ context.Context, value string, _ prompts.ValidationContext) error {
			if value == "ok" {
				return nil
			}
			return errors.New("rejected")
		}},
	})
	terminal := prompts.NewVirtualTerminal(80, 24)
	_, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("unpermitted unlimited retry error = %v", err)
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("bad"), prompts.KeyEvent(prompts.KeyEnter),
		prompts.KeyEvent(prompts.KeyHome), prompts.KeyEvent(prompts.KeyDelete),
		prompts.KeyEvent(prompts.KeyDelete), prompts.KeyEvent(prompts.KeyDelete),
		prompts.PasteEvent("ok"), prompts.KeyEvent(prompts.KeyEnter))
	execution := interactiveExecution(terminal)
	execution.Policy.PermitUnlimitedRetries = true
	value, err := prompts.Run(context.Background(), prompt, execution)
	if err != nil || value != "ok" || !strings.Contains(terminal.Output(), "rejected") {
		t.Fatalf("unlimited retry Run() = %q, %v", value, err)
	}
}

func TestInteractiveCallbackAndRetryRenderFailuresRestoreTerminal(t *testing.T) {
	t.Parallel()

	panicPrompt, err := prompts.NewSecret(prompts.SecretConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
		PostValidate: []prompts.Validator[prompts.SecretValue]{func(context.Context, prompts.SecretValue, prompts.ValidationContext) error {
			panic(secretCanary)
		}},
	})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent(secretCanary), prompts.KeyEvent(prompts.KeyEnter))
	_, err = prompts.Run(context.Background(), panicPrompt, interactiveExecution(terminal))
	if !errors.Is(err, prompts.ErrAdapter) || !terminal.Released() || !terminal.EchoEnabled() || strings.Contains(terminal.Output(), secretCanary) {
		t.Fatalf("panic Run() = %v, released %v, echo %v", err, terminal.Released(), terminal.EchoEnabled())
	}

	retryPrompt := newTextPrompt(t, prompts.TextConfig{
		ID: "name", Label: "Name",
		PostValidate: []prompts.Validator[string]{func(context.Context, string, prompts.ValidationContext) error {
			return errors.New("rejected")
		}},
	})
	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("bad"), prompts.KeyEvent(prompts.KeyEnter))
	calls := 0
	execution := interactiveExecution(terminal)
	execution.Renderer = rendererFunc(func(frame prompts.Frame, options prompts.RenderOptions) (string, error) {
		calls++
		if calls == 3 {
			return "", io.ErrClosedPipe
		}
		return prompts.PlainRenderer{}.Render(frame, options)
	})
	_, err = prompts.Run(context.Background(), retryPrompt, execution)
	if !errors.Is(err, prompts.ErrRenderer) || !terminal.Released() {
		t.Fatalf("retry render failure = %v, released %v", err, terminal.Released())
	}
}

func TestInteractiveANSIAndEchoRestoreFailures(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSecret(prompts.SecretConfig{ID: "token", Label: "Token", Class: prompts.SecretToken})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("value"), prompts.KeyEvent(prompts.KeyEnter))
	execution := interactiveExecution(terminal)
	execution.Capabilities.Color = prompts.ColorANSI16
	value, err := prompts.Run(context.Background(), prompt, execution)
	if err != nil || value.Reveal() != "value" || !strings.Contains(terminal.Output(), "\x1b[") {
		t.Fatalf("ANSI secret Run() = %v, %v, output %q", value, err, terminal.Output())
	}

	controller := &restoreFailTerminal{}
	terminal = prompts.NewVirtualTerminal(80, 24)
	terminal.Push(prompts.PasteEvent("value"), prompts.KeyEvent(prompts.KeyEnter))
	execution = interactiveExecution(terminal)
	execution.Terminal = controller
	_, err = prompts.Run(context.Background(), prompt, execution)
	if !errors.Is(err, prompts.ErrTerminalControl) || !controller.released {
		t.Fatalf("echo restoration error = %v, released %v", err, controller.released)
	}
}

func TestVirtualTerminalLifecycleAndCancellation(t *testing.T) {
	t.Parallel()

	terminal := prompts.NewVirtualTerminal(10, 4)
	if terminal.Acquired() || terminal.Height() != 4 {
		t.Fatal("unexpected initial virtual terminal state")
	}
	if err := terminal.Acquire(context.Background()); err != nil || !terminal.Acquired() {
		t.Fatalf("Acquire() = %v, acquired %v", err, terminal.Acquired())
	}
	terminal.Push(prompts.ResizeEvent(8, 2))
	event, err := terminal.Next(context.Background())
	if err != nil || event.Kind != prompts.EventResize || terminal.Width() != 8 || terminal.Height() != 2 {
		t.Fatalf("Next() = %#v, %v", event, err)
	}
	terminal.CloseInput()
	terminal.CloseInput()
	if err := terminal.Push(prompts.KeyEvent(prompts.KeyEnter)); !errors.Is(err, prompts.ErrEndOfInput) {
		t.Fatalf("closed Push() error = %v", err)
	}
	_, err = terminal.Next(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("closed Next() error = %v", err)
	}

	waiting := prompts.NewVirtualTerminal(1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = waiting.Next(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Next() error = %v", err)
	}
}

func TestVirtualTerminalRejectsBoundedQueueOverflow(t *testing.T) {
	t.Parallel()

	terminal := prompts.NewVirtualTerminal(1, 1)
	events := make([]prompts.InputEvent, 4097)
	if err := terminal.Push(events...); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("Push() overflow error = %v", err)
	}
}

type eventSourceFunc func(context.Context) (prompts.InputEvent, error)

func (source eventSourceFunc) Next(ctx context.Context) (prompts.InputEvent, error) {
	return source(ctx)
}

type rendererFunc func(prompts.Frame, prompts.RenderOptions) (string, error)

func (renderer rendererFunc) Render(frame prompts.Frame, options prompts.RenderOptions) (string, error) {
	return renderer(frame, options)
}

type restoreFailTerminal struct {
	echoCalls int
	released  bool
}

type cancelAcquireTerminal struct{ cancel context.CancelFunc }

func (terminal *cancelAcquireTerminal) Acquire(context.Context) error {
	terminal.cancel()

	return context.Canceled
}
func (*cancelAcquireTerminal) SetEcho(bool) error { return nil }
func (*cancelAcquireTerminal) Release() error     { return nil }

func (*restoreFailTerminal) Acquire(context.Context) error { return nil }
func (terminal *restoreFailTerminal) SetEcho(bool) error {
	terminal.echoCalls++
	if terminal.echoCalls == 2 {
		return io.ErrClosedPipe
	}
	return nil
}
func (terminal *restoreFailTerminal) Release() error {
	terminal.released = true
	return nil
}
