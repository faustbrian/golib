package faultinject_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestRuntimeRequiresExplicitAuthorizationAndBounds(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: time.Unix(100, 0)}
	injector := scopedInjector(t, faultinject.BoundaryHTTP,
		faultinject.ErrorFault(faultinject.PhaseBefore, errInjected))
	auditor := &auditRecorder{}
	var authorized atomic.Bool
	gate, err := faultinject.NewRuntime(faultinject.RuntimeConfig{
		Injector: injector,
		Authorizer: faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool {
			return authorized.Load()
		}),
		Allowlist:          []faultinject.Boundary{faultinject.BoundaryHTTP},
		ExpiresAt:          clock.now.Add(time.Minute),
		MaximumEvaluations: 1,
		Clock:              clock,
		Auditor:            auditor,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	metadata := faultinject.Metadata{Boundary: faultinject.BoundaryHTTP, Operation: 7}
	if gate.Decide(context.Background(), metadata).Injected() {
		t.Fatal("denied runtime decision injected")
	}
	authorized.Store(true)
	if !gate.Decide(context.Background(), metadata).Injected() {
		t.Fatal("authorized runtime decision did not inject")
	}
	if gate.Decide(context.Background(), metadata).Injected() {
		t.Fatal("runtime budget allowed a second evaluation")
	}
	gate.Disable()
	if gate.Decide(context.Background(), metadata).Injected() {
		t.Fatal("disabled runtime injected")
	}

	if got := auditor.outcomes(); !equalOutcomes(got, []faultinject.AuditOutcome{
		faultinject.AuditDenied,
		faultinject.AuditEvaluated,
		faultinject.AuditBudgetExhausted,
		faultinject.AuditDisabled,
	}) {
		t.Fatalf("audit outcomes = %v", got)
	}
	if snapshot := gate.Snapshot(); !snapshot.Disabled || snapshot.Evaluations != 1 || snapshot.Remaining != 0 {
		t.Fatalf("runtime snapshot = %+v", snapshot)
	}
}

func TestRuntimeExpiryAndAllowlistFailClosed(t *testing.T) {
	t.Parallel()

	clock := &fixedClock{now: time.Unix(100, 0)}
	gate, err := faultinject.NewRuntime(faultinject.RuntimeConfig{
		Injector: scopedInjector(t, faultinject.BoundaryHTTP,
			faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)),
		Authorizer:         faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist:          []faultinject.Boundary{faultinject.BoundaryHTTP},
		ExpiresAt:          clock.now,
		MaximumEvaluations: 1,
		Clock:              clock,
		Auditor:            &auditRecorder{},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if gate.Decide(context.Background(), faultinject.Metadata{Boundary: faultinject.BoundaryHTTP}).Injected() {
		t.Fatal("expired gate injected")
	}

	clock.now = clock.now.Add(-time.Second)
	if gate.Decide(context.Background(), faultinject.Metadata{Boundary: faultinject.BoundaryConn}).Injected() {
		t.Fatal("non-allowlisted boundary injected")
	}
}

func TestRuntimeBudgetIsConcurrentAndExact(t *testing.T) {
	t.Parallel()

	const maximum = 32
	clock := &fixedClock{now: time.Unix(100, 0)}
	rule := ruleWithFault("runtime", faultinject.ErrorFault(faultinject.PhaseBefore, errInjected))
	rule.Maximum = maximum
	rule.Scope = faultinject.BoundaryHTTP
	gate, err := faultinject.NewRuntime(faultinject.RuntimeConfig{
		Injector:           injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{rule}}),
		Authorizer:         faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist:          []faultinject.Boundary{faultinject.BoundaryHTTP},
		ExpiresAt:          clock.now.Add(time.Minute),
		MaximumEvaluations: maximum,
		Clock:              clock,
		Auditor:            faultinject.AuditorFunc(func(faultinject.AuditEvent) {}),
	})
	if err != nil {
		t.Fatal(err)
	}

	var injected atomic.Uint64
	var wait sync.WaitGroup
	for range maximum * 4 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if gate.Decide(context.Background(), faultinject.Metadata{Boundary: faultinject.BoundaryHTTP}).Injected() {
				injected.Add(1)
			}
		}()
	}
	wait.Wait()
	if injected.Load() != maximum {
		t.Fatalf("injections = %d, want %d", injected.Load(), maximum)
	}
}

