package gokafka

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestRecordCodecBoundaryContracts(t *testing.T) {
	t.Parallel()

	topics := make([]string, maxAllowedTopics)
	for index := range topics {
		topics[index] = fmt.Sprintf("topic-%d", index)
	}
	if _, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic(topics[0]),
		AllowedTopics: topics,
	}); err != nil {
		t.Fatalf("construct codec at topic limit: %v", err)
	}
	if _, err := NewRecordCodec(RecordCodecConfig{
		Resolver:      FixedTopic(topics[0]),
		AllowedTopics: append(topics, "topic-overflow"),
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("topic overflow error = %v", err)
	}

	headers := []kafka.Header{{Key: "application-header", Value: []byte("ignored")}}
	for _, key := range []string{
		HeaderMessageID,
		HeaderAggregateType,
		HeaderAggregateID,
		HeaderStreamVersion,
		HeaderEventName,
		HeaderEventSchemaVersion,
		HeaderContentType,
		HeaderRecordedAt,
		HeaderApplicationMetadata,
		HeaderDeliveryMode,
	} {
		headers = append(headers, kafka.Header{Key: key, Value: []byte("value")})
	}
	parsedHeaders, err := parseHeaders(headers)
	if err != nil {
		t.Fatalf("parse complete headers: %v", err)
	}
	if len(parsedHeaders) != len(headers)-1 {
		t.Fatalf("parsed headers = %#v", parsedHeaders)
	}
	if _, retained := parsedHeaders["application-header"]; retained {
		t.Fatal("non-reserved header was retained")
	}

	maximumUint32 := uint64(^uint32(0))
	value, err := requiredUint32(
		map[string]string{"value": fmt.Sprintf("%d", maximumUint32)},
		"value",
	)
	if err != nil || uint64(value) != maximumUint32 {
		t.Fatalf("maximum uint32 = %d, error = %v", value, err)
	}
	if _, err := requiredUint32(
		map[string]string{"value": fmt.Sprintf("%d", maximumUint32+1)},
		"value",
	); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("uint32 overflow error = %v", err)
	}
	if _, err := requiredUint32(map[string]string{}, "value"); !errors.Is(
		err,
		ErrRecordCorrupt,
	) {
		t.Fatalf("missing uint32 error = %v", err)
	}

	canonical := "2026-07-25T10:11:12.123456Z"
	if parsed, err := canonicalTime(canonical); err != nil ||
		parsed.Format(time.RFC3339Nano) != canonical {
		t.Fatalf("canonical time = %s, error = %v", parsed, err)
	}
	for _, value := range []string{
		"not-a-time",
		"0001-01-01T00:00:00Z",
		"2026-07-25T11:11:12.123456+01:00",
	} {
		if _, err := canonicalTime(value); !errors.Is(err, ErrRecordCorrupt) {
			t.Fatalf("non-canonical time %q error = %v", value, err)
		}
	}

	limits := kafka.MessageLimits{
		MaxTopicBytes:       1,
		MaxKeyBytes:         2,
		MaxValueBytes:       2,
		MaxHeaders:          2,
		MaxHeaderKeyBytes:   2,
		MaxHeaderValueBytes: 2,
		MaxHeaderBytes:      8,
	}
	validHeaders := []kafka.Header{
		{Key: "ab", Value: []byte("cd")},
		{Key: "ef", Value: []byte("gh")},
	}
	if err := validateRecord("t", []byte("ab"), []byte("cd"), validHeaders, limits); err != nil {
		t.Fatalf("validate exact record limits: %v", err)
	}
	keyBoundaryLimits := limits
	keyBoundaryLimits.MaxHeaderBytes = 6
	if err := validateRecord(
		"t",
		[]byte("a"),
		[]byte("b"),
		[]kafka.Header{
			{Key: "ab", Value: []byte("cd")},
			{Key: "ef"},
		},
		keyBoundaryLimits,
	); err != nil {
		t.Fatalf("validate exact aggregate header key: %v", err)
	}
	invalidRecords := []struct {
		name    string
		topic   string
		key     []byte
		value   []byte
		headers []kafka.Header
	}{
		{"topic", "tt", []byte("a"), []byte("b"), nil},
		{"empty key", "t", nil, []byte("b"), nil},
		{"key", "t", []byte("abc"), []byte("b"), nil},
		{"empty value", "t", []byte("a"), nil, nil},
		{"value", "t", []byte("a"), []byte("bcd"), nil},
		{
			"header count",
			"t",
			[]byte("a"),
			[]byte("b"),
			[]kafka.Header{{Key: "a"}, {Key: "b"}, {Key: "c"}},
		},
		{"empty header key", "t", []byte("a"), []byte("b"), []kafka.Header{{}}},
		{
			"header key",
			"t",
			[]byte("a"),
			[]byte("b"),
			[]kafka.Header{{Key: "abc"}},
		},
		{
			"header value",
			"t",
			[]byte("a"),
			[]byte("b"),
			[]kafka.Header{{Key: "a", Value: []byte("abc")}},
		},
	}
	for _, test := range invalidRecords {
		if err := validateRecord(
			test.topic,
			test.key,
			test.value,
			test.headers,
			limits,
		); !errors.Is(err, ErrRecordCorrupt) {
			t.Fatalf("%s error = %v", test.name, err)
		}
	}
	aggregateLimits := limits
	aggregateLimits.MaxHeaderBytes = 4
	if err := validateRecord(
		"t",
		[]byte("a"),
		[]byte("b"),
		[]kafka.Header{
			{Key: "a", Value: []byte("b")},
			{Key: "c", Value: []byte("de")},
		},
		aggregateLimits,
	); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("aggregate header error = %v", err)
	}
	if err := validateRecord(
		"t",
		[]byte("a"),
		[]byte("b"),
		[]kafka.Header{
			{Key: "a", Value: []byte("bc")},
			{Key: "de"},
		},
		aggregateLimits,
	); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("aggregate header key error = %v", err)
	}

	if !validTopic("azAZ09._-", len("azAZ09._-")) {
		t.Fatal("topic character endpoints were rejected")
	}
	for _, topic := range []string{"`", "{", "@", "[", "/", ":", " "} {
		if validTopic(topic, len(topic)) {
			t.Fatalf("invalid topic %q was accepted", topic)
		}
	}
	if validTopic("aa", 1) {
		t.Fatal("oversized topic was accepted")
	}

	category := errors.New("category")
	cause := errors.New("cause")
	if recordFailure(category, nil) != category ||
		recordFailure(category, category) != category {
		t.Fatal("record failure did not preserve its category")
	}
	combined := recordFailure(category, cause)
	if !errors.Is(combined, category) || !errors.Is(combined, cause) {
		t.Fatalf("combined record failure = %v", combined)
	}
}

