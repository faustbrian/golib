package password_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

func TestStandardLoggingDoesNotExposeHashOrErrorCause(t *testing.T) {
	hash, err := password.ParseEncodedHash(passwordtest.LaravelArgon2id, password.DefaultPolicy().Limits())
	if err != nil {
		t.Fatal(err)
	}
	service, err := password.NewTestService(password.DefaultPolicy(), errorReader{err: errors.New("sensitive entropy detail")})
	if err != nil {
		t.Fatal(err)
	}
	_, classified := service.Hash(context.Background(), []byte("synthetic"))
	for _, handler := range []func(*bytes.Buffer) slog.Handler{
		func(buffer *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buffer, nil) },
		func(buffer *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buffer, nil) },
	} {
		var output bytes.Buffer
		slog.New(handler(&output)).Info("password operation", "hash", hash, "error", classified)
		rendered := output.String()
		if strings.Contains(rendered, hash.String()) || strings.Contains(rendered, "sensitive entropy detail") {
			t.Fatalf("structured log exposed sensitive value: %s", rendered)
		}
	}
}

func TestHostileHashesAreRejectedBeforePrimitive(t *testing.T) {
	limits := password.DefaultPolicy().Limits()
	tests := []struct {
		name, encoded string
		want          error
	}{
		{"unsupported version", "$argon2id$v=16$m=65536,t=2,p=1$c3ludGhldGlj$c3ludGhldGljLWRpZ2VzdA", password.ErrUnsupportedVersion},
		{"time bomb", "$argon2id$v=19$m=65536,t=999999,p=1$c3ludGhldGlj$c3ludGhldGljLWRpZ2VzdA", password.ErrResourceRejected},
		{"invalid bcrypt alphabet", "$2y$10$" + strings.Repeat("!", 53), password.ErrMalformedHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := password.ParseEncodedHash(tt.encoded, limits)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestHostileHashesAreRejectedBeforeAdmission(t *testing.T) {
	admission, err := password.NewAdmission(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := admission.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	service, err := password.New(password.DefaultPolicy(), password.WithAdmission(admission))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		encoded string
		want    error
	}{
		{"broken", password.ErrMalformedHash},
		{"$argon2id$v=19$m=4294967295,t=4,p=4$c3ludGhldGlj$c3ludGhldGljLWRpZ2VzdA", password.ErrResourceRejected},
	} {
		_, err := service.Verify(context.Background(), []byte("synthetic"), test.encoded)
		if !errors.Is(err, test.want) || errors.Is(err, password.ErrClosed) {
			t.Fatalf("error = %v, want pre-admission %v", err, test.want)
		}
	}
}

func TestServiceAdmissionCancellationIsClassified(t *testing.T) {
	admission, err := password.NewAdmission(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	release, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	svc, err := password.New(password.DefaultPolicy(), password.WithAdmission(admission))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.Hash(ctx, []byte("synthetic"))
	if !errors.Is(err, password.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
