package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrBudgetRejected identifies local work-amplification denial.
	ErrBudgetRejected = errors.New("resilience: work budget rejected")
	// ErrBudgetClosed identifies use after a logical budget scope closed.
	ErrBudgetClosed = errors.New("resilience: budget scope closed")
	// ErrBudgetScopeMismatch identifies use of a scope for different metadata.
	ErrBudgetScopeMismatch = errors.New("resilience: budget scope mismatch")
	// ErrBudgetAlreadyAttached identifies nested scope creation on a budget context.
	ErrBudgetAlreadyAttached = errors.New("resilience: budget already attached")
	// ErrBudgetScopeRequired identifies work admission without a shared scope.
	ErrBudgetScopeRequired = errors.New("resilience: budget scope required")
	// ErrPermitCompleted identifies duplicate permit completion.
	ErrPermitCompleted = errors.New("resilience: permit already completed")
	// ErrPermitExpired identifies completion after abandoned-capacity recovery.
	ErrPermitExpired = errors.New("resilience: permit expired")
)

// RejectionReason identifies the bounded admission rule that denied work.
type RejectionReason string

const (
	// ReasonExecutionLimit denies work after an execution exhausts its total allowance.
	ReasonExecutionLimit RejectionReason = "execution_limit"
	// ReasonConcurrentLimit denies work while the resource has no concurrent capacity.
	ReasonConcurrentLimit RejectionReason = "concurrent_limit"
	// ReasonWindowLimit denies work after the resource exhausts its rolling allowance.
	ReasonWindowLimit RejectionReason = "window_limit"
	// ReasonResourceLimit denies creation of a new bounded resource identity.
	ReasonResourceLimit RejectionReason = "resource_limit"
	// ReasonDuplicateWork denies a physical attempt already admitted by the scope.
	ReasonDuplicateWork RejectionReason = "duplicate_work"
	// ReasonOriginalRequired denies additional work before the original attempt.
	ReasonOriginalRequired RejectionReason = "original_required"
	// ReasonUnknownParent denies additional work whose parent was never admitted.
	ReasonUnknownParent RejectionReason = "unknown_parent"
)

// BudgetConfig defines finite process-local amplification limits.
type BudgetConfig struct {
	MaxResources              int
	MaxAdditionalPerExecution uint64
	MaxConcurrentAdditional   uint64
	MaxAdditionalPerWindow    uint64
	AdditionalWindow          time.Duration
	PermitTTL                 time.Duration
	Clock                     Clock
}

// WorkBudget is the shared admission contract consumed by retry and hedge policies.
type WorkBudget interface {
	Start(context.Context, Metadata) (WorkBudgetScope, context.Context, error)
}

// WorkBudgetScope owns all physical work for one logical execution.
type WorkBudgetScope interface {
	Acquire(context.Context, Attempt) (Permit, error)
	Snapshot() BudgetSnapshot
	Matches(Metadata) bool
	Close() error
}

// Permit owns completion of one admitted physical attempt.
type Permit interface {
	Complete() error
}

// BudgetSnapshot is an immutable bounded accounting view.
type BudgetSnapshot struct {
	LogicalID          string
	Resource           string
	AdditionalAdmitted uint64
	AdditionalActive   uint64
	AdditionalRecent   uint64
	Closed             bool
}

// BudgetRejectionError reports local denial without classifying it as downstream failure.
type BudgetRejectionError struct {
	Reason   RejectionReason
	Snapshot BudgetSnapshot
}

func (err *BudgetRejectionError) Error() string {
	return fmt.Sprintf("%v: %s", ErrBudgetRejected, err.Reason)
}

func (err *BudgetRejectionError) Unwrap() error { return ErrBudgetRejected }

// RejectionReasonOf extracts a bounded reason or returns the empty value.
func RejectionReasonOf(err error) RejectionReason {
	var rejection *BudgetRejectionError
	if errors.As(err, &rejection) {
		return rejection.Reason
	}
	return ""
}

type resourceBudget struct {
	activeAdditional uint64
	recentAdditional []time.Time
	scopes           uint64
}

// Budget owns process-local accounting shared by retry and hedge attempts.
type Budget struct {
	mu        sync.Mutex
	config    BudgetConfig
	resources map[string]*resourceBudget
	scopes    map[*BudgetScope]struct{}
}

