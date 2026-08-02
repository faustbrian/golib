package faultinject_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestConfigurationBoundsAndDeclaredEnums(t *testing.T) {
	t.Parallel()

	valid := faultinject.Config{Rules: []faultinject.Rule{validRule("valid")}}
	tests := map[string]func(*faultinject.Config){
		"max rules below range":   func(config *faultinject.Config) { config.MaxRules = -1 },
		"max rules above range":   func(config *faultinject.Config) { config.MaxRules = 1025 },
		"max faults below range":  func(config *faultinject.Config) { config.MaxFaultsPerDecision = -1 },
		"max faults above range":  func(config *faultinject.Config) { config.MaxFaultsPerDecision = 257 },
		"max latency below range": func(config *faultinject.Config) { config.MaxLatency = -1 },
		"max bytes below range":   func(config *faultinject.Config) { config.MaxBytes = -1 },
		"max bytes above range":   func(config *faultinject.Config) { config.MaxBytes = 16*1024*1024 + 1 },
		"rules exceed configured max": func(config *faultinject.Config) {
			config.MaxRules = 1
			config.Rules = append(config.Rules, validRule("second"))
		},
		"terminal is undeclared":    func(config *faultinject.Config) { config.Rules[0].Terminal = 0 },
		"observation is undeclared": func(config *faultinject.Config) { config.Rules[0].Observation = 0 },
		"too many faults": func(config *faultinject.Config) {
			config.MaxFaultsPerDecision = 1
			config.Rules[0].Faults = append(config.Rules[0].Faults, faultinject.CancelFault(faultinject.PhaseAfter))
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configuration := valid
			configuration.Rules = append([]faultinject.Rule(nil), valid.Rules...)
			mutate(&configuration)
			_, err := faultinject.New(configuration)
			var configurationError *faultinject.InvalidConfigError
			if !errors.Is(err, faultinject.ErrInvalidConfig) || !errors.As(err, &configurationError) || configurationError.Error() == "" {
				t.Fatalf("New() error = %T %v", err, err)
			}
		})
	}
}

func TestConfigurationRejectsTypedNilDependencies(t *testing.T) {
	t.Parallel()

	var clock *fixedClock
	var sleeper *recordingSleeper
	var observer *observerStub
	for name, configuration := range map[string]faultinject.Config{
		"clock":    {Clock: clock},
		"sleeper":  {Sleeper: sleeper},
		"observer": {Observer: observer},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := faultinject.New(configuration); !errors.Is(err, faultinject.ErrInvalidConfig) {
				t.Fatalf("New() error = %v", err)
			}
		})
	}
}

func TestScheduleValidationAndBoundarySequences(t *testing.T) {
	t.Parallel()

	tests := map[string]faultinject.Schedule{
		"zero every":        faultinject.Every(0),
		"zero nth":          faultinject.Nth(0),
		"empty sequence":    faultinject.Sequence(nil, false),
		"oversize sequence": faultinject.Sequence(make([]bool, 1025), false),
		"unsupported":       struct{}{},
	}
	for name, schedule := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule := validRule("schedule")
			rule.Schedule = schedule
			if _, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{rule}}); !errors.Is(err, faultinject.ErrInvalidConfig) {
				t.Fatalf("New() error = %v", err)
			}
		})
	}

	for name, schedule := range map[string]faultinject.Schedule{
		"zero probability": faultinject.Probability(1, 0, 3),
		"full probability": faultinject.Probability(1, 3, 3),
		"finite sequence":  faultinject.Sequence([]bool{false}, false),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rule := validRule("boundary")
			rule.Maximum = 2
			rule.Schedule = schedule
			injector := injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{rule}})
			first := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected()
			second := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected()
			want := name == "full probability"
			if first != want || second != want {
				t.Fatalf("decisions = %t, %t, want %t", first, second, want)
			}
		})
	}
}

