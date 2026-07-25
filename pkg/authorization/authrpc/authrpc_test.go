package authrpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
)

type authorizerFunc func(context.Context, authorization.Request) (authorization.Decision, error)

func (authorize authorizerFunc) Decide(ctx context.Context, request authorization.Request) (authorization.Decision, error) {
	return authorize(ctx, request)
}

func TestMiddlewareAllowsAndExposesDecision(t *testing.T) {
	t.Parallel()

	middleware, err := NewMiddleware(
		authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
			return authorization.Decision{Outcome: authorization.Allow, Revision: 4}, nil
		}),
		func(context.Context, json.RawMessage) (authorization.Request, error) {
			return authorization.Request{Action: "read"}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	called := false
	result, err := middleware(func(ctx context.Context, _ json.RawMessage) (any, error) {
		called = true
		decision, ok := DecisionFromContext(ctx)
		if !ok || decision.Revision != 4 {
			t.Errorf("DecisionFromContext() = (%+v, %v)", decision, ok)
		}
		return "ok", nil
	})(context.Background(), json.RawMessage(`{}`))
	if err != nil || result != "ok" || !called {
		t.Fatalf("authorized handler = (%v, %v), called %v", result, err, called)
	}
}

func TestMiddlewareMapsDenialsAndFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("failed")
	tests := map[string]struct {
		mapper     RequestMapper
		authorizer Authorizer
		wantCode   int
	}{
		"mapper": {
			mapper: func(context.Context, json.RawMessage) (authorization.Request, error) {
				return authorization.Request{}, want
			},
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return authorization.Decision{}, nil
			}),
			wantCode: jsonrpc.CodeInternalError,
		},
		"evaluation": {
			mapper: func(context.Context, json.RawMessage) (authorization.Request, error) {
				return authorization.Request{}, nil
			},
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return authorization.Decision{}, want
			}),
			wantCode: jsonrpc.CodeInternalError,
		},
		"deny": {
			mapper: func(context.Context, json.RawMessage) (authorization.Request, error) {
				return authorization.Request{}, nil
			},
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return authorization.Decision{Outcome: authorization.Deny}, nil
			}),
			wantCode: CodeForbidden,
		},
		"not applicable": {
			mapper: func(context.Context, json.RawMessage) (authorization.Request, error) {
				return authorization.Request{}, nil
			},
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return authorization.Decision{Outcome: authorization.NotApplicable}, nil
			}),
			wantCode: CodeForbidden,
		},
		"invalid outcome": {
			mapper: func(context.Context, json.RawMessage) (authorization.Request, error) {
				return authorization.Request{}, nil
			},
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return authorization.Decision{Outcome: 99}, nil
			}),
			wantCode: jsonrpc.CodeInternalError,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			middleware, err := NewMiddleware(test.authorizer, test.mapper)
			if err != nil {
				t.Fatalf("NewMiddleware() error = %v", err)
			}
			_, err = middleware(func(context.Context, json.RawMessage) (any, error) {
				t.Error("next called")
				return nil, nil
			})(context.Background(), nil)
			var rpcError *jsonrpc.Error
			if !errors.As(err, &rpcError) || rpcError.Code != test.wantCode {
				t.Errorf("middleware error = %v, want code %d", err, test.wantCode)
			}
		})
	}
}

func TestMiddlewareOptionsAndValidation(t *testing.T) {
	t.Parallel()

	authorizer := authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{Outcome: authorization.Deny}, nil
	})
	mapper := func(context.Context, json.RawMessage) (authorization.Request, error) {
		return authorization.Request{}, nil
	}
	if _, err := NewMiddleware(nil, mapper); !errors.Is(err, ErrNilAuthorizer) {
		t.Errorf("NewMiddleware(nil authorizer) error = %v", err)
	}
	if _, err := NewMiddleware(authorizer, nil); !errors.Is(err, ErrNilRequestMapper) {
		t.Errorf("NewMiddleware(nil mapper) error = %v", err)
	}
	middleware, err := NewMiddleware(
		authorizer, mapper,
		WithDeniedError(func(authorization.Decision) *jsonrpc.Error { return jsonrpc.NewError(-32042, "No") }),
		WithErrorMapper(func(error) *jsonrpc.Error { return jsonrpc.NewError(-32043, "Failed") }),
		WithDeniedError(nil), WithErrorMapper(nil),
	)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	_, err = middleware(func(context.Context, json.RawMessage) (any, error) { return nil, nil })(context.Background(), nil)
	var rpcError *jsonrpc.Error
	if !errors.As(err, &rpcError) || rpcError.Code != -32042 {
		t.Errorf("custom denied error = %v", err)
	}
	if _, ok := DecisionFromContext(context.Background()); ok {
		t.Error("DecisionFromContext(empty) found decision")
	}

	nilDenied, err := NewMiddleware(
		authorizer, mapper,
		WithDeniedError(func(authorization.Decision) *jsonrpc.Error { return nil }),
	)
	if err != nil {
		t.Fatalf("NewMiddleware(nil denial result) error = %v", err)
	}
	_, err = nilDenied(func(context.Context, json.RawMessage) (any, error) { return nil, nil })(context.Background(), nil)
	if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc.CodeInternalError {
		t.Errorf("nil denied mapping error = %v", err)
	}

	nilFailure, err := NewMiddleware(
		authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
			return authorization.Decision{}, errors.New("failed")
		}),
		mapper,
		WithErrorMapper(func(error) *jsonrpc.Error { return nil }),
	)
	if err != nil {
		t.Fatalf("NewMiddleware(nil failure result) error = %v", err)
	}
	_, err = nilFailure(func(context.Context, json.RawMessage) (any, error) { return nil, nil })(context.Background(), nil)
	if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc.CodeInternalError {
		t.Errorf("nil failure mapping error = %v", err)
	}
}
