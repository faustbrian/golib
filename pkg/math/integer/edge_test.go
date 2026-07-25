package integer_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func TestIntegerConstructionAndParsingOptions(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	if integer.NewUnsigned(^uint64(0)).Sign() != 1 {
		t.Fatal("NewUnsigned() lost the unsigned range")
	}
	if _, err := integer.FromBig(nil, limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("FromBig(nil) error = %v", err)
	}
	invalidLimits := limits
	invalidLimits.MaxInputDigits = 0
	if _, err := integer.FromBig(big.NewInt(1), invalidLimits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("FromBig(invalid limits) error = %v", err)
	}
	tiny := limits
	tiny.MaxIntermediateBits = 1
	if _, err := integer.FromBig(big.NewInt(4), tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("FromBig(limit) error = %v", err)
	}

	parsed, err := integer.Parse("  +A_B  ", integer.ParseOptions{
		Base: 16, AllowWhitespace: true, AllowUnderscores: true,
		Limits: limits,
	})
	if err != nil || parsed.String() != "171" {
		t.Fatalf("Parse(options) = %s, %v", parsed, err)
	}
	for _, options := range []integer.ParseOptions{
		{Base: 1, Limits: limits},
		{Base: 10, RejectSign: true, Limits: limits},
	} {
		if _, err := integer.Parse("+1", options); err == nil {
			t.Fatalf("Parse(%+v) accepted invalid input", options)
		}
	}
	for _, input := range []string{"", "  ", "+", "01", "_1", "1_", "1__2", "z"} {
		if _, err := integer.Parse(input, integer.ParseOptions{
			Base: 10, AllowUnderscores: true, AllowWhitespace: true, Limits: limits,
		}); err == nil {
			t.Fatalf("Parse(%q) succeeded", input)
		}
	}
}

func TestIntegerOperationBoundsAndDomains(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	tiny := limits
	tiny.MaxIntermediateBits = 2
	broadValue, err := integer.FromBig(new(big.Int).Lsh(big.NewInt(1), 64), limits)
	if err != nil {
		t.Fatal(err)
	}
	for name, operation := range map[string]func() error{
		"addition": func() error {
			_, err := broadValue.Add(context.Background(), integer.Zero(), tiny)
			return err
		},
		"multiplication": func() error {
			_, err := broadValue.Mul(context.Background(), integer.Zero(), tiny)
			return err
		},
		"quotient dividend": func() error {
			_, err := broadValue.Quo(context.Background(), integer.New(1), tiny)
			return err
		},
		"quotient divisor": func() error {
			_, err := integer.New(1).Quo(context.Background(), broadValue, tiny)
			return err
		},
		"quotient remainder": func() error {
			_, _, err := broadValue.QuoRem(context.Background(), integer.New(1), tiny)
			return err
		},
		"modulus": func() error {
			_, err := broadValue.Mod(context.Background(), integer.New(3), tiny)
			return err
		},
		"power": func() error {
			_, err := broadValue.Pow(context.Background(), 0, tiny)
			return err
		},
		"root": func() error {
			_, err := broadValue.Root(context.Background(), 1, tiny)
			return err
		},
		"greatest common divisor": func() error {
			_, err := integer.GCD(context.Background(), broadValue, integer.Zero(), tiny)
			return err
		},
		"least common multiple": func() error {
			_, err := integer.LCM(context.Background(), broadValue, integer.Zero(), tiny)
			return err
		},
		"random range": func() error {
			_, err := integer.Random(context.Background(), bytes.NewReader(nil), integer.Zero(), broadValue, tiny)
			return err
		},
	} {
		if err := operation(); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("%s error = %v, want ErrLimitExceeded", name, err)
		}
	}
	if _, err := integer.New(3).Mul(context.Background(), integer.New(3), tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Mul(limit) error = %v", err)
	}
	if _, err := integer.New(3).Add(context.Background(), integer.New(3), tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Add(limit) error = %v", err)
	}
	if _, err := integer.New(2).Pow(context.Background(), 4, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Pow(result limit) error = %v", err)
	}
	if _, err := integer.New(1).Mod(context.Background(), integer.New(-2), limits); !errors.Is(err, gomath.ErrDomain) {
		t.Fatalf("Mod(negative) error = %v", err)
	}
	if _, err := integer.Clamp(integer.New(1), integer.New(2), integer.New(0)); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Clamp(inverted) error = %v", err)
	}
	if _, err := integer.New(4).Root(context.Background(), 0, limits); !errors.Is(err, gomath.ErrDomain) {
		t.Fatalf("Root(0) error = %v", err)
	}
	if _, err := integer.New(4).Root(context.Background(), limits.MaxRootDegree+1, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Root(limit) error = %v", err)
	}
	root, err := integer.New(-7).Root(context.Background(), 1, limits)
	if err != nil || root.String() != "-7" {
		t.Fatalf("Root(1) = %s, %v", root, err)
	}
	zeroRoot, err := integer.Zero().Root(context.Background(), 3, limits)
	if err != nil || zeroRoot.Sign() != 0 {
		t.Fatalf("Root(zero) = %s, %v", zeroRoot, err)
	}
	if result, err := integer.LCM(context.Background(), integer.Zero(), integer.New(5), limits); err != nil || result.Sign() != 0 {
		t.Fatalf("LCM(zero) = %s, %v", result, err)
	}
}

func TestIntegerRandomAndConversionsRejectInvalidInputs(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	if _, err := integer.Random(context.Background(), nil, integer.Zero(), integer.New(1), limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Random(nil) error = %v", err)
	}
	if _, err := integer.Random(context.Background(), strings.NewReader(""), integer.New(1), integer.New(1), limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Random(empty range) error = %v", err)
	}
	tiny := limits
	tiny.MaxRandomBits = 1
	if _, err := integer.Random(context.Background(), strings.NewReader("x"), integer.Zero(), integer.New(8), tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Random(limit) error = %v", err)
	}
	if _, err := integer.Random(context.Background(), errorReader{}, integer.Zero(), integer.New(2), limits); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Random(source) error = %v", err)
	}
	attemptLimited := limits
	attemptLimited.MaxRandomAttempts = 2
	if _, err := integer.Random(
		context.Background(), bytes.NewReader([]byte{0xff, 0xff}),
		integer.Zero(), integer.New(10), attemptLimited,
	); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Random(attempt limit) error = %v, want ErrLimitExceeded", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := integer.Random(canceled, strings.NewReader("x"), integer.Zero(), integer.New(2), limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("Random(canceled) error = %v", err)
	}
	if value, err := integer.NewUnsigned(^uint64(0)).Uint64(); err != nil || value != ^uint64(0) {
		t.Fatalf("Uint64() = %d, %v", value, err)
	}
	if _, err := integer.New(-1).Uint64(); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("Uint64(negative) error = %v", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
