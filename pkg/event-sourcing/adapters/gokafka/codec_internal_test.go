package gokafka

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestNewRecordCodecValidatesAndOwnsConfiguration(t *testing.T) {
	t.Parallel()

	invalidLimits := DefaultRecordLimits()
	invalidLimits.MaxTopicBytes = 0
	tooManyTopics := make([]string, maxAllowedTopics+1)
	for index := range tooManyTopics {
		tooManyTopics[index] = "topic-" + strings.Repeat("x", index)
	}

	tests := map[string]struct {
		config RecordCodecConfig
		target error
	}{
		"resolver": {
			config: RecordCodecConfig{
				AllowedTopics: []string{"events"},
			},
			target: ErrResolverRequired,
		},
		"limits": {
			config: RecordCodecConfig{
				Resolver:      FixedTopic("events"),
				AllowedTopics: []string{"events"},
				Limits:        invalidLimits,
			},
			target: kafka.ErrInvalidMessageLimits,
		},
		"empty allowlist": {
			config: RecordCodecConfig{Resolver: FixedTopic("events")},
			target: ErrInvalidConfig,
		},
		"bounded allowlist": {
			config: RecordCodecConfig{
				Resolver:      FixedTopic("events"),
				AllowedTopics: tooManyTopics,
			},
			target: ErrInvalidConfig,
		},
		"duplicate topic": {
			config: RecordCodecConfig{
				Resolver:      FixedTopic("events"),
				AllowedTopics: []string{"events", "events"},
			},
			target: ErrInvalidConfig,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewRecordCodec(test.config)
			if !errors.Is(err, test.target) {
				t.Fatalf("error = %v, want %v", err, test.target)
			}
		})
	}

	topics := []string{"events"}
	codec, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic("events"),
		AllowedTopics: topics,
	})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}
	topics[0] = "mutated"
	delivery, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}
	if _, err := codec.Encode(delivery); err != nil {
		t.Fatalf("encode after caller mutation: %v", err)
	}
}

func TestTopicResolverFuncRejectsNilFunction(t *testing.T) {
	t.Parallel()

	var resolver TopicResolverFunc
	if _, err := resolver.ResolveTopic(testMessage(t)); !errors.Is(
		err,
		ErrResolverRequired,
	) {
		t.Fatalf("error = %v, want ErrResolverRequired", err)
	}
}

func TestRecordCodecEncodeRejectsInvalidRoutingAndBounds(t *testing.T) {
	t.Parallel()

	delivery, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}
	if _, err := (*RecordCodec)(nil).Encode(delivery); !errors.Is(
		err,
		ErrRecordInvalid,
	) {
		t.Fatalf("nil codec error = %v", err)
	}
	codec := testRecordCodec(t)
	if _, err := codec.Encode(eventsourcing.Delivery{}); !errors.Is(
		err,
		ErrRecordInvalid,
	) {
		t.Fatalf("zero delivery error = %v", err)
	}

	sensitive := errors.New("credential=secret")
	errorCodec, err := NewRecordCodec(RecordCodecConfig{
		Resolver: TopicResolverFunc(func(
			eventsourcing.Message,
		) (string, error) {
			return "", sensitive
		}),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		t.Fatalf("construct error codec: %v", err)
	}
	_, err = errorCodec.Encode(delivery)
	if !errors.Is(err, ErrRecordInvalid) || !errors.Is(err, sensitive) {
		t.Fatalf("resolver error = %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("resolver diagnostic was disclosed: %v", err)
	}
	var recordError *RecordError
	if !errors.As(err, &recordError) || len(recordError.Unwrap()) != 2 {
		t.Fatalf("typed resolver error = %T", err)
	}

	panicCodec, err := NewRecordCodec(RecordCodecConfig{
		Resolver: TopicResolverFunc(func(
			eventsourcing.Message,
		) (string, error) {
			panic("secret")
		}),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		t.Fatalf("construct panic codec: %v", err)
	}
	_, err = panicCodec.Encode(delivery)
	if !errors.Is(err, ErrResolverPanic) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("resolver panic error = %v", err)
	}

	deniedCodec, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic("denied"),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		t.Fatalf("construct denied codec: %v", err)
	}
	_, err = deniedCodec.Encode(delivery)
	if !errors.Is(err, ErrRecordInvalid) ||
		!errors.Is(err, ErrTopicDenied) {
		t.Fatalf("denied topic error = %v", err)
	}

	limits := DefaultRecordLimits()
	limits.MaxValueBytes = 1
	boundedCodec, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic("accounts.events.v1"),
		AllowedTopics: []string{"accounts.events.v1"},
		Limits:        limits,
	})
	if err != nil {
		t.Fatalf("construct bounded codec: %v", err)
	}
	_, err = boundedCodec.Encode(delivery)
	if !errors.Is(err, ErrRecordInvalid) {
		t.Fatalf("bounded record error = %v", err)
	}
}

