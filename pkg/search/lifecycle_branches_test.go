package search

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var errLifecycleBranch = errors.New("lifecycle branch")

type branchLifecycleBackend struct {
	alias                                                                        string
	createErr, reindexErr, verifyErr, resolveErr, cutoverErr, swapErr, deleteErr error
	reindexDone                                                                  bool
	reindexCursor                                                                string
	verification                                                                 VerificationReport
}

func (b *branchLifecycleBackend) CreateIndex(context.Context, string, IndexDefinition) error {
	return b.createErr
}
func (b *branchLifecycleBackend) Reindex(context.Context, string, string, string, string) (string, bool, error) {
	return b.reindexCursor, b.reindexDone, b.reindexErr
}
func (b *branchLifecycleBackend) VerifyIndex(context.Context, string, string, string, string) (VerificationReport, error) {
	return b.verification, b.verifyErr
}
func (b *branchLifecycleBackend) ResolveAlias(context.Context, string, string) (string, error) {
	return b.alias, b.resolveErr
}
func (b *branchLifecycleBackend) CutoverAlias(context.Context, string, string, string, string, string) (VerificationReport, error) {
	return b.verification, b.cutoverErr
}
func (b *branchLifecycleBackend) SwapAlias(context.Context, string, string, string, string) error {
	return b.swapErr
}
func (b *branchLifecycleBackend) CleanupIndex(context.Context, LifecycleCleanupRequest) error {
	return b.deleteErr
}

type branchMigrationStore struct {
	state            MigrationState
	loadErr, saveErr error
}

type nonCoordinatingMigrationStore struct{}

func (nonCoordinatingMigrationStore) Load(context.Context, string) (MigrationState, error) {
	return MigrationState{}, ErrMigrationNotFound
}
func (nonCoordinatingMigrationStore) Save(context.Context, MigrationState) error { return nil }

func (s *branchMigrationStore) WithMigration(ctx context.Context, _ string, operation func(context.Context) error) error {
	return operation(ctx)
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
	plan := MigrationPlan{ID: "m", Tenant: "t", Alias: "events-read", SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1}
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
	if _, err := NewMigrator(backend, nonCoordinatingMigrationStore{}, branchAuthorizer{}, branchObserver{}); !errors.Is(err, ErrInvalidMigrator) {
		t.Fatalf("NewMigrator(non-coordinator) error = %v", err)
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
	for _, phase := range []MigrationPhase{MigrationCreating, MigrationDispatching, MigrationCleaning} {
		store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: phase}
		if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrMigrationRecovery) {
			t.Fatal(err)
		}
	}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationReindexing}
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrMigrationRecovery) {
		t.Fatal("cursorless reindex resume accepted", err)
	}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationReindexing, ReindexCursor: strings.Repeat("c", DefaultLimits().MaxQueryBytes+1)}
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrInvalidMigrationPhase) {
		t.Fatal("oversized persisted reindex cursor accepted", err)
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
	backend.reindexCursor = strings.Repeat("c", DefaultLimits().MaxQueryBytes+1)
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationCreated}
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrMigrationRecovery) {
		t.Fatal("oversized backend reindex cursor accepted", err)
	}
	backend.reindexCursor = "cursor"
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationCreated}
	store.saveErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.saveErr = nil
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationCreated}
	if _, err := newBranchMigrator(backend, store, nil, errLifecycleBranch).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationReindexing, ReindexCursor: "cursor"}
	if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
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
	backend.cutoverErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.cutoverErr = nil
	backend.verification = VerificationReport{}
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrMigrationVerification) {
		t.Fatal(err)
	}
	backend.verification = VerificationReport{Verified: true}
	backend.alias = plan.Target.Name()
	store.saveErr = errLifecycleBranch
	if _, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan); !errors.Is(err, ErrAliasChanged) {
		t.Fatal(err)
	}
}

