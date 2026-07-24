package ruleengine_test

import (
	"context"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
)

type recordingCache struct {
	plan ruleengine.Plan
	key  string
	gets int
	puts int
}

func (cache *recordingCache) Get(_ context.Context, key string) (ruleengine.Plan, bool, error) {
	cache.gets++
	return cache.plan, cache.key == key, nil
}

func (cache *recordingCache) Put(_ context.Context, key string, plan ruleengine.Plan) error {
	cache.puts++
	cache.key = key
	cache.plan = plan
	return nil
}

func TestCompileCachedReusesOnlyMatchingImmutablePlans(t *testing.T) {
	t.Parallel()

	set := ruleengine.RuleSet{ID: "cached", Rules: []ruleengine.Rule{{ID: "match", When: ruleengine.True()}}}
	compiler := ruleengine.NewCompiler(ruleengine.DefaultLimits())
	cache := &recordingCache{}

	first, _, err := compiler.CompileCached(context.Background(), set, cache)
	if err != nil {
		t.Fatalf("CompileCached(first) error = %v", err)
	}
	second, _, err := compiler.CompileCached(context.Background(), set, cache)
	if err != nil {
		t.Fatalf("CompileCached(second) error = %v", err)
	}
	if cache.gets != 2 || cache.puts != 1 {
		t.Fatalf("cache gets = %d, puts = %d", cache.gets, cache.puts)
	}
	if first.Hash() == "" || second.Hash() != first.Hash() {
		t.Fatalf("plan hashes = %q and %q", first.Hash(), second.Hash())
	}
	facts, _ := ruleengine.NewContext()
	if result := second.Evaluate(context.Background(), facts); result.Decision != ruleengine.Matched {
		t.Fatalf("cached Evaluate() = %#v", result)
	}
}

func TestMemoryPlanCacheIsBoundedAndLRU(t *testing.T) {
	t.Parallel()

	cache, err := ruleengine.NewMemoryPlanCache(2)
	if err != nil {
		t.Fatalf("NewMemoryPlanCache() error = %v", err)
	}
	ctx := context.Background()
	if err := cache.Put(ctx, "a", ruleengine.Plan{}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(ctx, "b", ruleengine.Plan{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.Get(ctx, "a"); !ok {
		t.Fatal("cache lost a before capacity was reached")
	}
	if err := cache.Put(ctx, "c", ruleengine.Plan{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.Get(ctx, "b"); ok {
		t.Fatal("cache did not evict least recently used b")
	}
	if cache.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", cache.Len())
	}
}

func TestMemoryPlanCacheHonorsCancellationAndInputValidation(t *testing.T) {
	t.Parallel()

	if _, err := ruleengine.NewMemoryPlanCache(0); err == nil {
		t.Fatal("NewMemoryPlanCache(0) error = nil")
	}
	cache, _ := ruleengine.NewMemoryPlanCache(1)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cache.Put(canceled, "key", ruleengine.Plan{}); err == nil {
		t.Fatal("Put() error = nil for canceled context")
	}
	if _, _, err := cache.Get(canceled, "key"); err == nil {
		t.Fatal("Get() error = nil for canceled context")
	}
}
