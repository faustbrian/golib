package authorization

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNilSnapshot = errors.New("authorization snapshot is nil")

var ErrBatchLimitExceeded = errors.New("authorization batch limit exceeded")

var ErrPolicyLimitExceeded = errors.New("authorization policy limit exceeded")

var ErrPolicyPanic = errors.New("authorization policy panicked")

var (
	ErrRevisionConflict     = errors.New("authorization revision conflict")
	ErrRevisionNotMonotonic = errors.New("authorization revision is not monotonic")
)

const (
	defaultMaxBatchSize        = 1000
	defaultMaxPolicies         = 1000
	defaultMaxTraceSize        = 100
	defaultMaxMatchedPolicyIDs = 100
)

// Limits bounds work and diagnostic cardinality for one engine.
type Limits struct {
	MaxBatchSize        int
	MaxPolicies         int
	MaxTraceSize        int
	MaxMatchedPolicyIDs int
}

type EngineOption func(*Engine)

// WithLimits configures positive limits and leaves zero-valued fields at safe
// defaults.
func WithLimits(limits Limits) EngineOption {
	return func(engine *Engine) {
		if limits.MaxBatchSize > 0 {
			engine.limits.MaxBatchSize = limits.MaxBatchSize
		}
		if limits.MaxPolicies > 0 {
			engine.limits.MaxPolicies = limits.MaxPolicies
		}
		if limits.MaxTraceSize > 0 {
			engine.limits.MaxTraceSize = limits.MaxTraceSize
		}
		if limits.MaxMatchedPolicyIDs > 0 {
			engine.limits.MaxMatchedPolicyIDs = limits.MaxMatchedPolicyIDs
		}
	}
}

// WithClock supplies the time used when a request omits Environment.Time.
func WithClock(clock func() time.Time) EngineOption {
	return func(engine *Engine) {
		if clock != nil {
			engine.clock = clock
		}
	}
}

// PolicyEvaluationError identifies a failed policy without exposing its
// internal error text. Unwrap retains programmatic error inspection.
type PolicyEvaluationError struct {
	PolicyID PolicyID
	Err      error
}

func (evaluationError *PolicyEvaluationError) Error() string {
	return fmt.Sprintf("policy %q evaluation failed", evaluationError.PolicyID)
}

func (evaluationError *PolicyEvaluationError) Unwrap() error {
	return evaluationError.Err
}

// Engine evaluates every request against one coherent snapshot.
type Engine struct {
	snapshot  atomic.Pointer[Snapshot]
	limits    Limits
	clock     func() time.Time
	replaceMu sync.Mutex
}

// NewEngine creates an engine from an immutable policy snapshot.
func NewEngine(snapshot *Snapshot, options ...EngineOption) (*Engine, error) {
	if snapshot == nil {
		return nil, ErrNilSnapshot
	}

	engine := &Engine{
		clock: time.Now,
		limits: Limits{
			MaxBatchSize:        defaultMaxBatchSize,
			MaxPolicies:         defaultMaxPolicies,
			MaxTraceSize:        defaultMaxTraceSize,
			MaxMatchedPolicyIDs: defaultMaxMatchedPolicyIDs,
		},
	}

	for _, option := range options {
		option(engine)
	}
	if len(snapshot.policies) > engine.limits.MaxPolicies {
		return nil, ErrPolicyLimitExceeded
	}
	engine.snapshot.Store(snapshot)

	return engine, nil
}

// Decide evaluates one request and denies when no policy applies.
func (engine *Engine) Decide(ctx context.Context, request Request) (Decision, error) {
	return engine.decide(ctx, request, engine.snapshot.Load())
}

// Revision returns the revision of the snapshot currently used for new
// decisions.
func (engine *Engine) Revision() Revision {
	return engine.snapshot.Load().revision
}

// ReplaceSnapshot atomically installs a newer snapshot when the caller's
// expected revision still matches the active view.
func (engine *Engine) ReplaceSnapshot(next *Snapshot, expected Revision) error {
	if next == nil {
		return ErrNilSnapshot
	}
	if len(next.policies) > engine.limits.MaxPolicies {
		return ErrPolicyLimitExceeded
	}

	engine.replaceMu.Lock()
	defer engine.replaceMu.Unlock()

	current := engine.snapshot.Load()
	if current.revision != expected {
		return ErrRevisionConflict
	}

	if next.revision <= current.revision {
		return ErrRevisionNotMonotonic
	}

	engine.snapshot.Store(next)

	return nil
}

