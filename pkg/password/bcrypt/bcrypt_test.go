package bcrypt

import (
	"errors"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestNew(t *testing.T) {
	limits := password.DefaultPolicy().Limits()
	service, err := New(4, limits)
	if err != nil || service == nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := New(3, limits); !errors.Is(err, password.ErrInvalidPolicy) {
		t.Fatalf("invalid New error = %v", err)
	}
}
