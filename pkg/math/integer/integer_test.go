package integer_test

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func TestIntegerDefensivelyCopiesBigIntegers(t *testing.T) {
	t.Parallel()

	source := big.NewInt(41)
	value, err := integer.FromBig(source, gomath.DefaultLimits())
	if err != nil {
		t.Fatalf("FromBig() error = %v", err)
	}
	source.SetInt64(99)

	derived, err := value.Add(context.Background(), integer.New(1), gomath.DefaultLimits())
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	exposed := value.Big()
	exposed.SetInt64(100)

	if got := value.String(); got != "41" {
		t.Fatalf("value changed through an alias: got %s", got)
	}

	if got := derived.String(); got != "42" {
		t.Fatalf("Add() = %s, want 42", got)
	}
}

func TestIntegerExactArithmetic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := gomath.DefaultLimits()
	a := integer.New(-42)
	b := integer.New(5)

	assertOperation(t, "-37", func() (integer.Integer, error) { return a.Add(ctx, b, limits) })
	assertOperation(t, "-47", func() (integer.Integer, error) { return a.Sub(ctx, b, limits) })
	assertOperation(t, "-210", func() (integer.Integer, error) { return a.Mul(ctx, b, limits) })
	assertValue(t, a.Neg(), "42")
	assertValue(t, a.Abs(), "42")
	if a.Sign() != -1 || a.Cmp(b) >= 0 || a.Equal(b) {
		t.Fatal("sign or comparison contract failed")
	}
	assertValue(t, integer.Min(a, b), "-42")
	assertValue(t, integer.Max(a, b), "5")
	assertOperation(t, "5", func() (integer.Integer, error) {
		return integer.Clamp(integer.New(9), a, b)
	})

	q, r, err := a.QuoRem(ctx, b, limits)
	if err != nil {
		t.Fatalf("QuoRem() error = %v", err)
	}
	assertValue(t, q, "-8")
	assertValue(t, r, "-2")
	assertOperation(t, "3", func() (integer.Integer, error) { return a.Mod(ctx, b, limits) })
	assertOperation(t, "6", func() (integer.Integer, error) {
		return integer.GCD(ctx, integer.New(84), integer.New(30), limits)
	})
	assertOperation(t, "42", func() (integer.Integer, error) {
		return integer.LCM(ctx, integer.New(21), integer.New(6), limits)
	})
	assertOperation(t, "-243", func() (integer.Integer, error) {
		return integer.New(-3).Pow(ctx, 5, limits)
	})
	assertOperation(t, "8", func() (integer.Integer, error) {
		return integer.New(80).Root(ctx, 2, limits)
	})
	assertOperation(t, "-3", func() (integer.Integer, error) {
		return integer.New(-28).Root(ctx, 3, limits)
	})
}

func TestIntegerRejectsInvalidAndOversizedOperations(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxInputDigits = 3
	if _, err := integer.Parse("1234", integer.ParseOptions{Base: 10, Limits: limits}); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Parse() error = %v, want ErrLimitExceeded", err)
	}
	if _, err := integer.Parse(" 12", integer.ParseOptions{Base: 10, Limits: limits}); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("Parse() whitespace error = %v", err)
	}
	if _, err := integer.Parse("1_2", integer.ParseOptions{Base: 10, Limits: limits}); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("Parse() underscore error = %v", err)
	}
	if _, err := integer.New(1).Quo(context.Background(), integer.Zero(), limits); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("Quo() error = %v", err)
	}
	if _, err := integer.New(-1).Root(context.Background(), 2, limits); !errors.Is(err, gomath.ErrDomain) {
		t.Fatalf("Root() error = %v", err)
	}
	if _, err := integer.New(2).Pow(context.Background(), uint64(limits.MaxPowerExponent)+1, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Pow() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := integer.New(2).Pow(canceled, 2, limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("Pow() cancellation error = %v", err)
	}
}

func TestIntegerParsingAndSerializationAreCanonical(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	value, err := integer.Parse("-ff", integer.ParseOptions{Base: 16, Limits: limits})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertValue(t, value, "-255")

	text, err := value.MarshalText()
	if err != nil || string(text) != "-255" {
		t.Fatalf("MarshalText() = %q, %v", text, err)
	}
	jsonValue, err := value.MarshalJSON()
	if err != nil || string(jsonValue) != `"-255"` {
		t.Fatalf("MarshalJSON() = %s, %v", jsonValue, err)
	}
	if _, err := value.Int64(); err != nil {
		t.Fatalf("Int64() error = %v", err)
	}
	tooLarge, err := integer.Parse(strings.Repeat("9", 30), integer.ParseOptions{Base: 10, Limits: limits})
	if err != nil {
		t.Fatalf("Parse(large) error = %v", err)
	}
	if _, err := tooLarge.Int64(); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("Int64() error = %v, want ErrConversion", err)
	}
}

func TestIntegerRandomUsesUnbiasedRejectionSampling(t *testing.T) {
	t.Parallel()

	// The first byte is rejected because 0xff lies outside the largest multiple
	// of a range of ten. The second byte maps deterministically to seven.
	reader := bytes.NewReader([]byte{0xff, 0x07})
	value, err := integer.Random(context.Background(), reader, integer.Zero(), integer.New(10), gomath.DefaultLimits())
	if err != nil {
		t.Fatalf("Random() error = %v", err)
	}
	assertValue(t, value, "7")
}

func TestIntegerRandomMappingIsUniformAcrossByteDomain(t *testing.T) {
	t.Parallel()

	counts := make(map[string]int)
	rejected := 0
	for candidate := 0; candidate < 256; candidate++ {
		value, err := integer.Random(
			context.Background(), bytes.NewReader([]byte{byte(candidate)}),
			integer.New(-3), integer.New(7), gomath.DefaultLimits(),
		)
		if err != nil {
			rejected++
			continue
		}
		counts[value.String()]++
	}
	if rejected != 6 {
		t.Fatalf("rejected %d byte values, want 6", rejected)
	}
	for value := -3; value < 7; value++ {
		if count := counts[integer.New(int64(value)).String()]; count != 25 {
			t.Fatalf("value %d has %d mappings, want 25", value, count)
		}
	}
}

func assertValue(t *testing.T, value integer.Integer, want string) {
	t.Helper()
	if got := value.String(); got != want {
		t.Fatalf("value = %s, want %s", got, want)
	}
}

func assertOperation(t *testing.T, want string, operation func() (integer.Integer, error)) {
	t.Helper()
	value, err := operation()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertValue(t, value, want)
}
