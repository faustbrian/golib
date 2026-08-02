package concurrencylimit

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAlgorithmMutationBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := NewAIMDAlgorithm(AIMDConfig{Increase: 1, DecreaseFactor: math.SmallestNonzeroFloat64}); err != nil {
		t.Fatalf("smallest positive AIMD factor rejected: %v", err)
	}
	if _, err := NewAIMDAlgorithm(AIMDConfig{Increase: MaxLimit, DecreaseFactor: 0.5}); err != nil {
		t.Fatalf("maximum AIMD increase rejected: %v", err)
	}
	if _, err := NewAIMDAlgorithm(AIMDConfig{Increase: 1, DecreaseFactor: 0}); err == nil {
		t.Fatal("zero AIMD factor accepted")
	}
	aimd, _ := NewAIMDAlgorithm(AIMDConfig{Increase: 2, DecreaseFactor: 0.5, LatencyLimit: 100})
	for _, test := range []struct {
		name string
		in   Window
		want int
	}{
		{"latency threshold is inclusive", Window{CurrentLimit: 5, MaxInFlight: 2, RecentLatency: 100}, 5},
		{"one above latency threshold decreases", Window{CurrentLimit: 5, MaxInFlight: 2, RecentLatency: 101}, 2},
		{"odd half threshold increases", Window{CurrentLimit: 5, MaxInFlight: 3, RecentLatency: 100}, 7},
	} {
		if got := aimd.Update(test.in).Limit; got != test.want {
			t.Errorf("%s: limit = %d, want %d", test.name, got, test.want)
		}
	}

	if _, err := NewVegasAlgorithm(VegasConfig{Alpha: 1, Beta: 2, Increase: 1, Decrease: 1}); err != nil {
		t.Fatalf("Vegas Alpha boundary rejected: %v", err)
	}
	if _, err := NewVegasAlgorithm(VegasConfig{Alpha: MaxLimit - 1, Beta: MaxLimit, Increase: MaxLimit, Decrease: MaxLimit}); err != nil {
		t.Fatalf("maximum Vegas tuning rejected: %v", err)
	}
	vegas, _ := NewVegasAlgorithm(VegasConfig{Alpha: 2, Beta: 4, Increase: 3, Decrease: 2})
	vegasCases := []struct {
		name   string
		window Window
		limit  int
		reason string
	}{
		{"overload decrease", Window{CurrentLimit: 10, MaxInFlight: 10, BaselineLatency: 10, RecentLatency: 10, Overloads: 1}, 8, "overload"},
		{"odd application limit", Window{CurrentLimit: 5, MaxInFlight: 2, BaselineLatency: 10, RecentLatency: 10}, 5, "application-limited"},
		{"odd utilization boundary", Window{CurrentLimit: 5, MaxInFlight: 3, BaselineLatency: 10, RecentLatency: 10}, 8, "low-queue"},
		{"alpha is target", Window{CurrentLimit: 10, MaxInFlight: 10, BaselineLatency: 80, RecentLatency: 100}, 10, "target-queue"},
		{"beta is target", Window{CurrentLimit: 10, MaxInFlight: 10, BaselineLatency: 60, RecentLatency: 100}, 10, "target-queue"},
	}
	for _, test := range vegasCases {
		got := vegas.Update(test.window)
		if got.Limit != test.limit || got.State.Reason != test.reason {
			t.Errorf("%s: decision = %+v, want limit %d reason %q", test.name, got, test.limit, test.reason)
		}
	}

	if _, err := NewGradient2Algorithm(Gradient2Config{LongWindow: 2, Smoothing: 1, Tolerance: 1, MinGradient: 1}); err != nil {
		t.Fatalf("Gradient2 MinGradient boundary rejected: %v", err)
	}
	if _, err := NewGradient2Algorithm(Gradient2Config{LongWindow: MaxSampleCapacity, Smoothing: 1, Tolerance: MaxLimit, MinGradient: 1, QueueSize: MaxLimit}); err != nil {
		t.Fatalf("maximum Gradient2 tuning rejected: %v", err)
	}
	gradient, _ := NewGradient2Algorithm(Gradient2Config{LongWindow: 3, Smoothing: 1, Tolerance: 1, MinGradient: 0.1, QueueSize: 0})
	implementation := gradient.(*gradient2Algorithm)
	first := implementation.Update(Window{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 100})
	second := implementation.Update(Window{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 200})
	if first.Limit != 10 || second.Limit != 7 || implementation.longRTT != 150 {
		t.Fatalf("Gradient2 EWMA decisions = %+v, %+v; long RTT = %v", first, second, implementation.longRTT)
	}
	implementation.longRTT = 100
	corrected := implementation.Update(Window{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 10})
	if implementation.longRTT != 52.25 || corrected.Limit != 10 {
		t.Fatalf("Gradient2 fast correction = %+v, long RTT = %v", corrected, implementation.longRTT)
	}
	implementation.longRTT = 100
	zeroRTT := implementation.Update(Window{CurrentLimit: 5, MaxInFlight: 3, RecentLatency: 0})
	if implementation.longRTT != 50 || zeroRTT.State.Reason != "application-limited" {
		t.Fatalf("Gradient2 zero RTT = %+v, long RTT = %v", zeroRTT, implementation.longRTT)
	}
	implementation.longRTT = 30
	ratioBoundary := implementation.Update(Window{CurrentLimit: 5, MaxInFlight: 2, RecentLatency: 10})
	if implementation.longRTT != 20 || ratioBoundary.State.Reason != "application-limited" {
		t.Fatalf("Gradient2 correction boundary = %+v, long RTT = %v", ratioBoundary, implementation.longRTT)
	}
	implementation.longRTT = 100
	utilizationBoundary := implementation.Update(Window{CurrentLimit: 5, MaxInFlight: 3, RecentLatency: 100})
	if utilizationBoundary.State.Reason != "latency-gradient" {
		t.Fatalf("Gradient2 utilization boundary = %+v", utilizationBoundary)
	}
	overload := implementation.Update(Window{CurrentLimit: 10, MaxInFlight: 1, RecentLatency: 10, Overloads: 1})
	if overload.Limit != 9 || overload.State.Reason != "overload" {
		t.Fatalf("Gradient2 overload = %+v", overload)
	}
	stall := implementation.Update(Window{CurrentLimit: 10, MaxInFlight: 10, PreviousMaxInFlight: 9, RecentLatency: 10, Throughput: 5, PreviousThroughput: 5})
	if stall.Limit != 9 || stall.State.Reason != "throughput" {
		t.Fatalf("Gradient2 throughput = %+v", stall)
	}
}

func TestAlgorithmHelperMutationBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		limit            int
		baseline, recent time.Duration
		want             float64
	}{
		{10, -1, 10, 0}, {10, 0, 10, 0}, {10, 10, -1, 0}, {10, 10, 0, 0}, {10, 10, 10, 0}, {10, 80, 100, 2},
	} {
		if got := queueEstimate(test.limit, test.baseline, test.recent); got != test.want {
			t.Errorf("queueEstimate(%d, %v, %v) = %v, want %v", test.limit, test.baseline, test.recent, got, test.want)
		}
	}
	for _, value := range []float64{math.SmallestNonzeroFloat64, 1} {
		if !unitInterval(value) {
			t.Errorf("unitInterval(%v) = false", value)
		}
	}
	for _, value := range []float64{0, math.Nextafter(1, 2), math.Inf(1), math.NaN()} {
		if unitInterval(value) {
			t.Errorf("unitInterval(%v) = true", value)
		}
	}
	base := Window{PreviousMaxInFlight: 1, MaxInFlight: 2, PreviousThroughput: 1, Throughput: 1}
	if !throughputStalled(base) {
		t.Fatal("throughput equality was not a stall")
	}
	for name, mutate := range map[string]func(*Window){
		"no previous load":     func(window *Window) { window.PreviousMaxInFlight = 0 },
		"no load increase":     func(window *Window) { window.MaxInFlight = 1 },
		"no prior throughput":  func(window *Window) { window.PreviousThroughput = 0 },
		"throughput increased": func(window *Window) { window.Throughput = math.Nextafter(1, 2) },
	} {
		window := base
		mutate(&window)
		if throughputStalled(window) {
			t.Errorf("%s was classified as a stall", name)
		}
	}
	zeroThroughput := base
	zeroThroughput.PreviousThroughput = 0
	zeroThroughput.Throughput = 0
	if throughputStalled(zeroThroughput) {
		t.Fatal("zero previous throughput was classified as a stall")
	}
}

