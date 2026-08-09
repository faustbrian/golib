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
	// ErrInvalidObservation identifies metadata outside the stable public
	// observation contract.
	ErrInvalidObservation = errors.New("kafka: observation is invalid")
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
	// ObservationConsumeRecord reports one completed record-handler call.
	ObservationConsumeRecord
	// ObservationConsumeBatch reports one completed partition-batch handler call.
	ObservationConsumeBatch
	// ObservationConsumeCommit reports one completed source-offset commit attempt.
	ObservationConsumeCommit
	// ObservationConsumePoll reports one completed bounded consumer poll cycle.
	ObservationConsumePoll
	// ObservationBrokerConnect reports one completed broker connection
	// initialization, including protocol negotiation and configured SASL.
	ObservationBrokerConnect
	// ObservationBrokerRequest reports one completed Kafka protocol request.
	ObservationBrokerRequest
	// ObservationBrokerThrottle reports broker-imposed request throttling.
	ObservationBrokerThrottle
	// ObservationBrokerDisconnect reports a broker connection closing.
	ObservationBrokerDisconnect
	// ObservationConsumeAssigned reports a completed consumer-group partition
	// assignment callback.
	ObservationConsumeAssigned
	// ObservationConsumeRevoked reports a completed consumer-group partition
	// revocation callback.
	ObservationConsumeRevoked
	// ObservationConsumeLost reports fatal consumer-group partition ownership
	// loss.
	ObservationConsumeLost
	// ObservationConsumeBlocked reports that a rebalance callback is waiting
	// for the current bounded poll to release its rebalance gate.
	ObservationConsumeBlocked
	// ObservationConsumeGroupError reports an error that ended a consumer-group
	// management session.
	ObservationConsumeGroupError
	// ObservationTransactionBegin reports a completed Kafka transaction begin
	// attempt.
	ObservationTransactionBegin
	// ObservationTransactionCommit reports a completed Kafka transaction commit
	// attempt.
	ObservationTransactionCommit
	// ObservationTransactionAbort reports a completed Kafka transaction abort
	// attempt.
	ObservationTransactionAbort
	// ObservationReplayPlan reports broker validation of one bounded replay
	// plan without executing its handlers.
	ObservationReplayPlan
	// ObservationReplayRecord reports one replay record outcome, including
	// processed, skipped, and failed records.
	ObservationReplayRecord
	// ObservationReplayRun reports the exact aggregate outcome of one replay
	// execution.
	ObservationReplayRun
	// ObservationReplayShutdown reports one bounded replay-reader shutdown.
	ObservationReplayShutdown
	// ObservationInspectorCluster reports one bounded cluster metadata query.
	ObservationInspectorCluster
	// ObservationInspectorTopics reports one bounded topic metadata and
	// durability query.
	ObservationInspectorTopics
	// ObservationInspectorConsumerGroups reports one bounded consumer-group
	// lag query.
	ObservationInspectorConsumerGroups
	// ObservationDependencyHealth reports one bounded Kafka connectivity probe.
	ObservationDependencyHealth
	// ObservationReadiness reports one conclusive readiness-hysteresis update.
	ObservationReadiness
	// ObservationInspectorShutdown reports the inspector client closing.
	ObservationInspectorShutdown
	// ObservationProducerShutdown reports one bounded producer shutdown
	// attempt that acquired lifecycle ownership.
	ObservationProducerShutdown
	// ObservationConsumerShutdown reports one bounded consumer shutdown
	// attempt that acquired lifecycle ownership.
	ObservationConsumerShutdown
	// ObservationTransactionProcessorShutdown reports one bounded
	// consume-transform-produce processor shutdown attempt that acquired
	// lifecycle ownership.
	ObservationTransactionProcessorShutdown
	// ObservationConsumeRetryScheduled reports one failed handler attempt that
	// selected a bounded in-process retry before its backoff wait begins.
	ObservationConsumeRetryScheduled
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
	case ObservationConsumeRecord:
		return "consumer.record"
	case ObservationConsumeBatch:
		return "consumer.batch"
	case ObservationConsumeCommit:
		return "consumer.commit"
	case ObservationConsumePoll:
		return "consumer.poll"
	case ObservationConsumeAssigned:
		return "consumer.assigned"
	case ObservationConsumeRevoked:
		return "consumer.revoked"
	case ObservationConsumeLost:
		return "consumer.lost"
	case ObservationConsumeBlocked:
		return "consumer.rebalance_blocked"
	case ObservationConsumeGroupError:
		return "consumer.group_error"
	case ObservationTransactionBegin:
		return "transaction.begin"
	case ObservationTransactionCommit:
		return "transaction.commit"
	case ObservationTransactionAbort:
		return "transaction.abort"
	case ObservationReplayPlan:
		return "replay.plan"
	case ObservationReplayRecord:
		return "replay.record"
	case ObservationReplayRun:
		return "replay.run"
	case ObservationReplayShutdown:
		return "replay.shutdown"
	case ObservationInspectorCluster:
		return "inspector.cluster"
	case ObservationInspectorTopics:
		return "inspector.topics"
	case ObservationInspectorConsumerGroups:
		return "inspector.consumer_groups"
	case ObservationDependencyHealth:
		return "inspector.dependency_health"
	case ObservationReadiness:
		return "inspector.readiness"
	case ObservationInspectorShutdown:
		return "inspector.shutdown"
	case ObservationProducerShutdown:
		return "producer.shutdown"
	case ObservationConsumerShutdown:
		return "consumer.shutdown"
	case ObservationTransactionProcessorShutdown:
		return "transaction_processor.shutdown"
	case ObservationConsumeRetryScheduled:
		return "consumer.retry_scheduled"
	case ObservationBrokerConnect:
		return "broker.connect"
	case ObservationBrokerRequest:
		return "broker.request"
	case ObservationBrokerThrottle:
		return "broker.throttle"
	case ObservationBrokerDisconnect:
		return "broker.disconnect"
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
	// GroupID is the copied configured consumer-group identity when applicable.
	GroupID string
	// BrokerID is the Kafka node ID when BrokerKnown is true. Broker endpoints
	// are never copied into observations.
	BrokerID int32
	// BrokerKnown reports whether BrokerID is authoritative.
	BrokerKnown bool
	// AuthenticationMethod is the configured SASL method for a broker-connect
	// initialization. AuthenticationNone means no SASL flow was configured.
	AuthenticationMethod AuthenticationMethod
	// APIKey is the Kafka protocol request key when APIKeyKnown is true.
	APIKey int16
	// APIKeyKnown reports whether APIKey is authoritative.
	APIKeyKnown bool
	// RequestBytes is the request size written below TLS framing.
	RequestBytes int64
	// ResponseBytes is the response size read below TLS framing.
	ResponseBytes int64
	// QueueDuration is time the request waited inside franz-go before its
	// network write, including client-side throttle waiting.
	QueueDuration time.Duration
	// ThrottleDuration is the broker-imposed throttle interval.
	ThrottleDuration time.Duration
	// ThrottledAfterResponse reports that franz-go applies the throttle after
	// the broker response rather than the broker delaying its response.
	ThrottledAfterResponse bool
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
	// PartitionCount is the bounded number of Kafka partitions represented.
	PartitionCount int
	// BrokerCount is the bounded number of Kafka brokers represented by a
	// cluster inspection. It is zero for other observations.
	BrokerCount int
	// TopicCount is the bounded number of requested Kafka topics represented
	// by a topic inspection. It is zero for other observations.
	TopicCount int
	// GroupCount is the bounded number of requested Kafka consumer groups
	// represented by a group inspection. It is zero for other observations.
	GroupCount int
	// GroupMemberCount is the bounded number of consumer-group members
	// represented by a group inspection. It is zero for other observations.
	GroupMemberCount int
	// ProcessedCount is the number of records whose handler completed.
	ProcessedCount int
	// CommittedCount is the number of source records durably settled.
	CommittedCount int
	// RecordBytes is a conservative payload and framing size, not a broker
	// encoded-byte measurement.
	RecordBytes int64
	// ReplayProcessed is the exact number of records processed by a replay
	// operation. It is zero for non-replay observations.
	ReplayProcessed int64
	// ReplaySkipped is the exact number of records skipped by a replay
	// operation. It is zero for non-replay observations.
	ReplaySkipped int64
	// ReplayFailed is the exact number of records failed by a replay operation.
	// It is zero for non-replay observations.
	ReplayFailed int64
	// ReplayRemaining is the exact number of requested offsets not yet
	// processed. It is zero for non-replay observations.
	ReplayRemaining int64
	// DependencyHealthy reports the result of a dependency probe or the
	// dependency state used by a readiness decision.
	DependencyHealthy bool
	// Ready reports the stateful readiness decision after a conclusive probe.
	Ready bool
	// ConsecutiveFailures is the bounded readiness failure count after a
	// conclusive probe.
	ConsecutiveFailures int
	// ConsecutiveSuccesses is the bounded readiness success count after a
	// conclusive probe.
	ConsecutiveSuccesses int
	// Succeeded reports whether the package operation returned success.
	Succeeded bool
	// Truncated reports that bounded diagnostic counts or metadata were clipped.
	Truncated bool
	// Category classifies failure and is ErrorUnknown after success.
	Category ErrorCategory
}

