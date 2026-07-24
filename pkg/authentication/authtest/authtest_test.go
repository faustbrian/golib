package authtest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
)

func TestPrincipalAndResultFixturesAreDeterministic(t *testing.T) {
	t.Parallel()

	principal := authtest.Principal(t, authentication.PrincipalSpec{Subject: "service", Method: "test"})
	if principal.Subject() != "service" || !principal.AuthenticatedAt().Equal(authtest.Epoch) {
		t.Fatalf("Principal() = subject %q at %v", principal.Subject(), principal.AuthenticatedAt())
	}
	result := authtest.Result(t, authentication.PrincipalSpec{Subject: "service", Method: "test"})
	if got, ok := result.Principal(); !ok || got.Subject() != "service" {
		t.Fatalf("Result().Principal() = (%v, %v)", got, ok)
	}
}

func TestFixedClockAdvancesSafely(t *testing.T) {
	t.Parallel()

	clock := authtest.NewClock(authtest.Epoch)
	var group sync.WaitGroup
	for i := 0; i < 10; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			clock.Advance(time.Second)
			_ = clock.Now()
		}()
	}
	group.Wait()
	if got := clock.Now(); !got.Equal(authtest.Epoch.Add(10 * time.Second)) {
		t.Fatalf("Clock.Now() = %v", got)
	}
}

func TestScriptedAuthenticatorReturnsOutcomesAndRedactedCalls(t *testing.T) {
	t.Parallel()

	success := authtest.Result(t, authentication.PrincipalSpec{Subject: "service", Method: "bearer"})
	authenticator := authtest.NewAuthenticator(
		authtest.Outcome{Err: authentication.NewFailure(authentication.FailureRejected)},
		authtest.Outcome{Result: success},
	)
	credential := authentication.NewBearerCredential("secret-token")
	if _, err := authenticator.Authenticate(context.Background(), credential); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("first Authenticate() error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), credential); err != nil {
		t.Fatalf("second Authenticate() error = %v", err)
	}
	if _, err := authenticator.Authenticate(context.Background(), credential); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("exhausted Authenticate() error = %v", err)
	}
	calls := authenticator.Calls()
	if len(calls) != 3 || calls[0].Kind != authentication.CredentialBearer {
		t.Fatalf("Calls() = %#v", calls)
	}
	if strings.Contains(calls[0].String(), "secret-token") {
		t.Fatalf("Call.String() disclosed token: %q", calls[0].String())
	}
	calls[0].Kind = authentication.CredentialBasic
	if authenticator.Calls()[0].Kind != authentication.CredentialBearer {
		t.Fatal("Calls() exposed internal storage")
	}
}

func TestHTTPFixtureAndAssertions(t *testing.T) {
	t.Parallel()

	fixture := authtest.NewHTTPFixture("POST", "https://service.test/path", strings.NewReader("body"))
	if fixture.Request.Method != "POST" || fixture.Request.URL.Path != "/path" || fixture.Recorder == nil {
		t.Fatalf("NewHTTPFixture() = %#v", fixture)
	}
	principal := authtest.Principal(t, authentication.PrincipalSpec{Subject: "service", Method: "test"})
	ctx := authentication.ContextWithPrincipal(context.Background(), principal)
	authtest.RequirePrincipal(t, ctx, "service")
	authtest.RequireFailure(t, authentication.NewFailure(authentication.FailureInvalid), authentication.FailureInvalid)
	authtest.RequireFailure(t, fmt.Errorf("wrapped: %w", authentication.NewFailure(authentication.FailureRejected)), authentication.FailureRejected)
}

func TestFixturesReportInvalidInputThroughTestingContract(t *testing.T) {
	t.Parallel()

	principalTesting := &recordingTesting{}
	principal := authtest.Principal(principalTesting, authentication.PrincipalSpec{})
	if !principal.IsAnonymous() || len(principalTesting.failures) != 1 {
		t.Fatalf("Principal() = %v, failures = %#v", principal, principalTesting.failures)
	}
	resultTesting := &recordingTesting{}
	result := authtest.Result(resultTesting, authentication.PrincipalSpec{})
	if result.State() != "" || len(resultTesting.failures) != 2 {
		t.Fatalf("Result() = %v, failures = %#v", result, resultTesting.failures)
	}
}

func TestScriptedAuthenticatorRecordsNilAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	authenticator := authtest.NewAuthenticator(authtest.Outcome{Result: authentication.AnonymousResult()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authenticator.Authenticate(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
	calls := authenticator.Calls()
	if len(calls) != 1 || calls[0].Kind != "" {
		t.Fatalf("Calls() = %#v", calls)
	}
}

func TestAssertionsReportMismatchesThroughTestingContract(t *testing.T) {
	t.Parallel()

	principalTesting := &recordingTesting{}
	authtest.RequirePrincipal(principalTesting, context.Background(), "service")
	if len(principalTesting.failures) != 1 {
		t.Fatalf("RequirePrincipal() failures = %#v", principalTesting.failures)
	}
	failureTesting := &recordingTesting{}
	authtest.RequireFailure(failureTesting, errors.New("wrong"), authentication.FailureRejected)
	if len(failureTesting.failures) != 1 {
		t.Fatalf("RequireFailure() failures = %#v", failureTesting.failures)
	}
}

type recordingTesting struct {
	failures []string
}

func (*recordingTesting) Helper() {}

func (t *recordingTesting) Fatalf(format string, arguments ...any) {
	t.failures = append(t.failures, fmt.Sprintf(format, arguments...))
}
