package codabar_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/codabar"
)

func TestEncodeUsesExplicitStartAndStopCharacters(t *testing.T) {
	defaultSymbol, err := codabar.Encode([]byte("1234"), codabar.Options{})
	if err != nil {
		t.Fatalf("Encode(default) error = %v", err)
	}
	custom, err := codabar.Encode([]byte("1234"), codabar.Options{Start: 'B', Stop: 'D'})
	if err != nil {
		t.Fatalf("Encode(custom) error = %v", err)
	}
	if defaultSymbol.Format() != barcode.Codabar || custom.Format() != barcode.Codabar {
		t.Fatal("wrong symbol format")
	}
	defaultBars, _ := defaultSymbol.Bars()
	customBars, _ := custom.Bars()
	if defaultBars.Width() == 0 || customBars.Width() == 0 {
		t.Fatal("logical bars are empty")
	}
	if first, last := customBars.Runs()[0], customBars.Runs()[len(customBars.Runs())-1]; first.Dark || last.Dark || first.Width != 10 || last.Width != 10 {
		t.Fatalf("quiet zones = (%v, %v)", first, last)
	}
}

func TestEncodeRejectsInvalidPayloadGuardsAndDimensions(t *testing.T) {
	tests := []struct {
		payload []byte
		options codabar.Options
	}{
		{},
		{payload: []byte("12A3")},
		{payload: []byte("12x3")},
		{payload: []byte("123"), options: codabar.Options{Start: 'X'}},
		{payload: []byte("123"), options: codabar.Options{Stop: 'X'}},
		{payload: []byte("123"), options: codabar.Options{QuietZone: 9}},
		{payload: []byte("123"), options: codabar.Options{QuietZone: -1}},
		{payload: []byte("123"), options: codabar.Options{Height: -1}},
		{payload: []byte("123"), options: codabar.Options{Height: 4097}},
		{payload: bytes.Repeat([]byte("1"), 81)},
	}
	for _, tt := range tests {
		if _, err := codabar.Encode(tt.payload, tt.options); !errors.Is(err, codabar.ErrInvalidInput) {
			t.Fatalf("Encode(%q, %+v) error = %v", tt.payload, tt.options, err)
		}
	}
}

func TestEncodePreservesExplicitDimensions(t *testing.T) {
	symbol, err := codabar.Encode([]byte("123"), codabar.Options{
		QuietZone: 12,
		Height:    72,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}
	if bars.Height() != 72 {
		t.Fatalf("Height() = %d, want 72", bars.Height())
	}
	runs := bars.Runs()
	if runs[0].Width != 12 || runs[len(runs)-1].Width != 12 {
		t.Fatalf("quiet zones = (%d, %d), want (12, 12)",
			runs[0].Width, runs[len(runs)-1].Width)
	}
}

func TestEncodeAcceptsPayloadAndOptionBoundaries(t *testing.T) {
	payload := bytes.Repeat([]byte("1"), 80)
	symbol, err := codabar.Encode(payload, codabar.Options{
		Start:     'A',
		Stop:      'D',
		QuietZone: 10,
		Height:    4096,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}
	if bars.Height() != 4096 || !bytes.Equal(symbol.Payload(), payload) {
		t.Fatalf("symbol = (height %d, payload length %d)",
			bars.Height(), len(symbol.Payload()))
	}
}
