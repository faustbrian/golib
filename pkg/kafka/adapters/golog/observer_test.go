package golog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
)

func TestObserverEmitsOnlyBoundedStructuredObservationMetadata(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
		Level:  slog.LevelInfo,
		Identities: IdentityPolicy{
			AllowedClientIDs:      []string{"orders-consumer"},
			AllowedTopics:         []string{"orders"},
			AllowedConsumerGroups: []string{"fulfillment"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observation := kafka.Observation{
		Kind:                   kafka.ObservationConsumeCommit,
		StartedAt:              time.Unix(1, 2).UTC(),
		Duration:               1500 * time.Millisecond,
		ClientID:               "orders-consumer",
		GroupID:                "fulfillment",
		BrokerID:               3,
		BrokerKnown:            true,
		APIKey:                 8,
		APIKeyKnown:            true,
		Topic:                  "orders",
		Partition:              2,
		PartitionKnown:         true,
		Offset:                 42,
		OffsetKnown:            true,
		Timestamp:              time.Unix(3, 4).UTC(),
		RecordCount:            4,
		PartitionCount:         1,
		ProcessedCount:         4,
		CommittedCount:         4,
		RecordBytes:            1024,
		RequestBytes:           128,
		ResponseBytes:          64,
		QueueDuration:          20 * time.Millisecond,
		ThrottleDuration:       30 * time.Millisecond,
		ThrottledAfterResponse: true,
		Succeeded:              true,
		Truncated:              true,
	}
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v, output = %q", err, output.String())
	}
	want := map[string]any{
		"msg":                                   "kafka client observation",
		"level":                                 "INFO",
		"messaging.system":                      "kafka",
		"kafka.operation":                       "consumer.commit",
		"kafka.outcome":                         "success",
		"error.type":                            "unknown",
		"kafka.started_at":                      "1970-01-01T00:00:01.000000002Z",
		"kafka.duration_ms":                     float64(1500),
		"kafka.record.count":                    float64(4),
		"kafka.partition.count":                 float64(1),
		"kafka.broker.count":                    float64(0),
		"kafka.topic.count":                     float64(0),
		"kafka.consumer_group.count":            float64(0),
		"kafka.consumer_group.member.count":     float64(0),
		"kafka.processed.count":                 float64(4),
		"kafka.committed.count":                 float64(4),
		"kafka.record.size":                     float64(1024),
		"kafka.replay.processed":                float64(0),
		"kafka.replay.skipped":                  float64(0),
		"kafka.replay.failed":                   float64(0),
		"kafka.replay.remaining":                float64(0),
		"kafka.dependency.healthy":              false,
		"kafka.readiness.ready":                 false,
		"kafka.readiness.consecutive_failures":  float64(0),
		"kafka.readiness.consecutive_successes": float64(0),
		"kafka.request.size":                    float64(128),
		"kafka.response.size":                   float64(64),
		"kafka.request.queue.duration_ms":       float64(20),
		"kafka.throttle.duration_ms":            float64(30),
		"kafka.throttled_after_response":        true,
		"kafka.truncated":                       true,
		"messaging.client.id":                   "orders-consumer",
		"messaging.destination.name":            "orders",
		"messaging.consumer.group.name":         "fulfillment",
		"messaging.destination.partition.id":    float64(2),
		"messaging.kafka.message.offset":        float64(42),
		"messaging.kafka.destination.broker.id": float64(3),
		"messaging.kafka.protocol.api_key":      float64(8),
		"messaging.kafka.message.timestamp":     "1970-01-01T00:00:03.000000004Z",
	}
	for key, value := range want {
		if got := record[key]; got != value {
			t.Fatalf("attribute %q = %#v, want %#v; record = %#v", key, got, value, record)
		}
	}
	if got, wantCount := len(record), len(want)+1; got != wantCount {
		t.Fatalf("record attributes = %d, want %d: %#v", got, wantCount, record)
	}
	rendered := output.String()
	for _, forbidden := range []string{
		"payload-secret",
		"key-secret",
		"header-secret",
		"credential-secret",
		"broker.example:9092",
		"application error",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("log contains forbidden value %q: %s", forbidden, rendered)
		}
	}
}

