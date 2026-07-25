package postgres

import (
	"context"
	"log/slog"
)

type slogObserver struct {
	logger *slog.Logger
}

// NewSlogObserver creates a privacy-preserving Observer compatible with slog
// and log. It records only fixed operation metadata and pool gauges.
func NewSlogObserver(logger *slog.Logger) Observer {
	if logger == nil {
		logger = slog.Default()
	}

	return &slogObserver{logger: logger}
}

func (o *slogObserver) Observe(ctx context.Context, observation Observation) {
	level := slog.LevelDebug
	if observation.Outcome != OutcomeSuccess {
		level = slog.LevelError
	}

	attributes := []slog.Attr{
		slog.String("operation", string(observation.Operation)),
		slog.String("outcome", string(observation.Outcome)),
		slog.Duration("duration", observation.Duration),
		slog.String("error.kind", string(observation.ErrorKind)),
		slog.String("db.response.status_code", observation.SQLState),
	}
	if observation.HasPoolStats {
		attributes = append(attributes,
			slog.Int64("pool.acquired", int64(observation.Pool.AcquiredConns)),
			slog.Int64("pool.idle", int64(observation.Pool.IdleConns)),
			slog.Int64("pool.total", int64(observation.Pool.TotalConns)),
			slog.Int64("pool.max", int64(observation.Pool.MaxConns)),
		)
	}
	o.logger.LogAttrs(ctx, level, "postgres operation", attributes...)
}

var _ Observer = (*slogObserver)(nil)