// DecideBatch evaluates a bounded request set against exactly one snapshot.
func (engine *Engine) DecideBatch(
	ctx context.Context,
	requests []Request,
) ([]Decision, error) {
	if len(requests) > engine.limits.MaxBatchSize {
		return nil, ErrBatchLimitExceeded
	}

	snapshot := engine.snapshot.Load()
	decisions := make([]Decision, len(requests))
	errorsByRequest := make([]error, 0)

	for index, request := range requests {
		decision, err := engine.decide(ctx, request, snapshot)
		decisions[index] = decision
		if err != nil {
			errorsByRequest = append(errorsByRequest, err)
		}
	}

	return decisions, errors.Join(errorsByRequest...)
}

func (engine *Engine) decide(
	ctx context.Context,
	request Request,
	snapshot *Snapshot,
) (Decision, error) {
	if err := request.Validate(); err != nil {
		return Decision{
			Outcome:  Deny,
			Reason:   ReasonInvalidRequest,
			Revision: snapshot.revision,
		}, err
	}
	if err := ctx.Err(); err != nil {
		return Decision{
			Outcome:  Deny,
			Reason:   ReasonContextCanceled,
			Revision: snapshot.revision,
		}, err
	}

	decisions := make([]Decision, 0, len(snapshot.policies))
	matchedPolicyIDs := make([]PolicyID, 0, len(snapshot.policies))
	matchedPolicyIDsTruncated := false
	traceCapacity := min(len(snapshot.policies), engine.limits.MaxTraceSize)
	trace := make([]TraceEntry, 0, traceCapacity)
	traceTruncated := false
	evaluationTime := request.Environment.Time
	if evaluationTime.IsZero() {
		evaluationTime = engine.clock()
	}

	for _, policy := range snapshot.policies {
		if err := ctx.Err(); err != nil {
			return Decision{
				Outcome:  Deny,
				Reason:   ReasonContextCanceled,
				Revision: snapshot.revision,
			}, err
		}

		decision := Decision{Outcome: NotApplicable, Reason: ReasonPolicyInactive}
		if policy.activeAt(evaluationTime) {
			var err error
			decision, err = evaluatePolicy(ctx, policy.evaluator, request)
			if err != nil {
				return failureDecision(snapshot), &PolicyEvaluationError{
					PolicyID: policy.id,
					Err:      err,
				}
			}
		}

		if decision.Outcome > Deny {
			return failureDecision(snapshot), ErrInvalidOutcome
		}

		decisions = append(decisions, decision)
		if len(trace) < engine.limits.MaxTraceSize {
			trace = append(trace, TraceEntry{
				PolicyID: policy.id,
				Outcome:  decision.Outcome,
				Reason:   decision.Reason,
			})
		} else {
			traceTruncated = true
		}

		if decision.Outcome != NotApplicable {
			matched := decision.MatchedPolicyIDs
			if len(matched) == 0 {
				matched = []PolicyID{policy.id}
			}
			remaining := engine.limits.MaxMatchedPolicyIDs - len(matchedPolicyIDs)
			if len(matched) > remaining {
				matchedPolicyIDs = append(matchedPolicyIDs, matched[:max(remaining, 0)]...)
				matchedPolicyIDsTruncated = true
			} else {
				matchedPolicyIDs = append(matchedPolicyIDs, matched...)
			}
			matchedPolicyIDsTruncated = matchedPolicyIDsTruncated || decision.MatchedPolicyIDsTruncated
		}

		if isDecisive(snapshot.algorithm, decision.Outcome) {
			break
		}
	}

	decision, err := Combine(snapshot.algorithm, decisions)
	if err != nil {
		return failureDecision(snapshot), err
	}

	if decision.Outcome == NotApplicable {
		decision.Outcome = Deny
		decision.Reason = ReasonDefaultDeny
	}

	decision.MatchedPolicyIDs = matchedPolicyIDs
	decision.MatchedPolicyIDsTruncated = matchedPolicyIDsTruncated
	decision.Revision = snapshot.revision
	decision.Trace = trace
	decision.TraceTruncated = traceTruncated

	return decision, nil
}

func evaluatePolicy(
	ctx context.Context,
	evaluator Evaluator,
	request Request,
) (decision Decision, err error) {
	defer func() {
		if recover() != nil {
			decision = Decision{}
			err = ErrPolicyPanic
		}
	}()
	return evaluator.Evaluate(ctx, request)
}

func failureDecision(snapshot *Snapshot) Decision {
	return Decision{
		Outcome:  Deny,
		Reason:   ReasonEvaluationError,
		Revision: snapshot.revision,
	}
}

func isDecisive(algorithm CombiningAlgorithm, outcome Outcome) bool {
	return (algorithm == DenyOverrides && outcome == Deny) ||
		(algorithm == AllowOverrides && outcome == Allow) ||
		((algorithm == FirstApplicable || algorithm == PriorityOrder) &&
			outcome != NotApplicable)
}
