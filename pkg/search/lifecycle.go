package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
	"unicode/utf8"
)

var (
	ErrInvalidMigrator       = errors.New("search: lifecycle dependencies are required")
	ErrInvalidMigrationPlan  = errors.New("search: invalid migration plan")
	ErrMigrationNotFound     = errors.New("search: migration state not found")
	ErrMigrationIncomplete   = errors.New("search: migration remains resumable")
	ErrMigrationPlanChanged  = errors.New("search: migration plan changed after execution began")
	ErrMigrationVerification = errors.New("search: migration verification failed")
	ErrMigrationRecovery     = errors.New("search: migration requires application-owned recovery before retry")
	ErrAliasChanged          = errors.New("search: alias no longer identifies the expected generation")
	ErrInvalidMigrationPhase = errors.New("search: operation is invalid for migration phase")
	ErrMigrationCoordination = errors.New("search: migration coordination contract was violated")
)

type MigrationPhase string

const (
	MigrationPending     MigrationPhase = "pending"
	MigrationCreating    MigrationPhase = "creating"
	MigrationCreated     MigrationPhase = "created"
	MigrationDispatching MigrationPhase = "reindex_dispatching"
	MigrationReindexing  MigrationPhase = "reindexing"
	MigrationReindexed   MigrationPhase = "reindexed"
	MigrationVerified    MigrationPhase = "verified"
	MigrationComplete    MigrationPhase = "complete"
	MigrationRolledBack  MigrationPhase = "rolled_back"
	MigrationCleaning    MigrationPhase = "cleaning"
	MigrationCleaned     MigrationPhase = "cleaned"
)

type LifecycleOperation string

const (
	LifecycleCreate   LifecycleOperation = "create"
	LifecycleReindex  LifecycleOperation = "reindex"
	LifecycleVerify   LifecycleOperation = "verify"
	LifecycleCutover  LifecycleOperation = "cutover"
	LifecycleRollback LifecycleOperation = "rollback"
	LifecycleCleanup  LifecycleOperation = "cleanup"
)

type MigrationPlan struct {
	ID                string
	Tenant            string
	Alias             string
	SourceIndex       string
	SourceFingerprint string
	Target            IndexDefinition
	MaxReindexSteps   int
}

type VerificationReport struct {
	Verified    bool
	SourceCount uint64
	TargetCount uint64
	Drift       uint64
}

type MigrationState struct {
	ID              string
	PlanFingerprint string
	Phase           MigrationPhase
	ReindexCursor   string
	Verification    VerificationReport
}

type LifecycleIntent struct {
	MigrationID, Tenant, Resource string
	Operation                     LifecycleOperation
}
type LifecycleEvent struct {
	MigrationID, Tenant, Resource string
	Operation                     LifecycleOperation
	Phase                         MigrationPhase
}

// LifecycleCleanupRequest binds irreversible deletion to the exact migration,
// active generation, inactive generation, and immutable definitions whose
// final eligibility must be proven under one durable exclusion boundary.
type LifecycleCleanupRequest struct {
	MigrationID                        string
	Tenant, Alias                      string
	ActiveIndex, ActiveFingerprint     string
	InactiveIndex, InactiveFingerprint string
}

type LifecycleBackend interface {
	CreateIndex(context.Context, string, IndexDefinition) error
	Reindex(context.Context, string, string, string, string) (cursor string, done bool, err error)
	VerifyIndex(context.Context, string, string, string, string) (VerificationReport, error)
	ResolveAlias(context.Context, string, string) (string, error)
	CutoverAlias(context.Context, string, string, string, string, string) (VerificationReport, error)
	CleanupIndex(context.Context, LifecycleCleanupRequest) error
}
type MigrationStore interface {
	Load(context.Context, string) (MigrationState, error)
	Save(context.Context, MigrationState) error
}

// MigrationCoordinator provides one durable exclusive execution boundary for
// a migration ID across all application instances. WithMigration must invoke
// operation synchronously exactly once and keep exclusivity until it returns.
// The boundary intentionally spans backend I/O so cleanup, rollback, cutover,
// and reindex dispatch cannot race one another.
type MigrationCoordinator interface {
	WithMigration(context.Context, string, func(context.Context) error) error
}
type LifecycleAuthorizer interface {
	Authorize(context.Context, LifecycleIntent) error
}
type LifecycleObserver interface {
	Record(context.Context, LifecycleEvent) error
}

type Migrator struct {
	backend     LifecycleBackend
	store       MigrationStore
	coordinator MigrationCoordinator
	authorizer  LifecycleAuthorizer
	observer    LifecycleObserver
}

