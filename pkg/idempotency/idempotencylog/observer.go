// Package idempotencylog adapts bounded idempotency observations to log/slog.
// Loggers constructed by github.com/faustbrian/golib/pkg/log use this standard type.
package idempotencylog

import (
	"context"
	"errors"
	"log/slog"

	"github.com/faustbrian/golib/pkg/idempotency"
)

// ErrNilLogger reports an unusable logger configuration.
var ErrNilLogger = errors.New("idempotencylog: nil logger")

// Observer writes bounded semantic transition fields to a slog logger.
type Observer struct {
	logger *slog.Logger
}

// New constructs an observer for a standard slog logger, including loggers
// returned by github.com/faustbrian/golib/pkg/log.
func New(logger *slog.Logger) (*Observer, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	return &Observer{logger: logger}, nil
}

// Observe writes one bounded transition record. Correlation is intended only
// for restricted logs and is never an unhashed logical idempotency key.
func (observer *Observer) Observe(ctx context.Context, event idempotency.Observation) {
	observer.logger.InfoContext(ctx, "idempotency transition",
		slog.String("transition", string(event.Transition)),
		slog.String("outcome", string(event.Outcome)),
		slog.String("reason", string(event.Reason)),
		slog.Bool("durable", event.Durable),
		slog.String("correlation", event.Correlation),
	)
}

var _ idempotency.Observer = (*Observer)(nil)
