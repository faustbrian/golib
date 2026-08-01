package datamatrix

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/makiuchi-d/gozxing/datamatrix/encoder"
)

func TestECICodewordsMatchAssignmentWidthBoundaries(t *testing.T) {
	tests := []struct {
		assignment int
		want       []byte
	}{
		{assignment: 1, want: []byte{2}},
		{assignment: 126, want: []byte{127}},
		{assignment: 127, want: []byte{128, 1}},
		{assignment: 16_382, want: []byte{191, 254}},
		{assignment: 16_383, want: []byte{192, 1, 1}},
		{assignment: 16_384, want: []byte{192, 1, 2}},
		{assignment: 100_000, want: []byte{193, 76, 52}},
		{assignment: 999_999, want: []byte{207, 63, 129}},
	}
	for _, test := range tests {
		if got := eciCodewords(test.assignment); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("eciCodewords(%d) = %v, want %v",
				test.assignment, got, test.want)
		}
	}
}

func TestPaddingRandomizationMatchesECC200Vectors(t *testing.T) {
	if got := randomize253(2); got != 175 {
		t.Fatalf("randomize253(2) = %d, want 175", got)
	}
	if got := randomize253(3); got != 70 {
		t.Fatalf("randomize253(3) = %d, want 70", got)
	}
	if got := randomize253(28); got != 254 {
		t.Fatalf("randomize253(28) = %d, want 254", got)
	}
	codewords := []byte{232, 231}
	codewords = appendBase256(codewords, 1)
	codewords = appendBase256(codewords, 0xe9)
	if !reflect.DeepEqual(codewords, []byte{232, 231, 194, 64}) {
		t.Fatalf("Base 256 codewords = %v, want [232 231 194 64]", codewords)
	}
}

func TestControlledEncodingDistinguishesBase256LengthLimit(t *testing.T) {
	_, err := encodeControlled(make([]byte, 1555), Options{ECI: 1}, encoder.SymbolShapeHint_FORCE_NONE, nil, nil)
	if err == nil || errors.Is(err, ErrInvalidInput) {
		t.Fatalf("encodeControlled(1555 bytes) error = %v, want symbol-capacity error", err)
	}
	if _, err := encodeControlled(make([]byte, 1556), Options{ECI: 1}, encoder.SymbolShapeHint_FORCE_NONE, nil, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("encodeControlled(1556 bytes) error = %v, want ErrInvalidInput", err)
	}
}

func TestStructuredAppendSequenceCodeword(t *testing.T) {
	for _, test := range []struct {
		index int
		total int
		want  byte
	}{
		{index: 0, total: 2, want: 15},
		{index: 15, total: 16, want: 241},
	} {
		if got := structuredAppendSequence(test.index, test.total); got != test.want {
			t.Fatalf("structuredAppendSequence(%d, %d) = %d, want %d",
				test.index, test.total, got, test.want)
		}
	}
}

func TestControlledMatrixGoldenModules(t *testing.T) {
	symbol, err := Encode([]byte("0109501101530003"), Options{GS1: true})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	modules := symbol.Logical().Matrix().Modules()
	packed := make([]byte, (len(modules)+7)/8)
	for index, dark := range modules {
		if dark {
			packed[index/8] |= 1 << (7 - index%8)
		}
	}
	hash := sha256.Sum256(packed)
	if got := hex.EncodeToString(hash[:]); got != "c09befbe084666641d2f861e225f27a73da39c3f933bf8bddf17401cac1736c2" {
		t.Fatalf("module SHA-256 = %s", got)
	}
}
