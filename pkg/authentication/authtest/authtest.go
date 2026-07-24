// Package authtest provides deterministic authentication fixtures and assertions.
package authtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

// Epoch is the default deterministic authentication time used by fixtures.
var Epoch = time.Unix(0, 0).UTC()

// TestingT is the subset of testing.TB required by fixture assertions.
type TestingT interface {
	Helper()
	Fatalf(string, ...any)
}

// Principal constructs a tested principal and supplies Epoch when no
// authentication time was specified.
func Principal(testing TestingT, spec authentication.PrincipalSpec) authentication.Principal {
	testing.Helper()
	if spec.AuthenticatedAt.IsZero() {
		spec.AuthenticatedAt = Epoch
	}
	principal, err := authentication.NewPrincipal(spec)
	if err != nil {
		testing.Fatalf("authtest.Principal: %v", err)
		return authentication.Principal{}
	}
	return principal
}

// Result constructs a tested authenticated result.
func Result(testing TestingT, spec authentication.PrincipalSpec) authentication.Result {
	testing.Helper()
	result, err := authentication.NewAuthenticatedResult(Principal(testing, spec))
	if err != nil {
		testing.Fatalf("authtest.Result: %v", err)
		return authentication.Result{}
	}
	return result
}

// Clock is a concurrency-safe deterministic clock.
type Clock struct {
	mutex sync.RWMutex
	clock *manual.Clock
}

// NewClock creates a clock at instant.
func NewClock(instant time.Time) *Clock {
	inner, _ := manual.New(instant)
	return &Clock{clock: inner}
}

// Now returns the current deterministic instant.
func (c *Clock) Now() time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.clock.Now()
}

// Advance moves the clock by duration and returns the new instant.
func (c *Clock) Advance(duration time.Duration) time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	inner, _ := manual.New(c.clock.Now().Add(duration))
	c.clock = inner
	return c.clock.Now()
}

// Outcome is one scripted authentication result.
type Outcome struct {
	Result authentication.Result
	Err    error
}

// Call records only non-secret authentication call metadata.
type Call struct{ Kind authentication.CredentialKind }

// String returns a secret-free call description.
func (c Call) String() string { return fmt.Sprintf("authentication call kind=%s", c.Kind) }

// Authenticator returns scripted outcomes in order and records redacted calls.
type Authenticator struct {
	mutex    sync.Mutex
	outcomes []Outcome
	calls    []Call
}

// NewAuthenticator creates a deterministic scripted authenticator.
func NewAuthenticator(outcomes ...Outcome) *Authenticator {
	return &Authenticator{outcomes: append([]Outcome(nil), outcomes...)}
}

// Authenticate records a non-secret call and consumes the next outcome.
func (a *Authenticator) Authenticate(ctx context.Context, credential authentication.Credential) (authentication.Result, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	kind := authentication.CredentialKind("")
	if credential != nil {
		kind = credential.Kind()
	}
	a.calls = append(a.calls, Call{Kind: kind})
	if err := ctx.Err(); err != nil {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(err))
	}
	if len(a.outcomes) == 0 {
		return authentication.Result{}, authentication.NewFailure(authentication.FailureUnavailable,
			authentication.WithFailureCause(authentication.ErrInvalidConfiguration))
	}
	outcome := a.outcomes[0]
	a.outcomes = a.outcomes[1:]
	return outcome.Result, outcome.Err
}

// Calls returns a copy of recorded non-secret call metadata.
func (a *Authenticator) Calls() []Call {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return append([]Call(nil), a.calls...)
}

// HTTPFixture contains a real request and response recorder.
type HTTPFixture struct {
	Request  *http.Request
	Recorder *httptest.ResponseRecorder
}

// NewHTTPFixture creates a deterministic real HTTP fixture.
func NewHTTPFixture(method, target string, body io.Reader) HTTPFixture {
	return HTTPFixture{
		Request:  httptest.NewRequest(method, target, body),
		Recorder: httptest.NewRecorder(),
	}
}

// RequirePrincipal requires an authenticated subject in ctx.
func RequirePrincipal(testing TestingT, ctx context.Context, subject string) {
	testing.Helper()
	principal, ok := authentication.PrincipalFromContext(ctx)
	if !ok || principal.IsAnonymous() || principal.Subject() != subject {
		testing.Fatalf("principal = (subject %q, found %v), want %q", principal.Subject(), ok, subject)
	}
}

// RequireFailure requires a classified authentication failure.
func RequireFailure(testing TestingT, err error, kind authentication.FailureKind) {
	testing.Helper()
	var failure *authentication.Failure
	if !errors.As(err, &failure) || failure.Kind() != kind {
		testing.Fatalf("failure = (%T, %v), want kind %q", err, err, kind)
	}
}

var _ authentication.Authenticator = (*Authenticator)(nil)
