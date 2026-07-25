package passwordservice_test

import (
	"context"
	"errors"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordservice"
)

func TestLifecycleProvidesServiceCompatibleHooks(t *testing.T) {
	a, err := password.NewAdmission(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := passwordservice.New(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Start(context.Background()); !errors.Is(err, password.ErrClosed) {
		t.Fatalf("restart = %v", err)
	}
	if _, err := passwordservice.New(nil); !errors.Is(err, passwordservice.ErrInvalidConfig) {
		t.Fatalf("nil config = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, err = password.NewAdmission(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err = passwordservice.New(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start = %v", err)
	}
}
