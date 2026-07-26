package kafka

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidObserverPolicy identifies an observer policy outside the
	// package's callback-count or deadline bounds.
	ErrInvalidObserverPolicy = errors.New(
		"kafka: observer policy is outside bounded limits",
	)
	// ErrObserverFailureHandlerRequired requires observer failures to have an
	// explicit reporting destination.
	ErrObserverFailureHandlerRequired = errors.New(
		"kafka: observer failure handler is required",
	)
	// ErrObserverPanic identifies a contained observer panic without retaining
	// or rendering its potentially sensitive panic value.
	ErrObserverPanic = errors.New("kafka: observer panicked")
	// ErrObserverReentry identifies an operation attempted with the context
	// supplied to an observer callback.
	ErrObserverReentry = errors.New("kafka: observer callback cannot re-enter client")
)

// ObservationKind identifies one stable package-policy event.
type ObservationKind uint8

const (
	// ObservationProduceRecord reports completion of synchronous single-record
	// production.
	ObservationProduceRecord ObservationKind = iota + 1
	// ObservationProduceBatch reports completion of synchronous batch
	// production.
	ObservationProduceBatch
	// ObservationProduceAsync reports final asynchronous record delivery.
	ObservationProduceAsync
)

// String returns the stable low-cardinality observation name.
func (kind ObservationKind) String() string {
	switch kind {
	case ObservationProduceRecord:
		return "producer.record"
	case ObservationProduceBatch:
		return "producer.batch"
	case ObservationProduceAsync:
		return "producer.async"
	default:
		return "unknown"
	}
}

// Observation is copied, payload-free metadata for one completed Kafka policy
// operation. Topic is populated only for a single validated topic. Category is
// ErrorUnknown when Succeeded is true.
type Observation struct {
	// Kind identifies the completed package operation.
	Kind ObservationKind
	// StartedAt is the local operation start time.
	StartedAt time.Time
	// Duration is the elapsed local operation time through final delivery.
	Duration time.Duration
	// ClientID is the copied configured Kafka client identity.
	ClientID string
	// Topic is present only when validated metadata has one common topic.
	Topic string
	// Partition is the delivered partition when PartitionKnown is true.
	Partition int32
	// PartitionKnown reports whether Partition is authoritative.
	PartitionKnown bool
	// Offset is the delivered Kafka offset when OffsetKnown is true.
	Offset int64
	// OffsetKnown reports whether Offset is authoritative.
	OffsetKnown bool
	// Timestamp is Kafka's delivered record timestamp when available.
	Timestamp time.Time
	// RecordCount is the bounded operation input count.
	RecordCount int
	// RecordBytes is a conservative payload and framing size, not a broker
	// encoded-byte measurement.
	RecordBytes int64
	// Succeeded reports whether the package operation returned success.
	Succeeded bool
	// Category classifies failure and is ErrorUnknown after success.
	Category ErrorCategory
}

// ObserverFunc synchronously observes one copied event. Implementations must
// return when ctx is done, must be concurrency-safe, and must not retain the
// callback context for later work.
type ObserverFunc func(context.Context, Observation) error

// ObservationFailureFunc synchronously receives a contained observer failure.
// It must follow the same deadline and concurrency rules as ObserverFunc.
type ObservationFailureFunc func(context.Context, ObservationFailure)

// ObserverPolicy bounds ordered synchronous observation. Observers and their
// failure handler share one timeout budget per event and are copied during
// client construction.
type ObserverPolicy struct {
	// Observers run synchronously in slice order and are copied during
	// construction.
	Observers []ObserverFunc
	// FailureHandler receives every contained observer error, panic, or
	// cooperative timeout before the next observer runs.
	FailureHandler ObservationFailureFunc
	// Timeout is one shared cooperative budget for an event's observers and
	// failure callbacks.
	Timeout time.Duration
}

// Validate reports whether the observer policy is internally compatible and
// bounded.
func (policy ObserverPolicy) Validate() error {
	_, err := normalizeObserverPolicy(policy)

	return err
}

