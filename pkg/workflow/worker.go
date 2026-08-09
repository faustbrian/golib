package workflow

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	// MaxWorkerConcurrency bounds goroutines owned by one worker.
	MaxWorkerConcurrency uint32 = 1_000
	// MaxWorkerPollInterval bounds idle and recoverable-error polling.
	MaxWorkerPollInterval = time.Minute
)

var (
	// ErrInvalidWorker classifies malformed worker configuration, decisions, or
	// adapter output.
	ErrInvalidWorker = errors.New("invalid workflow worker")
)

// WorkDecisionKind selects the fenced durable disposition after processing.
type WorkDecisionKind uint8

const (
	// WorkComplete acknowledges work only after its represented durable
	// transition has been persisted by the processor.
	WorkComplete WorkDecisionKind = 1
	// WorkRetryDecision releases a known-safe failure at an explicit time.
	WorkRetryDecision WorkDecisionKind = 2
	// WorkDeadLetterDecision records poison or manually resolvable work without
	// reporting successful completion.
	WorkDeadLetterDecision WorkDecisionKind = 3
)

// WorkDecisionSpec supplies one explicit processor disposition.
type WorkDecisionSpec struct {
	Kind    WorkDecisionKind
	Code    string
	RetryAt time.Time
}

// WorkDecision is an immutable processor disposition. A processor must not
// return WorkComplete until the workflow transition represented by the work is
// durable. Unknown external outcomes must first persist reconciliation state.
type WorkDecision struct {
	kind    WorkDecisionKind
	code    string
	retryAt time.Time
}

// NewWorkDecision validates one explicit bounded disposition.
func NewWorkDecision(spec WorkDecisionSpec) (WorkDecision, error) {
	decision := WorkDecision{kind: spec.Kind, code: spec.Code, retryAt: canonicalTime(spec.RetryAt)}
	if !decision.Valid() {
		return WorkDecision{}, ErrInvalidWorker
	}
	return decision, nil
}

// Kind returns the durable disposition.
func (decision WorkDecision) Kind() WorkDecisionKind { return decision.kind }

// Code returns the stable retry or dead-letter classification.
func (decision WorkDecision) Code() string { return decision.code }

// RetryAt returns explicit retry admission time, or zero otherwise.
func (decision WorkDecision) RetryAt() time.Time { return decision.retryAt }

// Valid reports whether the disposition is unambiguous.
func (decision WorkDecision) Valid() bool {
	switch decision.kind {
	case WorkComplete:
		return decision.code == "" && decision.retryAt.IsZero()
	case WorkRetryDecision:
		return stableName.MatchString(decision.code) && !decision.retryAt.IsZero()
	case WorkDeadLetterDecision:
		return stableName.MatchString(decision.code) && decision.retryAt.IsZero()
	default:
		return false
	}
}

// WorkProcessor executes one leased unit. It must honor context cancellation,
// return only after all owned goroutines stop, and persist any workflow
// transition before returning WorkComplete. An error leaves the lease for
// fenced recovery; it must not hide an unknown external side effect.
type WorkProcessor interface {
	Process(context.Context, WorkLease) (WorkDecision, error)
}

// ClockTimer is one caller-owned deterministic timer.
type ClockTimer interface {
	C() <-chan time.Time
	Stop() bool
}

// Clock supplies deterministic persisted decision and admission time.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) ClockTimer
}

// SystemClock uses the process wall clock. Tests and replay-sensitive callers
// should supply a deterministic implementation.
type SystemClock struct{}

// Now returns the current canonical wall-clock time.
func (SystemClock) Now() time.Time { return canonicalTime(time.Now()) }

// NewTimer creates one caller-owned process timer.
func (SystemClock) NewTimer(duration time.Duration) ClockTimer {
	return systemTimer{timer: time.NewTimer(duration)}
}

type systemTimer struct{ timer *time.Timer }

func (timer systemTimer) C() <-chan time.Time { return timer.timer.C }
func (timer systemTimer) Stop() bool          { return timer.timer.Stop() }

// WorkerConfig supplies explicit bounded worker ownership and timing policy.
type WorkerConfig struct {
	Store           WorkStore
	Processor       WorkProcessor
	Clock           Clock
	Owner           string
	MaxConcurrent   uint32
	ClaimLimit      uint32
	LeaseDuration   time.Duration
	RenewEvery      time.Duration
	PollInterval    time.Duration
	FinalizeTimeout time.Duration
	Hooks           WorkerHooks
}

// WorkerEventKind classifies bounded worker lifecycle hooks.
type WorkerEventKind uint8

