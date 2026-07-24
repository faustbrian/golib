package localized

// Error is a stable privacy-safe error identity.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

const (
	ErrConflict         Error = "localized: merge conflict"
	ErrDuplicateLocale  Error = "localized: duplicate locale"
	ErrInvalidEncoding  Error = "localized: invalid encoding"
	ErrInvalidLocale    Error = "localized: invalid locale"
	ErrInvalidPolicy    Error = "localized: invalid policy"
	ErrInvalidUTF8      Error = "localized: invalid UTF-8"
	ErrLimitExceeded    Error = "localized: limit exceeded"
	ErrLocaleRejected   Error = "localized: locale rejected"
	ErrMissingLocale    Error = "localized: missing locale"
	ErrNullValue        Error = "localized: null value"
	ErrResolverRequired Error = "localized: resolver required"
	ErrTrailingInput    Error = "localized: trailing input"
)
