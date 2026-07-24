// Package authentication defines framework-independent authentication contracts.
package authentication

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

const (
	// MaxClaims is the maximum number of entries in a principal claim map.
	MaxClaims = 128
	// MaxClaimDepth is the maximum nesting depth accepted in principal claims.
	MaxClaimDepth = 8
	// MaxClaimCollection is the maximum number of elements in a nested claim
	// map, slice, or array.
	MaxClaimCollection = 256
)

// ErrInvalidPrincipal identifies a principal that violates an identity
// invariant or contains claims that cannot be copied safely.
var ErrInvalidPrincipal = errors.New("authentication: invalid principal")

// PrincipalSpec contains the identity data used to construct a Principal.
// Callers may reuse or mutate its slices and maps after NewPrincipal returns.
type PrincipalSpec struct {
	Subject         string
	Method          string
	Issuer          string
	Audiences       []string
	TenantHints     []string
	Scopes          []string
	Claims          map[string]any
	AuthenticatedAt time.Time
}

// Principal is an immutable authenticated identity or the explicit anonymous
// identity. Its zero value is anonymous.
type Principal struct {
	subject         string
	method          string
	issuer          string
	audiences       []string
	tenantHints     []string
	scopes          []string
	claims          map[string]any
	authenticatedAt time.Time
}

// NewPrincipal validates and copies authenticated identity data.
func NewPrincipal(spec PrincipalSpec) (Principal, error) {
	if spec.Subject == "" {
		return Principal{}, fmt.Errorf("%w: subject is required", ErrInvalidPrincipal)
	}
	if spec.Method == "" {
		return Principal{}, fmt.Errorf("%w: method is required", ErrInvalidPrincipal)
	}
	if err := validateNonEmpty("audience", spec.Audiences); err != nil {
		return Principal{}, err
	}
	if err := validateNonEmpty("tenant hint", spec.TenantHints); err != nil {
		return Principal{}, err
	}
	if err := validateNonEmpty("scope", spec.Scopes); err != nil {
		return Principal{}, err
	}
	if len(spec.Claims) > MaxClaims {
		return Principal{}, fmt.Errorf("%w: too many claims", ErrInvalidPrincipal)
	}

	claims, err := cloneClaimMap(spec.Claims)
	if err != nil {
		return Principal{}, err
	}

	return Principal{
		subject:         spec.Subject,
		method:          spec.Method,
		issuer:          spec.Issuer,
		audiences:       cloneStrings(spec.Audiences),
		tenantHints:     cloneStrings(spec.TenantHints),
		scopes:          cloneStrings(spec.Scopes),
		claims:          claims,
		authenticatedAt: spec.AuthenticatedAt,
	}, nil
}

// AnonymousPrincipal returns the explicit anonymous identity.
func AnonymousPrincipal() Principal { return Principal{} }

// IsAnonymous reports whether p represents absence of an authenticated identity.
func (p Principal) IsAnonymous() bool { return p.subject == "" }

// Subject returns the stable subject identifier.
func (p Principal) Subject() string { return p.subject }

// Method returns the authentication method that established the identity.
func (p Principal) Method() string { return p.method }

// Issuer returns the authority that asserted the identity, when applicable.
func (p Principal) Issuer() string { return p.issuer }

// Audiences returns a copy of the intended audiences.
func (p Principal) Audiences() []string { return cloneStrings(p.audiences) }

// TenantHints returns a copy of non-authoritative tenant hints.
func (p Principal) TenantHints() []string { return cloneStrings(p.tenantHints) }

// Scopes returns a copy of the scopes asserted by the credential. Scopes are
// authentication data and are not an authorization decision.
func (p Principal) Scopes() []string { return cloneStrings(p.scopes) }

// Claims returns a deep copy of the bounded claim set.
func (p Principal) Claims() map[string]any {
	claims, _ := cloneClaimMap(p.claims)

	return claims
}

// AuthenticatedAt returns the time at which the identity was authenticated.
func (p Principal) AuthenticatedAt() time.Time { return p.authenticatedAt }

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	return append([]string(nil), values...)
}

func validateNonEmpty(name string, values []string) error {
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("%w: empty %s", ErrInvalidPrincipal, name)
		}
	}

	return nil
}

func cloneClaimMap(claims map[string]any) (map[string]any, error) {
	if claims == nil {
		return nil, nil
	}
	cloned := make(map[string]any, len(claims))
	for name, value := range claims {
		if name == "" {
			return nil, fmt.Errorf("%w: empty claim name", ErrInvalidPrincipal)
		}
		copied, err := cloneClaimValue(reflect.ValueOf(value), 1)
		if err != nil {
			return nil, err
		}
		cloned[name] = copied
	}

	return cloned, nil
}

func cloneClaimValue(value reflect.Value, depth int) (any, error) {
	if !value.IsValid() {
		return nil, nil
	}
	if depth > MaxClaimDepth {
		return nil, fmt.Errorf("%w: claim depth exceeded", ErrInvalidPrincipal)
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return nil, nil
		}
		return cloneClaimValue(value.Elem(), depth)
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return value.Interface(), nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String || value.Len() > MaxClaimCollection {
			return nil, fmt.Errorf("%w: unsupported claim map", ErrInvalidPrincipal)
		}
		cloned := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			copied, err := cloneClaimValue(iterator.Value(), depth+1)
			if err != nil {
				return nil, err
			}
			cloned[iterator.Key().String()] = copied
		}
		return cloned, nil
	case reflect.Slice, reflect.Array:
		if value.Len() > MaxClaimCollection {
			return nil, fmt.Errorf("%w: claim collection too large", ErrInvalidPrincipal)
		}
		cloned := make([]any, value.Len())
		for i := range value.Len() {
			copied, err := cloneClaimValue(value.Index(i), depth+1)
			if err != nil {
				return nil, err
			}
			cloned[i] = copied
		}
		return cloned, nil
	default:
		return nil, fmt.Errorf("%w: unsupported claim value", ErrInvalidPrincipal)
	}
}