const (
	// WorkerClaimFailed reports a recoverable durable admission failure.
	WorkerClaimFailed WorkerEventKind = 1
	// WorkerProcessingFailed reports work left leased for expiry recovery.
	WorkerProcessingFailed WorkerEventKind = 2
	// WorkerLeaseLost reports cancellation caused by a stale ownership fence.
	WorkerLeaseLost WorkerEventKind = 3
)

// WorkerEvent is one synchronous lifecycle notification. WorkID is data and
// must not be used as an unbounded metric label.
type WorkerEvent struct {
	kind   WorkerEventKind
	workID string
	at     time.Time
	cause  error
}

// Kind returns the lifecycle classification.
func (event WorkerEvent) Kind() WorkerEventKind { return event.kind }

// WorkID returns the affected work identity, or empty for claim failures.
func (event WorkerEvent) WorkID() string { return event.workID }

// At returns the deterministic observation time.
func (event WorkerEvent) At() time.Time { return event.at }

// Cause returns the underlying error for caller-owned logs and traces.
func (event WorkerEvent) Cause() error { return event.cause }

// WorkerHooks receives synchronous bounded lifecycle events. Implementations
// must return promptly and must not panic.
type WorkerHooks interface {
	OnWorkerEvent(WorkerEvent)
}

// Worker owns bounded claim, processing, renewal, and graceful shutdown
// goroutines. Process supervision remains the caller's responsibility.
type Worker struct{ config WorkerConfig }

// NewWorker validates explicit dependencies and resource limits.
func NewWorker(config WorkerConfig) (*Worker, error) {
	if config.Store == nil || config.Processor == nil || config.Clock == nil ||
		!instanceIDPattern.MatchString(config.Owner) || config.MaxConcurrent == 0 ||
		config.MaxConcurrent > MaxWorkerConcurrency || config.ClaimLimit == 0 ||
		config.ClaimLimit > MaxWorkClaimItems || config.LeaseDuration > MaxWorkLeaseDuration ||
		config.RenewEvery <= 0 ||
		config.RenewEvery >= config.LeaseDuration || config.PollInterval <= 0 ||
		config.PollInterval > MaxWorkerPollInterval || config.FinalizeTimeout <= 0 ||
		config.FinalizeTimeout > MaxWorkLeaseDuration {
		return nil, ErrInvalidWorker
	}
	return &Worker{config: config}, nil
}

// Run claims work immediately, owns at most MaxConcurrent handlers, and stops
// claiming on cancellation. It waits for every processor to honor cancellation
// and exit before returning. Recoverable store and processor failures leave
// fenced leases for expiry recovery and do not stop the process loop.
func (worker *Worker) Run(ctx context.Context) error {
	if worker == nil || ctx == nil {
		return ErrInvalidWorker
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan struct{}, worker.config.MaxConcurrent)
	active := uint32(0)
	var handlers sync.WaitGroup
	var runErr error
	for {
		if err := runContext.Err(); err != nil {
			handlers.Wait()
			return runErr
		}
		capacity := worker.config.MaxConcurrent - active
		if capacity > 0 {
			limit := min(worker.config.ClaimLimit, capacity)
			request, err := NewWorkClaimRequest(WorkClaimRequestSpec{
				Owner: worker.config.Owner, Now: worker.config.Clock.Now(),
				LeaseDuration: worker.config.LeaseDuration, Limit: limit,
			})
			if err != nil {
				runErr = ErrInvalidWorker
				cancel()
				continue
			}
			leases, claimErr := worker.config.Store.Claim(runContext, request)
			if claimErr != nil {
				worker.observe(WorkerClaimFailed, "", claimErr)
			}
			if claimErr == nil {
				if len(leases) > int(limit) {
					runErr = ErrInvalidWorker
					cancel()
					continue
				}
				for _, lease := range fairLeaseOrder(leases) {
					active++
					handlers.Add(1)
					go func(claimed WorkLease) {
						defer handlers.Done()
						if err := worker.Handle(runContext, claimed); err != nil {
							kind := WorkerProcessingFailed
							if errors.Is(err, ErrStaleWorkLease) {
								kind = WorkerLeaseLost
							}
							worker.observe(kind, claimed.Work().ID(), err)
						}
						results <- struct{}{}
					}(lease)
				}
				if len(leases) > 0 {
					continue
				}
			}
		}
		timer := worker.config.Clock.NewTimer(worker.config.PollInterval)
		select {
		case <-runContext.Done():
			timer.Stop()
		case <-timer.C():
		case <-results:
			timer.Stop()
			active--
		}
	}
}