func TestConfigurationMutationBoundaries(t *testing.T) {
	t.Parallel()

	partitions := make([]string, MaxPartitions)
	for index := range partitions {
		partitions[index] = strconv.Itoa(index)
	}
	algorithm := &testAlgorithm{name: strings.Repeat("a", maxAlgorithmNameBytes), state: AlgorithmState{Name: strings.Repeat("a", maxAlgorithmNameBytes), Reason: strings.Repeat("r", maxAlgorithmStateBytes), Estimate: 1}}
	limiter, err := New(Config{
		MinLimit: MaxLimit, MaxLimit: MaxLimit, InitialLimit: MaxLimit, Algorithm: algorithm,
		Sampling: SamplingConfig{MinDuration: time.Nanosecond, MaxDuration: time.Nanosecond, MinSamples: MaxSampleCapacity, Capacity: MaxSampleCapacity, Quantile: 1, BaselineSmoothing: 1, MaxIncrease: MaxLimit, MaxDecrease: MaxLimit},
		Queue:    QueueConfig{MaxQueued: MaxQueued, MaxWait: time.Nanosecond}, PermitTTL: time.Nanosecond,
		MaxPriority: MaxPriority, Partitions: partitions,
	})
	if err != nil {
		t.Fatalf("exact configuration boundaries rejected: %v", err)
	}
	if snapshot := limiter.Snapshot(); snapshot.Limit != MaxLimit || len(snapshot.Algorithm.Name) != maxAlgorithmNameBytes || len(snapshot.Algorithm.Reason) != maxAlgorithmStateBytes {
		t.Fatalf("boundary snapshot = %+v", snapshot)
	}
	defaults, err := normalizeConfig(Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm()})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.sampling.MinDuration != 100*time.Millisecond || defaults.sampling.MaxDuration != time.Second || defaults.sampling.MinSamples != 20 || defaults.sampling.Capacity != 256 || defaults.sampling.Quantile != 0.9 || defaults.sampling.BaselineSmoothing != 0.1 || defaults.sampling.MaxIncrease != 1 || defaults.sampling.MaxDecrease != MaxLimit || defaults.permitTTL != 5*time.Minute {
		t.Fatalf("normalized defaults = %+v", defaults)
	}
	if _, err = limiter.Acquire(context.Background(), Metadata{Priority: MaxPriority, Partition: partitions[0]}); err != nil {
		t.Fatalf("exact metadata boundaries rejected: %v", err)
	}
	longPartition := strings.Repeat("p", MaxPartitionBytes)
	metadataLimiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Partitions: []string{longPartition}})
	if _, err = metadataLimiter.Acquire(context.Background(), Metadata{Partition: longPartition}); err != nil {
		t.Fatalf("exact partition-byte boundary rejected: %v", err)
	}
	for _, algorithm := range []*testAlgorithm{
		{name: "panic", state: AlgorithmState{Name: "panic", Estimate: 1}, panicName: true},
		{name: "", state: AlgorithmState{Name: "state", Estimate: 1}},
	} {
		_, err = normalizeConfig(Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: algorithm})
		if err == nil || !strings.Contains(err.Error(), "Algorithm.Name") {
			t.Fatalf("algorithm-name error = %v", err)
		}
	}
}