// NewBudget validates finite limits and constructs an empty process-local budget.
func NewBudget(config BudgetConfig) (*Budget, error) {
	checks := []struct {
		field string
		valid bool
	}{
		{"max_resources", config.MaxResources > 0},
		{"max_additional_per_execution", config.MaxAdditionalPerExecution > 0},
		{"max_concurrent_additional", config.MaxConcurrentAdditional > 0},
		{"max_additional_per_window", config.MaxAdditionalPerWindow > 0},
		{"additional_window", config.AdditionalWindow > 0},
		{"permit_ttl", config.PermitTTL > 0},
		{"clock", !nilInterface(config.Clock)},
	}
	for _, check := range checks {
		if !check.valid {
			return nil, invalid(ErrInvalidComposition, check.field, "must be positive and configured")
		}
	}
	return &Budget{config: config, resources: make(map[string]*resourceBudget), scopes: make(map[*BudgetScope]struct{})}, nil
}

type budgetContextKey struct{}
type attemptContextKey struct{}

type budgetExecutionState struct {
	scope WorkBudgetScope
	mu    sync.Mutex
	next  uint64
}

// Start creates one logical scope and attaches it to a derived context.
func (budget *Budget) Start(ctx context.Context, metadata Metadata) (WorkBudgetScope, context.Context, error) {
	if ctx == nil {
		return nil, nil, invalid(ErrInvalidMetadata, "context", "must not be nil")
	}
	if _, attached := BudgetScopeFromContext(ctx); attached {
		return nil, nil, ErrBudgetAlreadyAttached
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := metadata.validate(); err != nil {
		return nil, nil, err
	}
	now := budget.config.Clock.Now()
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.reapLocked(now)
	resource := budget.resources[metadata.resource]
	if resource == nil {
		if len(budget.resources) >= budget.config.MaxResources {
			snapshot := BudgetSnapshot{LogicalID: metadata.logicalID, Resource: metadata.resource}
			return nil, nil, &BudgetRejectionError{Reason: ReasonResourceLimit, Snapshot: snapshot}
		}
		resource = &resourceBudget{}
		budget.resources[metadata.resource] = resource
	}
	resource.scopes++
	scope := &BudgetScope{
		budget:   budget,
		metadata: metadata,
		resource: resource,
		permits:  make(map[uint64]*WorkPermit),
		ordinals: make(map[uint64]struct{}),
	}
	budget.scopes[scope] = struct{}{}
	return scope, attachBudgetScope(ctx, scope), nil
}

// WithBudgetScope attaches a custom budget scope without replacing an existing owner.
func WithBudgetScope(ctx context.Context, scope WorkBudgetScope) (context.Context, error) {
	if ctx == nil {
		return nil, invalid(ErrInvalidMetadata, "context", "must not be nil")
	}
	if nilInterface(scope) {
		return nil, invalid(ErrInvalidComposition, "budget_scope", "must not be nil")
	}
	if _, attached := BudgetScopeFromContext(ctx); attached {
		return nil, ErrBudgetAlreadyAttached
	}
	return attachBudgetScope(ctx, scope), nil
}

func attachBudgetScope(ctx context.Context, scope WorkBudgetScope) context.Context {
	return context.WithValue(ctx, budgetContextKey{}, &budgetExecutionState{scope: scope})
}

// BudgetScopeFromContext returns the explicitly attached logical budget scope.
func BudgetScopeFromContext(ctx context.Context) (WorkBudgetScope, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(budgetContextKey{}).(*budgetExecutionState)
	if !ok {
		return nil, false
	}
	if state == nil {
		return nil, false
	}
	if nilInterface(state.scope) {
		return nil, false
	}
	return state.scope, true
}

// AttemptFromContext returns the physical attempt currently invoking work.
func AttemptFromContext(ctx context.Context) (Attempt, bool) {
	if ctx == nil {
		return Attempt{}, false
	}
	attempt, ok := ctx.Value(attemptContextKey{}).(Attempt)
	if !ok {
		return Attempt{}, false
	}
	if _, err := NewAttempt(attempt.Ordinal, attempt.Origin, attempt.ParentOrdinal, attempt.StartedAt); err != nil {
		return Attempt{}, false
	}
	return attempt, true
}

// AdmitAttempt allocates unique physical-attempt lineage, admits it through
// the attached shared budget, and returns a context carrying that attempt.
func AdmitAttempt(ctx context.Context, origin AttemptOrigin, parent uint64, startedAt time.Time) (context.Context, Attempt, Permit, error) {
	if ctx == nil {
		return nil, Attempt{}, nil, invalid(ErrInvalidMetadata, "context", "must not be nil")
	}
	state, ok := ctx.Value(budgetContextKey{}).(*budgetExecutionState)
	if !ok {
		return nil, Attempt{}, nil, ErrBudgetScopeRequired
	}
	if state == nil {
		return nil, Attempt{}, nil, ErrBudgetScopeRequired
	}
	if nilInterface(state.scope) {
		return nil, Attempt{}, nil, ErrBudgetScopeRequired
	}
	ordinal, err := reserveOrdinal(state)
	if err != nil {
		return nil, Attempt{}, nil, err
	}
	attempt, err := NewAttempt(ordinal, origin, parent, startedAt)
	if err != nil {
		return nil, Attempt{}, nil, err
	}
	return admitKnownAttempt(ctx, state, attempt)
}

func admitKnownAttempt(ctx context.Context, state *budgetExecutionState, attempt Attempt) (context.Context, Attempt, Permit, error) {
	advanceOrdinal(state, attempt.Ordinal)
	permit, err := state.scope.Acquire(ctx, attempt)
	if err != nil {
		return nil, Attempt{}, nil, err
	}
	return context.WithValue(ctx, attemptContextKey{}, attempt), attempt, permit, nil
}

func reserveOrdinal(state *budgetExecutionState) (uint64, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.next == ^uint64(0) {
		return 0, invalid(ErrInvalidAttempt, "ordinal", "exhausted")
	}
	state.next++
	return state.next, nil
}

func advanceOrdinal(state *budgetExecutionState, ordinal uint64) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if ordinal > state.next {
		state.next = ordinal
	}
}

