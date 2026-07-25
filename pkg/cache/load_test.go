package cache_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestGetOrLoadCoalescesConcurrentCallersPerLogicalKey(t *testing.T) {
	t.Parallel()

	const callers = 32
	backend := newRecordingBackend()
	backend.notifyAfterGets(callers)
	store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{
		MaxConcurrent:    4,
		MaxWaitersPerKey: callers,
	})
	start := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	loader := func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
		loads.Add(1)
		select {
		case <-release:
			return cache.LoadResult[string]{Value: "loaded", Found: true}, nil
		case <-ctx.Done():
			return cache.LoadResult[string]{}, ctx.Err()
		}
	}

	results := make(chan cache.Result[string], callers)
	errors := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := store.GetOrLoad(context.Background(), "key", loader)
			results <- result
			errors <- err
		}()
	}
	close(start)
	waitForSignal(t, backend.getsReached)
	close(release)
	group.Wait()
	close(results)
	close(errors)

	if loads.Load() != 1 {
		t.Fatalf("loader ran %d times, want 1", loads.Load())
	}
	for err := range errors {
		if err != nil {
			t.Fatalf("coalesced caller failed: %v", err)
		}
	}
	for result := range results {
		if result.State != cache.Hit || result.Value != "loaded" {
			t.Fatalf("unexpected coalesced result: %#v", result)
		}
	}
}

func TestGetOrLoadLeaderCancellationDoesNotCancelFollower(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	backend.notifyAfterGets(2)
	store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{
		MaxConcurrent:    1,
		MaxWaitersPerKey: 2,
	})
	release := make(chan struct{})
	loader := func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
		select {
		case <-release:
			return cache.LoadResult[string]{Value: "value", Found: true}, nil
		case <-ctx.Done():
			return cache.LoadResult[string]{}, ctx.Err()
		}
	}
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(leaderCtx, "key", loader)
		leaderDone <- err
	}()
	followerDone := make(chan error, 1)
	go func() {
		result, err := store.GetOrLoad(context.Background(), "key", loader)
		if err == nil && result.Value != "value" {
			err = fmt.Errorf("unexpected value %q", result.Value)
		}
		followerDone <- err
	}()
	waitForSignal(t, backend.getsReached)
	cancelLeader()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader returned %v, want cancellation", err)
	}
	close(release)
	if err := <-followerDone; err != nil {
		t.Fatalf("follower was corrupted by leader cancellation: %v", err)
	}
}

func TestGetOrLoadCanceledFollowerDetachesFromActiveFlight(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	backend.notifyAfterGets(3)
	store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{
		MaxConcurrent: 1, MaxWaitersPerKey: 2,
	})
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
		close(started)
		select {
		case <-release:
			return cache.LoadResult[string]{Value: "value", Found: true}, nil
		case <-ctx.Done():
			return cache.LoadResult[string]{}, ctx.Err()
		}
	}
	leaderDone := make(chan error, 1)
	go func() { _, err := store.GetOrLoad(context.Background(), "key", loader); leaderDone <- err }()
	waitForSignal(t, started)
	followerCtx, cancelFollower := context.WithCancel(context.Background())
	followerDone := make(chan error, 1)
	go func() { _, err := store.GetOrLoad(followerCtx, "key", loader); followerDone <- err }()
	waitForSignal(t, backend.getsReached)
	cancelFollower()
	if err := <-followerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("follower returned %v", err)
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader returned %v", err)
	}
}

