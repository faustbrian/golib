package password_test

import (
	"context"
	"errors"
	"testing"
	"time"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestAdmissionBoundsActiveAndQueuedOperations(t *testing.T) {
	a, err := password.NewAdmission(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	release, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		r, err := a.Acquire(ctx)
		if err == nil {
			r()
		}
		queued <- err
	}()
	deadline := time.Now().Add(time.Second)
	for a.Queued() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := a.Acquire(context.Background()); !errors.Is(err, password.ErrAdmission) {
		t.Fatalf("overflow = %v", err)
	}
	release()
	if err := <-queued; err != nil {
		t.Fatalf("queued acquire: %v", err)
	}
}

func TestAdmissionRejectsInvalidBounds(t *testing.T) {
	for _, limits := range [][2]int{{0, 0}, {1, -1}} {
		if _, err := password.NewAdmission(limits[0], limits[1]); !errors.Is(err, password.ErrInvalidPolicy) {
			t.Fatalf("NewAdmission%v = %v", limits, err)
		}
	}
}
