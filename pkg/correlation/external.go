package correlation

import (
	"errors"
	"fmt"
)

// ErrInvalidExternalID reports invalid external identifier metadata.
var ErrInvalidExternalID = errors.New("correlation: invalid external identifier")

// ExternalIDOptions require the caller to state type, source, and trust.
type ExternalIDOptions struct {
	Kind    string
	Value   string
	Source  string
	Trusted bool
	Policy  Policy
}

// ExternalID is an optional typed external identifier with explicit source
// and trust metadata. It is not implicitly promoted to any correlation ID.
type ExternalID struct {
	kind    string
	value   string
	source  string
	trusted bool
}

// NewExternalID validates and copies external metadata.
func NewExternalID(options ExternalIDOptions) (ExternalID, error) {
	if err := validate(options.Kind, Policy{MaxLength: 64}); err != nil {
		return ExternalID{}, fmt.Errorf("%w: kind", ErrInvalidExternalID)
	}
	if err := validate(options.Source, Policy{MaxLength: 64}); err != nil {
		return ExternalID{}, fmt.Errorf("%w: source", ErrInvalidExternalID)
	}
	if err := validate(options.Value, options.Policy); err != nil {
		return ExternalID{}, fmt.Errorf("%w: value", ErrInvalidExternalID)
	}
	return ExternalID{
		kind: options.Kind, value: options.Value,
		source: options.Source, trusted: options.Trusted,
	}, nil
}

// Kind returns the external identifier's declared semantic kind.
func (external ExternalID) Kind() string { return external.kind }

// Value returns the validated external identifier text.
func (external ExternalID) Value() string { return external.value }

// Source returns the declared transport or system source.
func (external ExternalID) Source() string { return external.source }

// Trusted reports the caller's explicit trust decision.
func (external ExternalID) Trusted() bool { return external.trusted }
