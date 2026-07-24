package prompts

import (
	"bytes"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
)

// DecoderConfig bounds undecoded terminal bytes and bracketed paste content.
type DecoderConfig struct {
	MaxPasteBytes  int
	MaxBufferBytes int
	ByteInput      bool
}

// Decoder incrementally translates common terminal byte sequences into
// semantic events. It reads no stream and owns no goroutine or terminal state.
type Decoder struct {
	mutex         sync.Mutex
	buffer        []byte
	paste         []byte
	inPaste       bool
	maxPasteBytes int
	maxBuffer     int
	byteInput     bool
}

// NewDecoder creates a bounded, concurrent-safe incremental decoder.
func NewDecoder(config DecoderConfig) (*Decoder, error) {
	if config.MaxPasteBytes < 0 || config.MaxBufferBytes < 0 {
		return nil, invalidBehaviorDefinition("define input decoder", "", ErrInvalidDefinition)
	}
	if config.MaxPasteBytes == 0 {
		config.MaxPasteBytes = defaultMaxPasteBytes
	}
	if config.MaxBufferBytes == 0 {
		config.MaxBufferBytes = defaultMaxInputBytes
	}
	if config.MaxPasteBytes > config.MaxBufferBytes {
		return nil, invalidBehaviorDefinition("define input decoder", "", ErrInvalidDefinition)
	}

	return &Decoder{
		maxPasteBytes: config.MaxPasteBytes,
		maxBuffer:     config.MaxBufferBytes,
		byteInput:     config.ByteInput,
	}, nil
}

// Feed copies and decodes one byte chunk. Incomplete UTF-8 and escape
// sequences remain buffered until a later Feed or Flush call.
func (decoder *Decoder) Feed(chunk []byte) ([]InputEvent, error) {
	decoder.mutex.Lock()
	defer decoder.mutex.Unlock()
	if len(chunk) == 0 {
		return []InputEvent{}, nil
	}
	if len(decoder.buffer)+len(chunk) > decoder.maxBuffer {
		return nil, decoder.fail()
	}
	decoder.buffer = append(decoder.buffer, chunk...)

	return decoder.decode()
}

// Flush resolves a lone Escape key and rejects every other incomplete
// sequence. It does not synthesize an EOF event.
func (decoder *Decoder) Flush() ([]InputEvent, error) {
	decoder.mutex.Lock()
	defer decoder.mutex.Unlock()
	if !decoder.inPaste && len(decoder.buffer) == 0 {
		return []InputEvent{}, nil
	}
	if !decoder.inPaste && bytes.Equal(decoder.buffer, []byte{0x1b}) {
		decoder.consume(1)
		return []InputEvent{KeyEvent(KeyEscape)}, nil
	}

	return nil, decoder.fail()
}

// Reset discards incomplete non-secret decoder state.
func (decoder *Decoder) Reset() {
	decoder.mutex.Lock()
	defer decoder.mutex.Unlock()
	decoder.reset()
}

func (decoder *Decoder) decode() ([]InputEvent, error) {
	events := make([]InputEvent, 0, len(decoder.buffer))
	for len(decoder.buffer) > 0 {
		if decoder.inPaste {
			complete, event, err := decoder.decodePaste()
			if err != nil {
				return nil, err
			}
			if !complete {
				return events, nil
			}
			events = append(events, event)
			continue
		}

		first := decoder.buffer[0]
		switch first {
		case 0x1b:
			event, consumed, incomplete, err := decodeEscape(decoder.buffer)
			if err != nil {
				return nil, decoder.fail()
			}
			if incomplete {
				return events, nil
			}
			decoder.consume(consumed)
			if consumed == len(bracketedPasteStart) && event == (InputEvent{}) {
				decoder.inPaste = true
				continue
			}
			events = append(events, event)
		case '\r':
			events = append(events, KeyEvent(KeyEnter))
			decoder.consume(1)
		case '\n':
			events = append(events, KeyEvent(KeyNewline))
			decoder.consume(1)
		case '\t':
			events = append(events, KeyEvent(KeyTab))
			decoder.consume(1)
		case 0x7f:
			events = append(events, KeyEvent(KeyBackspace))
			decoder.consume(1)
		case 0x03:
			events = append(events, KeyEvent(KeyCtrlC))
			decoder.consume(1)
		case 0x04:
			events = append(events, KeyEvent(KeyCtrlD))
			decoder.consume(1)
		default:
			if !utf8.FullRune(decoder.buffer) {
				return events, nil
			}
			value, size := utf8.DecodeRune(decoder.buffer)
			if value == utf8.RuneError && size == 1 || unicode.IsControl(value) || isBidiControl(value) {
				return nil, decoder.fail()
			}
			events = append(events, RuneEvent(value))
			decoder.consume(size)
		}
	}

	decoder.buffer = nil
	return events, nil
}

