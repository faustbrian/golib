package password_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/argon2id"
	passwordbcrypt "github.com/faustbrian/golib/pkg/password/bcrypt"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

func TestAlgorithmAdaptersAndPHPFixtures(t *testing.T) {
	argonService, err := argon2id.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	result, err := argonService.Verify(context.Background(), []byte(passwordtest.SyntheticPassword), passwordtest.LaravelArgon2id)
	if err != nil || !result.Match() {
		t.Fatalf("argon PHP fixture: %+v, %v", result, err)
	}

	limits := password.DefaultPolicy().Limits()
	bcryptService, err := passwordbcrypt.New(10, limits)
	if err != nil {
		t.Fatal(err)
	}
	result, err = bcryptService.Verify(context.Background(), []byte(passwordtest.SyntheticPassword), passwordtest.LaravelBcrypt)
	if err != nil || !result.Match() {
		t.Fatalf("bcrypt PHP fixture: %+v, %v", result, err)
	}
}

func TestRootContracts(_ *testing.T) {
	var _ password.HasherVerifier = (*password.Service)(nil)
}

func TestAdapterValidationAndDeterministicEntropy(t *testing.T) {
	limits := password.DefaultPolicy().Limits()
	parameters := password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}
	if _, err := argon2id.New(parameters, limits); err != nil {
		t.Fatal(err)
	}
	parameters.Version = 16
	if _, err := argon2id.New(parameters, limits); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("argon validation: %v", err)
	}
	if _, err := passwordbcrypt.New(3, limits); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("bcrypt validation: %v", err)
	}
	if _, err := passwordtest.NewEntropy(nil); err == nil {
		t.Fatal("empty deterministic entropy accepted")
	}
	seed := []byte{1, 2}
	entropy, err := passwordtest.NewEntropy(seed)
	if err != nil {
		t.Fatal(err)
	}
	seed[0] = 9
	got := make([]byte, 5)
	if _, err := entropy.Read(got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{1, 2, 1, 2, 1}) {
		t.Fatalf("entropy = %v", got)
	}
	if _, err := passwordtest.NewService(testArgonPolicy(t), nil); err == nil {
		t.Fatal("empty seed accepted")
	}
	if _, err := passwordtest.NewService(testArgonPolicy(t), []byte{7}); err != nil {
		t.Fatal(err)
	}
}
