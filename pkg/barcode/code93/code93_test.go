package code93_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/code93"
)

func TestEncodeAddsMandatoryChecksumsAndSupportsFullASCII(t *testing.T) {
	short, err := code93.Encode([]byte("A"), code93.Options{})
	if err != nil {
		t.Fatalf("Encode(A) error = %v", err)
	}
	long, err := code93.Encode([]byte("lowercase"), code93.Options{})
	if err != nil {
		t.Fatalf("Encode(full ASCII) error = %v", err)
	}
	if short.Format() != barcode.Code93 || long.Format() != barcode.Code93 {
		t.Fatal("wrong symbol format")
	}
	shortBars, _ := short.Bars()
	// Start, data, C, K, stop are nine modules each, followed by one bar.
	if got := shortBars.Width(); got != 10+(5*9+1)+10 {
		t.Fatalf("logical width = %d, want 66", got)
	}
	if first, last := shortBars.Runs()[0], shortBars.Runs()[len(shortBars.Runs())-1]; first.Dark || last.Dark || first.Width != 10 || last.Width != 10 {
		t.Fatalf("quiet zones = (%v, %v)", first, last)
	}
}

func TestEncodeRejectsInvalidInput(t *testing.T) {
	for _, payload := range [][]byte{nil, {0xff}, bytes.Repeat([]byte("A"), 81)} {
		if _, err := code93.Encode(payload, code93.Options{}); !errors.Is(err, code93.ErrInvalidInput) {
			t.Fatalf("Encode(%v) error = %v", payload, err)
		}
	}
	if _, err := code93.Encode([]byte("A"), code93.Options{QuietZone: 9}); !errors.Is(err, code93.ErrInvalidInput) {
		t.Fatalf("Encode(short quiet zone) error = %v", err)
	}
	for _, options := range []code93.Options{
		{QuietZone: -1},
		{Height: -1},
		{Height: 4097},
	} {
		if _, err := code93.Encode([]byte("A"), options); !errors.Is(err, code93.ErrInvalidInput) {
			t.Fatalf("Encode(%+v) error = %v", options, err)
		}
	}
}

func TestEncodePreservesExplicitDimensions(t *testing.T) {
	symbol, err := code93.Encode([]byte("A"), code93.Options{
		QuietZone: 14,
		Height:    64,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}
	if bars.Height() != 64 {
		t.Fatalf("Height() = %d, want 64", bars.Height())
	}
	runs := bars.Runs()
	if runs[0].Width != 14 || runs[len(runs)-1].Width != 14 {
		t.Fatalf("quiet zones = (%d, %d), want (14, 14)",
			runs[0].Width, runs[len(runs)-1].Width)
	}
}

func TestEncodeAcceptsPayloadAndOptionBoundaries(t *testing.T) {
	for _, payload := range [][]byte{
		bytes.Repeat([]byte("A"), 80),
		append(bytes.Repeat([]byte("A"), 78), byte(127)),
	} {
		symbol, err := code93.Encode(payload, code93.Options{
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
}
