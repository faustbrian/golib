// Package authhttp provides the canonical net/http authorization adapter.
package authhttp

import (
	"context"
	"net/http"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/httpauth"
)

type Authorizer = httpauth.Authorizer
type RequestMapper = httpauth.RequestMapper
type Option = httpauth.Option

var (
	WithDeniedHandler = httpauth.WithDeniedHandler
	WithErrorHandler  = httpauth.WithErrorHandler
)

func NewHandler(
	authorizer Authorizer,
	mapper RequestMapper,
	next http.Handler,
	options ...Option,
) (http.Handler, error) {
	return httpauth.NewHandler(authorizer, mapper, next, options...)
}

func DecisionFromContext(ctx context.Context) (authorization.Decision, bool) {
	return httpauth.DecisionFromContext(ctx)
}

func ErrorFromContext(ctx context.Context) (error, bool) {
	return httpauth.ErrorFromContext(ctx)
}
