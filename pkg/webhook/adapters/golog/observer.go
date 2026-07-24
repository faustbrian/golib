// Package golog adapts secret-safe webhook observations to log's standard
// slog surface.
package golog

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	webhook "github.com/faustbrian/golib/pkg/webhook"
)

var ErrInvalidConfig = errors.New("golog: logger is required")

// Observer emits a fixed structured record. Its schema intentionally has no
// payload, URL, header, event ID, key ID, signature, nonce, or raw error field.
type Observer struct {
	logger *slog.Logger
	level  slog.Level
}

var _ webhook.Observer = (*Observer)(nil)

// New constructs a log-compatible observer. log returns *slog.Logger,
// so no logger-specific interface or global state is required.
func New(logger *slog.Logger, level slog.Level) (*Observer, error) {
	if logger == nil {
		return nil, ErrInvalidConfig
	}

	return &Observer{logger: logger, level: level}, nil
}

// Observe implements webhook.Observer.
func (o *Observer) Observe(ctx context.Context, event webhook.Observation) {
	o.logger.LogAttrs(ctx, o.level, "webhook lifecycle",
		slog.String("webhook.operation", string(event.Operation)),
		slog.String("webhook.outcome", string(event.Outcome)),
		slog.String("webhook.reason", string(event.Reason)),
		slog.String("webhook.algorithm", string(event.Algorithm)),
		slog.String("webhook.classification", string(event.Classification)),
		slog.String("http.response.status_class", statusClass(event.StatusCode)),
		slog.Int("webhook.attempt", event.Attempt),
		slog.Int64("webhook.duration_ms", event.Duration.Milliseconds()),
	)
}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "none"
	}

	return strconv.Itoa(status/100) + "xx"
}
