package propagation

import (
	"context"
	"testing"

	otelpropagation "go.opentelemetry.io/otel/propagation"
)

func FuzzPropagationHeaders(f *testing.F) {
	f.Add(
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"vendor=value",
	)
	f.Add("malformed", "")
	policy, err := New(Config{MaxHeaderBytes: 256, MaxBaggageItems: 4})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, traceparent, tracestate string) {
		carrier := otelpropagation.MapCarrier{
			"traceparent": traceparent,
			"tracestate":  tracestate,
		}
		ctx := policy.Extract(context.Background(), carrier)
		policy.Inject(ctx, carrier)
		if len(carrier.Get("traceparent"))+len(carrier.Get("tracestate")) > 256 {
			t.Fatal("outbound trace context exceeded configured bound")
		}
	})
}

func FuzzUntrustedMetadata(f *testing.F) {
	f.Add("tenant.tier=gold,user.id=secret")
	f.Add("malformed baggage")
	policy, err := New(Config{
		BaggageEnabled:     true,
		TrustedBaggageKeys: []string{"tenant.tier"},
		MaxHeaderBytes:     256,
		MaxBaggageItems:    2,
	})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, metadata string) {
		carrier := otelpropagation.MapCarrier{"baggage": metadata}
		_ = policy.Extract(context.Background(), carrier)
		trusted := policy.ExtractTrusted(context.Background(), carrier)
		policy.Inject(trusted, carrier)
		if len(carrier.Get("baggage")) > 256 {
			t.Fatal("outbound baggage exceeded configured bound")
		}
	})
}
