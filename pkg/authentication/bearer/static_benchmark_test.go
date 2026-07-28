package bearer_test

import (
	"context"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/bearer"
)

func BenchmarkStaticAuthenticate(b *testing.B) {
	authenticator, err := bearer.NewStatic([]bearer.Entry{
		{Token: "current", Principal: authentication.PrincipalSpec{Subject: "service"}},
		{Token: "previous", Principal: authentication.PrincipalSpec{Subject: "service"}},
	})
	if err != nil {
		b.Fatalf("NewStatic() error = %v", err)
	}
	credential := authentication.NewBearerCredential("previous")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := authenticator.Authenticate(context.Background(), credential); err != nil {
			b.Fatal(err)
		}
	}
}
