package httpclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maximumPolicyScopeValueBytes = 4096
	maximumPolicyScopeNameBytes  = 64
)

var (
	// ErrInvalidPolicyScope indicates malformed scope state or resolution.
	ErrInvalidPolicyScope = errors.New("invalid HTTP policy scope")
)

// PolicyResource identifies independently scoped shared policy state.
type PolicyResource uint8

const (
	PolicyResourceTransport PolicyResource = iota
	PolicyResourceCookies
	PolicyResourceOAuthTokens
	PolicyResourceCache
	PolicyResourceCoalescing
	PolicyResourceRateLimiter
	PolicyResourceCircuitBreaker
	PolicyResourceMetrics
	policyResourceCount
)

// ScopeDimension identifies one canonical policy-scope component.
type ScopeDimension string

const (
	ScopeOrigin     ScopeDimension = "origin"
	ScopeHost       ScopeDimension = "host"
	ScopeEndpoint   ScopeDimension = "endpoint"
	ScopeCredential ScopeDimension = "credential"
	ScopeTenant     ScopeDimension = "tenant"
	ScopeAccount    ScopeDimension = "account"
)

const customScopePrefix = "custom:"

// PolicyScopeOptions supplies identity and caller-defined scope values. Origin
// and host are always derived from the concrete request URL.
type PolicyScopeOptions struct {
	Endpoint   string
	Credential string
	Tenant     string
	Account    string
	Custom     map[string]string
}

// PolicyScope is an immutable identity scope attached to a request context.
type PolicyScope struct {
	values map[ScopeDimension]string
	valid  bool
}

// NewPolicyScope validates and snapshots identity scope values.
func NewPolicyScope(options PolicyScopeOptions) (PolicyScope, error) {
	values := make(map[ScopeDimension]string, 4+len(options.Custom))
	for dimension, value := range map[ScopeDimension]string{
		ScopeEndpoint: options.Endpoint, ScopeCredential: options.Credential,
		ScopeTenant: options.Tenant, ScopeAccount: options.Account,
	} {
		if value == "" {
			continue
		}
		if !validPolicyScopeValue(value) {
			return PolicyScope{}, fmt.Errorf("%w: scope value is malformed", ErrInvalidPolicyScope)
		}
		values[dimension] = value
	}
	for name, value := range options.Custom {
		dimension, err := CustomScopeDimension(name)
		if err != nil {
			return PolicyScope{}, err
		}
		if value == "" || !validPolicyScopeValue(value) {
			return PolicyScope{}, fmt.Errorf("%w: custom scope value is malformed", ErrInvalidPolicyScope)
		}
		values[dimension] = value
	}
	return PolicyScope{values: values, valid: true}, nil
}

// CustomScopeDimension creates a validated caller-defined dimension.
func CustomScopeDimension(name string) (ScopeDimension, error) {
	if !validPolicyScopeName(name) {
		return "", fmt.Errorf("%w: custom dimension name is malformed", ErrInvalidPolicyScope)
	}
	return ScopeDimension(customScopePrefix + strings.ToLower(name)), nil
}

// WithPolicyScope attaches an immutable scope snapshot to ctx.
func WithPolicyScope(ctx context.Context, scope PolicyScope) (context.Context, error) {
	if ctx == nil || !scope.valid {
		return nil, fmt.Errorf("%w: context or scope is invalid", ErrInvalidPolicyScope)
	}
	return context.WithValue(ctx, policyScopeContextKey{}, clonePolicyScope(scope)), nil
}

// PolicyScopeFromContext returns an independent scope snapshot.
func PolicyScopeFromContext(ctx context.Context) (PolicyScope, bool) {
	if ctx == nil {
		return PolicyScope{}, false
	}
	scope, ok := ctx.Value(policyScopeContextKey{}).(PolicyScope)
	if !ok || !scope.valid {
		return PolicyScope{}, false
	}
	return clonePolicyScope(scope), true
}

// PolicyScopeKey is an opaque stable resource key. String never renders raw
// scope values.
type PolicyScopeKey struct {
	resource   PolicyResource
	dimensions []ScopeDimension
	digest     [sha256.Size]byte
}

// String returns the versioned opaque scope key.
func (key PolicyScopeKey) String() string {
	return "scope:v1:" + hex.EncodeToString(key.digest[:])
}

// Resource returns the independently scoped resource.
func (key PolicyScopeKey) Resource() PolicyResource { return key.resource }

// Dimensions returns an independent ordered provenance snapshot.
func (key PolicyScopeKey) Dimensions() []ScopeDimension {
	return append([]ScopeDimension(nil), key.dimensions...)
}

