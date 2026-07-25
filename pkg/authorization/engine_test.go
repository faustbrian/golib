package authorization

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type evaluatorFunc func(context.Context, Request) (Decision, error)

func (evaluate evaluatorFunc) Evaluate(
	ctx context.Context,
	request Request,
) (Decision, error) {
	return evaluate(ctx, request)
}

func TestEngineDefaultsToDeny(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(Revision(42), DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}

	if decision.Outcome != Deny {
		t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, Deny)
	}

	if decision.Reason != ReasonDefaultDeny {
		t.Errorf("Decision.Reason = %q, want %q", decision.Reason, ReasonDefaultDeny)
	}

	if decision.Revision != Revision(42) {
		t.Errorf("Decision.Revision = %d, want 42", decision.Revision)
	}

	if len(decision.MatchedPolicyIDs) != 0 {
		t.Errorf("Decision.MatchedPolicyIDs = %v, want empty", decision.MatchedPolicyIDs)
	}
}

func TestEngineCombinesAndExplainsApplicablePolicies(t *testing.T) {
	t.Parallel()

	allow := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: Allow, Reason: "owner"}, nil
	})
	deny := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: Deny, Reason: "suspended"}, nil
	})

	snapshot, err := NewSnapshot(
		Revision(7),
		DenyOverrides,
		PolicyDefinition{ID: "ownership", Evaluator: allow},
		PolicyDefinition{ID: "suspension", Evaluator: deny},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}

	if decision.Outcome != Deny {
		t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, Deny)
	}

	if decision.Reason != "suspended" {
		t.Errorf("Decision.Reason = %q, want suspended", decision.Reason)
	}

	assertPolicyIDs(t, decision.MatchedPolicyIDs, []PolicyID{"ownership", "suspension"})

	if len(decision.Trace) != 2 {
		t.Fatalf("len(Decision.Trace) = %d, want 2", len(decision.Trace))
	}

	if decision.Trace[0].PolicyID != "ownership" || decision.Trace[0].Outcome != Allow {
		t.Errorf("Decision.Trace[0] = %+v, want ownership allow", decision.Trace[0])
	}

	if decision.Trace[1].PolicyID != "suspension" || decision.Trace[1].Outcome != Deny {
		t.Errorf("Decision.Trace[1] = %+v, want suspension deny", decision.Trace[1])
	}
}

func TestEngineEvaluatesPriorityOrderUntilApplicable(t *testing.T) {
	order := make([]PolicyID, 0, 3)
	evaluator := func(id PolicyID, outcome Outcome) Evaluator {
		return evaluatorFunc(func(context.Context, Request) (Decision, error) {
			order = append(order, id)
			return Decision{Outcome: outcome}, nil
		})
	}

	snapshot, err := NewSnapshot(
		1,
		PriorityOrder,
		PolicyDefinition{ID: "low", Priority: 1, Evaluator: evaluator("low", Deny)},
		PolicyDefinition{ID: "high-b", Priority: 10, Evaluator: evaluator("high-b", Allow)},
		PolicyDefinition{ID: "high-a", Priority: 10, Evaluator: evaluator("high-a", NotApplicable)},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}

	if decision.Outcome != Allow {
		t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, Allow)
	}

	assertPolicyIDs(t, order, []PolicyID{"high-a", "high-b"})
	assertPolicyIDs(t, decision.MatchedPolicyIDs, []PolicyID{"high-b"})

	if len(decision.Trace) != 2 {
		t.Errorf("len(Decision.Trace) = %d, want 2", len(decision.Trace))
	}
}

