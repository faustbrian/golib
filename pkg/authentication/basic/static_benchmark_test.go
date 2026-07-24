package basic_test

import (
	"context"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/basic"
)

func BenchmarkStaticAuthenticate(b *testing.B) {
	authenticator, err := basic.NewStatic([]basic.Entry{{
		Username: "service", Password: "password",
		Principal: authentication.PrincipalSpec{Subject: "service"},
	}})
	if err != nil {
		b.Fatalf("NewStatic() error = %v", err)
	}
	credential := authentication.NewBasicCredential("service", "password")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := authenticator.Authenticate(context.Background(), credential); err != nil {
			b.Fatal(err)
		}
	}
}
