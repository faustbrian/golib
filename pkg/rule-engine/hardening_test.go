package ruleengine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

type concurrentOperator struct{ calls atomic.Int64 }

func (*concurrentOperator) Name() ruleengine.OperatorName { return "concurrent_equal" }
func (*concurrentOperator) Signatures() []ruleengine.Signature {
	return []ruleengine.Signature{{Left: ruleengine.KindInt, Right: ruleengine.KindInt}}
}
func (operator *concurrentOperator) Evaluate(_ context.Context, left, right ruleengine.Value) (bool, error) {
	operator.calls.Add(1)
	leftValue, _ := left.IntValue()
	rightValue, _ := right.IntValue()
	return leftValue == rightValue, nil
}

func TestHostileASTAndOutputBounds(t *testing.T) {
	t.Parallel()

	limits := ruleengine.DefaultLimits()
	limits.MaxASTDepth = 3
	limits.MaxOperands = 3
	limits.MaxExplanation = 1
	deep := ruleengine.Not(ruleengine.Not(ruleengine.Not(ruleengine.True())))
	wide := ruleengine.All(ruleengine.True(), ruleengine.True(), ruleengine.True())
	for _, predicate := range []ruleengine.Predicate{deep, wide} {
		set := ruleengine.RuleSet{ID: "hostile", Rules: []ruleengine.Rule{{ID: "hostile", When: predicate}}}
		if _, _, err := ruleengine.NewCompiler(limits).Compile(context.Background(), set); !ruleengine.IsCode(err, ruleengine.CodeLimitExceeded) {
			t.Fatalf("Compile() error = %v, want limit", err)
		}
	}

	set := ruleengine.RuleSet{ID: "explanation", Strategy: ruleengine.CollectAll, Rules: []ruleengine.Rule{
		{ID: "a", When: ruleengine.True()}, {ID: "b", When: ruleengine.True()},
	}}
	plan, _, err := ruleengine.NewCompiler(limits).Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := ruleengine.NewContext()
	if result := plan.Evaluate(context.Background(), facts); len(result.Explanation) != 1 {
		t.Fatalf("explanation length = %d", len(result.Explanation))
	}
}

func TestEvaluationErrorsStayBoundedRedactedAndIndeterminate(t *testing.T) {
	t.Parallel()

	limits := ruleengine.DefaultLimits()
	limits.MaxDiagnostics = 2
	failing := ruleengine.PredicateFunc(func(context.Context, ruleengine.Context) (bool, error) {
		return false, errors.New("sensitive-value-123")
	})
	set := ruleengine.RuleSet{ID: "errors", Strategy: ruleengine.CollectAll, Rules: []ruleengine.Rule{
		{ID: "a", Priority: 3, When: failing},
		{ID: "b", Priority: 2, When: failing},
		{ID: "c", Priority: 1, When: ruleengine.True()},
	}}
	plan, _, err := ruleengine.NewCompiler(limits).Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, _ := ruleengine.NewContext()
	result := plan.Evaluate(context.Background(), facts)
	if result.Decision != ruleengine.Indeterminate || len(result.Errors) != 2 {
		t.Fatalf("Evaluate() = %#v", result)
	}
	for _, err := range result.Errors {
		if strings.Contains(err.Error(), "sensitive-value-123") {
			t.Fatal("evaluation error disclosed predicate data")
		}
	}
}

func TestCompilationAndEvaluationHonorCancellation(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	set := ruleengine.RuleSet{ID: "cancel", Rules: []ruleengine.Rule{{ID: "rule", When: ruleengine.True()}}}
	if _, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(canceled, set); !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile() error = %v", err)
	}
	plan, _, _ := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	facts, _ := ruleengine.NewContext()
	if result := plan.Evaluate(canceled, facts); result.Decision != ruleengine.Indeterminate || len(result.Errors) == 0 {
		t.Fatalf("Evaluate() = %#v", result)
	}
}

func TestCompiledPlanIsDeterministicUnderConcurrency(t *testing.T) {
	t.Parallel()

	set, facts := benchmarkFixture()
	plan, _, err := ruleengine.NewCompiler(ruleengine.DefaultLimits()).Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	want := plan.Evaluate(context.Background(), facts).MatchedRules
	const workers = 64
	results := make(chan []string, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- plan.Evaluate(context.Background(), facts).MatchedRules
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if !reflect.DeepEqual(result, want) {
			t.Fatalf("matched rules = %#v, want %#v", result, want)
		}
	}
}

func TestContextsCachesAndCustomOperatorsAreConcurrentSafe(t *testing.T) {
	t.Parallel()

	valuePath := ruleengine.MustPath("subject", "value")
	operator := &concurrentOperator{}
	compiler, err := ruleengine.NewCompilerWithOperators(ruleengine.DefaultLimits(), operator)
	if err != nil {
		t.Fatal(err)
	}
	set := ruleengine.RuleSet{ID: "concurrent", Rules: []ruleengine.Rule{{
		ID: "match",
		When: ruleengine.Compare("concurrent_equal",
			ruleengine.Variable(valuePath), ruleengine.Literal(ruleengine.Int(42))),
	}}}
	plan, _, err := compiler.Compile(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := ruleengine.NewContext(ruleengine.Fact{Path: valuePath, Value: ruleengine.Int(42)})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := ruleengine.NewMemoryPlanCache(8)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 64
	const evaluations = 32
	var wait sync.WaitGroup
	for worker := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			key := string(rune('a' + worker%8))
			for range evaluations {
				if result := plan.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
					t.Errorf("Evaluate() decision = %v", result.Decision)
					return
				}
				if got, ok := facts.Lookup(valuePath).IntValue(); !ok || got != 42 {
					t.Errorf("Lookup() = %d, %v", got, ok)
					return
				}
				if err := cache.Put(context.Background(), key, plan); err != nil {
					t.Errorf("Put() error = %v", err)
					return
				}
				if _, ok, err := cache.Get(context.Background(), key); err != nil || !ok {
					t.Errorf("Get() = %v, %v", ok, err)
					return
				}
			}
		}()
	}
	wait.Wait()
	if got, want := operator.calls.Load(), int64(workers*evaluations); got != want {
		t.Fatalf("operator calls = %d, want %d", got, want)
	}
}
