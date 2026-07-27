// Package golog translates bounded Kafka observations into fixed structured
// log/slog records without adding payloads, credentials, endpoints, raw
// headers, or application error text.
package golog

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	kafka "github.com/faustbrian/golib/pkg/kafka"
)

const (
	maxAllowedValues  = 128
	maxTopicLength    = 249
	maxIdentityLength = 255
)

var (
	// ErrLoggerRequired reports a missing slog logger.
	ErrLoggerRequired = errors.New("kafka/golog: logger is required")
	// ErrInvalidIdentityPolicy reports an unbounded, duplicated, or invalid
	// logging identity allowlist.
	ErrInvalidIdentityPolicy = errors.New(
		"kafka/golog: identity policy is invalid",
	)
	// ErrContextRequired reports a nil observer context.
	ErrContextRequired = errors.New("kafka/golog: context is required")
	// ErrInvalidObservation reports metadata outside the root observation
	// contract.
	ErrInvalidObservation = errors.New("kafka/golog: observation is invalid")
	// ErrLoggerPanic identifies a contained slog handler panic without
	// retaining or rendering its potentially sensitive panic value.
	ErrLoggerPanic = errors.New("kafka/golog: logger panicked")
)

// IdentityPolicy explicitly bounds Kafka identities admitted to logs. Empty
// allowlists omit the corresponding identity. Values are matched exactly and
// copied during construction.
type IdentityPolicy struct {
	// AllowedClientIDs contains exact Kafka client IDs permitted in logs.
	AllowedClientIDs []string
	// AllowedTopics contains exact Kafka topic names permitted in logs.
	AllowedTopics []string
	// AllowedConsumerGroups contains exact Kafka consumer-group IDs permitted
	// in logs.
	AllowedConsumerGroups []string
}

// Validate reports whether every identity allowlist is bounded, unique, and
// safe to copy into structured logs.
func (policy IdentityPolicy) Validate() error {
	_, err := normalizeIdentityPolicy(policy)

	return err
}

// Config owns immutable adapter dependencies and identity policy.
type Config struct {
	// Logger is required and remains application-owned. New retains the
	// immutable logger pointer but does not modify its handler configuration.
	Logger *slog.Logger
	// Level is the level supplied to Logger. Its zero value is slog.LevelInfo.
	Level slog.Level
	// Identities is defensively copied by New. Its zero value denies every
	// client, topic, and consumer-group identity.
	Identities IdentityPolicy
}

// Validate checks the logger and identity policy without retaining either.
func (config Config) Validate() error {
	_, err := validateConfig(config)

	return err
}

// Adapter owns an immutable slog logger and copied identity policy. It starts
// no goroutines and is safe for concurrent observer calls when the supplied
// slog handler satisfies slog.Handler's concurrency contract.
type Adapter struct {
	logger     *slog.Logger
	level      slog.Level
	identities normalizedIdentityPolicy
}

type normalizedIdentityPolicy struct {
	clientIDs      map[string]struct{}
	topics         map[string]struct{}
	consumerGroups map[string]struct{}
}

// New validates and defensively copies configuration.
func New(config Config) (*Adapter, error) {
	identities, err := validateConfig(config)
	if err != nil {
		return nil, err
	}

	return &Adapter{
		logger:     config.Logger,
		level:      config.Level,
		identities: identities,
	}, nil
}

// Observer returns the synchronous Kafka observer owned by this adapter. The
// returned function does not retain callback contexts or observations.
func (adapter *Adapter) Observer() kafka.ObserverFunc {
	return adapter.observe
}

