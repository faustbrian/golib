package authorization

import "errors"

var (
	// ErrInvalidCombiningAlgorithm indicates an unsupported combining algorithm.
	ErrInvalidCombiningAlgorithm = errors.New("invalid combining algorithm")
	// ErrInvalidOutcome indicates a decision with an unsupported outcome.
	ErrInvalidOutcome = errors.New("invalid decision outcome")
)

// Outcome is the result of evaluating an authorization policy.
type Outcome uint8

const (
	NotApplicable Outcome = iota
	Allow
	Deny
)

func (outcome Outcome) String() string {
	switch outcome {
	case NotApplicable:
		return "not-applicable"
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	default:
		return "unknown"
	}
}

// Decision is the result of one or more policy evaluations.
type Decision struct {
	Outcome                   Outcome
	Reason                    ReasonCode
	MatchedPolicyIDs          []PolicyID
	MatchedPolicyIDsTruncated bool
	Revision                  Revision
	Trace                     []TraceEntry
	TraceTruncated            bool
}

// TraceEntry records one policy result without request or attribute values.
type TraceEntry struct {
	PolicyID PolicyID
	Outcome  Outcome
	Reason   ReasonCode
}

type ReasonCode string

const (
	ReasonDefaultDeny     ReasonCode = "default-deny"
	ReasonInvalidRequest  ReasonCode = "invalid-request"
	ReasonEvaluationError ReasonCode = "evaluation-error"
	ReasonContextCanceled ReasonCode = "context-canceled"
	ReasonPolicyInactive  ReasonCode = "policy-inactive"
	ReasonPolicyStale     ReasonCode = "policy-stale"
)

type PolicyID string
type Revision uint64

// CombiningAlgorithm determines how multiple policy decisions are resolved.
type CombiningAlgorithm uint8

const (
	DenyOverrides CombiningAlgorithm = iota
	AllowOverrides
	FirstApplicable
	PriorityOrder
)

// String returns the stable name of a combining algorithm.
func (algorithm CombiningAlgorithm) String() string {
	switch algorithm {
	case DenyOverrides:
		return "deny-overrides"
	case AllowOverrides:
		return "allow-overrides"
	case FirstApplicable:
		return "first-applicable"
	case PriorityOrder:
		return "priority-order"
	default:
		return "unknown"
	}
}

// Combine resolves decisions with the selected combining algorithm.
func Combine(algorithm CombiningAlgorithm, decisions []Decision) (Decision, error) {
	if algorithm > PriorityOrder {
		return Decision{}, ErrInvalidCombiningAlgorithm
	}

	for _, decision := range decisions {
		if decision.Outcome > Deny {
			return Decision{}, ErrInvalidOutcome
		}
	}

	result := Decision{Outcome: NotApplicable}

	for _, decision := range decisions {
		if (algorithm == FirstApplicable || algorithm == PriorityOrder) &&
			decision.Outcome != NotApplicable {
			return decision, nil
		}

		if algorithm == DenyOverrides && decision.Outcome == Deny {
			return decision, nil
		}

		if algorithm == AllowOverrides && decision.Outcome == Allow {
			return decision, nil
		}

		if decision.Outcome != NotApplicable {
			result = decision
		}
	}

	return result, nil
}
