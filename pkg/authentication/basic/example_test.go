package basic_test

import (
	"context"
	"fmt"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/basic"
)

func ExampleNewStatic() {
	authenticator, _ := basic.NewStatic([]basic.Entry{{
		Username: "service", Password: "password",
		Principal: authentication.PrincipalSpec{Subject: "service-account"},
	}})
	result, err := authenticator.Authenticate(
		context.Background(),
		authentication.NewBasicCredential("service", "password"),
	)
	principal, authenticated := result.Principal()
	fmt.Println(err, authenticated, principal.Subject())
	// Output: <nil> true service-account
}
