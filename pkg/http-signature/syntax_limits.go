package httpsignature

import "errors"

var (
	// ErrInvalidSyntaxLimits reports missing or contradictory parser bounds.
	ErrInvalidSyntaxLimits = errors.New("http signature: invalid syntax limits")
	// ErrSyntaxLimit reports input rejected by an explicit parser bound.
	ErrSyntaxLimit = errors.New("http signature: syntax resource limit exceeded")
)

// SyntaxLimits bounds untrusted Structured Fields before and after parsing.
// Limits are per logical combined field.
type SyntaxLimits struct {
	MaxFieldBytes             int
	MaxFieldLines             int
	MaxDictionaryMembers      int
	MaxComponentsPerSignature int
	MaxParametersPerItem      int
	MaxBinaryBytes            int
}

// DefaultSyntaxLimits returns the restrictive bound used by convenience
// parsers. A fresh value prevents mutable package-global parser policy.
func DefaultSyntaxLimits() SyntaxLimits {
	return SyntaxLimits{
		MaxFieldBytes:             64 << 10,
		MaxFieldLines:             32,
		MaxDictionaryMembers:      32,
		MaxComponentsPerSignature: 64,
		MaxParametersPerItem:      16,
		MaxBinaryBytes:            16 << 10,
	}
}

// Validate checks that every resource dimension has a positive bound.
func (limits SyntaxLimits) Validate() error {
	if limits.MaxFieldLines <= 0 || limits.MaxDictionaryMembers <= 0 ||
		limits.MaxComponentsPerSignature <= 0 || limits.MaxParametersPerItem <= 0 || limits.MaxBinaryBytes <= 0 ||
		limits.MaxBinaryBytes > limits.MaxFieldBytes {
		return ErrInvalidSyntaxLimits
	}
	return nil
}

func enforceRawSyntaxLimits(values []string, limits SyntaxLimits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if len(values) == 0 || len(values) > limits.MaxFieldLines {
		return ErrSyntaxLimit
	}
	remaining := limits.MaxFieldBytes
	for _, value := range values {
		if len(value) > remaining {
			return ErrSyntaxLimit
		}
		remaining = remaining - len(value)
	}
	return nil
}
