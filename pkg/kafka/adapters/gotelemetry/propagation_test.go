package gotelemetry

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceContextPropagationInjectsOwnedW3CHeaders(t *testing.T) {
	propagation, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		t.Fatalf("NewTraceContextPropagation() error = %v", err)
	}

	member, err := baggage.NewMember("tenant", "secret-tenant")
	if err != nil {
		t.Fatalf("baggage.NewMember() error = %v", err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatalf("baggage.New() error = %v", err)
	}
	traceState, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatalf("trace.ParseTraceState() error = %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: trace.FlagsSampled,
		TraceState: traceState,
	})
	ctx := baggage.ContextWithBaggage(
		trace.ContextWithSpanContext(context.Background(), spanContext),
		bag,
	)
	record := kafka.ProducerRecord{
		Topic: "orders.v1",
		Key:   []byte("order-1"),
		Value: []byte("payload"),
		Headers: []kafka.Header{
			{Key: "content-type", Value: []byte("application/octet-stream")},
			{Key: "TraceParent", Value: []byte("stale")},
			{Key: "tracestate", Value: []byte("stale=value")},
			{Key: "schema-version", Value: []byte("1")},
			{Key: "traceſtate", Value: []byte("application-metadata")},
		},
	}
	original := retainProducerRecord(record)

	injected, err := propagation.Inject(ctx, record)
	if err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if !reflect.DeepEqual(record, original) {
		t.Fatalf("Inject() mutated caller record = %#v", record)
	}
	if got := traceHeaderValues(injected.Headers, "traceparent"); !reflect.DeepEqual(
		got,
		[]string{"00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01"},
	) {
		t.Fatalf("traceparent headers = %#v", got)
	}
	if got := traceHeaderValues(injected.Headers, "tracestate"); !reflect.DeepEqual(
		got,
		[]string{"vendor=value"},
	) {
		t.Fatalf("tracestate headers = %#v", got)
	}
	if got := traceHeaderValues(injected.Headers, "baggage"); len(got) != 0 {
		t.Fatalf("baggage headers = %#v", got)
	}
	if len(injected.Headers) != 5 ||
		injected.Headers[0].Key != "content-type" ||
		injected.Headers[1].Key != "schema-version" ||
		injected.Headers[2].Key != "traceſtate" {
		t.Fatalf("non-trace header order = %#v", injected.Headers)
	}

	injected.Key[0] = 'X'
	injected.Value[0] = 'X'
	injected.Headers[0].Value[0] = 'X'
	if !bytes.Equal(record.Key, original.Key) ||
		!bytes.Equal(record.Value, original.Value) ||
		!bytes.Equal(record.Headers[0].Value, original.Headers[0].Value) {
		t.Fatal("Inject() result aliases caller-owned bytes")
	}
}

func TestTraceContextPropagationExtractsRemoteW3CContext(t *testing.T) {
	propagation, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		t.Fatalf("NewTraceContextPropagation() error = %v", err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "preserved")
	record := kafka.ConsumedRecord{
		Topic: "orders.v1",
		Key:   []byte("order-1"),
		Value: []byte("payload"),
		Headers: []kafka.Header{
			{
				Key: "traceparent",
				Value: []byte(
					"00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01",
				),
			},
			{Key: "tracestate", Value: []byte("vendor=value")},
			{Key: "baggage", Value: []byte("tenant=secret-tenant")},
		},
	}
	original := record.Retain()

	extracted, err := propagation.Extract(ctx, record)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if got := extracted.Value(contextKey{}); got != "preserved" {
		t.Fatalf("application context value = %#v", got)
	}
	spanContext := trace.SpanContextFromContext(extracted)
	if !spanContext.IsRemote() || !spanContext.IsSampled() ||
		spanContext.TraceID() != (trace.TraceID{
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		}) ||
		spanContext.SpanID() != (trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24}) ||
		spanContext.TraceState().String() != "vendor=value" {
		t.Fatalf("extracted span context = %#v", spanContext)
	}
	if baggage.FromContext(extracted).Len() != 0 {
		t.Fatal("Extract() imported application baggage")
	}
	if !reflect.DeepEqual(record, original) {
		t.Fatalf("Extract() mutated borrowed record = %#v", record)
	}
}

