package ruleenginemath_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func FuzzDecimalTaggedValues(f *testing.F) {
	f.Add("0")
	f.Add("-123.4500")
	f.Add("not-a-decimal")

	equal := ruleenginemath.Operators()[0]
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4_096 {
			t.Skip()
		}

		value := ruleengine.String("decimal:" + text)
		matched, err := equal.Evaluate(context.Background(), value, value)
		_, parseErr := decimal.Parse(text)
		if parseErr != nil {
			if err == nil {
				t.Fatalf("Evaluate(%q) accepted an invalid decimal", text)
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
