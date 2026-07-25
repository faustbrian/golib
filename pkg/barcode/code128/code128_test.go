package code128_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
)

func TestEncodeCodeSetBMatchesIndependentLogicalVector(t *testing.T) {
	symbol, err := code128.Encode([]byte("HI"), code128.Options{CodeSet: code128.CodeSetB})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.Format() != barcode.Code128 {
		t.Fatalf("Format() = %q, want %q", symbol.Format(), barcode.Code128)
	}
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}

	wantWidths := []int{
		10,
		2, 1, 1, 2, 1, 4,
		2, 3, 1, 1, 1, 3,
		2, 3, 1, 3, 1, 1,
		2, 2, 1, 2, 3, 1,
		2, 3, 3, 1, 1, 1, 2,
		10,
	}
	gotWidths := make([]int, 0, len(bars.Runs()))
	for _, run := range bars.Runs() {
		gotWidths = append(gotWidths, run.Width)
	}
	if !reflect.DeepEqual(gotWidths, wantWidths) {
		t.Fatalf("logical run widths = %v, want %v", gotWidths, wantWidths)
	}
	if bars.Runs()[0].Dark || bars.Runs()[1].Dark == false {
		t.Fatal("logical vector does not start with a light quiet zone then a bar")
	}
}

func TestEncodeDefaultsAreStandardsSafeAndAutoSwitchCodeSets(t *testing.T) {
	auto, err := code128.Encode([]byte("123456"), code128.Options{})
	if err != nil {
		t.Fatalf("Encode(auto) error = %v", err)
	}
	forced, err := code128.Encode([]byte("123456"), code128.Options{CodeSet: code128.CodeSetB})
	if err != nil {
		t.Fatalf("Encode(B) error = %v", err)
	}
	autoBars, _ := auto.Bars()
	forcedBars, _ := forced.Bars()
	if autoBars.Width() >= forcedBars.Width() {
		t.Fatalf("auto width = %d, forced B width = %d", autoBars.Width(), forcedBars.Width())
	}
	if first, last := autoBars.Runs()[0], autoBars.Runs()[len(autoBars.Runs())-1]; first.Dark || last.Dark || first.Width < 10 || last.Width < 10 {
		t.Fatalf("unsafe quiet zones: first=%v last=%v", first, last)
	}
}

func TestEncodeSupportsEachForcedCodeSet(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		codeSet code128.CodeSet
	}{
		{name: "A", payload: "ABC\x01", codeSet: code128.CodeSetA},
		{name: "B", payload: "abc", codeSet: code128.CodeSetB},
		{name: "C", payload: "123456", codeSet: code128.CodeSetC},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol, err := code128.Encode([]byte(tt.payload), code128.Options{CodeSet: tt.codeSet})
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if symbol.Format() != barcode.Code128 {
				t.Fatalf("Format() = %q, want %q", symbol.Format(), barcode.Code128)
			}
		})
	}
}

func TestEncodeGS1AddsFNC1AndReportsGS1Format(t *testing.T) {
	symbol, err := code128.Encode([]byte("0109501101530003"), code128.Options{GS1: true})
	if err != nil {
		t.Fatalf("Encode(GS1) error = %v", err)
	}
	if symbol.Format() != barcode.GS1128 {
		t.Fatalf("Format() = %q, want %q", symbol.Format(), barcode.GS1128)
	}
}

func TestEncodeGS1AcceptsValidatedStructuredElements(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(10)ABC", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("gs1.ParseBracketed() error = %v", err)
	}
	symbol, err := code128.EncodeGS1(elements, code128.Options{})
	if err != nil {
		t.Fatalf("EncodeGS1() error = %v", err)
	}
	if got := string(symbol.Payload()); got != elements.Raw() {
		t.Fatalf("Payload() = %q, want %q", got, elements.Raw())
	}
	if _, err := code128.Encode([]byte("019501101530004"), code128.Options{GS1: true}); !errors.Is(err, code128.ErrInvalidInput) {
		t.Fatalf("Encode(invalid GS1) error = %v", err)
	}
}

func TestEncodeRejectsUnsafeOptionsAndPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		options code128.Options
	}{
		{name: "empty"},
		{name: "non ASCII", payload: []byte{0xff}},
		{name: "short quiet zone", payload: []byte("A"), options: code128.Options{QuietZone: 9}},
		{name: "invalid height", payload: []byte("A"), options: code128.Options{Height: -1}},
		{name: "excessive height", payload: []byte("A"), options: code128.Options{Height: 4097}},
		{name: "invalid code set", payload: []byte("A"), options: code128.Options{CodeSet: 99}},
		{name: "payload too long", payload: []byte(strings.Repeat("A", 81))},
		{name: "odd forced code C", payload: []byte("123"), options: code128.Options{CodeSet: code128.CodeSetC}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := code128.Encode(tt.payload, tt.options); !errors.Is(err, code128.ErrInvalidInput) {
				t.Fatalf("Encode() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestEncodeAcceptsPayloadCharacterAndOptionBoundaries(t *testing.T) {
	for _, payload := range [][]byte{
		bytes.Repeat([]byte("A"), 80),
		{127},
	} {
		symbol, err := code128.Encode(payload, code128.Options{
			QuietZone: 10,
			Height:    4096,
		})
		if err != nil {
			t.Fatalf("Encode(%v) error = %v", payload, err)
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
