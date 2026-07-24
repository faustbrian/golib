package prompts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const (
	defaultMaxPasteBytes = 1 << 20
	defaultMaxInputBytes = 4 << 20
)

func runInteractive[T any](ctx context.Context, prompt Prompt[T], execution Execution) (result T, resultErr error) {
	if execution.Events == nil || execution.Terminal == nil || execution.Output == nil {
		return result, promptFailure(prompt.ID(), ErrTerminalUnavailable)
	}
	if prompt.definition.retry.Unlimited && !execution.Policy.PermitUnlimitedRetries {
		return result, invalidBehaviorDefinition("execute prompt", prompt.ID(), fmt.Errorf("%w: unlimited retry lacks caller permission", ErrInvalidDefinition))
	}
	if err := execution.Terminal.Acquire(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, contextFailure(prompt.ID(), ctxErr)
		}
		return result, terminalFailure(prompt.ID(), "acquire terminal", err)
	}
	defer func() {
		var cleanupErr error
		if err := execution.Terminal.SetEcho(true); err != nil {
			cleanupErr = terminalFailure(prompt.ID(), "restore terminal echo", err)
		}
		if err := execution.Terminal.Release(); err != nil {
			releaseErr := terminalFailure(prompt.ID(), "release terminal", err)
			cleanupErr = errors.Join(cleanupErr, releaseErr)
		}
		if cleanupErr != nil {
			var zero T
			result = zero
			if resultErr == nil {
				resultErr = cleanupErr
			} else {
				resultErr = &Error{
					Kind: ErrorTerminalControl, Operation: "restore terminal",
					PromptID: prompt.ID(), Cause: errors.Join(resultErr, cleanupErr),
				}
			}
		}
	}()
	if err := execution.Terminal.SetEcho(false); err != nil {
		return result, terminalFailure(prompt.ID(), "configure terminal echo", err)
	}

	limits := normalizeInputLimits(execution.Limits)
	if prompt.definition.kind == KindSecretBytes {
		return runInteractiveSecretBytes(ctx, prompt, execution, limits)
	}
	if prompt.definition.selection != nil {
		return runInteractiveSelection(ctx, prompt, execution, *prompt.definition.selection)
	}
	editor := lineEditor{maxBytes: limits.MaxInputBytes}
	navigation := formInteractionFrom(ctx)
	if navigation != nil && navigation.initial != nil && navigation.initial.kind == formReplayText {
		_ = editor.insert(navigation.initial.text, prompt.definition.kind == KindMultiline)
	}
	width := execution.Capabilities.Width
	if err := writeInteractive(execution, prompt.definition, editor.text(), "", width); err != nil {
		return result, err
	}
	attempts := uint(0)
	for {
		event, err := execution.Events.Next(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, contextFailure(prompt.ID(), ctxErr)
			}
			if errors.Is(err, io.EOF) {
				return resolveEOF(ctx, prompt, execution.Dependencies)
			}
			return result, eventReadFailure(prompt.ID(), "read terminal event", err)
		}

		submit := false
		switch event.Kind {
		case EventEOF:
			return resolveEOF(ctx, prompt, execution.Dependencies)
		case EventDetached:
			return result, streamFailure(prompt.ID(), ErrorTerminalDetached, "read terminal event", ErrTerminalDetached)
		case EventResize:
			if event.Width < 0 || event.Height < 0 {
				return result, streamFailure(prompt.ID(), ErrorReader, "read terminal resize", ErrReader)
			}
			width = event.Width
		case EventCapabilities:
			if err := applyCapabilityChange(&execution, event.Capabilities, &width, nil); err != nil {
				if errors.Is(err, ErrTerminalDetached) {
					return result, streamFailure(prompt.ID(), ErrorTerminalDetached, "update terminal capabilities", err)
				}
				return result, streamFailure(prompt.ID(), ErrorReader, "update terminal capabilities", err)
			}
		case EventPaste:
			if len(event.Text) > limits.MaxPasteBytes || !utf8.ValidString(event.Text) {
				return result, streamFailure(prompt.ID(), ErrorReader, "read terminal paste", ErrReader)
			}
			if err := editor.insert(event.Text, prompt.definition.kind == KindMultiline); err != nil {
				return result, streamFailure(prompt.ID(), ErrorReader, "read terminal paste", err)
			}
		case EventKey:
			event = execution.Keys.translate(event)
			if navigation != nil && event.Key == KeyShiftTab {
				navigation.captureText(editor.text())
				return result, errFormBack
			}
			switch event.Key {
			case KeyEscape, KeyCtrlC:
				return resolveCancel(ctx, prompt, execution.Dependencies)
			case KeyCtrlD:
				return resolveEOF(ctx, prompt, execution.Dependencies)
			case KeyEnter:
				submit = true
			case KeyNewline:
				if prompt.definition.kind == KindMultiline {
					if err := editor.insert("\n", true); err != nil {
						return result, streamFailure(prompt.ID(), ErrorReader, "edit prompt input", err)
					}
				}
			case KeyTab:
				if navigation != nil {
					submit = true
				}
			default:
				if err := editor.applyKey(event); err != nil {
					return result, streamFailure(prompt.ID(), ErrorReader, "edit terminal input", err)
				}
			}
		default:
			return result, streamFailure(prompt.ID(), ErrorReader, "read terminal event", ErrReader)
		}
		if submit {
			value, parseErr := prompt.definition.parse(editor.text())
			if parseErr == nil {
				value, parseErr = applyPipeline(ctx, prompt.definition, value, execution.Dependencies, false)
			} else {
				parseErr = validationFailure(prompt.ID(), parseErr, prompt.definition.secret)
			}
			if parseErr == nil {
				navigation.captureText(editor.text())
				return value, nil
			}
			if !errors.Is(parseErr, ErrValidationExhausted) {
				return result, parseErr
			}
			attempts++
			if !prompt.definition.retry.Unlimited && attempts >= prompt.definition.retry.MaxAttempts {
				return result, parseErr
			}
			if err := writeInteractive(
				execution, prompt.definition, editor.text(), validationMessage(parseErr), width,
			); err != nil {
				return result, err
			}
			continue
		}
		if err := writeInteractive(execution, prompt.definition, editor.text(), "", width); err != nil {
			return result, err
		}
	}
}

