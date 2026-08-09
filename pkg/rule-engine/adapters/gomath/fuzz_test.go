package ruleenginemath_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func FuzzDecimalTaggedValues(f *testing.F) {
	for _, seed := range []string{
		"0", "-123.4500", "-0", "not-a-decimal", "١.٢", "１.２",
		strings.Repeat("9", 256), strings.Repeat("9", 257),
		"0." + strings.Repeat("0", 254) + "1",
	} {
		f.Add(seed)
	}

	limits := decimalFuzzLimits()
	operators, err := ruleenginemath.OperatorsWithLimits(limits)
	if err != nil {
		f.Fatal(err)
	}
	equal := operators[0]
	f.Fuzz(func(t *testing.T, text string) {
		value := ruleengine.String(ruleenginemath.EncodingV1Prefix + text)
		matched, evaluateErr := equal.Evaluate(context.Background(), value, value)
		parsed, parseErr := decimal.ParseWithOptions(text, decimal.ParseOptions{Limits: limits})
		if parseErr != nil {
			if evaluateErr == nil {
				t.Fatal("Evaluate accepted an invalid decimal")
			}

			return
		}
		canonical, _ := parsed.MarshalText()
		if string(canonical) != text {
			if evaluateErr == nil {
				t.Fatal("Evaluate accepted a noncanonical decimal")
			}

			return
		}
		if evaluateErr != nil {
			t.Fatalf("Evaluate() error = %v", evaluateErr)
		}
		if !matched {
			t.Fatal("Evaluate(value, value) = false")
		}
	})
}

func FuzzDecimalOperatorBoundaries(f *testing.F) {
	prefix := ruleenginemath.EncodingV1Prefix
	f.Add(uint8(0), uint8(0), prefix+"-1.00", prefix+"0", false, uint8(1), uint8(0))
	f.Add(uint8(1), uint8(1), "1.00", "1.0", false, uint8(0), uint8(4))
	f.Add(uint8(2), uint8(0), "ignored", prefix+"1", false, uint8(4), uint8(2))
	f.Add(uint8(0), uint8(0), strings.TrimSuffix(prefix, ":"), prefix+"1", false, uint8(3), uint8(1))
	f.Add(uint8(0), uint8(0), prefix+"١", prefix+"1", false, uint8(2), uint8(3))
	f.Add(uint8(0), uint8(0), prefix+strings.Repeat("9", 257), prefix+"1", false, uint8(4), uint8(4))
	f.Add(uint8(0), uint8(0), prefix+"1", prefix+"1", true, uint8(0), uint8(0))

	limits := decimalFuzzLimits()
	f.Fuzz(func(
		t *testing.T,
		leftKind, rightKind uint8,
		leftInput, rightInput string,
		canceled bool,
		operatorIndex, mutationIndex uint8,
	) {
		first, err := ruleenginemath.OperatorsWithLimits(limits)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ruleenginemath.OperatorsWithLimits(limits)
		if err != nil {
			t.Fatal(err)
		}
		first[int(mutationIndex)%len(first)] = nil
		index := int(operatorIndex) % len(second)
		operator := second[index]
		second[index] = nil
		signatures := operator.Signatures()
		signatures[0] = ruleengine.Signature{}

		left := decimalFuzzValue(leftKind, leftInput)
		right := decimalFuzzValue(rightKind, rightInput)
		ctx := context.Background()
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		matched, evaluateErr := operator.Evaluate(ctx, left, right)
		if canceled {
			if !errors.Is(evaluateErr, context.Canceled) {
				t.Fatalf("canceled Evaluate() error = %v", evaluateErr)
			}

			return
		}

		leftDecimal, leftOK := decimalFuzzDecimal(leftKind, leftInput, limits)
		rightDecimal, rightOK := decimalFuzzDecimal(rightKind, rightInput, limits)
		if !leftOK || !rightOK {
			if evaluateErr == nil {
				t.Fatal("Evaluate accepted an invalid boundary value")
			}

			return
		}
		if evaluateErr != nil {
			t.Fatalf("Evaluate() error = %v", evaluateErr)
		}
		want := decimalRelation(operator.Name(), leftDecimal.Cmp(rightDecimal))
		if matched != want {
			t.Fatalf("%s comparison = %t, want %t", operator.Name(), matched, want)
		}
	})
}

func decimalFuzzLimits() gomath.Limits {
	limits := gomath.DefaultLimits()
	limits.MaxInputDigits = 256
	limits.MaxOutputDigits = 256
	limits.MaxExponentMagnitude = 256

	return limits
}

func decimalFuzzValue(kind uint8, input string) ruleengine.Value {
	switch kind % 7 {
	case 0:
		return ruleengine.String(input)
	case 1:
		return ruleengine.String(ruleenginemath.EncodingV1Prefix + input)
	case 2:
		return ruleengine.Int(int64(len(input)))
	case 3:
		return ruleengine.Null()
	case 4:
		return ruleengine.Bool(len(input)%2 == 0)
	case 5:
		return ruleengine.Float(1)
	default:
		return ruleengine.List(ruleengine.String(input))
	}
}

func decimalFuzzDecimal(kind uint8, input string, limits gomath.Limits) (decimal.Decimal, bool) {
	if kind%7 == 1 {
		input = ruleenginemath.EncodingV1Prefix + input
	} else if kind%7 != 0 {
		return decimal.Decimal{}, false
	}
	if !strings.HasPrefix(input, ruleenginemath.EncodingV1Prefix) {
		return decimal.Decimal{}, false
	}
	payload := strings.TrimPrefix(input, ruleenginemath.EncodingV1Prefix)
	parsed, err := decimal.ParseWithOptions(payload, decimal.ParseOptions{Limits: limits})
	if err != nil {
		return decimal.Decimal{}, false
	}
	canonical, _ := parsed.MarshalText()

	return parsed, string(canonical) == payload
}

func decimalRelation(name ruleengine.OperatorName, comparison int) bool {
	// This oracle receives only operators constructed by the gomath adapter.
	//nolint:exhaustive // Core operator names are intentionally outside its domain.
	switch name {
	case ruleenginemath.OpDecimalEqual:
		return comparison == 0
	case ruleenginemath.OpDecimalLessThan:
		return comparison < 0
	case ruleenginemath.OpDecimalLessOrEqual:
		return comparison <= 0
	case ruleenginemath.OpDecimalGreaterThan:
		return comparison > 0
	case ruleenginemath.OpDecimalGreaterOrEqual:
		return comparison >= 0
	default:
		panic("unknown decimal operator")
	}
}
