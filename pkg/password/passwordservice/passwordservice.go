package passwordservice

import (
	"context"
	"errors"

	password "github.com/faustbrian/golib/pkg/password"
)

// ErrInvalidConfig reports a missing admission controller.
var ErrInvalidConfig = errors.New("passwordservice: invalid configuration")

// Lifecycle exposes service-compatible start and stop hooks for Admission.
type Lifecycle struct{ admission *password.Admission }

// New wraps a caller-owned admission controller.
func New(admission *password.Admission) (*Lifecycle, error) {
	if admission == nil {
		return nil, ErrInvalidConfig
	}
	return &Lifecycle{admission: admission}, nil
}

// Start validates the context and rejects restart after shutdown.
func (l *Lifecycle) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.admission.Closed() {
		return password.ErrClosed
	}
	return nil
}

// Stop closes admission and drains active work within ctx.
func (l *Lifecycle) Stop(ctx context.Context) error { return l.admission.Shutdown(ctx) }
