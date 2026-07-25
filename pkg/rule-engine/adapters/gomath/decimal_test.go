package ruleenginemath_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func TestDecimalOperatorsPreserveExactOrdering(t *testing.T) {
	t.Parallel()

	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginemath.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	amount := ruleengine.MustPath("shipment", "amount")
	set := ruleengine.RuleSet{ID: "decimal", Rules: []ruleengine.Rule{{
		ID: "larger",
		When: ruleengine.Compare(ruleenginemath.OpDecimalGreaterThan,
			ruleengine.Variable(amount), ruleengine.Literal(ruleenginemath.Decimal(decimal.MustParse("0.3")))),
	}}}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: amount, Value: ruleenginemath.Decimal(decimal.MustParse("0.3000000000000000001"))})
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}
