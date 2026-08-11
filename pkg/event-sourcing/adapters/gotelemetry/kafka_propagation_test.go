package gotelemetry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestKafkaPublisherInjectsOwnedTraceContext(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	downstream := &recordingKafkaPublisher{}
	publisher, err := instrumentation.WrapKafkaPublisher(
		downstream,
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	message := kafka.Message{
		Topic:     "events",
		Partition: kafka.ExplicitPartition(7),
		Key:       []byte("aggregate-1"),
		Value:     []byte("payload"),
		Timestamp: time.Date(2026, time.August, 11, 12, 13, 14, 15, time.UTC),
		Headers: []kafka.Header{
			{Key: "traceparent", Value: []byte("stale")},
			{Key: "es.event_name", Value: []byte("order.placed")},
		},
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID:    trace.TraceID{1},
			SpanID:     trace.SpanID{2},
			TraceFlags: trace.FlagsSampled,
		},
	))

	if err := publisher.Publish(ctx, message); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if got := kafkaHeaderValue(downstream.message.Headers, "traceparent"); !strings.HasPrefix(got, "00-") {
		t.Fatalf("traceparent = %q, want W3C context", got)
	}
	if got := kafkaHeaderCount(downstream.message.Headers, "traceparent"); got != 1 {
		t.Fatalf("traceparent count = %d, want 1", got)
	}
	if got := kafkaHeaderValue(downstream.message.Headers, "es.event_name"); got != "order.placed" {
		t.Fatalf("event name = %q", got)
	}
	if downstream.message.Partition != message.Partition {
		t.Fatalf("partition = %#v, want %#v", downstream.message.Partition, message.Partition)
	}
	if !downstream.message.Timestamp.Equal(message.Timestamp) {
		t.Fatalf("timestamp = %v, want %v", downstream.message.Timestamp, message.Timestamp)
	}
	downstream.message.Key[0] = 'X'
	downstream.message.Value[0] = 'X'
	downstream.message.Headers[0].Value[0] = 'X'
	if string(message.Key) != "aggregate-1" ||
		string(message.Value) != "payload" ||
		string(message.Headers[0].Value) != "stale" {
		t.Fatal("Publish() transferred caller-owned message storage")
	}
}

func TestKafkaPublisherRemovesUntrustedPropagationWithoutContext(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	downstream := &recordingKafkaPublisher{}
	publisher, err := instrumentation.WrapKafkaPublisher(
		downstream,
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}

	err = publisher.Publish(context.Background(), kafka.Message{
		Topic: "events",
		Headers: []kafka.Header{
			{Key: "TraceParent", Value: []byte("untrusted")},
			{Key: "tracestate", Value: []byte("untrusted")},
		},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(downstream.message.Headers) != 0 {
		t.Fatalf("headers = %#v, want stale propagation removed", downstream.message.Headers)
	}
}

func TestKafkaPublisherRemovesUndeclaredW3CPropagation(t *testing.T) {
	tests := map[string]propagation.TextMapPropagator{
		"no declared fields": fieldPropagator{},
		"tracestate only": fieldPropagator{
			fields: []string{"tracestate"},
			setKey: "tracestate",
			value:  "vendor=fresh",
		},
	}
	for name, propagator := range tests {
		t.Run(name, func(t *testing.T) {
			instrumentation := newKafkaTestInstrumentation(t, propagator)
			downstream := &recordingKafkaPublisher{}
			publisher, err := instrumentation.WrapKafkaPublisher(
				downstream,
				KafkaPropagationConfig{},
			)
			if err != nil {
				t.Fatalf("WrapKafkaPublisher() error = %v", err)
			}

			err = publisher.Publish(context.Background(), kafka.Message{
				Topic: "events",
				Headers: []kafka.Header{
					{Key: "traceparent", Value: []byte("stale-parent")},
					{Key: "tracestate", Value: []byte("vendor=stale")},
				},
			})
			if err != nil {
				t.Fatalf("Publish() error = %v", err)
			}
			if got := kafkaHeaderValue(downstream.message.Headers, "traceparent"); got != "" {
				t.Fatalf("traceparent = %q, want removed", got)
			}
			wantState := ""
			if name == "tracestate only" {
				wantState = "vendor=fresh"
			}
			if got := kafkaHeaderValue(downstream.message.Headers, "tracestate"); got != wantState {
				t.Fatalf("tracestate = %q, want %q", got, wantState)
			}
		})
	}
}

func TestKafkaHandlerExtractsRemoteParentWithoutMutatingMessage(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	var received trace.SpanContext
	handler, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(ctx context.Context, message kafka.ConsumedMessage) error {
			received = trace.SpanContextFromContext(ctx)
			message.Headers[0].Value[0] = 'X'
			return nil
		}),
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler() error = %v", err)
	}
	value := []byte("00-00000000000000000000000000000001-0000000000000002-01")
	message := kafka.ConsumedMessage{
		Topic:   "events",
		Headers: []kafka.Header{{Key: "traceparent", Value: value}},
	}

	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !received.IsRemote() ||
		received.TraceID() != (trace.TraceID{15: 1}) ||
		received.SpanID() != (trace.SpanID{7: 2}) {
		t.Fatalf("received span context = %v", received)
	}
	if value[0] != '0' {
		t.Fatal("Handle() transferred caller-owned header storage")
	}
}

