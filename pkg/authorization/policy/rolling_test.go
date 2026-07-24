package policy_test

import (
	"context"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

func TestRollingDeploymentConvergesOnOneRevision(t *testing.T) {
	t.Parallel()

	allow := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.Allow, Reason: "revision-1"}, nil
	})
	deny := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.Deny, Reason: "revision-2"}, nil
	})
	initial := mustRollingSnapshot(t, 1, "policy-v1", allow)
	next := mustRollingSnapshot(t, 2, "policy-v2", deny)
	oldInstance := mustRollingEngine(t, initial)
	newInstance := mustRollingEngine(t, initial)
	request := validRequest()

	assertRollingDecision(t, oldInstance, request, authorization.Allow, 1)
	assertRollingDecision(t, newInstance, request, authorization.Allow, 1)

	if err := newInstance.ReplaceSnapshot(next, 1); err != nil {
		t.Fatalf("new instance ReplaceSnapshot() error = %v", err)
	}
	assertRollingDecision(t, oldInstance, request, authorization.Allow, 1)
	assertRollingDecision(t, newInstance, request, authorization.Deny, 2)

	if err := oldInstance.ReplaceSnapshot(next, 1); err != nil {
		t.Fatalf("old instance ReplaceSnapshot() error = %v", err)
	}
	assertRollingDecision(t, oldInstance, request, authorization.Deny, 2)
	assertRollingDecision(t, newInstance, request, authorization.Deny, 2)
}

func mustRollingSnapshot(
	t *testing.T,
	revision authorization.Revision,
	id authorization.PolicyID,
	evaluator authorization.Evaluator,
) *authorization.Snapshot {
	t.Helper()
	snapshot, err := authorization.NewSnapshot(revision, authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: id, Evaluator: evaluator},
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	return snapshot
}

func mustRollingEngine(t *testing.T, snapshot *authorization.Snapshot) *authorization.Engine {
	t.Helper()
	engine, err := authorization.NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	return engine
}

func assertRollingDecision(
	t *testing.T,
	engine *authorization.Engine,
	request authorization.Request,
	outcome authorization.Outcome,
	revision authorization.Revision,
) {
	t.Helper()
	decision, err := engine.Decide(context.Background(), request)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Outcome != outcome || decision.Revision != revision {
		t.Fatalf("Decide() = (%s, revision %d), want (%s, revision %d)",
			decision.Outcome, decision.Revision, outcome, revision,
		)
	}
}
