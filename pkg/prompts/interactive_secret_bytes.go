package prompts

import (
	"context"
	"errors"
	"io"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

type secretInputAction uint8

const (
	secretContinue secretInputAction = iota
	secretSubmit
	secretCancel
	secretEOF
)

func runInteractiveSecretBytes[T any](
	ctx context.Context,
	prompt Prompt[T],
	execution Execution,
	limits InputLimits,
) (result T, resultErr error) {
	editor := byteLineEditor{maxBytes: limits.MaxInputBytes}
	defer editor.destroy()
	navigation := formInteractionFrom(ctx)
	if navigation != nil && navigation.initial != nil && navigation.initial.kind == formReplayBytes {
		initial := navigation.initial.bytes.Reveal()
		_ = editor.insert(initial)
		clear(initial)
	}
	width := execution.Capabilities.Width
	if err := writeInteractive(execution, prompt.definition, "", "", width); err != nil {
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
		if event.Kind == EventCapabilities {
			eventErr := applyCapabilityChange(&execution, event.Capabilities, &width, nil)
			event.Destroy()
			if eventErr != nil {
				if errors.Is(eventErr, ErrTerminalDetached) {
					return result, streamFailure(
						prompt.ID(), ErrorTerminalDetached, "update terminal capabilities", eventErr,
					)
				}
				return result, streamFailure(prompt.ID(), ErrorReader, "update terminal capabilities", eventErr)
			}
			if err := writeInteractive(execution, prompt.definition, editor.renderValue(), "", width); err != nil {
				return result, err
			}
			continue
		}
		keyMap := execution.Keys
		if navigation != nil && event.Kind == EventKey {
			translated := execution.Keys.translate(event)
			if translated.Key == KeyShiftTab {
				draft := editor.bytes()
				navigation.captureBytes(draft)
				clear(draft)
				event.Destroy()
				return result, errFormBack
			}
			if translated.Key == KeyTab {
				event = KeyEvent(KeyEnter)
			} else {
				event = translated
			}
			keyMap = KeyMap{}
		}
		action, eventErr := handleSecretByteEvent(&event, &editor, keyMap, &width, limits)
		if eventErr != nil {
			if errors.Is(eventErr, ErrTerminalDetached) {
				return result, streamFailure(
					prompt.ID(), ErrorTerminalDetached, "read terminal event", eventErr,
				)
			}
			return result, streamFailure(prompt.ID(), ErrorReader, "edit terminal input", eventErr)
		}
		switch action {
		case secretCancel:
			return resolveCancel(ctx, prompt, execution.Dependencies)
		case secretEOF:
			return resolveEOF(ctx, prompt, execution.Dependencies)
		case secretSubmit:
			input := editor.bytes()
			value, parseErr := prompt.definition.parseBytes(input)
			clear(input)
			if parseErr == nil {
				value, parseErr = applyPipeline(ctx, prompt.definition, value, execution.Dependencies, false)
			} else {
				parseErr = validationFailure(prompt.ID(), parseErr, prompt.definition.secret)
			}
			if parseErr == nil {
				draft := editor.bytes()
				navigation.captureBytes(draft)
				clear(draft)
				return value, nil
			}
			prompt.definition.destroy(value)
			if !errors.Is(parseErr, ErrValidationExhausted) {
				return result, parseErr
			}
			attempts++
			if !prompt.definition.retry.Unlimited && attempts >= prompt.definition.retry.MaxAttempts {
				return result, parseErr
			}
			if err := writeInteractive(
				execution, prompt.definition, "secret entered", validationMessage(parseErr), width,
			); err != nil {
				return result, err
			}
			continue
		case secretContinue:
			if err := writeInteractive(execution, prompt.definition, editor.renderValue(), "", width); err != nil {
				return result, err
			}
		}
	}
}

func handleSecretByteEvent(
	event *InputEvent,
	editor *byteLineEditor,
	keys KeyMap,
	width *int,
	limits InputLimits,
) (action secretInputAction, resultErr error) {
	defer event.Destroy()
	switch event.Kind {
	case EventEOF:
		return secretEOF, nil
	case EventDetached:
		return secretContinue, ErrTerminalDetached
	case EventResize:
		if event.Width < 0 || event.Height < 0 {
			return secretContinue, ErrReader
		}
		*width = event.Width
		return secretContinue, nil
	case EventPaste:
		if event.Bytes != nil {
			if event.Bytes.Len() > limits.MaxPasteBytes {
				return secretContinue, ErrReader
			}
			input := event.Bytes.Reveal()
			defer clear(input)
			return secretContinue, editor.insert(input)
		}
		if len(event.Text) > limits.MaxPasteBytes {
			return secretContinue, ErrReader
		}
		fallback := []byte(event.Text)
		defer clear(fallback)
		return secretContinue, editor.insert(fallback)
	case EventKey:
		translated := keys.translate(*event)
		switch translated.Key {
		case KeyEscape, KeyCtrlC:
			return secretCancel, nil
		case KeyCtrlD:
			return secretEOF, nil
		case KeyEnter:
			return secretSubmit, nil
		default:
			return secretContinue, editor.applyKey(translated)
		}
	default:
		return secretContinue, ErrReader
	}
}

type byteLineEditor struct {
	cells    [][]byte
	cursor   int
	size     int
	maxBytes int
}

func (editor *byteLineEditor) insert(input []byte) error {
	if !utf8.Valid(input) {
		return ErrReader
	}
	clean := make([]byte, 0, len(input))
	for len(input) > 0 {
		char, size := utf8.DecodeRune(input)
		if char == '\r' || char == '\n' {
			clear(clean)
			return ErrReader
		}
		if !unicode.IsControl(char) && !isBidiControl(char) {
			clean = append(clean, input[:size]...)
		}
		input = input[size:]
	}
	defer clear(clean)
	if editor.size+len(clean) > editor.maxBytes {
		return ErrReader
	}
	inserted := splitByteGraphemes(clean)
	editor.cells = append(editor.cells, make([][]byte, len(inserted))...)
	copy(editor.cells[editor.cursor+len(inserted):], editor.cells[editor.cursor:])
	copy(editor.cells[editor.cursor:], inserted)
	editor.cursor += len(inserted)
	editor.size += len(clean)

	return nil
}

func (editor *byteLineEditor) applyKey(event InputEvent) error {
	switch event.Key {
	case KeyRune:
		if unicode.IsControl(event.Rune) || isBidiControl(event.Rune) {
			return ErrReader
		}
		var encoded [utf8.UTFMax]byte
		size := utf8.EncodeRune(encoded[:], event.Rune)
		return editor.insert(encoded[:size])
	case KeyBackspace:
		if editor.cursor > 0 {
			editor.remove(editor.cursor - 1)
			editor.cursor--
		}
	case KeyDelete:
		if editor.cursor < len(editor.cells) {
			editor.remove(editor.cursor)
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

func (editor *byteLineEditor) remove(index int) {
	editor.size -= len(editor.cells[index])
	clear(editor.cells[index])
	copy(editor.cells[index:], editor.cells[index+1:])
	editor.cells[len(editor.cells)-1] = nil
	editor.cells = editor.cells[:len(editor.cells)-1]
}

func (editor *byteLineEditor) wordLeft() {
	for editor.cursor > 0 && byteClusterSpace(editor.cells[editor.cursor-1]) {
		editor.cursor--
	}
	for editor.cursor > 0 && !byteClusterSpace(editor.cells[editor.cursor-1]) {
		editor.cursor--
	}
}

func (editor *byteLineEditor) wordRight() {
	for editor.cursor < len(editor.cells) && !byteClusterSpace(editor.cells[editor.cursor]) {
		editor.cursor++
	}
	for editor.cursor < len(editor.cells) && byteClusterSpace(editor.cells[editor.cursor]) {
		editor.cursor++
	}
}

func (editor *byteLineEditor) bytes() []byte {
	result := make([]byte, 0, editor.size)
	for _, cell := range editor.cells {
		result = append(result, cell...)
	}

	return result
}

func (editor *byteLineEditor) renderValue() string {
	if editor.size == 0 {
		return ""
	}

	return "secret entered"
}

func (editor *byteLineEditor) destroy() {
	for _, cell := range editor.cells {
		clear(cell)
	}
	clear(editor.cells)
	editor.cells = nil
	editor.cursor = 0
	editor.size = 0
}

func splitByteGraphemes(input []byte) [][]byte {
	result := make([][]byte, 0, utf8.RuneCount(input))
	state := -1
	for len(input) > 0 {
		cluster, rest, _, nextState := uniseg.FirstGraphemeCluster(input, state)
		result = append(result, append([]byte(nil), cluster...))
		input = rest
		state = nextState
	}

	return result
}

func byteClusterSpace(cluster []byte) bool {
	char, _ := utf8.DecodeRune(cluster)

	return unicode.IsSpace(char)
}