func TestFaultValidationAndAccessors(t *testing.T) {
	t.Parallel()

	fault := faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, 4, 0x20)
	if fault.Phase() != faultinject.PhaseAfter || fault.Limit() != 4 || fault.Mask() != 0x20 || fault.Error() != nil || fault.Delay() != 0 || fault.PanicValue() != "" {
		t.Fatalf("byte fault accessors returned unexpected values")
	}
	errorFault := faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)
	latencyFault := faultinject.LatencyFault(faultinject.PhaseDuring, time.Second)
	panicFault := faultinject.PanicFault(faultinject.PhaseAfter, "safe")
	if errorFault.Error() != errInjected || latencyFault.Delay() != time.Second || panicFault.PanicValue() != "safe" {
		t.Fatal("fault accessors lost constructor values")
	}

	invalidFaults := []faultinject.Fault{
		faultinject.ErrorFault(faultinject.PhaseBefore, nil),
		faultinject.LatencyFault(faultinject.PhaseBefore, 0),
		faultinject.LatencyFault(faultinject.PhaseBefore, -1),
		faultinject.PanicFault(faultinject.PhaseBefore, "unsafe value"),
		faultinject.ByteFault(faultinject.KindDuplicate, faultinject.PhaseAfter, 0, 0),
		faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, 0, 1),
		faultinject.ByteFault(faultinject.KindCorrupt, faultinject.PhaseAfter, 1, 0),
		faultinject.ByteFault("unknown", faultinject.PhaseAfter, 0, 0),
	}
	for index, invalidFault := range invalidFaults {
		rule := validRule(fmt.Sprintf("fault-%d", index))
		rule.Faults = []faultinject.Fault{invalidFault}
		if _, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{rule}}); !errors.Is(err, faultinject.ErrInvalidConfig) {
			t.Fatalf("fault %d New() error = %v", index, err)
		}
	}
}

func TestPrecedenceTieBreaksByIdentityAndCapsComposition(t *testing.T) {
	t.Parallel()

	first := validRule("b")
	second := validRule("a")
	injector := injectorWithConfig(t, faultinject.Config{
		MaxFaultsPerDecision: 1,
		Rules:                []faultinject.Rule{first, second},
	})
	decision := injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction})
	if len(decision.Faults()) != 1 {
		t.Fatalf("fault count = %d", len(decision.Faults()))
	}
	if got := injector.Snapshot().Rules; !reflect.DeepEqual(got, []faultinject.RuleSnapshot{{ID: "a", Calls: 1, Injections: 1}, {ID: "b"}}) {
		t.Fatalf("precedence snapshot = %#v", got)
	}
}

func TestCompositionTruncatesAtTheDeclaredDecisionBound(t *testing.T) {
	t.Parallel()

	first := validRule("first")
	first.Faults = []faultinject.Fault{
		faultinject.ErrorFault(faultinject.PhaseBefore, errInjected),
		faultinject.CancelFault(faultinject.PhaseAfter),
	}
	second := validRule("second")
	second.Order = 1
	second.Faults = []faultinject.Fault{
		faultinject.DeadlineFault(faultinject.PhaseAfter),
		faultinject.PanicFault(faultinject.PhaseAfter, "bounded"),
	}
	third := validRule("third")
	third.Order = 2
	injector := injectorWithConfig(t, faultinject.Config{
		MaxFaultsPerDecision: 3,
		Rules:                []faultinject.Rule{first, second, third},
	})
	if got := len(injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Faults()); got != 3 {
		t.Fatalf("fault count = %d, want 3", got)
	}
	for _, snapshot := range injector.Snapshot().Rules {
		if snapshot.ID == "third" && (snapshot.Calls != 0 || snapshot.Injections != 0) {
			t.Fatalf("rule after decision bound was evaluated: %+v", snapshot)
		}
	}
}

