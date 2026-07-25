package apikey_test

import (
	"context"
	"fmt"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
)

func ExampleStatic_Replace() {
	authenticator, _ := apikey.NewStatic([]apikey.Entry{{
		ID: "previous", Key: "old-secret",
		Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	_ = authenticator.Replace([]apikey.Entry{
		{ID: "current", Key: "new-secret", Principal: authentication.PrincipalSpec{Subject: "service"}},
		{ID: "previous", Key: "old-secret", Principal: authentication.PrincipalSpec{Subject: "service"}},
	})
	result, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewAPIKeyCredential("current", "new-secret"),
	)
	principal, authenticated := result.Principal()
	fmt.Println(err, authenticated, principal.Subject())
	// Output: <nil> true service
}
