package ruleenginemeasurement_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomeasurement"
)

func TestQuantityOperatorsConvertExactCompatibleUnits(t *testing.T) {
	t.Parallel()

	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginemeasurement.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	weight := ruleengine.MustPath("shipment", "weight")
	limit := measurement.MustNew(decimal.MustParse("1"), measurement.Kilogram)
	actual := measurement.MustNew(decimal.MustParse("1001"), measurement.Gram)
	set := ruleengine.RuleSet{ID: "weight", Rules: []ruleengine.Rule{{
		ID: "over",
		When: ruleengine.Compare(ruleenginemeasurement.OpQuantityGreaterThan,
			ruleengine.Variable(weight), ruleengine.Literal(ruleenginemeasurement.Quantity(limit))),
	}}}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: weight, Value: ruleenginemeasurement.Quantity(actual)})
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}
