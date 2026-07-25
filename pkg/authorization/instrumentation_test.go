package authorization

import (
	"context"
	"errors"
	"testing"
	"time"
)

type instrumenterStub struct {
	startPanic  bool
	finishPanic bool
	event       Event
	started     bool
	derivedKey  any
}

func (instrumenter *instrumenterStub) Start(ctx context.Context) (context.Context, func(Event)) {
	instrumenter.started = true
	if instrumenter.startPanic {
		panic("start")
	}
	ctx = context.WithValue(ctx, instrumenter.derivedKey, true)
	return ctx, func(event Event) {
		instrumenter.event = event
		if instrumenter.finishPanic {
			panic("finish")
		}
	}
}

type authorizerFunc func(context.Context, Request) (Decision, error)

func (authorize authorizerFunc) Decide(ctx context.Context, request Request) (Decision, error) {
	return authorize(ctx, request)
}

func TestInstrumentedAuthorizerEmitsBoundedEvent(t *testing.T) {
	t.Parallel()

	key := &struct{}{}
	instrumenter := &instrumenterStub{derivedKey: key}
	times := []time.Time{
		time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 15, 10, 0, 0, int(5*time.Millisecond), time.UTC),
	}
	index := 0
	authorizer, err := NewInstrumented(
		authorizerFunc(func(ctx context.Context, _ Request) (Decision, error) {
			if ctx.Value(key) != true {
				t.Error("authorizer did not receive derived instrumentation context")
			}
			return Decision{
				Outcome: Allow, Reason: "granted", Revision: 7,
				MatchedPolicyIDs: []PolicyID{"one", "two", "three"},
				Trace:            []TraceEntry{{PolicyID: "one"}}, TraceTruncated: true,
			}, nil
		}),
		instrumenter,
		InstrumentationConfig{
			Clock:        func() time.Time { value := times[index]; index++; return value },
			MaxPolicyIDs: 2,
		},
	)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	decision, err := authorizer.Decide(context.Background(), Request{})
	if err != nil || decision.Outcome != Allow {
		t.Fatalf("Decide() = (%+v, %v)", decision, err)
	}
	event := instrumenter.event
	if event.Outcome != Allow || event.Reason != "granted" || event.Revision != 7 ||
		event.Duration != 5*time.Millisecond || event.Failed ||
		len(event.MatchedPolicyIDs) != 2 || !event.MatchedPolicyIDsTruncated ||
		event.TraceCount != 1 || !event.TraceTruncated {
		t.Errorf("instrumentation event = %+v", event)
	}
	decision.MatchedPolicyIDs[0] = "changed"
	if event.MatchedPolicyIDs[0] != "one" {
		t.Error("event policy IDs alias decision data")
	}
}

func TestInstrumentedAuthorizerPreservesUpstreamDiagnosticTruncation(t *testing.T) {
	t.Parallel()

	instrumenter := &instrumenterStub{derivedKey: &struct{}{}}
	authorizer, err := NewInstrumented(
		authorizerFunc(func(context.Context, Request) (Decision, error) {
			return Decision{
				Outcome: Allow, MatchedPolicyIDs: []PolicyID{"one"},
				MatchedPolicyIDsTruncated: true,
			}, nil
		}),
		instrumenter,
		InstrumentationConfig{},
	)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	if _, err := authorizer.Decide(context.Background(), Request{}); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !instrumenter.event.MatchedPolicyIDsTruncated {
		t.Error("Event.MatchedPolicyIDsTruncated = false, want true")
	}
}

func TestInstrumentedAuthorizerPreservesFailuresAndIsolatesInstrumentation(t *testing.T) {
	t.Parallel()

	want := errors.New("evaluation failed")
	for _, instrumenter := range []*instrumenterStub{
		{startPanic: true, derivedKey: &struct{}{}},
		{finishPanic: true, derivedKey: &struct{}{}},
	} {
		authorizer, err := NewInstrumented(
			authorizerFunc(func(context.Context, Request) (Decision, error) {
				return Decision{Outcome: Deny, Reason: ReasonEvaluationError}, want
			}),
			instrumenter,
			InstrumentationConfig{Clock: func() time.Time { return time.Time{} }},
		)
		if err != nil {
			t.Fatalf("NewInstrumented() error = %v", err)
		}
		decision, err := authorizer.Decide(context.Background(), Request{})
		if !errors.Is(err, want) || decision.Outcome != Deny {
			t.Errorf("Decide() = (%+v, %v), want original failure", decision, err)
		}
		if !instrumenter.startPanic && !instrumenter.event.Failed {
			t.Errorf("failure event = %+v", instrumenter.event)
		}
	}
}

func TestInstrumentedAuthorizerValidatesConfigAndClampsClock(t *testing.T) {
	t.Parallel()

	instrumenter := &instrumenterStub{derivedKey: &struct{}{}}
	authorizer := authorizerFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Outcome: NotApplicable}, nil
	})
	if _, err := NewInstrumented(nil, instrumenter, InstrumentationConfig{}); !errors.Is(err, ErrNilAuthorizer) {
		t.Errorf("NewInstrumented(nil authorizer) error = %v", err)
	}
	if _, err := NewInstrumented(authorizer, nil, InstrumentationConfig{}); !errors.Is(err, ErrNilInstrumenter) {
		t.Errorf("NewInstrumented(nil instrumenter) error = %v", err)
	}
	if _, err := NewInstrumented(authorizer, instrumenter, InstrumentationConfig{MaxPolicyIDs: -1}); !errors.Is(err, ErrInvalidInstrumentationConfig) {
		t.Errorf("NewInstrumented(invalid config) error = %v", err)
	}
	if wrapped, err := NewInstrumented(authorizer, instrumenter, InstrumentationConfig{}); err != nil || wrapped == nil {
		t.Errorf("NewInstrumented(defaults) = (%v, %v)", wrapped, err)
	}

	times := []time.Time{time.Unix(2, 0), time.Unix(1, 0)}
	index := 0
	wrapped, err := NewInstrumented(authorizer, instrumenter, InstrumentationConfig{
		Clock: func() time.Time { value := times[index]; index++; return value },
	})
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	if _, err := wrapped.Decide(context.Background(), Request{}); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if instrumenter.event.Duration != 0 {
		t.Errorf("negative duration = %v, want zero", instrumenter.event.Duration)
	}
}