func TestEngineStopsAfterDecisiveOutcome(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		algorithm CombiningAlgorithm
		outcome   Outcome
	}{
		"deny overrides":   {algorithm: DenyOverrides, outcome: Deny},
		"allow overrides":  {algorithm: AllowOverrides, outcome: Allow},
		"first applicable": {algorithm: FirstApplicable, outcome: Allow},
		"priority ordered": {algorithm: PriorityOrder, outcome: Deny},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			called := 0
			decisive := evaluatorFunc(func(context.Context, Request) (Decision, error) {
				called++
				return Decision{Outcome: tt.outcome}, nil
			})
			unexpected := evaluatorFunc(func(context.Context, Request) (Decision, error) {
				called++
				return Decision{}, errors.New("must not evaluate after decisive outcome")
			})
			snapshot, err := NewSnapshot(
				1,
				tt.algorithm,
				PolicyDefinition{ID: "decisive", Priority: 2, Evaluator: decisive},
				PolicyDefinition{ID: "unexpected", Priority: 1, Evaluator: unexpected},
			)
			if err != nil {
				t.Fatalf("NewSnapshot() error = %v", err)
			}
			engine, err := NewEngine(snapshot)
			if err != nil {
				t.Fatalf("NewEngine() error = %v", err)
			}

			decision, err := engine.Decide(context.Background(), validRequest())
			if err != nil {
				t.Fatalf("Engine.Decide() error = %v", err)
			}
			if decision.Outcome != tt.outcome {
				t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, tt.outcome)
			}
			if called != 1 {
				t.Errorf("evaluator calls = %d, want 1", called)
			}
		})
	}
}

func TestEngineFailsClosed(t *testing.T) {
	t.Parallel()

	evaluationError := errors.New("sensitive backend detail")

	tests := map[string]struct {
		request    Request
		evaluator  Evaluator
		wantError  error
		wantReason ReasonCode
	}{
		"invalid request": {
			request:    Request{},
			wantError:  ErrInvalidRequest,
			wantReason: ReasonInvalidRequest,
		},
		"evaluator error": {
			request: validRequest(),
			evaluator: evaluatorFunc(func(context.Context, Request) (Decision, error) {
				return Decision{}, evaluationError
			}),
			wantError:  evaluationError,
			wantReason: ReasonEvaluationError,
		},
		"evaluator panic": {
			request: validRequest(),
			evaluator: evaluatorFunc(func(context.Context, Request) (Decision, error) {
				panic("sensitive panic detail")
			}),
			wantError:  ErrPolicyPanic,
			wantReason: ReasonEvaluationError,
		},
		"invalid evaluator outcome": {
			request: validRequest(),
			evaluator: evaluatorFunc(func(context.Context, Request) (Decision, error) {
				return Decision{Outcome: Outcome(255)}, nil
			}),
			wantError:  ErrInvalidOutcome,
			wantReason: ReasonEvaluationError,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			definitions := []PolicyDefinition{}
			if tt.evaluator != nil {
				definitions = append(definitions, PolicyDefinition{
					ID:        "test-policy",
					Evaluator: tt.evaluator,
				})
			}

			snapshot, err := NewSnapshot(9, DenyOverrides, definitions...)
			if err != nil {
				t.Fatalf("NewSnapshot() error = %v", err)
			}

			engine, err := NewEngine(snapshot)
			if err != nil {
				t.Fatalf("NewEngine() error = %v", err)
			}

			decision, err := engine.Decide(context.Background(), tt.request)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("Engine.Decide() error = %v, want %v", err, tt.wantError)
			}

			if decision.Outcome != Deny {
				t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, Deny)
			}

			if decision.Reason != tt.wantReason {
				t.Errorf("Decision.Reason = %q, want %q", decision.Reason, tt.wantReason)
			}

			if decision.Revision != 9 {
				t.Errorf("Decision.Revision = %d, want 9", decision.Revision)
			}
		})
	}
}

