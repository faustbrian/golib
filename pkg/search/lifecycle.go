package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	ErrInvalidMigrator       = errors.New("search: lifecycle dependencies are required")
	ErrInvalidMigrationPlan  = errors.New("search: invalid migration plan")
	ErrMigrationNotFound     = errors.New("search: migration state not found")
	ErrMigrationIncomplete   = errors.New("search: migration remains resumable")
	ErrMigrationPlanChanged  = errors.New("search: migration plan changed after execution began")
	ErrMigrationVerification = errors.New("search: migration verification failed")
	ErrAliasChanged          = errors.New("search: alias no longer identifies the expected generation")
	ErrInvalidMigrationPhase = errors.New("search: operation is invalid for migration phase")
)

type MigrationPhase string

const (
	MigrationPending    MigrationPhase = "pending"
	MigrationCreated    MigrationPhase = "created"
	MigrationReindexing MigrationPhase = "reindexing"
	MigrationReindexed  MigrationPhase = "reindexed"
	MigrationVerified   MigrationPhase = "verified"
	MigrationComplete   MigrationPhase = "complete"
	MigrationRolledBack MigrationPhase = "rolled_back"
	MigrationCleaned    MigrationPhase = "cleaned"
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
	ID              string
	Tenant          string
	Alias           string
	SourceIndex     string
	Target          IndexDefinition
	MaxReindexSteps int
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

type LifecycleBackend interface {
	CreateIndex(context.Context, string, IndexDefinition) error
	Reindex(context.Context, string, string, string, string) (cursor string, done bool, err error)
	VerifyIndex(context.Context, string, string, string) (VerificationReport, error)
	ResolveAlias(context.Context, string, string) (string, error)
	SwapAlias(context.Context, string, string, string, string) error
	DeleteIndex(context.Context, string, string) error
}
type MigrationStore interface {
	Load(context.Context, string) (MigrationState, error)
	Save(context.Context, MigrationState) error
}
type LifecycleAuthorizer interface {
	Authorize(context.Context, LifecycleIntent) error
}
type LifecycleObserver interface {
	Record(context.Context, LifecycleEvent) error
}

type Migrator struct {
	backend    LifecycleBackend
	store      MigrationStore
	authorizer LifecycleAuthorizer
	observer   LifecycleObserver
}

func NewMigrator(backend LifecycleBackend, store MigrationStore, authorizer LifecycleAuthorizer, observer LifecycleObserver) (*Migrator, error) {
	if backend == nil || store == nil || authorizer == nil || observer == nil {
		return nil, ErrInvalidMigrator
	}
	return &Migrator{backend: backend, store: store, authorizer: authorizer, observer: observer}, nil
}

// Run resumes an explicit create, reindex, verify, and atomic alias-cutover
// workflow. Each completed external step is persisted before the next begins.
func (m *Migrator) Run(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	fingerprint, err := validateMigrationPlan(plan)
	if err != nil {
		return MigrationState{}, err
	}
	state, err := m.store.Load(ctx, plan.ID)
	if errors.Is(err, ErrMigrationNotFound) {
		state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationPending}
	} else if err != nil {
		return MigrationState{}, err
	} else if state.PlanFingerprint != fingerprint {
		return state, ErrMigrationPlanChanged
	}
	if state.Phase == MigrationComplete {
		return state, nil
	}
	if state.Phase == MigrationRolledBack || state.Phase == MigrationCleaned {
		return state, ErrInvalidMigrationPhase
	}

	if state.Phase == MigrationPending {
		if err := m.authorize(ctx, plan, LifecycleCreate, plan.Target.Name()); err != nil {
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
	if state.Phase == MigrationCreated || state.Phase == MigrationReindexing {
		for range plan.MaxReindexSteps {
			if err := m.authorize(ctx, plan, LifecycleReindex, plan.SourceIndex); err != nil {
				return state, err
			}
			cursor, done, reindexErr := m.backend.Reindex(ctx, plan.Tenant, plan.SourceIndex, plan.Target.Name(), state.ReindexCursor)
			if reindexErr != nil {
				return state, reindexErr
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
		report, verifyErr := m.backend.VerifyIndex(ctx, plan.Tenant, plan.SourceIndex, plan.Target.Name())
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
		if current == plan.SourceIndex {
			if err := m.backend.SwapAlias(ctx, plan.Tenant, plan.Alias, plan.SourceIndex, plan.Target.Name()); err != nil {
				return state, err
			}
		} else if current != plan.Target.Name() {
			return state, ErrAliasChanged
		}
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
	if current == plan.Target.Name() {
		if err := m.backend.SwapAlias(ctx, plan.Tenant, plan.Alias, plan.Target.Name(), plan.SourceIndex); err != nil {
			return state, err
		}
	} else if current != plan.SourceIndex {
		return state, ErrAliasChanged
	}
	state.Phase = MigrationRolledBack
	if err := m.checkpoint(ctx, plan, state, LifecycleRollback, plan.Alias); err != nil {
		return state, err
	}
	return state, nil
}

// Cleanup deletes only the inactive generation after successful completion or
// rollback and therefore remains separately authorized.
func (m *Migrator) Cleanup(ctx context.Context, plan MigrationPlan) (MigrationState, error) {
	state, err := m.loadBound(ctx, plan)
	if err != nil {
		return state, err
	}
	if state.Phase == MigrationCleaned {
		return state, nil
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
	if err := m.backend.DeleteIndex(ctx, plan.Tenant, inactive); err != nil {
		return state, err
	}
	state.Phase = MigrationCleaned
	if err := m.checkpoint(ctx, plan, state, LifecycleCleanup, inactive); err != nil {
		return state, err
	}
	return state, nil
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
	if state.PlanFingerprint != fingerprint {
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
	if plan.ID == "" || plan.Tenant == "" || plan.Alias == "" || plan.SourceIndex == "" || target == "" || plan.Target.Fingerprint() == "" || plan.MaxReindexSteps <= 0 || !validIndexName(plan.SourceIndex) || !validIndexName(plan.Alias) || plan.SourceIndex == target || plan.Alias == plan.SourceIndex || plan.Alias == target {
		return "", ErrInvalidMigrationPlan
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s", plan.ID, plan.Tenant, plan.Alias, plan.SourceIndex, target, plan.Target.Fingerprint())))
	return hex.EncodeToString(sum[:]), nil
}
