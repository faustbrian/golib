package password_test

import (
	"context"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
)

func BenchmarkApprovedArgon2id(b *testing.B) {
	svc, err := password.New(password.DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	secret := []byte("synthetic benchmark password")
	hash, err := svc.Hash(context.Background(), secret)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("hash", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := svc.Hash(context.Background(), secret); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("verify", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := svc.Verify(context.Background(), secret, hash.String()); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkApprovedBcrypt(b *testing.B) {
	policy := testBcryptPolicy(b, 10)
	svc, err := password.New(policy)
	if err != nil {
		b.Fatal(err)
	}
	secret := []byte("synthetic benchmark password")
	hash, err := svc.Hash(context.Background(), secret)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("hash", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := svc.Hash(context.Background(), secret); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("verify", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := svc.Verify(context.Background(), secret, hash.String()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