func TestLimiterMutationBoundaries(t *testing.T) {
	t.Parallel()

	clock := &queueClock{now: time.Unix(0, 100)}
	limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 2, InitialLimit: 2, Algorithm: NewFixedAlgorithm(), Clock: clock, PermitTTL: 10})
	first, _ := limiter.Acquire(context.Background())
	second, _ := limiter.Acquire(context.Background())
	limiter.mu.Lock()
	limiter.expiredPermits = 5
	limiter.mu.Unlock()
	clock.now = time.Unix(0, 109)
	if got := limiter.ReapExpired(); got != 0 {
		t.Fatalf("reaped before TTL = %d", got)
	}
	clock.now = time.Unix(0, 110)
	if got := limiter.ReapExpired(); got != 2 {
		t.Fatalf("reaped at TTL = %d", got)
	}
	if snapshot := limiter.Snapshot(); snapshot.InFlight != 0 || snapshot.ExpiredPermits != 7 || snapshot.Outcomes.Ignored != 2 {
		t.Fatalf("reap snapshot = %+v", snapshot)
	}
	if err := first.Complete(OutcomeSuccess); !errors.Is(err, ErrStalePermit) {
		t.Fatalf("first reaped permit = %v", err)
	}
	if err := second.Complete(OutcomeSuccess); !errors.Is(err, ErrStalePermit) {
		t.Fatalf("second reaped permit = %v", err)
	}

	for _, state := range []AlgorithmState{
		{Name: strings.Repeat("n", maxAlgorithmNameBytes), Reason: strings.Repeat("r", maxAlgorithmStateBytes)},
	} {
		if !validAlgorithmState(state) {
			t.Fatalf("valid boundary state rejected: %+v", state)
		}
	}
	for _, state := range []AlgorithmState{
		{Name: strings.Repeat("n", maxAlgorithmNameBytes+1)},
		{Name: "x", Reason: strings.Repeat("r", maxAlgorithmStateBytes+1)},
	} {
		if validAlgorithmState(state) {
			t.Fatalf("invalid boundary state accepted: %+v", state)
		}
	}
}

func TestLimiterFaultAndEventMutationBoundaries(t *testing.T) {
	t.Parallel()

	validState := AlgorithmState{Name: "fault", Estimate: 1}
	if _, err := New(Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: &testAlgorithm{name: "fault", state: validState, panicReset: true}}); err == nil {
		t.Fatal("reset panic with otherwise valid state accepted")
	}

	events := make([]Event, 0, 1)
	limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Observer: func(event Event) { events = append(events, event) }})
	limiter.mu.Lock()
	limiter.nextID = math.MaxUint64
	limiter.mu.Unlock()
	if _, err := limiter.Acquire(context.Background()); !errors.Is(err, ErrIdentifierExhausted) {
		t.Fatalf("identifier exhaustion error = %v", err)
	}
	if len(events) != 1 || events[0].Type != EventRejected || limiter.Snapshot().Rejections != 1 {
		t.Fatalf("identifier exhaustion events = %+v", events)
	}

	cancelLimiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm()})
	cancelLimiter.mu.Lock()
	granted, err := cancelLimiter.newPermitLocked(time.Time{}, Metadata{})
	if err != nil {
		cancelLimiter.mu.Unlock()
		t.Fatal(err)
	}
	wait := &waiter{result: make(chan acquireResult, 1)}
	wait.result <- acquireResult{permit: granted}
	cancelLimiter.mu.Unlock()
	_, _ = cancelLimiter.cancelWaiter(wait, context.Canceled)
	if snapshot := cancelLimiter.Snapshot(); snapshot.InFlight != 0 || snapshot.Outcomes.Ignored != 1 {
		t.Fatalf("grant-cancel snapshot = %+v", snapshot)
	}
	emptyWait := &waiter{result: make(chan acquireResult, 1)}
	if _, err = cancelLimiter.cancelWaiter(emptyWait, context.DeadlineExceeded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("empty cancellation error = %v", err)
	}

	queuedEvents := make([]Event, 0, 3)
	exhaustedQueue := mustInternalLimiter(t, Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(),
		Queue:    QueueConfig{MaxQueued: 1, MaxWait: time.Second},
		Observer: func(event Event) { queuedEvents = append(queuedEvents, event) },
	})
	active, err := exhaustedQueue.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exhaustedQueue.mu.Lock()
	exhaustedQueue.nextID = math.MaxUint64
	exhaustedWait := &waiter{result: make(chan acquireResult, 1)}
	exhaustedQueue.queue = append(exhaustedQueue.queue, exhaustedWait)
	exhaustedQueue.mu.Unlock()
	if err = active.Complete(OutcomeSuccess); err != nil {
		t.Fatal(err)
	}
	if result := <-exhaustedWait.result; !errors.Is(result.err, ErrIdentifierExhausted) || result.permit != nil {
		t.Fatalf("exhausted queued result = %+v", result)
	}
	if snapshot := exhaustedQueue.Snapshot(); snapshot.Rejections != 1 || snapshot.Queued != 0 || snapshot.InFlight != 0 {
		t.Fatalf("exhausted queued snapshot = %+v", snapshot)
	}
	if len(queuedEvents) != 3 || queuedEvents[2].Type != EventRejected {
		t.Fatalf("exhausted queued events = %+v", queuedEvents)
	}

	for name, algorithm := range map[string]*testAlgorithm{
		"reset panic":   {name: "fault", state: validState, panicReset: true},
		"state panic":   {name: "fault", state: validState, panicState: true},
		"invalid state": {name: "fault", state: AlgorithmState{Name: "", Estimate: 1}},
	} {
		candidate := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: &testAlgorithm{name: "fault", state: validState}})
		candidate.config.algorithm = algorithm
		candidate.Reset()
		if got := candidate.Snapshot().AlgorithmErrors; got != 1 {
			t.Errorf("%s algorithm errors = %d, want 1", name, got)
		}
	}
}