func TestObserverEmitsConfiguredBrokerAuthenticationMethod(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observation := kafka.Observation{
		Kind:                 kafka.ObservationBrokerConnect,
		StartedAt:            time.Unix(1, 0),
		AuthenticationMethod: kafka.AuthenticationSCRAMSHA512,
		Succeeded:            true,
	}
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := record["kafka.authentication.method"]; got != "scram-sha-512" {
		t.Fatalf("kafka.authentication.method = %#v", got)
	}
}

func TestObserverEmitsBoundedInspectorAndReadinessState(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observation := kafka.Observation{
		Kind:                 kafka.ObservationReadiness,
		StartedAt:            time.Unix(1, 0),
		Duration:             time.Millisecond,
		DependencyHealthy:    false,
		Ready:                true,
		ConsecutiveFailures:  2,
		ConsecutiveSuccesses: 0,
		Succeeded:            false,
		Category:             kafka.ErrorRetryable,
	}
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := map[string]any{
		"kafka.operation":                       "inspector.readiness",
		"kafka.dependency.healthy":              false,
		"kafka.readiness.ready":                 true,
		"kafka.readiness.consecutive_failures":  float64(2),
		"kafka.readiness.consecutive_successes": float64(0),
	}
	for key, value := range want {
		if got := record[key]; got != value {
			t.Fatalf("attribute %q = %#v, want %#v", key, got, value)
		}
	}
}

func TestObserverEmitsExactReplayProgress(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observation := kafka.Observation{
		Kind:            kafka.ObservationReplayRun,
		StartedAt:       time.Unix(1, 0),
		Duration:        time.Second,
		PartitionCount:  2,
		ReplayProcessed: 5,
		ReplaySkipped:   3,
		ReplayFailed:    1,
		ReplayRemaining: 8,
		Succeeded:       false,
		Category:        kafka.ErrorPermanent,
	}
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := map[string]any{
		"kafka.operation":        "replay.run",
		"kafka.replay.processed": float64(5),
		"kafka.replay.skipped":   float64(3),
		"kafka.replay.failed":    float64(1),
		"kafka.replay.remaining": float64(8),
	}
	for key, value := range want {
		if got := record[key]; got != value {
			t.Fatalf("attribute %q = %#v, want %#v", key, got, value)
		}
	}
}

func TestIdentityPolicyValidationIsBoundedAndCanonical(t *testing.T) {
	t.Parallel()

	valid := IdentityPolicy{
		AllowedClientIDs:      []string{strings.Repeat("c", 255)},
		AllowedTopics:         []string{strings.Repeat("t", 249)},
		AllowedConsumerGroups: []string{strings.Repeat("g", 255)},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	if err := (Config{Logger: logger, Identities: valid}).Validate(); err != nil {
		t.Fatalf("Config.Validate() error = %v", err)
	}

	tooMany := make([]string, 129)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("client-%d", index)
	}
	invalidUTF8 := string([]byte{0xff})
	tests := []IdentityPolicy{
		{AllowedClientIDs: []string{""}},
		{AllowedClientIDs: []string{" client"}},
		{AllowedClientIDs: []string{"client "}},
		{AllowedClientIDs: []string{"client\x00id"}},
		{AllowedClientIDs: []string{invalidUTF8}},
		{AllowedClientIDs: []string{"client", "client"}},
		{AllowedClientIDs: tooMany},
		{AllowedClientIDs: []string{strings.Repeat("c", 256)}},
		{AllowedTopics: []string{strings.Repeat("t", 250)}},
		{AllowedTopics: []string{"."}},
		{AllowedTopics: []string{".."}},
		{AllowedTopics: []string{"not a topic"}},
		{AllowedTopics: []string{"orders/created"}},
		{AllowedTopics: []string{"orders", "orders"}},
		{AllowedConsumerGroups: []string{"workers", "workers"}},
	}
	for index, policy := range tests {
		if err := policy.Validate(); !errors.Is(
			err,
			ErrInvalidIdentityPolicy,
		) {
			t.Fatalf("invalid policy %d error = %v", index, err)
		}
		if err := (Config{
			Logger:     logger,
			Identities: policy,
		}).Validate(); !errors.Is(err, ErrInvalidIdentityPolicy) {
			t.Fatalf("invalid config %d error = %v", index, err)
		}
	}
	if err := (Config{}).Validate(); !errors.Is(err, ErrLoggerRequired) {
		t.Fatalf("Config{}.Validate() error = %v", err)
	}
	if _, err := New(Config{}); !errors.Is(err, ErrLoggerRequired) {
		t.Fatalf("New(Config{}) error = %v", err)
	}
}