// ObservationFailure reports which observer failed without formatting its
// potentially sensitive returned error. The application owns any error
// returned by Cause and must redact it before external reporting.
type ObservationFailure struct {
	// ObserverIndex is the failed callback's index in ObserverPolicy.Observers.
	ObserverIndex int
	// Kind identifies the event being observed.
	Kind ObservationKind
	// TimedOut reports that the shared callback deadline expired before this
	// observer returned.
	TimedOut bool
	// Panicked reports that the observer panic was contained.
	Panicked bool
	cause    error
}

// Error returns a stable message that does not render the observer error or
// panic value.
func (failure ObservationFailure) Error() string {
	return "kafka: observer failed"
}

// Cause returns the observer error for explicit application handling.
func (failure ObservationFailure) Cause() error {
	return failure.cause
}

type observerContextKey struct{}

type observerDispatcher struct {
	observers      []ObserverFunc
	failureHandler ObservationFailureFunc
	timeout        time.Duration
}

func normalizeObserverPolicy(policy ObserverPolicy) (ObserverPolicy, error) {
	if len(policy.Observers) == 0 {
		if policy.FailureHandler != nil || policy.Timeout != 0 {
			return ObserverPolicy{}, ErrInvalidObserverPolicy
		}

		return ObserverPolicy{}, nil
	}
	if len(policy.Observers) > 16 {
		return ObserverPolicy{}, ErrInvalidObserverPolicy
	}
	for _, observer := range policy.Observers {
		if observer == nil {
			return ObserverPolicy{}, ErrInvalidObserverPolicy
		}
	}
	if policy.FailureHandler == nil {
		return ObserverPolicy{}, ErrObserverFailureHandlerRequired
	}
	if policy.Timeout == 0 {
		policy.Timeout = 100 * time.Millisecond
	}
	if policy.Timeout < time.Millisecond || policy.Timeout > 5*time.Second {
		return ObserverPolicy{}, ErrInvalidObserverPolicy
	}
	policy.Observers = append([]ObserverFunc(nil), policy.Observers...)

	return policy, nil
}

func newObserverDispatcher(policy ObserverPolicy) observerDispatcher {
	return observerDispatcher{
		observers:      policy.Observers,
		failureHandler: policy.FailureHandler,
		timeout:        policy.Timeout,
	}
}

func (dispatcher observerDispatcher) enabled() bool {
	return len(dispatcher.observers) != 0
}

func (dispatcher observerDispatcher) observe(
	ctx context.Context,
	observation Observation,
) {
	if len(dispatcher.observers) == 0 {
		return
	}

	callbackCtx, cancel := context.WithTimeout(
		context.WithValue(
			context.WithoutCancel(ctx),
			observerContextKey{},
			true,
		),
		dispatcher.timeout,
	)
	defer cancel()

	for index, observer := range dispatcher.observers {
		err, panicked := callObserver(callbackCtx, observer, observation)
		timedOut := callbackCtx.Err() != nil
		if err == nil && !timedOut {
			continue
		}
		if timedOut {
			err = errors.Join(err, callbackCtx.Err())
		}
		callObservationFailureHandler(
			callbackCtx,
			dispatcher.failureHandler,
			ObservationFailure{
				ObserverIndex: index,
				Kind:          observation.Kind,
				TimedOut:      timedOut,
				Panicked:      panicked,
				cause:         err,
			},
		)
	}
}

func callObserver(
	ctx context.Context,
	observer ObserverFunc,
	observation Observation,
) (err error, panicked bool) {
	defer func() {
		if recover() != nil {
			err = ErrObserverPanic
			panicked = true
		}
	}()

	return observer(ctx, observation), false
}

func callObservationFailureHandler(
	ctx context.Context,
	handler ObservationFailureFunc,
	failure ObservationFailure,
) {
	defer func() {
		_ = recover()
	}()
	handler(ctx, failure)
}

func isObserverContext(ctx context.Context) bool {
	active, _ := ctx.Value(observerContextKey{}).(bool)

	return active
}
