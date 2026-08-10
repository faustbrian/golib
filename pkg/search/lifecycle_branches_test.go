package search

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

var errLifecycleBranch = errors.New("lifecycle branch")

type branchLifecycleBackend struct {
	alias                                                            string
	createErr, reindexErr, verifyErr, resolveErr, swapErr, deleteErr error
	reindexDone                                                      bool
	reindexCursor                                                    string
	verification                                                     VerificationReport
}

func (b *branchLifecycleBackend) CreateIndex(context.Context, string, IndexDefinition) error {
	return b.createErr
}
func (b *branchLifecycleBackend) Reindex(context.Context, string, string, string, string) (string, bool, error) {
	return b.reindexCursor, b.reindexDone, b.reindexErr
}
func (b *branchLifecycleBackend) VerifyIndex(context.Context, string, string, string) (VerificationReport, error) {
	return b.verification, b.verifyErr
}
func (b *branchLifecycleBackend) ResolveAlias(context.Context, string, string) (string, error) {
	return b.alias, b.resolveErr
}
func (b *branchLifecycleBackend) SwapAlias(context.Context, string, string, string, string) error {
	return b.swapErr
}
func (b *branchLifecycleBackend) DeleteIndex(context.Context, string, string) error {
	return b.deleteErr
}

type branchMigrationStore struct {
	state            MigrationState
	loadErr, saveErr error
}

func (s *branchMigrationStore) Load(context.Context, string) (MigrationState, error) {
	return s.state, s.loadErr
}
func (s *branchMigrationStore) Save(_ context.Context, state MigrationState) error {
	s.state = state
	return s.saveErr
}

type branchAuthorizer struct{ err error }

func (a branchAuthorizer) Authorize(context.Context, LifecycleIntent) error { return a.err }

type branchObserver struct{ err error }

func (o branchObserver) Record(context.Context, LifecycleEvent) error { return o.err }

func lifecycleBranchPlan(t *testing.T) (MigrationPlan, string) {
	t.Helper()
	definition, err := NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := MigrationPlan{ID: "m", Tenant: "t", Alias: "events-read", SourceIndex: "events-v1", Target: definition, MaxReindexSteps: 1}
	fingerprint, err := validateMigrationPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan, fingerprint
}

func newBranchMigrator(backend *branchLifecycleBackend, store *branchMigrationStore, authErr, observerErr error) *Migrator {
	migrator, _ := NewMigrator(backend, store, branchAuthorizer{authErr}, branchObserver{observerErr})
	return migrator
}

func TestInternalLifecycleConstructionAndRunFailures(t *testing.T) {
	backend := &branchLifecycleBackend{}
	store := &branchMigrationStore{}
	for _, args := range []struct {
		b LifecycleBackend
		s MigrationStore
		a LifecycleAuthorizer
		o LifecycleObserver
	}{{nil, store, branchAuthorizer{}, branchObserver{}}, {backend, nil, branchAuthorizer{}, branchObserver{}}, {backend, store, nil, branchObserver{}}, {backend, store, branchAuthorizer{}, nil}} {
		if _, err := NewMigrator(args.b, args.s, args.a, args.o); err == nil {
			t.Fatal("invalid migrator accepted")
		}
	}
	plan, fingerprint := lifecycleBranchPlan(t)
	invalid := plan
	invalid.ID = ""
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), invalid); !errors.Is(err, ErrInvalidMigrationPlan) {
		t.Fatal(err)
	}
	store.loadErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.loadErr = nil
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: "changed", Phase: MigrationPending}
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrMigrationPlanChanged) {
		t.Fatal(err)
	}
	for _, phase := range []MigrationPhase{MigrationRolledBack, MigrationCleaned} {
		store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: phase}
		if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrInvalidMigrationPhase) {
			t.Fatal(err)
		}
	}

	store.state = MigrationState{}
	store.loadErr = ErrMigrationNotFound
	if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.loadErr = ErrMigrationNotFound
	backend.createErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.createErr = nil
	store.saveErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.saveErr = nil
	if _, err := newBranchMigrator(backend, store, nil, errLifecycleBranch).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}

	store.loadErr = nil
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationCreated}
	if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.reindexErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.reindexErr = nil
	backend.reindexDone = false
	store.saveErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.saveErr = nil
	if _, err := newBranchMigrator(backend, store, nil, errLifecycleBranch).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}

	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationReindexed}
	if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.verifyErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.verifyErr = nil
	backend.verification = VerificationReport{}
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrMigrationVerification) {
		t.Fatal(err)
	}
	backend.verification = VerificationReport{Verified: true}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationReindexed}
	store.saveErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.saveErr = nil

	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationVerified}
	backend.verification = VerificationReport{Verified: true}
	if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.resolveErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.resolveErr = nil
	backend.alias = "unexpected"
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrAliasChanged) {
		t.Fatal(err)
	}
	backend.alias = plan.SourceIndex
	backend.swapErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.swapErr = nil
	backend.alias = plan.Target.Name()
	store.saveErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
}

