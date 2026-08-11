package gokafka

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestDeadLetterPolicyPublishesOwnedSourceRecord(t *testing.T) {
	t.Parallel()

	publisher := &deadLetterRecordingPublisher{}
	policy, err := NewDeadLetterPolicy(
		publisher,
		DeadLetterPolicyConfig{Topic: "accounts.events.dead-letter"},
	)
	if err != nil {
		t.Fatalf("construct dead-letter policy: %v", err)
	}
	record := consumedRecord(
		encodedLiveRecord(t, testRecordCodec(t), testMessage(t)),
	)
	record.Partition = 3
	record.Offset = 42
	record.Timestamp = time.Date(
		2026,
		time.July,
		25,
		12,
		34,
		56,
		123456789,
		time.FixedZone("EEST", 3*60*60),
	)
	originalKey := slices.Clone(record.Key)
	originalValue := slices.Clone(record.Value)
	originalHeaderValue := slices.Clone(record.Headers[0].Value)
	cause := errors.New("credential=secret")

	disposition, err := policy.HandleFailure(
		context.Background(),
		record,
		cause,
	)
	if err != nil || disposition != FailureHandled {
		t.Fatalf("disposition = %v, error = %v", disposition, err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("published messages = %d", len(publisher.messages))
	}
	published := publisher.messages[0]
	if published.Topic != "accounts.events.dead-letter" ||
		!slices.Equal(published.Key, originalKey) ||
		!slices.Equal(published.Value, originalValue) ||
		!published.Timestamp.Equal(record.Timestamp) {
		t.Fatalf("published record = %#v", published)
	}
	if len(published.Headers) != len(record.Headers)+4 {
		t.Fatalf("published headers = %#v", published.Headers)
	}
	assertDeadLetterHeader(
		t,
		published.Headers,
		len(record.Headers),
		HeaderDeadLetterSourceTopic,
		record.Topic,
	)
	assertDeadLetterHeader(
		t,
		published.Headers,
		len(record.Headers)+1,
		HeaderDeadLetterSourcePartition,
		"3",
	)
	assertDeadLetterHeader(
		t,
		published.Headers,
		len(record.Headers)+2,
		HeaderDeadLetterSourceOffset,
		"42",
	)
	assertDeadLetterHeader(
		t,
		published.Headers,
		len(record.Headers)+3,
		HeaderDeadLetterSourceTime,
		"2026-07-25T09:34:56.123456789Z",
	)
	if strings.Contains(stringHeaders(published.Headers), cause.Error()) {
		t.Fatal("dead-letter record disclosed failure diagnostics")
	}

	record.Key[0] ^= 0xff
	record.Value[0] ^= 0xff
	record.Headers[0].Value[0] ^= 0xff
	if !slices.Equal(published.Key, originalKey) ||
		!slices.Equal(published.Value, originalValue) ||
		!slices.Equal(published.Headers[0].Value, originalHeaderValue) {
		t.Fatal("published record retained borrowed source bytes")
	}
}

func TestNewDeadLetterPolicyValidatesDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		publisher Publisher
		topic     string
		limits    kafka.MessageLimits
		want      error
	}{
		{
			"missing publisher",
			nil,
			"events.dead-letter",
			kafka.MessageLimits{},
			ErrPublisherRequired,
		},
		{
			"empty topic",
			&deadLetterRecordingPublisher{},
			"",
			kafka.MessageLimits{},
			ErrInvalidDeadLetterTopic,
		},
		{
			"invalid topic",
			&deadLetterRecordingPublisher{},
			"events dead-letter",
			kafka.MessageLimits{},
			ErrInvalidDeadLetterTopic,
		},
		{
			"invalid limits",
			&deadLetterRecordingPublisher{},
			"events.dead-letter",
			kafka.MessageLimits{MaxTopicBytes: 250},
			ErrInvalidDeadLetterConfig,
		},
		{
			"topic exceeds limits",
			&deadLetterRecordingPublisher{},
			"events.dead-letter",
			deadLetterLimits(5, 64, 64, 8, 64, 64, 512),
			ErrInvalidDeadLetterTopic,
		},
		{
			"insufficient header count",
			&deadLetterRecordingPublisher{},
			"dead-letter",
			deadLetterLimits(64, 64, 64, 3, 64, 64, 512),
			ErrInvalidDeadLetterConfig,
		},
		{
			"insufficient header key",
			&deadLetterRecordingPublisher{},
			"dead-letter",
			deadLetterLimits(64, 64, 64, 4, 1, 64, 512),
			ErrInvalidDeadLetterConfig,
		},
		{
			"insufficient header value",
			&deadLetterRecordingPublisher{},
			"dead-letter",
			deadLetterLimits(64, 64, 64, 4, 64, 1, 512),
			ErrInvalidDeadLetterConfig,
		},
		{
			"insufficient aggregate headers",
			&deadLetterRecordingPublisher{},
			"dead-letter",
			deadLetterLimits(64, 64, 64, 4, 64, 64, 1),
			ErrInvalidDeadLetterConfig,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			policy, err := NewDeadLetterPolicy(
				test.publisher,
				DeadLetterPolicyConfig{
					Topic:  test.topic,
					Limits: test.limits,
				},
			)
			if policy != nil || !errors.Is(err, test.want) {
				t.Fatalf("policy = %#v, error = %v", policy, err)
			}
		})
	}
}

