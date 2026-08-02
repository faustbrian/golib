package hedge

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInternalDeterministicSelectionAndCauses(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	original := attemptCompletion[string]{result: AttemptResult[string]{Value: "original", Ordinal: 0}, classification: ClassificationSuccess, completed: now}
	hedgeEarlier := attemptCompletion[string]{result: AttemptResult[string]{Value: "hedge", Ordinal: 1}, classification: ClassificationSuccess, completed: now.Add(-time.Second)}
	hedgeTie := attemptCompletion[string]{result: AttemptResult[string]{Value: "tie", Ordinal: 2}, classification: ClassificationSuccess, completed: now}
	failure := attemptCompletion[string]{result: AttemptResult[string]{Err: errors.New("failure"), Ordinal: 3}, classification: ClassificationFailure, completed: now}
	winner, losers := chooseWinner(original, []attemptCompletion[string]{hedgeTie, hedgeEarlier, failure}, nil)
	if winner.result.Ordinal != 1 || len(losers) != 3 {
		t.Fatalf("winner=%d losers=%d", winner.result.Ordinal, len(losers))
	}
	if !completionLess(original, hedgeTie) || completionLess(hedgeTie, original) || !completionLess(hedgeEarlier, original) {
		t.Fatal("completion ordering is not time then ordinal")
	}

	classifierErr := errors.New("classifier")
	if !errors.Is(completionError(attemptCompletion[string]{classificationErr: classifierErr}), classifierErr) {
		t.Fatal("classifier cause not selected")
	}
	contextErr := context.Canceled
	if !errors.Is(completionError(attemptCompletion[string]{result: AttemptResult[string]{ContextErr: contextErr}}), contextErr) {
		t.Fatal("context cause not selected")
	}
	if completionError(attemptCompletion[string]{}).Error() != "hedge: attempt classified as failure without an error" {
		t.Fatal("synthetic cause changed")
	}
}

func TestExactSuccessTiesChooseLowestOrdinalForEveryPublishedPermutation(t *testing.T) {
	t.Parallel()

	now := time.Unix(200, 0)
	completions := []attemptCompletion[string]{
		{result: AttemptResult[string]{Ordinal: 0}, classification: ClassificationSuccess, completed: now},
		{result: AttemptResult[string]{Ordinal: 1}, classification: ClassificationSuccess, completed: now},
		{result: AttemptResult[string]{Ordinal: 2}, classification: ClassificationSuccess, completed: now},
	}
	for _, permutation := range completionPermutations(completions) {
		winner, losers := chooseWinner(permutation[0], permutation[1:], nil)
		if winner.result.Ordinal != 0 || len(losers) != 2 {
			t.Fatalf("permutation %v selected %d with %d losers", ordinals(permutation), winner.result.Ordinal, len(losers))
		}
	}
}

func completionPermutations(values []attemptCompletion[string]) [][]attemptCompletion[string] {
	if len(values) == 1 {
		return [][]attemptCompletion[string]{append([]attemptCompletion[string](nil), values...)}
	}
	var result [][]attemptCompletion[string]
	for index, value := range values {
		rest := append([]attemptCompletion[string](nil), values[:index]...)
		rest = append(rest, values[index+1:]...)
		for _, suffix := range completionPermutations(rest) {
			result = append(result, append([]attemptCompletion[string]{value}, suffix...))
		}
	}
	return result
}

func ordinals(values []attemptCompletion[string]) []uint {
	result := make([]uint, 0, len(values))
	for _, value := range values {
		result = append(result, value.result.Ordinal)
	}
	return result
}

func TestInternalNilPermitAndTypedNilObserver(t *testing.T) {
	t.Parallel()

	var permit *outstandingPermit
	permit.Release()
	var observer *testNilObserver
	config := Config[string]{
		MaxHedges: 1, ReplaySafe: true, Delay: time.Second, TotalTimeout: time.Second,
		CleanupTimeout: time.Second, Clock: RealClock{}, Budget: testBudget{},
		Classifier: ClassifyFunc[string](func(context.Context, AttemptResult[string]) (Classification, error) {
			return ClassificationSuccess, nil
		}),
		Disposer: DisposeFunc[string](func(context.Context, string) error { return nil }),
		Observer: observer, Resource: "resource", FactoryFailureMode: FactoryFailureStop,
	}
	policy, err := NewPolicy(config)
	if err != nil || policy.config.Observer != nil {
		t.Fatalf("typed nil observer policy = %+v, %v", policy, err)
	}
}

type testNilObserver struct{}

func (*testNilObserver) TryObserve(Observation) bool { return true }

type testBudget struct{}

func (testBudget) Capacity() uint                   { return 1 }
func (testBudget) TryAcquire(string) (Permit, bool) { return testPermit{}, true }

type testPermit struct{}

func (testPermit) Release() {}