// Validate reports whether the observation satisfies the public bounded
// metadata, settlement-count, and event-cardinality invariants.
func (observation Observation) Validate() error {
	if observation.Kind < ObservationProduceRecord {
		return ErrInvalidObservation
	}
	if observation.Kind > ObservationConsumeRetryScheduled {
		return ErrInvalidObservation
	}
	if observation.StartedAt.IsZero() ||
		observation.Duration < 0 ||
		observation.RecordCount < 0 ||
		observation.PartitionCount < 0 ||
		observation.BrokerCount < 0 ||
		observation.TopicCount < 0 ||
		observation.GroupCount < 0 ||
		observation.GroupMemberCount < 0 ||
		observation.ProcessedCount < 0 ||
		observation.CommittedCount < 0 ||
		observation.RecordBytes < 0 ||
		observation.ReplayProcessed < 0 ||
		observation.ReplaySkipped < 0 ||
		observation.ReplayFailed < 0 ||
		observation.ReplayRemaining < 0 ||
		observation.RequestBytes < 0 ||
		observation.ResponseBytes < 0 ||
		observation.ConsecutiveFailures < 0 ||
		observation.ConsecutiveSuccesses < 0 ||
		observation.QueueDuration < 0 ||
		observation.ThrottleDuration < 0 ||
		observation.ProcessedCount > observation.RecordCount ||
		observation.CommittedCount > observation.ProcessedCount ||
		(observation.PartitionKnown && observation.Partition < 0) ||
		(observation.OffsetKnown && observation.Offset < 0) ||
		(observation.BrokerKnown && observation.BrokerID < 0) ||
		(observation.APIKeyKnown && observation.APIKey < 0) ||
		(observation.Succeeded && observation.Category != ErrorUnknown) ||
		(!observation.Succeeded &&
			!validErrorCategory(observation.Category)) ||
		!validObservationAuthentication(observation) ||
		!validObservationRecordCardinality(observation) ||
		!validConsumeRetryObservation(observation) ||
		!validReplayObservationProgress(observation) ||
		!validInspectorObservationMetadata(observation) {
		return ErrInvalidObservation
	}

	return nil
}

