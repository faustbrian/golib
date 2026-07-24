package encoding_test

import (
	"bytes"
	"errors"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	mathencoding "github.com/faustbrian/golib/pkg/math/encoding"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestBinaryCodecsRoundTripDeterministically(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	integerValue := integer.New(-123456789)
	rationalValue, err := rational.New(-22, 7)
	if err != nil {
		t.Fatalf("rational.New() error = %v", err)
	}
	decimalValue := decimal.MustParse("123.4500")
	floatResult, err := bigfloat.NewInt64(42, bigfloat.Context{
		Precision: 96,
		Rounding:  gomath.RoundHalfEven,
		Limits:    limits,
	})
	if err != nil {
		t.Fatalf("bigfloat.NewInt64() error = %v", err)
	}

	integerData, err := mathencoding.MarshalInteger(integerValue)
	if err != nil {
		t.Fatalf("MarshalInteger() error = %v", err)
	}
	integerAgain, _ := mathencoding.MarshalInteger(integerValue)
	if !bytes.Equal(integerData, integerAgain) {
		t.Fatal("integer encoding is not deterministic")
	}
	decodedInteger, err := mathencoding.UnmarshalInteger(integerData, limits)
	if err != nil || !decodedInteger.Equal(integerValue) {
		t.Fatalf("UnmarshalInteger() = %s, %v", decodedInteger, err)
	}

	rationalData, _ := mathencoding.MarshalRational(rationalValue)
	decodedRational, err := mathencoding.UnmarshalRational(rationalData, limits)
	if err != nil || !decodedRational.Equal(rationalValue) {
		t.Fatalf("UnmarshalRational() = %s, %v", decodedRational, err)
	}

	decimalData, _ := mathencoding.MarshalDecimal(decimalValue)
	decodedDecimal, err := mathencoding.UnmarshalDecimal(decimalData, limits)
	if err != nil || !decodedDecimal.SameRepresentation(decimalValue) {
		t.Fatalf("UnmarshalDecimal() = %s, %v", decodedDecimal, err)
	}

	floatData, _ := mathencoding.MarshalFloat(floatResult.Value)
	decodedFloat, err := mathencoding.UnmarshalFloat(floatData, limits)
	if err != nil || !decodedFloat.Equal(floatResult.Value) || decodedFloat.Precision() != 96 {
		t.Fatalf("UnmarshalFloat() = %s at %d bits, %v", decodedFloat, decodedFloat.Precision(), err)
	}
}

func TestBinaryCodecsRejectWrongVersionsKindsAndTrailingData(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	data, err := mathencoding.MarshalInteger(integer.New(1))
	if err != nil {
		t.Fatalf("MarshalInteger() error = %v", err)
	}

	wrongVersion := append([]byte(nil), data...)
	wrongVersion[2]++
	if _, err := mathencoding.UnmarshalInteger(wrongVersion, limits); err == nil {
		t.Fatal("UnmarshalInteger() accepted an unknown version")
	}
	if _, err := mathencoding.UnmarshalDecimal(data, limits); err == nil {
		t.Fatal("UnmarshalDecimal() accepted an integer payload")
	}
	if _, err := mathencoding.UnmarshalInteger(append(data, 0), limits); err == nil {
		t.Fatal("UnmarshalInteger() accepted trailing data")
	}
	overlongLength := append(append([]byte(nil), data[:5]...), 0x81, 0x00, data[len(data)-1])
	if _, err := mathencoding.UnmarshalInteger(overlongLength, limits); err == nil {
		t.Fatal("UnmarshalInteger() accepted a non-canonical length")
	}
}

func TestBinaryCodecsRejectNonCanonicalAndOversizedValues(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	tiny := limits
	tiny.MaxIntermediateBits = 1
	integerData, err := mathencoding.MarshalInteger(integer.New(2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mathencoding.UnmarshalInteger(integerData, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("integer error = %v", err)
	}
	rationalValue, err := rational.New(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	rationalData, err := mathencoding.MarshalRational(rationalValue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mathencoding.UnmarshalRational(rationalData, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("rational error = %v", err)
	}
	decimalData, err := mathencoding.MarshalDecimal(decimal.New(2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mathencoding.UnmarshalDecimal(decimalData, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("decimal error = %v", err)
	}

	nonNormalizedRational := []byte{'G', 'M', 1, 2, 1, 1, 2, 1, 4}
	if _, err := mathencoding.UnmarshalRational(nonNormalizedRational, limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("non-normalized rational error = %v", err)
	}
	zeroDecimal, err := mathencoding.MarshalDecimal(decimal.New(0))
	if err != nil {
		t.Fatal(err)
	}
	overlongExponent := append(append([]byte(nil), zeroDecimal[:len(zeroDecimal)-1]...), 0x80, 0x00)
	if _, err := mathencoding.UnmarshalDecimal(overlongExponent, limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("overlong decimal exponent error = %v", err)
	}
	floatResult, err := bigfloat.NewInt64(1, bigfloat.Context{
		Precision: 64, Rounding: gomath.RoundHalfEven, Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	floatData, err := mathencoding.MarshalFloat(floatResult.Value)
	if err != nil {
		t.Fatal(err)
	}
	if floatData[5] >= 0x80 {
		t.Fatal("test requires a one-byte float payload length")
	}
	overlongFloatLength := append(append([]byte(nil), floatData[:5]...), floatData[5]|0x80, 0)
	overlongFloatLength = append(overlongFloatLength, floatData[6:]...)
	if _, err := mathencoding.UnmarshalFloat(overlongFloatLength, limits); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("overlong float length error = %v", err)
	}
}
