package policy_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/policy"
)

type evaluatorFunc func(context.Context, authorization.Request) (authorization.Decision, error)

func (evaluate evaluatorFunc) Evaluate(
	ctx context.Context,
	request authorization.Request,
) (authorization.Decision, error) {
	return evaluate(ctx, request)
}

func TestDiffReportsAddedRemovedAndChangedPolicies(t *testing.T) {
	t.Parallel()

	noop := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.NotApplicable}, nil
	})
	current, err := authorization.NewSnapshot(
		1,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "removed", Revision: 1, Evaluator: noop},
		authorization.PolicyDefinition{ID: "changed", Revision: 1, Evaluator: noop},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	candidate, err := authorization.NewSnapshot(
		2,
		authorization.AllowOverrides,
		authorization.PolicyDefinition{ID: "changed", Revision: 2, Evaluator: noop},
		authorization.PolicyDefinition{ID: "added", Revision: 1, Evaluator: noop},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}

	diff, err := policy.Diff(current, candidate)
	if err != nil {
		t.Fatalf("policy.Diff() error = %v", err)
	}
	if diff.FromRevision != 1 || diff.ToRevision != 2 {
		t.Errorf("Diff revisions = %d -> %d, want 1 -> 2", diff.FromRevision, diff.ToRevision)
	}
	if !diff.AlgorithmChanged {
		t.Error("Diff.AlgorithmChanged = false, want true")
	}
	assertIDs(t, diff.Added, []authorization.PolicyID{"added"})
	assertIDs(t, diff.Removed, []authorization.PolicyID{"removed"})
	assertIDs(t, diff.Changed, []authorization.PolicyID{"changed"})
}

func TestDryRunComparesDecisionsWithoutChangingPrimary(t *testing.T) {
	t.Parallel()

	allow := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.Allow, Reason: "allow"}, nil
	})
	deny := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.Deny, Reason: "deny"}, nil
	})
	current, err := authorization.NewSnapshot(
		1,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "current", Evaluator: allow},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	candidate, err := authorization.NewSnapshot(
		2,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "candidate", Evaluator: deny},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}

	report, err := policy.DryRun(
		context.Background(),
		current,
		candidate,
		[]authorization.Request{validRequest()},
	)
	if err != nil {
		t.Fatalf("policy.DryRun() error = %v", err)
	}
	if len(report.Decisions) != 1 {
		t.Fatalf("len(Report.Decisions) = %d, want 1", len(report.Decisions))
	}
	comparison := report.Decisions[0]
	if comparison.Current.Outcome != authorization.Allow ||
		comparison.Candidate.Outcome != authorization.Deny || !comparison.Changed {
		t.Errorf("decision comparison = %+v", comparison)
	}
}

func TestDryRunDetectsMatchedPolicyDiagnosticTruncation(t *testing.T) {
	t.Parallel()

	currentEvaluator := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{
			Outcome: authorization.Allow, MatchedPolicyIDs: []authorization.PolicyID{"same"},
		}, nil
	})
	candidateEvaluator := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{
			Outcome: authorization.Allow, MatchedPolicyIDs: []authorization.PolicyID{"same"},
			MatchedPolicyIDsTruncated: true,
		}, nil
	})
	current, err := authorization.NewSnapshot(
		1,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "same", Evaluator: currentEvaluator},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot(current) error = %v", err)
	}
	candidate, err := authorization.NewSnapshot(
		2,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "same", Evaluator: candidateEvaluator},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot(candidate) error = %v", err)
	}

	report, err := policy.DryRun(
		context.Background(),
		current,
		candidate,
		[]authorization.Request{validRequest()},
	)
	if err != nil {
		t.Fatalf("policy.DryRun() error = %v", err)
	}
	if !report.Decisions[0].Changed {
		t.Error("DecisionComparison.Changed = false, want true")
	}
}

func TestDiffAndDryRunRejectNilAndBoundOversizedSnapshots(t *testing.T) {
	t.Parallel()

	if _, err := policy.Diff(nil, nil); !errors.Is(err, policy.ErrNilSnapshot) {
		t.Errorf("policy.Diff(nil) error = %v, want ErrNilSnapshot", err)
	}
	if _, err := policy.DryRun(context.Background(), nil, nil, nil); !errors.Is(err, policy.ErrNilSnapshot) {
		t.Errorf("policy.DryRun(nil) error = %v, want ErrNilSnapshot", err)
	}

	noop := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.NotApplicable}, nil
	})
	small, err := authorization.NewSnapshot(1, authorization.DenyOverrides)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	definitions := make([]authorization.PolicyDefinition, 1001)
	for index := range definitions {
		definitions[index] = authorization.PolicyDefinition{
			ID:        authorization.PolicyID(fmt.Sprintf("policy-%04d", index)),
			Evaluator: noop,
		}
	}
	oversized, err := authorization.NewSnapshot(2, authorization.DenyOverrides, definitions...)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}

	if _, err := policy.DryRun(context.Background(), oversized, small, nil); !errors.Is(err, authorization.ErrPolicyLimitExceeded) {
		t.Errorf("oversized current DryRun() error = %v, want ErrPolicyLimitExceeded", err)
	}
	if _, err := policy.DryRun(context.Background(), small, oversized, nil); !errors.Is(err, authorization.ErrPolicyLimitExceeded) {
		t.Errorf("oversized candidate DryRun() error = %v, want ErrPolicyLimitExceeded", err)
	}

	requests := make([]authorization.Request, 1001)
	if _, err := policy.DryRun(context.Background(), small, small, requests); !errors.Is(err, authorization.ErrBatchLimitExceeded) {
		t.Errorf("oversized batch DryRun() error = %v, want ErrBatchLimitExceeded", err)
	}
}

func TestDiffSortsMultiplePolicyIDs(t *testing.T) {
	t.Parallel()

	noop := evaluatorFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.NotApplicable}, nil
	})
	current, err := authorization.NewSnapshot(1, authorization.DenyOverrides)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	candidate, err := authorization.NewSnapshot(
		2,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{ID: "zulu", Evaluator: noop},
		authorization.PolicyDefinition{ID: "alpha", Evaluator: noop},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	diff, err := policy.Diff(current, candidate)
	if err != nil {
		t.Fatalf("policy.Diff() error = %v", err)
	}
	assertIDs(t, diff.Added, []authorization.PolicyID{"alpha", "zulu"})
}

func validRequest() authorization.Request {
	return authorization.Request{
		Subject:  authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		Action:   "document.read",
		Resource: authorization.Resource{Type: "document", ID: "document-1"},
	}
}

func assertIDs(t *testing.T, got, want []authorization.PolicyID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("IDs[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}
