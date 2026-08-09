package opensearch

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type TelemetryCategory string

const (
	TelemetrySuccess      TelemetryCategory = "success"
	TelemetryTransport    TelemetryCategory = "transport"
	TelemetryCredentials  TelemetryCategory = "credentials"
	TelemetryBackpressure TelemetryCategory = "backpressure"
	TelemetryCircuitOpen  TelemetryCategory = "circuit_open"
	TelemetryOverloaded   TelemetryCategory = "overloaded"
	TelemetryHTTPFailure  TelemetryCategory = "http_failure"
)

// TelemetryEvent contains only stable low-cardinality adapter state. It never
// contains endpoints, tenant labels, index names, query fields, request bodies,
// credentials, OpenSearch reason strings, or node IDs.
type TelemetryEvent struct {
	Operation   Operation
	Category    TelemetryCategory
	Status      int
	Duration    time.Duration
	InFlight    int
	Queued      int
	CircuitOpen bool
}

type TelemetryObserver interface {
	Observe(context.Context, TelemetryEvent) error
}
type TelemetryObserverFunc func(context.Context, TelemetryEvent) error

func (observe TelemetryObserverFunc) Observe(ctx context.Context, event TelemetryEvent) error {
	return observe(ctx, event)
}

type TelemetryConfig struct {
	Observer TelemetryObserver
	Clock    func() time.Time
}

type telemetry struct {
	observer TelemetryObserver
	clock    func() time.Time
}

func newTelemetry(config *TelemetryConfig) (*telemetry, error) {
	if config == nil {
		return &telemetry{clock: time.Now}, nil
	}
	if config.Observer == nil || config.Clock == nil {
		return nil, ErrInvalidConfig
	}
	return &telemetry{observer: config.Observer, clock: config.Clock}, nil
}

func (telemetry *telemetry) now() time.Time { return telemetry.clock() }

func (telemetry *telemetry) observe(ctx context.Context, event TelemetryEvent) {
	if telemetry.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = telemetry.observer.Observe(ctx, event)
}

func (telemetry *telemetry) event(operation Operation, started time.Time, status int, err error, resilience ResilienceSnapshot) TelemetryEvent {
	category := TelemetrySuccess
	switch {
	case errors.Is(err, ErrCredentials):
		category = TelemetryCredentials
	case errors.Is(err, ErrBackpressure):
		category = TelemetryBackpressure
	case errors.Is(err, ErrCircuitOpen):
		category = TelemetryCircuitOpen
	case err != nil:
		category = TelemetryTransport
	case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
		category = TelemetryOverloaded
	case status >= 400:
		category = TelemetryHTTPFailure
	}
	duration := telemetry.now().Sub(started)
	duration = max(duration, 0)
	return TelemetryEvent{Operation: operation, Category: category, Status: status, Duration: duration, InFlight: resilience.InFlight, Queued: resilience.Queued, CircuitOpen: resilience.CircuitOpen}
}

type operationContextKey struct{}

func withOperation(ctx context.Context, operation Operation) context.Context {
	return context.WithValue(ctx, operationContextKey{}, operation)
}
func operationFromContext(ctx context.Context) Operation {
	if operation, ok := ctx.Value(operationContextKey{}).(Operation); ok {
		return operation
	}
	return Operation("unknown")
}
