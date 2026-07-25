package authorization

import (
	"context"
	"errors"
	"time"
)

const defaultMaxInstrumentationPolicyIDs = 100

var (
	ErrNilAuthorizer                = errors.New("authorization instrumented authorizer is nil")
	ErrNilInstrumenter              = errors.New("authorization instrumenter is nil")
	ErrInvalidInstrumentationConfig = errors.New("authorization instrumentation config is invalid")
)

// Event contains bounded decision metadata without subject, resource, tenant,
// attribute, or policy-document contents.
type Event struct {
	Outcome                   Outcome
	Reason                    ReasonCode
	Revision                  Revision
	MatchedPolicyIDs          []PolicyID
	MatchedPolicyIDsTruncated bool
	TraceCount                int
	TraceTruncated            bool
	Duration                  time.Duration
	Failed                    bool
}

type Instrumenter interface {
	Start(context.Context) (context.Context, func(Event))
}

type InstrumentationConfig struct {
	Clock        func() time.Time
	MaxPolicyIDs int
}

type Instrumented struct {
	authorizer   Authorizer
	instrumenter Instrumenter
	clock        func() time.Time
	maxPolicyIDs int
}

func NewInstrumented(
	authorizer Authorizer,
	instrumenter Instrumenter,
	config InstrumentationConfig,
) (*Instrumented, error) {
	if authorizer == nil {
		return nil, ErrNilAuthorizer
	}
	if instrumenter == nil {
		return nil, ErrNilInstrumenter
	}
	if config.MaxPolicyIDs < 0 {
		return nil, ErrInvalidInstrumentationConfig
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.MaxPolicyIDs == 0 {
		config.MaxPolicyIDs = defaultMaxInstrumentationPolicyIDs
	}
	return &Instrumented{
		authorizer: authorizer, instrumenter: instrumenter,
		clock: config.Clock, maxPolicyIDs: config.MaxPolicyIDs,
	}, nil
}

func (instrumented *Instrumented) Decide(
	ctx context.Context,
	request Request,
) (Decision, error) {
	started := safeInstrumentationNow(instrumented.clock, time.Time{})
	next, finish := safeInstrumentationStart(instrumented.instrumenter, ctx)
	decision, err := instrumented.authorizer.Decide(next, request)
	finished := safeInstrumentationNow(instrumented.clock, started)
	duration := finished.Sub(started)
	if duration < 0 {
		duration = 0
	}
	matched := decision.MatchedPolicyIDs
	truncated := decision.MatchedPolicyIDsTruncated
	if len(matched) > instrumented.maxPolicyIDs {
		matched = matched[:instrumented.maxPolicyIDs]
		truncated = true
	}
	event := Event{
		Outcome: decision.Outcome, Reason: decision.Reason,
		Revision:                  decision.Revision,
		MatchedPolicyIDs:          append([]PolicyID(nil), matched...),
		MatchedPolicyIDsTruncated: truncated,
		TraceCount:                len(decision.Trace), TraceTruncated: decision.TraceTruncated,
		Duration: duration, Failed: err != nil,
	}
	safeInstrumentationFinish(finish, event)
	return decision, err
}

type Authorizer interface {
	Decide(context.Context, Request) (Decision, error)
}

func safeInstrumentationStart(
	instrumenter Instrumenter,
	ctx context.Context,
) (next context.Context, finish func(Event)) {
	next = ctx
	defer func() {
		if recover() != nil {
			next = ctx
			finish = nil
		}
	}()
	candidate, callback := instrumenter.Start(ctx)
	if candidate != nil {
		next = candidate
	}
	return next, callback
}

func safeInstrumentationFinish(finish func(Event), event Event) {
	if finish == nil {
		return
	}
	defer func() { _ = recover() }()
	finish(event)
}

func safeInstrumentationNow(clock func() time.Time, fallback time.Time) (now time.Time) {
	now = fallback
	defer func() { _ = recover() }()
	return clock()
}
