package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestAttributePolicyMatchesKafkaIdentityBounds(t *testing.T) {
	t.Parallel()

	if err := (AttributePolicy{
		AllowedClientIDs:      []string{strings.Repeat("c", 255)},
		AllowedTopics:         []string{strings.Repeat("t", 249)},
		AllowedConsumerGroups: []string{strings.Repeat("g", 255)},
	}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (AttributePolicy{
		AllowedTopics: []string{strings.Repeat("t", 250)},
	}).Validate(); !errors.Is(err, ErrInvalidAttributePolicy) {
		t.Fatalf("oversized topic error = %v", err)
	}
}

func TestAttributePolicyRejectsNonCanonicalKafkaIdentities(t *testing.T) {
	t.Parallel()

	tests := []AttributePolicy{
		{AllowedClientIDs: []string{" client"}},
		{AllowedClientIDs: []string{"client "}},
		{AllowedConsumerGroups: []string{" group"}},
		{AllowedTopics: []string{"."}},
		{AllowedTopics: []string{".."}},
		{AllowedTopics: []string{"not a topic"}},
		{AllowedTopics: []string{"orders/created"}},
	}
	for _, policy := range tests {
		if err := policy.Validate(); !errors.Is(
			err,
			ErrInvalidAttributePolicy,
		) {
			t.Fatalf("Validate(%#v) error = %v", policy, err)
		}
	}
	if err := (Config{
		Runtime:    completeTestRuntime(),
		Attributes: tests[0],
	}).Validate(); !errors.Is(err, ErrInvalidAttributePolicy) {
		t.Fatalf("Config.Validate() error = %v", err)
	}
}

func TestAttributePolicyRejectsInvalidAndUnboundedAllowlists(t *testing.T) {
	t.Parallel()

	tooMany := make([]string, 129)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("client-%d", index)
	}
	invalidUTF8 := string([]byte{0xff})
	tests := []AttributePolicy{
		{AllowedClientIDs: []string{""}},
		{AllowedClientIDs: []string{"\t"}},
		{AllowedClientIDs: []string{"client\x00id"}},
		{AllowedClientIDs: []string{invalidUTF8}},
		{AllowedClientIDs: []string{"client", "client"}},
		{AllowedClientIDs: tooMany},
		{AllowedTopics: []string{"orders", "orders"}},
		{AllowedConsumerGroups: []string{"workers", "workers"}},
	}
	for _, policy := range tests {
		if err := policy.Validate(); !errors.Is(
			err,
			ErrInvalidAttributePolicy,
		) {
			t.Fatalf("Validate(%#v) error = %v", policy, err)
		}
	}
}

func TestNewCopiesAttributePolicyAndSuppressesUnlistedIdentities(t *testing.T) {
	t.Parallel()

	clientIDs := []string{"client"}
	topics := []string{"orders"}
	groups := []string{"workers"}
	spans := tracetest.NewSpanRecorder()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: metricnoop.NewMeterProvider(),
		},
		Attributes: AttributePolicy{
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

	if err := instrumentation.Observer()(context.Background(), kafka.Observation{
		Kind:        kafka.ObservationConsumeRecord,
		StartedAt:   time.Unix(1, 0),
		Duration:    time.Millisecond,
		ClientID:    "client",
		Topic:       "orders",
		GroupID:     "workers",
		RecordCount: 1,
		Succeeded:   true,
	}); err != nil {
		t.Fatalf("observe allowed values: %v", err)
	}
	if err := instrumentation.Observer()(context.Background(), kafka.Observation{
		Kind:        kafka.ObservationConsumeRecord,
		StartedAt:   time.Unix(2, 0),
		Duration:    time.Millisecond,
		ClientID:    "secret-client",
		Topic:       "secret-topic",
		GroupID:     "secret-group",
		RecordCount: 1,
		Succeeded:   true,
	}); err != nil {
		t.Fatalf("observe disallowed values: %v", err)
	}

	ended := spans.Ended()
	if len(ended) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(ended))
	}
	assertSpanAttributes(t, ended[0].Attributes(), map[string]any{
		"messaging.client.id":           "client",
		"messaging.destination.name":    "orders",
		"messaging.consumer.group.name": "workers",
	})
	if output := fmt.Sprint(ended[1]); strings.Contains(output, "secret") {
		t.Fatalf("disallowed identities escaped policy: %s", output)
	}
}

func TestConfigValidationRequiresCompleteProviders(t *testing.T) {
	t.Parallel()

	var typedNilTracer *sdktrace.TracerProvider
	var typedNilMeter *typedNilMeterProvider
	tests := []Config{
		{},
		{Runtime: testRuntime{meterProvider: metricnoop.NewMeterProvider()}},
		{Runtime: testRuntime{tracerProvider: tracenoop.NewTracerProvider()}},
		{Runtime: testRuntime{
			tracerProvider: typedNilTracer,
			meterProvider:  metricnoop.NewMeterProvider(),
		}},
		{Runtime: testRuntime{
			tracerProvider: tracenoop.NewTracerProvider(),
			meterProvider:  typedNilMeter,
		}},
	}
	for _, config := range tests {
		if err := config.Validate(); !errors.Is(err, ErrRuntimeRequired) {
			t.Fatalf("Validate(%#v) error = %v", config, err)
		}
		instrumentation, err := New(config)
		if instrumentation != nil || !errors.Is(err, ErrRuntimeRequired) {
			t.Fatalf("New(%#v) = %#v, %v", config, instrumentation, err)
		}
	}
	if err := (Config{Runtime: completeTestRuntime()}).Validate(); err != nil {
		t.Fatalf("complete Validate() error = %v", err)
	}
}

type typedNilMeterProvider struct {
	metric.MeterProvider
}

func completeTestRuntime() testRuntime {
	return testRuntime{
		tracerProvider: tracenoop.NewTracerProvider(),
		meterProvider:  metricnoop.NewMeterProvider(),
	}
}
