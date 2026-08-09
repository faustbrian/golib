package ruleenginemath_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func FuzzDecimalTaggedValues(f *testing.F) {
	f.Add("0")
	f.Add("-123.4500")
	f.Add("-0")
	f.Add("not-a-decimal")

	equal := ruleenginemath.Operators()[0]
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4_096 {
			t.Skip()
		}

		value := ruleengine.String(ruleenginemath.EncodingV1Prefix + text)
		matched, err := equal.Evaluate(context.Background(), value, value)
		parsed, parseErr := decimal.Parse(text)
		if parseErr != nil {
			if err == nil {
				t.Fatalf("Evaluate(%q) accepted an invalid decimal", text)
			}

			return
		}
		if parsed.String() != text {
			if err == nil {
				t.Fatalf("Evaluate(%q) accepted a noncanonical decimal", text)
			}

			return
		}
		if err != nil {
			t.Fatalf("Evaluate(%q) error = %v", text, err)
		}
		if !matched {
			t.Fatalf("Evaluate(%q, %q) = false", text, text)
		}
	})
}

func FuzzDecimalOperatorBoundaries(f *testing.F) {
	f.Add(uint8(0), "golib.rule-engine.decimal/v1:1", false, uint8(0))
	f.Add(uint8(1), "ignored", false, uint8(4))
	f.Add(uint8(2), "-0", false, uint8(2))
	f.Add(uint8(2), "1.00", true, uint8(3))

	f.Fuzz(func(t *testing.T, kind uint8, input string, canceled bool, operatorIndex uint8) {
		if len(input) > 4_096 {
			t.Skip()
		}

		operators := ruleenginemath.Operators()
		operator := operators[int(operatorIndex)%len(operators)]
		var value ruleengine.Value
		switch kind % 3 {
		case 0:
			value = ruleengine.String(input)
		case 1:
			value = ruleengine.Int(1)
		default:
			value = ruleengine.String(ruleenginemath.EncodingV1Prefix + input)
		}
		ctx := context.Background()
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		matched, err := operator.Evaluate(ctx, value, value)
		if canceled {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled Evaluate() error = %v", err)
			}

			return
		}
		if kind%3 == 1 || !isCanonicalTaggedDecimal(input, kind%3 == 2) {
			if err == nil {
				t.Fatalf("Evaluate(%q) accepted invalid boundary input", input)
			}

			return
		}
		if err != nil {
			t.Fatalf("Evaluate(%q) error = %v", input, err)
		}
		want := operator.Name() == ruleenginemath.OpDecimalEqual ||
			operator.Name() == ruleenginemath.OpDecimalLessOrEqual ||
			operator.Name() == ruleenginemath.OpDecimalGreaterOrEqual
		if matched != want {
			t.Fatalf("%s equal operands = %t, want %t", operator.Name(), matched, want)
		}
	})
}

func isCanonicalTaggedDecimal(input string, addPrefix bool) bool {
	if addPrefix {
		input = ruleenginemath.EncodingV1Prefix + input
	}
	if !strings.HasPrefix(input, ruleenginemath.EncodingV1Prefix) {
		return false
	}
	payload := strings.TrimPrefix(input, ruleenginemath.EncodingV1Prefix)
	parsed, err := decimal.Parse(payload)
	return err == nil && parsed.String() == payload
}