func TestDeadLetterPolicyRejectsRecordsOutsideConfiguredBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		limits  kafka.MessageLimits
		prepare func(*kafka.ConsumedMessage)
	}{
		{
			name:   "key too large",
			limits: deadLetterLimits(64, 1, 64, 8, 64, 64, 512),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Key = []byte("ab")
			},
		},
		{
			name:   "value too large",
			limits: deadLetterLimits(64, 64, 1, 8, 64, 64, 512),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Value = []byte("ab")
			},
		},
		{
			name:   "insufficient header count",
			limits: deadLetterLimits(64, 64, 64, 4, 64, 64, 512),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{{Key: "a"}}
			},
		},
		{
			name:   "empty source header key",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 64, 512),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{{Value: []byte("a")}}
			},
		},
		{
			name:   "source header key too large",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 64, 512),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{{
					Key:   strings.Repeat("a", 65),
					Value: []byte("a"),
				}}
			},
		},
		{
			name:   "source header value too large",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 20, 512),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{{
					Key:   "a",
					Value: []byte(strings.Repeat("a", 21)),
				}}
			},
		},
		{
			name:   "source header key exceeds total",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 512, 128),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{
					{
						Key:   "a",
						Value: []byte(strings.Repeat("a", 126)),
					},
					{Key: "ab"},
				}
			},
		},
		{
			name:   "source header value exceeds total",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 512, 128),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{{
					Key:   "a",
					Value: []byte(strings.Repeat("a", 128)),
				}}
			},
		},
		{
			name:   "position header value too large",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 20, 512),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Topic = strings.Repeat("a", 21)
			},
		},
		{
			name:   "position header key exceeds total",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 512, 128),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{{
					Key: "a",
					Value: []byte(strings.Repeat(
						"a",
						128-len(HeaderDeadLetterSourceTopic),
					)),
				}}
			},
		},
		{
			name:   "position header value exceeds total",
			limits: deadLetterLimits(64, 64, 64, 8, 64, 512, 128),
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = []kafka.Header{{
					Key: "a",
					Value: []byte(strings.Repeat(
						"a",
						128-
							len(HeaderDeadLetterSourceTopic)-
							len(record.Topic),
					)),
				}}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			policy, err := NewDeadLetterPolicy(
				&deadLetterRecordingPublisher{},
				DeadLetterPolicyConfig{
					Topic:  "dead-letter",
					Limits: test.limits,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			record := kafka.ConsumedMessage{
				Topic:     "events",
				Key:       []byte("a"),
				Value:     []byte("a"),
				Timestamp: time.Unix(1, 0),
				Partition: 0,
				Offset:    0,
			}
			if test.prepare != nil {
				test.prepare(&record)
			}

			disposition, err := policy.HandleFailure(
				context.Background(),
				record,
				errors.New("failed"),
			)
			if disposition != FailureRetry ||
				!errors.Is(err, ErrRecordCorrupt) {
				t.Fatalf("disposition = %v, error = %v", disposition, err)
			}
		})
	}
}