func TestDeadLetterBoundaryContracts(t *testing.T) {
	t.Parallel()

	longestKey := max(
		len(HeaderDeadLetterSourceTopic),
		len(HeaderDeadLetterSourcePartition),
		len(HeaderDeadLetterSourceOffset),
		len(HeaderDeadLetterSourceTime),
	)
	timestampBytes := len("1970-01-01T00:00:00Z")
	minimumHeaderBytes := len(HeaderDeadLetterSourceTopic) +
		len(HeaderDeadLetterSourcePartition) +
		len(HeaderDeadLetterSourceOffset) +
		len(HeaderDeadLetterSourceTime) +
		3 +
		timestampBytes
	exact := kafka.MessageLimits{
		MaxHeaders:          4,
		MaxHeaderKeyBytes:   longestKey,
		MaxHeaderValueBytes: timestampBytes,
		MaxHeaderBytes:      minimumHeaderBytes,
	}
	if !validDeadLetterLimits(exact) {
		t.Fatal("exact dead-letter limits were rejected")
	}
	for _, limits := range []kafka.MessageLimits{
		func() kafka.MessageLimits { value := exact; value.MaxHeaders--; return value }(),
		func() kafka.MessageLimits { value := exact; value.MaxHeaderKeyBytes--; return value }(),
		func() kafka.MessageLimits { value := exact; value.MaxHeaderValueBytes--; return value }(),
		func() kafka.MessageLimits { value := exact; value.MaxHeaderBytes--; return value }(),
	} {
		if validDeadLetterLimits(limits) {
			t.Fatalf("insufficient dead-letter limits were accepted: %#v", limits)
		}
	}

	record := kafka.ConsumedMessage{
		Topic:     "events",
		Partition: 12,
		Offset:    34,
		Key:       []byte("key"),
		Value:     []byte("value"),
		Timestamp: time.Unix(1, 0).UTC(),
		Headers: []kafka.Header{
			{Key: "a", Value: []byte("b")},
			{Key: "c", Value: []byte("d")},
		},
	}
	positionHeaders := deadLetterPositionHeaders(record)
	headerBytes := 0
	maximumHeaderKeyBytes := 0
	maximumHeaderValueBytes := 0
	for _, item := range append(record.Headers, positionHeaders[:]...) {
		headerBytes += len(item.Key) + len(item.Value)
		maximumHeaderKeyBytes = max(maximumHeaderKeyBytes, len(item.Key))
		maximumHeaderValueBytes = max(maximumHeaderValueBytes, len(item.Value))
	}
	limits := kafka.MessageLimits{
		MaxTopicBytes:       len(record.Topic),
		MaxKeyBytes:         len(record.Key),
		MaxValueBytes:       len(record.Value),
		MaxHeaders:          len(record.Headers) + len(positionHeaders),
		MaxHeaderKeyBytes:   maximumHeaderKeyBytes,
		MaxHeaderValueBytes: maximumHeaderValueBytes,
		MaxHeaderBytes:      headerBytes,
	}
	if err := validateDeadLetterRecord(record, limits); err != nil {
		t.Fatalf("validate exact dead-letter record: %v", err)
	}
	for _, mutate := range []func(*kafka.MessageLimits){
		func(value *kafka.MessageLimits) { value.MaxTopicBytes-- },
		func(value *kafka.MessageLimits) { value.MaxKeyBytes-- },
		func(value *kafka.MessageLimits) { value.MaxValueBytes-- },
		func(value *kafka.MessageLimits) { value.MaxHeaders-- },
		func(value *kafka.MessageLimits) { value.MaxHeaderKeyBytes-- },
		func(value *kafka.MessageLimits) { value.MaxHeaderValueBytes-- },
		func(value *kafka.MessageLimits) { value.MaxHeaderBytes-- },
	} {
		insufficient := limits
		mutate(&insufficient)
		if err := validateDeadLetterRecord(record, insufficient); !errors.Is(
			err,
			ErrRecordCorrupt,
		) {
			t.Fatalf("insufficient limits %#v error = %v", insufficient, err)
		}
	}
	invalidHeader := record
	invalidHeader.Headers = []kafka.Header{{Value: []byte("value")}}
	if err := validateDeadLetterRecord(invalidHeader, limits); !errors.Is(
		err,
		ErrRecordCorrupt,
	) {
		t.Fatalf("empty header key error = %v", err)
	}
	zeroTimestamp := record
	zeroTimestamp.Timestamp = time.Time{}
	if err := validateDeadLetterRecord(zeroTimestamp, limits); !errors.Is(
		err,
		ErrRecordCorrupt,
	) {
		t.Fatalf("zero timestamp error = %v", err)
	}
}

func TestDispatcherReplayDenialReportsExactProgress(t *testing.T) {
	t.Parallel()

	dispatcher, err := NewDispatcher(&controlledPublisher{}, testRecordCodec(t))
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	replay, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryReplay,
	)
	if err != nil {
		t.Fatalf("construct replay delivery: %v", err)
	}
	err = dispatcher.Dispatch(context.Background(), []eventsourcing.Delivery{replay})
	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) ||
		dispatchErr.Published() != 0 ||
		dispatchErr.Failed() != 1 ||
		dispatchErr.Attempted() != 1 ||
		dispatchErr.Total() != 1 {
		t.Fatalf("replay denial progress = %#v, error = %v", dispatchErr, err)
	}
}
