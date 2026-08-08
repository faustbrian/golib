package discovery_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/discovery"
)

func TestCacheDeduplicatesConcurrentDiscoveryAndInvalidatesExplicitly(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { closeSignal(release) })
	provider := discovery.ProviderFunc(func(ctx context.Context) (openrpc.Document, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
			return testDocument(t, "cached"), nil
		case <-ctx.Done():
			return openrpc.Document{}, ctx.Err()
		}
	})
	service, err := discovery.NewService(provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := discovery.NewCache(service)
	if err != nil {
		t.Fatal(err)
	}

	const callers = 8
	results := make(chan discovery.Snapshot, callers)
	errors := make(chan error, callers)
	var group sync.WaitGroup
	group.Add(callers)
	for range callers {
		go func() {
			defer group.Done()
			snapshot, discoverErr := cache.Discover(context.Background())
			results <- snapshot
			errors <- discoverErr
		}()
	}
	awaitSignal(t, entered, "provider entry")
	closeSignal(release)
	groupDone := make(chan struct{})
	go func() {
		group.Wait()
		close(groupDone)
	}()
	awaitSignal(t, groupDone, "concurrent callers")
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var revision string
	for snapshot := range results {
		if revision == "" {
			revision = snapshot.Revision()
		}
		if snapshot.Revision() != revision {
			t.Fatalf("revision = %q, want %q", snapshot.Revision(), revision)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d", calls.Load())
	}

	cache.Invalidate()
	if _, err := cache.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls after invalidation = %d", calls.Load())
	}
}

func TestCacheWaiterCanCancelWithoutCancelingSharedDiscovery(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { closeSignal(release) })
	service, err := discovery.NewService(discovery.ProviderFunc(func(ctx context.Context) (openrpc.Document, error) {
		close(entered)
		select {
		case <-release:
			return testDocument(t, "cached"), nil
		case <-ctx.Done():
			return openrpc.Document{}, ctx.Err()
		}
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := discovery.NewCache(service)
	if err != nil {
		t.Fatal(err)
	}
	leaderDone := make(chan error, 1)
	go func() {
		_, discoverErr := cache.Discover(context.Background())
		leaderDone <- discoverErr
	}()
	awaitSignal(t, entered, "leader provider entry")
	baseContext, cancel := context.WithCancel(context.Background())
	ctx := &checkedContext{Context: baseContext, checked: make(chan struct{})}
	waiterDone := make(chan error, 1)
	go func() {
		_, discoverErr := cache.Discover(ctx)
		waiterDone <- discoverErr
	}()
	awaitSignal(t, ctx.checked, "waiter context check")
	cancel()
	if err := awaitError(t, waiterDone, "canceled waiter"); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v", err)
	}
	closeSignal(release)
	if err := awaitError(t, leaderDone, "leader completion"); err != nil {
		t.Fatal(err)
	}
}

func TestCacheWaiterUsesCompletedSharedDiscovery(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { closeSignal(release) })
	var calls atomic.Int64
	service, err := discovery.NewService(discovery.ProviderFunc(func(context.Context) (openrpc.Document, error) {
		calls.Add(1)
		close(entered)
		<-release
		return testDocument(t, "shared"), nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := discovery.NewCache(service)
	if err != nil {
		t.Fatal(err)
	}

	leaderDone := make(chan error, 1)
	go func() {
		_, discoverErr := cache.Discover(context.Background())
		leaderDone <- discoverErr
	}()
	awaitSignal(t, entered, "leader provider entry")

	waiterContext := &doneObservedContext{
		Context:  context.Background(),
		observed: make(chan struct{}),
	}
	waiterDone := make(chan error, 1)
	go func() {
		_, discoverErr := cache.Discover(waiterContext)
		waiterDone <- discoverErr
	}()
	awaitSignal(t, waiterContext.observed, "waiter completion observation")
	closeSignal(release)

	if err := awaitError(t, leaderDone, "leader completion"); err != nil {
		t.Fatal(err)
	}
	if err := awaitError(t, waiterDone, "waiter completion"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
}

type checkedContext struct {
	context.Context
	checked chan struct{}
}

type doneObservedContext struct {
	context.Context
	observed chan struct{}
}

func (ctx *doneObservedContext) Done() <-chan struct{} {
	select {
	case <-ctx.observed:
	default:
		close(ctx.observed)
	}
	return ctx.Context.Done()
}

func (ctx *checkedContext) Err() error {
	select {
	case <-ctx.checked:
	default:
		close(ctx.checked)
	}
	return ctx.Context.Err()
}

func TestNewCacheRequiresDiscoverer(t *testing.T) {
	t.Parallel()

	if _, err := discovery.NewCache(nil); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("NewCache error = %v", err)
	}
}

func TestCacheRejectsInvalidStateAndRetriesFailures(t *testing.T) {
	t.Parallel()

	var nilCache *discovery.Cache
	if _, err := nilCache.Discover(context.Background()); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("nil cache error = %v", err)
	}
	nilCache.Invalidate()
	zeroCache := &discovery.Cache{}
	if _, err := zeroCache.Discover(context.Background()); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("zero cache error = %v", err)
	}

	var calls atomic.Int64
	service, err := discovery.NewService(discovery.ProviderFunc(func(context.Context) (openrpc.Document, error) {
		if calls.Add(1) == 1 {
			return openrpc.Document{}, errors.New("transient provider detail")
		}
		return testDocument(t, "retried"), nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := discovery.NewCache(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Discover(context.Background()); err == nil {
		t.Fatal("failed refresh succeeded")
	}
	if _, err := cache.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Discover(canceledContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	var invalidContext context.Context
	if _, err := cache.Discover(invalidContext); !errors.Is(err, discovery.ErrInvalidOptions) {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestCacheDoesNotPublishRefreshInvalidatedInFlight(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { closeSignal(release) })
	var calls atomic.Int64
	service, err := discovery.NewService(discovery.ProviderFunc(func(context.Context) (openrpc.Document, error) {
		call := calls.Add(1)
		if call == 1 {
			close(entered)
			<-release
		}
		return testDocument(t, "generation"), nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := discovery.NewCache(service)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, discoverErr := cache.Discover(context.Background())
		done <- discoverErr
	}()
	awaitSignal(t, entered, "invalidated provider entry")
	cache.Invalidate()
	closeSignal(release)
	if err := awaitError(t, done, "invalidated refresh completion"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want 2", calls.Load())
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func awaitError(t *testing.T, result <-chan error, operation string) error {
	t.Helper()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}

func closeSignal(signal chan struct{}) {
	select {
	case <-signal:
	default:
		close(signal)
	}
}
