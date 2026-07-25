package encoding

import (
	"encoding/binary"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
)

func TestReaderAndCodecMalformedEdges(t *testing.T) {
	limits := gomath.DefaultLimits()
	bad := limits
	bad.MaxInputDigits = 0
	if _, err := newReader(header(kindInteger), kindInteger, bad); err == nil {
		t.Fatal("expected invalid limits")
	}
	tooLarge := make([]byte, limits.MaxIntermediateBits/8+65)
	if _, err := newReader(tooLarge, kindInteger, limits); err == nil {
		t.Fatal("expected size limit")
	}

	integerCases := [][]byte{
		header(kindInteger),
		append(header(kindInteger), 3, 0),
		append(header(kindInteger), 0, 1, 1),
		append(header(kindInteger), 1, 0),
		append(header(kindInteger), 1, 1, 0),
		append(header(kindInteger), 1, 2, 1),
		append(header(kindInteger), 1, 0x80),
	}
	for _, data := range integerCases {
		if _, err := UnmarshalInteger(data, limits); err == nil {
			t.Fatalf("accepted malformed integer %v", data)
		}
	}

	rationalCases := [][]byte{
		append(header(kindRational), 3),
		appendSigned(header(kindRational), big.NewInt(1)),
		append(appendSigned(header(kindRational), big.NewInt(1)), 0),
		append(appendSigned(header(kindRational), big.NewInt(1)), 1, 0),
	}
	for _, data := range rationalCases {
		if _, err := UnmarshalRational(data, limits); err == nil {
			t.Fatalf("accepted malformed rational %v", data)
		}
	}

	decimalData := appendSigned(header(kindDecimal), big.NewInt(1))
	if _, err := UnmarshalDecimal(append(header(kindDecimal), 3), limits); err == nil {
		t.Fatal("accepted malformed decimal coefficient")
	}
	for _, suffix := range [][]byte{{}, {0x80}, binary.AppendVarint(nil, 1<<40)} {
		if _, err := UnmarshalDecimal(append(append([]byte(nil), decimalData...), suffix...), limits); err == nil {
			t.Fatalf("accepted malformed decimal suffix %v", suffix)
		}
	}

	floatCases := [][]byte{
		header(kindFloat),
		append(header(kindFloat), byte(gomath.RoundHalfEven)),
		append(append(header(kindFloat), byte(gomath.RoundHalfEven)), 1),
		append(append(header(kindFloat), byte(gomath.RoundHalfEven), 1), 0xff),
	}
	for _, data := range floatCases {
		if _, err := UnmarshalFloat(data, limits); err == nil {
			t.Fatalf("accepted malformed float %v", data)
		}
	}

	result, err := bigfloat.NewInt64(1, bigfloat.Context{Precision: 8, Rounding: gomath.RoundHalfEven, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalFloat(result.Value)
	if err != nil {
		t.Fatal(err)
	}
	data[4] = 255
	if _, err := UnmarshalFloat(data, limits); err == nil {
		t.Fatal("accepted invalid float rounding")
	}
	if invalidEncoding(nil) != gomath.ErrInvalidSyntax {
		t.Fatal("nil encoding error mismatch")
	}
	encodeError := errors.New("encode")
	if _, err := marshalFloat(result.Value, func(*big.Float) ([]byte, error) { return nil, encodeError }); !errors.Is(err, encodeError) {
		t.Fatal("expected float encoding error")
	}
	if _, err := marshalFloat(result.Value, func(*big.Float) ([]byte, error) { return nil, nil }); err == nil {
		t.Fatal("expected nil float payload error")
	}
}
