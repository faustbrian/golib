package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func TestMigrationIsAuthorizedObservableResumableAndAtomicallyCutOver(t *testing.T) {
	t.Parallel()

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
	plan := search.MigrationPlan{ID: "migration-1", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1", Target: definition, MaxReindexSteps: 3}

	state, err := migrator.Run(t.Context(), plan)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if state.Phase != search.MigrationComplete || backend.alias != "events-v2" || backend.createCalls != 1 || backend.reindexCalls != 2 || backend.verifyCalls != 1 || backend.swapCalls != 1 {
		t.Fatalf("migration state/backend = %#v/%#v", state, backend)
	}
	if len(authorizer.operations) != 5 || len(observer.events) != 5 {
		t.Fatalf("authorization/events = %v/%v", authorizer.operations, observer.events)
	}

	state, err = migrator.Run(t.Context(), plan)
	if err != nil {
		t.Fatalf("resumed Run() error = %v", err)
	}
	if state.Phase != search.MigrationComplete || backend.createCalls != 1 || backend.swapCalls != 1 {
		t.Fatalf("completed migration repeated side effects: %#v", backend)
	}

	state, err = migrator.Rollback(t.Context(), plan)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if state.Phase != search.MigrationRolledBack || backend.alias != "events-v1" || backend.swapCalls != 2 {
		t.Fatalf("rollback = %#v/%#v", state, backend)
	}
	state, err = migrator.Cleanup(t.Context(), plan)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if state.Phase != search.MigrationCleaned || fmt.Sprint(backend.deleted) != "[events-v2]" {
		t.Fatalf("cleanup = %#v/%v", state, backend.deleted)
	}
}

func TestMigrationCheckpointsEveryReindexStepAndRejectsPlanDrift(t *testing.T) {
	t.Parallel()

	definition, _ := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	backend := &lifecycleBackend{alias: "events-v1", reindexSteps: 4}
	store := &migrationStore{}
	migrator, _ := search.NewMigrator(backend, store, &lifecycleAuthorizer{}, &lifecycleObserver{})
	plan := search.MigrationPlan{ID: "migration-2", Tenant: "tenant-a", Alias: "events-read", SourceIndex: "events-v1", Target: definition, MaxReindexSteps: 2}

	state, err := migrator.Run(t.Context(), plan)
	if !errors.Is(err, search.ErrMigrationIncomplete) || state.Phase != search.MigrationReindexing || state.ReindexCursor != "step-2" || store.saves < 3 {
		t.Fatalf("Run() state/error/saves = %#v/%v/%d", state, err, store.saves)
	}
	plan.SourceIndex = "different-source"
	if _, err := migrator.Run(t.Context(), plan); !errors.Is(err, search.ErrMigrationPlanChanged) {
		t.Fatalf("changed Run() error = %v", err)
	}
}

type lifecycleBackend struct {
	alias                                             string
	reindexSteps                                      int
	createCalls, reindexCalls, verifyCalls, swapCalls int
	deleted                                           []string
}

func (b *lifecycleBackend) CreateIndex(context.Context, string, search.IndexDefinition) error {
	b.createCalls++
	return nil
}
func (b *lifecycleBackend) Reindex(_ context.Context, _ string, _, _ string, _ string) (string, bool, error) {
	b.reindexCalls++
	return fmt.Sprintf("step-%d", b.reindexCalls), b.reindexCalls >= b.reindexSteps, nil
}
func (b *lifecycleBackend) VerifyIndex(context.Context, string, string, string) (search.VerificationReport, error) {
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
func (b *lifecycleBackend) DeleteIndex(_ context.Context, _ string, index string) error {
	b.deleted = append(b.deleted, index)
	return nil
}

type migrationStore struct {
	state search.MigrationState
	found bool
	saves int
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
