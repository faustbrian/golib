package password_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

func TestNeedsRehashNeverRecommendsDowngrade(t *testing.T) {
	limits := password.DefaultPolicy().Limits()
	weakerArgon, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 32 * 1024, Parallelism: 1, SaltLength: 16, OutputLength: 16}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := password.New(weakerArgon)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Verify(context.Background(), []byte(passwordtest.SyntheticPassword), passwordtest.LaravelArgon2id)
	if err != nil {
		t.Fatal(err)
	}
	if result.NeedsRehash() {
		t.Fatal("stronger Argon2id hash must not be downgraded")
	}

	bcryptPolicy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: 8, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	svc, err = password.New(bcryptPolicy)
	if err != nil {
		t.Fatal(err)
	}
	result, err = svc.Verify(context.Background(), []byte(passwordtest.SyntheticPassword), passwordtest.LaravelBcrypt)
	if err != nil {
		t.Fatal(err)
	}
	if result.NeedsRehash() {
		t.Fatal("higher bcrypt cost must not be downgraded")
	}
	result, err = svc.Verify(context.Background(), []byte(passwordtest.SyntheticPassword), passwordtest.LaravelArgon2id)
	if err != nil {
		t.Fatal(err)
	}
	if result.NeedsRehash() {
		t.Fatal("Argon2id must not be downgraded to bcrypt")
	}
}

func TestNeedsRehashDetectsWeakerArgon2id(t *testing.T) {
	policy := password.DefaultPolicy()
	service, err := password.New(policy)
	if err != nil {
		t.Fatal(err)
	}
	weaker := strings.Replace(passwordtest.LaravelArgon2id, "t=2", "t=1", 1)
	hash, err := password.ParseEncodedHash(weaker, policy.Limits())
	if err != nil {
		t.Fatal(err)
	}
	if !service.NeedsRehash(hash) {
		t.Fatal("weaker Argon2id hash did not require rehash")
	}
}

func TestNeedsRehashDoesNotLowerAnyArgon2idDimension(t *testing.T) {
	service, err := password.New(password.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	encode := func(timeCost, memory, parallelism, saltLength, outputLength int) string {
		salt := base64.RawStdEncoding.EncodeToString(make([]byte, saltLength))
		output := base64.RawStdEncoding.EncodeToString(make([]byte, outputLength))
		return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, timeCost, parallelism, salt, output)
	}
	tests := []struct {
		name    string
		encoded string
	}{
		{"stronger memory", encode(1, 128*1024, 1, 16, 32)},
		{"longer salt", encode(1, 64*1024, 1, 32, 32)},
		{"longer output", encode(1, 64*1024, 1, 16, 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := password.ParseEncodedHash(tt.encoded, service.Policy().Limits())
			if err != nil {
				t.Fatal(err)
			}
			if service.NeedsRehash(hash) {
				t.Fatal("incomparable hash would be partially downgraded")
			}
		})
	}
}

func TestNeedsRehashArgon2idTransitionMatrix(t *testing.T) {
	service, err := password.New(password.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	encode := func(timeCost, memory, parallelism, saltLength, outputLength int) string {
		salt := base64.RawStdEncoding.EncodeToString(make([]byte, saltLength))
		output := base64.RawStdEncoding.EncodeToString(make([]byte, outputLength))
		return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, timeCost, parallelism, salt, output)
	}
	tests := []struct {
		name        string
		encoded     string
		needsRehash bool
	}{
		{"equal", encode(2, 64*1024, 1, 16, 32), false},
		{"lower time", encode(1, 64*1024, 1, 16, 32), true},
		{"lower memory", encode(2, 32*1024, 1, 16, 32), true},
		{"parallelism change", encode(2, 64*1024, 2, 16, 32), true},
		{"shorter salt", encode(2, 64*1024, 1, 8, 32), true},
		{"shorter output", encode(2, 64*1024, 1, 16, 16), true},
		{"higher time", encode(3, 64*1024, 1, 16, 32), false},
		{"higher memory", encode(2, 128*1024, 1, 16, 32), false},
		{"longer salt", encode(2, 64*1024, 1, 32, 32), false},
		{"longer output", encode(2, 64*1024, 1, 16, 64), false},
		{"parallelism and higher memory", encode(2, 128*1024, 2, 16, 32), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := password.ParseEncodedHash(tt.encoded, service.Policy().Limits())
			if err != nil {
				t.Fatal(err)
			}
			if got := service.NeedsRehash(hash); got != tt.needsRehash {
				t.Fatalf("NeedsRehash = %v, want %v", got, tt.needsRehash)
			}
		})
	}
}

func TestParserRejectsNonCanonicalDecimal(t *testing.T) {
	encoded := strings.Replace(passwordtest.LaravelArgon2id, "m=65536", "m=065536", 1)
	_, err := password.ParseEncodedHash(encoded, password.DefaultPolicy().Limits())
	if !errors.Is(err, password.ErrMalformedHash) {
		t.Fatalf("error = %v", err)
	}
}

func TestAdmissionRejectsAlreadyCanceledContext(t *testing.T) {
	a, err := password.NewAdmission(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = a.Acquire(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if a.Active() != 0 {
		t.Fatalf("active = %d", a.Active())
	}
}