func TestGetOrLoadBoundsIndependentLoaders(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	backend.notifyAfterGets(4)
	store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{
		MaxConcurrent:    1,
		MaxWaitersPerKey: 1,
	})
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	loader := func(ctx context.Context, key string) (cache.LoadResult[string], error) {
		switch key {
		case "first":
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return cache.LoadResult[string]{}, ctx.Err()
			}
		case "second":
			close(secondStarted)
		}
		return cache.LoadResult[string]{Value: key, Found: true}, nil
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(context.Background(), "first", loader)
		firstDone <- err
	}()
	waitForSignal(t, firstStarted)
	secondDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(context.Background(), "second", loader)
		secondDone <- err
	}()
	waitForSignal(t, backend.getsReached)
	select {
	case <-secondStarted:
		t.Fatal("second loader exceeded concurrency bound")
	default:
	}
	close(releaseFirst)
	waitForSignal(t, secondStarted)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestGetOrLoadRejectsWaitersBeyondPerKeyLimit(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	backend.notifyAfterGets(3)
	store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{
		MaxConcurrent:    1,
		MaxWaitersPerKey: 1,
	})
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
		close(started)
		select {
		case <-release:
			return cache.LoadResult[string]{Value: "value", Found: true}, nil
		case <-ctx.Done():
			return cache.LoadResult[string]{}, ctx.Err()
		}
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(context.Background(), "key", loader)
		firstDone <- err
	}()
	waitForSignal(t, started)
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	cancelDone := make(chan struct{})
	go func() {
		waitForSignal(t, backend.getsReached)
		cancelSecond()
		close(cancelDone)
	}()
	_, err := store.GetOrLoad(secondCtx, "key", loader)
	if !errors.Is(err, cache.ErrWaiterLimit) {
		t.Fatalf("overflow waiter returned %v", err)
	}
	<-cancelDone
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestCloseCancelsLoadersAndRejectsFurtherOperations(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{
		MaxConcurrent:    1,
		MaxWaitersPerKey: 1,
	})
	started := make(chan struct{})
	loadDone := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(context.Background(), "key", func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
			close(started)
			<-ctx.Done()
			return cache.LoadResult[string]{}, ctx.Err()
		})
		loadDone <- err
	}()
	waitForSignal(t, started)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-loadDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("loader returned %v, want cancellation", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := store.Get(context.Background(), "key"); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("Get after close returned %v", err)
	}
	if err := store.Set(context.Background(), "key", "value"); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("Set after close returned %v", err)
	}
	if err := store.Delete(context.Background(), "key"); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("Delete after close returned %v", err)
	}
}

func TestGetOrLoadNegativeCachingAndPanicCleanup(t *testing.T) {
	t.Parallel()

	clock := &mutableClock{now: time.Now()}
	backend := newRecordingBackend()
	store := newLoadingStringCache(t, backend, clock, cache.LoadPolicy{
		MaxConcurrent:    1,
		MaxWaitersPerKey: 2,
		NegativeTTL:      time.Minute,
	})
	var loads atomic.Int32
	missingLoader := func(context.Context, string) (cache.LoadResult[string], error) {
		loads.Add(1)
		return cache.LoadResult[string]{Found: false}, nil
	}
	for range 2 {
		result, err := store.GetOrLoad(context.Background(), "missing", missingLoader)
		if err != nil || result.State != cache.Miss || !result.Negative {
			t.Fatalf("unexpected negative result: result=%#v err=%v", result, err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("negative cache did not suppress load: %d", loads.Load())
	}
	clock.now = clock.now.Add(2 * time.Minute)
	if _, err := store.GetOrLoad(context.Background(), "missing", missingLoader); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 {
		t.Fatalf("expired negative entry did not reload: %d", loads.Load())
	}

	panicking := func(context.Context, string) (cache.LoadResult[string], error) {
		panic("boom")
	}
	if _, err := store.GetOrLoad(context.Background(), "panic", panicking); !errors.Is(err, cache.ErrLoaderPanic) {
		t.Fatalf("expected classified loader panic, got %v", err)
	}
	success := func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Value: "recovered", Found: true}, nil
	}
	result, err := store.GetOrLoad(context.Background(), "panic", success)
	if err != nil || result.Value != "recovered" {
		t.Fatalf("panic left poisoned flight state: result=%#v err=%v", result, err)
	}
}

