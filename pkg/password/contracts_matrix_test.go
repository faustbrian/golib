package password_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

func testLimits() password.Limits {
	return password.Limits{PasswordBytes: 1024, EncodedHashBytes: 256, Argon2Time: 4, MemoryKiB: 1024, Parallelism: 4, SaltBytes: 64, OutputBytes: 64, BcryptCost: 14, Concurrent: 2, Queue: 2}
}

func testArgonPolicy(t testing.TB) password.Policy {
	t.Helper()
	policy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: testLimits()})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func testBcryptPolicy(t testing.TB, cost int) password.Policy {
	t.Helper()
	policy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: cost, Limits: testLimits()})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestPolicyValidationMatrix(t *testing.T) {
	validArgon := password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: testLimits()}
	tests := []struct {
		name   string
		mutate func(*password.PolicyConfig)
	}{
		{"password limit", func(c *password.PolicyConfig) { c.Limits.PasswordBytes = 0 }},
		{"encoded limit", func(c *password.PolicyConfig) { c.Limits.EncodedHashBytes = 59 }},
		{"concurrent", func(c *password.PolicyConfig) { c.Limits.Concurrent = 0 }},
		{"queue", func(c *password.PolicyConfig) { c.Limits.Queue = -1 }},
		{"argon version", func(c *password.PolicyConfig) { c.Argon2id.Version = 16 }},
		{"argon time", func(c *password.PolicyConfig) { c.Argon2id.Time = 5 }},
		{"argon memory", func(c *password.PolicyConfig) { c.Argon2id.MemoryKiB = 7 }},
		{"argon parallelism", func(c *password.PolicyConfig) { c.Argon2id.Parallelism = 0 }},
		{"argon salt", func(c *password.PolicyConfig) { c.Argon2id.SaltLength = 7 }},
		{"argon output", func(c *password.PolicyConfig) { c.Argon2id.OutputLength = 15 }},
		{"algorithm", func(c *password.PolicyConfig) { c.Algorithm = "unknown" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validArgon
			tt.mutate(&c)
			_, err := password.NewPolicy(c)
			if !errors.Is(err, password.ErrInvalidPolicy) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	for _, cost := range []int{3, 15} {
		_, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: cost, Limits: testLimits()})
		if !errors.Is(err, password.ErrInvalidPolicy) {
			t.Fatalf("bcrypt cost %d: %v", cost, err)
		}
	}
	argon := testArgonPolicy(t)
	if argon.Argon2idParameters().Time != 1 || argon.BcryptCost() != 0 {
		t.Fatal("policy getters")
	}
	bcryptPolicy := testBcryptPolicy(t, 4)
	if bcryptPolicy.BcryptCost() != 4 {
		t.Fatal("bcrypt cost getter")
	}
}