func TestRecordCodecDecodeRejectsCorruptRecords(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := testEncodedRecord(t, codec)

	tests := map[string]func(*kafka.ConsumedMessage){
		"topic denied": func(message *kafka.ConsumedMessage) {
			message.Topic = "denied"
		},
		"empty key": func(message *kafka.ConsumedMessage) {
			message.Key = nil
		},
		"empty payload": func(message *kafka.ConsumedMessage) {
			message.Value = nil
		},
		"too many headers": func(message *kafka.ConsumedMessage) {
			for len(message.Headers) <= DefaultRecordLimits().MaxHeaders {
				message.Headers = append(
					message.Headers,
					kafka.Header{Key: "trace", Value: []byte("x")},
				)
			}
		},
		"empty header key": func(message *kafka.ConsumedMessage) {
			message.Headers = append(
				message.Headers,
				kafka.Header{Value: []byte("x")},
			)
		},
		"large header key": func(message *kafka.ConsumedMessage) {
			message.Headers = append(message.Headers, kafka.Header{
				Key: strings.Repeat(
					"k",
					DefaultRecordLimits().MaxHeaderKeyBytes+1,
				),
			})
		},
		"large header value": func(message *kafka.ConsumedMessage) {
			message.Headers = append(message.Headers, kafka.Header{
				Key: "trace",
				Value: []byte(strings.Repeat(
					"x",
					DefaultRecordLimits().MaxHeaderValueBytes+1,
				)),
			})
		},
		"large total headers": func(message *kafka.ConsumedMessage) {
			value := []byte(strings.Repeat("x", 40<<10))
			message.Headers = append(
				message.Headers,
				kafka.Header{Key: "trace-a", Value: value},
				kafka.Header{Key: "trace-b", Value: value},
			)
		},
		"unknown reserved header": func(message *kafka.ConsumedMessage) {
			message.Headers = append(message.Headers, kafka.Header{
				Key:   "es.future",
				Value: []byte("x"),
			})
		},
		"duplicate reserved header": func(message *kafka.ConsumedMessage) {
			message.Headers = append(
				message.Headers,
				kafka.Header{
					Key:   HeaderMessageID,
					Value: []byte("duplicate"),
				},
			)
		},
		"missing required header": func(message *kafka.ConsumedMessage) {
			deleteHeader(message, HeaderEventName)
		},
		"aggregate key mismatch": func(message *kafka.ConsumedMessage) {
			message.Key = []byte("other")
		},
		"stream version": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderStreamVersion, "01")
		},
		"schema version": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderEventSchemaVersion, "4294967296")
		},
		"global position": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderGlobalPosition, "0")
		},
		"recorded time": func(message *kafka.ConsumedMessage) {
			setHeader(
				message,
				HeaderRecordedAt,
				"2026-07-25T12:11:12.123456+02:00",
			)
		},
		"metadata JSON": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderApplicationMetadata, `{"source":`)
		},
		"metadata null": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderApplicationMetadata, "null")
		},
		"metadata not canonical": func(message *kafka.ConsumedMessage) {
			setHeader(
				message,
				HeaderApplicationMetadata,
				`{"source":"test", "region":"eu"}`,
			)
		},
		"aggregate type": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderAggregateType, "invalid type")
		},
		"event name": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderEventName, "invalid event")
		},
		"message ID": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderMessageID, "invalid id")
		},
		"correlation ID": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderCorrelationID, "invalid id")
		},
		"delivery mode": func(message *kafka.ConsumedMessage) {
			setHeader(message, HeaderDeliveryMode, "unknown")
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate := consumedRecord(record)
			mutate(&candidate)
			_, err := codec.Decode(candidate)
			if !errors.Is(err, ErrRecordCorrupt) {
				t.Fatalf("error = %v, want ErrRecordCorrupt", err)
			}
		})
	}

	if _, err := (*RecordCodec)(nil).Decode(
		consumedRecord(record),
	); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("nil codec error = %v", err)
	}
}