func TestKafkaHandlerPreservesCallerContextFromHostilePropagator(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	wantValue := &struct{}{}
	wantCause := errors.New("caller canceled")
	ctx, cancel := context.WithCancelCause(
		context.WithValue(context.Background(), key, wantValue),
	)
	cancel(wantCause)
	remote := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	instrumentation := newKafkaTestInstrumentation(
		t,
		replacingContextPropagator{remote: remote},
	)
	handler, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(downstream context.Context, _ kafka.ConsumedMessage) error {
			if downstream.Value(key) != wantValue {
				return errors.New("caller context value was replaced")
			}
			if context.Cause(downstream) != wantCause {
				return errors.New("caller cancellation cause was replaced")
			}
			if got := trace.SpanContextFromContext(downstream); !sameSpanContext(got, remote) {
				return errors.New("remote span context was not extracted")
			}
			return nil
		}),
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler() error = %v", err)
	}
	if err := handler.Handle(ctx, kafka.ConsumedMessage{
		Headers: []kafka.Header{{Key: "traceparent", Value: []byte("bounded")}},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
}

func sameSpanContext(left, right trace.SpanContext) bool {
	return left.TraceID() == right.TraceID() &&
		left.SpanID() == right.SpanID() &&
		left.TraceFlags() == right.TraceFlags() &&
		left.TraceState().String() == right.TraceState().String() &&
		left.IsRemote() == right.IsRemote()
}

func TestKafkaHandlerIgnoresInvalidPropagationBounds(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	var received trace.SpanContext
	handler, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(ctx context.Context, _ kafka.ConsumedMessage) error {
			received = trace.SpanContextFromContext(ctx)
			return nil
		}),
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler() error = %v", err)
	}

	err = handler.Handle(context.Background(), kafka.ConsumedMessage{
		Topic: "events",
		Headers: []kafka.Header{{
			Key:   "traceparent",
			Value: []byte(strings.Repeat("x", 65)),
		}},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if received.IsValid() {
		t.Fatalf("received span context = %v, want ignored propagation", received)
	}
}

func TestKafkaHandlerIgnoresDuplicatePropagation(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	var received trace.SpanContext
	handler, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(ctx context.Context, _ kafka.ConsumedMessage) error {
			received = trace.SpanContextFromContext(ctx)
			return nil
		}),
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler() error = %v", err)
	}
	value := []byte("00-00000000000000000000000000000001-0000000000000002-01")
	err = handler.Handle(context.Background(), kafka.ConsumedMessage{
		Topic: "events",
		Headers: []kafka.Header{
			{Key: "traceparent", Value: value},
			{Key: "TraceParent", Value: value},
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if received.IsValid() {
		t.Fatalf("received span context = %v, want ambiguous context ignored", received)
	}
}

func TestKafkaPropagationRejectsInvalidConfigurationAndOutboundData(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	invalidLimits := tinyKafkaLimits()
	invalidLimits.MaxHeaders = 0
	if _, err := instrumentation.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{Limits: invalidLimits},
	); !errors.Is(err, ErrInvalidKafkaPropagation) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error { return nil }),
		KafkaPropagationConfig{Limits: invalidLimits},
	); !errors.Is(err, ErrInvalidKafkaPropagation) {
		t.Fatalf("invalid limits handler error = %v", err)
	}

	publisher, err := instrumentation.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if err != nil {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	err = publisher.Publish(context.Background(), kafka.Message{
		Topic: "events",
		Value: []byte(strings.Repeat("x", 65)),
	})
	if !errors.Is(err, ErrKafkaPropagationRejected) {
		t.Fatalf("oversized message error = %v", err)
	}
	var propagationErr *KafkaPropagationError
	if !errors.As(err, &propagationErr) {
		t.Fatalf("oversized message error type = %T", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("x", 8)) {
		t.Fatalf("error disclosed message data: %v", err)
	}
}

func TestKafkaPropagationValidatesDependenciesContextAndFields(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	if _, err := instrumentation.WrapKafkaPublisher(nil, KafkaPropagationConfig{}); !errors.Is(err, ErrKafkaPublisherRequired) {
		t.Fatalf("nil publisher error = %v", err)
	}
	if _, err := instrumentation.WrapKafkaHandler(nil, KafkaPropagationConfig{}); !errors.Is(err, ErrKafkaHandlerRequired) {
		t.Fatalf("nil handler error = %v", err)
	}
	publisher, err := instrumentation.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	if err := publisher.Publish(nilContext(), kafka.Message{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil publisher context error = %v", err)
	}
	handler, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error { return nil }),
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler() error = %v", err)
	}
	if err := handler.Handle(nilContext(), kafka.ConsumedMessage{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil handler context error = %v", err)
	}

	invalid := newKafkaTestInstrumentation(t, hostilePropagator{})
	if _, err := invalid.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{},
	); !errors.Is(err, ErrInvalidKafkaPropagation) {
		t.Fatalf("reserved field error = %v", err)
	}
}