func TestEngineDecideBatchMatchesIndependentDecisions(t *testing.T) {
	t.Parallel()

	evaluator := evaluatorFunc(func(_ context.Context, request Request) (Decision, error) {
		if request.Action == "document.read" {
			return Decision{Outcome: Allow, Reason: "reader"}, nil
		}

		return Decision{Outcome: NotApplicable}, nil
	})

	snapshot, err := NewSnapshot(
		3,
		DenyOverrides,
		PolicyDefinition{ID: "documents", Evaluator: evaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	engine, err := NewEngine(snapshot, WithLimits(Limits{MaxBatchSize: 2}))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	requests := []Request{validRequest(), validRequest()}
	requests[1].Action = "document.delete"

	batch, err := engine.DecideBatch(context.Background(), requests)
	if err != nil {
		t.Fatalf("Engine.DecideBatch() error = %v", err)
	}

	for index, request := range requests {
		independent, decideErr := engine.Decide(context.Background(), request)
		if decideErr != nil {
			t.Fatalf("Engine.Decide() error = %v", decideErr)
		}

		if batch[index].Outcome != independent.Outcome ||
			batch[index].Reason != independent.Reason ||
			batch[index].Revision != independent.Revision {
			t.Errorf("batch[%d] = %+v, independent = %+v", index, batch[index], independent)
		}
	}

	_, err = engine.DecideBatch(context.Background(), append(requests, validRequest()))
	if !errors.Is(err, ErrBatchLimitExceeded) {
		t.Errorf("oversized Engine.DecideBatch() error = %v, want ErrBatchLimitExceeded", err)
	}
}

func TestEngineReplaceSnapshotIsMonotonicAndOptimistic(t *testing.T) {
	t.Parallel()

	initial, err := NewSnapshot(4, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	engine, err := NewEngine(initial)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	next, err := NewSnapshot(5, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	if err := engine.ReplaceSnapshot(next, 4); err != nil {
		t.Fatalf("Engine.ReplaceSnapshot() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}
	if decision.Revision != 5 {
		t.Errorf("Decision.Revision = %d, want 5", decision.Revision)
	}

	newer, err := NewSnapshot(6, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	if err := engine.ReplaceSnapshot(newer, 4); !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("conflicting Engine.ReplaceSnapshot() error = %v, want ErrRevisionConflict", err)
	}

	stale, err := NewSnapshot(5, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	if err := engine.ReplaceSnapshot(stale, 5); !errors.Is(err, ErrRevisionNotMonotonic) {
		t.Errorf("stale Engine.ReplaceSnapshot() error = %v, want ErrRevisionNotMonotonic", err)
	}
}

func TestEngineBoundsPoliciesAndTrace(t *testing.T) {
	t.Parallel()

	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: NotApplicable}, nil
	})
	snapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "first", Evaluator: evaluator},
		PolicyDefinition{ID: "second", Evaluator: evaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	if _, err := NewEngine(snapshot, WithLimits(Limits{MaxPolicies: 1})); !errors.Is(err, ErrPolicyLimitExceeded) {
		t.Errorf("NewEngine() error = %v, want ErrPolicyLimitExceeded", err)
	}

	engine, err := NewEngine(snapshot, WithLimits(Limits{MaxTraceSize: 1}))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}

	if len(decision.Trace) != 1 {
		t.Errorf("len(Decision.Trace) = %d, want 1", len(decision.Trace))
	}
	if !decision.TraceTruncated {
		t.Error("Decision.TraceTruncated = false, want true")
	}

	matchedEvaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{
			Outcome:          Allow,
			MatchedPolicyIDs: []PolicyID{"one", "two", "three"},
		}, nil
	})
	matchedSnapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "matched", Evaluator: matchedEvaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot(matched) error = %v", err)
	}
	matchedEngine, err := NewEngine(
		matchedSnapshot,
		WithLimits(Limits{MaxMatchedPolicyIDs: 2}),
	)
	if err != nil {
		t.Fatalf("NewEngine(matched) error = %v", err)
	}
	matchedDecision, err := matchedEngine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide(matched) error = %v", err)
	}
	assertPolicyIDs(t, matchedDecision.MatchedPolicyIDs, []PolicyID{"one", "two"})
	if !matchedDecision.MatchedPolicyIDsTruncated {
		t.Error("Decision.MatchedPolicyIDsTruncated = false, want true")
	}

	exactEvaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{
			Outcome:          Allow,
			MatchedPolicyIDs: []PolicyID{"one", "two"},
		}, nil
	})
	exactSnapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "exact", Evaluator: exactEvaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot(exact) error = %v", err)
	}
	exactEngine, err := NewEngine(
		exactSnapshot,
		WithLimits(Limits{MaxMatchedPolicyIDs: 2}),
	)
	if err != nil {
		t.Fatalf("NewEngine(exact) error = %v", err)
	}
	exactDecision, err := exactEngine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide(exact) error = %v", err)
	}
	assertPolicyIDs(t, exactDecision.MatchedPolicyIDs, []PolicyID{"one", "two"})
	if exactDecision.MatchedPolicyIDsTruncated {
		t.Error("Decision.MatchedPolicyIDsTruncated = true at exact limit")
	}

	upstreamEvaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{
			Outcome: Allow, MatchedPolicyIDs: []PolicyID{"one"},
			MatchedPolicyIDsTruncated: true,
		}, nil
	})
	upstreamSnapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "upstream", Evaluator: upstreamEvaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot(upstream) error = %v", err)
	}
	upstreamEngine, err := NewEngine(upstreamSnapshot)
	if err != nil {
		t.Fatalf("NewEngine(upstream) error = %v", err)
	}
	upstreamDecision, err := upstreamEngine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide(upstream) error = %v", err)
	}
	if !upstreamDecision.MatchedPolicyIDsTruncated {
		t.Error("upstream Decision.MatchedPolicyIDsTruncated = false, want true")
	}

	outerSnapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "one", Evaluator: exactEvaluatorWithoutMatches(Allow)},
		PolicyDefinition{ID: "two", Evaluator: exactEvaluatorWithoutMatches(Allow)},
		PolicyDefinition{ID: "three", Evaluator: exactEvaluatorWithoutMatches(Allow)},
	)
	if err != nil {
		t.Fatalf("NewSnapshot(outer) error = %v", err)
	}
	outerEngine, err := NewEngine(
		outerSnapshot,
		WithLimits(Limits{MaxMatchedPolicyIDs: 2}),
	)
	if err != nil {
		t.Fatalf("NewEngine(outer) error = %v", err)
	}
	outerDecision, err := outerEngine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide(outer) error = %v", err)
	}
	assertPolicyIDs(t, outerDecision.MatchedPolicyIDs, []PolicyID{"one", "two"})
	if !outerDecision.MatchedPolicyIDsTruncated {
		t.Error("outer Decision.MatchedPolicyIDsTruncated = false, want true")
	}
}