func TestNewCopiesIdentityPolicyAndDeniesUnlistedIdentities(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	clientIDs := []string{"allowed-client"}
	topics := []string{"allowed-topic"}
	groups := []string{"allowed-group"}
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
		Identities: IdentityPolicy{
			AllowedClientIDs:      clientIDs,
			AllowedTopics:         topics,
			AllowedConsumerGroups: groups,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	clientIDs[0] = "secret-client"
	topics[0] = "secret-topic"
	groups[0] = "secret-group"

	observation := validObservation()
	observation.ClientID = "secret-client"
	observation.Topic = "secret-topic"
	observation.GroupID = "secret-group"
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}
	rendered := output.String()
	for _, secret := range []string{
		"secret-client",
		"secret-topic",
		"secret-group",
	} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("log contains denied identity %q: %s", secret, rendered)
		}
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	for _, key := range []string{
		"messaging.client.id",
		"messaging.destination.name",
		"messaging.consumer.group.name",
		"messaging.destination.partition.id",
		"messaging.kafka.message.offset",
		"messaging.kafka.destination.broker.id",
		"messaging.kafka.protocol.api_key",
		"messaging.kafka.message.timestamp",
	} {
		if _, exists := record[key]; exists {
			t.Fatalf("unexpected optional attribute %q: %#v", key, record)
		}
	}
}

func TestObserverRejectsInvalidInputsBeforeLogging(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	adapter, err := New(Config{Logger: slog.New(handler)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := adapter.Observer()(nil, validObservation()); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Observer()(ctx, validObservation()); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled context error = %v", err)
	}
	invalid := validObservation()
	invalid.StartedAt = time.Time{}
	if err := adapter.Observer()(
		context.Background(),
		invalid,
	); !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("invalid observation error = %v", err)
	}
	var nilAdapter *Adapter
	if err := nilAdapter.Observer()(
		context.Background(),
		validObservation(),
	); !errors.Is(err, ErrLoggerRequired) {
		t.Fatalf("nil adapter error = %v", err)
	}
	if got := handler.count(); got != 0 {
		t.Fatalf("logged records = %d, want 0", got)
	}
}

func TestObserverContainsHandlerPanicAndDocumentsSlogErrorBoundary(t *testing.T) {
	t.Parallel()

	panicAdapter, err := New(Config{Logger: slog.New(panicHandler{})})
	if err != nil {
		t.Fatalf("New(panic handler) error = %v", err)
	}
	panicErr := panicAdapter.Observer()(
		context.Background(),
		validObservation(),
	)
	if !errors.Is(panicErr, ErrLoggerPanic) ||
		strings.Contains(panicErr.Error(), "credential-secret") {
		t.Fatalf("panic handler error = %v", panicErr)
	}

	handleErr := errors.New("secret handler error")
	errorAdapter, err := New(Config{
		Logger: slog.New(errorHandler{err: handleErr}),
	})
	if err != nil {
		t.Fatalf("New(error handler) error = %v", err)
	}
	if err := errorAdapter.Observer()(
		context.Background(),
		validObservation(),
	); err != nil {
		t.Fatalf("slog handler errors are not returned by slog.Logger: %v", err)
	}
}

func TestObserverEmitsStableFailureCategoryAtConfiguredLevel(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(
			&output,
			&slog.HandlerOptions{Level: slog.LevelDebug},
		)),
		Level: slog.LevelDebug,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observation := validObservation()
	observation.Succeeded = false
	observation.Category = kafka.ErrorAuthorization
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if record["level"] != "DEBUG" ||
		record["kafka.outcome"] != "failure" ||
		record["error.type"] != "authorization" {
		t.Fatalf("failure record = %#v", record)
	}
}

