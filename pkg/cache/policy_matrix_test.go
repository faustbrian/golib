package cache_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

func TestGetOrLoadStatePolicyMatrix(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	sourceFailure := errors.New("source unavailable")
	states := []string{"hit", "negative", "miss", "stale"}
	policies := map[string]cache.LoadPolicy{
		"plain":                  {NegativeTTL: time.Minute},
		"stale-if-error":         {NegativeTTL: time.Minute, StaleIfError: true},
		"stale-while-revalidate": {NegativeTTL: time.Minute, StaleWhileRevalidate: true},
	}
	outcomes := []string{"found", "absent", "error"}

	for _, state := range states {
		for policyName, policy := range policies {
			for _, loaderOutcome := range outcomes {
				name := fmt.Sprintf("%s/%s/%s", state, policyName, loaderOutcome)
				t.Run(name, func(t *testing.T) {
					backend := newRecordingBackend()
					seedMatrixState(t, backend, state, now)
					store := newLoadingStringCache(t, backend, fixedClock{now: now}, policy)
					var loads atomic.Int32
					started := make(chan struct{})
					var once sync.Once
					loader := func(context.Context, string) (cache.LoadResult[string], error) {
						loads.Add(1)
						once.Do(func() { close(started) })
						switch loaderOutcome {
						case "found":
							return cache.LoadResult[string]{Value: "loaded", Found: true}, nil
						case "absent":
							return cache.LoadResult[string]{Found: false}, nil
						default:
							return cache.LoadResult[string]{}, sourceFailure
						}
					}

					result, err := store.GetOrLoad(t.Context(), "key", loader)
					shouldLoad := state == "miss" || state == "stale"
					if shouldLoad && policyName == "stale-while-revalidate" && state == "stale" {
						waitForSignal(t, started)
					}
					if err := store.Close(); err != nil {
						t.Fatal(err)
					}
					assertMatrixResult(
						t,
						state,
						policyName,
						loaderOutcome,
						result,
						err,
						sourceFailure,
					)
					if shouldLoad && loads.Load() != 1 {
						t.Fatalf("loader calls=%d, want 1", loads.Load())
					}
					if !shouldLoad && loads.Load() != 0 {
						t.Fatalf("loader calls=%d, want 0", loads.Load())
					}
				})
			}
		}
	}
}

func seedMatrixState(t *testing.T, backend *recordingBackend, state string, now time.Time) {
	t.Helper()
	key := backendKey(t, "key")
	switch state {
	case "hit":
		backend.records[key] = cache.Record{
			Payload:   []byte{1, '"', 'c', 'a', 'c', 'h', 'e', 'd', '"'},
			ExpiresAt: now.Add(time.Minute),
			StaleAt:   now.Add(2 * time.Minute),
		}
	case "negative":
		backend.records[key] = cache.Record{
			ExpiresAt: now.Add(time.Minute),
			StaleAt:   now.Add(time.Minute),
			Negative:  true,
		}
	case "stale":
		backend.records[key] = cache.Record{
			Payload:   []byte{1, '"', 'o', 'l', 'd', '"'},
			ExpiresAt: now.Add(-time.Minute),
			StaleAt:   now.Add(time.Minute),
		}
	}
}

func assertMatrixResult(
	t *testing.T,
	state string,
	policy string,
	loaderOutcome string,
	result cache.Result[string],
	err error,
	sourceFailure error,
) {
	t.Helper()
	if state == "hit" {
		if err != nil || result.State != cache.Hit || result.Value != "cached" {
			t.Fatalf("hit result=%#v err=%v", result, err)
		}
		return
	}
	if state == "negative" {
		if err != nil || result.State != cache.Miss || !result.Negative {
			t.Fatalf("negative result=%#v err=%v", result, err)
		}
		return
	}
	if state == "stale" && policy == "stale-while-revalidate" {
		if err != nil || result.State != cache.Stale || result.Value != "old" {
			t.Fatalf("stale-while-revalidate result=%#v err=%v", result, err)
		}
		return
	}
	switch loaderOutcome {
	case "found":
		if err != nil || result.State != cache.Hit || result.Value != "loaded" {
			t.Fatalf("found result=%#v err=%v", result, err)
		}
	case "absent":
		if err != nil || result.State != cache.Miss || !result.Negative {
			t.Fatalf("absent result=%#v err=%v", result, err)
		}
	case "error":
		if !errors.Is(err, cache.ErrLoader) || !errors.Is(err, sourceFailure) {
			t.Fatalf("loader error result=%#v err=%v", result, err)
		}
		if state == "stale" && policy == "stale-if-error" {
			if result.State != cache.Stale || result.Value != "old" {
				t.Fatalf("stale-if-error lost stale result: %#v", result)
			}
		} else if result.State != 0 {
			t.Fatalf("plain loader error returned a value: %#v", result)
		}
	}
}
