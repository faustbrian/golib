package integer_test

import (
	"context"
	"math/big"
	"math/rand/v2"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func TestIntegerOperationsMatchBigInt(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	source := rand.New(rand.NewPCG(0x6d617468, 0x696e7465676572))
	for range 2_000 {
		leftBig := randomBigInt(source)
		rightBig := randomBigInt(source)
		left, err := integer.FromBig(leftBig, limits)
		if err != nil {
			t.Fatal(err)
		}
		right, err := integer.FromBig(rightBig, limits)
		if err != nil {
			t.Fatal(err)
		}

		got, operationErr := left.Add(context.Background(), right, limits)
		checkInteger(t, got, operationErr, new(big.Int).Add(leftBig, rightBig))
		got, operationErr = left.Sub(context.Background(), right, limits)
		checkInteger(t, got, operationErr, new(big.Int).Sub(leftBig, rightBig))
		got, operationErr = left.Mul(context.Background(), right, limits)
		checkInteger(t, got, operationErr, new(big.Int).Mul(leftBig, rightBig))
		if rightBig.Sign() != 0 {
			quotient, remainder, err := left.QuoRem(context.Background(), right, limits)
			if err != nil {
				t.Fatal(err)
			}
			wantQuotient, wantRemainder := new(big.Int), new(big.Int)
			wantQuotient.QuoRem(leftBig, rightBig, wantRemainder)
			if quotient.Big().Cmp(wantQuotient) != 0 || remainder.Big().Cmp(wantRemainder) != 0 {
				t.Fatalf("QuoRem(%s, %s) = %s, %s; want %s, %s", left, right, quotient, remainder, wantQuotient, wantRemainder)
			}
		}
		positiveModulus := new(big.Int).Abs(rightBig)
		if positiveModulus.Sign() != 0 {
			modulus, err := integer.FromBig(positiveModulus, limits)
			if err != nil {
				t.Fatal(err)
			}
			got, operationErr = left.Mod(context.Background(), modulus, limits)
			checkInteger(t, got, operationErr, new(big.Int).Mod(leftBig, positiveModulus))
		}
		got, operationErr = integer.GCD(context.Background(), left, right, limits)
		checkInteger(t, got, operationErr, new(big.Int).GCD(nil, nil, leftBig, rightBig))
		got, operationErr = integer.LCM(context.Background(), left, right, limits)
		wantLCM := new(big.Int)
		if leftBig.Sign() != 0 && rightBig.Sign() != 0 {
			gcd := new(big.Int).GCD(nil, nil, leftBig, rightBig)
			wantLCM.Quo(new(big.Int).Abs(leftBig), gcd).Mul(wantLCM, new(big.Int).Abs(rightBig))
		}
		checkInteger(t, got, operationErr, wantLCM)
		exponent := source.Uint64() % 5
		got, operationErr = left.Pow(context.Background(), exponent, limits)
		checkInteger(t, got, operationErr, new(big.Int).Exp(leftBig, new(big.Int).SetUint64(exponent), nil))
		absolute := new(big.Int).Abs(leftBig)
		absoluteValue, err := integer.FromBig(absolute, limits)
		if err != nil {
			t.Fatal(err)
		}
		got, operationErr = absoluteValue.Root(context.Background(), 2, limits)
		checkInteger(t, got, operationErr, new(big.Int).Sqrt(absolute))
		degree := uint32(source.Uint64()%7 + 2)
		got, operationErr = absoluteValue.Root(context.Background(), degree, limits)
		if operationErr != nil {
			t.Fatalf("Root(%s, %d) error = %v", absoluteValue, degree, operationErr)
		}
		root := got.Big()
		rootPower := new(big.Int).Exp(root, new(big.Int).SetUint64(uint64(degree)), nil)
		nextPower := new(big.Int).Exp(new(big.Int).Add(root, big.NewInt(1)), new(big.Int).SetUint64(uint64(degree)), nil)
		if rootPower.Cmp(absolute) > 0 || nextPower.Cmp(absolute) <= 0 {
			t.Fatalf("Root(%s, %d) = %s does not bound the radicand", absoluteValue, degree, got)
		}
		if got := left.Cmp(right); sign(got) != sign(leftBig.Cmp(rightBig)) {
			t.Fatalf("Cmp(%s, %s) = %d", left, right, got)
		}
	}
}

func randomBigInt(source *rand.Rand) *big.Int {
	words := []big.Word{big.Word(source.Uint64()), big.Word(source.Uint64())}
	value := new(big.Int).SetBits(words)
	if source.Uint64()&1 != 0 {
		value.Neg(value)
	}
	return value
}

func checkInteger(t *testing.T, got integer.Integer, err error, want *big.Int) {
	t.Helper()
	if err != nil || got.Big().Cmp(want) != 0 {
		t.Fatalf("operation = %s, %v; want %s", got, err, want)
	}
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
