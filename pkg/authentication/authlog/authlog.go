// Package authlog adapts authentication instrumentation to log/slog.
package authlog

import (
	"context"
	"fmt"
	"log/slog"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

// Instrumenter emits one bounded structured log record per attempt.
type Instrumenter struct {
	logger *slog.Logger
}

// New creates a structured authentication log instrumenter.
func New(logger *slog.Logger) (*Instrumenter, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: nil logger", authentication.ErrInvalidConfiguration)
	}
	return &Instrumenter{logger: logger}, nil
}

// Start implements authentication.Instrumenter.
func (i *Instrumenter) Start(
	ctx context.Context,
	kind authentication.CredentialKind,
) (context.Context, func(authentication.Event)) {
	return ctx, func(event authentication.Event) {
		i.logger.InfoContext(ctx, "authentication completed",
			"credential_kind", kind,
			"outcome", event.Outcome,
			"failure_kind", event.Failure,
			"duration_ms", event.Duration.Milliseconds(),
		)
	}
}

var _ authentication.Instrumenter = (*Instrumenter)(nil)
