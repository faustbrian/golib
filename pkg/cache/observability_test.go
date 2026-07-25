package cache_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestObserverReceivesLowCardinalityRedactedEvents(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	backend := newRecordingBackend()
	store := newObservedStringCache(t, backend, observer)
	ctx := context.Background()
	secretKey := "tenant-42/customer@example.com"
	secretValue := "private-value"

	if result, err := store.Get(ctx, secretKey); err != nil || result.State != cache.Miss {
		t.Fatalf("miss: result=%#v err=%v", result, err)
	}
	if err := store.Set(ctx, secretKey, secretValue); err != nil {
		t.Fatal(err)
	}
	if result, err := store.Get(ctx, secretKey); err != nil || result.State != cache.Hit {
		t.Fatalf("hit: result=%#v err=%v", result, err)
	}

	events := observer.Events()
	if !containsEvent(events, cache.OperationGet, cache.OutcomeMiss) ||
		!containsEvent(events, cache.OperationSet, cache.OutcomeSuccess) ||
		!containsEvent(events, cache.OperationGet, cache.OutcomeHit) {
		t.Fatalf("missing semantic events: %#v", events)
	}
	for _, event := range events {
		rendered := fmt.Sprintf("%#v", event)
		if strings.Contains(rendered, secretKey) || strings.Contains(rendered, secretValue) {
			t.Fatalf("event leaked key or value: %s", rendered)
		}
		if event.Duration < 0 {
			t.Fatalf("event has negative latency: %#v", event)
		}
	}
}

func TestObserverFailureAndPanicCannotChangeCacheBehavior(t *testing.T) {
	t.Parallel()

	for name, observer := range map[string]cache.Observer{
		"error": observerFunc(func(context.Context, cache.Event) error {
			return errors.New("exporter unavailable")
		}),
		"panic": observerFunc(func(context.Context, cache.Event) error {
			panic("broken hook")
		}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newObservedStringCache(t, newRecordingBackend(), observer)
			if err := store.Set(context.Background(), "key", "value"); err != nil {
				t.Fatalf("observer changed Set result: %v", err)
			}
			result, err := store.Get(context.Background(), "key")
			if err != nil || result.Value != "value" {
				t.Fatalf("observer changed Get result: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestActualLoaderEmitsOneLoadEvent(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	backend := newRecordingBackend()
	store := newObservedStringCache(t, backend, observer)
	result, err := store.GetOrLoad(context.Background(), "key", func(context.Context, string) (cache.LoadResult[string], error) {
		return cache.LoadResult[string]{Value: "loaded", Found: true}, nil
	})
	if err != nil || result.Value != "loaded" {
		t.Fatalf("load: result=%#v err=%v", result, err)
	}

	count := 0
	for _, event := range observer.Events() {
		if event.Operation == cache.OperationLoad {
			count++
			if event.Outcome != cache.OutcomeSuccess {
				t.Fatalf("unexpected load outcome: %#v", event)
			}
		}
	}
	if count != 1 {
		t.Fatalf("emitted %d load events, want 1", count)
	}
}

func newObservedStringCache(t *testing.T, backend cache.Backend, observer cache.Observer) *cache.Cache[string, string] {
	t.Helper()
	space, err := cache.NewKeySpace("test", "observed", 1, cache.StringKeyEncoder{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	store, err := cache.New(cache.Config[string, string]{
		Backend:  backend,
		Keys:     space,
		Codec:    cache.JSONCodec[string]{Version: 1},
		TTL:      cache.TTLPolicy{TTL: time.Minute},
		Clock:    cache.SystemClock{},
		MaxValue: 1024,
		Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func containsEvent(events []cache.Event, operation cache.Operation, outcome cache.Outcome) bool {
	for _, event := range events {
		if event.Operation == operation && event.Outcome == outcome {
			return true
		}
	}
	return false
}

type recordingObserver struct {
	mu     sync.Mutex
	events []cache.Event
}

func (o *recordingObserver) Observe(_ context.Context, event cache.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
	return nil
}

func (o *recordingObserver) Events() []cache.Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]cache.Event(nil), o.events...)
}

type observerFunc func(context.Context, cache.Event) error

func (f observerFunc) Observe(ctx context.Context, event cache.Event) error {
	return f(ctx, event)
}
