package bearer_test

import (
	"context"
	"fmt"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/bearer"
)

func ExampleNew() {
	authenticator, _ := bearer.New(bearer.ValidatorFunc(
		func(_ context.Context, token string) (authentication.Principal, error) {
			if token != "opaque-token" {
				return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
			}
			return authentication.NewPrincipal(authentication.PrincipalSpec{
				Subject: "service", Method: "bearer",
			})
		},
	))
	result, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewBearerCredential("opaque-token"),
	)
	principal, authenticated := result.Principal()
	fmt.Println(err, authenticated, principal.Subject())
	// Output: <nil> true service
}