func NewMigrator(backend LifecycleBackend, store MigrationStore, authorizer LifecycleAuthorizer, observer LifecycleObserver) (*Migrator, error) {
	if backend == nil || store == nil || authorizer == nil || observer == nil {
		return nil, ErrInvalidMigrator
	}
	coordinator, ok := store.(MigrationCoordinator)
	if !ok {
		return nil, ErrInvalidMigrator
	}
	return &Migrator{backend: backend, store: store, coordinator: coordinator, authorizer: authorizer, observer: observer}, nil
}

// Run resumes an explicit create, reindex, verify, and atomic alias-cutover
// workflow. Each completed external step is persisted before the next begins.
func (m *Migrator) Run(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	return coordinateMigration(ctx, m.coordinator, plan.ID, func(operationCtx context.Context) (MigrationState, error) {
		return m.run(operationCtx, plan)
	})
}

func (m *Migrator) run(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	fingerprint, err := validateMigrationPlan(plan)
	if err != nil {
		return MigrationState{}, err
	}
	state, err := m.store.Load(ctx, plan.ID)
	if errors.Is(err, ErrMigrationNotFound) {
		state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationPending}
	} else if err != nil {
		return MigrationState{}, err
	} else if state.ID != plan.ID || state.PlanFingerprint != fingerprint {
		return state, ErrMigrationPlanChanged
	}
	if len(state.ReindexCursor) > DefaultLimits().MaxQueryBytes {
		return state, ErrInvalidMigrationPhase
	}
	if state.Phase == MigrationComplete {
		return state, nil
	}
	if state.Phase == MigrationCreating || state.Phase == MigrationDispatching || state.Phase == MigrationCleaning {
		return state, ErrMigrationRecovery
	}
	if state.Phase == MigrationRolledBack || state.Phase == MigrationCleaned {
		return state, ErrInvalidMigrationPhase
	}
	switch state.Phase {
	case MigrationPending, MigrationCreated, MigrationReindexing, MigrationReindexed, MigrationVerified:
	default:
		return state, ErrInvalidMigrationPhase
	}

	if state.Phase == MigrationPending {
		if err := m.authorize(ctx, plan, LifecycleCreate, plan.Target.Name()); err != nil {
			return state, err
		}
		state.Phase = MigrationCreating
		if err := m.checkpoint(ctx, plan, state, LifecycleCreate, plan.Target.Name()); err != nil {
			return state, err
		}
		if err := m.backend.CreateIndex(ctx, plan.Tenant, plan.Target); err != nil {
			return state, err
		}
		state.Phase = MigrationCreated
		if err := m.checkpoint(ctx, plan, state, LifecycleCreate, plan.Target.Name()); err != nil {
			return state, err
		}
	}
	reindexAuthorized := false
	if state.Phase == MigrationCreated {
		if err := m.authorize(ctx, plan, LifecycleReindex, plan.SourceIndex); err != nil {
			return state, err
		}
		state.Phase = MigrationDispatching
		if err := m.checkpoint(ctx, plan, state, LifecycleReindex, plan.SourceIndex); err != nil {
			return state, err
		}
		reindexAuthorized = true
	}
	if state.Phase == MigrationDispatching || state.Phase == MigrationReindexing {
		if state.Phase == MigrationReindexing && state.ReindexCursor == "" {
			return state, ErrMigrationRecovery
		}
		for range plan.MaxReindexSteps {
			if !reindexAuthorized {
				if err := m.authorize(ctx, plan, LifecycleReindex, plan.SourceIndex); err != nil {
					return state, err
				}
			}
			reindexAuthorized = false
			cursor, done, reindexErr := m.backend.Reindex(ctx, plan.Tenant, plan.SourceIndex, plan.Target.Name(), state.ReindexCursor)
			if reindexErr != nil {
				return state, reindexErr
			}
			if len(cursor) > DefaultLimits().MaxQueryBytes || !done && cursor == "" {
				return state, ErrMigrationRecovery
			}
			state.ReindexCursor = cursor
			if done {
				state.Phase = MigrationReindexed
			} else {
				state.Phase = MigrationReindexing
			}
			if err := m.checkpoint(ctx, plan, state, LifecycleReindex, plan.SourceIndex); err != nil {
				return state, err
			}
			if done {
				break
			}
		}
		if state.Phase != MigrationReindexed {
			return state, ErrMigrationIncomplete
		}
	}
	if state.Phase == MigrationReindexed {
		if err := m.authorize(ctx, plan, LifecycleVerify, plan.Target.Name()); err != nil {
			return state, err
		}
		report, verifyErr := m.backend.VerifyIndex(ctx, plan.Tenant, plan.SourceIndex, plan.Target.Name(), plan.Target.Fingerprint())
		if verifyErr != nil {
			return state, verifyErr
		}
		if !report.Verified || report.Drift != 0 {
			return state, ErrMigrationVerification
		}
		state.Verification, state.Phase = report, MigrationVerified
		if err := m.checkpoint(ctx, plan, state, LifecycleVerify, plan.Target.Name()); err != nil {
			return state, err
		}
	}
	if state.Phase == MigrationVerified {
		if err := m.authorize(ctx, plan, LifecycleCutover, plan.Alias); err != nil {
			return state, err
		}
		current, resolveErr := m.backend.ResolveAlias(ctx, plan.Tenant, plan.Alias)
		if resolveErr != nil {
			return state, resolveErr
		}
		if current != plan.SourceIndex {
			return state, ErrAliasChanged
		}
		fresh, cutoverErr := m.backend.CutoverAlias(ctx, plan.Tenant, plan.Alias, plan.SourceIndex, plan.Target.Name(), plan.Target.Fingerprint())
		if cutoverErr != nil {
			return state, cutoverErr
		}
		if !fresh.Verified || fresh.Drift != 0 {
			return state, ErrMigrationVerification
		}
		state.Verification = fresh
		state.Phase = MigrationComplete
		if err := m.checkpoint(ctx, plan, state, LifecycleCutover, plan.Alias); err != nil {
			return state, err
		}
	}
	return state, nil
}