func TestObserverEmitsScheduledConsumerRetry(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
		Identities: IdentityPolicy{
			AllowedTopics: []string{"events"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observation := kafka.Observation{
		Kind:           kafka.ObservationConsumeRetryScheduled,
		StartedAt:      time.Unix(1, 0),
		Duration:       time.Millisecond,
		Topic:          "events",
		Partition:      1,
		PartitionKnown: true,
		Offset:         4,
		OffsetKnown:    true,
		RecordCount:    2,
		PartitionCount: 1,
		RecordBytes:    128,
		Category:       kafka.ErrorRetryable,
	}
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if record["kafka.operation"] != "consumer.retry_scheduled" ||
		record["kafka.outcome"] != "failure" ||
		record["error.type"] != "retryable" ||
		record["kafka.record.count"] != float64(2) ||
		record["messaging.destination.name"] != "events" ||
		record["messaging.destination.partition.id"] != float64(1) ||
		record["messaging.kafka.message.offset"] != float64(4) {
		t.Fatalf("retry record = %#v", record)
	}
}

func TestObserverEmitsConsumerRebalanceWait(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter, err := New(Config{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
		Identities: IdentityPolicy{
			AllowedClientIDs:      []string{"projection"},
			AllowedConsumerGroups: []string{"projection-v1"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observation := kafka.Observation{
		Kind:      kafka.ObservationConsumeRebalanceWait,
		StartedAt: time.Unix(1, 0),
		Duration:  25 * time.Millisecond,
		ClientID:  "projection",
		GroupID:   "projection-v1",
		Succeeded: true,
	}
	if err := adapter.Observer()(context.Background(), observation); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if record["kafka.operation"] != "consumer.rebalance_wait" ||
		record["kafka.outcome"] != "success" ||
		record["messaging.client.id"] != "projection" ||
		record["messaging.consumer.group.name"] != "projection-v1" ||
		record["kafka.duration_ms"] != float64(25) {
		t.Fatalf("rebalance wait record = %#v", record)
	}
}

func TestObserverIsSafeForConcurrentCalls(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	adapter, err := New(Config{Logger: slog.New(handler)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const calls = 64
	var wait sync.WaitGroup
	wait.Add(calls)
	for range calls {
		go func() {
			defer wait.Done()
			if err := adapter.Observer()(
				context.Background(),
				validObservation(),
			); err != nil {
				t.Errorf("Observer() error = %v", err)
			}
		}()
	}
	wait.Wait()
	if got := handler.count(); got != calls {
		t.Fatalf("logged records = %d, want %d", got, calls)
	}
}

func validObservation() kafka.Observation {
	return kafka.Observation{
		Kind:        kafka.ObservationProduceRecord,
		StartedAt:   time.Unix(1, 0),
		Duration:    time.Millisecond,
		RecordCount: 1,
		Succeeded:   true,
	}
}

type captureHandler struct {
	mu      sync.Mutex
	records int
}

func (*captureHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (handler *captureHandler) Handle(context.Context, slog.Record) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.records++

	return nil
}

func (handler *captureHandler) WithAttrs([]slog.Attr) slog.Handler {
	return handler
}

func (handler *captureHandler) WithGroup(string) slog.Handler {
	return handler
}

func (handler *captureHandler) count() int {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	return handler.records
}

type panicHandler struct{}

func (panicHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (panicHandler) Handle(context.Context, slog.Record) error {
	panic("credential-secret")
}

func (handler panicHandler) WithAttrs([]slog.Attr) slog.Handler {
	return handler
}

func (handler panicHandler) WithGroup(string) slog.Handler {
	return handler
}

type errorHandler struct {
	err error
}

func (errorHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (handler errorHandler) Handle(context.Context, slog.Record) error {
	return handler.err
}

func (handler errorHandler) WithAttrs([]slog.Attr) slog.Handler {
	return handler
}

func (handler errorHandler) WithGroup(string) slog.Handler {
	return handler
}
