package password_test

import (
	"errors"
	"fmt"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestPolicyAndEncodedHashContracts(t *testing.T) {
	t.Parallel()

	policy, err := password.NewPolicy(password.PolicyConfig{
		Algorithm: password.Argon2id,
		Argon2id:  password.Argon2idParameters{Version: 19, Time: 2, MemoryKiB: 64 * 1024, Parallelism: 1, SaltLength: 16, OutputLength: 32},
		Limits:    password.Limits{PasswordBytes: 1024, EncodedHashBytes: 512, Argon2Time: 4, MemoryKiB: 128 * 1024, Parallelism: 4, SaltBytes: 64, OutputBytes: 64, BcryptCost: 14, Concurrent: 2, Queue: 1},
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if policy.Algorithm() != password.Argon2id {
		t.Fatalf("Algorithm = %q", policy.Algorithm())
	}

	hash, err := password.ParseEncodedHash("$argon2id$v=19$m=65536,t=2,p=1$c3ludGhldGljLXNhbHQ$RkFLRUZBS0VGQUtFRkFLRUZBS0VGQUtFRkFLRUZBS0VGQUtFRkE", policy.Limits())
	if err != nil {
		t.Fatalf("ParseEncodedHash: %v", err)
	}
	if got := fmt.Sprintf("%v", hash); got != "[password hash redacted]" {
		t.Fatalf("diagnostic leaked: %q", got)
	}
	if hash.String() == "" {
		t.Fatal("String must return persistence encoding")
	}

	_, err = password.NewPolicy(password.PolicyConfig{})
	if !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("error = %v", err)
	}
}