func (worker *Worker) observe(kind WorkerEventKind, workID string, cause error) {
	if worker.config.Hooks == nil {
		return
	}
	worker.config.Hooks.OnWorkerEvent(WorkerEvent{
		kind: kind, workID: workID, at: worker.config.Clock.Now(), cause: cause,
	})
}

// Handle processes one valid current lease, renews it on a bounded cadence,
// and applies its explicit fenced disposition. It is exposed for embedding in
// caller-owned admission loops.
func (worker *Worker) Handle(ctx context.Context, lease WorkLease) error {
	if worker == nil || ctx == nil || !lease.Valid() || lease.Owner() != worker.config.Owner {
		return ErrInvalidWorker
	}
	processContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type processResult struct {
		decision WorkDecision
		err      error
	}
	processed := make(chan processResult, 1)
	go func() {
		decision, err := worker.config.Processor.Process(processContext, lease)
		processed <- processResult{decision: decision, err: err}
	}()
	timer := worker.config.Clock.NewTimer(worker.config.RenewEvery)
	defer func() { timer.Stop() }()
	current := lease
	for {
		select {
		case result := <-processed:
			if result.err != nil {
				return result.err
			}
			if !result.decision.Valid() {
				return ErrInvalidWorker
			}
			return worker.finalize(ctx, current, result.decision)
		case <-timer.C():
			now := worker.config.Clock.Now()
			renewal, err := NewWorkLeaseRenewal(WorkLeaseRenewalSpec{
				WorkID: current.Work().ID(), Owner: current.Owner(), Token: current.Token(),
				Now: now, ExtendBy: worker.config.LeaseDuration,
			})
			if err != nil {
				cancel()
				<-processed
				return err
			}
			current, err = worker.config.Store.Renew(ctx, renewal)
			if err != nil {
				cancel()
				<-processed
				return err
			}
			timer.Stop()
			timer = worker.config.Clock.NewTimer(worker.config.RenewEvery)
		case <-ctx.Done():
			cancel()
			result := <-processed
			if result.err != nil {
				return ctx.Err()
			}
			if !result.decision.Valid() {
				return ErrInvalidWorker
			}
			return worker.finalize(ctx, current, result.decision)
		}
	}
}

func (worker *Worker) finalize(ctx context.Context, lease WorkLease, decision WorkDecision) error {
	finalizeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), worker.config.FinalizeTimeout)
	defer cancel()
	now := worker.config.Clock.Now()
	switch decision.Kind() {
	case WorkComplete:
		completion, err := NewWorkCompletion(WorkCompletionSpec{
			WorkID: lease.Work().ID(), Owner: lease.Owner(), Token: lease.Token(), CompletedAt: now,
		})
		if err != nil {
			return err
		}
		return worker.config.Store.Complete(finalizeContext, completion)
	case WorkRetryDecision:
		failure, err := NewWorkFailure(WorkFailureSpec{
			WorkID: lease.Work().ID(), Owner: lease.Owner(), Token: lease.Token(), FailedAt: now,
			Code: decision.Code(), Disposition: WorkRetry, RetryAt: decision.RetryAt(),
		})
		if err != nil {
			return err
		}
		return worker.config.Store.Fail(finalizeContext, failure)
	default:
		failure, err := NewWorkFailure(WorkFailureSpec{
			WorkID: lease.Work().ID(), Owner: lease.Owner(), Token: lease.Token(), FailedAt: now,
			Code: decision.Code(), Disposition: WorkDeadLetter,
		})
		if err != nil {
			return err
		}
		return worker.config.Store.Fail(finalizeContext, failure)
	}
}

func fairLeaseOrder(leases []WorkLease) []WorkLease {
	groups := make(map[string][]WorkLease, len(leases))
	order := make([]string, 0, len(leases))
	maximumGroupSize := 0
	for _, lease := range leases {
		tenant := lease.Work().TenantID()
		if _, exists := groups[tenant]; !exists {
			order = append(order, tenant)
		}
		groups[tenant] = append(groups[tenant], lease)
		maximumGroupSize = max(maximumGroupSize, len(groups[tenant]))
	}
	result := leases[:0:0]
	for index := range maximumGroupSize {
		for _, tenant := range order {
			if index >= len(groups[tenant]) {
				continue
			}
			result = append(result, groups[tenant][index])
		}
	}
	return result
}

var _ Clock = SystemClock{}
var _ ClockTimer = systemTimer{}
