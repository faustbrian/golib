//go:build integration && (darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	official "github.com/opensearch-project/opensearch-go/v4"
)

func TestRealOpenSearchConformancePersistsAndResumesMigratorLifecycle(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL is invalid")
	}
	limits := search.DefaultLimits()
	suffix := time.Now().UTC().Format("150405000000000")
	tenant := "durable-migration-tenant"
	alias := "golib-search-durable-migration-" + suffix
	sourceName := alias + "-v1"
	targetName := alias + "-v2"
	settings := json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`)
	source, err := search.NewIndexDefinition(sourceName, settings,
		json.RawMessage(`{"dynamic":"strict","properties":{"value":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	target, err := search.NewIndexDefinition(targetName, settings,
		json.RawMessage(`{"dynamic":"strict","properties":{"value":{"type":"keyword"},"added":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}

	direct, err := official.NewClient(official.Config{Addresses: []string{endpoint}, DisableRetry: true, HealthCheckMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	verifier := &realLifecycleVerifier{
		client: direct, pageSize: 16, maximumRecords: 128, maximumResponseBytes: 1 << 20,
		expectedDefinitions: map[string]search.IndexDefinition{
			source.Fingerprint(): source,
			target.Fingerprint(): target,
		},
	}
	var plan search.MigrationPlan
	var cleanupEntered chan struct{}
	var cleanupRelease chan struct{}
	mutationGuardPath := filepath.Join(t.TempDir(), "lifecycle-mutation.lock")
	newBackend := func() *adapter.Client {
		client, createErr := adapter.New(adapter.Config{
			Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
			RequestTimeout: 30 * time.Second, MaximumResponseBytes: 16 << 20,
			Lifecycle: &adapter.LifecycleConfig{
				Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
					if gotTenant != tenant || len(resources) == 0 {
						return errors.New("durable migration lifecycle denied")
					}
					for _, resource := range resources {
						if resource != alias && resource != sourceName && resource != targetName {
							return errors.New("durable migration lifecycle denied")
						}
					}
					return nil
				}),
				Verifier:      verifier,
				MutationGuard: &fileLifecycleMutationGuard{path: mutationGuardPath},
				CutoverGuard: adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
					return operation()
				}),
				CleanupGuard: adapter.LifecycleCleanupGuardFunc(func(ctx context.Context, request search.LifecycleCleanupRequest, operation func() error) error {
					expected := search.LifecycleCleanupRequest{
						MigrationID: plan.ID, Tenant: tenant, Alias: alias,
						ActiveIndex: sourceName, ActiveFingerprint: source.Fingerprint(),
						InactiveIndex: targetName, InactiveFingerprint: target.Fingerprint(),
					}
					if request != expected {
						return errors.New("durable migration cleanup identity changed")
					}
					if cleanupEntered != nil {
						close(cleanupEntered)
						select {
						case <-cleanupRelease:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return operation()
				}),
				ReindexCursorCodec: mustIntegrationReindexCursorCodec(t),
			},
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return client
	}

	bootstrap := newBackend()
	t.Cleanup(func() { _ = bootstrap.Close() })
	if err := bootstrap.CreateIndex(t.Context(), tenant, source); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.AddAlias(t.Context(), tenant, alias, sourceName, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deleteDisposableIndex(ctx, endpoint, targetName)
		_ = deleteDisposableIndex(ctx, endpoint, sourceName)
	})
	for _, id := range []string{"a", "b", "c"} {
		requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+alias+"/_doc/"+id+"?version=1&version_type=external&refresh=wait_for&require_alias=true",
			[]byte(`{"value":"`+id+`"}`), http.StatusCreated)
	}

	storePath := filepath.Join(t.TempDir(), "migration-state.json")
	plan = search.MigrationPlan{
		ID: "durable-migration", Tenant: tenant, Alias: alias,
		SourceIndex: sourceName, SourceFingerprint: source.Fingerprint(), Target: target, MaxReindexSteps: 1,
	}
	firstBackend := newBackend()
	firstMigrator, err := search.NewMigrator(firstBackend, newFileMigrationStore(storePath), allowCoreLifecycle{}, discardCoreLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := firstMigrator.Run(t.Context(), plan)
	_ = firstBackend.Close()
	if !errors.Is(err, search.ErrMigrationIncomplete) || state.Phase != search.MigrationReindexing || state.ReindexCursor == "" {
		t.Fatalf("initial persisted migration = %#v/%v", state, err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for state.Phase != search.MigrationComplete {
		if time.Now().After(deadline) {
			t.Fatalf("persisted migration did not complete: %#v", state)
		}
		backend := newBackend()
		migrator, createErr := search.NewMigrator(backend, newFileMigrationStore(storePath), allowCoreLifecycle{}, discardCoreLifecycle{})
		if createErr != nil {
			_ = backend.Close()
			t.Fatal(createErr)
		}
		state, err = migrator.Run(t.Context(), plan)
		_ = backend.Close()
		if err != nil && !errors.Is(err, search.ErrMigrationIncomplete) {
			t.Fatalf("resumed persisted migration = %#v/%v", state, err)
		}
		if state.Phase != search.MigrationComplete {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if resolved, resolveErr := bootstrap.ResolveAlias(t.Context(), tenant, alias); resolveErr != nil || resolved != targetName {
		t.Fatalf("completed persisted alias = %q/%v", resolved, resolveErr)
	}

	rollbackBackend := newBackend()
	rollbackMigrator, err := search.NewMigrator(rollbackBackend, newFileMigrationStore(storePath), allowCoreLifecycle{}, discardCoreLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	state, err = rollbackMigrator.Rollback(t.Context(), plan)
	_ = rollbackBackend.Close()
	if err != nil || state.Phase != search.MigrationRolledBack {
		t.Fatalf("persisted rollback = %#v/%v", state, err)
	}
	cleanupEntered = make(chan struct{})
	cleanupRelease = make(chan struct{})
	cleanupBackend := newBackend()
	cleanupMigrator, err := search.NewMigrator(cleanupBackend, newFileMigrationStore(storePath), allowCoreLifecycle{}, discardCoreLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	type cleanupResult struct {
		state search.MigrationState
		err   error
	}
	cleanupResultChannel := make(chan cleanupResult, 1)
	go func() {
		cleaned, cleanupErr := cleanupMigrator.Cleanup(t.Context(), plan)
		cleanupResultChannel <- cleanupResult{state: cleaned, err: cleanupErr}
	}()
	select {
	case <-cleanupEntered:
	case <-time.After(time.Second):
		t.Fatal("durable cleanup guard was not entered")
	}
	concurrentBackend := newBackend()
	concurrentMigrator, createErr := search.NewMigrator(concurrentBackend, newFileMigrationStore(storePath), allowCoreLifecycle{}, discardCoreLifecycle{})
	if createErr != nil {
		t.Fatal(createErr)
	}
	blockedCtx, cancelBlocked := context.WithTimeout(t.Context(), 50*time.Millisecond)
	_, blockedErr := concurrentMigrator.Cleanup(blockedCtx, plan)
	cancelBlocked()
	_ = concurrentBackend.Close()
	if !errors.Is(blockedErr, context.DeadlineExceeded) {
		t.Fatalf("concurrent cleanup error = %v, want coordinator deadline", blockedErr)
	}
	aliasBackend := newBackend()
	aliasCtx, cancelAlias := context.WithTimeout(t.Context(), 50*time.Millisecond)
	aliasErr := aliasBackend.AddAlias(aliasCtx, tenant, alias, targetName, false)
	cancelAlias()
	_ = aliasBackend.Close()
	if !errors.Is(aliasErr, context.DeadlineExceeded) {
		t.Fatalf("concurrent inactive alias mutation error = %v, want lifecycle mutation deadline", aliasErr)
	}
	close(cleanupRelease)
	cleanup := <-cleanupResultChannel
	state, err = cleanup.state, cleanup.err
	_ = cleanupBackend.Close()
	if err != nil || state.Phase != search.MigrationCleaned {
		t.Fatalf("persisted cleanup = %#v/%v", state, err)
	}
	if resolved, resolveErr := bootstrap.ResolveAlias(t.Context(), tenant, alias); resolveErr != nil || resolved != sourceName {
		t.Fatalf("rolled-back persisted alias = %q/%v", resolved, resolveErr)
	}
}

func TestFileMigrationStoreSerializesIndependentInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	first := newFileMigrationStore(path)
	second := newFileMigrationStore(path)
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- first.WithMigration(t.Context(), "migration", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first file migration coordinator did not acquire its OS lock")
	}
	blockedCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	invoked := false
	if err := second.WithMigration(blockedCtx, "migration", func(context.Context) error {
		invoked = true
		return nil
	}); !errors.Is(err, context.DeadlineExceeded) || invoked {
		t.Fatalf("second file migration coordinator = invoked %t error %v", invoked, err)
	}
	close(release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first file migration coordinator did not release its OS lock")
	}
	if err := second.WithMigration(t.Context(), "migration", func(context.Context) error {
		invoked = true
		return nil
	}); err != nil || !invoked {
		t.Fatalf("released file migration coordinator = invoked %t error %v", invoked, err)
	}
}

type allowCoreLifecycle struct{}

func (allowCoreLifecycle) Authorize(context.Context, search.LifecycleIntent) error { return nil }

type discardCoreLifecycle struct{}

func (discardCoreLifecycle) Record(context.Context, search.LifecycleEvent) error { return nil }

type fileMigrationStore struct {
	path     string
	lockPath string
}

type fileLifecycleMutationGuard struct{ path string }

func (guard *fileLifecycleMutationGuard) WithLifecycleMutation(ctx context.Context, request adapter.LifecycleMutationRequest, operation func() error) error {
	if request.Tenant == "" || request.Operation == "" || len(request.Resources) == 0 || operation == nil {
		return errors.New("invalid lifecycle mutation binding")
	}
	return withExclusiveFileLock(ctx, guard.path, operation)
}

func withExclusiveFileLock(ctx context.Context, path string, operation func() error) error {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	return operation()
}

func newFileMigrationStore(path string) *fileMigrationStore {
	return &fileMigrationStore{path: path, lockPath: path + ".lock"}
}

func (store *fileMigrationStore) WithMigration(ctx context.Context, _ string, operation func(context.Context) error) error {
	return withExclusiveFileLock(ctx, store.lockPath, func() error { return operation(ctx) })
}

func (store *fileMigrationStore) Load(ctx context.Context, _ string) (search.MigrationState, error) {
	if err := ctx.Err(); err != nil {
		return search.MigrationState{}, err
	}
	body, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return search.MigrationState{}, search.ErrMigrationNotFound
	}
	if err != nil {
		return search.MigrationState{}, err
	}
	var state search.MigrationState
	if json.Unmarshal(body, &state) != nil {
		return search.MigrationState{}, errors.New("persisted migration state is malformed")
	}
	return state, nil
}

func (store *fileMigrationStore) Save(ctx context.Context, state search.MigrationState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".migration-state-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, store.path)
}

var _ search.MigrationStore = (*fileMigrationStore)(nil)
var _ search.MigrationCoordinator = (*fileMigrationStore)(nil)
