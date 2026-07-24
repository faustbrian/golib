package knapsack

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidItem identifies item-domain validation failure.
	ErrInvalidItem = errors.New("knapsack: invalid item")
	// ErrInvalidContainer identifies container-domain validation failure.
	ErrInvalidContainer = errors.New("knapsack: invalid container")
	// ErrInvalidRequest identifies an invalid or contradictory request.
	ErrInvalidRequest = errors.New("knapsack: invalid request")
	// ErrInvalidOptions identifies zero, conflicting, or unsupported options.
	ErrInvalidOptions = errors.New("knapsack: invalid options")
	// ErrDuplicateID identifies ambiguous item or container identity.
	ErrDuplicateID = errors.New("knapsack: duplicate ID")
	// ErrInexactResolution identifies a quantity not exactly representable on
	// the selected lattice.
	ErrInexactResolution = errors.New("knapsack: quantity is inexact at the chosen resolution")
	// ErrOverflow identifies checked arithmetic outside the int64 lattice.
	ErrOverflow = errors.New("knapsack: integer overflow")
	// ErrImpossibleItem identifies an item that fits no eligible container.
	ErrImpossibleItem = errors.New("knapsack: item cannot fit an eligible container")
	// ErrOverweightItem identifies an item exceeding every eligible weight cap.
	ErrOverweightItem = errors.New("knapsack: item exceeds every eligible container weight")
	// ErrInsufficientStock identifies finite stock unable to satisfy the request.
	ErrInsufficientStock = errors.New("knapsack: insufficient finite stock")
	// ErrConflictingConstraint identifies mutually inconsistent domain rules.
	ErrConflictingConstraint = errors.New("knapsack: conflicting constraints")
	// ErrNoFeasiblePlacement identifies search without a feasible candidate; it
	// does not prove mathematical infeasibility.
	ErrNoFeasiblePlacement = errors.New("knapsack: no feasible placement found")
	// ErrProvenInfeasible identifies exhaustive bounded proof of infeasibility.
	ErrProvenInfeasible = errors.New("knapsack: proven infeasible")
	// ErrBudgetExhausted identifies a work limit reached before proof completion.
	ErrBudgetExhausted = errors.New("knapsack: work budget exhausted")
	// ErrMemoryBudgetExhausted identifies rejected estimated working memory and
	// supports errors.Is with ErrBudgetExhausted.
	ErrMemoryBudgetExhausted = fmt.Errorf("%w: memory", ErrBudgetExhausted)
	// ErrInternalInvariant identifies solver output rejected by independent
	// verification and is always a release-blocking defect.
	ErrInternalInvariant = errors.New("knapsack: internal verifier invariant failed")
)

// FieldError supplies stable machine-readable context while supporting
// errors.Is/errors.As through its wrapped category.
type FieldError struct {
	// Category is the stable sentinel returned by Unwrap.
	Category error
	// Field is the machine-readable input field name when known.
	Field string
	// ID is the affected item or container ID when known.
	ID string
	// Reason is the human-readable validation detail.
	Reason string
}

// Error formats the stable category with optional ID, field, and reason.
func (e *FieldError) Error() string {
	message := e.Category.Error()
	if e.ID != "" {
		message += " " + e.ID
	}
	if e.Field != "" {
		message += " field " + e.Field
	}
	if e.Reason != "" {
		message += ": " + e.Reason
	}
	return message
}

// Unwrap returns Category for errors.Is and errors.As chains.
func (e *FieldError) Unwrap() error { return e.Category }
