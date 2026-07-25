package ruleenginetemporal_test

import (
	"context"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestPeriodContainsInstant(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	period, err := instant.New(start, start.Add(time.Hour), temporal.ClosedOpen)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), ruleenginetemporal.Operators()...)
	if err != nil {
		t.Fatal(err)
	}
	window := ruleengine.MustPath("delivery", "window")
	set := ruleengine.RuleSet{ID: "window", Rules: []ruleengine.Rule{{
		ID: "inside",
		When: ruleengine.Compare(ruleenginetemporal.OpPeriodContains,
			ruleengine.Variable(window), ruleengine.Literal(ruleenginetemporal.Instant(start.Add(time.Minute)))),
	}}}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: window, Value: ruleenginetemporal.Period(period)})
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}
