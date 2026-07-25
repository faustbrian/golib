package authentication_test

import (
	"context"
	"fmt"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
)

func ExampleAuthenticator_backgroundConsumer() {
	authenticator, _ := apikey.NewStatic([]apikey.Entry{{
		ID: "worker", Key: "secret",
		Principal: authentication.PrincipalSpec{Subject: "invoice-worker"},
	}})
	result, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewAPIKeyCredential("worker", "secret"),
	)
	principal, authenticated := result.Principal()
	fmt.Println(err, authenticated, principal.Subject())
	// Output: <nil> true invoice-worker
}
