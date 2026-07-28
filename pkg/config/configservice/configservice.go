// Package configservice adapts typed configuration plans to service command
// loaders without owning long-lived resources.
package configservice

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/dotenv"
	"github.com/faustbrian/golib/pkg/config/environment"
	"github.com/faustbrian/golib/pkg/config/validation"
	"github.com/faustbrian/golib/pkg/service"
)

// ErrInvalidOptions identifies invalid loader construction.
var ErrInvalidOptions = errors.New("invalid config service options")

// OptionsError identifies one invalid loader option without formatting its
// underlying cause.
type OptionsError struct {
	// Field identifies the rejected option.
	Field string
	// Reason describes the safe failure category.
	Reason string
	// Cause retains the construction failure for errors.Is and errors.As.
	Cause error
}

// Error returns a safe loader-construction diagnostic.
func (err *OptionsError) Error() string {
	return fmt.Sprintf("%s: %s: %v", err.Field, err.Reason, ErrInvalidOptions)
}

// Unwrap exposes both the option classification and construction cause.
func (err *OptionsError) Unwrap() []error {
	causes := []error{ErrInvalidOptions}
	if err.Cause != nil {
		causes = append(causes, err.Cause)
	}
	return causes
}

// Dotenv describes an explicitly local dotenv source. The caller retains
// ownership of FS; each load opens Path through the config dotenv adapter.
type Dotenv struct {
	// FS contains Path.
	FS fs.FS
	// Path identifies the dotenv document within FS.
	Path string
	// Options configure bounded parsing and typed mapping.
	Options dotenv.Options
}

// Options configure one immutable typed service loader.
type Options[T any] struct {
	// Sources contains caller-owned sources grouped by default precedence.
	Sources config.DefaultSources
	// Local explicitly permits Dotenv. Dotenv is rejected when Local is false.
	Local bool
	// Dotenv adds one local dotenv source when non-nil.
	Dotenv *Dotenv
	// Environment adds the process environment when non-nil.
	Environment *environment.Options
	// Validators run after complete typed decoding.
	Validators []validation.Validator[T]
}

// Loader is directly assignable to service.CommandSpec.Load. It owns no
// resource, performs no retries, and is safe to invoke repeatedly when its
// caller-provided sources are repeatable.
type Loader[T any] func(context.Context, service.Invocation) (T, error)

// New constructs a typed loader. Configuration is resolved only when the
// selected service command invokes the loader, before component construction.
func New[T any](options Options[T]) (Loader[T], error) {
	sources := options.Sources
	if options.Dotenv != nil {
		if !options.Local {
			return nil, invalid("Dotenv", "requires explicit local mode", nil)
		}
		if options.Dotenv.FS == nil || strings.TrimSpace(options.Dotenv.Path) == "" {
			return nil, invalid("Dotenv", "requires a filesystem and path", nil)
		}
		source, err := dotenv.FromFSFor[T](
			options.Dotenv.FS,
			options.Dotenv.Path,
			options.Dotenv.Options,
		)
		if err != nil {
			return nil, invalid("Dotenv", "source construction failed", err)
		}
		sources.Dotenv = append(sources.Dotenv, source)
	}
	if options.Environment != nil {
		source, err := environment.ProcessFor[T](*options.Environment)
		if err != nil {
			return nil, invalid("Environment", "source construction failed", err)
		}
		sources.Environment = append(sources.Environment, source)
	}
	plan, err := config.NewDefaultPlan(sources)
	if err != nil {
		return nil, invalid("Sources", "plan construction failed", err)
	}
	validators := append([]validation.Validator[T](nil), options.Validators...)

	return func(ctx context.Context, _ service.Invocation) (T, error) {
		snapshot, err := config.LoadWithValidators[T](ctx, plan, validators...)
		if err != nil {
			var zero T
			return zero, err
		}
		return snapshot.Value(), nil
	}, nil
}

func invalid(field, reason string, cause error) error {
	return &OptionsError{Field: field, Reason: reason, Cause: cause}
}
