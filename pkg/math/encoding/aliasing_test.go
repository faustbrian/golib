package encoding_test

import (
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	mathencoding "github.com/faustbrian/golib/pkg/math/encoding"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestBinaryDecodersDoNotRetainInputBuffers(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	rationalValue, err := rational.New(-22, 7)
	if err != nil {
		t.Fatal(err)
	}
	floatResult, err := bigfloat.Parse("-0", 10, bigfloat.Context{
		Precision: 64, Rounding: gomath.RoundHalfEven, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}

	integerData, _ := mathencoding.MarshalInteger(integer.New(-123))
	decodedInteger, err := mathencoding.UnmarshalInteger(integerData, limits)
	if err != nil {
		t.Fatal(err)
	}
	rationalData, _ := mathencoding.MarshalRational(rationalValue)
	decodedRational, err := mathencoding.UnmarshalRational(rationalData, limits)
	if err != nil {
		t.Fatal(err)
	}
	decimalData, _ := mathencoding.MarshalDecimal(decimal.MustParse("1.2300"))
	decodedDecimal, err := mathencoding.UnmarshalDecimal(decimalData, limits)
	if err != nil {
		t.Fatal(err)
	}
	floatData, _ := mathencoding.MarshalFloat(floatResult.Value)
	decodedFloat, err := mathencoding.UnmarshalFloat(floatData, limits)
	if err != nil {
		t.Fatal(err)
	}

	for _, data := range [][]byte{integerData, rationalData, decimalData, floatData} {
		clear(data)
	}
	if decodedInteger.String() != "-123" || decodedRational.String() != "-22/7" ||
		!decodedDecimal.SameRepresentation(decimal.MustParse("1.2300")) ||
		decodedFloat.String() != "-0" || !decodedFloat.Signbit() {
		t.Fatal("decoded value changed after its input buffer was overwritten")
	}
}

func TestEncodedBuffersCannotMutateSourceValues(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	integerValue := integer.New(-123)
	rationalValue, err := rational.New(-22, 7)
	if err != nil {
		t.Fatal(err)
	}
	decimalValue := decimal.MustParse("1.2300")
	floatResult, err := bigfloat.Parse("-0", 10, bigfloat.Context{
		Precision: 64, Rounding: gomath.RoundHalfEven, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}

	buffers := make([][]byte, 0, 12)
	appendBuffer := func(data []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		buffers = append(buffers, data)
	}
	appendBuffer(integerValue.MarshalText())
	appendBuffer(integerValue.MarshalJSON())
	appendBuffer(mathencoding.MarshalInteger(integerValue))
	appendBuffer(rationalValue.MarshalText())
	appendBuffer(rationalValue.MarshalJSON())
	appendBuffer(mathencoding.MarshalRational(rationalValue))
	appendBuffer(decimalValue.MarshalText())
	appendBuffer(decimalValue.MarshalJSON())
	appendBuffer(mathencoding.MarshalDecimal(decimalValue))
	appendBuffer(floatResult.Value.MarshalText())
	appendBuffer(floatResult.Value.MarshalJSON())
	appendBuffer(mathencoding.MarshalFloat(floatResult.Value))
	for _, buffer := range buffers {
		clear(buffer)
	}

	if integerValue.String() != "-123" || rationalValue.String() != "-22/7" ||
		!decimalValue.SameRepresentation(decimal.MustParse("1.2300")) ||
		floatResult.Value.String() != "-0" || !floatResult.Value.Signbit() {
		t.Fatal("mutating an encoded buffer changed its source value")
	}
}