// Rollback atomically restores the prior alias generation. It does not delete
// either index.
func (m *Migrator) Rollback(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	return coordinateMigration(ctx, m.coordinator, plan.ID, func(operationCtx context.Context) (MigrationState, error) {
		return m.rollback(operationCtx, plan)
	})
}

func (m *Migrator) rollback(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	state, err := m.loadBound(ctx, plan)
	if err != nil {
		return state, err
	}
	if state.Phase == MigrationRolledBack {
		return state, nil
	}
	if state.Phase != MigrationComplete {
		return state, ErrInvalidMigrationPhase
	}
	if err := m.authorize(ctx, plan, LifecycleRollback, plan.Alias); err != nil {
		return state, err
	}
	current, err := m.backend.ResolveAlias(ctx, plan.Tenant, plan.Alias)
	if err != nil {
		return state, err
	}
	if current != plan.Target.Name() {
		return state, ErrAliasChanged
	}
	fresh, cutoverErr := m.backend.CutoverAlias(ctx, plan.Tenant, plan.Alias, plan.Target.Name(), plan.SourceIndex, plan.SourceFingerprint)
	if cutoverErr != nil {
		return state, cutoverErr
	}
	if !fresh.Verified || fresh.Drift != 0 {
		return state, ErrMigrationVerification
	}
	state.Verification = fresh
	state.Phase = MigrationRolledBack
	if err := m.checkpoint(ctx, plan, state, LifecycleRollback, plan.Alias); err != nil {
		return state, err
	}
	return state, nil
}

// Cleanup deletes only the inactive generation after successful completion or
// rollback and therefore remains separately authorized.
func (m *Migrator) Cleanup(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	return coordinateMigration(ctx, m.coordinator, plan.ID, func(operationCtx context.Context) (MigrationState, error) {
		return m.cleanup(operationCtx, plan)
	})
}

func (m *Migrator) cleanup(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	state, err := m.loadBound(ctx, plan)
	if err != nil {
		return state, err
	}
	if state.Phase == MigrationCleaned {
		return state, nil
	}
	if state.Phase == MigrationCleaning {
		return state, ErrMigrationRecovery
	}
	var active, inactive string
	switch state.Phase {
	case MigrationComplete:
		active = plan.Target.Name()
		inactive = plan.SourceIndex
	case MigrationRolledBack:
		active = plan.SourceIndex
		inactive = plan.Target.Name()
	default:
		return state, ErrInvalidMigrationPhase
	}
	if err := m.authorize(ctx, plan, LifecycleCleanup, inactive); err != nil {
		return state, err
	}
	current, err := m.backend.ResolveAlias(ctx, plan.Tenant, plan.Alias)
	if err != nil {
		return state, err
	}
	if current != active {
		return state, ErrAliasChanged
	}
	state.Phase = MigrationCleaning
	if err := m.checkpoint(ctx, plan, state, LifecycleCleanup, inactive); err != nil {
		return state, err
	}
	activeFingerprint, inactiveFingerprint := plan.Target.Fingerprint(), plan.SourceFingerprint
	if active == plan.SourceIndex {
		activeFingerprint, inactiveFingerprint = plan.SourceFingerprint, plan.Target.Fingerprint()
	}
	if err := m.backend.CleanupIndex(ctx, LifecycleCleanupRequest{
		MigrationID: plan.ID, Tenant: plan.Tenant, Alias: plan.Alias,
		ActiveIndex: active, ActiveFingerprint: activeFingerprint,
		InactiveIndex: inactive, InactiveFingerprint: inactiveFingerprint,
	}); err != nil {
		return state, err
	}
	state.Phase = MigrationCleaned
	if err := m.checkpoint(ctx, plan, state, LifecycleCleanup, inactive); err != nil {
		return state, err
	}
	return state, nil
}