func TestGetOrLoadRejectsNilLoaderAndClassifiesLoadFailure(t *testing.T) {
	t.Parallel()

	store := newLoadingStringCache(t, newRecordingBackend(), fixedClock{now: time.Now()}, cache.LoadPolicy{})
	if _, err := store.GetOrLoad(context.Background(), "key", nil); !errors.Is(err, cache.ErrLoader) {
		t.Fatalf("nil loader returned %v", err)
	}
	cause := errors.New("source unavailable")
	if _, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{}, cause
	}); !errors.Is(err, cache.ErrLoader) || !errors.Is(err, cause) {
		t.Fatalf("loader failure returned %v", err)
	}
}

func TestGetOrLoadRejectsReentrantLoaderCalls(t *testing.T) {
	t.Parallel()

	for _, nestedKey := range []string{"outer", "other"} {
		t.Run(nestedKey, func(t *testing.T) {
			store := newLoadingStringCache(t, newRecordingBackend(), fixedClock{now: time.Now()}, cache.LoadPolicy{
				MaxConcurrent: 1,
			})
			_, err := store.GetOrLoad(context.Background(), "outer", func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
				_, err := store.GetOrLoad(ctx, nestedKey, func(context.Context, string) (cache.LoadResult[string], error) {
					return cache.LoadResult[string]{Value: "nested", Found: true}, nil
				})
				return cache.LoadResult[string]{}, err
			})
			if !errors.Is(err, cache.ErrLoader) || !errors.Is(err, cache.ErrRecursiveLoad) {
				t.Fatalf("reentrant load returned %v", err)
			}
		})
	}
}

func TestRandomJitterRemainsWithinRequestedRange(t *testing.T) {
	t.Parallel()

	var jitter cache.RandomJitter
	if got := jitter.Duration(0); got != 0 {
		t.Fatalf("zero maximum returned %v", got)
	}
	if got := jitter.Duration(-time.Second); got != 0 {
		t.Fatalf("negative maximum returned %v", got)
	}
	for range 100 {
		if got := jitter.Duration(time.Millisecond); got < 0 || got >= time.Millisecond {
			t.Fatalf("jitter outside [0, max): %v", got)
		}
	}
}

func TestGetOrLoadRejectsInvalidInjectedJitter(t *testing.T) {
	t.Parallel()

	for name, duration := range map[string]time.Duration{
		"negative":      -time.Nanosecond,
		"above maximum": 11 * time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			space := mustStringKeySpace(t)
			store, err := cache.New(cache.Config[string, string]{
				Backend: newRecordingBackend(), Keys: space, Codec: cache.JSONCodec[string]{Version: 1},
				TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: time.Now()}, MaxValue: 1024,
				Load: cache.LoadPolicy{RefreshJitter: 10 * time.Second}, Jitter: fixedJitter{duration: duration},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
				return cache.LoadResult[string]{Value: "value", Found: true}, nil
			})
			if !errors.Is(err, cache.ErrInvalidPolicy) {
				t.Fatalf("invalid jitter returned %v", err)
			}
		})
	}
}

func TestGetOrLoadDoubleChecksBeforeCallingLoader(t *testing.T) {
	t.Parallel()

	now := time.Now()
	for name, record := range map[string]cache.Record{
		"hit": {
			Payload:   []byte{1, '"', 'w', 'r', 'i', 't', 't', 'e', 'n', '"'},
			ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute),
		},
		"negative": {
			ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(time.Minute), Negative: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			backend := newRecordingBackend()
			backend.recordOnGet = map[int]cache.Record{2: record}
			store := newLoadingStringCache(t, backend, fixedClock{now: now}, cache.LoadPolicy{})
			called := false
			result, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
				called = true
				return cache.LoadResult[string]{Value: "loader", Found: true}, nil
			})
			if err != nil || called {
				t.Fatalf("double check result=%#v err=%v loader_called=%t", result, err, called)
			}
			if name == "hit" && (result.State != cache.Hit || result.Value != "written") {
				t.Fatalf("writer hit lost: %#v", result)
			}
			if name == "negative" && !result.Negative {
				t.Fatalf("writer negative lost: %#v", result)
			}
		})
	}
}