func TestSamplingMutationBoundaries(t *testing.T) {
	t.Parallel()

	newSamplingLimiter := func(config SamplingConfig) *Limiter {
		return mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 10, InitialLimit: 5, Algorithm: NewFixedAlgorithm(), Sampling: config})
	}
	config := SamplingConfig{MinDuration: 2, MaxDuration: 4, MinSamples: 2, Capacity: 2, Quantile: 0.5, BaselineSmoothing: 0.5, MaxIncrease: 2, MaxDecrease: 2}
	limiter := newSamplingLimiter(config)
	limiter.mu.Lock()
	if window := limiter.addSampleLocked(time.Unix(0, 1), 2, OutcomeSuccess); window != nil {
		t.Fatalf("sparse window = %+v", window)
	}
	window := limiter.addSampleLocked(time.Unix(0, 3), 4, OutcomeDependencyFailure)
	limiter.mu.Unlock()
	if window == nil || window.Samples != 2 || window.RecentLatency != 2 || window.BaselineLatency != 2 || window.Throughput != float64(2)/(2*time.Nanosecond).Seconds() {
		t.Fatalf("exact sampling window = %+v", window)
	}

	sparse := newSamplingLimiter(SamplingConfig{MinDuration: 1, MaxDuration: 2, MinSamples: 3, Capacity: 3, Quantile: 1, BaselineSmoothing: 1, MaxIncrease: 1, MaxDecrease: 1})
	sparse.mu.Lock()
	_ = sparse.addSampleLocked(time.Unix(0, 1), 1, OutcomeSuccess)
	_ = sparse.addSampleLocked(time.Unix(0, 3), 2, OutcomeSuccess)
	if len(sparse.recent) != 2 {
		t.Fatalf("exact max duration reset samples: %v", sparse.recent)
	}
	_ = sparse.addSampleLocked(time.Unix(0, 4), 3, OutcomeSuccess)
	sparse.mu.Unlock()
	if len(sparse.recent) != 1 || sparse.recent[0] != 3 {
		t.Fatalf("expired sparse samples = %v", sparse.recent)
	}

	full := newSamplingLimiter(SamplingConfig{MinDuration: 1, MaxDuration: 2, MinSamples: 3, Capacity: 3, Quantile: 1, BaselineSmoothing: 1, MaxIncrease: 1, MaxDecrease: 1})
	full.mu.Lock()
	full.windowStart = time.Unix(0, 1)
	full.recent = []time.Duration{1, 2, 3}
	full.adapting = true
	_ = full.addSampleLocked(time.Unix(0, 4), 4, OutcomeSuccess)
	full.mu.Unlock()
	if len(full.recent) != 3 || full.recent[0] != 4 {
		t.Fatalf("full ring samples = %v", full.recent)
	}

	baseline := newSamplingLimiter(SamplingConfig{MinDuration: 1, MaxDuration: 10, MinSamples: 2, Capacity: 2, Quantile: 1, BaselineSmoothing: 0.5, MaxIncrease: 1, MaxDecrease: 1})
	baseline.mu.Lock()
	baseline.baseline = 2
	_ = baseline.addSampleLocked(time.Unix(0, 1), 2, OutcomeSuccess)
	smoothed := baseline.addSampleLocked(time.Unix(0, 2), 4, OutcomeSuccess)
	baseline.mu.Unlock()
	if smoothed == nil || smoothed.RecentLatency != 4 || smoothed.BaselineLatency != 3 || smoothed.Throughput != float64(2)/time.Nanosecond.Seconds() {
		t.Fatalf("smoothed window = %+v", smoothed)
	}

	zeroElapsed := newSamplingLimiter(SamplingConfig{MinDuration: 1, MaxDuration: 10, MinSamples: 1, Capacity: 1, Quantile: 1, BaselineSmoothing: 1, MaxIncrease: 1, MaxDecrease: 1})
	zeroElapsed.config.sampling.MinDuration = 0
	zeroElapsed.mu.Lock()
	instant := zeroElapsed.addSampleLocked(time.Unix(0, 1), 1, OutcomeOverload)
	zeroElapsed.mu.Unlock()
	if instant == nil || instant.Throughput != 1/time.Nanosecond.Seconds() {
		t.Fatalf("zero-elapsed window = %+v", instant)
	}

	clockFailure := &sequenceClock{times: []time.Time{{}}, panicAfter: true}
	clockLimiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: clockFailure})
	clockPermit, err := clockLimiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = clockPermit.Complete(OutcomeIgnored); err != nil {
		t.Fatal(err)
	}
	if snapshot := clockLimiter.Snapshot(); snapshot.ClockErrors != 1 {
		t.Fatalf("clock-only failure snapshot = %+v", snapshot)
	}
	backwardClock := &sequenceClock{times: []time.Time{time.Unix(0, 2), time.Unix(0, 1)}}
	backwardLimiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: backwardClock})
	backwardPermit, err := backwardLimiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = backwardPermit.Complete(OutcomeIgnored); err != nil {
		t.Fatal(err)
	}
	if snapshot := backwardLimiter.Snapshot(); snapshot.ClockErrors != 1 {
		t.Fatalf("backward-clock snapshot = %+v", snapshot)
	}
	equalClock := &sequenceClock{times: []time.Time{time.Unix(0, 2), time.Unix(0, 2)}}
	equalLimiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: equalClock})
	equalPermit, err := equalLimiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = equalPermit.Complete(OutcomeIgnored); err != nil {
		t.Fatal(err)
	}
	if snapshot := equalLimiter.Snapshot(); snapshot.ClockErrors != 0 {
		t.Fatalf("equal-clock snapshot = %+v", snapshot)
	}
}