func TestRuntimeRejectsIncompleteSafetyConfiguration(t *testing.T) {
	t.Parallel()

	valid := faultinject.RuntimeConfig{
		Injector:           scopedInjector(t, faultinject.BoundaryHTTP, faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)),
		Authorizer:         faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist:          []faultinject.Boundary{faultinject.BoundaryHTTP},
		ExpiresAt:          time.Now().Add(time.Hour),
		MaximumEvaluations: 1,
		Auditor:            faultinject.AuditorFunc(func(faultinject.AuditEvent) {}),
	}
	tests := map[string]func(*faultinject.RuntimeConfig){
		"injector":   func(config *faultinject.RuntimeConfig) { config.Injector = nil },
		"authorizer": func(config *faultinject.RuntimeConfig) { config.Authorizer = nil },
		"allowlist":  func(config *faultinject.RuntimeConfig) { config.Allowlist = nil },
		"expiry":     func(config *faultinject.RuntimeConfig) { config.ExpiresAt = time.Time{} },
		"budget":     func(config *faultinject.RuntimeConfig) { config.MaximumEvaluations = 0 },
		"auditor":    func(config *faultinject.RuntimeConfig) { config.Auditor = nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configuration := valid
			mutate(&configuration)
			if _, err := faultinject.NewRuntime(configuration); !errors.Is(err, faultinject.ErrInvalidConfig) {
				t.Fatalf("NewRuntime() error = %v", err)
			}
		})
	}
}

func TestRuntimeRejectsInvalidAllowlistsAndTypedNilClock(t *testing.T) {
	t.Parallel()

	valid := faultinject.RuntimeConfig{
		Injector:           scopedInjector(t, faultinject.BoundaryHTTP, faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)),
		Authorizer:         faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist:          []faultinject.Boundary{faultinject.BoundaryHTTP},
		ExpiresAt:          time.Now().Add(time.Hour),
		MaximumEvaluations: 2,
		Auditor:            faultinject.AuditorFunc(func(faultinject.AuditEvent) {}),
	}
	var typedNilClock *fixedClock
	var typedNilAuthorizer *authorizerStub
	var typedNilAuditor *auditRecorder
	for name, mutate := range map[string]func(*faultinject.RuntimeConfig){
		"typed nil clock":      func(config *faultinject.RuntimeConfig) { config.Clock = typedNilClock },
		"typed nil authorizer": func(config *faultinject.RuntimeConfig) { config.Authorizer = typedNilAuthorizer },
		"typed nil auditor":    func(config *faultinject.RuntimeConfig) { config.Auditor = typedNilAuditor },
		"invalid boundary":     func(config *faultinject.RuntimeConfig) { config.Allowlist = []faultinject.Boundary{"unsafe boundary"} },
		"duplicate boundary": func(config *faultinject.RuntimeConfig) {
			config.Allowlist = []faultinject.Boundary{faultinject.BoundaryHTTP, faultinject.BoundaryHTTP}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configuration := valid
			mutate(&configuration)
			if _, err := faultinject.NewRuntime(configuration); !errors.Is(err, faultinject.ErrInvalidConfig) {
				t.Fatalf("NewRuntime() error = %v", err)
			}
		})
	}
}

type authorizerStub struct{}

func (*authorizerStub) Authorize(context.Context, faultinject.Metadata) bool { return true }

func TestRuntimeClockFailureFailsClosedAndFreshSnapshotShowsBudget(t *testing.T) {
	t.Parallel()

	recorder := &auditRecorder{}
	gate, err := faultinject.NewRuntime(faultinject.RuntimeConfig{
		Injector:           scopedInjector(t, faultinject.BoundaryHTTP, faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)),
		Authorizer:         faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist:          []faultinject.Boundary{faultinject.BoundaryHTTP},
		ExpiresAt:          time.Now().Add(time.Hour),
		MaximumEvaluations: 2,
		Clock:              panicClock{},
		Auditor:            recorder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := gate.Snapshot(); snapshot.Evaluations != 0 || snapshot.Remaining != 2 {
		t.Fatalf("fresh snapshot = %+v", snapshot)
	}
	if gate.Decide(context.Background(), faultinject.Metadata{Boundary: faultinject.BoundaryHTTP}).Injected() {
		t.Fatal("clock failure injected")
	}
	if got := recorder.outcomes(); !equalOutcomes(got, []faultinject.AuditOutcome{faultinject.AuditClockFailure}) {
		t.Fatalf("audit outcomes = %v", got)
	}
}

type auditRecorder struct {
	mu     sync.Mutex
	events []faultinject.AuditEvent
}

func (recorder *auditRecorder) Audit(event faultinject.AuditEvent) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *auditRecorder) outcomes() []faultinject.AuditOutcome {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := make([]faultinject.AuditOutcome, len(recorder.events))
	for index := range recorder.events {
		result[index] = recorder.events[index].Outcome
	}
	return result
}

func equalOutcomes(left, right []faultinject.AuditOutcome) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