func TestMutationDuringLoadWinsWithoutResurrection(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*testing.T, *cache.Cache[string, string]){
		"set": func(t *testing.T, store *cache.Cache[string, string]) {
			t.Helper()
			if err := store.Set(context.Background(), "key", "manual"); err != nil {
				t.Fatal(err)
			}
		},
		"delete": func(t *testing.T, store *cache.Cache[string, string]) {
			t.Helper()
			if err := store.Delete(context.Background(), "key"); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			backend := newRecordingBackend()
			store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{})
			started := make(chan struct{})
			release := make(chan struct{})
			done := make(chan struct {
				result cache.Result[string]
				err    error
			}, 1)
			go func() {
				result, err := store.GetOrLoad(context.Background(), "key", func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
					close(started)
					select {
					case <-release:
						return cache.LoadResult[string]{Value: "loaded", Found: true}, nil
					case <-ctx.Done():
						return cache.LoadResult[string]{}, ctx.Err()
					}
				})
				done <- struct {
					result cache.Result[string]
					err    error
				}{result: result, err: err}
			}()
			waitForSignal(t, started)
			mutate(t, store)
			close(release)
			loaded := <-done
			if loaded.err != nil {
				t.Fatalf("GetOrLoad returned %v", loaded.err)
			}
			current, err := store.Get(context.Background(), "key")
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "set":
				if loaded.result.State != cache.Hit || loaded.result.Value != "manual" ||
					current.State != cache.Hit || current.Value != "manual" {
					t.Fatalf("manual Set lost: load=%#v current=%#v", loaded.result, current)
				}
			case "delete":
				if loaded.result.State != cache.Miss || current.State != cache.Miss {
					t.Fatalf("Delete resurrected: load=%#v current=%#v", loaded.result, current)
				}
			}
		})
	}
}

func TestSupersededLoadAllowsObserverReentrancy(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	space := mustStringKeySpace(t)
	var store *cache.Cache[string, string]
	var gets atomic.Int32
	observer := observerFunc(func(_ context.Context, event cache.Event) error {
		if event.Operation == cache.OperationGet && gets.Add(1) == 3 {
			return store.Set(context.Background(), "key", "observer")
		}
		return nil
	})
	var err error
	store, err = cache.New(cache.Config[string, string]{
		Backend: backend, Keys: space, Codec: cache.JSONCodec[string]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: time.Now()},
		MaxValue: 1024, Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(context.Background(), "key", func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
			close(started)
			select {
			case <-release:
				return cache.LoadResult[string]{Value: "loaded", Found: true}, nil
			case <-ctx.Done():
				return cache.LoadResult[string]{}, ctx.Err()
			}
		})
		done <- err
	}()
	waitForSignal(t, started)
	if err := store.Set(context.Background(), "key", "manual"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	result, err := store.Get(context.Background(), "key")
	if err != nil || result.Value != "observer" {
		t.Fatalf("reentrant observer result=%#v err=%v", result, err)
	}
}

func TestGetOrLoadPropagatesDoubleCheckAndNegativeWriteFailures(t *testing.T) {
	t.Parallel()

	t.Run("double check", func(t *testing.T) {
		cause := errors.New("read failed")
		backend := newRecordingBackend()
		backend.getErrors = map[int]error{2: cause}
		store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{})
		called := false
		_, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
			called = true
			return cache.LoadResult[string]{}, nil
		})
		if !errors.Is(err, cache.ErrBackend) || !errors.Is(err, cause) || called {
			t.Fatalf("double-check error=%v loader_called=%t", err, called)
		}
	})

	t.Run("negative write", func(t *testing.T) {
		cause := errors.New("negative write failed")
		backend := newRecordingBackend()
		backend.setErrors = map[string]error{backendKey(t, "key"): cause}
		store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{NegativeTTL: time.Minute})
		result, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
			return cache.LoadResult[string]{Found: false}, nil
		})
		if !result.Negative || !errors.Is(err, cache.ErrBackend) || !errors.Is(err, cause) {
			t.Fatalf("negative write result=%#v err=%v", result, err)
		}
	})
}

