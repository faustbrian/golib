package validation

import "fmt"

// Context is immutable deterministic validation state. It intentionally does
// not embed context.Context because ordinary validators cannot perform I/O.
type Context struct {
	limits    Limits
	locale    string
	operation string
	metadata  map[string]string
	path      Path
}

type contextConfig struct {
	locale    string
	operation string
	metadata  map[string]string
}

// ContextOption configures NewContext.
type ContextOption func(*contextConfig)

// WithLocale records an application-defined locale identifier.
func WithLocale(locale string) ContextOption {
	return func(config *contextConfig) { config.locale = locale }
}

// WithOperation records an application-defined operation identifier.
func WithOperation(operation string) ContextOption {
	return func(config *contextConfig) { config.operation = operation }
}

// WithMetadata adds bounded non-sensitive metadata.
func WithMetadata(key, value string) ContextOption {
	return func(config *contextConfig) { config.metadata[key] = value }
}

// NewContext constructs immutable validation state.
func NewContext(limits Limits, options ...ContextOption) (Context, error) {
	if err := limits.validate(); err != nil {
		return Context{}, err
	}
	config := contextConfig{metadata: make(map[string]string)}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	if len(config.locale) > limits.MaxMetadataValueLength {
		return Context{}, fmt.Errorf("%w: locale size", ErrLimitExceeded)
	}
	if len(config.operation) > limits.MaxMetadataValueLength {
		return Context{}, fmt.Errorf("%w: operation size", ErrLimitExceeded)
	}
	if len(config.metadata) > limits.MaxMetadataEntries {
		return Context{}, fmt.Errorf("%w: metadata entries", ErrLimitExceeded)
	}
	for key, value := range config.metadata {
		if len(key) > limits.MaxMetadataKeyLength ||
			len(value) > limits.MaxMetadataValueLength {
			return Context{}, fmt.Errorf("%w: metadata size", ErrLimitExceeded)
		}
	}
	return Context{limits: limits, locale: config.locale,
		operation: config.operation, metadata: cloneMap(config.metadata)}, nil
}

// Limits returns the work limits. The zero-value Context uses DefaultLimits so
// validators fail closed without requiring construction for simple use.
func (c Context) Limits() Limits {
	if c.limits.MaxViolations == 0 {
		return DefaultLimits()
	}
	return c.limits
}

// Locale returns the application-defined locale.
func (c Context) Locale() string { return c.locale }

// Operation returns the application-defined operation.
func (c Context) Operation() string { return c.operation }

// Metadata returns one metadata value.
func (c Context) Metadata(key string) (string, bool) {
	value, ok := c.metadata[key]
	return value, ok
}

// Path returns the current immutable path.
func (c Context) Path() Path { return c.path }

// WithPath returns a copy with segment appended.
func (c Context) WithPath(segment Segment) Context {
	c.path = c.path.Append(segment)
	return c
}

func cloneMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