func TestKafkaPropagationPreservesDownstreamBehavior(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	want := errors.New("downstream")
	publisher, err := instrumentation.WrapKafkaPublisher(
		&recordingKafkaPublisher{err: want},
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	if err := publisher.Publish(context.Background(), kafka.Message{}); !errors.Is(err, want) {
		t.Fatalf("Publish() error = %v", err)
	}
	handler, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
			return want
		}),
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler() error = %v", err)
	}
	if err := handler.Handle(context.Background(), kafka.ConsumedMessage{}); !errors.Is(err, want) {
		t.Fatalf("Handle() error = %v", err)
	}
}

func TestKafkaPropagationRejectsRuntimeAndUnsafeDeclaredFields(t *testing.T) {
	var missing *Instrumentation
	if _, err := missing.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{},
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("missing runtime error = %v", err)
	}

	tests := map[string]propagation.TextMapPropagator{
		"empty":      fieldPropagator{fields: []string{""}},
		"uppercase":  fieldPropagator{fields: []string{"TraceParent"}},
		"credential": fieldPropagator{fields: []string{"authorization"}},
		"baggage":    fieldPropagator{fields: []string{"baggage"}},
		"reserved":   fieldPropagator{fields: []string{"es.event_name"}},
		"invalid":    fieldPropagator{fields: []string{"trace parent"}},
		"duplicate":  fieldPropagator{fields: []string{"traceparent", "traceparent"}},
		"too many":   fieldPropagator{fields: []string{"one", "two", "three", "four", "five"}},
		"key length": fieldPropagator{fields: []string{strings.Repeat("x", 129)}},
	}
	for name, propagator := range tests {
		t.Run(name, func(t *testing.T) {
			instrumentation := newKafkaTestInstrumentation(t, propagator)
			if _, err := instrumentation.WrapKafkaPublisher(
				&recordingKafkaPublisher{},
				KafkaPropagationConfig{Limits: tinyKafkaLimits()},
			); !errors.Is(err, ErrInvalidKafkaPropagation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestKafkaPropagationHonorsExactDeclaredFieldLimits(t *testing.T) {
	tests := []struct {
		name      string
		fields    []string
		configure func(*kafka.MessageLimits)
		wantError bool
	}{
		{
			name:   "header count exact",
			fields: []string{"traceparent", "tracestate"},
			configure: func(limits *kafka.MessageLimits) {
				limits.MaxHeaders = 2
			},
		},
		{
			name:   "header count exceeded",
			fields: []string{"traceparent", "tracestate"},
			configure: func(limits *kafka.MessageLimits) {
				limits.MaxHeaders = 1
			},
			wantError: true,
		},
		{
			name:   "header key exact",
			fields: []string{"traceparent"},
			configure: func(limits *kafka.MessageLimits) {
				limits.MaxHeaderKeyBytes = len("traceparent")
			},
		},
		{
			name:   "header key exceeded",
			fields: []string{"traceparent"},
			configure: func(limits *kafka.MessageLimits) {
				limits.MaxHeaderKeyBytes = len("traceparent") - 1
			},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := tinyKafkaLimits()
			test.configure(&limits)
			instrumentation := newKafkaTestInstrumentation(t, fieldPropagator{
				fields: test.fields,
			})
			_, err := instrumentation.WrapKafkaPublisher(
				&recordingKafkaPublisher{},
				KafkaPropagationConfig{Limits: limits},
			)
			if test.wantError && !errors.Is(err, ErrInvalidKafkaPropagation) {
				t.Fatalf("WrapKafkaPublisher() error = %v, want invalid propagation", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("WrapKafkaPublisher() error = %v", err)
			}
		})
	}
}

func TestKafkaPublisherPreservesUnicodeConfusableApplicationHeaders(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, fieldPropagator{
		fields: []string{"tracestate"},
		setKey: "tracestate",
		value:  "vendor=value",
	})
	downstream := &recordingKafkaPublisher{}
	publisher, err := instrumentation.WrapKafkaPublisher(
		downstream,
		KafkaPropagationConfig{},
	)
	if err != nil {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	const confusable = "traceſtate"
	if err := publisher.Publish(context.Background(), kafka.Message{
		Headers: []kafka.Header{{Key: confusable, Value: []byte("application")}},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	got := ""
	for _, header := range downstream.message.Headers {
		if header.Key == confusable {
			got = string(header.Value)
		}
	}
	if got != "application" {
		t.Fatalf("confusable application header = %q, want preserved", got)
	}
}

func TestKafkaPublisherIsolatesPropagatorOutputBeyondBounds(t *testing.T) {
	tests := map[string]struct {
		propagator propagation.TextMapPropagator
		message    kafka.Message
	}{
		"undeclared field": {
			propagator: fieldPropagator{
				fields: []string{"traceparent"},
				setKey: "unexpected",
				value:  "value",
			},
		},
		"oversized value": {
			propagator: fieldPropagator{
				fields: []string{"traceparent"},
				setKey: "traceparent",
				value:  strings.Repeat("x", 65),
			},
		},
		"header count": {
			propagator: fieldPropagator{
				fields: []string{"traceparent"},
				setKey: "traceparent",
				value:  "value",
			},
			message: kafka.Message{Headers: []kafka.Header{
				{Key: "one"}, {Key: "two"}, {Key: "three"}, {Key: "four"},
			}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			instrumentation := newKafkaTestInstrumentation(t, test.propagator)
			next := &recordingKafkaPublisher{}
			publisher, err := instrumentation.WrapKafkaPublisher(
				next,
				KafkaPropagationConfig{Limits: tinyKafkaLimits()},
			)
			if err != nil {
				t.Fatalf("WrapKafkaPublisher() error = %v", err)
			}
			if err := publisher.Publish(context.Background(), test.message); err != nil {
				t.Fatalf("Publish() error = %v", err)
			}
			if len(next.message.Headers) != len(test.message.Headers) {
				t.Fatalf("published headers = %#v", next.message.Headers)
			}
			if kafkaHeaderValue(next.message.Headers, "traceparent") != "" ||
				kafkaHeaderValue(next.message.Headers, "unexpected") != "" {
				t.Fatalf("published propagation = %#v", next.message.Headers)
			}
		})
	}
}

func TestKafkaPropagationIsolatesPropagatorPanics(t *testing.T) {
	fieldsInstrumentation := newKafkaTestInstrumentation(
		t,
		panickingPropagator{panicFields: true},
	)
	_, err := fieldsInstrumentation.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if !errors.Is(err, ErrInvalidKafkaPropagation) {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}

	message := kafka.Message{Topic: "events", Value: []byte("payload")}
	panicInstrumentation := newKafkaTestInstrumentation(
		t,
		panickingPropagator{
			fields:       []string{"traceparent"},
			panicInject:  true,
			panicExtract: true,
		},
	)
	injectNext := &recordingKafkaPublisher{}
	publisher, err := panicInstrumentation.WrapKafkaPublisher(
		injectNext,
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if err != nil {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	if err := publisher.Publish(context.Background(), message); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if kafkaHeaderValue(injectNext.message.Headers, "traceparent") != "" {
		t.Fatalf("published partial propagation = %#v", injectNext.message.Headers)
	}

	handled := false
	handler, err := panicInstrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
			handled = true
			return nil
		}),
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler() error = %v", err)
	}
	if err := handler.Handle(context.Background(), kafka.ConsumedMessage{
		Headers: []kafka.Header{{Key: "traceparent", Value: []byte("value")}},
	}); err != nil || !handled {
		t.Fatalf("Handle() = handled %t, error %v", handled, err)
	}

	nilExtractInstrumentation := newKafkaTestInstrumentation(
		t,
		panickingPropagator{
			fields:     []string{"traceparent"},
			nilExtract: true,
		},
	)
	type propagationContextKey struct{}
	contextKey := propagationContextKey{}
	originalContext := context.WithValue(context.Background(), contextKey, "preserved")
	nilExtractHandler, err := nilExtractInstrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(ctx context.Context, _ kafka.ConsumedMessage) error {
			if ctx == nil || ctx.Value(contextKey) != "preserved" {
				return errors.New("original context was not preserved")
			}
			return nil
		}),
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if err != nil {
		t.Fatalf("WrapKafkaHandler(nil extract) error = %v", err)
	}
	if err := nilExtractHandler.Handle(originalContext, kafka.ConsumedMessage{
		Headers: []kafka.Header{{Key: "traceparent", Value: []byte("value")}},
	}); err != nil {
		t.Fatalf("Handle(nil extract) error = %v", err)
	}
}

func TestKafkaPropagationCarriers(t *testing.T) {
	fields := map[string]struct{}{"traceparent": {}, "tracestate": {}}
	headers := []kafka.Header{
		{Key: "traceparent", Value: []byte("old")},
		{Key: "TraceParent", Value: []byte("new")},
		{Key: "other", Value: []byte("preserved")},
	}
	extract := kafkaExtractCarrier{headers: headers, fields: fields}
	if got := extract.Get("TRACEPARENT"); got != "new" {
		t.Fatalf("Get() = %q", got)
	}
	if got := extract.Get("baggage"); got != "" {
		t.Fatalf("Get(disallowed) = %q", got)
	}
	extract.Set("traceparent", "ignored")
	keys := extract.Keys()
	if len(keys) != 1 || keys[0] != "traceparent" {
		t.Fatalf("Keys() = %#v", keys)
	}

	inject := &kafkaInjectCarrier{
		headers: headers,
		fields:  fields,
		limits:  tinyKafkaLimits(),
	}
	if got := inject.Get("traceparent"); got != "new" {
		t.Fatalf("inject Get() = %q", got)
	}
	if got := inject.Keys(); len(got) != 1 || got[0] != "traceparent" {
		t.Fatalf("inject Keys() = %#v", got)
	}
	inject.Set("traceparent", "replaced")
	if got := kafkaHeaderCount(inject.headers, "traceparent"); got != 1 {
		t.Fatalf("replaced header count = %d", got)
	}
	inject.Set("unknown", "rejected")
	before := len(inject.headers)
	inject.Set("traceparent", "ignored after rejection")
	if len(inject.headers) != before {
		t.Fatal("rejected carrier accepted later mutation")
	}
}

func TestKafkaMessageAndHeaderBounds(t *testing.T) {
	limits := tinyKafkaLimits()
	valid := kafka.Message{
		Topic: "events",
		Key:   []byte("key"),
		Value: []byte("value"),
		Headers: []kafka.Header{{
			Key:   "header",
			Value: []byte("value"),
		}},
	}
	if !validKafkaMessage(valid, limits) {
		t.Fatal("validKafkaMessage() rejected valid message")
	}

	messages := map[string]kafka.Message{
		"topic": func() kafka.Message {
			message := valid
			message.Topic = strings.Repeat("x", 250)
			return message
		}(),
		"key": func() kafka.Message {
			message := valid
			message.Key = []byte(strings.Repeat("x", 65))
			return message
		}(),
		"value": func() kafka.Message {
			message := valid
			message.Value = []byte(strings.Repeat("x", 65))
			return message
		}(),
	}
	for name, message := range messages {
		t.Run(name, func(t *testing.T) {
			if validKafkaMessage(message, limits) {
				t.Fatal("validKafkaMessage() accepted oversized field")
			}
		})
	}

	headerTests := map[string][]kafka.Header{
		"count": {
			{Key: "1"}, {Key: "2"}, {Key: "3"}, {Key: "4"}, {Key: "5"},
		},
		"empty key":    {{Value: []byte("value")}},
		"key length":   {{Key: strings.Repeat("x", 129)}},
		"value length": {{Key: "key", Value: []byte(strings.Repeat("x", 65))}},
		"total key": {
			{Key: strings.Repeat("x", 64), Value: []byte(strings.Repeat("x", 64))},
			{Key: "x"},
		},
		"total value": {
			{Key: strings.Repeat("x", 64), Value: []byte(strings.Repeat("x", 63))},
			{Key: "x", Value: []byte("x")},
		},
	}
	for name, headers := range headerTests {
		t.Run(name, func(t *testing.T) {
			if validKafkaHeaders(headers, limits) {
				t.Fatal("validKafkaHeaders() accepted invalid headers")
			}
		})
	}

	fields := map[string]struct{}{"traceparent": {}}
	if validKafkaPropagationHeaders(
		[]kafka.Header{
			{Key: string([]byte{0x80}), Value: []byte("unrelated")},
			{Key: "traceparent", Value: []byte("one")},
			{Key: "TraceParent", Value: []byte("two")},
		},
		fields,
		limits,
	) {
		t.Fatal("validKafkaPropagationHeaders() accepted duplicate fields")
	}
	if !validKafkaPropagationHeaders(
		[]kafka.Header{{Key: "other", Value: []byte("value")}},
		fields,
		limits,
	) {
		t.Fatal("validKafkaPropagationHeaders() rejected unrelated header")
	}
	if validKafkaPropagationHeaders(
		headerTests["count"],
		fields,
		limits,
	) {
		t.Fatal("validKafkaPropagationHeaders() accepted invalid bounds")
	}
}

func TestCanonicalKafkaHeaderKeyAcceptsOnlyASCII(t *testing.T) {
	for value := range 256 {
		input := string([]byte{byte(value)})
		got, canonical := canonicalKafkaHeaderKey(input)
		if value >= 0x80 {
			if canonical || got != "" {
				t.Fatalf("canonicalKafkaHeaderKey(%#x) = %q, %t", value, got, canonical)
			}
			continue
		}
		want := input
		if value >= 'A' && value <= 'Z' {
			want = string([]byte{byte(value + ('a' - 'A'))})
		}
		if !canonical || got != want {
			t.Fatalf(
				"canonicalKafkaHeaderKey(%#x) = %q, %t; want %q, true",
				value,
				got,
				canonical,
				want,
			)
		}
	}
}

type recordingKafkaPublisher struct {
	message kafka.Message
	err     error
}

func (publisher *recordingKafkaPublisher) Publish(
	_ context.Context,
	message kafka.Message,
) error {
	publisher.message = message
	return publisher.err
}

type hostilePropagator struct{}

func (hostilePropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (hostilePropagator) Extract(
	ctx context.Context,
	_ propagation.TextMapCarrier,
) context.Context {
	return ctx
}

func (hostilePropagator) Fields() []string {
	return []string{"es.event_name"}
}

type replacingContextPropagator struct {
	remote trace.SpanContext
}

func (replacingContextPropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (propagator replacingContextPropagator) Extract(
	_ context.Context,
	_ propagation.TextMapCarrier,
) context.Context {
	return trace.ContextWithRemoteSpanContext(context.Background(), propagator.remote)
}

func (replacingContextPropagator) Fields() []string {
	return []string{"traceparent"}
}

type panickingPropagator struct {
	fields       []string
	panicFields  bool
	panicInject  bool
	panicExtract bool
	nilExtract   bool
}

func (propagator panickingPropagator) Inject(
	_ context.Context,
	carrier propagation.TextMapCarrier,
) {
	if propagator.panicInject {
		carrier.Set("traceparent", "partial")
		panic("propagator inject")
	}
}

func (propagator panickingPropagator) Extract(
	ctx context.Context,
	_ propagation.TextMapCarrier,
) context.Context {
	if propagator.panicExtract {
		panic("propagator extract")
	}
	if propagator.nilExtract {
		return nil
	}
	return ctx
}

func (propagator panickingPropagator) Fields() []string {
	if propagator.panicFields {
		panic("propagator fields")
	}
	return propagator.fields
}

type fieldPropagator struct {
	fields []string
	setKey string
	value  string
}

func (propagator fieldPropagator) Inject(
	_ context.Context,
	carrier propagation.TextMapCarrier,
) {
	if propagator.setKey != "" {
		carrier.Set(propagator.setKey, propagator.value)
	}
}

func (fieldPropagator) Extract(
	ctx context.Context,
	_ propagation.TextMapCarrier,
) context.Context {
	return ctx
}

func (propagator fieldPropagator) Fields() []string {
	return propagator.fields
}

func newKafkaTestInstrumentation(
	t *testing.T,
	propagator propagation.TextMapPropagator,
) *Instrumentation {
	t.Helper()
	runtime := testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagator,
	}
	instrumentation, err := New(runtime)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return instrumentation
}

func tinyKafkaLimits() kafka.MessageLimits {
	return kafka.MessageLimits{
		MaxTopicBytes:       249,
		MaxKeyBytes:         64,
		MaxValueBytes:       64,
		MaxHeaders:          4,
		MaxHeaderKeyBytes:   128,
		MaxHeaderValueBytes: 64,
		MaxHeaderBytes:      128,
	}
}

func kafkaHeaderValue(headers []kafka.Header, key string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Key, key) {
			return string(header.Value)
		}
	}
	return ""
}

func kafkaHeaderCount(headers []kafka.Header, key string) int {
	count := 0
	for _, header := range headers {
		if strings.EqualFold(header.Key, key) {
			count++
		}
	}
	return count
}
