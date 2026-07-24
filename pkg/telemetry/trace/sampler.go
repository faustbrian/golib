// Package trace defines stable trace configuration and sampling policies.
package trace

import (
	"fmt"
	"math"

	"go.opentelemetry.io/otel/sdk/trace"
)

// Mode selects the root-span sampling strategy.
type Mode string

const (
	// ModeAlwaysOn samples every root span.
	ModeAlwaysOn Mode = "always_on"
	// ModeAlwaysOff drops every root span.
	ModeAlwaysOff Mode = "always_off"
	// ModeRatio deterministically samples a ratio of trace IDs.
	ModeRatio Mode = "ratio"
)

// Config describes a sampler without replacing the OpenTelemetry sampler API.
type Config struct {
	Mode        Mode
	Ratio       float64
	ParentBased bool
}

// NewSampler constructs a standard OpenTelemetry SDK sampler.
func NewSampler(config Config) (trace.Sampler, error) {
	var sampler trace.Sampler
	switch config.Mode {
	case ModeAlwaysOn:
		sampler = trace.AlwaysSample()
	case ModeAlwaysOff:
		sampler = trace.NeverSample()
	case ModeRatio:
		if math.IsNaN(config.Ratio) || math.IsInf(config.Ratio, 0) || config.Ratio < 0 || config.Ratio > 1 {
			return nil, fmt.Errorf("sample ratio %f must be between zero and one", config.Ratio)
		}
		sampler = trace.TraceIDRatioBased(config.Ratio)
	default:
		return nil, fmt.Errorf("sampling mode %q is unsupported", config.Mode)
	}
	if config.ParentBased {
		sampler = trace.ParentBased(sampler)
	}
	return sampler, nil
}
