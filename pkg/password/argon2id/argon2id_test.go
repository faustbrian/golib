package argon2id

import (
	"errors"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestConstructors(t *testing.T) {
	limits := password.DefaultPolicy().Limits()
	parameters := password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}
	service, err := New(parameters, limits)
	if err != nil || service == nil {
		t.Fatalf("New: %v", err)
	}
	parameters.Version = 16
	if _, err := New(parameters, limits); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("invalid New error = %v", err)
	}
	service, err = NewDefault()
	if err != nil || service == nil {
		t.Fatalf("NewDefault: %v", err)
	}
}
