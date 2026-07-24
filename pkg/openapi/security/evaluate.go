// Package security evaluates OpenAPI Security Requirement Objects without
// coupling authorization decisions to an HTTP framework.
package security

import (
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidRequirements reports malformed Security Requirement values.
	ErrInvalidRequirements = errors.New("invalid OpenAPI security requirements")
	// ErrLimitExceeded reports bounded security evaluation exhaustion.
	ErrLimitExceeded = errors.New("OpenAPI security evaluation limit exceeded")
)

// Credentials maps an available security scheme to its granted OAuth or
// OpenID Connect scopes. Presence with an empty scope list satisfies schemes
// that do not use scopes.
type Credentials map[string][]string

// Limits bounds one Security Requirement array evaluation.
type Limits struct {
	MaxAlternatives int
	MaxSchemes      int
	MaxScopes       int
}

// DefaultLimits returns conservative limits for untrusted requirements.
func DefaultLimits() Limits {
	return Limits{MaxAlternatives: 1_000, MaxSchemes: 1_000, MaxScopes: 10_000}
}

// Satisfied reports whether credentials satisfy a Security Requirement array.
// Array entries are alternatives (OR), while names in one entry are combined
// requirements (AND). An empty array or an empty requirement permits anonymous
// access.
func Satisfied(
	requirements jsonvalue.Value,
	credentials Credentials,
	limits Limits,
) (bool, error) {
	limits, valid := effectiveLimits(limits)
	if !valid {
		return false, fmt.Errorf("%w: invalid limits", ErrInvalidRequirements)
	}
	alternatives, valid := requirements.Elements()
	if !valid {
		return false, fmt.Errorf("%w: expected array", ErrInvalidRequirements)
	}
	if len(alternatives) > limits.MaxAlternatives {
		return false, ErrLimitExceeded
	}
	if len(alternatives) == 0 {
		return true, nil
	}
	totalSchemes := 0
	totalScopes := 0
	for _, alternative := range alternatives {
		schemes, valid := alternative.Members()
		if !valid {
			return false, fmt.Errorf("%w: expected requirement object", ErrInvalidRequirements)
		}
		totalSchemes += len(schemes)
		if totalSchemes > limits.MaxSchemes {
			return false, ErrLimitExceeded
		}
		matched := true
		for _, scheme := range schemes {
			requiredScopes, valid := scheme.Value.Elements()
			if !valid {
				return false, fmt.Errorf("%w: expected scope array", ErrInvalidRequirements)
			}
			totalScopes += len(requiredScopes)
			if totalScopes > limits.MaxScopes {
				return false, ErrLimitExceeded
			}
			granted, available := credentials[scheme.Name]
			if !available {
				matched = false
			}
			grantedSet := make(map[string]struct{}, len(granted))
			for _, scope := range granted {
				grantedSet[scope] = struct{}{}
			}
			for _, rawScope := range requiredScopes {
				scope, valid := rawScope.Text()
				if !valid {
					return false, fmt.Errorf("%w: scope must be a string", ErrInvalidRequirements)
				}
				if _, exists := grantedSet[scope]; !exists {
					matched = false
				}
			}
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func effectiveLimits(limits Limits) (Limits, bool) {
	if limits.MaxAlternatives < 0 || limits.MaxSchemes < 0 || limits.MaxScopes < 0 {
		return Limits{}, false
	}
	defaults := DefaultLimits()
	if limits.MaxAlternatives == 0 {
		limits.MaxAlternatives = defaults.MaxAlternatives
	}
	if limits.MaxSchemes == 0 {
		limits.MaxSchemes = defaults.MaxSchemes
	}
	if limits.MaxScopes == 0 {
		limits.MaxScopes = defaults.MaxScopes
	}
	return limits, true
}
