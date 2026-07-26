package prompts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestDynamicOptionsDebouncesBoundsAndCopies(t *testing.T) {
	clock := prompts.NewVirtualClock(time.Unix(100, 0))
	option := mustOption(t, prompts.OptionConfig[int]{ID: "one", Label: "One", Value: 1})
	providerCalls := 0
	dynamic, err := prompts.NewDynamicOptions(prompts.DynamicOptionsConfig[int]{
		Clock: clock, Debounce: 50 * time.Millisecond, MaxOptions: 2,
		MaxQueryRunes: 4,
		Provider: func(ctx context.Context, query string) ([]prompts.Option[int], error) {
			providerCalls++
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if query != "one" {
				t.Fatalf("provider query = %q", query)
			}
			return []prompts.Option[int]{option}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewDynamicOptions() error = %v", err)
	}

	generation, err := dynamic.Schedule("one")
	if err != nil || generation == 0 {
		t.Fatalf("Schedule() = %d, %v", generation, err)
	}
	options, applied, err := dynamic.Resolve(context.Background(), generation)
	if err != nil || applied || options != nil || providerCalls != 0 {
		t.Fatalf("early Resolve() = %#v, %t, %v; calls = %d", options, applied, err, providerCalls)
	}
	if err := clock.Advance(50 * time.Millisecond); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	options, applied, err = dynamic.Resolve(context.Background(), generation)
	if err != nil || !applied || len(options) != 1 || providerCalls != 1 {
		t.Fatalf("Resolve() = %#v, %t, %v; calls = %d", options, applied, err, providerCalls)
	}
	options[0] = mustOption(t, prompts.OptionConfig[int]{ID: "changed", Label: "Changed", Value: 2})
	snapshot, current := dynamic.Snapshot()
	if current != generation || len(snapshot) != 1 || snapshot[0].ID() != "one" {
		t.Fatalf("Snapshot() = %#v, %d", snapshot, current)
	}
	snapshot[0] = mustOption(t, prompts.OptionConfig[int]{ID: "changed", Label: "Changed", Value: 2})
	again, _ := dynamic.Snapshot()
	if again[0].ID() != "one" {
		t.Fatal("Snapshot retained caller mutation")
	}

	if _, err := dynamic.Schedule("12345"); !errors.Is(err, prompts.ErrUnsupported) {
		t.Fatalf("oversized Schedule() error = %v", err)
	}
	if _, err := dynamic.Schedule(string([]byte{0xff})); !errors.Is(err, prompts.ErrUnsupported) {
		t.Fatalf("invalid UTF-8 Schedule() error = %v", err)
	}
}

func TestDynamicOptionsRejectsStaleGeneration(t *testing.T) {
	clock := prompts.NewVirtualClock(time.Unix(200, 0))
	started := make(chan struct{})
	release := make(chan struct{})
	dynamic, err := prompts.NewDynamicOptions(prompts.DynamicOptionsConfig[string]{
		Clock: clock,
		Provider: func(_ context.Context, query string) ([]prompts.Option[string], error) {
			if query == "old" {
				close(started)
				<-release
			}
			return []prompts.Option[string]{mustOption(t, prompts.OptionConfig[string]{
				ID: query, Label: query, Value: query,
			})}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewDynamicOptions() error = %v", err)
	}
	oldGeneration, _ := dynamic.Schedule("old")
	type result struct {
		options []prompts.Option[string]
		applied bool
		err     error
	}
	oldResult := make(chan result, 1)
	go func() {
		options, applied, resolveErr := dynamic.Resolve(context.Background(), oldGeneration)
		oldResult <- result{options: options, applied: applied, err: resolveErr}
	}()
	<-started
	newGeneration, _ := dynamic.Schedule("new")
	options, applied, err := dynamic.Resolve(context.Background(), newGeneration)
	if err != nil || !applied || options[0].ID() != "new" {
		t.Fatalf("new Resolve() = %#v, %t, %v", options, applied, err)
	}
	close(release)
	stale := <-oldResult
	if stale.err != nil || stale.applied || stale.options != nil {
		t.Fatalf("stale Resolve() = %#v, %t, %v", stale.options, stale.applied, stale.err)
	}
	snapshot, generation := dynamic.Snapshot()
	if generation != newGeneration || snapshot[0].ID() != "new" {
		t.Fatalf("Snapshot() = %#v, %d", snapshot, generation)
	}
}

func TestDynamicOptionsReportsSafeProviderFailures(t *testing.T) {
	tests := map[string]struct {
		provider prompts.OptionProvider[int]
		context  func() context.Context
		want     error
	}{
		"provider error": {
			provider: func(context.Context, string) ([]prompts.Option[int], error) {
				return nil, errors.New("unsafe\x1b[31m")
			},
			context: context.Background,
			want:    prompts.ErrAdapter,
		},
		"provider panic": {
			provider: func(context.Context, string) ([]prompts.Option[int], error) {
				panic("unsafe")
			},
			context: context.Background,
			want:    prompts.ErrAdapter,
		},
		"canceled": {
			provider: func(context.Context, string) ([]prompts.Option[int], error) {
				return nil, context.Canceled
			},
			context: context.Background,
			want:    prompts.ErrCanceled,
		},
		"invalid options": {
			provider: func(context.Context, string) ([]prompts.Option[int], error) {
				return []prompts.Option[int]{{}}, nil
			},
			context: context.Background,
			want:    prompts.ErrAdapter,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dynamic, err := prompts.NewDynamicOptions(prompts.DynamicOptionsConfig[int]{
				Clock: prompts.NewVirtualClock(time.Time{}), Provider: test.provider,
			})
			if err != nil {
				t.Fatalf("NewDynamicOptions() error = %v", err)
			}
			generation, _ := dynamic.Schedule("query")
			_, applied, err := dynamic.Resolve(test.context(), generation)
			if applied || !errors.Is(err, test.want) {
				t.Fatalf("Resolve() = %t, %v; want %v", applied, err, test.want)
			}
			if err != nil && (containsUnsafe(err.Error())) {
				t.Fatalf("Resolve() leaked unsafe cause: %q", err)
			}
		})
	}
}

func TestDynamicOptionsValidatesDefinitionAndResults(t *testing.T) {
	provider := func(context.Context, string) ([]prompts.Option[int], error) { return nil, nil }
	validClock := prompts.NewVirtualClock(time.Time{})
	for name, config := range map[string]prompts.DynamicOptionsConfig[int]{
		"provider":    {Clock: validClock},
		"clock":       {Provider: provider},
		"debounce":    {Clock: validClock, Provider: provider, Debounce: -1},
		"max options": {Clock: validClock, Provider: provider, MaxOptions: -1},
		"max query":   {Clock: validClock, Provider: provider, MaxQueryRunes: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := prompts.NewDynamicOptions(config); !errors.Is(err, prompts.ErrInvalidDefinition) {
				t.Fatalf("NewDynamicOptions() error = %v", err)
			}
		})
	}

	duplicate := mustOption(t, prompts.OptionConfig[int]{ID: "same", Label: "Same", Value: 1})
	tooMany := []prompts.Option[int]{
		mustOption(t, prompts.OptionConfig[int]{ID: "one", Label: "One", Value: 1}),
		mustOption(t, prompts.OptionConfig[int]{ID: "two", Label: "Two", Value: 2}),
	}
	for name, options := range map[string][]prompts.Option[int]{
		"duplicate": {duplicate, duplicate},
		"too many":  tooMany,
	} {
		t.Run(name, func(t *testing.T) {
			maximum := 1
			if name == "duplicate" {
				maximum = 2
			}
			dynamic, err := prompts.NewDynamicOptions(prompts.DynamicOptionsConfig[int]{
				Clock: validClock, MaxOptions: maximum,
				Provider: func(context.Context, string) ([]prompts.Option[int], error) {
					return options, nil
				},
			})
			if err != nil {
				t.Fatalf("NewDynamicOptions() error = %v", err)
			}
			generation, _ := dynamic.Schedule("")
			if _, applied, err := dynamic.Resolve(context.Background(), generation); applied || !errors.Is(err, prompts.ErrAdapter) {
				t.Fatalf("Resolve() = %t, %v", applied, err)
			}
		})
	}

	dynamic, _ := prompts.NewDynamicOptions(prompts.DynamicOptionsConfig[int]{Clock: validClock, Provider: provider})
	if _, applied, err := dynamic.Resolve(context.Background(), 99); err != nil || applied {
		t.Fatalf("unknown generation Resolve() = %t, %v", applied, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, applied, err := dynamic.Resolve(canceled, 0); applied || !errors.Is(err, prompts.ErrCanceled) {
		t.Fatalf("canceled Resolve() = %t, %v", applied, err)
	}
	var nilContext context.Context
	if _, applied, err := dynamic.Resolve(nilContext, 0); applied || !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil context Resolve() = %t, %v", applied, err)
	}
}

func containsUnsafe(value string) bool {
	for _, fragment := range []string{"unsafe", "\x1b"} {
		if len(value) >= len(fragment) {
			for index := 0; index+len(fragment) <= len(value); index++ {
				if value[index:index+len(fragment)] == fragment {
					return true
				}
			}
		}
	}
	return false
}
