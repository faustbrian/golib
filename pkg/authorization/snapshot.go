package authorization

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidPolicy           = errors.New("invalid policy")
	ErrDuplicatePolicy         = errors.New("duplicate policy")
	ErrInvalidActivationWindow = errors.New("invalid policy activation window")
	ErrInvalidRevision         = errors.New("invalid policy revision")
)

// Evaluator is the bounded, I/O-free decision interface implemented by policy
// models such as ACL, RBAC, and ABAC.
type Evaluator interface {
	Evaluate(context.Context, Request) (Decision, error)
}

// PolicyDefinition binds a stable policy identity to its evaluator.
type PolicyDefinition struct {
	ID          PolicyID
	Revision    Revision
	Priority    int
	ActiveFrom  time.Time
	ActiveUntil time.Time
	Metadata    map[string]string
	Evaluator   Evaluator
}

type policyEntry struct {
	id          PolicyID
	revision    Revision
	priority    int
	activeFrom  time.Time
	activeUntil time.Time
	metadata    map[string]string
	evaluator   Evaluator
}

func (policy policyEntry) activeAt(at time.Time) bool {
	return (policy.activeFrom.IsZero() || !at.Before(policy.activeFrom)) &&
		(policy.activeUntil.IsZero() || at.Before(policy.activeUntil))
}

// PolicyInfo is the safe, inspectable metadata for one snapshotted policy.
type PolicyInfo struct {
	ID          PolicyID
	Revision    Revision
	Priority    int
	ActiveFrom  time.Time
	ActiveUntil time.Time
	Metadata    map[string]string
}

// Snapshot is one coherent, immutable policy view used for a complete
// decision. Its policy contents are intentionally private.
type Snapshot struct {
	revision  Revision
	algorithm CombiningAlgorithm
	policies  []policyEntry
}

// NewSnapshot validates and creates a revisioned policy snapshot.
func NewSnapshot(
	revision Revision,
	algorithm CombiningAlgorithm,
	definitions ...PolicyDefinition,
) (*Snapshot, error) {
	if revision == 0 {
		return nil, ErrInvalidRevision
	}
	if _, err := Combine(algorithm, nil); err != nil {
		return nil, err
	}

	policies := make([]policyEntry, len(definitions))
	policyIDs := make(map[PolicyID]struct{}, len(definitions))
	for index, definition := range definitions {
		if definition.ID == "" || definition.Evaluator == nil {
			return nil, fmt.Errorf("policy %d: %w", index, ErrInvalidPolicy)
		}

		if _, exists := policyIDs[definition.ID]; exists {
			return nil, fmt.Errorf("policy %q: %w", definition.ID, ErrDuplicatePolicy)
		}
		if !definition.ActiveFrom.IsZero() && !definition.ActiveUntil.IsZero() &&
			!definition.ActiveUntil.After(definition.ActiveFrom) {
			return nil, fmt.Errorf("policy %q: %w", definition.ID, ErrInvalidActivationWindow)
		}

		policyIDs[definition.ID] = struct{}{}
		policies[index] = policyEntry{
			id:          definition.ID,
			revision:    definition.Revision,
			priority:    definition.Priority,
			activeFrom:  definition.ActiveFrom,
			activeUntil: definition.ActiveUntil,
			metadata:    cloneMetadata(definition.Metadata),
			evaluator:   definition.Evaluator,
		}
		if policies[index].revision == 0 {
			policies[index].revision = revision
		}
	}

	if algorithm == PriorityOrder {
		sort.Slice(policies, func(left, right int) bool {
			if policies[left].priority == policies[right].priority {
				return policies[left].id < policies[right].id
			}

			return policies[left].priority > policies[right].priority
		})
	}

	return &Snapshot{
		revision:  revision,
		algorithm: algorithm,
		policies:  policies,
	}, nil
}

// Revision returns the immutable snapshot revision.
func (snapshot *Snapshot) Revision() Revision {
	return snapshot.revision
}

// Algorithm returns the snapshot's validated combining algorithm.
func (snapshot *Snapshot) Algorithm() CombiningAlgorithm {
	return snapshot.algorithm
}

// Policies returns a defensive copy of inspectable policy metadata.
func (snapshot *Snapshot) Policies() []PolicyInfo {
	policies := make([]PolicyInfo, len(snapshot.policies))
	for index, policy := range snapshot.policies {
		policies[index] = PolicyInfo{
			ID:          policy.id,
			Revision:    policy.revision,
			Priority:    policy.priority,
			ActiveFrom:  policy.activeFrom,
			ActiveUntil: policy.activeUntil,
			Metadata:    cloneMetadata(policy.metadata),
		}
	}

	return policies
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}

	clone := make(map[string]string, len(metadata))
	for key, value := range metadata {
		clone[key] = value
	}

	return clone
}
