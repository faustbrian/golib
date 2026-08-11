package ruleenginemeasurement

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func TestQuantityOperatorTruthAndFailureTable(t *testing.T) {
	t.Parallel()

	left := Quantity(measurement.MustNew(decimal.MustParse("1.5"), measurement.Kilogram))
	equal := Quantity(measurement.MustNew(decimal.MustParse("1500"), measurement.Gram))
	lower := Quantity(measurement.MustNew(decimal.MustParse("1499"), measurement.Gram))
	higher := Quantity(measurement.MustNew(decimal.MustParse("1501"), measurement.Gram))
	tests := []struct {
		index int
		right ruleengine.Value
	}{
		{0, equal}, {1, higher}, {2, equal}, {3, lower}, {4, equal},
	}
	operators := Operators()
	for _, test := range tests {
		operator := operators[test.index]
		if operator.Name() == "" || len(operator.Signatures()) != 1 {
			t.Fatalf("operator metadata = %q, %#v", operator.Name(), operator.Signatures())
		}
		got, err := operator.Evaluate(context.Background(), left, test.right)
		if err != nil || !got {
			t.Fatalf("%s Evaluate() = %v, %v", operator.Name(), got, err)
		}
	}
	if got, err := operators[0].Evaluate(context.Background(), left, higher); err != nil || got {
		t.Fatalf("unequal Evaluate() = %v, %v", got, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := operators[0].Evaluate(canceled, left, equal); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	for _, pair := range [][2]ruleengine.Value{
		{{}, equal},
		{ruleengine.String(quantityPrefix + "invalid"), equal},
		{left, ruleengine.Int(1)},
		{left, ruleengine.String(quantityPrefix + "invalid")},
		{left, Quantity(measurement.MustNew(decimal.MustParse("1"), measurement.Metre))},
	} {
		if _, err := operators[0].Evaluate(context.Background(), pair[0], pair[1]); err == nil {
			t.Fatalf("Evaluate(%#v, %#v) error = nil", pair[0], pair[1])
		}
	}
}
