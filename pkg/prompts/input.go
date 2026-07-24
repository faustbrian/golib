package prompts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// EventKind identifies one bounded terminal input or capability event.
type EventKind uint8

const (
	EventKey EventKind = iota
	EventPaste
	EventResize
	EventEOF
	EventDetached
	EventCapabilities
)

// Key identifies a semantic key independently of terminal byte encoding.
type Key uint8

const (
	KeyRune Key = iota
	KeyEnter
	KeyEscape
	KeyCtrlC
	KeyCtrlD
	KeyTab
	KeyShiftTab
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyWordLeft
	KeyWordRight
	KeyPageUp
	KeyPageDown
	KeyIgnored
	KeyNewline
)

// InputEvent is a secret-redacting semantic input event.
type InputEvent struct {
	Kind          EventKind
	Key           Key
	Rune          rune
	Text          string
	Bytes         *SecretBytes
	Width, Height int
	Capabilities  Capabilities
}

// RuneEvent creates a text insertion event.
func RuneEvent(value rune) InputEvent { return InputEvent{Kind: EventKey, Key: KeyRune, Rune: value} }

// KeyEvent creates a non-text key event.
func KeyEvent(key Key) InputEvent { return InputEvent{Kind: EventKey, Key: key} }

// PasteEvent creates a bracketed or ordinary paste event.
func PasteEvent(value string) InputEvent { return InputEvent{Kind: EventPaste, Text: value} }

// PasteBytesEvent creates an owned byte-oriented paste event. Call Destroy
// when the event is not transferred to prompt execution.
func PasteBytesEvent(value []byte) InputEvent {
	return InputEvent{Kind: EventPaste, Bytes: NewSecretBytes(value)}
}

// ResizeEvent creates a terminal dimension event.
func ResizeEvent(width, height int) InputEvent {
	return InputEvent{Kind: EventResize, Width: width, Height: height}
}

// CapabilityEvent reports a complete replacement capability snapshot.
func CapabilityEvent(capabilities Capabilities) InputEvent {
	return InputEvent{Kind: EventCapabilities, Capabilities: capabilities}
}

// Destroy overwrites an owned byte payload and releases text references.
func (event *InputEvent) Destroy() {
	if event == nil {
		return
	}
	event.Bytes.Destroy()
	event.Text = ""
	event.Rune = 0
	event.Capabilities = Capabilities{}
}

func (InputEvent) String() string   { return "[INPUT EVENT]" }
func (InputEvent) GoString() string { return "[INPUT EVENT]" }

// Format prevents pasted or typed input from appearing through fmt.
func (InputEvent) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("[INPUT EVENT]")) }

// MarshalText redacts event payloads.
func (InputEvent) MarshalText() ([]byte, error) { return []byte("[INPUT EVENT]"), nil }

// MarshalJSON redacts event payloads.
func (InputEvent) MarshalJSON() ([]byte, error) { return json.Marshal("[INPUT EVENT]") }

// LogValue redacts event payloads in structured logging.
func (InputEvent) LogValue() slog.Value { return slog.StringValue("[INPUT EVENT]") }

// EventSource supplies decoded, cancellable semantic terminal events. It must
// honor context cancellation; the core never starts a goroutine around it.
type EventSource interface {
	Next(context.Context) (InputEvent, error)
}

// TerminalController owns terminal acquisition and echo restoration.
type TerminalController interface {
	Acquire(context.Context) error
	SetEcho(bool) error
	Release() error
}

// InputLimits bound retained input and individual paste events.
type InputLimits struct {
	MaxPasteBytes int
	MaxInputBytes int
}
