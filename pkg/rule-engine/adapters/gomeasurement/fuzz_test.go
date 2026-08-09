package ruleenginemeasurement_test

import (
	"context"
	"errors"
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

	equal := ruleenginemeasurement.Operators()[0]
	f.Fuzz(func(t *testing.T, text string, kind uint8) {
		if len(text) > 4_096 {
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
