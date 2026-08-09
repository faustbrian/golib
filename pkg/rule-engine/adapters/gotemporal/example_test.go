package ruleenginetemporal_test

import (
	"context"
	"fmt"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func Example() {
	start := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	window, _ := instant.New(start, start.Add(time.Hour), temporal.ClosedOpen)
	windowValue, _ := ruleenginetemporal.Period(window)
	pointValue, _ := ruleenginetemporal.Instant(start.Add(30 * time.Minute))
	compiler, _ := ruleengine.NewCompilerWithOperators(
		ruleengine.DefaultLimits(),
		ruleenginetemporal.Operators()...,
	)
	windowPath := ruleengine.MustPath("delivery", "window")
	set := ruleengine.RuleSet{ID: "delivery", Rules: []ruleengine.Rule{{
		ID: "inside-window",
		When: ruleengine.Compare(
			ruleenginetemporal.OpPeriodContains,
			ruleengine.Variable(windowPath),
			ruleengine.Literal(pointValue),
		),
	}}}
	plan, _, _ := compiler.Compile(context.Background(), set)
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: windowPath, Value: windowValue})

	fmt.Println(plan.Evaluate(context.Background(), facts).Decision == ruleengine.Matched)
	// Output: true
}