func validObservationAuthentication(observation Observation) bool {
	if observation.Kind != ObservationBrokerConnect {
		return observation.AuthenticationMethod == AuthenticationNone
	}

	switch observation.AuthenticationMethod {
	case AuthenticationNone,
		AuthenticationPlain,
		AuthenticationSCRAMSHA256,
		AuthenticationSCRAMSHA512,
		AuthenticationOAuthBearer:
		return true
	default:
		return false
	}
}

func validInspectorObservationMetadata(observation Observation) bool {
	isInspector := observation.Kind >= ObservationInspectorCluster &&
		observation.Kind <= ObservationInspectorShutdown
	if !isInspector {
		return observation.BrokerCount == 0 &&
			observation.TopicCount == 0 &&
			observation.GroupCount == 0 &&
			observation.GroupMemberCount == 0 &&
			!observation.DependencyHealthy &&
			!observation.Ready &&
			observation.ConsecutiveFailures == 0 &&
			observation.ConsecutiveSuccesses == 0
	}
	if observation.BrokerCount > 10_000 ||
		observation.TopicCount > 64 ||
		observation.GroupCount > 64 ||
		observation.GroupMemberCount > 100_000 ||
		observation.PartitionCount > 1_000_000 ||
		observation.ConsecutiveFailures > 100 ||
		observation.ConsecutiveSuccesses > 100 {
		return false
	}

	switch observation.Kind {
	case ObservationInspectorCluster:
		return observation.TopicCount == 0 &&
			observation.GroupCount == 0 &&
			observation.GroupMemberCount == 0 &&
			observation.PartitionCount == 0 &&
			!observation.DependencyHealthy &&
			!observation.Ready &&
			observation.ConsecutiveFailures == 0 &&
			observation.ConsecutiveSuccesses == 0 &&
			observation.Succeeded == (observation.BrokerCount > 0)
	case ObservationInspectorTopics:
		return observation.BrokerCount == 0 &&
			observation.TopicCount > 0 &&
			observation.GroupCount == 0 &&
			observation.GroupMemberCount == 0 &&
			!observation.DependencyHealthy &&
			!observation.Ready &&
			observation.ConsecutiveFailures == 0 &&
			observation.ConsecutiveSuccesses == 0 &&
			((observation.Succeeded && observation.PartitionCount > 0) ||
				(!observation.Succeeded && observation.PartitionCount == 0))
	case ObservationInspectorConsumerGroups:
		return observation.BrokerCount == 0 &&
			observation.TopicCount == 0 &&
			observation.GroupCount > 0 &&
			!observation.DependencyHealthy &&
			!observation.Ready &&
			observation.ConsecutiveFailures == 0 &&
			observation.ConsecutiveSuccesses == 0 &&
			(observation.Succeeded ||
				(observation.GroupMemberCount == 0 &&
					observation.PartitionCount == 0))
	case ObservationDependencyHealth:
		return observation.BrokerCount == 0 &&
			observation.TopicCount == 0 &&
			observation.GroupCount == 0 &&
			observation.GroupMemberCount == 0 &&
			observation.PartitionCount == 0 &&
			!observation.Ready &&
			observation.ConsecutiveFailures == 0 &&
			observation.ConsecutiveSuccesses == 0 &&
			observation.Succeeded == observation.DependencyHealthy
	case ObservationReadiness:
		countsValid := (observation.DependencyHealthy &&
			observation.ConsecutiveFailures == 0 &&
			observation.ConsecutiveSuccesses > 0) ||
			(!observation.DependencyHealthy &&
				observation.ConsecutiveFailures > 0 &&
				observation.ConsecutiveSuccesses == 0)

		return observation.BrokerCount == 0 &&
			observation.TopicCount == 0 &&
			observation.GroupCount == 0 &&
			observation.GroupMemberCount == 0 &&
			observation.PartitionCount == 0 &&
			observation.Succeeded == observation.DependencyHealthy &&
			countsValid
	default:
		return observation.BrokerCount == 0 &&
			observation.TopicCount == 0 &&
			observation.GroupCount == 0 &&
			observation.GroupMemberCount == 0 &&
			observation.PartitionCount == 0 &&
			!observation.DependencyHealthy &&
			!observation.Ready &&
			observation.ConsecutiveFailures == 0 &&
			observation.ConsecutiveSuccesses == 0 &&
			observation.Succeeded
	}
}

