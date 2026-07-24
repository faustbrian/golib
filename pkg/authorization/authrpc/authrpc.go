// Package authrpc provides fail-closed jsonrpc authorization middleware.
package authrpc

import (
	"context"
	"encoding/json"
	"errors"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
)

const CodeForbidden = -32001

var (
	ErrNilAuthorizer    = errors.New("JSON-RPC authorization authorizer is nil")
	ErrNilRequestMapper = errors.New("JSON-RPC authorization request mapper is nil")
)

type Authorizer interface {
	Decide(context.Context, authorization.Request) (authorization.Decision, error)
}

type RequestMapper func(context.Context, json.RawMessage) (authorization.Request, error)
type DeniedError func(authorization.Decision) *jsonrpc.Error
type ErrorMapper func(error) *jsonrpc.Error
type Option func(*options)

type options struct {
	denied DeniedError
	failed ErrorMapper
}

func WithDeniedError(mapper DeniedError) Option {
	return func(options *options) {
		if mapper != nil {
			options.denied = mapper
		}
	}
}

func WithErrorMapper(mapper ErrorMapper) Option {
	return func(options *options) {
		if mapper != nil {
			options.failed = mapper
		}
	}
}

func NewMiddleware(
	authorizer Authorizer,
	mapper RequestMapper,
	middlewareOptions ...Option,
) (jsonrpc.Middleware, error) {
	if authorizer == nil {
		return nil, ErrNilAuthorizer
	}
	if mapper == nil {
		return nil, ErrNilRequestMapper
	}
	configured := options{
		denied: func(authorization.Decision) *jsonrpc.Error {
			return jsonrpc.NewError(CodeForbidden, "Forbidden")
		},
		failed: func(err error) *jsonrpc.Error {
			return jsonrpc.InternalError().WithCause(err)
		},
	}
	for _, option := range middlewareOptions {
		option(&configured)
	}

	return func(next jsonrpc.Handler) jsonrpc.Handler {
		return func(ctx context.Context, params json.RawMessage) (any, error) {
			request, err := mapper(ctx, params)
			if err != nil {
				return nil, mapFailure(configured.failed, err)
			}
			decision, err := authorizer.Decide(ctx, request)
			if err != nil {
				return nil, mapFailure(configured.failed, err)
			}
			if decision.Outcome > authorization.Deny {
				return nil, mapFailure(configured.failed, authorization.ErrInvalidOutcome)
			}
			ctx = context.WithValue(ctx, decisionContextKey{}, decision)
			if decision.Outcome != authorization.Allow {
				denied := configured.denied(decision)
				if denied == nil {
					return nil, jsonrpc.InternalError().WithCause(authorization.ErrInvalidOutcome)
				}
				return nil, denied
			}
			return next(ctx, params)
		}
	}, nil
}

type decisionContextKey struct{}

func DecisionFromContext(ctx context.Context) (authorization.Decision, bool) {
	decision, ok := ctx.Value(decisionContextKey{}).(authorization.Decision)
	return decision, ok
}

func mapFailure(mapper ErrorMapper, err error) *jsonrpc.Error {
	mapped := mapper(err)
	if mapped == nil {
		return jsonrpc.InternalError().WithCause(err)
	}
	return mapped
}
