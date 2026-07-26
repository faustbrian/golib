package breaker

import (
	"testing"
	"time"
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

func assertPanic(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	operation()
}