func validObservationRecordCardinality(observation Observation) bool {
	switch observation.Kind {
	case ObservationProduceRecord,
		ObservationProduceAsync,
		ObservationConsumeRecord,
		ObservationReplayRecord:
		return observation.RecordCount == 1
	case ObservationProduceBatch,
		ObservationConsumeBatch,
		ObservationConsumeCommit,
		ObservationConsumeRetryScheduled:
		return observation.RecordCount > 0
	case ObservationConsumePoll:
		return true
	default:
		return observation.RecordCount == 0 &&
			observation.ProcessedCount == 0 &&
			observation.CommittedCount == 0 &&
			observation.RecordBytes == 0
	}
}

func validConsumeRetryObservation(observation Observation) bool {
	if observation.Kind != ObservationConsumeRetryScheduled {
		return true
	}

	return !observation.Succeeded &&
		observation.PartitionCount == 1 &&
		observation.PartitionKnown &&
		observation.OffsetKnown &&
		observation.ProcessedCount == 0 &&
		observation.CommittedCount == 0 &&
		observation.RecordBytes > 0
}

func validReplayObservationProgress(observation Observation) bool {
	switch observation.Kind {
	case ObservationReplayPlan:
		return observation.PartitionCount > 0 &&
			observation.RecordCount == 0 &&
			observation.ProcessedCount == 0 &&
			observation.ReplayProcessed == 0 &&
			observation.ReplaySkipped == 0 &&
			observation.ReplayFailed == 0 &&
			(observation.Succeeded || observation.ReplayRemaining == 0)
	case ObservationReplayRecord:
		outcomes := observation.ReplayProcessed +
			observation.ReplaySkipped +
			observation.ReplayFailed

		return outcomes == 1 &&
			observation.ReplayRemaining == 0 &&
			int64(observation.ProcessedCount) == observation.ReplayProcessed &&
			observation.Succeeded == (observation.ReplayFailed == 0)
	case ObservationReplayRun:
		return observation.PartitionCount > 0 &&
			observation.RecordCount == 0 &&
			observation.ProcessedCount == 0 &&
			observation.CommittedCount == 0 &&
			observation.RecordBytes == 0 &&
			(!observation.Succeeded ||
				(observation.ReplayFailed == 0 &&
					observation.ReplayRemaining == 0))
	case ObservationReplayShutdown:
		return observation.ReplayProcessed == 0 &&
			observation.ReplaySkipped == 0 &&
			observation.ReplayFailed == 0 &&
			observation.ReplayRemaining == 0
	default:
		return observation.ReplayProcessed == 0 &&
			observation.ReplaySkipped == 0 &&
			observation.ReplayFailed == 0 &&
			observation.ReplayRemaining == 0
	}
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
