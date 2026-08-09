package golib_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/correlation"
	telemetrypropagation "github.com/faustbrian/golib/pkg/telemetry/propagation"
	"github.com/faustbrian/golib/pkg/tenancy"
	"go.opentelemetry.io/otel/trace"
)

func baseEvent(t *testing.T) cloudevents.Event {
	t.Helper()
	data, err := cloudevents.NewJSONData([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestCorrelationAndTenantMetadataRequireExplicitTrustAndRejectCollisions(t *testing.T) {
	t.Parallel()

	event := baseEvent(t)
	values := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("correlation-1", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("request-1", correlation.Policy{}),
		CausationID:   correlation.MustCausationID("cause-1", correlation.Policy{}),
	}
	withCorrelation, err := golib.AddCorrelation(event, values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := golib.ExtractCorrelation(withCorrelation, false, correlation.Policy{}); !errors.Is(err, golib.ErrUntrustedMetadata) {
		t.Fatalf("untrusted correlation error = %v", err)
	}
	extracted, err := golib.ExtractCorrelation(withCorrelation, true, correlation.Policy{})
	if err != nil || extracted != values {
		t.Fatalf("correlation = %#v, %v", extracted, err)
	}
	if _, err := golib.AddCorrelation(withCorrelation, correlation.Values{
		CorrelationID: correlation.MustCorrelationID("different", correlation.Policy{}),
	}); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("correlation collision error = %v", err)
	}

	tenant := tenancy.MustTenantID("tenant-a")
	withTenant, err := golib.AddTenant(withCorrelation, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := golib.ExtractTenant(withTenant, false); !errors.Is(err, golib.ErrUntrustedMetadata) {
		t.Fatalf("untrusted tenant error = %v", err)
	}
	extractedTenant, err := golib.ExtractTenant(withTenant, true)
	if err != nil || !extractedTenant.Equal(tenant) {
		t.Fatalf("tenant = %v, %v", extractedTenant, err)
	}
	if _, err := golib.AddTenant(withTenant, tenancy.MustTenantID("tenant-b")); !errors.Is(err, golib.ErrMetadataCollision) {
		t.Fatalf("tenant collision error = %v", err)
	}
	if _, ok := withTenant.Extension("correlationid"); !ok {
		t.Fatal("adding tenant dropped an existing extension")
	}
}

func TestTelemetryAdapterUsesExplicitGolibPropagationPolicy(t *testing.T) {
	t.Parallel()

	policy, err := telemetrypropagation.New(telemetrypropagation.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanContext)
	event, report, err := golib.InjectTraceContext(ctx, baseEvent(t), policy)
	if err != nil || len(report.Losses) != 0 {
		t.Fatalf("inject trace context = %#v, %v", report, err)
	}
	if _, ok := event.Extension("traceparent"); !ok {
		t.Fatal("traceparent extension is absent")
	}
	extracted := golib.ExtractTraceContext(context.Background(), event, policy, false)
	if got := trace.SpanContextFromContext(extracted); !got.IsValid() || got.TraceID() != spanContext.TraceID() {
		t.Fatalf("extracted span context = %v", got)
	}
}
