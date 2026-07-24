package authorization

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewSnapshotRejectsInvalidPolicies(t *testing.T) {
	t.Parallel()

	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: NotApplicable}, nil
	})

	tests := map[string]struct {
		definitions []PolicyDefinition
		want        error
	}{
		"empty policy id": {
			definitions: []PolicyDefinition{{Evaluator: evaluator}},
			want:        ErrInvalidPolicy,
		},
		"nil evaluator": {
			definitions: []PolicyDefinition{{ID: "policy"}},
			want:        ErrInvalidPolicy,
		},
		"duplicate policy id": {
			definitions: []PolicyDefinition{
				{ID: "policy", Evaluator: evaluator},
				{ID: "policy", Evaluator: evaluator},
			},
			want: ErrDuplicatePolicy,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewSnapshot(1, DenyOverrides, tt.definitions...)
			if !errors.Is(err, tt.want) {
				t.Errorf("NewSnapshot() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNewSnapshotRejectsZeroRevision(t *testing.T) {
	t.Parallel()

	if _, err := NewSnapshot(0, DenyOverrides); !errors.Is(err, ErrInvalidRevision) {
		t.Errorf("NewSnapshot(0) error = %v, want ErrInvalidRevision", err)
	}
}

func TestSnapshotCopiesInspectablePolicyMetadata(t *testing.T) {
	t.Parallel()

	evaluator := evaluatorFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: Allow}, nil
	})
	metadata := map[string]string{"owner": "security"}
	activeFrom := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	activeUntil := activeFrom.Add(24 * time.Hour)

	snapshot, err := NewSnapshot(11, DenyOverrides, PolicyDefinition{
		ID:          "policy",
		Priority:    10,
		ActiveFrom:  activeFrom,
		ActiveUntil: activeUntil,
		Metadata:    metadata,
		Evaluator:   evaluator,
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	metadata["owner"] = "mutated"
	policies := snapshot.Policies()
	if len(policies) != 1 {
		t.Fatalf("len(Snapshot.Policies()) = %d, want 1", len(policies))
	}
	if policies[0].Metadata["owner"] != "security" {
		t.Errorf("policy metadata owner = %q, want security", policies[0].Metadata["owner"])
	}

	policies[0].Metadata["owner"] = "also-mutated"
	if snapshot.Policies()[0].Metadata["owner"] != "security" {
		t.Error("Snapshot.Policies() exposed mutable metadata")
	}
	if snapshot.Revision() != 11 {
		t.Errorf("Snapshot.Revision() = %d, want 11", snapshot.Revision())
	}
	if snapshot.Algorithm() != DenyOverrides {
		t.Errorf("Snapshot.Algorithm() = %v, want DenyOverrides", snapshot.Algorithm())
	}
}

func TestNewSnapshotRejectsInvalidActivationWindow(t *testing.T) {
	t.Parallel()

	now := time.Now()
	_, err := NewSnapshot(1, DenyOverrides, PolicyDefinition{
		ID:          "policy",
		ActiveFrom:  now,
		ActiveUntil: now.Add(-time.Second),
		Evaluator: evaluatorFunc(func(context.Context, Request) (Decision, error) {
			return Decision{Outcome: Allow}, nil
		}),
	})
	if !errors.Is(err, ErrInvalidActivationWindow) {
		t.Errorf("NewSnapshot() error = %v, want ErrInvalidActivationWindow", err)
	}
}

func TestNewSnapshotRejectsInvalidCombiningAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := NewSnapshot(1, CombiningAlgorithm(255))
	if !errors.Is(err, ErrInvalidCombiningAlgorithm) {
		t.Errorf("NewSnapshot() error = %v, want ErrInvalidCombiningAlgorithm", err)
	}
}
