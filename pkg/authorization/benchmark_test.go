package authorization

import (
	"context"
	"testing"
)

func BenchmarkEngineDecideWarm(b *testing.B) {
	snapshot, err := NewSnapshot(1, DenyOverrides, PolicyDefinition{
		ID: "allow", Evaluator: evaluatorFunc(func(context.Context, Request) (Decision, error) {
			return Decision{Outcome: Allow}, nil
		}),
	})
	if err != nil {
		b.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		b.Fatalf("NewEngine() error = %v", err)
	}
	request := validRequest()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := engine.Decide(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineDecideCold(b *testing.B) {
	request := validRequest()
	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: Allow}, nil
	})
	b.ReportAllocs()
	for range b.N {
		snapshot, err := NewSnapshot(1, DenyOverrides, PolicyDefinition{
			ID: "allow", Evaluator: evaluator,
		})
		if err != nil {
			b.Fatal(err)
		}
		engine, err := NewEngine(snapshot)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := engine.Decide(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineDecideBatch100(b *testing.B) {
	snapshot, err := NewSnapshot(1, DenyOverrides, PolicyDefinition{
		ID: "allow", Evaluator: evaluatorFunc(func(context.Context, Request) (Decision, error) {
			return Decision{Outcome: Allow}, nil
		}),
	})
	if err != nil {
		b.Fatal(err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		b.Fatal(err)
	}
	requests := make([]Request, 100)
	for index := range requests {
		requests[index] = validRequest()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := engine.DecideBatch(context.Background(), requests); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineReload(b *testing.B) {
	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: Allow}, nil
	})
	initial, err := NewSnapshot(1, DenyOverrides,
		PolicyDefinition{ID: "allow", Evaluator: evaluator},
	)
	if err != nil {
		b.Fatal(err)
	}
	engine, err := NewEngine(initial)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for index := range b.N {
		revision := Revision(index + 2)
		next, snapshotErr := NewSnapshot(revision, DenyOverrides,
			PolicyDefinition{ID: "allow", Evaluator: evaluator},
		)
		if snapshotErr != nil {
			b.Fatal(snapshotErr)
		}
		if err := engine.ReplaceSnapshot(next, revision-1); err != nil {
			b.Fatal(err)
		}
	}
}
