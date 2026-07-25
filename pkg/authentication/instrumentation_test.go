package authentication_test

import (
	"context"
	"errors"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
)

type instrumenterFunc func(context.Context, authentication.CredentialKind) (context.Context, func(authentication.Event))

func (f instrumenterFunc) Start(ctx context.Context, kind authentication.CredentialKind) (context.Context, func(authentication.Event)) {
	return f(ctx, kind)
}

func TestInstrumentedAuthenticatorReportsBoundedOutcomeMetadata(t *testing.T) {
	t.Parallel()

	clock := authtest.NewClock(authtest.Epoch)
	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{Subject: "service", Method: "bearer"})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	result, err := authentication.NewAuthenticatedResult(principal)
	if err != nil {
		t.Fatalf("NewAuthenticatedResult() error = %v", err)
	}
	var gotKind authentication.CredentialKind
	var gotEvent authentication.Event
	instrumenter := instrumenterFunc(func(ctx context.Context, kind authentication.CredentialKind) (context.Context, func(authentication.Event)) {
		gotKind = kind
		return context.WithValue(ctx, instrumentationContextKey{}, "instrumented"), func(event authentication.Event) {
			gotEvent = event
		}
	})
	authenticator, err := authentication.NewInstrumented(authenticatorFunc(func(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
		if ctx.Value(instrumentationContextKey{}) != "instrumented" {
			t.Fatal("authenticator did not receive instrumented context")
		}
		clock.Advance(25 * time.Millisecond)
		return result, nil
	}), instrumenter, clock)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}

	if _, err := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("secret-token")); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if gotKind != authentication.CredentialBearer || gotEvent.Outcome != authentication.OutcomeAuthenticated ||
		gotEvent.Duration != 25*time.Millisecond || gotEvent.Failure != "" {
		t.Fatalf("instrumentation = kind %q event %#v", gotKind, gotEvent)
	}
}

func TestInstrumentedAuthenticatorClassifiesFailureWithoutChangingIt(t *testing.T) {
	t.Parallel()

	want := authentication.NewFailure(authentication.FailureRejected,
		authentication.WithFailureCause(errors.New("secret-token")),
	)
	var event authentication.Event
	authenticator, err := authentication.NewInstrumented(
		authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			return authentication.Result{}, want
		}),
		instrumenterFunc(func(ctx context.Context, _ authentication.CredentialKind) (context.Context, func(authentication.Event)) {
			return ctx, func(got authentication.Event) { event = got }
		}),
		authtest.NewClock(authtest.Epoch),
	)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	_, got := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("secret-token"))
	if got != want {
		t.Fatalf("Authenticate() error = %v, want original %v", got, want)
	}
	if event.Outcome != authentication.OutcomeFailed || event.Failure != authentication.FailureRejected {
		t.Fatalf("event = %#v", event)
	}
}

func TestInstrumentationCannotBreakAuthentication(t *testing.T) {
	t.Parallel()

	result := authtest.Result(t, authentication.PrincipalSpec{Subject: "service", Method: "bearer"})
	tests := []struct {
		name         string
		instrumenter authentication.Instrumenter
	}{
		{name: "start panic", instrumenter: instrumenterFunc(func(context.Context, authentication.CredentialKind) (context.Context, func(authentication.Event)) {
			panic("instrumentation failed")
		})},
		{name: "finish panic", instrumenter: instrumenterFunc(func(ctx context.Context, _ authentication.CredentialKind) (context.Context, func(authentication.Event)) {
			return ctx, func(authentication.Event) { panic("instrumentation failed") }
		})},
		{name: "nil context and finish", instrumenter: instrumenterFunc(func(context.Context, authentication.CredentialKind) (context.Context, func(authentication.Event)) {
			return nil, nil
		})},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			authenticator, err := authentication.NewInstrumented(
				authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) { return result, nil }),
				tt.instrumenter, authtest.NewClock(authtest.Epoch),
			)
			if err != nil {
				t.Fatalf("NewInstrumented() error = %v", err)
			}
			if _, err := authenticator.Authenticate(context.Background(), authentication.NewBearerCredential("token")); err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
		})
	}
}

func TestInstrumentedRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	authenticator := authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
		return authentication.Result{}, nil
	})
	instrumenter := instrumenterFunc(func(ctx context.Context, _ authentication.CredentialKind) (context.Context, func(authentication.Event)) {
		return ctx, func(authentication.Event) {}
	})
	if _, err := authentication.NewInstrumented(nil, instrumenter, authtest.NewClock(authtest.Epoch)); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewInstrumented(nil authenticator) error = %v", err)
	}
	if _, err := authentication.NewInstrumented(authenticator, nil, authtest.NewClock(authtest.Epoch)); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewInstrumented(nil instrumenter) error = %v", err)
	}
	if _, err := authentication.NewInstrumented(authenticator, instrumenter, nil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewInstrumented(nil clock) error = %v", err)
	}
	var typedNil *panicClock
	if _, err := authentication.NewInstrumented(authenticator, instrumenter, typedNil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewInstrumented(typed nil clock) error = %v", err)
	}
}

func TestInstrumentedReportsAnonymousAndUnclassifiedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		result      authentication.Result
		err         error
		wantOutcome authentication.Outcome
	}{
		{name: "anonymous", result: authentication.AnonymousResult(), wantOutcome: authentication.OutcomeAnonymous},
		{name: "unclassified failure", err: errors.New("provider failed"), wantOutcome: authentication.OutcomeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var event authentication.Event
			authenticator, err := authentication.NewInstrumented(
				authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
					return tt.result, tt.err
				}),
				instrumenterFunc(func(ctx context.Context, _ authentication.CredentialKind) (context.Context, func(authentication.Event)) {
					return ctx, func(got authentication.Event) { event = got }
				}),
				valueClock{now: authtest.Epoch},
			)
			if err != nil {
				t.Fatalf("NewInstrumented() error = %v", err)
			}
			_, gotErr := authenticator.Authenticate(context.Background(), nil)
			if gotErr != tt.err || event.Outcome != tt.wantOutcome || event.Failure != "" {
				t.Fatalf("Authenticate() = error %v, event %#v", gotErr, event)
			}
		})
	}
}

func TestInstrumentationContainsPanickingInputsAndClocks(t *testing.T) {
	t.Parallel()

	var kind authentication.CredentialKind
	var event authentication.Event
	authenticator, err := authentication.NewInstrumented(
		authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			return authentication.AnonymousResult(), nil
		}),
		instrumenterFunc(func(ctx context.Context, got authentication.CredentialKind) (context.Context, func(authentication.Event)) {
			kind = got
			return ctx, func(got authentication.Event) { event = got }
		}),
		panicClock{},
	)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), panicCredential{}); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if kind != "" || event.Duration != 0 {
		t.Fatalf("instrumentation = kind %q event %#v", kind, event)
	}

	clock := &reverseClock{times: []time.Time{authtest.Epoch.Add(time.Second), authtest.Epoch}}
	authenticator, err = authentication.NewInstrumented(
		authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			return authentication.AnonymousResult(), nil
		}),
		instrumenterFunc(func(ctx context.Context, _ authentication.CredentialKind) (context.Context, func(authentication.Event)) {
			return ctx, func(got authentication.Event) { event = got }
		}),
		clock,
	)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	_, _ = authenticator.Authenticate(context.Background(), nil)
	if event.Duration != 0 {
		t.Fatalf("negative duration was reported: %v", event.Duration)
	}
}

type valueClock struct{ now time.Time }

func (c valueClock) Now() time.Time { return c.now }

type panicClock struct{}

func (panicClock) Now() time.Time { panic("clock failed") }

type reverseClock struct{ times []time.Time }

func (c *reverseClock) Now() time.Time {
	now := c.times[0]
	c.times = c.times[1:]
	return now
}

type panicCredential struct{}

func (panicCredential) Kind() authentication.CredentialKind { panic("credential failed") }
func (panicCredential) String() string                      { return "credential [REDACTED]" }

type instrumentationContextKey struct{}
