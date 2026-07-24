package slog

import (
	"context"
	"fmt"
	logslog "log/slog"

	cache "github.com/faustbrian/golib/pkg/cache"
)

// Config selects the logger and level used by an Observer.
type Config struct {
	Logger      *logslog.Logger
	Level       logslog.Level
	IncludeSize bool
}

// Observer writes redacted semantic cache events through slog.
type Observer struct {
	logger      *logslog.Logger
	level       logslog.Level
	includeSize bool
}

// New validates config and constructs a logging observer.
func New(config Config) (*Observer, error) {
	if config.Logger == nil {
		return nil, fmt.Errorf("slog observer requires a logger")
	}
	return &Observer{
		logger:      config.Logger,
		level:       config.Level,
		includeSize: config.IncludeSize,
	}, nil
}

// Observe logs one event without key or value data.
func (o *Observer) Observe(ctx context.Context, event cache.Event) error {
	attributes := []logslog.Attr{
		logslog.String("operation", string(event.Operation)),
		logslog.String("outcome", string(event.Outcome)),
		logslog.Float64("duration_ms", float64(event.Duration.Microseconds())/1000),
	}
	if o.includeSize {
		attributes = append(attributes, logslog.Int("size_bytes", event.Size))
	}
	o.logger.LogAttrs(ctx, o.level, "cache operation", attributes...)
	return nil
}
