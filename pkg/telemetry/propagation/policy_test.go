package propagation

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestPolicySeparatesTrustedAndUntrustedInboundBaggage(t *testing.T) {
	t.Parallel()

	policy, err := New(Config{
		BaggageEnabled:     true,
		TrustedBaggageKeys: []string{"tenant.tier"},
		MaxHeaderBytes:     1_024,
		MaxBaggageItems:    2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	carrier := otelpropagation.MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"baggage":     "tenant.tier=gold,user.id=secret",
	}

	untrusted := policy.Extract(context.Background(), carrier)
	if !trace.SpanContextFromContext(untrusted).IsValid() {
		t.Fatal("untrusted trace context was not extracted")
	}
	if baggage.FromContext(untrusted).Len() != 0 {
		t.Fatal("untrusted baggage was accepted")
	}

	trusted := policy.ExtractTrusted(context.Background(), carrier)
	got := baggage.FromContext(trusted)
	if got.Len() != 1 || got.Member("tenant.tier").Value() != "gold" {
		t.Fatalf("trusted baggage = %q, want only tenant.tier=gold", got.String())
	}
}

func TestPolicyDropsOversizedPropagationHeaders(t *testing.T) {
	t.Parallel()

	policy, err := New(Config{
		BaggageEnabled:     true,
		TrustedBaggageKeys: []string{"tenant.tier"},
		MaxHeaderBytes:     32,
		MaxBaggageItems:    1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	carrier := otelpropagation.MapCarrier{"baggage": "tenant.tier=" + strings.Repeat("x", 64)}

	ctx := policy.ExtractTrusted(context.Background(), carrier)
	if baggage.FromContext(ctx).Len() != 0 {
		t.Fatal("oversized baggage was accepted")
	}
}

func TestPolicyReplacesOutboundHeadersAndFiltersBaggage(t *testing.T) {
	t.Parallel()

	policy, err := New(Config{
		BaggageEnabled:     true,
		TrustedBaggageKeys: []string{"tenant.tier"},
		MaxHeaderBytes:     1_024,
		MaxBaggageItems:    2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	allowed, _ := baggage.NewMember("tenant.tier", "gold")
	secret, _ := baggage.NewMember("user.id", "secret")
	bag, _ := baggage.New(allowed, secret)
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := baggage.ContextWithBaggage(trace.ContextWithSpanContext(context.Background(), spanContext), bag)
	carrier := otelpropagation.MapCarrier{
		"traceparent": "stale",
		"baggage":     "stale=secret",
	}

	policy.Inject(ctx, carrier)
	if carrier.Get("traceparent") == "stale" || carrier.Get("traceparent") == "" {
		t.Fatalf("traceparent = %q, want replacement", carrier.Get("traceparent"))
	}
	if carrier.Get("baggage") != "tenant.tier=gold" {
		t.Fatalf("baggage = %q, want filtered replacement", carrier.Get("baggage"))
	}
}

func TestPolicyValidatesBoundsAndKeys(t *testing.T) {
	t.Parallel()

	for _, config := range []Config{
		{},
		{MaxHeaderBytes: 1, MaxBaggageItems: 0},
		{MaxHeaderBytes: 1, MaxBaggageItems: 1, TrustedBaggageKeys: []string{"invalid key"}},
		{MaxHeaderBytes: 1, MaxBaggageItems: 1, TrustedBaggageKeys: []string{"duplicate", "duplicate"}},
	} {
		if _, err := New(config); err == nil {
			t.Fatalf("New(%+v) error = nil, want validation error", config)
		}
	}
}

func TestDefaultPolicyAndFields(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	policy, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := policy.Fields(); len(got) != 2 || got[0] != "traceparent" || got[1] != "tracestate" {
		t.Fatalf("Fields() = %v, want trace context fields", got)
	}
	config.BaggageEnabled = true
	policy, err = New(config)
	if err != nil {
		t.Fatalf("New() with baggage error = %v", err)
	}
	if got := policy.Fields(); len(got) != 3 || got[2] != "baggage" {
		t.Fatalf("Fields() = %v, want baggage field", got)
	}
}

func TestDisabledPolicyClearsExistingBaggage(t *testing.T) {
	t.Parallel()

	policy, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	member, _ := baggage.NewMember("tenant.tier", "gold")
	bag, _ := baggage.New(member)
	ctx := baggage.ContextWithBaggage(context.Background(), bag)
	carrier := otelpropagation.MapCarrier{"baggage": "stale=secret"}

	trusted := policy.ExtractTrusted(ctx, carrier)
	if baggage.FromContext(trusted).Len() != 0 {
		t.Fatal("disabled trusted extraction retained baggage")
	}
	policy.Inject(ctx, carrier)
	if carrier.Get("baggage") != "" {
		t.Fatalf("disabled injection baggage = %q, want empty replacement", carrier.Get("baggage"))
	}
}

func TestTrustedPolicyBoundsItemCountAndOutboundSize(t *testing.T) {
	t.Parallel()

	policy, err := New(Config{
		BaggageEnabled:     true,
		TrustedBaggageKeys: []string{"first", "second"},
		MaxHeaderBytes:     16,
		MaxBaggageItems:    1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	carrier := otelpropagation.MapCarrier{"baggage": "first=1,second=2"}
	extracted := policy.ExtractTrusted(context.Background(), carrier)
	if bag := baggage.FromContext(extracted); bag.Len() != 1 {
		t.Fatalf("trusted baggage = %q, want one bounded item", bag.String())
	}
	long, _ := baggage.NewMember("first", strings.Repeat("x", 32))
	bag, _ := baggage.New(long)
	policy.Inject(baggage.ContextWithBaggage(context.Background(), bag), carrier)
	if carrier.Get("baggage") != "" {
		t.Fatalf("oversized outbound baggage = %q, want empty", carrier.Get("baggage"))
	}
}

func TestPolicyIgnoresOversizedTraceContext(t *testing.T) {
	t.Parallel()

	policy, err := New(Config{MaxHeaderBytes: 8, MaxBaggageItems: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	carrier := otelpropagation.MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}
	ctx := policy.Extract(context.Background(), carrier)
	if trace.SpanContextFromContext(ctx).IsValid() {
		t.Fatal("oversized trace context was extracted")
	}
}