func TestTraceContextPropagationRejectsInvalidOrAmbiguousFields(t *testing.T) {
	propagation, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		t.Fatalf("NewTraceContextPropagation() error = %v", err)
	}
	existing := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		SpanID:  trace.SpanID{24, 23, 22, 21, 20, 19, 18, 17},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), existing)
	traceParent := []byte(
		"00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01",
	)
	tests := map[string][]kafka.Header{
		"invalid traceparent": {
			{Key: "traceparent", Value: []byte("malformed")},
		},
		"traceparent": {
			{Key: "traceparent", Value: traceParent},
			{Key: "TraceParent", Value: bytes.Clone(traceParent)},
		},
		"tracestate": {
			{Key: "traceparent", Value: traceParent},
			{Key: "tracestate", Value: []byte("vendor=one")},
			{Key: "TraceState", Value: []byte("vendor=two")},
		},
	}
	for name, headers := range tests {
		t.Run(name, func(t *testing.T) {
			record := kafka.ConsumedRecord{Topic: "orders.v1", Headers: headers}
			extracted, extractErr := propagation.Extract(ctx, record)
			if extractErr != nil {
				t.Fatalf("Extract() error = %v", extractErr)
			}
			if got := trace.SpanContextFromContext(extracted); !got.Equal(existing) {
				t.Fatalf("duplicate field replaced context with %#v", got)
			}
		})
	}
}

func TestTraceContextPropagationEnforcesContextsAndKafkaLimits(t *testing.T) {
	if _, err := NewTraceContextPropagation(kafka.MessageLimits{}); !errors.Is(
		err,
		kafka.ErrInvalidMessageLimits,
	) {
		t.Fatalf("invalid limits error = %v", err)
	}
	propagation, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		t.Fatalf("NewTraceContextPropagation() error = %v", err)
	}
	validProducer := kafka.ProducerRecord{Topic: "orders.v1"}
	validConsumed := kafka.ConsumedRecord{Topic: "orders.v1"}
	var nilContext context.Context
	if _, err := propagation.Inject(nilContext, validProducer); !errors.Is(
		err,
		kafka.ErrContextRequired,
	) {
		t.Fatalf("Inject(nil) error = %v", err)
	}
	if _, err := propagation.Extract(nilContext, validConsumed); !errors.Is(
		err,
		kafka.ErrContextRequired,
	) {
		t.Fatalf("Extract(nil) error = %v", err)
	}
	if _, err := propagation.Inject(
		context.Background(),
		kafka.ProducerRecord{},
	); !errors.Is(err, kafka.ErrTopicRequired) {
		t.Fatalf("Inject(invalid record) error = %v", err)
	}
	if _, err := propagation.Extract(
		context.Background(),
		kafka.ConsumedRecord{},
	); !errors.Is(err, kafka.ErrTopicRequired) {
		t.Fatalf("Extract(invalid record) error = %v", err)
	}

	ctx := trace.ContextWithSpanContext(
		context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SpanID:  trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24},
		}),
	)
	limitTests := map[string]struct {
		configure func(*kafka.MessageLimits)
		record    kafka.ProducerRecord
		want      error
	}{
		"aggregate header bytes": {
			configure: func(limits *kafka.MessageLimits) { limits.MaxHeaderBytes = 65 },
			record:    validProducer,
			want:      kafka.ErrHeadersTooLarge,
		},
		"header count": {
			configure: func(limits *kafka.MessageLimits) { limits.MaxHeaders = 1 },
			record: kafka.ProducerRecord{Topic: "orders.v1", Headers: []kafka.Header{
				{Key: "application", Value: []byte("value")},
			}},
			want: kafka.ErrTooManyHeaders,
		},
		"header key bytes": {
			configure: func(limits *kafka.MessageLimits) { limits.MaxHeaderKeyBytes = 10 },
			record:    validProducer,
			want:      kafka.ErrHeaderKeyTooLarge,
		},
		"header value bytes": {
			configure: func(limits *kafka.MessageLimits) { limits.MaxHeaderValueBytes = 10 },
			record:    validProducer,
			want:      kafka.ErrHeaderValueTooLarge,
		},
	}
	for name, test := range limitTests {
		t.Run(name, func(t *testing.T) {
			limits := kafka.DefaultMessageLimits()
			test.configure(&limits)
			limited, limitErr := NewTraceContextPropagation(limits)
			if limitErr != nil {
				t.Fatalf("NewTraceContextPropagation() error = %v", limitErr)
			}
			if _, injectErr := limited.Inject(ctx, test.record); !errors.Is(
				injectErr,
				test.want,
			) {
				t.Fatalf("Inject() error = %v, want %v", injectErr, test.want)
			}
		})
	}

	zero := TraceContextPropagation{}
	if _, err := zero.Inject(context.Background(), validProducer); !errors.Is(
		err,
		kafka.ErrInvalidMessageLimits,
	) {
		t.Fatalf("zero Inject() error = %v", err)
	}
	if _, err := zero.Extract(context.Background(), validConsumed); !errors.Is(
		err,
		kafka.ErrInvalidMessageLimits,
	) {
		t.Fatalf("zero Extract() error = %v", err)
	}
}

