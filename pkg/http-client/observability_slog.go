package httpclient

import (
	"context"
	"fmt"
	"log/slog"
)

// SlogTelemetryObserver emits safe fixed telemetry fields without URLs,
// headers, bodies, identity scope values, or error text.
type SlogTelemetryObserver struct{ logger *slog.Logger }

// NewSlogTelemetryObserver adapts a standard-library structured logger.
func NewSlogTelemetryObserver(logger *slog.Logger) (*SlogTelemetryObserver, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: slog logger is nil", ErrInvalidTelemetry)
	}
	return &SlogTelemetryObserver{logger: logger}, nil
}

// Start logs one operation or attempt start and preserves ctx.
func (observer *SlogTelemetryObserver) Start(ctx context.Context, event TelemetryEvent) context.Context {
	observer.log(ctx, event)
	return ctx
}

// Finish logs one operation or attempt completion.
func (observer *SlogTelemetryObserver) Finish(ctx context.Context, event TelemetryEvent) {
	observer.log(ctx, event)
}

func (observer *SlogTelemetryObserver) log(ctx context.Context, event TelemetryEvent) {
	attributes := []slog.Attr{
		slog.String("phase", string(event.Phase)),
		slog.String("scope", string(event.Scope)),
		slog.Int("attempt", event.Attempt),
		slog.String("operation_id", event.OperationID),
		slog.String("method", telemetryMethod(event.Method)),
		slog.String("profile", string(event.Profile)),
		slog.String("outcome", string(event.Outcome)),
		slog.String("status_class", event.StatusClass),
		slog.String("cache", string(event.Cache)),
	}
	observer.logger.LogAttrs(ctx, slog.LevelDebug, "http client telemetry", attributes...)
}
