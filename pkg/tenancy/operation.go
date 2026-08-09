package tenancy

import (
	"context"
	"errors"
)

// ErrInvalidOperation reports a nil scoped callback.
var ErrInvalidOperation = errors.New("tenancy: invalid operation")

// RunScoped invokes operation synchronously with explicit immutable scope. It
// derives from ctx, so cancellation, deadlines, and caller values are retained.
func RunScoped(ctx context.Context, scope Scope, operation func(context.Context) error) error {
	if operation == nil {
		return ErrInvalidOperation
	}
	scoped, err := WithScope(ctx, scope)
	if err != nil {
		return err
	}
	return operation(scoped)
}
