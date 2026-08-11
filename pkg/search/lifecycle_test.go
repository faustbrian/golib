package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func TestMigrationIsAuthorizedObservableResumableAndAtomicallyCutOver(t *testing.T) {
	t.Parallel()

	sourceDefinition, err := search.NewIndexDefinition("events-v1", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{"properties":{}}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	backend := &lifecycleBackend{alias: "events-v1", reindexSteps: 2}
	store := &migrationStore{}
	authorizer := &lifecycleAuthorizer{}
	observer := &lifecycleObserver{}
	migrator, err := search.NewMigrator(backend, store, authorizer, observer)
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{ID: "migration-1", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1", SourceFingerprint: sourceDefinition.Fingerprint(), Target: definition, MaxReindexSteps: 3}

	state, err := migrator.Run(t.Context(), plan)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if state.Phase != search.MigrationComplete || backend.alias != "events-v2" || backend.createCalls != 1 || backend.reindexCalls != 2 || backend.verifyCalls != 1 || backend.cutoverCalls != 1 || backend.swapCalls != 0 {
		t.Fatalf("migration state/backend = %#v/%#v", state, backend)
	}
	if len(authorizer.operations) != 5 || len(observer.events) != 7 {
		t.Fatalf("authorization/events = %v/%v", authorizer.operations, observer.events)
	}

	state, err = migrator.Run(t.Context(), plan)
	if err != nil {
		t.Fatalf("resumed Run() error = %v", err)
	}
	if state.Phase != search.MigrationComplete || backend.createCalls != 1 || backend.cutoverCalls != 1 || backend.swapCalls != 0 {
		t.Fatalf("completed migration repeated side effects: %#v", backend)
	}

	state, err = migrator.Rollback(t.Context(), plan)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if state.Phase != search.MigrationRolledBack || backend.alias != "events-v1" || backend.cutoverCalls != 2 || backend.swapCalls != 0 {
		t.Fatalf("rollback = %#v/%#v", state, backend)
	}
	state, err = migrator.Cleanup(t.Context(), plan)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	wantCleanup := search.LifecycleCleanupRequest{
		MigrationID: plan.ID, Tenant: plan.Tenant, Alias: plan.Alias,
		ActiveIndex: plan.SourceIndex, ActiveFingerprint: plan.SourceFingerprint,
		InactiveIndex: plan.Target.Name(), InactiveFingerprint: plan.Target.Fingerprint(),
	}
	if state.Phase != search.MigrationCleaned || fmt.Sprint(backend.deleted) != "[events-v2]" ||
		len(backend.cleanupRequests) != 1 || backend.cleanupRequests[0] != wantCleanup {
		t.Fatalf("cleanup = %#v/%v/%#v", state, backend.deleted, backend.cleanupRequests)
	}
}

func TestMigrationCheckpointsEveryReindexStepAndRejectsPlanDrift(t *testing.T) {
	t.Parallel()

	definition, _ := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	backend := &lifecycleBackend{alias: "events-v1", reindexSteps: 4}
	store := &migrationStore{}
	migrator, _ := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	plan := search.MigrationPlan{ID: "migration-2", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 2}

	state, err := migrator.Run(t.Context(), plan)
	if !errors.Is(err, search.ErrMigrationIncomplete) || state.Phase != search.MigrationReindexing || state.ReindexCursor != "step-2" || store.saves < 3 {
		t.Fatalf("Run() state/error/saves = %#v/%v/%d", state, err, store.saves)
	}
	plan.SourceIndex = "different-source"
	if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, search.ErrMigrationPlanChanged) {
		t.Fatalf("changed Run() error = %v", err)
	}
}

func TestMigratorCoordinatesEveryWorkflowAcrossTheCompleteExternalOperation(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	store := &coordinationAssertingStore{}
	backend := &lifecycleBackend{alias: "events-v1", reindexSteps: 1}
	migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{ID: "coordinated", Tenant: "tenant", Alias: "events-read", SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1}
	if _, err := migrator.Run(t.Context(), plan); err != nil {
		t.Fatalf("Run() escaped coordination: %v", err)
	}
	if _, err := migrator.Rollback(t.Context(), plan); err != nil {
		t.Fatalf("Rollback() escaped coordination: %v", err)
	}
	if _, err := migrator.Cleanup(t.Context(), plan); err != nil {
		t.Fatalf("Cleanup() escaped coordination: %v", err)
	}
	if store.calls != 3 {
		t.Fatalf("coordination calls = %d, want 3", store.calls)
	}
}

func TestMigrationRejectsUnknownPersistedPhase(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-corrupt", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1",
		SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1,
	}
	store := &migrationStore{}
	migrator, err := search.NewMigrator(
		&lifecycleBackend{alias: plan.SourceIndex, reindexSteps: 1}, store,
		&lifecycleAuthorizer{}, &lifecycleObserver{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Run(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	store.state.Phase = search.MigrationPhase("corrupt")

	state, err := migrator.Run(t.Context(), plan)
	if !errors.Is(err, search.ErrInvalidMigrationPhase) || state.Phase != search.MigrationPhase("corrupt") {
		t.Fatalf("Run() state/error = %#v/%v", state, err)
	}
}

func TestMigrationRejectsMisattributedPersistedState(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-bound", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1",
		SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1,
	}
	store := &migrationStore{}
	migrator, err := search.NewMigrator(
		&lifecycleBackend{alias: plan.SourceIndex, reindexSteps: 1}, store,
		&lifecycleAuthorizer{}, &lifecycleObserver{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Run(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	store.state.ID = "other-migration"

	state, err := migrator.Run(t.Context(), plan)
	if !errors.Is(err, search.ErrMigrationPlanChanged) || state.ID != "other-migration" {
		t.Fatalf("Run() state/error = %#v/%v", state, err)
	}
}

func TestMigrationRejectsConflictingLifecycleNames(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	base := search.MigrationPlan{ID: "migration-conflict", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1}
	for name, mutate := range map[string]func(*search.MigrationPlan){
		"source equals target": func(plan *search.MigrationPlan) { plan.SourceIndex = plan.Target.Name() },
		"alias equals source":  func(plan *search.MigrationPlan) { plan.Alias = plan.SourceIndex },
		"alias equals target":  func(plan *search.MigrationPlan) { plan.Alias = plan.Target.Name() },
	} {
		t.Run(name, func(t *testing.T) {
			plan := base
			mutate(&plan)
			migrator, err := search.NewMigrator(&lifecycleBackend{alias: plan.SourceIndex, reindexSteps: 1}, &migrationStore{}, &lifecycleAuthorizer{}, &lifecycleObserver{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, search.ErrInvalidMigrationPlan) {
				t.Fatalf("Run() error = %v, want ErrInvalidMigrationPlan", err)
			}
		})
	}
}

func TestMigrationRejectsUnboundedIdentifiersAndTraversal(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	base := search.MigrationPlan{ID: "migration", Tenant: "tenant", Alias: "events-read", SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1}
	for name, mutate := range map[string]func(*search.MigrationPlan){
		"migration id":         func(plan *search.MigrationPlan) { plan.ID = strings.Repeat("m", limits.MaxIDBytes+1) },
		"tenant":               func(plan *search.MigrationPlan) { plan.Tenant = strings.Repeat("t", limits.MaxTenantBytes+1) },
		"invalid UTF-8 id":     func(plan *search.MigrationPlan) { plan.ID = string([]byte{0xff}) },
		"invalid UTF-8 tenant": func(plan *search.MigrationPlan) { plan.Tenant = string([]byte{0xff}) },
		"steps":                func(plan *search.MigrationPlan) { plan.MaxReindexSteps = limits.MaxPages + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			plan := base
			mutate(&plan)
			migrator, createErr := search.NewMigrator(&lifecycleBackend{}, &migrationStore{}, &lifecycleAuthorizer{}, &lifecycleObserver{})
			if createErr != nil {
				t.Fatal(createErr)
			}
			if _, runErr := migrator.Run(t.Context(), plan); !errors.Is(runErr, search.ErrInvalidMigrationPlan) {
				t.Fatalf("Run() error = %v, want ErrInvalidMigrationPlan", runErr)
			}
		})
	}
}

func TestMigrationCleanupRefusesToDeleteTheActiveIndex(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	backend := &lifecycleBackend{alias: "events-v1", reindexSteps: 1}
	store := &migrationStore{}
	migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{ID: "migration-cleanup", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1}
	if _, err := migrator.Run(t.Context(), plan); err != nil {
		t.Fatal(err)
	}

	backend.alias = plan.SourceIndex
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, search.ErrAliasChanged) {
		t.Fatalf("Cleanup() error = %v, want ErrAliasChanged", err)
	}
	if len(backend.deleted) != 0 {
		t.Fatalf("Cleanup() deleted active indexes: %v", backend.deleted)
	}
}

func TestMigrationCutoverAndRollbackUseVerifiedBackendFence(t *testing.T) {
	t.Parallel()

	source, err := search.NewIndexDefinition("events-v1", json.RawMessage(`{}`), json.RawMessage(`{"properties":{}}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	target, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{"properties":{"status":{"type":"keyword"}}}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	backend := &lifecycleBackend{alias: source.Name(), reindexSteps: 1}
	migrator, err := search.NewMigrator(backend, &migrationStore{}, &lifecycleAuthorizer{}, &lifecycleObserver{})
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-fenced", Tenant: "tenant", Alias: "events-read",
		SourceIndex: source.Name(), SourceFingerprint: source.Fingerprint(), Target: target, MaxReindexSteps: 1,
	}
	if _, err := migrator.Run(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Rollback(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	want := []string{target.Fingerprint(), source.Fingerprint()}
	if fmt.Sprint(backend.cutoverFingerprints) != fmt.Sprint(want) || backend.swapCalls != 0 {
		t.Fatalf("verified cutovers/raw swaps = %v/%d, want %v/0", backend.cutoverFingerprints, backend.swapCalls, want)
	}
	state, err := migrator.Cleanup(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	wantCleanup := search.LifecycleCleanupRequest{
		MigrationID: plan.ID, Tenant: plan.Tenant, Alias: plan.Alias,
		ActiveIndex: source.Name(), ActiveFingerprint: source.Fingerprint(),
		InactiveIndex: target.Name(), InactiveFingerprint: target.Fingerprint(),
	}
	if state.Phase != search.MigrationCleaned || len(backend.cleanupRequests) != 1 || backend.cleanupRequests[0] != wantCleanup {
		t.Fatalf("rolled-back cleanup = %#v/%#v, want %#v", state, backend.cleanupRequests, wantCleanup)
	}
}

func TestMigrationRefusesToInferUncheckpointedCutoverOrRollback(t *testing.T) {
	t.Parallel()

	source, err := search.NewIndexDefinition("events-v1", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	target, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{"properties":{"status":{"type":"keyword"}}}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-uncheckpointed", Tenant: "tenant", Alias: "events-read",
		SourceIndex: source.Name(), SourceFingerprint: source.Fingerprint(), Target: target, MaxReindexSteps: 1,
	}

	t.Run("cutover", func(t *testing.T) {
		backend := &lifecycleBackend{alias: source.Name(), reindexSteps: 1}
		store := &phaseFailingMigrationStore{failPhase: search.MigrationComplete}
		migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, errMigrationCheckpoint) || backend.alias != target.Name() {
			t.Fatalf("first Run() alias/error = %q/%v", backend.alias, err)
		}
		store.failPhase = ""
		if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, search.ErrAliasChanged) {
			t.Fatalf("resumed Run() error = %v, want ErrAliasChanged", err)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		backend := &lifecycleBackend{alias: source.Name(), reindexSteps: 1}
		store := &phaseFailingMigrationStore{}
		migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrator.Run(t.Context(), plan); err != nil {
			t.Fatal(err)
		}
		store.failPhase = search.MigrationRolledBack
		if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, errMigrationCheckpoint) || backend.alias != source.Name() {
			t.Fatalf("first Rollback() alias/error = %q/%v", backend.alias, err)
		}
		store.failPhase = ""
		if _, err := migrator.Rollback(t.Context(), plan); !errors.Is(err, search.ErrAliasChanged) {
			t.Fatalf("resumed Rollback() error = %v, want ErrAliasChanged", err)
		}
	})
}

func TestMigrationDoesNotRepeatReindexAfterProgressCheckpointFailure(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-reindex-checkpoint", Tenant: "tenant", Alias: "events-read",
		SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1,
	}
	backend := &lifecycleBackend{alias: plan.SourceIndex, reindexSteps: 2}
	store := &phaseFailingMigrationStore{failPhase: search.MigrationReindexing}
	migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, errMigrationCheckpoint) || backend.reindexCalls != 1 {
		t.Fatalf("first Run() error/reindex calls = %v/%d", err, backend.reindexCalls)
	}
	store.failPhase = ""
	if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, search.ErrMigrationRecovery) || backend.reindexCalls != 1 {
		t.Fatalf("resumed Run() error/reindex calls = %v/%d, want fail-closed without repeat", err, backend.reindexCalls)
	}
}

func TestMigrationDoesNotRepeatCreateAfterCheckpointFailure(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-create-checkpoint", Tenant: "tenant", Alias: "events-read",
		SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1,
	}
	backend := &lifecycleBackend{alias: plan.SourceIndex, reindexSteps: 1}
	store := &phaseFailingMigrationStore{failPhase: search.MigrationCreated}
	migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, errMigrationCheckpoint) || backend.createCalls != 1 {
		t.Fatalf("first Run() error/create calls = %v/%d", err, backend.createCalls)
	}
	store.failPhase = ""
	if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, search.ErrMigrationRecovery) || backend.createCalls != 1 {
		t.Fatalf("resumed Run() error/create calls = %v/%d, want fail-closed without repeat", err, backend.createCalls)
	}
}

func TestMigrationDoesNotRepeatCleanupAfterCheckpointFailure(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-cleanup-checkpoint", Tenant: "tenant", Alias: "events-read",
		SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1,
	}
	backend := &lifecycleBackend{alias: plan.SourceIndex, reindexSteps: 1}
	store := &phaseFailingMigrationStore{}
	migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Run(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	store.failPhase = search.MigrationCleaned
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, errMigrationCheckpoint) || len(backend.deleted) != 1 {
		t.Fatalf("first Cleanup() error/deletes = %v/%v", err, backend.deleted)
	}
	store.failPhase = ""
	if _, err := migrator.Cleanup(t.Context(), plan); !errors.Is(err, search.ErrMigrationRecovery) || len(backend.deleted) != 1 {
		t.Fatalf("resumed Cleanup() error/deletes = %v/%v, want fail-closed without repeat", err, backend.deleted)
	}
}

func TestMigrationFailsClosedWhenReindexDispatchReturnsNoResumableCursor(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan := search.MigrationPlan{
		ID: "migration-cursor-loss", Tenant: "tenant", Alias: "events-read",
		SourceIndex: "events-v1", SourceFingerprint: definition.Fingerprint(), Target: definition, MaxReindexSteps: 1,
	}
	backend := &invalidCursorLifecycleBackend{lifecycleBackend: lifecycleBackend{alias: plan.SourceIndex}}
	store := &migrationStore{}
	migrator, err := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	if err != nil {
		t.Fatal(err)
	}

	state, err := migrator.Run(t.Context(), plan)
	if !errors.Is(err, search.ErrMigrationRecovery) || state.Phase != search.MigrationDispatching ||
		store.state.Phase != search.MigrationDispatching || backend.reindexCalls != 1 {
		t.Fatalf("Run() state/error/store/calls = %#v/%v/%#v/%d", state, err, store.state, backend.reindexCalls)
	}
}

var errMigrationCheckpoint = errors.New("migration checkpoint failed")

type phaseFailingMigrationStore struct {
	migrationStore
	failPhase search.MigrationPhase
}

func (s *phaseFailingMigrationStore) Save(ctx context.Context, state search.MigrationState) error {
	if state.Phase == s.failPhase {
		return errMigrationCheckpoint
	}
	return s.migrationStore.Save(ctx, state)
}

type lifecycleBackend struct {
	alias                                                           string
	reindexSteps                                                    int
	createCalls, reindexCalls, verifyCalls, cutoverCalls, swapCalls int
	deleted                                                         []string
	cleanupRequests                                                 []search.LifecycleCleanupRequest
	cutoverFingerprints                                             []string
}

type invalidCursorLifecycleBackend struct{ lifecycleBackend }

func (b *invalidCursorLifecycleBackend) Reindex(context.Context, string, string, string, string) (string, bool, error) {
	b.reindexCalls++
	return "", false, nil
}

func (b *lifecycleBackend) CreateIndex(context.Context, string, search.IndexDefinition) error {
	b.createCalls++
	return nil
}
func (b *lifecycleBackend) Reindex(_ context.Context, _ string, _, _ string, _ string) (string, bool, error) {
	b.reindexCalls++
	return fmt.Sprintf("step-%d", b.reindexCalls), b.reindexCalls >= b.reindexSteps, nil
}
func (b *lifecycleBackend) VerifyIndex(context.Context, string, string, string, string) (search.VerificationReport, error) {
	b.verifyCalls++
	return search.VerificationReport{Verified: true, SourceCount: 10, TargetCount: 10}, nil
}
func (b *lifecycleBackend) ResolveAlias(context.Context, string, string) (string, error) {
	return b.alias, nil
}
func (b *lifecycleBackend) SwapAlias(_ context.Context, _ string, _ string, from, to string) error {
	if b.alias != from {
		return errors.New("alias source mismatch")
	}
	b.alias = to
	b.swapCalls++
	return nil
}
func (b *lifecycleBackend) CutoverAlias(_ context.Context, _ string, _ string, from, to, fingerprint string) (search.VerificationReport, error) {
	if b.alias != from {
		return search.VerificationReport{}, errors.New("alias source mismatch")
	}
	b.alias = to
	b.cutoverCalls++
	b.cutoverFingerprints = append(b.cutoverFingerprints, fingerprint)
	return search.VerificationReport{Verified: true}, nil
}
func (b *lifecycleBackend) CleanupIndex(_ context.Context, request search.LifecycleCleanupRequest) error {
	b.deleted = append(b.deleted, request.InactiveIndex)
	b.cleanupRequests = append(b.cleanupRequests, request)
	return nil
}

type migrationStore struct {
	coordination sync.Mutex
	state        search.MigrationState
	found        bool
	saves        int
}

type coordinationAssertingStore struct {
	state  search.MigrationState
	found  bool
	active bool
	calls  int
}

func (s *coordinationAssertingStore) WithMigration(ctx context.Context, _ string, operation func(context.Context) error) error {
	if s.active {
		return errors.New("nested migration coordination")
	}
	s.active = true
	s.calls++
	defer func() { s.active = false }()
	return operation(ctx)
}

func (s *coordinationAssertingStore) Load(context.Context, string) (search.MigrationState, error) {
	if !s.active {
		return search.MigrationState{}, errors.New("migration load outside coordination")
	}
	if !s.found {
		return search.MigrationState{}, search.ErrMigrationNotFound
	}
	return s.state, nil
}

func (s *coordinationAssertingStore) Save(_ context.Context, state search.MigrationState) error {
	if !s.active {
		return errors.New("migration save outside coordination")
	}
	s.state, s.found = state, true
	return nil
}

func (s *migrationStore) WithMigration(ctx context.Context, _ string, operation func(context.Context) error) error {
	s.coordination.Lock()
	defer s.coordination.Unlock()
	return operation(ctx)
}

func (s *migrationStore) Load(context.Context, string) (search.MigrationState, error) {
	if !s.found {
		return search.MigrationState{}, search.ErrMigrationNotFound
	}
	return s.state, nil
}
func (s *migrationStore) Save(_ context.Context, state search.MigrationState) error {
	s.state = state
	s.found = true
	s.saves++
	return nil
}

type lifecycleAuthorizer struct{ operations []search.LifecycleOperation }

func (a *lifecycleAuthorizer) Authorize(_ context.Context, intent search.LifecycleIntent) error {
	a.operations = append(a.operations, intent.Operation)
	return nil
}

type lifecycleObserver struct{ events []search.LifecycleEvent }

func (o *lifecycleObserver) Record(_ context.Context, event search.LifecycleEvent) error {
	o.events = append(o.events, event)
	return nil
}
