package ruleengine_test

import (
	"context"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

func BenchmarkCompile(b *testing.B) {
	set, _ := benchmarkFixture()
	compiler := ruleengine.NewCompiler(ruleengine.DefaultLimits())
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := compiler.Compile(ctx, set); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvaluate(b *testing.B) {
	set, facts := benchmarkFixture()
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if result := plan.Evaluate(ctx, facts); result.Decision != ruleengine.Matched {
			b.Fatalf("Evaluate() = %#v", result)
		}
	}
}

func benchmarkFixture() (ruleengine.RuleSet, ruleengine.Context) {
	country := ruleengine.MustPath("shipment", "country")
	weight := ruleengine.MustPath("shipment", "weight_grams")
	tags := ruleengine.MustPath("shipment", "tags")
	set := ruleengine.RuleSet{ID: "benchmark", Strategy: ruleengine.CollectAll, Rules: []ruleengine.Rule{
		{ID: "country", Priority: 30, When: ruleengine.Compare(ruleengine.OpEqual, ruleengine.Variable(country), ruleengine.Literal(ruleengine.String("FI")))},
		{ID: "weight", Priority: 20, When: ruleengine.Compare(ruleengine.OpGreaterOrEqual, ruleengine.Variable(weight), ruleengine.Literal(ruleengine.Int(1_000)))},
		{ID: "tag", Priority: 10, When: ruleengine.Compare(ruleengine.OpContains, ruleengine.Variable(tags), ruleengine.Literal(ruleengine.String("express")))},
	}}
	facts, _ := ruleengine.NewContext(
		ruleengine.Fact{Path: country, Value: ruleengine.String("FI")},
		ruleengine.Fact{Path: weight, Value: ruleengine.Int(1_500)},
		ruleengine.Fact{Path: tags, Value: ruleengine.List(ruleengine.String("express"), ruleengine.String("fragile"))},
	)
	return set, facts
}
