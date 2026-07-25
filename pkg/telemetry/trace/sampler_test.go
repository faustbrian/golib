package trace

import (
	"context"
	"math"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	traceapi "go.opentelemetry.io/otel/trace"
)

func TestParentBasedSamplerPreservesRemoteDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parentFlag traceapi.TraceFlags
		ratio      float64
		want       trace.SamplingDecision
	}{
		{name: "sampled parent", parentFlag: traceapi.FlagsSampled, ratio: 0, want: trace.RecordAndSample},
		{name: "unsampled parent", parentFlag: 0, ratio: 1, want: trace.Drop},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sampler, err := NewSampler(Config{Mode: ModeRatio, Ratio: test.ratio, ParentBased: true})
			if err != nil {
				t.Fatalf("NewSampler() error = %v", err)
			}
			parent := traceapi.NewSpanContext(traceapi.SpanContextConfig{
				TraceID:    traceapi.TraceID{1},
				SpanID:     traceapi.SpanID{1},
				TraceFlags: test.parentFlag,
				Remote:     true,
			})
			result := sampler.ShouldSample(trace.SamplingParameters{
				ParentContext: traceapi.ContextWithRemoteSpanContext(context.Background(), parent),
				TraceID:       parent.TraceID(),
				Name:          "child",
			})
			if result.Decision != test.want {
				t.Fatalf("sampling decision = %v, want %v", result.Decision, test.want)
			}
		})
	}
}

func TestNewSamplerValidatesModesAndRatio(t *testing.T) {
	t.Parallel()

	for _, config := range []Config{
		{Mode: Mode("unknown")},
		{Mode: ModeRatio, Ratio: -0.1},
		{Mode: ModeRatio, Ratio: 1.1},
		{Mode: ModeRatio, Ratio: math.NaN()},
		{Mode: ModeRatio, Ratio: math.Inf(1)},
	} {
		if _, err := NewSampler(config); err == nil {
			t.Fatalf("NewSampler(%+v) error = nil, want validation error", config)
		}
	}
}

func TestSamplerModes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mode Mode
		want trace.SamplingDecision
	}{
		{mode: ModeAlwaysOn, want: trace.RecordAndSample},
		{mode: ModeAlwaysOff, want: trace.Drop},
	} {
		sampler, err := NewSampler(Config{Mode: test.mode})
		if err != nil {
			t.Fatalf("NewSampler() error = %v", err)
		}
		result := sampler.ShouldSample(trace.SamplingParameters{TraceID: traceapi.TraceID{1}})
		if result.Decision != test.want {
			t.Fatalf("mode %q decision = %v, want %v", test.mode, result.Decision, test.want)
		}
	}
}
