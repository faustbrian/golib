package idempotency

import "context"

type ownershipContextKey struct{}

// WithOwnership adds the elected owner and fencing token to a handler context.
func WithOwnership(ctx context.Context, ownership Ownership) context.Context {
	return context.WithValue(ctx, ownershipContextKey{}, ownership)
}

// OwnershipFromContext returns the elected handler ownership, when present.
func OwnershipFromContext(ctx context.Context) (Ownership, bool) {
	ownership, found := ctx.Value(ownershipContextKey{}).(Ownership)
	return ownership, found
}
