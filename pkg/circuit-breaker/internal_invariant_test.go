package breaker

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

type invalidOpenDuration struct{}

func (invalidOpenDuration) openDurationPolicy() {}

func TestPermitInternalLifecycleGuardsAreIdempotent(t *testing.T) {
	t.Parallel()

	b, err := New(Config{Name: "inventory"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	completed := &Permit{breaker: b, status: permitCompleted}
	completed.expireLocked()
	if completed.status != permitCompleted {
		t.Fatalf("expireLocked() changed completed status to %v", completed.status)
	}

	b.state = StateHalfOpen
	missing := &Permit{breaker: b, generation: b.generation, state: StateHalfOpen}
	missing.releaseHalfOpenLocked()
	if b.halfOpenActive != 0 {
		t.Fatalf("releaseHalfOpenLocked() active count = %d", b.halfOpenActive)
	}
}

func TestInternalValidatedPolicyDefaultsAreUnreachable(t *testing.T) {
	t.Parallel()

	assertPanic(t, func() { _ = modeReason(Mode(99)) })
	b := &Breaker{config: normalizedConfig{openDuration: invalidOpenDuration{}}}
	assertPanic(t, func() { _ = b.openDurationLocked(0) })
}

func TestSealedPolicyMarkersImplementInternalContracts(t *testing.T) {
	t.Parallel()

	(CountWindow{}).windowConfig()
	(TimeWindow{}).windowConfig()
	FixedOpenDuration(1).openDurationPolicy()
	(ExponentialOpenDuration{}).openDurationPolicy()
	(RejectExcessProbes{}).halfOpenAdmissionPolicy()
	(WaitForProbe{}).halfOpenAdmissionPolicy()
	(SynchronousEvents{}).eventDeliveryPolicy()
	(AsynchronousEvents{}).eventDeliveryPolicy()
}

func TestSystemClockAndRandomImplementRuntimeContracts(t *testing.T) {
	t.Parallel()

	clock := systemClock{}
	timer := clock.NewTimer(time.Nanosecond)
	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("system timer did not fire")
	}
	if sample := (standardRandom{}).Float64(); sample < 0 || sample >= 1 {
		t.Fatalf("standard random sample = %v", sample)
	}
}

func TestInternalBoundaryContracts(t *testing.T) {
	t.Parallel()

	if got := elapsed(time.Unix(1, 0), time.Unix(1, 0)); got != 0 {
		t.Fatalf("elapsed(equal) = %v, want zero", got)
	}
	if openingDecision(OpeningRules{Combination: OpenWhenAll}, 0, 0, window.Snapshot{}) {
		t.Fatal("openingDecision() opened with no enabled rules")
	}
	if _, _, err := normalizeWindow(CountWindow{Size: 0}); err == nil {
		t.Fatal("normalizeWindow(zero count size) error = nil")
	}
	if _, err := normalizeHalfOpen(&HalfOpenPolicy{}); err == nil {
		t.Fatal("normalizeHalfOpen(zero probes) error = nil")
	}
	for _, value := range []float64{0, 1} {
		if err := validateRatio("ratio", value); err != nil {
			t.Fatalf("validateRatio(%v) error = %v", value, err)
		}
	}
	for _, value := range []float64{math.Nextafter(0, -1), math.Nextafter(1, 2)} {
		if err := validateRatio("ratio", value); err == nil {
			t.Fatalf("validateRatio(%v) error = nil", value)
		}
	}
}

func TestPermitEpochMatchingDistinguishesStateAndGeneration(t *testing.T) {
	t.Parallel()

	b := &Breaker{state: StateClosed, generation: 2}
	if !(&Permit{breaker: b, state: StateClosed, generation: 2}).belongsToCurrentStateLocked() {
		t.Fatal("matching permit epoch was rejected")
	}
	if (&Permit{breaker: b, state: StateOpen, generation: 2}).belongsToCurrentStateLocked() {
		t.Fatal("state-mismatched permit epoch was accepted")
	}
	if (&Permit{breaker: b, state: StateClosed, generation: 1}).belongsToCurrentStateLocked() {
		t.Fatal("generation-mismatched permit epoch was accepted")
	}
}

func TestNormalizationAcceptsExactResourceBounds(t *testing.T) {
	t.Parallel()

	config := Config{
		Name:              strings.Repeat("n", MaxNameBytes),
		Window:            CountWindow{Size: window.MaxCountSize},
		MinimumThroughput: window.MaxCountSize,
		Opening: &OpeningRules{
			FailureCount: uint64(window.MaxCountSize),
			SlowCount:    uint64(window.MaxCountSize),
		},
		HalfOpen: &HalfOpenPolicy{
			MaxProbes:         MaxHalfOpenProbes,
			RequiredSuccesses: MaxHalfOpenProbes,
		},
	}
	if _, err := normalizeConfig(config); err != nil {
		t.Fatalf("normalizeConfig(exact bounds) error = %v", err)
	}
	if _, err := normalizeObserver(func(TransitionEvent) error { return nil }, AsynchronousEvents{
		Buffer: MaxEventBuffer,
	}); err != nil {
		t.Fatalf("normalizeObserver(max buffer) error = %v", err)
	}
	maxBucketDuration := time.Duration(math.MaxInt64 / int64(window.MaxBucketCount))
	if _, _, err := normalizeWindow(TimeWindow{
		BucketDuration: maxBucketDuration,
		BucketCount:    window.MaxBucketCount,
	}); err != nil {
		t.Fatalf("normalizeWindow(exact bounds) error = %v", err)
	}
	if _, err := normalizeOpenDuration(ExponentialOpenDuration{
		Initial: time.Second, Multiplier: 1, Maximum: time.Second,
	}); err != nil {
		t.Fatalf("normalizeOpenDuration(equal maximum) error = %v", err)
	}
}

func assertPanic(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	operation()
}
