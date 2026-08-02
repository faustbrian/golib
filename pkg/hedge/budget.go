package hedge

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// ErrInvalidBudget identifies a zero or otherwise unusable shared-work bound.
var ErrInvalidBudget = errors.New("hedge: invalid budget")

// OutstandingBudget bounds concurrent additional attempts across every policy
// and execution sharing the instance. Use one instance per resource when
// resources require independent limits; doing so avoids an unbounded hidden
// resource-key registry.
type OutstandingBudget struct {
	limit       uint64
	outstanding atomic.Uint64
}

// NewOutstandingBudget constructs a finite shared concurrency budget.
func NewOutstandingBudget(limit uint) (*OutstandingBudget, error) {
	if limit == 0 || limit > MaxBudgetCapacity {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidBudget, MaxBudgetCapacity)
	}
	return &OutstandingBudget{limit: uint64(limit)}, nil
}

// Capacity returns the immutable maximum number of outstanding hedges.
func (budget *OutstandingBudget) Capacity() uint {
	if budget == nil {
		return 0
	}
	return uint(budget.limit)
}

// TryAcquire reserves one additional attempt without blocking. A policy holds
// the permit until the completed result is consumed or reclaimed. Resource is
// accepted for the Budget contract but this implementation deliberately has a
// single bounded scope.
func (budget *OutstandingBudget) TryAcquire(_ string) (Permit, bool) {
	if budget == nil {
		return nil, false
	}
	for {
		current := budget.outstanding.Load()
		if current >= budget.limit {
			return nil, false
		}
		if budget.outstanding.CompareAndSwap(current, current+1) {
			return &outstandingPermit{budget: budget}, true
		}
	}
}

// Outstanding returns the number of currently admitted additional attempts.
func (budget *OutstandingBudget) Outstanding() uint64 {
	if budget == nil {
		return 0
	}
	return budget.outstanding.Load()
}

type outstandingPermit struct {
	budget *OutstandingBudget
	once   sync.Once
}

func (permit *outstandingPermit) Release() {
	if permit == nil {
		return
	}
	permit.once.Do(func() { permit.budget.outstanding.Add(^uint64(0)) })
}
