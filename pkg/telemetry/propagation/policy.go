// Package propagation provides bounded W3C propagation with explicit inbound
// trust decisions.
package propagation

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/baggage"
	otelpropagation "go.opentelemetry.io/otel/propagation"
)

// Config bounds propagation input and allow-lists baggage keys.
type Config struct {
	BaggageEnabled     bool
	TrustedBaggageKeys []string
	MaxHeaderBytes     int
	MaxBaggageItems    int
}

// DefaultConfig returns conservative propagation bounds. Baggage is disabled
// until applications define a trusted key contract.
func DefaultConfig() Config {
	return Config{
		BaggageEnabled:  false,
		MaxHeaderBytes:  8 * 1_024,
		MaxBaggageItems: 16,
	}
}

// Policy implements the standard TextMapPropagator interface. Its Extract
// method treats all inbound carriers as untrusted.
type Policy struct {
	config  Config
	allowed map[string]struct{}
	trace   otelpropagation.TraceContext
	bag     otelpropagation.Baggage
}

// New validates and constructs a propagation policy.
func New(config Config) (*Policy, error) {
	var errs []error
	if config.MaxHeaderBytes <= 0 {
		errs = append(errs, errors.New("maximum propagation header bytes must be positive"))
	}
	if config.MaxBaggageItems <= 0 {
		errs = append(errs, errors.New("maximum baggage items must be positive"))
	}
	allowed := make(map[string]struct{}, len(config.TrustedBaggageKeys))
	for _, key := range config.TrustedBaggageKeys {
		if _, err := baggage.NewMember(key, "value"); err != nil {
			errs = append(errs, fmt.Errorf("trusted baggage key %q: %w", key, err))
		}
		if _, duplicate := allowed[key]; duplicate {
			errs = append(errs, fmt.Errorf("trusted baggage key %q is duplicated", key))
		}
		allowed[key] = struct{}{}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &Policy{config: config, allowed: allowed}, nil
}

// Extract applies the untrusted inbound policy: valid W3C trace context is
// accepted, while all baggage is removed.
func (p *Policy) Extract(ctx context.Context, carrier otelpropagation.TextMapCarrier) context.Context {
	ctx = p.extractTrace(ctx, carrier)
	return baggage.ContextWithBaggage(ctx, baggage.Baggage{})
}

// ExtractTrusted accepts only allow-listed, bounded baggage from a trusted
// peer while applying the same bounded trace-context parsing.
func (p *Policy) ExtractTrusted(ctx context.Context, carrier otelpropagation.TextMapCarrier) context.Context {
	ctx = p.extractTrace(ctx, carrier)
	ctx = baggage.ContextWithBaggage(ctx, baggage.Baggage{})
	if !p.config.BaggageEnabled || !withinLimit(carrier, p.config.MaxHeaderBytes, "baggage") {
		return ctx
	}
	extracted := p.bag.Extract(ctx, carrier)
	members := filterMembers(baggage.FromContext(extracted).Members(), p.allowed, p.config.MaxBaggageItems)
	filtered, _ := baggage.New(members...)
	return baggage.ContextWithBaggage(ctx, filtered)
}

// Inject replaces outbound W3C headers and propagates only allow-listed,
// bounded baggage.
func (p *Policy) Inject(ctx context.Context, carrier otelpropagation.TextMapCarrier) {
	carrier.Set("traceparent", "")
	carrier.Set("tracestate", "")
	p.trace.Inject(ctx, carrier)
	carrier.Set("baggage", "")
	if !p.config.BaggageEnabled {
		return
	}
	members := filterMembers(baggage.FromContext(ctx).Members(), p.allowed, p.config.MaxBaggageItems)
	filtered, _ := baggage.New(members...)
	filteredContext := baggage.ContextWithBaggage(ctx, filtered)
	p.bag.Inject(filteredContext, carrier)
	if len(carrier.Get("baggage")) > p.config.MaxHeaderBytes {
		carrier.Set("baggage", "")
	}
}

// Fields returns every carrier field this policy may read or write.
func (p *Policy) Fields() []string {
	fields := []string{"traceparent", "tracestate"}
	if p.config.BaggageEnabled {
		fields = append(fields, "baggage")
	}
	return fields
}

func (p *Policy) extractTrace(ctx context.Context, carrier otelpropagation.TextMapCarrier) context.Context {
	if !withinLimit(carrier, p.config.MaxHeaderBytes, "traceparent", "tracestate") {
		return ctx
	}
	return p.trace.Extract(ctx, carrier)
}

func withinLimit(carrier otelpropagation.TextMapCarrier, limit int, fields ...string) bool {
	total := 0
	for _, field := range fields {
		total += len(carrier.Get(field))
		if total > limit {
			return false
		}
	}
	return true
}

func filterMembers(members []baggage.Member, allowed map[string]struct{}, limit int) []baggage.Member {
	filtered := make([]baggage.Member, 0, min(len(members), limit))
	for _, member := range members {
		if _, ok := allowed[member.Key()]; !ok {
			continue
		}
		filtered = append(filtered, member)
		if len(filtered) == limit {
			break
		}
	}
	return filtered
}
