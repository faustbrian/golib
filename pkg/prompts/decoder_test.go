package prompts_test

import (
	"errors"
	"reflect"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestDecoderIncrementallyDecodesTextAndKeys(t *testing.T) {
	t.Parallel()

	decoder, err := prompts.NewDecoder(prompts.DecoderConfig{})
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	first, err := decoder.Feed([]byte("a\xf0\x9f"))
	if err != nil || !reflect.DeepEqual(first, []prompts.InputEvent{prompts.RuneEvent('a')}) {
		t.Fatalf("first Feed() = %#v, %v", first, err)
	}
	second, err := decoder.Feed([]byte("\x91\xa9\n\r\t\x7f\x03\x04"))
	want := []prompts.InputEvent{
		prompts.RuneEvent('👩'), prompts.KeyEvent(prompts.KeyNewline),
		prompts.KeyEvent(prompts.KeyEnter),
		prompts.KeyEvent(prompts.KeyTab), prompts.KeyEvent(prompts.KeyBackspace),
		prompts.KeyEvent(prompts.KeyCtrlC), prompts.KeyEvent(prompts.KeyCtrlD),
	}
	if err != nil || !reflect.DeepEqual(second, want) {
		t.Fatalf("second Feed() = %#v, %v", second, err)
	}
	flushed, err := decoder.Flush()
	if err != nil || len(flushed) != 0 {
		t.Fatalf("Flush() = %#v, %v", flushed, err)
	}
}

func TestDecoderDecodesNavigationSequences(t *testing.T) {
	t.Parallel()

	decoder, err := prompts.NewDecoder(prompts.DecoderConfig{})
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	input := "\x1b[A\x1b[B\x1b[C\x1b[D\x1b[H\x1b[F\x1b[1~\x1b[4~" +
		"\x1b[3~\x1b[5~\x1b[6~\x1b[Z\x1b[1;5D\x1b[1;5C"
	events, err := decoder.Feed([]byte(input))
	wantKeys := []prompts.Key{
		prompts.KeyUp, prompts.KeyDown, prompts.KeyRight, prompts.KeyLeft,
		prompts.KeyHome, prompts.KeyEnd, prompts.KeyHome, prompts.KeyEnd,
		prompts.KeyDelete, prompts.KeyPageUp, prompts.KeyPageDown,
		prompts.KeyShiftTab, prompts.KeyWordLeft, prompts.KeyWordRight,
	}
	want := make([]prompts.InputEvent, len(wantKeys))
	for index, key := range wantKeys {
		want[index] = prompts.KeyEvent(key)
	}
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatalf("Feed() = %#v, %v", events, err)
	}
}

func TestDecoderBoundsAndDecodesBracketedPaste(t *testing.T) {
	t.Parallel()

	decoder, err := prompts.NewDecoder(prompts.DecoderConfig{MaxPasteBytes: 16, MaxBufferBytes: 32})
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	events, err := decoder.Feed([]byte("\x1b[200~line\n\x1b[20"))
	if err != nil || len(events) != 0 {
		t.Fatalf("partial Feed() = %#v, %v", events, err)
	}
	events, err = decoder.Feed([]byte("1~x"))
	want := []prompts.InputEvent{prompts.PasteEvent("line\n"), prompts.RuneEvent('x')}
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatalf("complete Feed() = %#v, %v", events, err)
	}

	limited, err := prompts.NewDecoder(prompts.DecoderConfig{MaxPasteBytes: 3, MaxBufferBytes: 32})
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	if _, err := limited.Feed([]byte("\x1b[200~four")); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("oversized paste error = %v", err)
	}
	if _, err := limited.Feed([]byte("\x1b[200~four\x1b[201~")); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("completed oversized paste error = %v", err)
	}
	if _, err := limited.Feed([]byte("\x1b[200~\xff\x1b[201~")); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("invalid paste error = %v", err)
	}
	events, err = limited.Feed([]byte("ok"))
	if err != nil || !reflect.DeepEqual(events, []prompts.InputEvent{prompts.RuneEvent('o'), prompts.RuneEvent('k')}) {
		t.Fatalf("Feed() after reset = %#v, %v", events, err)
	}
}

func TestDecoderByteModeAvoidsStringPastePayload(t *testing.T) {
	t.Parallel()

	decoder, err := prompts.NewDecoder(prompts.DecoderConfig{
		MaxPasteBytes: 32, MaxBufferBytes: 64, ByteInput: true,
	})
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	events, err := decoder.Feed([]byte("\x1b[200~secret-👩‍💻\x1b[201~"))
	if err != nil || len(events) != 1 {
		t.Fatalf("Feed() = %#v, %v", events, err)
	}
	event := &events[0]
	if event.Kind != prompts.EventPaste || event.Text != "" ||
		string(event.Bytes.Reveal()) != "secret-👩‍💻" {
		t.Fatalf("byte event = %#v", event)
	}
	event.Destroy()
	if !event.Bytes.Destroyed() || event.Bytes.Len() != 0 {
		t.Fatal("Destroy() retained byte event payload")
	}
}

func TestDecoderRejectsUnsafeAndIncompleteInput(t *testing.T) {
	t.Parallel()

	tests := map[string][]byte{
		"invalid UTF-8":      {0xff},
		"unsupported escape": []byte("\x1b[9~"),
		"control":            {0x01},
		"bidi control":       []byte("\u202e"),
		"stray paste end":    []byte("\x1b[201~"),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			decoder, err := prompts.NewDecoder(prompts.DecoderConfig{})
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}
			if _, err := decoder.Feed(input); !errors.Is(err, prompts.ErrReader) {
				t.Fatalf("Feed() error = %v", err)
			}
		})
	}

	decoder, _ := prompts.NewDecoder(prompts.DecoderConfig{})
	if events, err := decoder.Feed([]byte{0x1b}); err != nil || len(events) != 0 {
		t.Fatalf("escape Feed() = %#v, %v", events, err)
	}
	events, err := decoder.Flush()
	if err != nil || !reflect.DeepEqual(events, []prompts.InputEvent{prompts.KeyEvent(prompts.KeyEscape)}) {
		t.Fatalf("escape Flush() = %#v, %v", events, err)
	}
	_, _ = decoder.Feed([]byte("\x1b["))
	if _, err := decoder.Flush(); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("partial Flush() error = %v", err)
	}
	_, _ = decoder.Feed([]byte("\x1b[1"))
	if _, err := decoder.Flush(); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("partial navigation Flush() error = %v", err)
	}
	_, _ = decoder.Feed([]byte("\x1b[200~partial"))
	if _, err := decoder.Flush(); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("paste Flush() error = %v", err)
	}
}

func TestDecoderValidatesLimitsAndBufferBound(t *testing.T) {
	t.Parallel()

	for _, config := range []prompts.DecoderConfig{
		{MaxPasteBytes: -1},
		{MaxBufferBytes: -1},
		{MaxPasteBytes: 2, MaxBufferBytes: 1},
	} {
		if _, err := prompts.NewDecoder(config); !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("NewDecoder(%#v) error = %v", config, err)
		}
	}
	decoder, err := prompts.NewDecoder(prompts.DecoderConfig{MaxPasteBytes: 4, MaxBufferBytes: 4})
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	if _, err := decoder.Feed([]byte("12345")); !errors.Is(err, prompts.ErrReader) {
		t.Fatalf("buffer error = %v", err)
	}
	decoder.Reset()
	if events, err := decoder.Feed(nil); err != nil || len(events) != 0 {
		t.Fatalf("empty Feed() = %#v, %v", events, err)
	}
}