func (budget *Budget) reapLocked(now time.Time) {
	for scope := range budget.scopes {
		scope.reapLocked(now)
	}
	for identity, resource := range budget.resources {
		budget.pruneWindowLocked(resource, now)
		if resource.scopes == 0 && resource.activeAdditional == 0 && len(resource.recentAdditional) == 0 {
			delete(budget.resources, identity)
		}
	}
}

func (budget *Budget) pruneWindowLocked(resource *resourceBudget, now time.Time) {
	cutoff := now.Add(-budget.config.AdditionalWindow)
	first := 0
	for first < len(resource.recentAdditional) && !resource.recentAdditional[first].After(cutoff) {
		first++
	}
	resource.recentAdditional = append(resource.recentAdditional[:0], resource.recentAdditional[first:]...)
}

// BudgetScope is the single retry-plus-hedge accounting owner for one logical execution.
type BudgetScope struct {
	budget             *Budget
	metadata           Metadata
	resource           *resourceBudget
	permits            map[uint64]*WorkPermit
	ordinals           map[uint64]struct{}
	additionalAdmitted uint64
	closed             bool
	released           bool
}

// Acquire atomically admits original, retry, or hedge work without waiting.
func (scope *BudgetScope) Acquire(ctx context.Context, attempt Attempt) (Permit, error) {
	if ctx == nil {
		return nil, invalid(ErrInvalidMetadata, "context", "must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := NewAttempt(attempt.Ordinal, attempt.Origin, attempt.ParentOrdinal, attempt.StartedAt); err != nil {
		return nil, err
	}
	now := scope.budget.config.Clock.Now()
	scope.budget.mu.Lock()
	defer scope.budget.mu.Unlock()
	scope.reapLocked(now)
	if scope.closed {
		return nil, ErrBudgetClosed
	}
	attached, ok := BudgetScopeFromContext(ctx)
	if !ok || attached != scope {
		return nil, ErrBudgetScopeMismatch
	}
	if _, exists := scope.ordinals[attempt.Ordinal]; exists {
		return nil, scope.rejectionLocked(ReasonDuplicateWork)
	}
	if attempt.Origin != OriginOriginal {
		if _, exists := scope.ordinals[1]; !exists {
			return nil, scope.rejectionLocked(ReasonOriginalRequired)
		}
		if _, exists := scope.ordinals[attempt.ParentOrdinal]; !exists {
			return nil, scope.rejectionLocked(ReasonUnknownParent)
		}
		if scope.additionalAdmitted >= scope.budget.config.MaxAdditionalPerExecution {
			return nil, scope.rejectionLocked(ReasonExecutionLimit)
		}
		scope.budget.pruneWindowLocked(scope.resource, now)
		if uint64(len(scope.resource.recentAdditional)) >= scope.budget.config.MaxAdditionalPerWindow {
			return nil, scope.rejectionLocked(ReasonWindowLimit)
		}
		if scope.resource.activeAdditional >= scope.budget.config.MaxConcurrentAdditional {
			return nil, scope.rejectionLocked(ReasonConcurrentLimit)
		}
	}

	permit := &WorkPermit{scope: scope, ordinal: attempt.Ordinal, additional: attempt.Origin != OriginOriginal, expiresAt: now.Add(scope.budget.config.PermitTTL)}
	permit.state.Store(permitActive)
	scope.permits[attempt.Ordinal] = permit
	scope.ordinals[attempt.Ordinal] = struct{}{}
	if permit.additional {
		scope.additionalAdmitted++
		scope.resource.activeAdditional++
		scope.resource.recentAdditional = append(scope.resource.recentAdditional, now)
	}
	return permit, nil
}

func (scope *BudgetScope) rejectionLocked(reason RejectionReason) error {
	return &BudgetRejectionError{Reason: reason, Snapshot: scope.snapshotLocked()}
}

func (scope *BudgetScope) reapLocked(now time.Time) {
	for ordinal, permit := range scope.permits {
		if !now.Before(permit.expiresAt) && permit.state.CompareAndSwap(permitActive, permitExpired) {
			delete(scope.permits, ordinal)
			if permit.additional {
				scope.resource.activeAdditional--
			}
		}
	}
	if scope.closed && len(scope.permits) == 0 {
		scope.releaseLocked()
	}
}

func (scope *BudgetScope) releaseLocked() {
	if scope.released {
		return
	}
	scope.released = true
	scope.resource.scopes--
	delete(scope.budget.scopes, scope)
}

func (scope *BudgetScope) snapshotLocked() BudgetSnapshot {
	scope.budget.pruneWindowLocked(scope.resource, scope.budget.config.Clock.Now())
	active := uint64(0)
	for _, permit := range scope.permits {
		if permit.additional {
			active++
		}
	}
	return BudgetSnapshot{
		LogicalID:          scope.metadata.logicalID,
		Resource:           scope.metadata.resource,
		AdditionalAdmitted: scope.additionalAdmitted,
		AdditionalActive:   active,
		AdditionalRecent:   uint64(len(scope.resource.recentAdditional)),
		Closed:             scope.closed,
	}
}

// Snapshot reaps expired permits and returns bounded accounting state.
func (scope *BudgetScope) Snapshot() BudgetSnapshot {
	scope.budget.mu.Lock()
	defer scope.budget.mu.Unlock()
	scope.reapLocked(scope.budget.config.Clock.Now())
	return scope.snapshotLocked()
}

// Matches reports whether this scope owns the supplied immutable metadata.
func (scope *BudgetScope) Matches(metadata Metadata) bool {
	return scope.metadata == metadata
}

// Close rejects new work and releases resource identity after active permits settle.
func (scope *BudgetScope) Close() error {
	scope.budget.mu.Lock()
	defer scope.budget.mu.Unlock()
	if scope.closed {
		return ErrBudgetClosed
	}
	scope.closed = true
	scope.reapLocked(scope.budget.config.Clock.Now())
	if len(scope.permits) == 0 {
		scope.releaseLocked()
	}
	return nil
}

const (
	permitActive uint32 = iota + 1
	permitCompleted
	permitExpired
)

// WorkPermit owns exactly one admitted physical-work lifecycle.
type WorkPermit struct {
	scope      *BudgetScope
	ordinal    uint64
	additional bool
	expiresAt  time.Time
	state      atomic.Uint32
}

// Complete releases concurrent capacity exactly once.
func (permit *WorkPermit) Complete() error {
	permit.scope.budget.mu.Lock()
	defer permit.scope.budget.mu.Unlock()
	if !permit.state.CompareAndSwap(permitActive, permitCompleted) {
		if permit.state.Load() == permitExpired {
			return ErrPermitExpired
		}
		return ErrPermitCompleted
	}
	delete(permit.scope.permits, permit.ordinal)
	if permit.additional {
		permit.scope.resource.activeAdditional--
	}
	if permit.scope.closed && len(permit.scope.permits) == 0 {
		permit.scope.releaseLocked()
	}
	return nil
}