func (decoder *Decoder) decodePaste() (bool, InputEvent, error) {
	if end := bytes.Index(decoder.buffer, []byte(bracketedPasteEnd)); end >= 0 {
		if len(decoder.paste)+end > decoder.maxPasteBytes {
			return false, InputEvent{}, decoder.fail()
		}
		decoder.paste = append(decoder.paste, decoder.buffer[:end]...)
		if !utf8.Valid(decoder.paste) {
			return false, InputEvent{}, decoder.fail()
		}
		var event InputEvent
		if decoder.byteInput {
			event = PasteBytesEvent(decoder.paste)
		} else {
			event = PasteEvent(string(decoder.paste))
		}
		decoder.consume(end + len(bracketedPasteEnd))
		clear(decoder.paste)
		decoder.paste = decoder.paste[:0]
		decoder.inPaste = false
		return true, event, nil
	}

	keep := terminalPrefixSuffix(decoder.buffer, []byte(bracketedPasteEnd))
	content := len(decoder.buffer) - keep
	if len(decoder.paste)+content > decoder.maxPasteBytes {
		return false, InputEvent{}, decoder.fail()
	}
	decoder.paste = append(decoder.paste, decoder.buffer[:content]...)
	remaining := copy(decoder.buffer, decoder.buffer[content:])
	clear(decoder.buffer[remaining:])
	decoder.buffer = decoder.buffer[:remaining]

	return false, InputEvent{}, nil
}

func decodeEscape(input []byte) (InputEvent, int, bool, error) {
	sequences := []struct {
		value string
		key   Key
	}{
		{"\x1b[A", KeyUp}, {"\x1b[B", KeyDown}, {"\x1b[C", KeyRight},
		{"\x1b[D", KeyLeft}, {"\x1b[H", KeyHome}, {"\x1b[F", KeyEnd},
		{"\x1b[1~", KeyHome}, {"\x1b[4~", KeyEnd}, {"\x1b[3~", KeyDelete},
		{"\x1b[5~", KeyPageUp}, {"\x1b[6~", KeyPageDown}, {"\x1b[Z", KeyShiftTab},
		{"\x1b[1;5D", KeyWordLeft}, {"\x1b[1;5C", KeyWordRight},
	}
	if bytes.HasPrefix(input, []byte(bracketedPasteStart)) {
		return InputEvent{}, len(bracketedPasteStart), false, nil
	}
	for _, sequence := range sequences {
		if bytes.HasPrefix(input, []byte(sequence.value)) {
			return KeyEvent(sequence.key), len(sequence.value), false, nil
		}
	}
	if bytes.HasPrefix([]byte(bracketedPasteStart), input) {
		return InputEvent{}, 0, true, nil
	}
	for _, sequence := range sequences {
		if bytes.HasPrefix([]byte(sequence.value), input) {
			return InputEvent{}, 0, true, nil
		}
	}

	return InputEvent{}, 0, false, ErrReader
}

func terminalPrefixSuffix(content, marker []byte) int {
	maximum := min(len(content), len(marker)-1)
	for size := maximum; size > 0; size-- {
		if bytes.Equal(content[len(content)-size:], marker[:size]) {
			return size
		}
	}

	return 0
}

func (decoder *Decoder) fail() error {
	decoder.reset()
	return &Error{Kind: ErrorReader, Operation: "decode terminal input", Cause: ErrReader}
}

func (decoder *Decoder) reset() {
	clear(decoder.buffer)
	decoder.buffer = nil
	for index := range decoder.paste {
		decoder.paste[index] = 0
	}
	decoder.paste = nil
	decoder.inPaste = false
}

func (decoder *Decoder) consume(count int) {
	clear(decoder.buffer[:count])
	decoder.buffer = decoder.buffer[count:]
}