func (adapter *Adapter) observe(
	ctx context.Context,
	observation kafka.Observation,
) (err error) {
	if ctx == nil {
		return ErrContextRequired
	}
	if adapter == nil || adapter.logger == nil {
		return ErrLoggerRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := observation.Validate(); err != nil {
		return ErrInvalidObservation
	}

	defer func() {
		if recover() != nil {
			err = ErrLoggerPanic
		}
	}()

	attributes := []slog.Attr{
		slog.String("messaging.system", "kafka"),
		slog.String("kafka.operation", observation.Kind.String()),
		slog.String("kafka.outcome", outcome(observation.Succeeded)),
		slog.String("error.type", observation.Category.String()),
		slog.Time("kafka.started_at", observation.StartedAt),
		slog.Int64("kafka.duration_ms", observation.Duration.Milliseconds()),
		slog.Int("kafka.record.count", observation.RecordCount),
		slog.Int("kafka.partition.count", observation.PartitionCount),
		slog.Int("kafka.broker.count", observation.BrokerCount),
		slog.Int("kafka.topic.count", observation.TopicCount),
		slog.Int("kafka.consumer_group.count", observation.GroupCount),
		slog.Int(
			"kafka.consumer_group.member.count",
			observation.GroupMemberCount,
		),
		slog.Int("kafka.processed.count", observation.ProcessedCount),
		slog.Int("kafka.committed.count", observation.CommittedCount),
		slog.Int64("kafka.record.size", observation.RecordBytes),
		slog.Int64("kafka.replay.processed", observation.ReplayProcessed),
		slog.Int64("kafka.replay.skipped", observation.ReplaySkipped),
		slog.Int64("kafka.replay.failed", observation.ReplayFailed),
		slog.Int64("kafka.replay.remaining", observation.ReplayRemaining),
		slog.Bool("kafka.dependency.healthy", observation.DependencyHealthy),
		slog.Bool("kafka.readiness.ready", observation.Ready),
		slog.Int(
			"kafka.readiness.consecutive_failures",
			observation.ConsecutiveFailures,
		),
		slog.Int(
			"kafka.readiness.consecutive_successes",
			observation.ConsecutiveSuccesses,
		),
		slog.Int64("kafka.request.size", observation.RequestBytes),
		slog.Int64("kafka.response.size", observation.ResponseBytes),
		slog.Int64(
			"kafka.request.queue.duration_ms",
			observation.QueueDuration.Milliseconds(),
		),
		slog.Int64(
			"kafka.throttle.duration_ms",
			observation.ThrottleDuration.Milliseconds(),
		),
		slog.Bool(
			"kafka.throttled_after_response",
			observation.ThrottledAfterResponse,
		),
		slog.Bool("kafka.truncated", observation.Truncated),
	}
	if adapter.identities.allowsClientID(observation.ClientID) {
		attributes = append(
			attributes,
			slog.String("messaging.client.id", observation.ClientID),
		)
	}
	if adapter.identities.allowsTopic(observation.Topic) {
		attributes = append(
			attributes,
			slog.String("messaging.destination.name", observation.Topic),
		)
	}
	if adapter.identities.allowsConsumerGroup(observation.GroupID) {
		attributes = append(
			attributes,
			slog.String("messaging.consumer.group.name", observation.GroupID),
		)
	}
	if observation.PartitionKnown {
		attributes = append(
			attributes,
			slog.Int64(
				"messaging.destination.partition.id",
				int64(observation.Partition),
			),
		)
	}
	if observation.OffsetKnown {
		attributes = append(
			attributes,
			slog.Int64("messaging.kafka.message.offset", observation.Offset),
		)
	}
	if observation.BrokerKnown {
		attributes = append(
			attributes,
			slog.Int64(
				"messaging.kafka.destination.broker.id",
				int64(observation.BrokerID),
			),
		)
	}
	if observation.APIKeyKnown {
		attributes = append(
			attributes,
			slog.Int64(
				"messaging.kafka.protocol.api_key",
				int64(observation.APIKey),
			),
		)
	}
	if !observation.Timestamp.IsZero() {
		attributes = append(
			attributes,
			slog.Time("messaging.kafka.message.timestamp", observation.Timestamp),
		)
	}

	adapter.logger.LogAttrs(
		ctx,
		adapter.level,
		"kafka client observation",
		attributes...,
	)

	return nil
}

func validateConfig(config Config) (normalizedIdentityPolicy, error) {
	if config.Logger == nil {
		return normalizedIdentityPolicy{}, ErrLoggerRequired
	}

	return normalizeIdentityPolicy(config.Identities)
}

func normalizeIdentityPolicy(
	policy IdentityPolicy,
) (normalizedIdentityPolicy, error) {
	clientIDs, err := normalizeAllowlist(
		policy.AllowedClientIDs,
		maxIdentityLength,
		validIdentity,
	)
	if err != nil {
		return normalizedIdentityPolicy{}, err
	}
	topics, err := normalizeAllowlist(
		policy.AllowedTopics,
		maxTopicLength,
		validTopic,
	)
	if err != nil {
		return normalizedIdentityPolicy{}, err
	}
	consumerGroups, err := normalizeAllowlist(
		policy.AllowedConsumerGroups,
		maxIdentityLength,
		validIdentity,
	)
	if err != nil {
		return normalizedIdentityPolicy{}, err
	}

	return normalizedIdentityPolicy{
		clientIDs:      clientIDs,
		topics:         topics,
		consumerGroups: consumerGroups,
	}, nil
}

func normalizeAllowlist(
	values []string,
	maxLength int,
	validate func(string, int) bool,
) (map[string]struct{}, error) {
	if len(values) > maxAllowedValues {
		return nil, ErrInvalidIdentityPolicy
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validate(value, maxLength) {
			return nil, ErrInvalidIdentityPolicy
		}
		if _, duplicate := result[value]; duplicate {
			return nil, ErrInvalidIdentityPolicy
		}
		result[value] = struct{}{}
	}

	return result, nil
}

func validIdentity(value string, maxLength int) bool {
	if len(value) == 0 ||
		len(value) > maxLength ||
		!utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) {
			return false
		}
	}

	return true
}

func validTopic(value string, maxLength int) bool {
	if !validIdentity(value, maxLength) ||
		value == "." ||
		value == ".." {
		return false
	}
	for _, current := range value {
		if (current < 'a' || current > 'z') &&
			(current < 'A' || current > 'Z') &&
			(current < '0' || current > '9') &&
			current != '.' &&
			current != '_' &&
			current != '-' {
			return false
		}
	}

	return true
}

func (policy normalizedIdentityPolicy) allowsClientID(value string) bool {
	_, ok := policy.clientIDs[value]

	return ok
}

func (policy normalizedIdentityPolicy) allowsTopic(value string) bool {
	_, ok := policy.topics[value]

	return ok
}

func (policy normalizedIdentityPolicy) allowsConsumerGroup(value string) bool {
	_, ok := policy.consumerGroups[value]

	return ok
}

func outcome(succeeded bool) string {
	if succeeded {
		return "success"
	}

	return "failure"
}