func TestEncodedHashParserMatrix(t *testing.T) {
	limits := testLimits()
	valid := "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$MDEyMzQ1Njc4OWFiY2RlZg"
	tests := []struct {
		name, encoded string
		want          error
	}{
		{"empty", "", password.ErrMalformedHash},
		{"oversized", strings.Repeat("x", limits.EncodedHashBytes+1), password.ErrResourceRejected},
		{"unsupported algorithm", "$scrypt$v=1$x", password.ErrUnsupportedAlgorithm},
		{"missing marker", "argon2id", password.ErrMalformedHash},
		{"truncated", "$argon2id$v=19$m=8,t=1,p=1$salt", password.ErrMalformedHash},
		{"bad version field", "$argon2id$version=19$m=8,t=1,p=1$MTIzNDU2Nzg$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrMalformedHash},
		{"duplicate field", "$argon2id$v=19$m=8,m=8,t=1,p=1$MTIzNDU2Nzg$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrMalformedHash},
		{"wrong field", "$argon2id$v=19$x=8,t=1,p=1$MTIzNDU2Nzg$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrMalformedHash},
		{"numeric syntax", "$argon2id$v=19$m=8,t=+,p=1$MTIzNDU2Nzg$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrMalformedHash},
		{"numeric overflow", "$argon2id$v=19$m=4294967296,t=1,p=1$MTIzNDU2Nzg$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrResourceRejected},
		{"memory minimum", "$argon2id$v=19$m=7,t=1,p=1$MTIzNDU2Nzg$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrResourceRejected},
		{"bad salt base64", "$argon2id$v=19$m=8,t=1,p=1$!!!!$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrMalformedHash},
		{"short output", "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$MTIz", password.ErrMalformedHash},
		{"excessive salt", "$argon2id$v=19$m=8,t=1,p=1$" + base64.RawStdEncoding.EncodeToString(make([]byte, limits.SaltBytes+1)) + "$MDEyMzQ1Njc4OWFiY2RlZg", password.ErrResourceRejected},
		{"excessive output", "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$" + base64.RawStdEncoding.EncodeToString(make([]byte, limits.OutputBytes+1)), password.ErrResourceRejected},
		{"bcrypt length", "$2y$10$short", password.ErrMalformedHash},
		{"bcrypt cost", "$2y$99$" + strings.Repeat(".", 53), password.ErrResourceRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := password.ParseEncodedHash(tt.encoded, limits)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
	hash, err := password.ParseEncodedHash(valid, limits)
	if err != nil {
		t.Fatal(err)
	}
	if hash.Argon2idParameters().OutputLength != 16 || hash.BcryptCost() != 0 {
		t.Fatal("argon hash getters")
	}
	if hash.GoString() != "password.EncodedHash{redacted}" || fmt.Sprintf("%#v", hash) != "[password hash redacted]" {
		t.Fatalf("diagnostic formatting = %#v", hash)
	}
	bcryptHash, err := password.ParseEncodedHash(passwordtest.LaravelBcrypt, password.DefaultPolicy().Limits())
	if err != nil {
		t.Fatal(err)
	}
	if bcryptHash.BcryptCost() != 10 {
		t.Fatal("bcrypt hash cost")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestServiceFailureAndBoundaryMatrix(t *testing.T) {
	if _, err := password.New(password.Policy{}); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("zero policy: %v", err)
	}
	if _, err := password.New(testArgonPolicy(t), nil); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("nil option: %v", err)
	}
	if _, err := password.New(testArgonPolicy(t), password.WithAdmission(nil)); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("nil admission: %v", err)
	}
	if _, err := password.NewTestService(testArgonPolicy(t), nil); !errors.Is(err, password.ErrEntropy) {
		t.Fatalf("nil entropy: %v", err)
	}
	if _, err := password.NewTestService(password.Policy{}, strings.NewReader("entropy")); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("invalid test policy: %v", err)
	}

	badEntropy, err := password.NewTestService(testArgonPolicy(t), failingReader{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badEntropy.Hash(context.Background(), []byte("secret")); !errors.Is(err, password.ErrEntropy) {
		t.Fatalf("entropy: %v", err)
	}

	argon, err := password.NewTestService(testArgonPolicy(t), bytes.NewReader(bytes.Repeat([]byte{1}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	boundaryPassword := bytes.Repeat([]byte{'x'}, testLimits().PasswordBytes)
	boundaryHash, err := argon.Hash(context.Background(), boundaryPassword)
	if err != nil {
		t.Fatalf("exact password limit hash: %v", err)
	}
	if _, err := argon.Verify(context.Background(), boundaryPassword, boundaryHash.String()); err != nil {
		t.Fatalf("exact password limit verify: %v", err)
	}
	hash, err := argon.Hash(context.Background(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	result, same, err := argon.VerifyAndUpgrade(context.Background(), []byte("secret"), hash.String())
	if err != nil || !result.Match() || same.String() != hash.String() {
		t.Fatalf("no upgrade: %+v %v", result, err)
	}
	if _, _, err := argon.VerifyAndUpgrade(context.Background(), []byte("wrong"), hash.String()); !errors.Is(err, password.ErrMismatch) {
		t.Fatalf("verify failure: %v", err)
	}

	bcryptService, err := password.New(testBcryptPolicy(t, 4))
	if err != nil {
		t.Fatal(err)
	}
	bcryptHash, err := bcryptService.Hash(context.Background(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	result, err = bcryptService.Verify(context.Background(), []byte("secret"), bcryptHash.String())
	if err != nil || !result.Match() || result.NeedsRehash() {
		t.Fatalf("bcrypt verify: %+v %v", result, err)
	}
	maximumBcryptPassword := bytes.Repeat([]byte{'x'}, 72)
	maximumBcryptHash, err := bcryptService.Hash(context.Background(), maximumBcryptPassword)
	if err != nil {
		t.Fatalf("bcrypt exact password limit hash: %v", err)
	}
	if _, err := bcryptService.Verify(context.Background(), maximumBcryptPassword, maximumBcryptHash.String()); err != nil {
		t.Fatalf("bcrypt exact password limit verify: %v", err)
	}
	if _, err := bcryptService.Hash(context.Background(), bytes.Repeat([]byte{'x'}, 73)); !errors.Is(err, password.ErrResourceRejected) {
		t.Fatalf("bcrypt long password: %v", err)
	}
	if _, err := bcryptService.Verify(context.Background(), bytes.Repeat([]byte{'x'}, 73), bcryptHash.String()); !errors.Is(err, password.ErrResourceRejected) {
		t.Fatalf("bcrypt long password verify: %v", err)
	}
	if _, err := bcryptService.Hash(context.Background(), bytes.Repeat([]byte{'x'}, 1025)); !errors.Is(err, password.ErrResourceRejected) {
		t.Fatalf("oversized hash input: %v", err)
	}
	if _, err := bcryptService.Verify(context.Background(), bytes.Repeat([]byte{'x'}, 1025), bcryptHash.String()); !errors.Is(err, password.ErrResourceRejected) {
		t.Fatalf("oversized verify input: %v", err)
	}
	if _, err := bcryptService.Verify(context.Background(), []byte("secret"), "broken"); !errors.Is(err, password.ErrMalformedHash) {
		t.Fatalf("malformed verify: %v", err)
	}

	upgrade, err := password.NewTestService(testArgonPolicy(t), failingReader{})
	if err != nil {
		t.Fatal(err)
	}
	result, replacement, err := upgrade.VerifyAndUpgrade(context.Background(), []byte(passwordtest.SyntheticPassword), passwordtest.LaravelBcrypt)
	if !result.Match() || !result.NeedsRehash() || replacement.String() != "" || !errors.Is(err, password.ErrEntropy) {
		t.Fatalf("failed upgrade: %+v %q %v", result, replacement.String(), err)
	}
}

func TestServiceAdmissionOverflowAndTimeout(t *testing.T) {
	for _, tt := range []struct {
		name    string
		queue   int
		context func() (context.Context, context.CancelFunc)
		want    error
	}{
		{"overflow", 0, func() (context.Context, context.CancelFunc) { return context.Background(), func() {} }, password.ErrAdmission},
		{"timeout", 1, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 10*time.Millisecond)
		}, password.ErrCanceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, err := password.NewAdmission(1, tt.queue)
			if err != nil {
				t.Fatal(err)
			}
			release, err := a.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			svc, err := password.New(testArgonPolicy(t), password.WithAdmission(a))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := tt.context()
			defer cancel()
			_, err = svc.Hash(ctx, []byte("secret"))
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v", err)
			}
			if errors.Is(tt.want, password.ErrCanceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline cause = %v", err)
			}
		})
	}
}

func TestVerifyCancellationAndAdmission(t *testing.T) {
	source, err := password.NewTestService(testArgonPolicy(t), strings.NewReader("12345678"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := source.Hash(context.Background(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Verify(ctx, []byte("secret"), hash.String()); !errors.Is(err, password.ErrCanceled) {
		t.Fatalf("canceled verify: %v", err)
	}
	a, err := password.NewAdmission(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	release, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	blocked, err := password.New(testArgonPolicy(t), password.WithAdmission(a))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blocked.Verify(context.Background(), []byte("secret"), hash.String()); !errors.Is(err, password.ErrAdmission) {
		t.Fatalf("admission verify: %v", err)
	}
}

func TestClassifiedErrorDoesNotExposeCause(t *testing.T) {
	service, err := password.NewTestService(testArgonPolicy(t), errorReader{err: errors.New("sensitive cause")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Hash(context.Background(), []byte("synthetic"))
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("error leaked cause: %v", err)
	}
	var classified *password.Error
	if !errors.As(err, &classified) || !errors.Is(err, password.ErrEntropy) {
		t.Fatal("classification failed")
	}
	if !errors.Is(classified.Kind(), password.ErrEntropy) || classified.Operation() != "read salt" || classified.Cause() == nil {
		t.Fatalf("classified error accessors = %v %q %v", classified.Kind(), classified.Operation(), classified.Cause())
	}
	for _, format := range []string{"%s", "%q", "%v", "%+v", "%#v"} {
		if rendered := fmt.Sprintf(format, err); strings.Contains(rendered, "sensitive") {
			t.Fatalf("format %s leaked cause: %s", format, rendered)
		}
	}
}

func TestClassifiedErrorUnwrapOmitsNilCause(t *testing.T) {
	_, err := password.ParseEncodedHash("broken", testLimits())
	var classified *password.Error
	if !errors.As(err, &classified) {
		t.Fatalf("error = %v", err)
	}
	unwrapped := classified.Unwrap()
	if len(unwrapped) != 1 || !errors.Is(unwrapped[0], password.ErrMalformedHash) {
		t.Fatalf("Unwrap = %#v", unwrapped)
	}
}

func TestPolicyRequiresCrossAlgorithmVerificationLimits(t *testing.T) {
	limits := testLimits()
	invalid := []struct {
		name   string
		mutate func(*password.Limits)
	}{
		{"argon time", func(l *password.Limits) { l.Argon2Time = 0 }},
		{"argon memory", func(l *password.Limits) { l.MemoryKiB = 7 }},
		{"argon parallelism", func(l *password.Limits) { l.Parallelism = 0 }},
		{"argon salt", func(l *password.Limits) { l.SaltBytes = 7 }},
		{"argon output", func(l *password.Limits) { l.OutputBytes = 15 }},
		{"bcrypt minimum", func(l *password.Limits) { l.BcryptCost = 3 }},
		{"bcrypt maximum", func(l *password.Limits) { l.BcryptCost = 32 }},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			candidate := limits
			tt.mutate(&candidate)
			_, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: 4, Limits: candidate})
			if !errors.Is(err, password.ErrInvalidPolicy) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	limits.BcryptCost = 3
	_, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: limits})
	if !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("Argon2id with unusable bcrypt verification limit: %v", err)
	}
	limits = testLimits()
	limits.EncodedHashBytes = 60
	_, err = password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: limits})
	if !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("Argon2id with undersized encoding limit: %v", err)
	}
}

func TestBcryptParserRejectsNonCanonicalBase64(t *testing.T) {
	limits := password.DefaultPolicy().Limits()
	saltAlias := passwordtest.LaravelBcrypt[:28] + "v" + passwordtest.LaravelBcrypt[29:]
	if _, err := password.ParseEncodedHash(saltAlias, limits); !errors.Is(err, password.ErrMalformedHash) {
		t.Fatalf("non-canonical salt error = %v", err)
	}
	digestAlias := passwordtest.LaravelBcrypt[:59] + "n"
	if _, err := password.ParseEncodedHash(digestAlias, limits); !errors.Is(err, password.ErrMalformedHash) {
		t.Fatalf("non-canonical digest error = %v", err)
	}
}
