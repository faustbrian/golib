package password_test

import (
	"context"
	"errors"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

func FuzzParseEncodedHash(f *testing.F) {
	for _, seed := range []string{
		passwordtest.LaravelArgon2id,
		passwordtest.LaravelBcrypt,
		"$argon2id$v=19$m=1,t=1,p=1$x$x",
		"$argon2id$v=19$m=4294967296,t=1,p=1$c29tZXNhbHQ$ZVrRXqxlLcWfcXCnMyv0m4Rpvh/bnCi7",
		"$argon2id$v=19$m=8,m=8,t=1,p=1$x$x",
		"$argon2id$v=19$m=8,t=1,p=1$x",
		"$2y$99$.....................................................",
	} {
		f.Add(seed)
	}
	limits := password.DefaultPolicy().Limits()
	f.Fuzz(func(t *testing.T, encoded string) {
		hash, err := password.ParseEncodedHash(encoded, limits)
		if err != nil {
			return
		}
		if hash.String() != encoded {
			t.Fatal("successful parse did not round trip")
		}
		if hash.Algorithm() != password.Argon2id && hash.Algorithm() != password.Bcrypt {
			t.Fatalf("unexpected algorithm %q", hash.Algorithm())
		}
	})
}

func FuzzBoundedVerify(f *testing.F) {
	for _, seed := range []string{passwordtest.LaravelBcrypt, "$argon2id$v=19$m=64,t=1,p=1$c29tZXNhbHQ$ZVrRXqxlLcWfcXCnMyv0m4Rpvh/bnCi7", "", "$argon2id$"} {
		f.Add(seed)
	}
	limits := testLimits()
	limits.MemoryKiB = 64
	limits.Argon2Time = 2
	limits.Parallelism = 2
	limits.BcryptCost = 4
	policy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: limits})
	if err != nil {
		f.Fatal(err)
	}
	svc, err := password.New(policy)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		_, err := svc.Verify(context.Background(), []byte("synthetic fuzz password"), encoded)
		if err == nil {
			return
		}
		if !errors.Is(err, password.ErrMismatch) && !errors.Is(err, password.ErrMalformedHash) && !errors.Is(err, password.ErrUnsupportedAlgorithm) && !errors.Is(err, password.ErrUnsupportedVersion) && !errors.Is(err, password.ErrResourceRejected) {
			t.Fatalf("unclassified error: %v", err)
		}
	})
}