func TestGetOrLoadRejectsNegativeTTLOverflowBeforeBackend(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	store := newLoadingStringCache(t, backend, fixedClock{
		now: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}, cache.LoadPolicy{NegativeTTL: time.Duration(math.MaxInt64)})
	result, err := store.GetOrLoad(t.Context(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Found: false}, nil
	})
	if !result.Negative || !errors.Is(err, cache.ErrInvalidRecord) {
		t.Fatalf("negative overflow result=%#v err=%v", result, err)
	}
	if backend.setCount != 0 {
		t.Fatalf("invalid negative record reached backend %d times", backend.setCount)
	}
}

func TestStaleWhileRevalidateCoalescesBackgroundRefreshes(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := newRecordingBackend()
	backend.records[backendKey(t, "key")] = cache.Record{
		Payload: []byte{1, '"', 'o', 'l', 'd', '"'}, ExpiresAt: now.Add(-time.Minute), StaleAt: now.Add(time.Minute),
	}
	store := newLoadingStringCache(t, backend, fixedClock{now: now}, cache.LoadPolicy{StaleWhileRevalidate: true})
	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	loader := func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return cache.LoadResult[string]{Value: "fresh", Found: true}, nil
		case <-ctx.Done():
			return cache.LoadResult[string]{}, ctx.Err()
		}
	}
	first, err := store.GetOrLoad(context.Background(), "key", loader)
	if err != nil || first.State != cache.Stale {
		t.Fatalf("first stale result=%#v err=%v", first, err)
	}
	waitForSignal(t, started)
	second, err := store.GetOrLoad(context.Background(), "key", loader)
	if err != nil || second.State != cache.Stale {
		t.Fatalf("second stale result=%#v err=%v", second, err)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("background loader ran %d times", got)
	}
	close(release)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteDuringBackgroundRefreshPreventsResurrection(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := newRecordingBackend()
	backend.notifyAfterGets(3)
	key := backendKey(t, "key")
	backend.records[key] = cache.Record{
		Payload: []byte{1, '"', 'o', 'l', 'd', '"'}, ExpiresAt: now.Add(-time.Minute), StaleAt: now.Add(time.Minute),
	}
	store := newLoadingStringCache(t, backend, fixedClock{now: now}, cache.LoadPolicy{
		StaleWhileRevalidate: true,
	})
	started := make(chan struct{})
	release := make(chan struct{})
	result, err := store.GetOrLoad(t.Context(), "key", func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
		close(started)
		select {
		case <-release:
			return cache.LoadResult[string]{Value: "refreshed", Found: true}, nil
		case <-ctx.Done():
			return cache.LoadResult[string]{}, ctx.Err()
		}
	})
	if err != nil || result.State != cache.Stale {
		t.Fatalf("stale result=%#v err=%v", result, err)
	}
	waitForSignal(t, started)
	if err := store.Delete(t.Context(), "key"); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForSignal(t, backend.getsReached)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if record, found, err := backend.Get(t.Context(), key); err != nil || found {
		t.Fatalf("refresh resurrected deleted record: record=%#v found=%t err=%v", record, found, err)
	}
}

func TestCloseCancelsLoaderWaitingForGlobalSlot(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	backend.notifyAfterGets(4)
	store := newLoadingStringCache(t, backend, fixedClock{now: time.Now()}, cache.LoadPolicy{MaxConcurrent: 1})
	firstStarted := make(chan struct{})
	loader := func(ctx context.Context, key string) (cache.LoadResult[string], error) {
		if key == "first" {
			close(firstStarted)
		}
		<-ctx.Done()
		return cache.LoadResult[string]{}, ctx.Err()
	}
	errs := make(chan error, 2)
	go func() { _, err := store.GetOrLoad(context.Background(), "first", loader); errs <- err }()
	waitForSignal(t, firstStarted)
	go func() { _, err := store.GetOrLoad(context.Background(), "second", loader); errs <- err }()
	waitForSignal(t, backend.getsReached)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-errs; !errors.Is(err, context.Canceled) {
			t.Fatalf("queued load returned %v", err)
		}
	}
}