func TestDeadLetterPolicyFailsClosed(t *testing.T) {
	t.Parallel()

	publishFailure := errors.New("broker credential=secret")
	tests := []struct {
		name      string
		publisher Publisher
		prepare   func(*kafka.ConsumedMessage)
		context   func() context.Context
		cause     error
		want      error
	}{
		{
			name:      "nil context",
			publisher: &deadLetterRecordingPublisher{},
			context:   func() context.Context { return nil },
			cause:     errors.New("failed"),
			want:      ErrContextRequired,
		},
		{
			name:      "cancelled context",
			publisher: &deadLetterRecordingPublisher{},
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx
			},
			cause: errors.New("failed"),
			want:  context.Canceled,
		},
		{
			name:      "missing cause",
			publisher: &deadLetterRecordingPublisher{},
			context:   context.Background,
			want:      ErrFailureCauseRequired,
		},
		{
			name:      "invalid partition",
			publisher: &deadLetterRecordingPublisher{},
			prepare: func(record *kafka.ConsumedMessage) {
				record.Partition = -1
			},
			context: context.Background,
			cause:   errors.New("failed"),
			want:    ErrInvalidKafkaPosition,
		},
		{
			name:      "invalid offset",
			publisher: &deadLetterRecordingPublisher{},
			prepare: func(record *kafka.ConsumedMessage) {
				record.Offset = -1
			},
			context: context.Background,
			cause:   errors.New("failed"),
			want:    ErrInvalidKafkaPosition,
		},
		{
			name:      "invalid source topic",
			publisher: &deadLetterRecordingPublisher{},
			prepare: func(record *kafka.ConsumedMessage) {
				record.Topic = "invalid topic"
			},
			context: context.Background,
			cause:   errors.New("failed"),
			want:    ErrRecordCorrupt,
		},
		{
			name:      "source is dead-letter topic",
			publisher: &deadLetterRecordingPublisher{},
			prepare: func(record *kafka.ConsumedMessage) {
				record.Topic = "events.dead-letter"
			},
			context: context.Background,
			cause:   errors.New("failed"),
			want:    ErrDeadLetterLoop,
		},
		{
			name:      "reserved header collision",
			publisher: &deadLetterRecordingPublisher{},
			prepare: func(record *kafka.ConsumedMessage) {
				record.Headers = append(record.Headers, kafka.Header{
					Key:   "esdlq.future_header",
					Value: []byte("1"),
				})
			},
			context: context.Background,
			cause:   errors.New("failed"),
			want:    ErrDeadLetterLoop,
		},
		{
			name:      "zero source time",
			publisher: &deadLetterRecordingPublisher{},
			prepare: func(record *kafka.ConsumedMessage) {
				record.Timestamp = time.Time{}
			},
			context: context.Background,
			cause:   errors.New("failed"),
			want:    ErrRecordCorrupt,
		},
		{
			name:      "publisher failure",
			publisher: &deadLetterRecordingPublisher{err: publishFailure},
			context:   context.Background,
			cause:     errors.New("failed"),
			want:      publishFailure,
		},
		{
			name:      "publisher panic",
			publisher: &deadLetterRecordingPublisher{panicValue: "secret"},
			context:   context.Background,
			cause:     errors.New("failed"),
			want:      ErrDeadLetterPublisherPanic,
		},
		{
			name:      "cancelled after publish",
			publisher: &deadLetterRecordingPublisher{cancel: true},
			context:   context.Background,
			cause:     context.Canceled,
			want:      context.Canceled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			policy, err := NewDeadLetterPolicy(
				test.publisher,
				DeadLetterPolicyConfig{Topic: "events.dead-letter"},
			)
			if err != nil {
				t.Fatal(err)
			}
			record := consumedRecord(
				encodedLiveRecord(t, testRecordCodec(t), testMessage(t)),
			)
			record.Partition = 2
			record.Offset = 7
			record.Timestamp = time.Date(
				2026,
				time.July,
				25,
				10,
				0,
				0,
				0,
				time.UTC,
			)
			if test.prepare != nil {
				test.prepare(&record)
			}
			ctx := test.context()
			if publisher, ok := test.publisher.(*deadLetterRecordingPublisher); ok &&
				publisher.cancel {
				cancelCtx, cancel := context.WithCancel(ctx)
				publisher.cancelContext = cancel
				ctx = cancelCtx
			}

			disposition, err := policy.HandleFailure(ctx, record, test.cause)
			if disposition != FailureRetry || !errors.Is(err, test.want) {
				t.Fatalf("disposition = %v, error = %v", disposition, err)
			}
			if errors.Is(err, publishFailure) &&
				strings.Contains(err.Error(), "credential") {
				t.Fatalf("error disclosed publisher diagnostics: %v", err)
			}
			if test.name == "publisher failure" {
				var deadLetterError *DeadLetterError
				if !errors.As(err, &deadLetterError) {
					t.Fatalf("error type = %T, want *DeadLetterError", err)
				}
				if deadLetterError.SourceTopic() != record.Topic ||
					deadLetterError.Partition() != record.Partition ||
					deadLetterError.Offset() != record.Offset {
					t.Fatalf(
						"dead-letter position = %s/%d/%d",
						deadLetterError.SourceTopic(),
						deadLetterError.Partition(),
						deadLetterError.Offset(),
					)
				}
			}
		})
	}

	var nilPolicy *DeadLetterPolicy
	disposition, err := nilPolicy.HandleFailure(
		context.Background(),
		kafka.ConsumedMessage{},
		errors.New("failed"),
	)
	if disposition != FailureRetry || !errors.Is(err, ErrPublisherRequired) {
		t.Fatalf("nil policy disposition = %v, error = %v", disposition, err)
	}
}

