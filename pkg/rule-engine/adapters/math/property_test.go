package ruleenginemath_test

import (
	"context"
	"math/big"
	"math/rand"
	"testing"
	"testing/quick"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/math"
)

func TestDecimalOperatorRelationalProperties(t *testing.T) {
	t.Parallel()

	operators := ruleenginemath.Operators()
	property := func(
		leftCoefficient, rightCoefficient, thirdCoefficient int64,
		leftRawExponent, rightRawExponent, thirdRawExponent int8,
	) bool {
		left := propertyDecimal(leftCoefficient, leftRawExponent)
		right := propertyDecimal(rightCoefficient, rightRawExponent)
		third := propertyDecimal(thirdCoefficient, thirdRawExponent)
		comparison := left.Cmp(right)
		want := []bool{comparison == 0, comparison < 0, comparison <= 0, comparison > 0, comparison >= 0}
		for index, operator := range operators {
			matched, err := operator.Evaluate(
				context.Background(),
				ruleenginemath.Decimal(left),
				ruleenginemath.Decimal(right),
			)
			if err != nil || matched != want[index] {
				return false
			}
		}

		reverse := right.Cmp(left)
		if comparison != -reverse || left.Equal(right) != (comparison == 0) {
			return false
		}
		if comparison <= 0 && right.Cmp(third) <= 0 && left.Cmp(third) > 0 {
			return false
		}
		if comparison >= 0 && right.Cmp(third) >= 0 && left.Cmp(third) < 0 {
			return false
		}

		return true
	}
	configuration := &quick.Config{
		MaxCount: 2_000,
		Rand:     rand.New(rand.NewSource(1)), //nolint:gosec // Deterministic property corpus, not security randomness.
	}
	if err := quick.Check(property, configuration); err != nil {
		t.Fatal(err)
	}
}

func propertyDecimal(coefficient int64, rawExponent int8) decimal.Decimal {
	exponent := int32(uint8(rawExponent)%37) - 18
	value, err := decimal.FromBig(big.NewInt(coefficient), exponent, gomath.DefaultLimits())
	if err != nil {
		panic(err)
	}

	return value
}
