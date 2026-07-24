package competitors_test

import (
	"context"
	"testing"

	"github.com/ccpgames/grule-rule-engine/ast"
	"github.com/ccpgames/grule-rule-engine/builder"
	gruleengine "github.com/ccpgames/grule-rule-engine/engine"
	grulepkg "github.com/ccpgames/grule-rule-engine/pkg"
	"github.com/expr-lang/expr"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

type facts struct {
	Country string
	Weight  int64
	Matched bool
}

const gruleDefinition = `
rule EquivalentDecision "equivalent country and weight decision" salience 10 {
when
    Fact.Country == "FI" && Fact.Weight >= 1000
then
    Fact.Matched = true;
    Complete();
}
`

func BenchmarkEquivalentDecision(b *testing.B) {
	b.Run("rule-engine", benchmarkRuleEngine)
	b.Run("expr", benchmarkExpr)
	b.Run("grule", benchmarkGrule)
}

func benchmarkRuleEngine(b *testing.B) {
	country := ruleengine.MustPath("shipment", "country")
	weight := ruleengine.MustPath("shipment", "weight")
	set := ruleengine.RuleSet{ID: "equivalent", Rules: []ruleengine.Rule{{
		ID: "match",
		When: ruleengine.All(
			ruleengine.Compare(ruleengine.OpEqual, ruleengine.Variable(country), ruleengine.Literal(ruleengine.String("FI"))),
			ruleengine.Compare(ruleengine.OpGreaterOrEqual, ruleengine.Variable(weight), ruleengine.Literal(ruleengine.Int(1_000))),
		),
	}}}
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		b.Fatal(err)
	}
	contextFacts, _ := ruleengine.NewContext(
		ruleengine.Fact{Path: country, Value: ruleengine.String("FI")},
		ruleengine.Fact{Path: weight, Value: ruleengine.Int(1_500)},
	)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if result := plan.Evaluate(ctx, contextFacts); result.Decision != ruleengine.Matched {
			b.Fatalf("Evaluate() = %#v", result)
		}
	}
}

func benchmarkExpr(b *testing.B) {
	program, err := expr.Compile(`Country == "FI" && Weight >= 1000`, expr.Env(facts{}))
	if err != nil {
		b.Fatal(err)
	}
	input := facts{Country: "FI", Weight: 1_500}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := expr.Run(program, input)
		if err != nil || result != true {
			b.Fatalf("Run() = %#v, %v", result, err)
		}
	}
}

func benchmarkGrule(b *testing.B) {
	input := &facts{Country: "FI", Weight: 1_500}
	dataContext := ast.NewDataContext()
	if err := dataContext.Add("Fact", input); err != nil {
		b.Fatal(err)
	}
	library := ast.NewKnowledgeLibrary()
	ruleBuilder := builder.NewRuleBuilder(library)
	if err := ruleBuilder.BuildRuleFromResource(
		"equivalent", "1.0.0", grulepkg.NewBytesResource([]byte(gruleDefinition)),
	); err != nil {
		b.Fatal(err)
	}
	knowledge, err := library.NewKnowledgeBaseInstance("equivalent", "1.0.0")
	if err != nil {
		b.Fatal(err)
	}
	engine := gruleengine.NewGruleEngine()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		input.Matched = false
		knowledge.Reset()
		if err := engine.Execute(dataContext, knowledge); err != nil || !input.Matched {
			b.Fatalf("Execute() matched = %v, error = %v", input.Matched, err)
		}
	}
}

func TestEquivalentDecisionBaselines(t *testing.T) {
	t.Parallel()
	for _, benchmark := range []func(*testing.B){benchmarkRuleEngine, benchmarkExpr, benchmarkGrule} {
		result := testing.Benchmark(benchmark)
		if result.N == 0 {
			t.Fatal("benchmark executed no iterations")
		}
	}
}