func TestApplyWindowMutationBoundaries(t *testing.T) {
	t.Parallel()

	validState := AlgorithmState{Name: "fault", Estimate: 1}
	for name, algorithm := range map[string]*testAlgorithm{
		"update panic":     {name: "fault", state: validState, panicUpdate: true},
		"state panic":      {name: "fault", state: validState, panicState: true},
		"invalid decision": {name: "fault", state: validState, useUpdateState: true, updateState: AlgorithmState{Name: "", Estimate: 1}},
		"invalid state":    {name: "fault", state: AlgorithmState{Name: "", Estimate: 1}, useUpdateState: true, updateState: validState},
		"state mismatch":   {name: "fault", state: AlgorithmState{Name: "fault", Estimate: 2}, useUpdateState: true, updateState: AlgorithmState{Name: "fault", Estimate: 1}},
	} {
		limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 10, InitialLimit: 5, Algorithm: &testAlgorithm{name: "fault", state: validState}, Sampling: SamplingConfig{MaxIncrease: 2, MaxDecrease: 2}})
		limiter.config.algorithm = algorithm
		_ = limiter.applyWindow(Window{CurrentLimit: 5}, time.Time{})
		if got := limiter.Snapshot().AlgorithmErrors; got != 1 {
			t.Errorf("%s algorithm errors = %d, want 1", name, got)
		}
	}

	algorithm := &testAlgorithm{name: "fault", state: validState, decisionLimit: -100}
	limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 10, InitialLimit: 5, Algorithm: algorithm, Sampling: SamplingConfig{MaxIncrease: 2, MaxDecrease: 2}})
	algorithm.state = validState
	if events := limiter.applyWindow(Window{CurrentLimit: 5}, time.Time{}); len(events) != 1 || limiter.Snapshot().Limit != 3 {
		t.Fatalf("bounded decrease events = %+v snapshot = %+v", events, limiter.Snapshot())
	}
}

