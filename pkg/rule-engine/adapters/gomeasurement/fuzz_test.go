package ruleenginemeasurement_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomeasurement"
)

func FuzzQuantityTaggedValues(f *testing.F) {
	f.Add("quantity:v1|1|kg", uint8(0))
	f.Add("quantity:v1|-0.25|m", uint8(0))
	f.Add("quantity:v2|1|kg", uint8(0))
	f.Add("quantity:v1|1|unknown", uint8(0))
	f.Add("quantity:v1|1e100|kg", uint8(0))
	f.Add("quantity:v1|1|kg|extra", uint8(0))
	f.Add("quantity:v1|1|㎏", uint8(0))
	f.Add("not-a-quantity", uint8(0))
	f.Add("quantity:v1|1|kg", uint8(1))
	f.Add("quantity:v1|1|kg", uint8(2))
	f.Add("quantity:v1|-0|kg", uint8(0))
	f.Add("quantity:v1|"+strings.Repeat("9", 100_000)+"|kg", uint8(0))
	f.Add("quantity:v1|"+strings.Repeat("9", ruleenginemeasurement.MaxTaggedValueBytes)+"|kg", uint8(0))

	equal := ruleenginemeasurement.Operators()[0]
	f.Fuzz(func(t *testing.T, text string, kind uint8) {
		if len(text) > ruleenginemeasurement.MaxTaggedValueBytes+1 {
			t.Skip()
		}

		value := ruleengine.String(text)
		switch kind % 3 {
		case 1:
			value = ruleengine.Int(int64(len(text)))
		case 2:
			value = ruleengine.Null()
		}
		matched, err := equal.Evaluate(context.Background(), value, value)
		if err == nil && !matched {
			t.Fatalf("Evaluate(%q, %q) = false", text, text)
		}
		if err != nil && !errors.Is(err, ruleenginemeasurement.ErrInvalidQuantity) {
			t.Fatalf("Evaluate(%q) error = %v", text, err)
		}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := equal.Evaluate(canceled, value, value); !errors.Is(err, context.Canceled) {
			t.Fatalf("Evaluate(canceled, %q) error = %v", text, err)
		}
	})
}

func FuzzQuantityOperatorPairs(f *testing.F) {
	f.Add("quantity:v1|1|kg", "quantity:v1|1000|g", uint8(0))
	f.Add("quantity:v1|-1|m", "quantity:v1|0|cm", uint8(0))
	f.Add("quantity:v1|1|kg", "quantity:v1|1|m", uint8(0))
	f.Add("quantity:v1|1|degC", "quantity:v1|1|degF", uint8(0))
	f.Add("quantity:v1|1e100|kg", "quantity:v1|1|kg", uint8(0))
	f.Add("quantity:v1|1|㎏", "quantity:v1|1|kg", uint8(1))

	f.Fuzz(func(t *testing.T, leftText, rightText string, kindMask uint8) {
		if len(leftText) > ruleenginemeasurement.MaxTaggedValueBytes+1 || len(rightText) > ruleenginemeasurement.MaxTaggedValueBytes+1 {
			t.Skip()
		}
		values := []ruleengine.Value{ruleengine.String(leftText), ruleengine.String(rightText)}
		if kindMask&1 != 0 {
			values[0] = ruleengine.Int(int64(len(leftText)))
		}
		if kindMask&2 != 0 {
			values[1] = ruleengine.Null()
		}
		results := make([]bool, 5)
		var resultErr error
		for index, operator := range ruleenginemeasurement.Operators() {
			matched, err := operator.Evaluate(context.Background(), values[0], values[1])
			if index == 0 {
				resultErr = err
			} else if !sameErrorClass(resultErr, err) {
				t.Fatalf("operator errors differ: %v, %v", resultErr, err)
			}
			results[index] = matched
		}
		if resultErr == nil {
			if countTrue(results[0], results[1], results[3]) != 1 || results[2] != (results[0] || results[1]) || results[4] != (results[0] || results[3]) {
				t.Fatalf("inconsistent relations: %#v", results)
			}
		} else if !errors.Is(resultErr, ruleenginemeasurement.ErrInvalidQuantity) && !errors.Is(resultErr, ruleenginemeasurement.ErrIncompatibleQuantity) {
			t.Fatalf("unexpected error: %v", resultErr)
		}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := ruleenginemeasurement.Operators()[kindMask%5].Evaluate(canceled, values[0], values[1]); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled error = %v", err)
		}
	})
}

func sameErrorClass(left, right error) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftInvalid := errors.Is(left, ruleenginemeasurement.ErrInvalidQuantity)
	rightInvalid := errors.Is(right, ruleenginemeasurement.ErrInvalidQuantity)
	leftIncompatible := errors.Is(left, ruleenginemeasurement.ErrIncompatibleQuantity)
	rightIncompatible := errors.Is(right, ruleenginemeasurement.ErrIncompatibleQuantity)

	if leftInvalid == leftIncompatible || rightInvalid == rightIncompatible {
		return false
	}

	return leftInvalid == rightInvalid && leftIncompatible == rightIncompatible
}

func TestSameErrorClassRequiresOneExactRecognizedClass(t *testing.T) {
	t.Parallel()

	invalid := ruleenginemeasurement.ErrInvalidQuantity
	incompatible := ruleenginemeasurement.ErrIncompatibleQuantity
	unknown := errors.New("unknown")
	dual := errors.Join(invalid, incompatible)
	tests := []struct {
		name        string
		left, right error
		want        bool
	}{
		{"both nil", nil, nil, true},
		{"one nil", nil, invalid, false},
		{"invalid", invalid, errors.Join(errors.New("detail"), invalid), true},
		{"incompatible", incompatible, errors.Join(errors.New("detail"), incompatible), true},
		{"mismatched", invalid, incompatible, false},
		{"unknown", unknown, unknown, false},
		{"dual", dual, dual, false},
		{"dual mismatch", dual, invalid, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sameErrorClass(test.left, test.right); got != test.want {
				t.Fatalf("sameErrorClass(%v, %v) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
	}
}

func countTrue(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}
