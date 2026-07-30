package gotelemetry

import (
	"context"
	"math"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestKafkaPropagationAcceptsExactConfigurationAndMessageLimits(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(t, fieldPropagator{
		fields: []string{"one", "two", "three", "four"},
	})
	if _, _, err := instrumentation.kafkaPropagation(KafkaPropagationConfig{
		Limits: tinyKafkaLimits(),
	}); err != nil {
		t.Fatalf("kafkaPropagation() exact header count error = %v", err)
	}

	limits := tinyKafkaLimits()
	message := kafka.Message{
		Topic: strings.Repeat("t", limits.MaxTopicBytes),
		Key:   []byte(strings.Repeat("k", limits.MaxKeyBytes)),
		Value: []byte(strings.Repeat("v", limits.MaxValueBytes)),
		Headers: []kafka.Header{
			{Key: "a"},
			{Key: "b"},
			{Key: "c"},
			{Key: "d"},
		},
	}
	if !validKafkaMessage(message, limits) {
		t.Fatal("validKafkaMessage() rejected exact field and header limits")
	}

	carrier := &kafkaInjectCarrier{
		fields: map[string]struct{}{"traceparent": {}},
		limits: limits,
	}
	carrier.Set(
		"traceparent",
		strings.Repeat("v", limits.MaxHeaderValueBytes),
	)
	if carrier.rejected ||
		len(carrier.headers) != 1 ||
		len(carrier.headers[0].Value) != limits.MaxHeaderValueBytes {
		t.Fatalf("Set() rejected exact value limit: %#v", carrier)
	}
}

func TestKafkaPropagationFieldAlphabetAndLengthBoundaries(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"a", "z", "0", "9", ".", "_", "-"} {
		if !validPropagationField(field, 1) {
			t.Fatalf("validPropagationField(%q) = false", field)
		}
	}
	exact := strings.Repeat("x", 128)
	if !validPropagationField(exact, len(exact)) {
		t.Fatal("validPropagationField() rejected exact length limit")
	}
	for _, field := range []string{"`", "{", "/", ":"} {
		if validPropagationField(field, 1) {
			t.Fatalf("validPropagationField(%q) = true", field)
		}
	}
}

func TestKafkaHeaderAggregateBoundaries(t *testing.T) {
	t.Parallel()

	limits := tinyKafkaLimits()
	tests := [][]kafka.Header{
		{{Key: strings.Repeat("k", limits.MaxHeaderKeyBytes)}},
		{{Key: "k", Value: []byte(strings.Repeat("v", limits.MaxHeaderValueBytes))}},
		{{
			Key:   strings.Repeat("k", limits.MaxHeaderBytes/2),
			Value: []byte(strings.Repeat("v", limits.MaxHeaderBytes/2)),
		}},
		{
			{Key: strings.Repeat("k", limits.MaxHeaderBytes-1)},
			{Key: "k"},
		},
		{
			{Key: strings.Repeat("k", limits.MaxHeaderBytes-2)},
			{Key: "k", Value: []byte("v")},
		},
	}
	for index, headers := range tests {
		if !validKafkaHeaders(headers, limits) {
			t.Fatalf("validKafkaHeaders() exact boundary %d = false", index)
		}
	}
}

func TestKafkaPropagationKeyScanningPreservesOrderAndDetectsLateDuplicates(
	t *testing.T,
) {
	t.Parallel()

	fields := map[string]struct{}{
		"traceparent": {},
		"tracestate":  {},
	}
	headers := []kafka.Header{
		{Key: "unrelated"},
		{Key: "TraceParent"},
		{Key: "traceparent"},
		{Key: "tracestate"},
	}
	keys := propagationKeys(headers, fields)
	if len(keys) != 2 || keys[0] != "traceparent" || keys[1] != "tracestate" {
		t.Fatalf("propagationKeys() = %#v", keys)
	}
	if validKafkaPropagationHeaders(headers, fields, tinyKafkaLimits()) {
		t.Fatal("validKafkaPropagationHeaders() accepted a duplicate after an unrelated header")
	}
}

func TestProjectionLagAcceptsExactBounds(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	for _, test := range []struct {
		current       eventsourcing.GlobalPosition
		highWatermark eventsourcing.GlobalPosition
	}{
		{current: 1, highWatermark: 1},
		{current: 0, highWatermark: eventsourcing.GlobalPosition(math.MaxInt64)},
	} {
		if err := instrumentation.RecordProjectionLag(
			context.Background(),
			"summary",
			test.current,
			test.highWatermark,
		); err != nil {
			t.Fatalf("RecordProjectionLag(%d, %d) error = %v",
				test.current, test.highWatermark, err)
		}
	}
}

func TestIncompleteInstrumentationIsRejectedWithoutDereference(t *testing.T) {
	t.Parallel()

	instrumentation := &Instrumentation{}
	if _, err := instrumentation.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{},
	); err != ErrRuntimeRequired {
		t.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	if _, err := instrumentation.WrapProjectionRunner(
		"summary",
		&telemetryProjectionRunner{},
	); err != ErrRuntimeRequired {
		t.Fatalf("WrapProjectionRunner() error = %v", err)
	}
	if err := instrumentation.RecordProjectionLag(
		context.Background(),
		"summary",
		0,
		0,
	); err != ErrRuntimeRequired {
		t.Fatalf("RecordProjectionLag() error = %v", err)
	}
}

func assertAllSpansError(t *testing.T, spans []sdktrace.ReadOnlySpan) {
	t.Helper()
	if len(spans) == 0 {
		t.Fatal("no completed spans")
	}
	for index, span := range spans {
		if span.Status().Code != codes.Error {
			t.Fatalf("span %d status = %s, want Error", index, span.Status().Code)
		}
	}
}