func normalizeInputLimits(limits InputLimits) InputLimits {
	if limits.MaxPasteBytes <= 0 {
		limits.MaxPasteBytes = defaultMaxPasteBytes
	}
	if limits.MaxInputBytes <= 0 {
		limits.MaxInputBytes = defaultMaxInputBytes
	}
	return limits
}

func writeInteractive[T any](execution Execution, definition definition[T], value, validation string, width int) error {
	renderer := execution.Renderer
	if renderer == nil {
		if execution.Capabilities.Color == ColorNone {
			renderer = PlainRenderer{Theme: execution.Theme}
		} else {
			renderer = ANSIRenderer{Theme: execution.Theme}
		}
	}
	shown := value
	if definition.secret != SecretNone {
		if value == "" {
			shown = ""
		} else {
			shown = "secret entered"
		}
	} else if value == "" {
		shown = definition.placeholder
	}
	lines := []SemanticLine{
		Line(Text(RoleLabel, presentationLabel(definition))),
		Line(Text(RoleValue, shown)),
	}
	lines = append(lines, presentationMetadata(definition)...)
	if validation != "" {
		lines = append(lines, Line(Text(RoleError, validation)))
	}
	output, err := renderer.Render(NewFrame(lines...), RenderOptions{
		Width: width, Color: execution.Capabilities.Color,
		ASCIIOnly:  !execution.Capabilities.Unicode,
		Hyperlinks: execution.Capabilities.Hyperlinks,
	})
	if err != nil {
		return streamFailure(definition.id, ErrorRenderer, "render prompt", err)
	}
	if _, err = io.WriteString(execution.Output, output); err != nil {
		return streamFailure(definition.id, ErrorWriter, "write prompt", err)
	}
	return nil
}

func presentationLabel[T any](definition definition[T]) string {
	if definition.accessibility.Label != "" {
		return definition.accessibility.Label
	}

	return definition.label
}

func presentationMetadata[T any](definition definition[T]) []SemanticLine {
	description := definition.description
	if definition.accessibility.Description != "" {
		description = definition.accessibility.Description
	}
	lines := make([]SemanticLine, 0, 4)
	if description != "" {
		lines = append(lines, Line(Text(RoleHint, description)))
	}
	if definition.hint != "" {
		lines = append(lines, Line(Text(RoleHint, definition.hint)))
	}
	if definition.help != "" {
		lines = append(lines, Line(Text(RoleHelp, definition.help)))
	}
	if definition.accessibility.TextualHint != "" {
		lines = append(lines, Line(Text(RoleHelp, definition.accessibility.TextualHint)))
	}

	return lines
}

func validationMessage(err error) string {
	var issue *ValidationIssue
	if errors.As(err, &issue) {
		return issue.Message()
	}
	return "Value was rejected"
}

func resolveCancel[T any](ctx context.Context, prompt Prompt[T], dependencies any) (T, error) {
	return resolveBehavior(ctx, prompt, dependencies, prompt.definition.cancel == CancelUseDefault,
		prompt.definition.cancel == CancelUseFallback, ErrCanceled)
}

func resolveEOF[T any](ctx context.Context, prompt Prompt[T], dependencies any) (T, error) {
	return resolveBehavior(ctx, prompt, dependencies, prompt.definition.endOfInput == EOFUseDefault,
		prompt.definition.endOfInput == EOFUseFallback, ErrEndOfInput)
}

