package ruleengine_test

import (
	"context"
	"fmt"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func Example() {
	country := ruleengine.MustPath("shipment", "country")
	weight := ruleengine.MustPath("shipment", "weight_grams")
	set := ruleengine.RuleSet{ID: "location-routing", Rules: []ruleengine.Rule{{
		ID:       "finland-heavy",
		Priority: 100,
		When: ruleengine.All(
			ruleengine.Compare(ruleengine.OpEqual,
				ruleengine.Variable(country), ruleengine.Literal(ruleengine.String("FI"))),
			ruleengine.Compare(ruleengine.OpGreaterOrEqual,
				ruleengine.Variable(weight), ruleengine.Literal(ruleengine.Int(1_000))),
		),
	}}}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		panic(err)
	}
	facts, err := ruleengine.NewContext(
		ruleengine.Fact{Path: country, Value: ruleengine.String("FI"), Owner: ruleengine.OwnerResource},
		ruleengine.Fact{Path: weight, Value: ruleengine.Int(1_500), Owner: ruleengine.OwnerResource},
	)
	if err != nil {
		panic(err)
	}
	result := plan.Evaluate(context.Background(), facts)

	fmt.Println(result.Decision == ruleengine.Matched)
	fmt.Println(result.MatchedRules)
	// Output:
	// true
	// [finland-heavy]
}