func TestCloseBetweenReadAndFlightCreation(t *testing.T) {
	t.Parallel()

	observer := &blockingObserver{reached: make(chan struct{}), release: make(chan struct{})}
	space := mustStringKeySpace(t)
	store, err := cache.New(cache.Config[string, string]{
		Backend: newRecordingBackend(), Keys: space, Codec: cache.JSONCodec[string]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: time.Now()}, MaxValue: 1024,
		Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
			return cache.LoadResult[string]{Value: "unexpected", Found: true}, nil
		})
		done <- err
	}()
	waitForSignal(t, observer.reached)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	close(observer.release)
	if err := <-done; !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("GetOrLoad returned %v", err)
	}
}

func TestCloseBetweenStaleReadAndBackgroundRefresh(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := newRecordingBackend()
	backend.records[backendKey(t, "key")] = cache.Record{
		Payload: []byte{1, '"', 'o', 'l', 'd', '"'}, ExpiresAt: now.Add(-time.Minute), StaleAt: now.Add(time.Minute),
	}
	observer := &blockingObserver{reached: make(chan struct{}), release: make(chan struct{})}
	store, err := cache.New(cache.Config[string, string]{
		Backend: backend, Keys: mustStringKeySpace(t), Codec: cache.JSONCodec[string]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: now}, MaxValue: 1024,
		Load: cache.LoadPolicy{StaleWhileRevalidate: true}, Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
			return cache.LoadResult[string]{Value: "unexpected", Found: true}, nil
		})
		done <- err
	}()
	waitForSignal(t, observer.reached)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	close(observer.release)
	if err := <-done; !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("stale GetOrLoad returned %v", err)
	}
}

func TestGetOrLoadClassifiesSecondKeyEncodingFailure(t *testing.T) {
	t.Parallel()

	encoder := &sequenceKeyEncoder{err: errors.New("encoder became unavailable")}
	space, err := cache.NewKeySpace("test", "sequence", 1, encoder, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := cache.New(cache.Config[string, string]{
		Backend: newRecordingBackend(), Keys: space, Codec: cache.JSONCodec[string]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: time.Now()}, MaxValue: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Value: "unexpected", Found: true}, nil
	})
	if !errors.Is(err, cache.ErrInvalidKey) {
		t.Fatalf("second key encoding returned %v", err)
	}
}

