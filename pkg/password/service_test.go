package password_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestArgon2idHashVerifyAndUpgrade(t *testing.T) {
	policy := password.DefaultPolicy()
	svc, err := password.NewTestService(policy, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("synthetic password")
	original := bytes.Clone(secret)
	hash, err := svc.Hash(context.Background(), secret)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !bytes.Equal(secret, original) {
		t.Fatal("Hash mutated the caller-owned password buffer")
	}
	if _, err := svc.Verify(context.Background(), secret, hash.String()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !bytes.Equal(secret, original) {
		t.Fatal("Verify mutated the caller-owned password buffer")
	}
	secret[0] = 'X'
	result, err := svc.Verify(context.Background(), []byte("synthetic password"), hash.String())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Match() || result.NeedsRehash() {
		t.Fatalf("result = %+v", result)
	}
	result, err = svc.Verify(context.Background(), []byte("wrong"), hash.String())
	if !errors.Is(err, password.ErrMismatch) || result.Match() {
		t.Fatalf("mismatch = %+v, %v", result, err)
	}
}

func TestLaravelBcryptUpgrade(t *testing.T) {
	svc, err := password.New(password.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	const laravel = "$2y$10$ABk0ypUBDSb78zn66THffuHDCkhvUWaMk2g..sQiEEfh1RemSi6vm"
	result, upgraded, err := svc.VerifyAndUpgrade(context.Background(), []byte("synthetic password"), laravel)
	if err != nil {
		t.Fatalf("VerifyAndUpgrade: %v", err)
	}
	if !result.Match() || !result.NeedsRehash() || upgraded.String() == laravel || upgraded.Algorithm() != password.Argon2id {
		t.Fatalf("result=%+v upgraded=%v", result, upgraded)
	}
}