func TestApplyWindowArithmeticIsPortableTo32Bit(t *testing.T) {
	t.Parallel()

	state := AlgorithmState{Name: "fault", Estimate: MaxLimit}
	algorithm := &testAlgorithm{name: "fault", state: state, decisionLimit: MaxLimit}
	limiter := mustInternalLimiter(t, Config{
		MinLimit: 1, MaxLimit: MaxLimit, InitialLimit: MaxLimit, Algorithm: algorithm,
		Sampling: SamplingConfig{MaxIncrease: MaxLimit, MaxDecrease: MaxLimit},
	})
	if events := limiter.applyWindow(Window{CurrentLimit: MaxLimit}, time.Time{}); len(events) != 0 || limiter.Snapshot().Limit != MaxLimit {
		t.Fatalf("maximum arithmetic events = %+v snapshot = %+v", events, limiter.Snapshot())
	}
}

func TestResetCannotInterleaveBetweenAlgorithmDecisionAndApplication(t *testing.T) {
	algorithm := &orderingAlgorithm{
		state:       AlgorithmState{Name: "ordering", Estimate: 1},
		updateState: make(chan struct{}),
		secondReset: make(chan struct{}),
	}
	limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 2, InitialLimit: 1, Algorithm: algorithm})
	limiter.mu.Lock()
	applyDone := make(chan struct{})
	go func() {
		_ = limiter.applyWindow(Window{CurrentLimit: 1}, time.Time{})
		close(applyDone)
	}()
	<-algorithm.updateState
	resetDone := make(chan struct{})
	go func() {
		limiter.Reset()
		close(resetDone)
	}()
	select {
	case <-algorithm.secondReset:
		limiter.mu.Unlock()
		<-applyDone
		<-resetDone
		t.Fatal("Reset entered the algorithm between decision and state application")
	case <-time.After(10 * time.Millisecond):
	}
	limiter.mu.Unlock()
	<-applyDone
	<-resetDone
}

type orderingAlgorithm struct {
	state       AlgorithmState
	updateState chan struct{}
	secondReset chan struct{}
	resets      int
	updated     bool
}

func (*orderingAlgorithm) Name() string { return "ordering" }
func (*orderingAlgorithm) algorithm()   {}
func (algorithm *orderingAlgorithm) Update(Window) Decision {
	algorithm.updated = true
	return Decision{Limit: 1, State: algorithm.state}
}
func (algorithm *orderingAlgorithm) Reset(int) {
	algorithm.resets++
	if algorithm.resets == 2 {
		close(algorithm.secondReset)
	}
}
func (algorithm *orderingAlgorithm) State() AlgorithmState {
	if algorithm.updated {
		algorithm.updated = false
		close(algorithm.updateState)
	}
	return algorithm.state
}
