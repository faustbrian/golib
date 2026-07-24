package password_test

import (
	"context"
	"errors"
	"testing"
	"time"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestAdmissionShutdownWakesQueueAndDrainsWithinContext(t *testing.T) {
	a, err := password.NewAdmission(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	release, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waiter := make(chan error, 1)
	go func() { _, err := a.Acquire(context.Background()); waiter <- err }()
	deadline := time.Now().Add(time.Second)
	for a.Queued() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := a.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v", err)
	}
	if err := <-waiter; !errors.Is(err, password.ErrClosed) {
		t.Fatalf("waiter = %v", err)
	}
	if !a.Closed() {
		t.Fatal("admission remains open")
	}
	if _, err := a.Acquire(context.Background()); !errors.Is(err, password.ErrClosed) {
		t.Fatalf("new acquire = %v", err)
	}
	release()
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("drain = %v", err)
	}
}
