package hedge_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestScheduledDelaysLaunchEachBoundedHedge(t *testing.T) {
	t.Parallel()

	clock := newManualClock()
	config := validConfig()
	config.Clock = clock
	config.MaxHedges = 2
	config.Delay = 0
	config.Schedule = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	started := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		close(started[info.Ordinal])
		if info.Ordinal == 2 {
			return func(context.Context) (string, error) { return "second-hedge", nil }, "pod-c", nil
		}
		return func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "loser", ctx.Err()
		}, "pod", nil
	})
	type result struct {
		value  string
		report hedge.Report
		err    error
	}
	done := make(chan result, 1)
	go func() {
		value, report, doErr := hedge.Do(context.Background(), policy, factory)
		done <- result{value: value, report: report, err: doErr}
	}()
	<-started[0]
	clock.WaitTimers(2)
	clock.Advance(10 * time.Millisecond)
	<-started[1]
	clock.WaitTimers(3)
	clock.Advance(20 * time.Millisecond)
	<-started[2]
	got := <-done
	if got.err != nil || got.value != "second-hedge" || got.report.HedgesStarted != 2 || got.report.WinnerOrdinal != 2 {
		t.Fatalf("Do() = (%q, %+v, %v)", got.value, got.report, got.err)
	}
	if err := got.report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicDelayFailureStopsScheduledWork(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = 0
	config.DynamicDelay = func(input hedge.DelayInput) (time.Duration, error) {
		if input.Hedge != 1 || input.Previous != 0 {
			t.Fatalf("delay input = %+v", input)
		}
		return 0, errors.New("private percentile failure")
	}
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "resource", ctx.Err()
		}, "pod-a", nil
	})
	_, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr == nil || report.Reason != hedge.ReasonDelayFailure || report.HedgesStarted != 0 {
		t.Fatalf("Do() = (%+v, %v)", report, gotErr)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicDelayPanicStopsScheduledWork(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = 0
	config.DynamicDelay = func(hedge.DelayInput) (time.Duration, error) { panic("private percentile state") }
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(ctx context.Context) (string, error) { <-ctx.Done(); return "resource", ctx.Err() }, "pod", nil
	})
	_, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr == nil || report.Reason != hedge.ReasonDelayFailure {
		t.Fatalf("Do() = (%+v, %v)", report, gotErr)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryFailureModesAreExplicit(t *testing.T) {
	t.Parallel()

	for _, mode := range []hedge.FactoryFailureMode{hedge.FactoryFailureStop, hedge.FactoryFailureContinue} {
		mode := mode
		t.Run(modeName(mode), func(t *testing.T) {
			t.Parallel()
			clock := newManualClock()
			config := validConfig()
			config.Clock = clock
			config.Delay = time.Millisecond
			config.FactoryFailureMode = mode
			policy, err := hedge.NewPolicy(config)
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			factoryFailed := make(chan struct{})
			factoryErr := errors.New("factory detail")
			factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
				if info.Hedge {
					close(factoryFailed)
					return nil, "", factoryErr
				}
				return func(ctx context.Context) (string, error) {
					select {
					case <-release:
						return "original", nil
					case <-ctx.Done():
						return "canceled", ctx.Err()
					}
				}, "pod-a", nil
			})
			type result struct {
				value  string
				report hedge.Report
				err    error
			}
			done := make(chan result, 1)
			go func() {
				value, report, doErr := hedge.Do(context.Background(), policy, factory)
				done <- result{value: value, report: report, err: doErr}
			}()
			clock.WaitTimers(2)
			clock.Advance(time.Millisecond)
			<-factoryFailed
			if mode == hedge.FactoryFailureContinue {
				close(release)
			}
			got := <-done
			if mode == hedge.FactoryFailureStop {
				if !errors.Is(got.err, factoryErr) || got.report.Reason != hedge.ReasonFactoryFailure {
					t.Fatalf("stop result = (%+v, %v)", got.report, got.err)
				}
			} else if got.err != nil || got.value != "original" || got.report.Reason != hedge.ReasonNoHedgeNeeded || len(got.report.Failures) != 1 {
				t.Fatalf("continue result = (%q, %+v, %v)", got.value, got.report, got.err)
			}
			if err := got.report.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func modeName(mode hedge.FactoryFailureMode) string {
	if mode == hedge.FactoryFailureStop {
		return "stop"
	}
	return "continue"
}

func TestScheduleConfigurationIsCopiedAndFullyValidated(t *testing.T) {
	t.Parallel()

	tests := []func(*hedge.Config[string]){
		func(config *hedge.Config[string]) {
			config.Schedule = []time.Duration{time.Second}
			config.Delay = time.Second
		},
		func(config *hedge.Config[string]) {
			config.Schedule = []time.Duration{time.Second}
			config.Delay = 0
			config.MaxHedges = 2
		},
		func(config *hedge.Config[string]) { config.Schedule = []time.Duration{0}; config.Delay = 0 },
		func(config *hedge.Config[string]) { config.AttemptTimeout = -time.Second },
		func(config *hedge.Config[string]) { config.AttemptTimeout = 2 * config.TotalTimeout },
		func(config *hedge.Config[string]) { config.Resource = string(make([]byte, hedge.MaxResourceLength+1)) },
	}
	for index, mutate := range tests {
		config := validConfig()
		mutate(&config)
		if _, err := hedge.NewPolicy(config); !errors.Is(err, hedge.ErrInvalidPolicy) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}

	schedule := []time.Duration{time.Millisecond}
	config := validConfig()
	config.Delay = 0
	config.Schedule = schedule
	if _, err := hedge.NewPolicy(config); err != nil {
		t.Fatal(err)
	}
	schedule[0] = 0
}

type observationCollector struct {
	mu     sync.Mutex
	events []hedge.Observation
}

func (collector *observationCollector) TryObserve(event hedge.Observation) bool {
	collector.mu.Lock()
	collector.events = append(collector.events, event)
	collector.mu.Unlock()
	return true
}
