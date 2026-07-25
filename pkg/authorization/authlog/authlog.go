// Package authlog emits bounded authorization audit events through log/slog.
// Loggers constructed by log use this standard logger type directly.
package authlog

import (
	"context"
	"errors"
	"log/slog"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

var ErrNilLogger = errors.New("authorization audit logger is nil")

type Instrumenter struct {
	logger *slog.Logger
	level  slog.Level
}

func New(logger *slog.Logger, level slog.Level) (*Instrumenter, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	return &Instrumenter{logger: logger, level: level}, nil
}

func (instrumenter *Instrumenter) Start(
	ctx context.Context,
) (context.Context, func(authorization.Event)) {
	return ctx, func(event authorization.Event) {
		instrumenter.logger.LogAttrs(ctx, instrumenter.level, "authorization decision",
			slog.String("outcome", event.Outcome.String()),
			slog.String("reason", string(event.Reason)),
			slog.Uint64("revision", uint64(event.Revision)),
			slog.Any("matched_policy_ids", event.MatchedPolicyIDs),
			slog.Bool("matched_policy_ids_truncated", event.MatchedPolicyIDsTruncated),
			slog.Int("trace_count", event.TraceCount),
			slog.Bool("trace_truncated", event.TraceTruncated),
			slog.Float64("duration_ms", float64(event.Duration.Microseconds())/1000),
			slog.Bool("failed", event.Failed),
		)
	}
}

var _ authorization.Instrumenter = (*Instrumenter)(nil)
