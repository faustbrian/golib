package ruleengine_test

import (
	"context"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

type sameLengthOperator struct{}

func (sameLengthOperator) Name() ruleengine.OperatorName { return "same_length" }
func (sameLengthOperator) Signatures() []ruleengine.Signature {
	return []ruleengine.Signature{{Left: ruleengine.KindString, Right: ruleengine.KindString}}
}
func (sameLengthOperator) Evaluate(_ context.Context, left, right ruleengine.Value) (bool, error) {
	leftValue, _ := left.StringValue()
	rightValue, _ := right.StringValue()
	return len(leftValue) == len(rightValue), nil
}

func TestCompilerUsesAnExplicitTypedOperator(t *testing.T) {
	t.Parallel()

	path := ruleengine.MustPath("parcel", "code")
	set := ruleengine.RuleSet{ID: "custom", Rules: []ruleengine.Rule{{
		ID: "same-length",
		When: ruleengine.Compare("same_length",
			ruleengine.Variable(path), ruleengine.Literal(ruleengine.String("ABC"))),
	}}}
	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), sameLengthOperator{})
	if err != nil {
		t.Fatalf("NewCompilerWithOperators() error = %v", err)
	}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	facts, _ := ruleengine.NewContext(ruleengine.Fact{Path: path, Value: ruleengine.String("XYZ")})
	if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("Evaluate() = %#v", result)
	}
}

func TestCompilerRejectsUnsafeOperatorRegistries(t *testing.T) {
	t.Parallel()

	for _, operators := range [][]ruleengine.Operator{
		{sameLengthOperator{}, sameLengthOperator{}},
		{namedOperator{name: ruleengine.OpEqual}},
		{namedOperator{name: "no_signatures"}},
	} {
		if _, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), operators...); err == nil {
			t.Fatal("NewCompilerWithOperators() error = nil")
		}
	}
}

type namedOperator struct{ name ruleengine.OperatorName }

func (operator namedOperator) Name() ruleengine.OperatorName { return operator.name }
func (namedOperator) Signatures() []ruleengine.Signature     { return nil }
func (namedOperator) Evaluate(context.Context, ruleengine.Value, ruleengine.Value) (bool, error) {
	return false, nil
}

type mapResolver struct {
	values map[string]ruleengine.Value
	calls  []string
}

func (resolver *mapResolver) Resolve(_ context.Context, path ruleengine.Path) (ruleengine.Value, ruleengine.Owner, bool, error) {
	resolver.calls = append(resolver.calls, path.String())
	value, ok := resolver.values[path.String()]
	return value, ruleengine.OwnerResource, ok, nil
}

func TestEvaluateResolvedRequestsOnlyMissingFactsInStableOrder(t *testing.T) {
	t.Parallel()

	a := ruleengine.MustPath("facts", "a")
	b := ruleengine.MustPath("facts", "b")
	set := ruleengine.RuleSet{ID: "resolved", Rules: []ruleengine.Rule{{
		ID: "both",
		When: ruleengine.All(
			ruleengine.Compare(ruleengine.OpEqual, ruleengine.Variable(b), ruleengine.Literal(ruleengine.Int(2))),
			ruleengine.Compare(ruleengine.OpEqual, ruleengine.Variable(a), ruleengine.Literal(ruleengine.Int(1))),
		),
	}}}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	base, _ := ruleengine.NewContext(ruleengine.Fact{Path: b, Value: ruleengine.Int(2)})
	resolver := &mapResolver{values: map[string]ruleengine.Value{a.String(): ruleengine.Int(1)}}

	result := plan.EvaluateResolved(context.Background(), base, resolver)
	if result.Decision != ruleengine.Matched || len(result.Errors) != 0 {
		t.Fatalf("EvaluateResolved() = %#v", result)
	}
	if len(resolver.calls) != 1 || resolver.calls[0] != "facts.a" {
		t.Fatalf("resolver calls = %#v", resolver.calls)
	}
}
