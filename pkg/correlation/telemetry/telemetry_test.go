package telemetry_test

import (
	"errors"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	correlationtelemetry "github.com/faustbrian/golib/pkg/correlation/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestLinkKeepsTraceSemanticsAndRedactsCorrelation(t *testing.T) {
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, Remote: true,
	})
	values := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("request", correlation.Policy{}),
	}
	link, err := correlationtelemetry.Link(spanContext, values, correlation.DisclosurePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if link.SpanContext.TraceID() != spanContext.TraceID() ||
		link.SpanContext.SpanID() != spanContext.SpanID() ||
		link.SpanContext.IsRemote() != spanContext.IsRemote() ||
		stringValue(link.Attributes, "correlation.id") != "[redacted]" {
		t.Fatalf("link = %#v", link)
	}
}

func TestTelemetryRejectsInvalidTraceAndDisclosure(t *testing.T) {
	values := correlation.Values{CorrelationID: "flow"}
	if _, err := correlationtelemetry.Link(trace.SpanContext{}, values, correlation.DisclosurePolicy{}); !errors.Is(err, correlationtelemetry.ErrInvalidSpanContext) {
		t.Fatalf("invalid span context error = %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	policy := correlation.DisclosurePolicy{Mode: correlation.HashDisclosure}
	if _, err := correlationtelemetry.Attributes(values, policy); !errors.Is(err, correlation.ErrInvalidDisclosure) {
		t.Fatalf("Attributes() error = %v", err)
	}
	if _, err := correlationtelemetry.Link(spanContext, values, policy); !errors.Is(err, correlation.ErrInvalidDisclosure) {
		t.Fatalf("Link() error = %v", err)
	}
}

func TestMetricAttributesNeverContainRawIdentifiers(t *testing.T) {
	values := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("d1_businessderived", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("request", correlation.Policy{}),
	}
	attributes := correlationtelemetry.MetricAttributes(values)
	for _, attr := range attributes {
		if attr.Value.AsString() == values.CorrelationID.String() || attr.Value.AsString() == values.RequestID.String() {
			t.Fatalf("raw identifier leaked to metric attribute: %v", attr)
		}
	}
	if !boolValue(attributes, "correlation.present") {
		t.Fatalf("metric attributes = %v", attributes)
	}
}

func stringValue(attributes []attribute.KeyValue, key string) string {
	for _, attr := range attributes {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

func boolValue(attributes []attribute.KeyValue, key string) bool {
	for _, attr := range attributes {
		if string(attr.Key) == key {
			return attr.Value.AsBool()
		}
	}
	return false
}
