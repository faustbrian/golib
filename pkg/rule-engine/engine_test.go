package ruleengine_test

import (
	"context"
	"reflect"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func TestCompileRejectsDuplicateRulesAndTypeConfusion(t *testing.T) {
	t.Parallel()

	age := ruleengine.MustPath("recipient", "age")
	valid := ruleengine.Rule{
		ID:       "adult",
		Priority: 10,
		When: ruleengine.Compare(ruleengine.OpGreaterOrEqual,
			ruleengine.Variable(age), ruleengine.Literal(ruleengine.Int(18))),
	}

	for _, test := range []struct {
		name string
		set  ruleengine.RuleSet
		code ruleengine.Code
	}{
		{name: "duplicate", set: ruleengine.RuleSet{ID: "eligibility", Rules: []ruleengine.Rule{valid, valid}}, code: ruleengine.CodeDuplicateRule},
		{name: "unknown operator", set: ruleengine.RuleSet{ID: "eligibility", Rules: []ruleengine.Rule{{ID: "bad", When: ruleengine.Compare("invented", ruleengine.Variable(age), ruleengine.Literal(ruleengine.Int(18)))}}}, code: ruleengine.CodeUnknownOperator},
		{name: "type confusion", set: ruleengine.RuleSet{ID: "eligibility", Rules: []ruleengine.Rule{{ID: "bad", When: ruleengine.Compare(ruleengine.OpGreaterThan, ruleengine.Literal(ruleengine.String("18")), ruleengine.Literal(ruleengine.Int(18)))}}}, code: ruleengine.CodeTypeMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, diagnostics, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), test.set)
			if !ruleengine.IsCode(err, test.code) {
				t.Fatalf("Compile() error = %v, want code %s", err, test.code)
			}
			if len(diagnostics) == 0 || diagnostics[0].RuleID == "" {
				t.Fatalf("diagnostics = %#v, want bounded rule diagnostic", diagnostics)
			}
			if diagnostics[0].Message == "18" {
				t.Fatal("diagnostic disclosed an operand value")
			}
		})
	}
}

func TestEvaluationUsesStablePriorityAndIDOrdering(t *testing.T) {
	t.Parallel()

	country := ruleengine.MustPath("recipient", "country")
	set := ruleengine.RuleSet{
		ID:       "routing",
		Strategy: ruleengine.CollectAll,
		Rules: []ruleengine.Rule{
			{ID: "later", Priority: 10, Tags: []string{"express"}, When: ruleengine.Compare(ruleengine.OpEqual, ruleengine.Variable(country), ruleengine.Literal(ruleengine.String("FI")))},
			{ID: "first", Priority: 20, When: ruleengine.True()},
			{ID: "earlier", Priority: 10, When: ruleengine.True()},
		},
	}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: country, Value: ruleengine.String("FI")})

	result := plan.Evaluate(context.Background(), facts)
	if result.Decision != ruleengine.Matched || len(result.Errors) != 0 {
		t.Fatalf("Evaluate() = %#v", result)
	}
	want := []string{"first", "earlier", "later"}
	if !reflect.DeepEqual(result.MatchedRules, want) {
		t.Fatalf("MatchedRules = %#v, want %#v", result.MatchedRules, want)
	}
	if len(result.Explanation) == 0 || result.Duration <= 0 {
		t.Fatalf("result lacks explanation or duration: %#v", result)
	}
}

func TestLogicalOperatorsShortCircuitDeterministically(t *testing.T) {
	t.Parallel()

	called := 0
	probe := ruleengine.PredicateFunc(func(_ context.Context, _ ruleengine.Context) (bool, error) {
		called++
		return true, nil
	})
	set := ruleengine.RuleSet{ID: "short-circuit", Rules: []ruleengine.Rule{
		{ID: "all", When: ruleengine.All(ruleengine.False(), probe)},
		{ID: "any", When: ruleengine.Any(ruleengine.True(), probe)},
	}}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	facts, _ := ruleengine.NewContext()
	result := plan.Evaluate(context.Background(), facts)
	if len(result.Errors) != 0 {
		t.Fatalf("Evaluate() errors = %v", result.Errors)
	}
	if called != 0 {
		t.Fatalf("short-circuited predicate called %d times", called)
	}
}

func TestFirstMatchStopsAfterFirstOrderedMatch(t *testing.T) {
	t.Parallel()

	set := ruleengine.RuleSet{ID: "first", Strategy: ruleengine.FirstMatch, Rules: []ruleengine.Rule{
		{ID: "low", Priority: 1, When: ruleengine.True()},
		{ID: "high", Priority: 2, When: ruleengine.True()},
	}}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	facts, _ := ruleengine.NewContext()
	result := plan.Evaluate(context.Background(), facts)
	if !reflect.DeepEqual(result.MatchedRules, []string{"high"}) {
		t.Fatalf("MatchedRules = %#v", result.MatchedRules)
	}
}