func TestRecordCodecRoundTripsMinimalLiveDeliveryWithTraceHeader(
	t *testing.T,
) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-7")
	if err != nil {
		t.Fatalf("construct stream: %v", err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.opened",
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte(`{"owner":"Ada"}`),
		},
	)
	if err != nil {
		t.Fatalf("construct event: %v", err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-7",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, time.July, 25, 8, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("construct pending message: %v", err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatalf("construct message: %v", err)
	}
	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}
	codec := testRecordCodec(t)
	record, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("encode delivery: %v", err)
	}
	record.Headers = append(record.Headers, kafka.Header{
		Key:   "traceparent",
		Value: []byte("00-opaque"),
	})

	decoded, err := codec.Decode(consumedRecord(record))
	if err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if decoded.Mode() != eventsourcing.DeliveryLive ||
		!decoded.Message().Equal(message) {
		t.Fatalf("decoded delivery = %#v", decoded)
	}
}

func TestRecordCodecOwnsEncodedAndDecodedBytes(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	message := testMessage(t)
	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}
	record, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("encode delivery: %v", err)
	}
	record.Value[0] = '!'
	record.Key[0] = '!'
	record.Headers[0].Value[0] = '!'
	if string(message.Event().Payload()) != `{"amount":9007199254740993}` {
		t.Fatal("encoded record mutation changed source message")
	}

	record = testEncodedRecord(t, codec)
	consumed := consumedRecord(record)
	decoded, err := codec.Decode(consumed)
	if err != nil {
		t.Fatalf("decode record: %v", err)
	}
	consumed.Value[0] = '!'
	consumed.Key[0] = '!'
	for index := range consumed.Headers {
		if len(consumed.Headers[index].Value) > 0 {
			consumed.Headers[index].Value[0] = '!'
		}
	}
	if !decoded.Message().Equal(message) {
		t.Fatal("source record mutation changed decoded message")
	}
}

func TestRecordHelpersClassifyErrors(t *testing.T) {
	t.Parallel()

	if got := recordFailure(ErrRecordInvalid, nil); !errors.Is(
		got,
		ErrRecordInvalid,
	) {
		t.Fatalf("nil cause = %v", got)
	}
	if got := recordFailure(
		ErrRecordInvalid,
		ErrRecordInvalid,
	); !errors.Is(got, ErrRecordInvalid) {
		t.Fatalf("same cause = %v", got)
	}
	if knownHeader("traceparent") {
		t.Fatal("trace header was classified as reserved")
	}
	if validTopic("", 249) ||
		validTopic("events", 5) ||
		validTopic("bad topic", 249) ||
		!validTopic("account.events_v1-compact", 249) {
		t.Fatal("topic validation result is incorrect")
	}
}

func testRecordCodec(t testing.TB) *RecordCodec {
	t.Helper()

	codec, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic("accounts.events.v1"),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		t.Fatalf("construct codec: %v", err)
	}

	return codec
}

func testEncodedRecord(
	t testing.TB,
	codec *RecordCodec,
) kafka.Message {
	t.Helper()

	delivery, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryReplay,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}
	record, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("encode delivery: %v", err)
	}

	return record
}

func consumedRecord(record kafka.Message) kafka.ConsumedMessage {
	headers := make([]kafka.Header, len(record.Headers))
	for index, item := range record.Headers {
		headers[index] = kafka.Header{
			Key:   item.Key,
			Value: slices.Clone(item.Value),
		}
	}

	return kafka.ConsumedMessage{
		Topic:   record.Topic,
		Key:     slices.Clone(record.Key),
		Value:   slices.Clone(record.Value),
		Headers: headers,
	}
}

func setHeader(record *kafka.ConsumedMessage, key, value string) {
	for index := range record.Headers {
		if record.Headers[index].Key == key {
			record.Headers[index].Value = []byte(value)

			return
		}
	}
	record.Headers = append(
		record.Headers,
		kafka.Header{Key: key, Value: []byte(value)},
	)
}

func deleteHeader(record *kafka.ConsumedMessage, key string) {
	for index := range record.Headers {
		if record.Headers[index].Key == key {
			record.Headers = slices.Delete(record.Headers, index, index+1)

			return
		}
	}
}