func TestEngineZeroLimitsRetainDefaultsAndExactPolicyLimitIsAccepted(t *testing.T) {
	t.Parallel()

	allow := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: Allow}, nil
	})
	notApplicable := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: NotApplicable}, nil
	})
	snapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "allow", Evaluator: allow},
		PolicyDefinition{ID: "not-applicable", Evaluator: notApplicable},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(
		snapshot,
		WithLimits(Limits{
			MaxPolicies: 2,
		}),
		WithLimits(Limits{}),
	)
	if err != nil {
		t.Fatalf("NewEngine(at policy limit) error = %v", err)
	}
	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}
	if decision.Outcome != Allow || len(decision.Trace) != 2 ||
		len(decision.MatchedPolicyIDs) != 1 {
		t.Errorf("Engine.Decide(zero limits) = %+v, want bounded allow", decision)
	}
	if _, err := engine.DecideBatch(
		context.Background(),
		[]Request{validRequest()},
	); err != nil {
		t.Errorf("Engine.DecideBatch(zero limits) error = %v", err)
	}

	replacement, err := NewSnapshot(
		2,
		DenyOverrides,
		PolicyDefinition{ID: "allow", Evaluator: allow},
		PolicyDefinition{ID: "not-applicable", Evaluator: notApplicable},
	)
	if err != nil {
		t.Fatalf("NewSnapshot(replacement) error = %v", err)
	}
	if err := engine.ReplaceSnapshot(replacement, 1); err != nil {
		t.Errorf("Engine.ReplaceSnapshot(at policy limit) error = %v", err)
	}
}

