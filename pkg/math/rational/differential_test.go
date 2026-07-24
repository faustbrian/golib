package rational_test

import (
	"context"
	"math/big"
	"math/rand/v2"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestRationalOperationsMatchBigRat(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	source := rand.New(rand.NewPCG(0x726174696f6e616c, 0x646966666572656e))
	for range 2_000 {
		leftBig := randomBigRat(source)
		rightBig := randomBigRat(source)
		left, err := rational.FromBig(leftBig, limits)
		if err != nil {
			t.Fatal(err)
		}
		right, err := rational.FromBig(rightBig, limits)
		if err != nil {
			t.Fatal(err)
		}

		got, operationErr := left.Add(context.Background(), right, limits)
		checkRational(t, got, operationErr, new(big.Rat).Add(leftBig, rightBig))
		got, operationErr = left.Sub(context.Background(), right, limits)
		checkRational(t, got, operationErr, new(big.Rat).Sub(leftBig, rightBig))
		got, operationErr = left.Mul(context.Background(), right, limits)
		checkRational(t, got, operationErr, new(big.Rat).Mul(leftBig, rightBig))
		if right.Sign() != 0 {
			got, operationErr = left.Quo(context.Background(), right, limits)
			checkRational(t, got, operationErr, new(big.Rat).Quo(leftBig, rightBig))
		}
		exponent := int64(source.Uint64()%7) - 3
		if left.Sign() != 0 || exponent >= 0 {
			got, operationErr = left.Pow(context.Background(), exponent, limits)
			checkRational(t, got, operationErr, powBigRat(leftBig, exponent))
		}
		if got := left.Cmp(right); sign(got) != sign(leftBig.Cmp(rightBig)) {
			t.Fatalf("Cmp(%s, %s) = %d", left, right, got)
		}
	}
}

func powBigRat(value *big.Rat, exponent int64) *big.Rat {
	magnitude := exponent
	if magnitude < 0 {
		magnitude = -magnitude
	}
	numerator := new(big.Int).Exp(value.Num(), big.NewInt(magnitude), nil)
	denominator := new(big.Int).Exp(value.Denom(), big.NewInt(magnitude), nil)
	if exponent < 0 {
		numerator, denominator = denominator, numerator
	}
	return new(big.Rat).SetFrac(numerator, denominator)
}

func sign(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

func randomBigRat(source *rand.Rand) *big.Rat {
	numerator := int64(source.Uint64())
	denominator := int64(source.Uint64()>>1) + 1
	return new(big.Rat).SetFrac(big.NewInt(numerator), big.NewInt(denominator))
}

func checkRational(t *testing.T, got rational.Rational, err error, want *big.Rat) {
	t.Helper()
	if err != nil || got.Big().Cmp(want) != 0 {
		t.Fatalf("operation = %s, %v; want %s", got, err, want)
	}
}