func coordinateMigration[T any](ctx context.Context, coordinator MigrationCoordinator, id string, operation func(context.Context) (T, error)) (T, error) {
	type migrationOutcome struct {
		result T
		err    error
	}
	var zero T
	var calls atomic.Uint32
	var phase atomic.Uint32
	outcomes := make(chan migrationOutcome, 1)
	operationCtx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	coordinationErr := coordinator.WithMigration(operationCtx, id, func(callbackCtx context.Context) error {
		if calls.Add(1) != 1 {
			return ErrMigrationCoordination
		}
		if phase.Load() != 0 {
			return ErrMigrationCoordination
		}
		result, operationErr := operation(callbackCtx)
		if !phase.CompareAndSwap(0, 1) {
			return ErrMigrationCoordination
		}
		outcomes <- migrationOutcome{result: result, err: operationErr}
		return operationErr
	})
	phase.Swap(2)
	cancelOperation()
	if calls.Load() == 0 && coordinationErr != nil {
		return zero, coordinationErr
	}
	if calls.Load() != 1 {
		return zero, ErrMigrationCoordination
	}
	var outcome migrationOutcome
	select {
	case outcome = <-outcomes:
	default:
		return zero, ErrMigrationCoordination
	}
	if coordinationErr != nil {
		return outcome.result, coordinationErr
	}
	return outcome.result, outcome.err
}

func (m *Migrator) loadBound(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	fingerprint, err := validateMigrationPlan(plan)
	if err != nil {
		return MigrationState{}, err
	}
	state, err := m.store.Load(ctx, plan.ID)
	if err != nil {
		return state, err
	}
	if state.ID != plan.ID || state.PlanFingerprint != fingerprint {
		return state, ErrMigrationPlanChanged
	}
	return state, nil
}
func (m *Migrator) authorize(ctx context.Context, plan MigrationPlan, operation LifecycleOperation, resource string) error {
	return m.authorizer.Authorize(ctx, LifecycleIntent{MigrationID: plan.ID, Tenant: plan.Tenant, Resource: resource, Operation: operation})
}
func (m *Migrator) checkpoint(ctx context.Context, plan MigrationPlan, state MigrationState, operation LifecycleOperation, resource string) error {
	if err := m.store.Save(ctx, state); err != nil {
		return err
	}
	return m.observer.Record(ctx, LifecycleEvent{MigrationID: plan.ID, Tenant: plan.Tenant, Resource: resource, Operation: operation, Phase: state.Phase})
}
func validateMigrationPlan(plan MigrationPlan) (string, error) {
	target := plan.Target.Name()
	limits := DefaultLimits()
	if plan.ID == "" || len(plan.ID) > limits.MaxIDBytes || !utf8.ValidString(plan.ID) ||
		plan.Tenant == "" || len(plan.Tenant) > limits.MaxTenantBytes || !utf8.ValidString(plan.Tenant) ||
		plan.Alias == "" || plan.SourceIndex == "" || !validDefinitionFingerprint(plan.SourceFingerprint) ||
		target == "" || !validDefinitionFingerprint(plan.Target.Fingerprint()) ||
		plan.MaxReindexSteps <= 0 || plan.MaxReindexSteps > limits.MaxPages ||
		!validIndexName(plan.SourceIndex) || !validIndexName(plan.Alias) ||
		plan.SourceIndex == target || plan.Alias == plan.SourceIndex || plan.Alias == target {
		return "", ErrInvalidMigrationPlan
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", plan.ID, plan.Tenant, plan.Alias, plan.SourceIndex, plan.SourceFingerprint, target, plan.Target.Fingerprint())))
	return hex.EncodeToString(sum[:]), nil
}

func validDefinitionFingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, _ := hex.DecodeString(value)
	return hex.EncodeToString(decoded) == value
}