func exactEvaluatorWithoutMatches(outcome Outcome) Evaluator {
	return evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: outcome}, nil
	})
}

func TestEngineHonorsCancellationBeforeEvaluation(t *testing.T) {
	t.Parallel()

	called := false
	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		called = true
		return Decision{Outcome: Allow}, nil
	})
	snapshot, err := NewSnapshot(
		8,
		DenyOverrides,
		PolicyDefinition{ID: "allow", Evaluator: evaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	decision, err := engine.Decide(ctx, validRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Engine.Decide() error = %v, want context.Canceled", err)
	}
	if called {
		t.Error("evaluator was called after cancellation")
	}
	if decision.Outcome != Deny || decision.Reason != ReasonContextCanceled {
		t.Errorf("Decision = %+v, want deny with cancellation reason", decision)
	}
}

func TestEngineUsesRequestTimeForActivationWindows(t *testing.T) {
	t.Parallel()

	called := false
	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		called = true
		return Decision{Outcome: Allow}, nil
	})
	activeFrom := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
	snapshot, err := NewSnapshot(2, DenyOverrides, PolicyDefinition{
		ID:          "scheduled",
		ActiveFrom:  activeFrom,
		ActiveUntil: activeFrom.Add(time.Hour),
		Evaluator:   evaluator,
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	request := validRequest()
	request.Environment.Time = activeFrom.Add(-time.Nanosecond)
	decision, err := engine.Decide(context.Background(), request)
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}
	if called {
		t.Error("inactive evaluator was called")
	}
	if decision.Outcome != Deny || decision.Reason != ReasonDefaultDeny {
		t.Errorf("Decision = %+v, want default deny", decision)
	}
	if len(decision.Trace) != 1 || decision.Trace[0].Reason != ReasonPolicyInactive {
		t.Errorf("Decision.Trace = %+v, want inactive trace", decision.Trace)
	}

	request.Environment.Time = activeFrom
	decision, err = engine.Decide(context.Background(), request)
	if err != nil {
		t.Fatalf("active Engine.Decide() error = %v", err)
	}
	if !called || decision.Outcome != Allow {
		t.Errorf("active Decision = %+v, called = %v, want allow", decision, called)
	}

	called = false
	request.Environment.Time = activeFrom.Add(time.Hour)
	decision, err = engine.Decide(context.Background(), request)
	if err != nil {
		t.Fatalf("expired Engine.Decide() error = %v", err)
	}
	if called {
		t.Error("evaluator was called at the exclusive activation end")
	}
	if decision.Outcome != Deny || decision.Reason != ReasonDefaultDeny {
		t.Errorf("expired Decision = %+v, want default deny", decision)
	}
}

func TestNewEngineRejectsNilSnapshot(t *testing.T) {
	t.Parallel()

	_, err := NewEngine(nil)
	if !errors.Is(err, ErrNilSnapshot) {
		t.Errorf("NewEngine() error = %v, want ErrNilSnapshot", err)
	}
}

func TestEngineRevision(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(7, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	if revision := engine.Revision(); revision != 7 {
		t.Errorf("Engine.Revision() = %d, want 7", revision)
	}
}

func TestEngineUsesInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: Allow}, nil
	})
	snapshot, err := NewSnapshot(1, DenyOverrides, PolicyDefinition{
		ID:          "scheduled",
		ActiveFrom:  now,
		ActiveUntil: now.Add(time.Hour),
		Evaluator:   evaluator,
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot, WithClock(func() time.Time { return now }), WithClock(nil))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}
	if decision.Outcome != Allow {
		t.Errorf("Decision.Outcome = %v, want %v", decision.Outcome, Allow)
	}
}

func TestPolicyEvaluationErrorRedactsCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("secret attribute value")
	evaluationError := &PolicyEvaluationError{PolicyID: "policy", Err: cause}
	if !errors.Is(evaluationError, cause) {
		t.Error("PolicyEvaluationError does not unwrap its cause")
	}
	if strings.Contains(evaluationError.Error(), cause.Error()) {
		t.Error("PolicyEvaluationError exposed its cause text")
	}
	if !strings.Contains(evaluationError.Error(), "policy") {
		t.Error("PolicyEvaluationError omitted the policy ID")
	}
}

func TestEngineReplaceSnapshotRejectsNilAndOversizedSnapshots(t *testing.T) {
	t.Parallel()

	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: NotApplicable}, nil
	})
	initial, err := NewSnapshot(1, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(initial, WithLimits(Limits{MaxPolicies: 1}))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	if err := engine.ReplaceSnapshot(nil, 1); !errors.Is(err, ErrNilSnapshot) {
		t.Errorf("nil Engine.ReplaceSnapshot() error = %v, want ErrNilSnapshot", err)
	}

	oversized, err := NewSnapshot(
		2,
		DenyOverrides,
		PolicyDefinition{ID: "first", Evaluator: evaluator},
		PolicyDefinition{ID: "second", Evaluator: evaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	if err := engine.ReplaceSnapshot(oversized, 1); !errors.Is(err, ErrPolicyLimitExceeded) {
		t.Errorf("oversized Engine.ReplaceSnapshot() error = %v, want ErrPolicyLimitExceeded", err)
	}
}

func TestEngineBatchReturnsDecisionsWithValidationErrors(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(1, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decisions, err := engine.DecideBatch(context.Background(), []Request{{}, validRequest()})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Engine.DecideBatch() error = %v, want ErrInvalidRequest", err)
	}
	if len(decisions) != 2 || decisions[0].Reason != ReasonInvalidRequest ||
		decisions[1].Reason != ReasonDefaultDeny {
		t.Errorf("Engine.DecideBatch() decisions = %+v", decisions)
	}
}

func TestEngineHonorsCancellationBetweenPolicies(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	secondCalled := false
	first := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		cancel()
		return Decision{Outcome: NotApplicable}, nil
	})
	second := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		secondCalled = true
		return Decision{Outcome: Allow}, nil
	})
	snapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "first", Evaluator: first},
		PolicyDefinition{ID: "second", Evaluator: second},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(ctx, validRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Engine.Decide() error = %v, want context.Canceled", err)
	}
	if secondCalled {
		t.Error("second evaluator was called after cancellation")
	}
	if decision.Reason != ReasonContextCanceled {
		t.Errorf("Decision.Reason = %q, want %q", decision.Reason, ReasonContextCanceled)
	}
}

