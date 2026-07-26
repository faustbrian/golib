package clock_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

func TestObservedClockReportsBoundedLifecycleData(t *testing.T) {
	t.Parallel()

	base, err := manual.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	observations := make([]clock.Observation, 0, 4)
	observed, err := clock.Observe(base, clock.ObserverFunc(func(observation clock.Observation) {
		mu.Lock()
		defer mu.Unlock()
		observations = append(observations, observation)
	}), clock.WithTags(map[string]string{"component": "test"}))
	if err != nil {
		t.Fatal(err)
	}

	timer, err := observed.NewTimer(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !timer.Stop() {
		t.Fatal("Stop() = false")
	}
	callback, err := observed.AfterFunc(2*time.Second, func() {})
	if err != nil {
		t.Fatal(err)
	}
	_ = callback
	waiter, err := base.Advance(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(observations) != 4 {
		t.Fatalf("observations = %+v, want four", observations)
	}
	want := []struct {
		kind    clock.Kind
		outcome clock.Outcome
	}{{clock.KindTimer, clock.OutcomeCreated}, {clock.KindTimer, clock.OutcomeStopped},
		{clock.KindCallback, clock.OutcomeCreated}, {clock.KindCallback, clock.OutcomeFired}}
	for index, expected := range want {
		got := observations[index]
		if got.Kind != expected.kind || got.Outcome != expected.outcome {
			t.Fatalf("observation %d = %+v", index, got)
		}
		if got.Tags["component"] != "test" || len(got.Tags) != 1 {
			t.Fatalf("tags = %v", got.Tags)
		}
	}
	if observations[3].Requested != 2*time.Second || observations[3].Elapsed != 2*time.Second {
		t.Fatalf("callback observation = %+v", observations[3])
	}
}

func TestObserveValidatesInputsAndContainsObserverPanics(t *testing.T) {
	t.Parallel()

	if _, err := clock.Observe(nil, clock.ObserverFunc(func(clock.Observation) {})); !errors.Is(err, clock.ErrInvalidClock) {
		t.Fatalf("Observe(nil) error = %v", err)
	}
	if _, err := clock.Observe(clock.System{}, nil); !errors.Is(err, clock.ErrInvalidObserver) {
		t.Fatalf("Observe(nil observer) error = %v", err)
	}
	if _, err := clock.Observe(clock.System{}, clock.ObserverFunc(nil)); !errors.Is(err, clock.ErrInvalidObserver) {
		t.Fatalf("Observe(nil function) error = %v", err)
	}
	tags := make(map[string]string, clock.MaxObservationTags+1)
	for index := 0; index <= clock.MaxObservationTags; index++ {
		tags[string(rune('a'+index))] = "value"
	}
	if _, err := clock.Observe(clock.System{}, clock.ObserverFunc(func(clock.Observation) {}), clock.WithTags(tags)); !errors.Is(err, clock.ErrObservationTags) {
		t.Fatalf("Observe(tags) error = %v", err)
	}
	for _, invalid := range []map[string]string{{"": "value"}, {"key": string(make([]byte, clock.MaxObservationTagBytes+1))}} {
		if _, err := clock.Observe(clock.System{}, clock.ObserverFunc(func(clock.Observation) {}), clock.WithTags(invalid)); !errors.Is(err, clock.ErrObservationTags) {
			t.Fatalf("Observe(invalid tags) error = %v", err)
		}
	}
	boundaryTags := make(map[string]string, clock.MaxObservationTags)
	for index := range clock.MaxObservationTags {
		key := string(rune('a'+index)) + strings.Repeat("k", clock.MaxObservationTagBytes-1)
		boundaryTags[key] = strings.Repeat("v", clock.MaxObservationTagBytes)
	}
	if _, err := clock.Observe(clock.System{}, clock.ObserverFunc(func(clock.Observation) {}), clock.WithTags(boundaryTags)); err != nil {
		t.Fatalf("Observe(maximum tags) error = %v", err)
	}

	observed, err := clock.Observe(clock.System{}, clock.ObserverFunc(func(clock.Observation) { panic("observer") }))
	if err != nil {
		t.Fatal(err)
	}
	timer, err := observed.NewTimer(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !timer.Stop() {
		t.Fatal("observer panic corrupted timer creation")
	}
}

func TestObservedClockDelegatesCapabilitiesAndAllLifecycleTransitions(t *testing.T) {
	t.Parallel()

	base, err := manual.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	observations := make([]clock.Observation, 0, 16)
	observed, err := clock.Observe(base, clock.ObserverFunc(func(observation clock.Observation) {
		mu.Lock()
		observations = append(observations, observation)
		mu.Unlock()
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.Now().Equal(base.Now()) || observed.Since(base.Now()) != 0 || observed.Measure()() != 0 {
		t.Fatal("wall or elapsed capability did not delegate")
	}
	if err := observed.Sleep(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := observed.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() error = %v", err)
	}
	mu.Lock()
	if len(observations) != 2 || observations[0].Kind != clock.KindSleep ||
		observations[0].Outcome != clock.OutcomeCompleted ||
		observations[1].Kind != clock.KindSleep ||
		observations[1].Outcome != clock.OutcomeCanceled {
		mu.Unlock()
		t.Fatalf("sleep observations = %+v", observations)
	}
	mu.Unlock()

	timer, err := observed.NewTimer(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if active, err := timer.Reset(time.Minute); err != nil || !active {
		t.Fatalf("timer Reset() = (%v, %v)", active, err)
	}
	timer.Stop()
	timer.Stop()

	ticker, err := observed.NewTicker(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := ticker.Reset(time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := ticker.Reset(0); !errors.Is(err, clock.ErrInvalidDuration) {
		t.Fatalf("ticker Reset() error = %v", err)
	}
	ticker.Stop()
	if _, err := observed.NewTicker(0); !errors.Is(err, clock.ErrInvalidDuration) {
		t.Fatalf("NewTicker() error = %v", err)
	}

	callback, err := observed.AfterFunc(time.Hour, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if active, err := callback.Reset(time.Minute); err != nil || !active {
		t.Fatalf("callback Reset() = (%v, %v)", active, err)
	}
	callback.Stop()
	callback.Stop()
	if _, err := observed.AfterFunc(time.Second, nil); !errors.Is(err, clock.ErrInvalidCallback) {
		t.Fatalf("AfterFunc(nil) error = %v", err)
	}

	panicking, err := observed.AfterFunc(0, func() { panic("payload") })
	if err != nil {
		t.Fatal(err)
	}
	_ = panicking
	waiter, err := base.Advance(0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := waiter.Wait(context.Background())
	if err != nil || result.Panics != 1 {
		t.Fatalf("panic advancement = (%+v, %v)", result, err)
	}

	mu.Lock()
	defer mu.Unlock()
	foundPanic := false
	foundRejected := false
	foundCompleted := false
	foundCanceled := false
	foundReset := false
	foundInactive := false
	for _, observation := range observations {
		foundPanic = foundPanic || observation.Outcome == clock.OutcomePanicked
		foundRejected = foundRejected || observation.Outcome == clock.OutcomeRejected
		foundCompleted = foundCompleted || observation.Outcome == clock.OutcomeCompleted
		foundCanceled = foundCanceled || observation.Outcome == clock.OutcomeCanceled
		foundReset = foundReset || observation.Outcome == clock.OutcomeReset
		foundInactive = foundInactive || observation.Outcome == clock.OutcomeInactive
	}
	if !foundPanic || !foundRejected || !foundCompleted || !foundCanceled || !foundReset || !foundInactive {
		t.Fatalf("observations lack required outcomes: %+v", observations)
	}
}

func TestObservedClockReportsFactoryAndResetErrors(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	observations := make([]clock.Observation, 0, 12)
	record := clock.ObserverFunc(func(observation clock.Observation) {
		mu.Lock()
		defer mu.Unlock()
		observations = append(observations, observation)
	})

	limited, err := manual.New(time.Unix(1, 0), manual.WithLimits(manual.Limits{MaxActive: 1, MaxWorkPerAdvance: 10}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limited.NewTimer(time.Hour); err != nil {
		t.Fatal(err)
	}
	observed, err := clock.Observe(limited, record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observed.NewTimer(time.Hour); !errors.Is(err, manual.ErrActiveLimit) {
		t.Fatalf("NewTimer() error = %v", err)
	}
	if _, err := observed.AfterFunc(time.Hour, func() {}); !errors.Is(err, manual.ErrActiveLimit) {
		t.Fatalf("AfterFunc() error = %v", err)
	}

	base, err := manual.New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	observed, err = clock.Observe(base, record)
	if err != nil {
		t.Fatal(err)
	}
	timer, err := observed.NewTimer(time.Duration(1<<63 - 1))
	if err != nil {
		t.Fatal(err)
	}
	callback, err := observed.AfterFunc(time.Duration(1<<63-1), func() {})
	if err != nil {
		t.Fatal(err)
	}
	ticker, err := observed.NewTicker(time.Duration(1<<63 - 1))
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := base.Advance(time.Duration(1<<63 - 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := timer.Reset(1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("timer Reset() error = %v", err)
	}
	if _, err := callback.Reset(1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("callback Reset() error = %v", err)
	}
	if err := ticker.Reset(1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("ticker Reset() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	rejected := map[clock.Kind]int{}
	for _, observation := range observations {
		if observation.Outcome == clock.OutcomeRejected {
			rejected[observation.Kind]++
		}
	}
	if rejected[clock.KindTimer] != 2 || rejected[clock.KindCallback] != 2 || rejected[clock.KindTicker] != 1 {
		t.Fatalf("rejected observations = %v; all factory/reset errors must be classified", rejected)
	}
}
