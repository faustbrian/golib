package opensearch

import (
	"context"
	"errors"

	"github.com/faustbrian/golib/pkg/search"
)

// WriteAuthorization is one validated, bounded, single-tenant write intent.
// Accessors return caller-independent copies so a guard cannot alter the
// operation that will be resolved and dispatched.
type WriteAuthorization struct {
	operations []search.WriteOperation
	refresh    search.RefreshPolicy
}

// Operations returns every write in original bulk position order.
func (authorization WriteAuthorization) Operations() []search.WriteOperation {
	return cloneWriteOperations(authorization.operations)
}

// Refresh returns the requested post-write visibility policy.
func (authorization WriteAuthorization) Refresh() search.RefreshPolicy {
	return authorization.refresh
}

// WriteGuard validates every operation against application-owned durable
// current versions or tombstones. Implementations must be concurrency-safe.
type WriteGuard interface {
	AuthorizeWrite(context.Context, WriteAuthorization) error
}

// WriteGuardFunc adapts a function to WriteGuard.
type WriteGuardFunc func(context.Context, WriteAuthorization) error

func (authorize WriteGuardFunc) AuthorizeWrite(ctx context.Context, request WriteAuthorization) error {
	return authorize(ctx, request)
}

func cloneWriteOperations(operations []search.WriteOperation) []search.WriteOperation {
	cloned := make([]search.WriteOperation, len(operations))
	for position, operation := range operations {
		operation.Source = append([]byte(nil), operation.Source...)
		cloned[position] = operation
	}
	return cloned
}

func (c *Client) authorizeWrite(ctx context.Context, operation Operation, operations []search.WriteOperation, refresh search.RefreshPolicy) error {
	if err := ctx.Err(); err != nil {
		return cancelledFailure(operation, err)
	}
	authorization := WriteAuthorization{operations: cloneWriteOperations(operations), refresh: refresh}
	if err := c.search.WriteGuard.AuthorizeWrite(ctx, authorization); err != nil {
		return sanitizedCallbackFailure(operation, ErrWriteDenied, err)
	}
	if err := ctx.Err(); err != nil {
		return cancelledFailure(operation, err)
	}
	return nil
}

func sanitizedCallbackFailure(operation Operation, denied, err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return cancelledFailure(operation, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return cancelledFailure(operation, context.DeadlineExceeded)
	default:
		return denied
	}
}