func TestDeadLetterPolicySettlesRecordHandlerAfterPublish(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	publisher := &deadLetterRecordingPublisher{}
	policy, err := NewDeadLetterPolicy(
		publisher,
		DeadLetterPolicyConfig{Topic: "events.dead-letter"},
	)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return errors.New("projection failed")
		}),
		WithFailurePolicy(policy),
	)
	if err != nil {
		t.Fatal(err)
	}
	record := consumedRecord(
		encodedLiveRecord(t, codec, testMessage(t)),
	)
	record.Partition = 1
	record.Offset = 9
	record.Timestamp = time.Now().UTC()

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle failed record: %v", err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("dead-letter messages = %d", len(publisher.messages))
	}
}

type deadLetterRecordingPublisher struct {
	messages      []kafka.Message
	err           error
	panicValue    any
	cancel        bool
	cancelContext context.CancelFunc
}

func (publisher *deadLetterRecordingPublisher) Publish(
	_ context.Context,
	message kafka.Message,
) error {
	if publisher.panicValue != nil {
		panic(publisher.panicValue)
	}
	publisher.messages = append(publisher.messages, message)
	if publisher.cancelContext != nil {
		publisher.cancelContext()
	}

	return publisher.err
}

func assertDeadLetterHeader(
	t *testing.T,
	headers []kafka.Header,
	index int,
	key string,
	value string,
) {
	t.Helper()

	if headers[index].Key != key || string(headers[index].Value) != value {
		t.Fatalf("header %d = %#v", index, headers[index])
	}
}

func stringHeaders(headers []kafka.Header) string {
	var builder strings.Builder
	for _, header := range headers {
		builder.WriteString(header.Key)
		builder.Write(header.Value)
	}

	return builder.String()
}

func deadLetterLimits(
	topic int,
	key int,
	value int,
	headers int,
	headerKey int,
	headerValue int,
	headerBytes int,
) kafka.MessageLimits {
	return kafka.MessageLimits{
		MaxTopicBytes:       topic,
		MaxKeyBytes:         key,
		MaxValueBytes:       value,
		MaxHeaders:          headers,
		MaxHeaderKeyBytes:   headerKey,
		MaxHeaderValueBytes: headerValue,
		MaxHeaderBytes:      headerBytes,
	}
}
