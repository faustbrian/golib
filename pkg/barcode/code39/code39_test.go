package code39_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/code39"
)

func TestChecksumMatchesModulo43Vector(t *testing.T) {
	got, err := code39.Checksum([]byte("CODE39"))
	if err != nil {
		t.Fatalf("Checksum() error = %v", err)
	}
	if got != 'W' {
		t.Fatalf("Checksum() = %q, want W", got)
	}
}

func TestChecksumAcceptsAlphabetBoundary(t *testing.T) {
	got, err := code39.Checksum([]byte("0"))
	if err != nil {
		t.Fatalf("Checksum() error = %v", err)
	}
	if got != '0' {
		t.Fatalf("Checksum() = %q, want 0", got)
	}
}

func TestEncodeSupportsChecksumsAndFullASCII(t *testing.T) {
	plain, err := code39.Encode([]byte("CODE39"), code39.Options{})
	if err != nil {
		t.Fatalf("Encode(plain) error = %v", err)
	}
	checked, err := code39.Encode([]byte("CODE39"), code39.Options{Checksum: true})
	if err != nil {
		t.Fatalf("Encode(checksum) error = %v", err)
	}
	fullASCII, err := code39.Encode([]byte("lowercase"), code39.Options{})
	if err != nil {
		t.Fatalf("Encode(full ASCII) error = %v", err)
	}
	fullASCIIWithChecksum, err := code39.Encode([]byte("lowercase"), code39.Options{Checksum: true})
	if err != nil {
		t.Fatalf("Encode(full ASCII checksum) error = %v", err)
	}
	if checked.Format() != barcode.Code39 || fullASCII.Format() != barcode.Code39 {
		t.Fatal("wrong symbol format")
	}
	if string(fullASCIIWithChecksum.Payload()) != "lowercase" {
		t.Fatalf("Payload() = %q, want lowercase", fullASCIIWithChecksum.Payload())
	}
	plainBars, _ := plain.Bars()
	checkedBars, _ := checked.Bars()
	if checkedBars.Width() <= plainBars.Width() {
		t.Fatal("checksum character did not extend the logical symbol")
	}
	if first, last := checkedBars.Runs()[0], checkedBars.Runs()[len(checkedBars.Runs())-1]; first.Dark || last.Dark || first.Width < 10 || last.Width < 10 {
		t.Fatalf("unsafe quiet zones: first=%v last=%v", first, last)
	}
}

func TestEncodeRejectsInvalidInput(t *testing.T) {
	for _, payload := range [][]byte{nil, {0xff}, bytes.Repeat([]byte("A"), 81), bytes.Repeat([]byte("a"), 41)} {
		if _, err := code39.Encode(payload, code39.Options{}); !errors.Is(err, code39.ErrInvalidInput) {
			t.Fatalf("Encode(%v) error = %v", payload, err)
		}
	}
	if _, err := code39.Encode([]byte("A"), code39.Options{QuietZone: 9}); !errors.Is(err, code39.ErrInvalidInput) {
		t.Fatalf("Encode(short quiet zone) error = %v", err)
	}
	if _, err := code39.Encode(bytes.Repeat([]byte("A"), 80), code39.Options{Checksum: true}); !errors.Is(err, code39.ErrInvalidInput) {
		t.Fatalf("Encode(checksum capacity) error = %v", err)
	}
	for _, options := range []code39.Options{
		{QuietZone: -1},
		{Height: -1},
		{Height: 4097},
	} {
		if _, err := code39.Encode([]byte("A"), options); !errors.Is(err, code39.ErrInvalidInput) {
			t.Fatalf("Encode(%+v) error = %v", options, err)
		}
	}
}

func TestEncodeAcceptsCapacityCharacterAndOptionBoundaries(t *testing.T) {
	for _, test := range []struct {
		payload  []byte
		checksum bool
	}{
		{payload: bytes.Repeat([]byte("A"), 80)},
		{payload: bytes.Repeat([]byte{127}, 40)},
		{payload: bytes.Repeat([]byte("A"), 79), checksum: true},
		{payload: bytes.Repeat([]byte("a"), 39), checksum: true},
	} {
		symbol, err := code39.Encode(test.payload, code39.Options{
			Checksum:  test.checksum,
			QuietZone: 10,
			Height:    4096,
		})
		if err != nil {
			t.Fatalf("Encode(length %d, checksum %t) error = %v",
				len(test.payload), test.checksum, err)
		}
		bars, ok := symbol.Bars()
		if !ok {
			t.Fatal("Bars() not present")
		}
		if bars.Height() != 4096 || !bytes.Equal(symbol.Payload(), test.payload) {
			t.Fatalf("symbol = (height %d, payload length %d)",
				bars.Height(), len(symbol.Payload()))
		}
	}
}