func TestDuringLatencyFailureAndPanicAreApplied(t *testing.T) {
	t.Parallel()

	injector := injectorWithConfig(t, faultinject.Config{
		Sleeper: errorSleeper{err: errInjected},
		Rules: []faultinject.Rule{ruleWithFault("latency-error",
			faultinject.LatencyFault(faultinject.PhaseDuring, time.Second))},
	})
	called := false
	_, err := faultinject.Run(context.Background(), injector, faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, func(context.Context) (int, error) {
		called = true
		return 0, nil
	})
	if !errors.Is(err, errInjected) || !called {
		t.Fatalf("Run() = %v, called=%t", err, called)
	}

	injector = injectorWithFault(t, faultinject.PanicFault(faultinject.PhaseDuring, "during_panic"))
	defer func() {
		if recovered := recover(); recovered != "during_panic" {
			t.Fatalf("panic = %#v", recovered)
		}
	}()
	_, _ = faultinject.Run(context.Background(), injector, faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, func(context.Context) (int, error) {
		return 0, nil
	})
}

func TestPredicateAndObservationPanicsAreContained(t *testing.T) {
	t.Parallel()

	clock := panicClock{}
	rule := validRule("panic-safe")
	rule.Predicate = func(faultinject.Metadata) bool { panic("predicate") }
	injector := injectorWithConfig(t, faultinject.Config{Clock: clock, Rules: []faultinject.Rule{rule}})
	if injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected() {
		t.Fatal("panicking predicate matched")
	}

	rule.Predicate = nil
	rule.Observation = faultinject.Observe
	injector = injectorWithConfig(t, faultinject.Config{
		Clock:    clock,
		Observer: faultinject.ObserverFunc(func(faultinject.Event) { panic("observer") }),
		Rules:    []faultinject.Rule{rule},
	})
	if !injector.Decide(faultinject.Metadata{Boundary: faultinject.BoundaryFunction}).Injected() {
		t.Fatal("observer or clock panic changed selection")
	}
}

func TestNilResetAndNoMatchRunRemainInert(t *testing.T) {
	t.Parallel()

	var injector *faultinject.Injector
	if injector.Reset() != 0 {
		t.Fatal("nil reset was not inert")
	}
	active := injectorWithConfig(t, faultinject.Config{})
	value, err := faultinject.Run(context.Background(), active, faultinject.Metadata{}, func(context.Context) (string, error) {
		return "direct", nil
	})
	if value != "direct" || err != nil {
		t.Fatalf("Run() = %q, %v", value, err)
	}
}

type observerStub struct{}

func (*observerStub) Observe(faultinject.Event) {}

type panicClock struct{}

func (panicClock) Now() time.Time { panic("clock") }

type errorSleeper struct{ err error }

func (sleeper errorSleeper) Sleep(context.Context, time.Duration) error { return sleeper.err }

func TestRuntimeAuditSanitizesHostileMetadata(t *testing.T) {
	t.Parallel()

	recorder := &auditRecorder{}
	gate, err := faultinject.NewRuntime(faultinject.RuntimeConfig{
		Injector:           scopedInjector(t, faultinject.BoundaryHTTP, faultinject.ErrorFault(faultinject.PhaseBefore, errInjected)),
		Authorizer:         faultinject.AuthorizerFunc(func(context.Context, faultinject.Metadata) bool { return true }),
		Allowlist:          []faultinject.Boundary{faultinject.BoundaryHTTP},
		ExpiresAt:          time.Now().Add(time.Hour),
		MaximumEvaluations: 2,
		Auditor:            recorder,
	})
	if err != nil {
		t.Fatal(err)
	}
	gate.Decide(context.Background(), faultinject.Metadata{Boundary: faultinject.Boundary(strings.Repeat("secret", 100))})
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.events) != 1 || recorder.events[0].Metadata.Boundary != "invalid" {
		t.Fatalf("audit metadata = %+v", recorder.events)
	}
}
