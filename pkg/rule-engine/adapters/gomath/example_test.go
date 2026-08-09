package ruleenginemath_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func Example() {
	operators := ruleenginemath.Operators()
	compiler, _ := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), operators...)
	total := ruleengine.MustPath("order", "total")
	set := ruleengine.RuleSet{ID: "minimum", Rules: []ruleengine.Rule{{
		ID: "qualified",
		When: ruleengine.Compare(
			ruleenginemath.OpDecimalGreaterOrEqual,
			ruleengine.Variable(total),
			ruleengine.Literal(ruleenginemath.Decimal(decimal.MustParse("10.00"))),
		),
	}}}
	plan, _, _ := compiler.Compile(context.Background(), set)
	facts, _ := ruleengine.NewContext(ruleengine.Fact{
		Path:  total,
		Value: ruleenginemath.Decimal(decimal.MustParse("10.000")),
	})

	fmt.Println(plan.Evaluate(context.Background(), facts).Decision == ruleengine.Matched)
	// Output: true
}
