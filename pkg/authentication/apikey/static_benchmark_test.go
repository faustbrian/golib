package apikey_test

import (
	"context"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/apikey"
)

func BenchmarkStaticAuthenticate(b *testing.B) {
	authenticator, err := apikey.NewStatic([]apikey.Entry{{
		ID: "primary", Key: "secret",
		Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	if err != nil {
		b.Fatalf("NewStatic() error = %v", err)
	}
	credential := authentication.NewAPIKeyCredential("primary", "secret")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := authenticator.Authenticate(context.Background(), credential); err != nil {
			b.Fatal(err)
		}
	}
}
