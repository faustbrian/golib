// Package requestidbridge integrates explicitly with request ID middleware
// such as http-middleware/requestid without importing hidden context keys.
package requestidbridge

import (
	"context"
	"errors"
	"fmt"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

var (
	// ErrInvalidLookup reports a missing lookup or malformed returned value.
	ErrInvalidLookup = errors.New("request ID bridge: invalid lookup")
	// ErrUntrusted reports a source that was not explicitly trusted.
	ErrUntrusted = errors.New("request ID bridge: source is not trusted")
	// ErrMissing reports an absent middleware request ID.
	ErrMissing = errors.New("request ID bridge: request ID is missing")
	// ErrOverwrite reports an attempt to replace an existing request ID.
	ErrOverwrite = errors.New("request ID bridge: request ID overwrite")
)

// Lookup matches a bound call to requestid.FromContext. The application keeps
// ownership of which middleware package and Kind it selects.
type Lookup func(context.Context) (string, bool)

// Options require explicit trust for the middleware-owned context value.
type Options struct {
	Policy  correlation.Policy
	Trusted bool
}

// Adopt validates a middleware request ID, refuses overwrite, and returns a
// derived context containing the complete correlation Values.
func Adopt(ctx context.Context, values correlation.Values, lookup Lookup, options Options) (context.Context, correlation.Values, error) {
	if ctx == nil || lookup == nil {
		return ctx, correlation.Values{}, ErrInvalidLookup
	}
	if !options.Trusted {
		return ctx, correlation.Values{}, ErrUntrusted
	}
	if values.RequestID != "" {
		return ctx, correlation.Values{}, ErrOverwrite
	}
	text, ok := lookup(ctx)
	if !ok || text == "" {
		return ctx, correlation.Values{}, ErrMissing
	}
	requestID, err := correlation.ParseRequestID(text, options.Policy)
	if err != nil {
		return ctx, correlation.Values{}, fmt.Errorf("%w: %w", ErrInvalidLookup, err)
	}
	values.RequestID = requestID
	return correlation.WithValues(ctx, values), values, nil
}
