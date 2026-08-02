package faultinject_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestExactConfigurationLimitsAreAccepted(t *testing.T) {
	t.Parallel()

	maximumRules := make([]faultinject.Rule, 1024)
	for index := range maximumRules {
		maximumRules[index] = validRule(fmt.Sprintf("rule-%04d", index))
	}
	maximumFaults := make([]faultinject.Fault, 256)
	for index := range maximumFaults {
		maximumFaults[index] = faultinject.CancelFault(faultinject.PhaseBefore)
	}
	configurations := map[string]faultinject.Config{
		"one rule":   {MaxRules: 1, Rules: []faultinject.Rule{validRule("one")}},
		"1024 rules": {MaxRules: 1024, Rules: maximumRules},
		"one fault":  {MaxFaultsPerDecision: 1, Rules: []faultinject.Rule{validRule("one-fault")}},
		"256 faults": func() faultinject.Config {
			rule := validRule("many-faults")
			rule.Faults = maximumFaults
			return faultinject.Config{MaxFaultsPerDecision: 256, Rules: []faultinject.Rule{rule}}
		}(),
		"one nanosecond": func() faultinject.Config {
			rule := validRule("latency")
			rule.Faults = []faultinject.Fault{faultinject.LatencyFault(faultinject.PhaseBefore, time.Nanosecond)}
			return faultinject.Config{MaxLatency: time.Nanosecond, Rules: []faultinject.Rule{rule}}
		}(),
		"one byte": func() faultinject.Config {
			rule := validRule("one-byte")
			rule.Faults = []faultinject.Fault{faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, 1, 1)}
			return faultinject.Config{MaxBytes: 1, Rules: []faultinject.Rule{rule}}
		}(),
		"maximum bytes": func() faultinject.Config {
			rule := validRule("maximum-bytes")
			rule.Faults = []faultinject.Fault{faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, 16*1024*1024, 1)}
			return faultinject.Config{MaxBytes: 16 * 1024 * 1024, Rules: []faultinject.Rule{rule}}
		}(),
		"maximum duplicate bytes": func() faultinject.Config {
			rule := validRule("maximum-duplicate")
			rule.Faults = []faultinject.Fault{faultinject.ByteFault(faultinject.KindDuplicate, faultinject.PhaseAfter, 16*1024*1024, 0)}
			return faultinject.Config{MaxBytes: 16 * 1024 * 1024, Rules: []faultinject.Rule{rule}}
		}(),
		"maximum activation": func() faultinject.Config {
			rule := validRule("maximum-activation")
			rule.Maximum = 1_000_000_000
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
		"maximum sequence": func() faultinject.Config {
			rule := validRule("maximum-sequence")
			rule.Schedule = faultinject.Sequence(make([]bool, 1024), false)
			return faultinject.Config{Rules: []faultinject.Rule{rule}}
		}(),
	}
	for name, configuration := range configurations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := faultinject.New(configuration); err != nil {
				t.Fatalf("New() error = %v", err)
			}
		})
	}
}

func TestIdentityAlphabetAndLengthBoundaries(t *testing.T) {
	t.Parallel()

	for _, identity := range []string{"a", "z", "A", "Z", "0", "9", "-", "_", ".", strings.Repeat("a", 64)} {
		rule := validRule(identity)
		if _, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{rule}}); err != nil {
			t.Fatalf("identity %q rejected: %v", identity, err)
		}
	}
	for _, identity := range []string{"/", "`", "[", "@", ":", strings.Repeat("a", 65)} {
		rule := validRule(identity)
		if _, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{rule}}); !errors.Is(err, faultinject.ErrInvalidConfig) {
			t.Fatalf("identity %q error = %v", identity, err)
		}
	}
}