func TestMigrationCoordinatorMustInvokeExactlyOnceWhileActive(t *testing.T) {
	var retained func(context.Context) error
	retainedOperationCalls := 0
	noCall := migrationCoordinatorFunc(func(_ context.Context, _ string, operation func(context.Context) error) error {
		retained = operation
		return nil
	})
	if _, err := coordinateMigration(t.Context(), noCall, "migration", func(context.Context) (int, error) {
		retainedOperationCalls++
		return 1, nil
	}); !errors.Is(err, ErrMigrationCoordination) {
		t.Fatalf("coordinateMigration(no call) error = %v", err)
	}
	if err := retained(t.Context()); !errors.Is(err, ErrMigrationCoordination) {
		t.Fatalf("retained operation error = %v", err)
	}
	if retainedOperationCalls != 0 {
		t.Fatalf("retained operation calls = %d, want zero", retainedOperationCalls)
	}

	coordinatorDeadline := migrationCoordinatorFunc(func(context.Context, string, func(context.Context) error) error {
		return context.DeadlineExceeded
	})
	if _, err := coordinateMigration(t.Context(), coordinatorDeadline, "migration", func(context.Context) (int, error) {
		return 1, nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("coordinateMigration(pre-callback deadline) error = %v", err)
	}

	twice := migrationCoordinatorFunc(func(ctx context.Context, _ string, operation func(context.Context) error) error {
		if err := operation(ctx); err != nil {
			return err
		}
		return operation(ctx)
	})
	if _, err := coordinateMigration(t.Context(), twice, "migration", func(context.Context) (int, error) { return 1, nil }); !errors.Is(err, ErrMigrationCoordination) {
		t.Fatalf("coordinateMigration(twice) error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	operationReturned := make(chan error, 1)
	earlyReturn := migrationCoordinatorFunc(func(ctx context.Context, _ string, operation func(context.Context) error) error {
		go func() { operationReturned <- operation(ctx) }()
		<-entered
		return nil
	})
	earlyResult, earlyErr := coordinateMigration(t.Context(), earlyReturn, "migration", func(context.Context) (int, error) {
		close(entered)
		<-release
		return 11, nil
	})
	close(release)
	if callbackErr := <-operationReturned; !errors.Is(callbackErr, ErrMigrationCoordination) {
		t.Fatalf("early-return callback error = %v, want ErrMigrationCoordination", callbackErr)
	}
	if earlyResult != 0 || !errors.Is(earlyErr, ErrMigrationCoordination) {
		t.Fatalf("coordinateMigration(early return) = %d/%v", earlyResult, earlyErr)
	}

	operationErr := errors.New("operation")
	coordinationErr := errors.New("coordination")
	failed := migrationCoordinatorFunc(func(ctx context.Context, _ string, operation func(context.Context) error) error {
		if err := operation(ctx); !errors.Is(err, operationErr) {
			t.Fatalf("coordinated operation error = %v, want operation error", err)
		}
		return coordinationErr
	})
	result, err := coordinateMigration(t.Context(), failed, "migration", func(context.Context) (int, error) {
		return 7, operationErr
	})
	if result != 7 || !errors.Is(err, coordinationErr) || errors.Is(err, operationErr) {
		t.Fatalf("coordinateMigration(coordination failure) = %d/%v", result, err)
	}
}

type migrationCoordinatorFunc func(context.Context, string, func(context.Context) error) error

func (coordinate migrationCoordinatorFunc) WithMigration(ctx context.Context, id string, operation func(context.Context) error) error {
	return coordinate(ctx, id, operation)
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
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationRolledBack}
	if _, err := migrator.Rollback(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationPending}
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, ErrInvalidMigrationPhase) {
		t.Fatal(err)
	}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationComplete}
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
	backend.cutoverErr = errLifecycleBranch
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	backend.cutoverErr = nil
	backend.verification = VerificationReport{}
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, ErrMigrationVerification) {
		t.Fatal(err)
	}
	backend.verification = VerificationReport{Verified: true}
	backend.alias = plan.SourceIndex
	store.saveErr = errLifecycleBranch
	if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, ErrAliasChanged) {
		t.Fatal(err)
	}

	store.saveErr = nil
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationCleaned}
	if _, err := migrator.Cleanup(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationPending}
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, ErrInvalidMigrationPhase) {
		t.Fatal(err)
	}
	for _, phase := range []MigrationPhase{MigrationComplete, MigrationRolledBack} {
		store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: phase}
		if _, err := newBranchMigrator(backend, store, errLifecycleBranch, nil).Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
			t.Fatal(err)
		}
	}
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationComplete}
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
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationComplete}
	store.saveErr = errLifecycleBranch
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, errLifecycleBranch) {
		t.Fatal(err)
	}
	store.saveErr = nil
	store.state = MigrationState{ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationComplete}
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

func TestMigrationAcceptsExactReindexCursorBoundaries(t *testing.T) {
	plan, fingerprint := lifecycleBranchPlan(t)
	maximumCursor := strings.Repeat("c", DefaultLimits().MaxQueryBytes)

	t.Run("persisted cursor", func(t *testing.T) {
		backend := &branchLifecycleBackend{
			alias: plan.SourceIndex, reindexDone: true,
			verification: VerificationReport{Verified: true},
		}
		store := &branchMigrationStore{state: MigrationState{
			ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationReindexing,
			ReindexCursor: maximumCursor,
		}}

		state, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan)
		if err != nil || state.Phase != MigrationComplete {
			t.Fatalf("Run() state/error = %#v/%v", state, err)
		}
	})

	t.Run("returned cursor", func(t *testing.T) {
		backend := &branchLifecycleBackend{reindexCursor: maximumCursor}
		store := &branchMigrationStore{state: MigrationState{
			ID: plan.ID, PlanFingerprint: fingerprint, Phase: MigrationCreated,
		}}

		state, err := newBranchMigrator(backend, store, nil, nil).Run(t.Context(), plan)
		if !errors.Is(err, ErrMigrationIncomplete) || state.Phase != MigrationReindexing || state.ReindexCursor != maximumCursor {
			t.Fatalf("Run() state/error = %#v/%v", state, err)
		}
	})
}