func TestEngineFailsClosedForCorruptedSnapshotAlgorithm(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot(1, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	snapshot.algorithm = CombiningAlgorithm(255)
	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if !errors.Is(err, ErrInvalidCombiningAlgorithm) {
		t.Fatalf("Engine.Decide() error = %v, want ErrInvalidCombiningAlgorithm", err)
	}
	if decision.Outcome != Deny || decision.Reason != ReasonEvaluationError {
		t.Errorf("Decision = %+v, want fail-closed evaluation error", decision)
	}
}

func TestEngineAllowsOnlyOneConcurrentSnapshotReplacement(t *testing.T) {
	t.Parallel()

	initial, err := NewSnapshot(1, DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(initial)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	start := make(chan struct{})
	errorsByReplacement := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for revision := Revision(2); revision <= 3; revision++ {
		revision := revision
		go func() {
			next, snapshotErr := NewSnapshot(revision, DenyOverrides)
			if snapshotErr != nil {
				errorsByReplacement <- snapshotErr
				return
			}
			ready.Done()
			<-start
			errorsByReplacement <- engine.ReplaceSnapshot(next, 1)
		}()
	}
	ready.Wait()
	close(start)

	successes := 0
	conflicts := 0
	for range 2 {
		replaceErr := <-errorsByReplacement
		switch {
		case replaceErr == nil:
			successes++
		case errors.Is(replaceErr, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("Engine.ReplaceSnapshot() error = %v", replaceErr)
		}
	}

	if successes != 1 || conflicts != 1 {
		t.Errorf("replacement results = %d successes, %d conflicts; want 1 each", successes, conflicts)
	}
}

func TestEngineConcurrentDecisionsUseOneCoherentSnapshot(t *testing.T) {
	t.Parallel()

	newSnapshot := func(revision Revision) *Snapshot {
		outcome := Deny
		if revision%2 == 1 {
			outcome = Allow
		}
		snapshot, err := NewSnapshot(
			revision,
			DenyOverrides,
			PolicyDefinition{
				ID: "revision-outcome",
				Evaluator: evaluatorFunc(func(context.Context, Request) (Decision, error) {
					return Decision{Outcome: outcome}, nil
				}),
			},
		)
		if err != nil {
			t.Fatalf("NewSnapshot() error = %v", err)
		}

		return snapshot
	}

	engine, err := NewEngine(newSnapshot(1))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	const readers = 8
	start := make(chan struct{})
	done := make(chan struct{})
	errorsByDecision := make(chan error, readers)
	var ready sync.WaitGroup
	var finished sync.WaitGroup
	ready.Add(readers)
	finished.Add(readers)
	for range readers {
		go func() {
			defer finished.Done()
			ready.Done()
			<-start
			for {
				select {
				case <-done:
					return
				default:
				}
				decision, decideErr := engine.Decide(context.Background(), validRequest())
				if decideErr != nil {
					errorsByDecision <- decideErr
					return
				}
				want := Deny
				if decision.Revision%2 == 1 {
					want = Allow
				}
				if decision.Outcome != want {
					errorsByDecision <- errors.New("decision combined data from different snapshots")
					return
				}
			}
		}()
	}
	ready.Wait()
	close(start)

	for revision := Revision(2); revision <= 100; revision++ {
		if err := engine.ReplaceSnapshot(newSnapshot(revision), revision-1); err != nil {
			t.Fatalf("Engine.ReplaceSnapshot() error = %v", err)
		}
	}
	close(done)
	finished.Wait()
	close(errorsByDecision)
	for decideErr := range errorsByDecision {
		t.Error(decideErr)
	}
}

func TestEnginePreservesEvaluatorMatchedPolicyIDs(t *testing.T) {
	t.Parallel()

	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{
			Outcome:          Allow,
			MatchedPolicyIDs: []PolicyID{"rule-1", "rule-2"},
		}, nil
	})
	snapshot, err := NewSnapshot(
		1,
		DenyOverrides,
		PolicyDefinition{ID: "model", Evaluator: evaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Decide(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Engine.Decide() error = %v", err)
	}
	assertPolicyIDs(t, decision.MatchedPolicyIDs, []PolicyID{"rule-1", "rule-2"})
}

func assertPolicyIDs(t *testing.T, got, want []PolicyID) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("policy IDs = %v, want %v", got, want)
	}

	for index := range want {
		if got[index] != want[index] {
			t.Errorf("policy IDs[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func validRequest() Request {
	return Request{
		Subject:  Subject{Kind: SubjectUser, ID: "user-123"},
		Action:   "document.read",
		Resource: Resource{Type: "document", ID: "document-456"},
		Tenant:   "tenant-789",
	}
}
