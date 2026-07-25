// Package telemetry links correlation metadata to OpenTelemetry without
// treating correlation IDs as trace or span IDs.
package telemetry

import (
	"errors"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ErrInvalidSpanContext reports a missing telemetry-owned trace identity.
var ErrInvalidSpanContext = errors.New("correlation telemetry: invalid span context")

// Attributes returns bounded span/link attributes under an explicit
// disclosure policy.
func Attributes(values correlation.Values, policy correlation.DisclosurePolicy) ([]attribute.KeyValue, error) {
	inputs := []struct {
		key   string
		value string
	}{
		{"correlation.id", values.CorrelationID.String()},
		{"request.id", values.RequestID.String()},
		{"causation.id", values.CausationID.String()},
	}
	attributes := make([]attribute.KeyValue, 0, len(inputs))
	for _, input := range inputs {
		if input.value == "" {
			continue
		}
		value, err := correlation.Disclose(input.key, input.value, policy)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, attribute.String(input.key, value))
	}
	return attributes, nil
}

// Link attaches correlation attributes to a telemetry-owned SpanContext. The
// supplied trace and span IDs are neither derived from nor replaced by IDs.
func Link(spanContext trace.SpanContext, values correlation.Values, policy correlation.DisclosurePolicy) (trace.Link, error) {
	if !spanContext.IsValid() {
		return trace.Link{}, ErrInvalidSpanContext
	}
	attributes, err := Attributes(values, policy)
	if err != nil {
		return trace.Link{}, err
	}
	return trace.Link{SpanContext: spanContext, Attributes: attributes}, nil
}

// MetricAttributes exposes fixed-cardinality presence signals only. Raw,
// hashed, and deterministic business-derived identifiers are never included.
func MetricAttributes(values correlation.Values) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Bool("correlation.present", values.CorrelationID != ""),
		attribute.Bool("request.present", values.RequestID != ""),
		attribute.Bool("causation.present", values.CausationID != ""),
	}
}
