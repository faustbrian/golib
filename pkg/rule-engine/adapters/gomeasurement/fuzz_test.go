package ruleenginemeasurement_test

import (
	"context"
	"testing"

	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomeasurement"
)

func FuzzQuantityTaggedValues(f *testing.F) {
	f.Add("1 kg")
	f.Add("-0.25 m")
	f.Add("not-a-quantity")

	equal := ruleenginemeasurement.Operators()[0]
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4_096 {
			t.Skip()
		}

		value := ruleengine.String("quantity:" + text)
		matched, err := equal.Evaluate(context.Background(), value, value)
		_, parseErr := measurement.Parse(text, measurement.SymbolProfile())
		if parseErr != nil {
			if err == nil {
				t.Fatalf("Evaluate(%q) accepted an invalid quantity", text)
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
