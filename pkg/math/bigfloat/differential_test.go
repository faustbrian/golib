package bigfloat_test

import (
	"context"
	"math/big"
	"math/rand/v2"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
)

func TestFloatOperationsMatchBigFloat(t *testing.T) {
	t.Parallel()

	operation := bigfloat.Context{
		Precision: 113,
		Rounding:  gomath.RoundHalfEven,
		Limits:    gomath.DefaultLimits(),
	}
	source := rand.New(rand.NewPCG(0x626967666c6f6174, 0x646966666572656e))
	for range 2_000 {
		leftRat := big.NewRat(int64(source.Uint64()), int64(source.Uint64()>>1)+1)
		rightRat := big.NewRat(int64(source.Uint64()), int64(source.Uint64()>>1)+1)
		leftResult, err := bigfloat.FromRat(leftRat, operation)
		if err != nil {
			t.Fatal(err)
		}
		rightResult, err := bigfloat.FromRat(rightRat, operation)
		if err != nil {
			t.Fatal(err)
		}
		left, right := leftResult.Value, rightResult.Value
		leftBig, rightBig := left.Big(), right.Big()

		got, operationErr := operation.Add(context.Background(), left, right)
		checkFloat(t, got, operationErr, newBigFloat(operation).Add(leftBig, rightBig))
		got, operationErr = operation.Sub(context.Background(), left, right)
		checkFloat(t, got, operationErr, newBigFloat(operation).Sub(leftBig, rightBig))
		got, operationErr = operation.Mul(context.Background(), left, right)
		checkFloat(t, got, operationErr, newBigFloat(operation).Mul(leftBig, rightBig))
		if right.Sign() != 0 {
			got, operationErr = operation.Quo(context.Background(), left, right)
			checkFloat(t, got, operationErr, newBigFloat(operation).Quo(leftBig, rightBig))
		}
		absolute := left.Abs()
		got, operationErr = operation.Sqrt(context.Background(), absolute)
		checkFloat(t, got, operationErr, newBigFloat(operation).Sqrt(absolute.Big()))
	}
}

func newBigFloat(operation bigfloat.Context) *big.Float {
	return new(big.Float).SetPrec(operation.Precision).SetMode(big.ToNearestEven)
}

func checkFloat(t *testing.T, got bigfloat.Result, err error, want *big.Float) {
	t.Helper()
	if err != nil || got.Value.Big().Cmp(want) != 0 || got.Accuracy != want.Acc() {
		t.Fatalf("operation = %s (%s), %v; want %s (%s)", got.Value, got.Accuracy, err, want, want.Acc())
	}
}