func TestInternalLifecycleRollbackCleanupAndLoadBranches(t *testing.T) {
	plan, fingerprint := lifecycleBranchPlan(t)
	backend := &branchLifecycleBackend{}
	store := &branchMigrationStore{}
	migrator := newBranchMigrator(backend, store, nil, nil)
	store.loadErr = errLifecycleBranch
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.loadErr = errLifecycleBranch
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.loadErr = nil
	store.state = MigrationState{PlanFingerprint: fingerprint, Phase: MigrationRolledBack}
	if _, err := migrator.Rollback(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	store.state = MigrationState{PlanFingerprint: fingerprint, Phase: MigrationPending}
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, ErrInvalidMigrationPhase) {
		t.Fatal(err)
	}
	store.state = MigrationState{PlanFingerprint: fingerprint, Phase: MigrationComplete}
	if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Rollback(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.resolveErr = errLifecycleBranch
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.resolveErr = nil
	backend.alias = "unexpected"
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, ErrAliasChanged) {
		t.Fatal(err)
	}
	backend.alias = plan.Target.Name()
	backend.swapErr = errLifecycleBranch
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.swapErr = nil
	backend.alias = plan.SourceIndex
	store.saveErr = errLifecycleBranch
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}

	store.saveErr = nil
	store.state = MigrationState{PlanFingerprint: fingerprint, Phase: MigrationCleaned}
	if _, err := migrator.Cleanup(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	store.state = MigrationState{PlanFingerprint: fingerprint, Phase: MigrationPending}
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, ErrInvalidMigrationPhase) {
		t.Fatal(err)
	}
	for _, phase := range []MigrationPhase{MigrationComplete, MigrationRolledBack} {
		store.state = MigrationState{PlanFingerprint: fingerprint, Phase: phase}
		if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
			t.Fatal(err)
		}
	}
	store.state = MigrationState{PlanFingerprint: fingerprint, Phase: MigrationComplete}
	backend.resolveErr = errLifecycleBranch
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.resolveErr = nil
	backend.alias = "unexpected"
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, ErrAliasChanged) {
		t.Fatal(err)
	}
	backend.alias = plan.Target.Name()
	backend.deleteErr = errLifecycleBranch
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.deleteErr = nil
	store.saveErr = errLifecycleBranch
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.saveErr = nil
	store.state = MigrationState{PlanFingerprint: fingerprint, Phase: MigrationComplete}
	if _, err := newBranchMigrator(backend, store, nil, errLifecycleBranch).Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}

	invalid := plan
	invalid.Alias = ""
	if _, err := migrator.loadBound(t.Context(), invalid); !errors.Is(err, ErrInvalidMigrationPlan) {
		t.Fatal(err)
	}
	store.loadErr = errLifecycleBranch
	if _, err := migrator.loadBound(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.loadErr = nil
	store.state.PlanFingerprint = "changed"
	if _, err := migrator.loadBound(t.Context(), plan); !errors.Is(err, ErrMigrationPlanChanged) {
		t.Fatal(err)
	}
}

func TestMigrationResumeRejectsDifferentPhysicalTargetWithSameDefinition(t *testing.T) {
	plan, fingerprint := lifecycleBranchPlan(t)
	changedTarget, err := NewIndexDefinition("events-v3", plan.Target.Settings(), plan.Target.Mappings(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	changedPlan := plan
	changedPlan.Target = changedTarget
	backend := &branchLifecycleBackend{alias: plan.SourceIndex}
	store := &branchMigrationStore{state: MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationVerified}}

	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), changedPlan); !errors.Is(err, ErrMigrationPlanChanged) {
		t.Fatalf("Run() error = %v, want ErrMigrationPlanChanged", err)
	}
}