func resolveBehavior[T any](ctx context.Context, prompt Prompt[T], dependencies any, useDefault, useFallback bool, target error) (T, error) {
	if useDefault {
		if value, ok := prompt.definition.defaultValue.Get(); ok {
			return applyPipeline(ctx, prompt.definition, value, dependencies, true)
		}
	}
	if useFallback {
		if value, ok := prompt.definition.fallbackValue.Get(); ok {
			return applyPipeline(ctx, prompt.definition, value, dependencies, true)
		}
	}
	var zero T
	kind := ErrorCanceled
	if errors.Is(target, ErrEndOfInput) {
		kind = ErrorEndOfInput
	}
	return zero, streamFailure(prompt.ID(), kind, "read terminal event", target)
}

func terminalFailure(promptID, operation string, cause error) error {
	return streamFailure(promptID, ErrorTerminalControl, operation, cause)
}

func streamFailure(promptID string, kind ErrorKind, operation string, cause error) error {
	return &Error{Kind: kind, Operation: operation, PromptID: promptID, Cause: cause}
}

func eventReadFailure(promptID, operation string, cause error) error {
	if errors.Is(cause, ErrTerminalDetached) {
		return streamFailure(promptID, ErrorTerminalDetached, operation, cause)
	}

	return streamFailure(promptID, ErrorReader, operation, cause)
}

func applyCapabilityChange(execution *Execution, capabilities Capabilities, width, height *int) error {
	if capabilities.Width < 0 || capabilities.Height < 0 || capabilities.Color > ColorTrueColor {
		return ErrReader
	}
	if !capabilities.InputTerminal || !capabilities.OutputTerminal {
		return ErrTerminalDetached
	}
	execution.Capabilities = capabilities
	*width = capabilities.Width
	if height != nil {
		*height = capabilities.Height
	}

	return nil
}

type lineEditor struct {
	cells    []string
	cursor   int
	maxBytes int
}

func (editor *lineEditor) text() string { return strings.Join(editor.cells, "") }

func (editor *lineEditor) insert(value string, multiline bool) error {
	if !multiline && strings.ContainsAny(value, "\r\n") {
		return ErrReader
	}
	clean := strings.Map(func(char rune) rune {
		if char == '\n' && multiline {
			return char
		}
		if unicode.IsControl(char) || isBidiControl(char) {
			return -1
		}
		return char
	}, value)
	if len(editor.text())+len(clean) > editor.maxBytes {
		return ErrReader
	}
	inserted := splitGraphemes(clean)
	editor.cells = append(editor.cells, make([]string, len(inserted))...)
	copy(editor.cells[editor.cursor+len(inserted):], editor.cells[editor.cursor:])
	copy(editor.cells[editor.cursor:], inserted)
	editor.cursor += len(inserted)
	return nil
}

func (editor *lineEditor) applyKey(event InputEvent) error {
	switch event.Key {
	case KeyRune:
		if unicode.IsControl(event.Rune) || isBidiControl(event.Rune) {
			return ErrReader
		}
		return editor.insert(string(event.Rune), false)
	case KeyBackspace:
		if editor.cursor > 0 {
			editor.cells = append(editor.cells[:editor.cursor-1], editor.cells[editor.cursor:]...)
			editor.cursor--
		}
	case KeyDelete:
		if editor.cursor < len(editor.cells) {
			editor.cells = append(editor.cells[:editor.cursor], editor.cells[editor.cursor+1:]...)
		}
	case KeyLeft:
		if editor.cursor > 0 {
			editor.cursor--
		}
	case KeyRight:
		if editor.cursor < len(editor.cells) {
			editor.cursor++
		}
	case KeyHome:
		editor.cursor = 0
	case KeyEnd:
		editor.cursor = len(editor.cells)
	case KeyWordLeft:
		editor.wordLeft()
	case KeyWordRight:
		editor.wordRight()
	case KeyTab, KeyShiftTab, KeyUp, KeyDown, KeyPageUp, KeyPageDown, KeyIgnored:
		return nil
	default:
		return ErrReader
	}
	return nil
}

func (editor *lineEditor) wordLeft() {
	for editor.cursor > 0 && strings.TrimSpace(editor.cells[editor.cursor-1]) == "" {
		editor.cursor--
	}
	for editor.cursor > 0 && strings.TrimSpace(editor.cells[editor.cursor-1]) != "" {
		editor.cursor--
	}
}

func (editor *lineEditor) wordRight() {
	for editor.cursor < len(editor.cells) && strings.TrimSpace(editor.cells[editor.cursor]) != "" {
		editor.cursor++
	}
	for editor.cursor < len(editor.cells) && strings.TrimSpace(editor.cells[editor.cursor]) == "" {
		editor.cursor++
	}
}

func splitGraphemes(value string) []string {
	graphemes := uniseg.NewGraphemes(value)
	result := make([]string, 0, utf8.RuneCountInString(value))
	for graphemes.Next() {
		result = append(result, graphemes.Str())
	}
	return result
}