func TestRuleEligibilityOrderingLimitsAndObservation(t *testing.T) {
	t.Parallel()

	var observed atomic.Uint64
	rules := []faultinject.Rule{
		func() faultinject.Rule {
			rule := validRule("inactive")
			rule.Activation = faultinject.Inactive
			return rule
		}(),
		func() faultinject.Rule {
			rule := validRule("scope")
			rule.Scope = faultinject.BoundaryHTTP
			return rule
		}(),
		func() faultinject.Rule {
			rule := validRule("predicate")
			rule.Predicate = func(faultinject.Metadata) bool { return false }
			return rule
		}(),
		func() faultinject.Rule {
			rule := validRule("later")
			rule.Order = 10
			rule.Observation = faultinject.Observe
			rule.Faults = []faultinject.Fault{faultinject.CancelFault(faultinject.PhaseBefore)}
			return rule
		}(),
		func() faultinject.Rule {
			rule := validRule("earlier")
			rule.Order = -10
			rule.Observation = faultinject.Suppress
			return rule
		}(),
	}
	injector := injectorWithConfig(t, faultinject.Config{
		Observer: faultinject.ObserverFunc(func(faultinject.Event) { observed.Add(1) }),
		Rules:    rules,
	})
	decision := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
	faults := decision.Faults()
	if len(faults) != 2 || faults[0].Kind != faultinject.KindError || faults[1].Kind != faultinject.KindCancel {
		t.Fatalf("ordered faults = %#v", faults)
	}
	if observed.Load() != 1 {
		t.Fatalf("observed events = %d", observed.Load())
	}
	snapshot := injector.Snapshot()
	wantCalls := map[string]uint64{"earlier": 1, "later": 1}
	for _, rule := range snapshot.Rules {
		if rule.Calls != wantCalls[rule.ID] {
			t.Fatalf("rule %s calls = %d", rule.ID, rule.Calls)
		}
	}
	if injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected() {
		t.Fatal("maximum-one rules injected twice")
	}
}

func TestNilInjectorMethodsAreInert(t *testing.T) {
	t.Parallel()

	var injector *faultinject.Injector
	if injector.Decide(faultinject.Metadata{}).Injected() || injector.Snapshot().Generation != 0 {
		t.Fatal("nil injector was not inert")
	}
}

func TestRepeatingSequenceUsesModuloPosition(t *testing.T) {
	t.Parallel()

	rule := validRule("repeat")
	rule.Maximum = 4
	rule.Schedule = faultinject.Sequence([]bool{true, false, false}, true)
	injector := injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{rule}})
	want := []bool{true, false, false, true, false, false, true}
	for call, expected := range want {
		if got := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected(); got != expected {
			t.Fatalf("call %d = %t, want %t", call+1, got, expected)
		}
	}
}

func TestRuntimeExactBoundsAndRemainingBudget(t *testing.T) {
	t.Parallel()

	allowlist := make([]faultinject.Boundary, 64)
	for index := range allowlist {
		allowlist[index] = faultinject.Boundary(fmt.Sprintf("boundary-%02d", index))
	}
	clock := &fixedClock{now: time.Unix(10, 0)}
	gate, err := faultinject.NewRuntime(faultinject.RuntimeConfig{
		Injector:           scopedInjector(t, allowlist[0], faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)),
		Authorizer:         faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist:          allowlist,
		ExpiresAt:          clock.now.Add(time.Hour),
		MaximumEvaluations: 1_000_000_000,
		Clock:              clock,
		Auditor:            faultinject.AuditorFunc(func(faultinject.AuditEvent) {}),
	})
	if err != nil {
		t.Fatal(err)
	}
	gate.Decide(context.Background(), faultinject.Metadata{Boundary: allowlist[0]})
	if snapshot := gate.Snapshot(); snapshot.Evaluations != 1 || snapshot.Remaining != 999_999_999 {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	tooMany := append(append([]faultinject.Boundary(nil), allowlist...), "boundary-64")
	if _, err := faultinject.NewRuntime(faultinject.RuntimeConfig{
		Injector: gateInjector(t), Authorizer: faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist: tooMany, ExpiresAt: clock.now.Add(time.Hour), MaximumEvaluations: 1,
		Clock: clock, Auditor: faultinject.AuditorFunc(func(faultinject.AuditEvent) {}),
	}); !errors.Is(err, faultinject.ErrInvalidConfig) {
		t.Fatalf("65-boundary error = %v", err)
	}
}

func gateInjector(t *testing.T) *faultinject.Injector {
	t.Helper()
	return scopedInjector(t, faultinject.BoundaryHTTP, faultinject.ErrorFault(faultinject.PhaseBefore, errInjected))
}
