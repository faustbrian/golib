package ruleengine_test

import (
	"context"
	"reflect"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func TestForwardChainingReachesAStableDecision(t *testing.T) {
	t.Parallel()

	country := ruleengine.MustPath("shipment", "country")
	service := ruleengine.MustPath("shipment", "service")
	set := ruleengine.RuleSet{ID: "service", Strategy: ruleengine.CollectAll, Rules: []ruleengine.Rule{
		{
			ID:       "finish",
			Priority: 20,
			When: ruleengine.Compare(ruleengine.OpEqual,
				ruleengine.Variable(service), ruleengine.Literal(ruleengine.String("express"))),
		},
		{
			ID:       "derive-service",
			Priority: 10,
			When: ruleengine.Compare(ruleengine.OpEqual,
				ruleengine.Variable(country), ruleengine.Literal(ruleengine.String("FI"))),
			Derive: []ruleengine.Fact{{Path: service, Value: ruleengine.String("express"), Owner: ruleengine.OwnerEnvironment}},
		},
	}}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: country, Value: ruleengine.String("FI")})

	result := plan.Evaluate(context.Background(), facts)
	if result.Decision != ruleengine.Matched || len(result.Errors) != 0 {
		t.Fatalf("Evaluate() = %#v", result)
	}
	if !reflect.DeepEqual(result.MatchedRules, []string{"derive-service", "finish"}) {
		t.Fatalf("MatchedRules = %#v", result.MatchedRules)
	}
	value := result.DerivedFacts.Lookup(service)
	if got, ok := value.StringValue(); !ok || got != "express" {
		t.Fatalf("derived service = %#v", value)
	}
}

func TestCompileRejectsForwardChainingCycles(t *testing.T) {
	t.Parallel()

	x := ruleengine.MustPath("derived", "x")
	y := ruleengine.MustPath("derived", "y")
	set := ruleengine.RuleSet{ID: "cycle", Rules: []ruleengine.Rule{
		{ID: "x-from-y", When: ruleengine.Exists(y), Derive: []ruleengine.Fact{{Path: x, Value: ruleengine.Int(1)}}},
		{ID: "y-from-x", When: ruleengine.Exists(x), Derive: []ruleengine.Fact{{Path: y, Value: ruleengine.Int(1)}}},
	}}

	_, diagnostics, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if !ruleengine.IsCode(err, ruleengine.CodeCycle) {
		t.Fatalf("Compile() error = %v, want cycle", err)
	}
	if len(diagnostics) == 0 {
		t.Fatal("Compile() returned no diagnostic")
	}
}

func TestForwardChainingHonorsIterationLimit(t *testing.T) {
	t.Parallel()

	limits := ruleengine.DefaultLimits()
	limits.MaxIterations = 1
	seed := ruleengine.MustPath("derived", "seed")
	next := ruleengine.MustPath("derived", "next")
	set := ruleengine.RuleSet{ID: "bounded", Strategy: ruleengine.CollectAll, Rules: []ruleengine.Rule{
		{ID: "seed", When: ruleengine.True(), Derive: []ruleengine.Fact{{Path: seed, Value: ruleengine.Int(1)}}},
		{ID: "next", Priority: 10, When: ruleengine.Exists(seed), Derive: []ruleengine.Fact{{Path: next, Value: ruleengine.Int(1)}}},
	}}
	plan, _, err := ruleengine.NewCompiler(limits).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	facts, _ := ruleengine.NewContext()

	result := plan.Evaluate(context.Background(), facts)
	if result.Decision != ruleengine.Indeterminate || len(result.Errors) == 0 ||
		!ruleengine.IsCode(result.Errors[0], ruleengine.CodeLimitExceeded) {
		t.Fatalf("Evaluate() = %#v, want iteration limit error", result)
	}
}
