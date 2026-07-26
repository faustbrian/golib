package httpauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

type authorizerFunc func(context.Context, authorization.Request) (authorization.Decision, error)

func (authorize authorizerFunc) Decide(
	ctx context.Context,
	request authorization.Request,
) (authorization.Decision, error) {
	return authorize(ctx, request)
}

func TestHandlerAllowsAndExposesDecision(t *testing.T) {
	t.Parallel()

	wantRequest := authorization.Request{Action: "read"}
	wantDecision := authorization.Decision{
		Outcome: authorization.Allow, Reason: "allowed", Revision: 7,
	}
	handler, err := NewHandler(
		authorizerFunc(func(_ context.Context, request authorization.Request) (authorization.Decision, error) {
			if request.Action != wantRequest.Action {
				t.Errorf("mapped action = %q, want read", request.Action)
			}
			return wantDecision, nil
		}),
		func(*http.Request) (authorization.Request, error) { return wantRequest, nil },
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			decision, ok := DecisionFromContext(request.Context())
			if !ok || decision.Outcome != wantDecision.Outcome ||
				decision.Reason != wantDecision.Reason ||
				decision.Revision != wantDecision.Revision {
				t.Errorf("DecisionFromContext() = (%+v, %v)", decision, ok)
			}
			writer.WriteHeader(http.StatusNoContent)
		}),
	)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestHandlerDeniesWithoutCallingNext(t *testing.T) {
	t.Parallel()

	for _, outcome := range []authorization.Outcome{authorization.NotApplicable, authorization.Deny} {
		outcome := outcome
		t.Run(outcomeName(outcome), func(t *testing.T) {
			t.Parallel()
			handler, err := NewHandler(
				authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
					return authorization.Decision{Outcome: outcome}, nil
				}),
				func(*http.Request) (authorization.Request, error) { return authorization.Request{}, nil },
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("next called") }),
			)
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}
		})
	}
}

func TestHandlerFailsClosedOnMapperEvaluatorAndOutcomeErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("failure")
	tests := map[string]struct {
		mapper     RequestMapper
		authorizer Authorizer
	}{
		"mapper": {
			mapper: func(*http.Request) (authorization.Request, error) {
				return authorization.Request{}, want
			},
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				t.Fatal("authorizer called")
				return authorization.Decision{}, nil
			}),
		},
		"evaluator": {
			mapper: func(*http.Request) (authorization.Request, error) { return authorization.Request{}, nil },
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return authorization.Decision{Outcome: authorization.Deny}, want
			}),
		},
		"outcome": {
			mapper: func(*http.Request) (authorization.Request, error) { return authorization.Request{}, nil },
			authorizer: authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return authorization.Decision{Outcome: authorization.Outcome(99)}, nil
			}),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewHandler(tt.authorizer, tt.mapper, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("next called")
			}))
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestHandlerSupportsCustomDenialAndErrorHandlers(t *testing.T) {
	t.Parallel()

	denied := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	})
	failed := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := ErrorFromContext(request.Context()); !ok {
			t.Error("error handler context has no error")
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	})
	for _, test := range []struct {
		decision authorization.Decision
		err      error
		want     int
	}{
		{decision: authorization.Decision{Outcome: authorization.Deny}, want: http.StatusUnauthorized},
		{err: errors.New("failed"), want: http.StatusServiceUnavailable},
	} {
		handler, err := NewHandler(
			authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
				return test.decision, test.err
			}),
			func(*http.Request) (authorization.Request, error) { return authorization.Request{}, nil },
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("next called") }),
			WithDeniedHandler(denied), WithErrorHandler(failed),
		)
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != test.want {
			t.Errorf("status = %d, want %d", recorder.Code, test.want)
		}
	}
}

func TestNewHandlerValidatesDependencies(t *testing.T) {
	t.Parallel()

	authorizer := authorizerFunc(func(context.Context, authorization.Request) (authorization.Decision, error) {
		return authorization.Decision{}, nil
	})
	mapper := func(*http.Request) (authorization.Request, error) { return authorization.Request{}, nil }
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	tests := []struct {
		authorizer Authorizer
		mapper     RequestMapper
		next       http.Handler
		want       error
	}{
		{mapper: mapper, next: next, want: ErrNilAuthorizer},
		{authorizer: authorizer, next: next, want: ErrNilRequestMapper},
		{authorizer: authorizer, mapper: mapper, want: ErrNilNextHandler},
	}
	for _, test := range tests {
		if _, err := NewHandler(test.authorizer, test.mapper, test.next); !errors.Is(err, test.want) {
			t.Errorf("NewHandler() error = %v, want %v", err, test.want)
		}
	}

	handler, err := NewHandler(authorizer, mapper, next, WithDeniedHandler(nil), WithErrorHandler(nil))
	if err != nil || handler == nil {
		t.Errorf("NewHandler(nil options) = (%v, %v), want handler", handler, err)
	}
	if _, ok := DecisionFromContext(context.Background()); ok {
		t.Error("DecisionFromContext(empty) found a decision")
	}
	if _, ok := ErrorFromContext(context.Background()); ok {
		t.Error("ErrorFromContext(empty) found an error")
	}
}

func outcomeName(outcome authorization.Outcome) string {
	if outcome == authorization.Deny {
		return "deny"
	}
	return "not-applicable"
}