func TestTraceContextPropagationClearsStaleFieldsWithoutSpan(t *testing.T) {
	propagation, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		t.Fatalf("NewTraceContextPropagation() error = %v", err)
	}
	record := kafka.ProducerRecord{
		Topic: "orders.v1",
		Headers: []kafka.Header{
			{Key: "traceparent", Value: []byte("stale")},
			{Key: "tracestate", Value: []byte("stale=value")},
			{Key: "application", Value: []byte("preserved")},
		},
	}

	injected, err := propagation.Inject(context.Background(), record)
	if err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if len(injected.Headers) != 1 || injected.Headers[0].Key != "application" ||
		string(injected.Headers[0].Value) != "preserved" {
		t.Fatalf("Inject() headers = %#v", injected.Headers)
	}
	if len(record.Headers) != 3 {
		t.Fatalf("Inject() mutated caller headers = %#v", record.Headers)
	}
}

func TestTraceHeaderCarrierFulfillsTextMapContract(t *testing.T) {
	carrier := traceHeaderCarrier{headers: []kafka.Header{
		{Key: "TraceParent", Value: []byte("old")},
		{Key: "application", Value: []byte("value")},
		{Key: "TRACEPARENT", Value: []byte("ambiguous")},
	}}
	if got := carrier.Get("traceparent"); got != "" {
		t.Fatalf("Get(duplicate traceparent) = %q", got)
	}
	carrier.Set("traceparent", "new")
	if got := carrier.Get("TRACEPARENT"); got != "new" {
		t.Fatalf("Get(traceparent) = %q", got)
	}
	if got := carrier.Get("missing"); got != "" {
		t.Fatalf("Get(missing) = %q", got)
	}
	if got := carrier.Keys(); !reflect.DeepEqual(
		got,
		[]string{"application", "traceparent"},
	) {
		t.Fatalf("Keys() = %#v", got)
	}
	carrier.Set("tracestate", "vendor=value")
	if got := carrier.Get("tracestate"); got != "vendor=value" {
		t.Fatalf("Get(tracestate) = %q", got)
	}
}

func retainProducerRecord(record kafka.ProducerRecord) kafka.ProducerRecord {
	retained := record
	retained.Key = bytes.Clone(record.Key)
	retained.Value = bytes.Clone(record.Value)
	retained.Headers = make([]kafka.Header, len(record.Headers))
	for index, header := range record.Headers {
		retained.Headers[index] = kafka.Header{
			Key:   header.Key,
			Value: bytes.Clone(header.Value),
		}
	}

	return retained
}

func traceHeaderValues(headers []kafka.Header, key string) []string {
	var values []string
	for _, header := range headers {
		if equalASCIIFold(header.Key, key) {
			values = append(values, string(header.Value))
		}
	}

	return values
}
