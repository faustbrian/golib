// Package httpauth provides fail-closed net/http authorization integration.
package httpauth

import (
	"context"
	"errors"
	"net/http"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

var (
	ErrNilAuthorizer    = errors.New("HTTP authorization authorizer is nil")
	ErrNilRequestMapper = errors.New("HTTP authorization request mapper is nil")
	ErrNilNextHandler   = errors.New("HTTP authorization next handler is nil")
)

type Authorizer interface {
	Decide(context.Context, authorization.Request) (authorization.Decision, error)
}

type RequestMapper func(*http.Request) (authorization.Request, error)

type Option func(*options)

type options struct {
	denied http.Handler
	failed http.Handler
}

func WithDeniedHandler(handler http.Handler) Option {
	return func(options *options) {
		if handler != nil {
			options.denied = handler
		}
	}
}

func WithErrorHandler(handler http.Handler) Option {
	return func(options *options) {
		if handler != nil {
			options.failed = handler
		}
	}
}

// NewHandler maps each HTTP request, evaluates it, and invokes next only for
// explicit allow decisions. Mapper and evaluation failures use the error
// handler; deny and not-applicable outcomes use the denial handler.
func NewHandler(
	authorizer Authorizer,
	mapper RequestMapper,
	next http.Handler,
	handlerOptions ...Option,
) (http.Handler, error) {
	if authorizer == nil {
		return nil, ErrNilAuthorizer
	}
	if mapper == nil {
		return nil, ErrNilRequestMapper
	}
	if next == nil {
		return nil, ErrNilNextHandler
	}
	configured := options{
		denied: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusForbidden)
		}),
		failed: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusInternalServerError)
		}),
	}
	for _, option := range handlerOptions {
		option(&configured)
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorizationRequest, err := mapper(request)
		if err != nil {
			configured.failed.ServeHTTP(writer, request.WithContext(withError(request.Context(), err)))
			return
		}
		decision, err := authorizer.Decide(request.Context(), authorizationRequest)
		if err != nil {
			configured.failed.ServeHTTP(writer, request.WithContext(withError(request.Context(), err)))
			return
		}
		if decision.Outcome > authorization.Deny {
			configured.failed.ServeHTTP(
				writer,
				request.WithContext(withError(request.Context(), authorization.ErrInvalidOutcome)),
			)
			return
		}
		request = request.WithContext(context.WithValue(
			request.Context(), decisionContextKey{}, decision,
		))
		if decision.Outcome != authorization.Allow {
			configured.denied.ServeHTTP(writer, request)
			return
		}
		next.ServeHTTP(writer, request)
	}), nil
}

type decisionContextKey struct{}
type errorContextKey struct{}

func DecisionFromContext(ctx context.Context) (authorization.Decision, bool) {
	decision, ok := ctx.Value(decisionContextKey{}).(authorization.Decision)
	return decision, ok
}

func ErrorFromContext(ctx context.Context) (error, bool) {
	err, ok := ctx.Value(errorContextKey{}).(error)
	return err, ok
}

func withError(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, errorContextKey{}, err)
}
