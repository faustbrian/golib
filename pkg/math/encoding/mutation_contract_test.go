package encoding

import (
	"encoding/binary"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestMutationReaderHeaderAndSizeBoundaries(t *testing.T) {
	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 8
	maximumBytes := limits.MaxIntermediateBits/8 + 64

	minimum := header(kindInteger)
	reader, err := newReader(minimum, kindInteger, limits)
	if err != nil || reader.offset != 4 || !reader.done() {
		t.Fatalf("newReader(minimum) = %+v, %v", reader, err)
	}
	exactMaximum := make([]byte, maximumBytes)
	copy(exactMaximum, minimum)
	if _, err := newReader(exactMaximum, kindInteger, limits); err != nil {
		t.Fatalf("newReader(exact maximum) error = %v", err)
	}
	for _, size := range []int{3, maximumBytes + 1} {
		if _, err := newReader(make([]byte, size), kindInteger, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("newReader(size %d) error = %v", size, err)
		}
	}

	for index := range minimum {
		malformed := append([]byte(nil), minimum...)
		malformed[index]++
		if _, err := newReader(malformed, kindInteger, limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("newReader(header byte %d) error = %v", index, err)
		}
	}
}

func TestMutationSignedMagnitudeContracts(t *testing.T) {
	for _, test := range []struct {
		data []byte
		want string
	}{
		{[]byte{0, 0}, "0"},
		{[]byte{1, 1, 1}, "1"},
		{[]byte{2, 1, 1}, "-1"},
	} {
		reader := &reader{data: test.data}
		value, err := reader.signed()
		if err != nil || value.String() != test.want || !reader.done() {
			t.Fatalf("signed(%v) = %v, %v", test.data, value, err)
		}
	}
	for _, data := range [][]byte{
		{},
		{3, 0},
		{0, 1, 1},
		{1, 0},
		{2, 0},
		{1, 1, 0},
	} {
		reader := &reader{data: data}
		if _, err := reader.signed(); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("signed(%v) error = %v", data, err)
		}
	}

	for _, test := range []struct {
		value int64
		want  []byte
	}{
		{0, []byte{0, 0}},
		{1, []byte{1, 1, 1}},
		{-1, []byte{2, 1, 1}},
	} {
		got := appendSigned(nil, big.NewInt(test.value))
		if string(got) != string(test.want) {
			t.Fatalf("appendSigned(%d) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestMutationReaderPrimitiveBoundaries(t *testing.T) {
	for _, test := range []struct {
		data []byte
		want []byte
	}{
		{[]byte{0}, []byte{}},
		{[]byte{1, 7}, []byte{7}},
		{[]byte{2, 7, 8}, []byte{7, 8}},
	} {
		reader := &reader{data: test.data}
		got, err := reader.bytes()
		if err != nil || string(got) != string(test.want) || !reader.done() {
			t.Fatalf("bytes(%v) = %v, %v", test.data, got, err)
		}
	}
	for _, data := range [][]byte{
		{},
		{0x80},
		{2, 1},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02},
	} {
		reader := &reader{data: data}
		if _, err := reader.bytes(); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("bytes(%v) error = %v", data, err)
		}
	}

	for _, value := range []int64{-1, 0, 1} {
		data := binary.AppendVarint(nil, value)
		reader := &reader{data: data}
		got, err := reader.varint()
		if err != nil || got != value || !reader.done() {
			t.Fatalf("varint(%d) = %d, %v", value, got, err)
		}
	}
	for _, data := range [][]byte{{}, {0x80}} {
		reader := &reader{data: data}
		if _, err := reader.varint(); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("varint(%v) error = %v", data, err)
		}
	}

	reader := &reader{data: []byte{7}}
	if value, err := reader.byte(); err != nil || value != 7 || !reader.done() {
		t.Fatalf("byte() = %d, %v", value, err)
	}
	if _, err := reader.byte(); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("exhausted byte() error = %v", err)
	}
}

func TestMutationDecoderCompositionBoundaries(t *testing.T) {
	limits := gomath.DefaultLimits()

	integerData, _ := MarshalInteger(integer.New(1))
	if _, err := UnmarshalInteger(append(integerData, 0), limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("integer trailing data error = %v", err)
	}

	rationalValue, err := rational.New(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	rationalData, _ := MarshalRational(rationalValue)
	if _, err := UnmarshalRational(append(rationalData, 0), limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("rational trailing data error = %v", err)
	}
	zeroDenominator := appendSigned(header(kindRational), big.NewInt(1))
	zeroDenominator = appendMagnitude(zeroDenominator, new(big.Int))
	if _, err := UnmarshalRational(zeroDenominator, limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("zero denominator error = %v", err)
	}

	decimalData, _ := MarshalDecimal(decimal.New(1))
	if _, err := UnmarshalDecimal(append(decimalData, 0), limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("decimal trailing data error = %v", err)
	}
	broad := limits
	broad.MaxExponentMagnitude = 1<<31 - 1
	for _, exponent := range []int64{-1 << 31, 1<<31 - 1} {
		data := appendSigned(header(kindDecimal), big.NewInt(1))
		data = binary.AppendVarint(data, exponent)
		_, err := UnmarshalDecimal(data, broad)
		if errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("decimal exponent boundary %d reported invalid syntax: %v", exponent, err)
		}
	}
	for _, exponent := range []int64{-1<<31 - 1, 1 << 31} {
		data := appendSigned(header(kindDecimal), big.NewInt(1))
		data = binary.AppendVarint(data, exponent)
		if _, err := UnmarshalDecimal(data, broad); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("decimal exponent %d error = %v", exponent, err)
		}
	}

	floatResult, err := bigfloat.NewInt64(1, bigfloat.Context{
		Precision: 8,
		Rounding:  gomath.RoundHalfEven,
		Limits:    limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	floatData, _ := MarshalFloat(floatResult.Value)
	if _, err := UnmarshalFloat(append(floatData, 0), limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("float trailing data error = %v", err)
	}
}
