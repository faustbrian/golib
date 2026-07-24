package jwt

import (
	"context"
	"errors"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

func TestConcurrentCloseStateHonorsCompletionAndCancellation(t *testing.T) {
	t.Parallel()

	completed := &Remote{closing: true, closeDone: make(chan struct{})}
	completed.closeErr = errors.New("shutdown failed")
	close(completed.closeDone)
	if err := completed.Close(context.Background()); err != completed.closeErr {
		t.Fatalf("Close(completed) error = %v", err)
	}

	waiting := &Remote{closing: true, closeDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waiting.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close(canceled waiter) error = %v", err)
	}

	closed := &Remote{closed: true}
	if err := closed.closeResult(); err != nil {
		t.Fatalf("closeResult(closed) error = %v", err)
	}
	failed := &Remote{closeErr: context.DeadlineExceeded}
	if err := failed.closeResult(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("closeResult(failed) error = %v", err)
	}

	busy := &Remote{operations: map[uint64]context.CancelFunc{1: func() {}}}
	deadline, deadlineCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer deadlineCancel()
	<-deadline.Done()
	if err := busy.Close(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close(active operation deadline) error = %v", err)
	}
}

func TestKeySetClassifiesUnregisteredCacheLookup(t *testing.T) {
	t.Parallel()

	cache, err := jwk.NewCache(context.Background(), httprc.NewClient())
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	t.Cleanup(func() { _ = cache.Shutdown(context.Background()) })
	remote := &Remote{cache: cache, url: "https://issuer.example.test/unregistered"}
	if _, err := remote.KeySet(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("KeySet(unregistered) error = %v", err)
	}
}
