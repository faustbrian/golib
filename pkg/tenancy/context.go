package tenancy

import (
	"context"
	"errors"
)

var (
	// ErrInvalidContext reports a nil context or invalid scope input.
	ErrInvalidContext = errors.New("tenancy: invalid context")
	// ErrScopeRequired reports an operation without explicit scope.
	ErrScopeRequired = errors.New("tenancy: scope required")
	// ErrTenantScopeRequired reports an operation without tenant-bound scope.
	ErrTenantScopeRequired = errors.New("tenancy: tenant scope required")
	// ErrSystemScopeRequired reports an operation without system-wide scope.
	ErrSystemScopeRequired = errors.New("tenancy: system scope required")
	// ErrConflictingScope reports an attempt to replace an existing scope.
	ErrConflictingScope = errors.New("tenancy: conflicting scope")
)

type contextKey struct{}

// WithScope returns a context carrying scope. An existing equal scope is
// retained; any attempt to replace scope fails deterministically. Parent
// cancellation, values, and deadlines are preserved by the returned context.
func WithScope(ctx context.Context, scope Scope) (context.Context, error) {
	if ctx == nil || !scope.Valid() {
		return nil, ErrInvalidContext
	}
	if current, ok := ScopeFromContext(ctx); ok {
		if !current.Equal(scope) {
			return nil, ErrConflictingScope
		}
		return ctx, nil
	}
	return context.WithValue(ctx, contextKey{}, scope), nil
}

// ScopeFromContext retrieves explicit scope without treating absence as valid.
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	if ctx == nil {
		return Scope{}, false
	}
	scope, ok := ctx.Value(contextKey{}).(Scope)
	return scope, ok && scope.Valid()
}

// RequireScope fails closed when ctx has no explicit valid scope.
func RequireScope(ctx context.Context) (Scope, error) {
	scope, ok := ScopeFromContext(ctx)
	if !ok {
		return Scope{}, ErrScopeRequired
	}
	return scope, nil
}

// RequireTenant returns the tenant ID only for tenant-bound scope.
func RequireTenant(ctx context.Context) (TenantID, error) {
	scope, err := RequireScope(ctx)
	if err != nil || scope.Kind() != ScopeTenant {
		return TenantID{}, ErrTenantScopeRequired
	}
	return scope.TenantID(), nil
}

// RequireSystem returns scope only for explicitly capable system-wide work.
func RequireSystem(ctx context.Context) (Scope, error) {
	scope, err := RequireScope(ctx)
	if err != nil || scope.Kind() != ScopeSystem {
		return Scope{}, ErrSystemScopeRequired
	}
	return scope, nil
}

// RequireUnscoped returns scope only for intentionally unscoped work.
func RequireUnscoped(ctx context.Context) (Scope, error) {
	scope, err := RequireScope(ctx)
	if err != nil || scope.Kind() != ScopeUnscoped {
		return Scope{}, ErrScopeRequired
	}
	return scope, nil
}
