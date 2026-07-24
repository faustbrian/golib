package idempotency

import (
	"context"
	"errors"
)

// AvailabilityPolicy controls whether work may run after acquisition storage fails.
type AvailabilityPolicy uint8

const (
	// AvailabilityFailClosed rejects execution when ownership is unavailable.
	AvailabilityFailClosed AvailabilityPolicy = iota
	// AvailabilityAllowUntracked permits explicitly duplicate-tolerant execution.
	AvailabilityAllowUntracked
)

// BeginRequest combines durable acquisition with an availability policy.
type BeginRequest struct {
	Acquire      AcquireRequest
	Availability AvailabilityPolicy
}

// BeginResult describes whether work should execute and whether it is tracked.
type BeginResult struct {
	Outcome Outcome
	Record  Record
	Execute bool
	Durable bool
	Failure error
}

// Service normalizes store failures and applies availability policy to acquisition.
type Service struct {
	store     Store
	observer  Observer
	keyHasher KeyHasher
}

// NewService constructs the semantic service over a non-nil store.
func NewService(store Store) (*Service, error) {
	return NewServiceWithOptions(store, ServiceOptions{})
}

// NewServiceWithOptions constructs a service with optional bounded observation.
func NewServiceWithOptions(store Store, options ServiceOptions) (*Service, error) {
	if store == nil {
		return nil, &Error{
			Reason: ReasonInvalidConfiguration,
			Field:  "store",
		}
	}
	return &Service{
		store: store, observer: options.Observer, keyHasher: options.KeyHasher,
	}, nil
}

// Begin attempts ownership and decides whether the caller may execute.
func (s *Service) Begin(
	ctx context.Context,
	request BeginRequest,
) (result BeginResult, err error) {
	defer func() {
		observedErr := err
		if observedErr == nil {
			observedErr = result.Failure
		}
		s.observe(
			ctx, TransitionAcquire, request.Acquire.Key,
			result.Outcome, result.Durable, observedErr,
		)
	}()
	if request.Availability != AvailabilityFailClosed &&
		request.Availability != AvailabilityAllowUntracked {
		return BeginResult{}, &Error{
			Reason: ReasonInvalidConfiguration,
			Field:  "availability",
		}
	}

	acquired, err := s.store.Acquire(ctx, request.Acquire)
	if err == nil {
		if !isDurableOutcome(acquired.Outcome) {
			return BeginResult{}, &Error{
				Reason: ReasonInvalidTransition,
				Field:  "acquire_outcome",
			}
		}
		return BeginResult{
			Outcome: acquired.Outcome,
			Record:  acquired.Record,
			Execute: acquired.Outcome == OutcomeAcquired ||
				acquired.Outcome == OutcomeStaleOwnerTakeover,
			Durable: true,
		}, nil
	}

	err = normalizeStoreError("acquire", err)
	var semanticError *Error
	if !errors.As(err, &semanticError) || semanticError.Reason != ReasonUnavailable {
		return BeginResult{}, err
	}

	result = BeginResult{
		Outcome: OutcomeUnavailable,
		Failure: err,
	}
	if request.Availability == AvailabilityAllowUntracked {
		result.Execute = true
		return result, nil
	}
	return result, err
}

func (s *Service) observe(
	ctx context.Context,
	transition Transition,
	key Key,
	outcome Outcome,
	durable bool,
	err error,
) {
	defer func() { _ = recover() }()
	if s.observer == nil {
		return
	}
	observation := Observation{
		Transition: transition,
		Outcome:    outcome,
		Durable:    durable,
	}
	var semantic *Error
	if errors.As(err, &semantic) {
		observation.Reason = semantic.Reason
	}
	if s.keyHasher != nil {
		observation.Correlation = s.keyHasher(key)
	}
	s.observer.Observe(ctx, observation)
}

func isDurableOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeAcquired, OutcomeStaleOwnerTakeover, OutcomeReplayed,
		OutcomeInProgress, OutcomeConflict, OutcomeTerminalFailure:
		return true
	default:
		return false
	}
}

// Inspect returns the authoritative record for key.
func (s *Service) Inspect(ctx context.Context, key Key) (record Record, err error) {
	defer func() {
		s.observe(ctx, TransitionInspect, key, "", err == nil, err)
	}()
	record, err = s.store.Inspect(ctx, key)
	err = normalizeStoreError("inspect", err)
	return record, err
}

// Heartbeat extends a live current owner's lease.
func (s *Service) Heartbeat(
	ctx context.Context,
	request HeartbeatRequest,
) (record Record, err error) {
	defer func() {
		s.observe(ctx, TransitionHeartbeat, request.Ownership.Key, "", err == nil, err)
	}()
	record, err = s.store.Heartbeat(ctx, request)
	err = normalizeStoreError("heartbeat", err)
	return record, err
}

// Complete conditionally records a successful terminal result.
func (s *Service) Complete(
	ctx context.Context,
	request CompleteRequest,
) (record Record, err error) {
	defer func() {
		s.observe(ctx, TransitionComplete, request.Ownership.Key, "", err == nil, err)
	}()
	record, err = s.store.Complete(ctx, request)
	err = normalizeStoreError("complete", err)
	return record, err
}

// Fail conditionally records a terminal failure result.
func (s *Service) Fail(
	ctx context.Context,
	request FailRequest,
) (record Record, err error) {
	defer func() {
		s.observe(ctx, TransitionFail, request.Ownership.Key, "", err == nil, err)
	}()
	record, err = s.store.Fail(ctx, request)
	err = normalizeStoreError("fail", err)
	return record, err
}

// Release conditionally abandons a live attempt without a terminal result.
func (s *Service) Release(
	ctx context.Context,
	ownership Ownership,
) (record Record, err error) {
	defer func() {
		s.observe(ctx, TransitionRelease, ownership.Key, "", err == nil, err)
	}()
	record, err = s.store.Release(ctx, ownership)
	err = normalizeStoreError("release", err)
	return record, err
}

// Expire records that an active lease elapsed without granting new ownership.
func (s *Service) Expire(ctx context.Context, key Key) (record Record, err error) {
	defer func() {
		s.observe(ctx, TransitionExpire, key, "", err == nil, err)
	}()
	record, err = s.store.Expire(ctx, key)
	err = normalizeStoreError("expire", err)
	return record, err
}

func normalizeStoreError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var semanticError *Error
	if errors.As(err, &semanticError) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &Error{
		Reason: ReasonUnavailable,
		Field:  operation,
		Cause:  err,
	}
}
