package passwordauth_test

import (
	"context"
	"fmt"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordauth"
)

type exampleLookup struct{ hash string }

func (l exampleLookup) LookupPassword(context.Context, string) (passwordauth.Record, bool, error) {
	return passwordauth.Record{Subject: "user-123", EncodedHash: l.hash}, true, nil
}

func ExampleAuthenticator() {
	limits := password.DefaultPolicy().Limits()
	bcryptPolicy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: 4, Limits: limits})
	if err != nil {
		panic(err)
	}
	bcryptService, err := password.New(bcryptPolicy)
	if err != nil {
		panic(err)
	}
	current, err := bcryptService.Hash(context.Background(), []byte("synthetic example password"))
	if err != nil {
		panic(err)
	}
	dummy, err := bcryptService.Hash(context.Background(), []byte("synthetic dummy password"))
	if err != nil {
		panic(err)
	}
	passwords, err := password.New(password.DefaultPolicy())
	if err != nil {
		panic(err)
	}
	authenticator, err := passwordauth.New(passwordauth.Config{Passwords: passwords, Lookup: exampleLookup{hash: current.String()}, DummyHash: dummy.String()})
	if err != nil {
		panic(err)
	}
	result, err := authenticator.Authenticate(context.Background(), "user", []byte("synthetic example password"))
	fmt.Println(err == nil, result.Subject(), result.Upgrade().Required())
	// Output: true user-123 true
}