// ResolvePolicyScope resolves a request to an opaque resource key. Empty
// dimensions select secure resource-specific defaults.
func ResolvePolicyScope(
	request *http.Request,
	resource PolicyResource,
	dimensions ...ScopeDimension,
) (PolicyScopeKey, error) {
	if request == nil || request.URL == nil || resource >= policyResourceCount {
		return PolicyScopeKey{}, fmt.Errorf("%w: request or resource is invalid", ErrInvalidPolicyScope)
	}
	if len(dimensions) == 0 {
		dimensions = defaultScopeDimensions(resource)
	} else {
		dimensions = append([]ScopeDimension(nil), dimensions...)
	}
	scope, _ := PolicyScopeFromContext(request.Context())
	seen := make(map[ScopeDimension]struct{}, len(dimensions))
	hash := sha256.New()
	writeScopeMaterial(hash, "resource", strconv.Itoa(int(resource)))
	for _, dimension := range dimensions {
		if !validScopeDimension(dimension) {
			return PolicyScopeKey{}, fmt.Errorf("%w: dimension is invalid", ErrInvalidPolicyScope)
		}
		if _, duplicate := seen[dimension]; duplicate {
			return PolicyScopeKey{}, fmt.Errorf("%w: dimension is duplicated", ErrInvalidPolicyScope)
		}
		seen[dimension] = struct{}{}
		value, err := resolveScopeDimension(request, scope, dimension)
		if err != nil {
			return PolicyScopeKey{}, err
		}
		writeScopeMaterial(hash, string(dimension), value)
	}

	key := PolicyScopeKey{resource: resource, dimensions: dimensions}
	copy(key.digest[:], hash.Sum(nil))
	return key, nil
}

func resolveScopeDimension(
	request *http.Request,
	scope PolicyScope,
	dimension ScopeDimension,
) (string, error) {
	switch dimension {
	case ScopeOrigin:
		origin, err := canonicalOrigin(request.URL)
		if err != nil {
			return "", fmt.Errorf("%w: request origin is invalid", ErrInvalidPolicyScope)
		}
		return origin, nil
	case ScopeHost:
		if request.URL.Hostname() == "" {
			return "", fmt.Errorf("%w: request host is invalid", ErrInvalidPolicyScope)
		}
		return strings.ToLower(request.URL.Hostname()), nil
	case ScopeCredential:
		if value := scope.values[dimension]; value != "" {
			return value, nil
		}
		return requestCredentialScope(request.Header), nil
	case ScopeEndpoint, ScopeTenant, ScopeAccount:
		return scopeValueOrUnset(scope, dimension), nil
	default:
		if strings.HasPrefix(string(dimension), customScopePrefix) {
			return scopeValueOrUnset(scope, dimension), nil
		}
	}
	return "", fmt.Errorf("%w: dimension is invalid", ErrInvalidPolicyScope)
}

func requestCredentialScope(header http.Header) string {
	hash := sha256.New()
	names := []string{"Authorization", "Cookie", "Proxy-Authorization"}
	for _, name := range names {
		values := append([]string(nil), header.Values(name)...)
		sort.Strings(values)
		for _, value := range values {
			writeScopeMaterial(hash, name, value)
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func scopeValueOrUnset(scope PolicyScope, dimension ScopeDimension) string {
	if value := scope.values[dimension]; value != "" {
		return value
	}
	return "<unset>"
}

func defaultScopeDimensions(resource PolicyResource) []ScopeDimension {
	switch resource {
	case PolicyResourceTransport:
		return []ScopeDimension{ScopeOrigin}
	case PolicyResourceMetrics:
		return []ScopeDimension{ScopeOrigin, ScopeEndpoint}
	case PolicyResourceCookies, PolicyResourceOAuthTokens, PolicyResourceCache,
		PolicyResourceCoalescing, PolicyResourceRateLimiter:
		return []ScopeDimension{ScopeOrigin, ScopeCredential, ScopeTenant, ScopeAccount}
	case PolicyResourceCircuitBreaker:
		return []ScopeDimension{
			ScopeOrigin, ScopeEndpoint, ScopeCredential, ScopeTenant, ScopeAccount,
		}
	default:
		return nil
	}
}

func validScopeDimension(dimension ScopeDimension) bool {
	switch dimension {
	case ScopeOrigin, ScopeHost, ScopeEndpoint, ScopeCredential, ScopeTenant, ScopeAccount:
		return true
	default:
		name := strings.TrimPrefix(string(dimension), customScopePrefix)
		return name != string(dimension) && validPolicyScopeName(name)
	}
}

func validPolicyScopeName(name string) bool {
	if name == "" || len(name) > maximumPolicyScopeNameBytes {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func validPolicyScopeValue(value string) bool {
	if !utf8.ValidString(value) || len(value) > maximumPolicyScopeValueBytes {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func clonePolicyScope(scope PolicyScope) PolicyScope {
	clone := PolicyScope{values: make(map[ScopeDimension]string, len(scope.values)), valid: scope.valid}
	for dimension, value := range scope.values {
		clone.values[dimension] = value
	}
	return clone
}

func writeScopeMaterial(writer interface{ Write([]byte) (int, error) }, name string, value string) {
	_, _ = fmt.Fprintf(writer, "%d:%s%d:%s", len(name), name, len(value), value)
}

type policyScopeContextKey struct{}