func TestGetOrLoadUsesDefaultRandomJitter(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := newRecordingBackend()
	store, err := cache.New(cache.Config[string, string]{
		Backend: backend, Keys: mustStringKeySpace(t), Codec: cache.JSONCodec[string]{Version: 1},
		TTL: cache.TTLPolicy{TTL: time.Minute}, Clock: fixedClock{now: now}, MaxValue: 1024,
		Load: cache.LoadPolicy{RefreshJitter: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Value: "value", Found: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	expires := backend.records[backendKey(t, "key")].ExpiresAt
	if expires.Before(now.Add(50*time.Second)) || expires.After(now.Add(time.Minute)) {
		t.Fatalf("default jitter expiration outside range: %v", expires)
	}
}

type blockingObserver struct {
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

type sequenceKeyEncoder struct {
	calls atomic.Int32
	err   error
}

func (e *sequenceKeyEncoder) EncodeKey(key string) ([]byte, error) {
	if e.calls.Add(1) > 1 {
		return nil, e.err
	}
	return []byte(key), nil
}

func (o *blockingObserver) Observe(context.Context, cache.Event) error {
	o.once.Do(func() {
		close(o.reached)
		<-o.release
	})
	return nil
}

func TestGetOrLoadStalePolicyPrecedence(t *testing.T) {
	t.Parallel()

	now := time.Now()
	staleRecord := cache.Record{
		Payload:   []byte{1, '"', 'o', 'l', 'd', '"'},
		ExpiresAt: now.Add(-time.Minute),
		StaleAt:   now.Add(time.Minute),
	}
	loadFailure := errors.New("vendor unavailable")

	t.Run("stale if error", func(t *testing.T) {
		backend := newRecordingBackend()
		backend.records[backendKey(t, "key")] = staleRecord
		store := newLoadingStringCache(t, backend, fixedClock{now: now}, cache.LoadPolicy{
			MaxConcurrent:    1,
			MaxWaitersPerKey: 1,
			StaleIfError:     true,
		})
		result, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
			return cache.LoadResult[string]{}, loadFailure
		})
		if result.State != cache.Stale || result.Value != "old" || !errors.Is(err, loadFailure) {
			t.Fatalf("stale fallback lost value or cause: result=%#v err=%v", result, err)
		}
	})

	t.Run("stale while revalidate", func(t *testing.T) {
		backend := newRecordingBackend()
		backend.records[backendKey(t, "key")] = staleRecord
		backend.notifyAfterSets(1)
		store := newLoadingStringCache(t, backend, fixedClock{now: now}, cache.LoadPolicy{
			MaxConcurrent:        1,
			MaxWaitersPerKey:     1,
			StaleWhileRevalidate: true,
		})
		started := make(chan struct{})
		release := make(chan struct{})
		loader := func(ctx context.Context, _ string) (cache.LoadResult[string], error) {
			close(started)
			select {
			case <-release:
				return cache.LoadResult[string]{Value: "fresh", Found: true}, nil
			case <-ctx.Done():
				return cache.LoadResult[string]{}, ctx.Err()
			}
		}
		result, err := store.GetOrLoad(context.Background(), "key", loader)
		if err != nil || result.State != cache.Stale || result.Value != "old" {
			t.Fatalf("SWR did not return stale immediately: result=%#v err=%v", result, err)
		}
		waitForSignal(t, started)
		close(release)
		waitForSignal(t, backend.setsReached)
		result, err = store.Get(context.Background(), "key")
		if err != nil || result.State != cache.Hit || result.Value != "fresh" {
			t.Fatalf("background refresh not stored: result=%#v err=%v", result, err)
		}
	})
}

func TestNewRejectsContradictoryStalePolicies(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: time.Now()},
		MaxValue: 1024,
		Load: cache.LoadPolicy{
			StaleWhileRevalidate: true,
			StaleIfError:         true,
		},
	})
	if !errors.Is(err, cache.ErrInvalidPolicy) {
		t.Fatalf("expected invalid policy error, got %v", err)
	}
}

func TestLoadedValuesApplyInjectedRefreshJitter(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := &mutableClock{now: now}
	backend := newRecordingBackend()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute, StaleFor: 30 * time.Second},
		Clock:    clock,
		MaxValue: 1024,
		Load: cache.LoadPolicy{
			RefreshJitter: 15 * time.Second,
		},
		Jitter: fixedJitter{duration: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Value: "value", Found: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	record := backend.records[backendKey(t, "key")]
	if !record.ExpiresAt.Equal(now.Add(50*time.Second)) || !record.StaleAt.Equal(now.Add(80*time.Second)) {
		t.Fatalf("unexpected jittered deadlines: %#v", record)
	}
}

func TestNewRejectsRefreshJitterAtOrAboveTTL(t *testing.T) {
	t.Parallel()

	backend := newRecordingBackend()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    fixedClock{now: time.Now()},
		MaxValue: 1024,
		Load:     cache.LoadPolicy{RefreshJitter: time.Minute},
		Jitter:   fixedJitter{},
	})
	if !errors.Is(err, cache.ErrInvalidPolicy) {
		t.Fatalf("expected invalid jitter policy, got %v", err)
	}
}

func newLoadingStringCache(t *testing.T, backend cache.Backend, clock cache.Clock, policy cache.LoadPolicy) *cache.Cache[string, string] {
	t.Helper()
	space, err := cache.NewKeySpace("test", "strings", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    clock,
		MaxValue: 1024,
		Load:     policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for concurrency signal")
	}
}

type fixedJitter struct{ duration time.Duration }

func (j fixedJitter) Duration(time.Duration) time.Duration { return j.duration }
