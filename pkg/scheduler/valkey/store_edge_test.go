package valkey

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

func TestStoreMutationValidationAndCancellation(t *testing.T) {
	t.Parallel()

	store, _ := newStore(&fakeExecutor{}, "scheduler")
	invalid := lease.Lease{Key: "key", Owner: "owner"}
	if _, err := store.Heartbeat(context.Background(), invalid, 0, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Heartbeat(invalid) error = %v", err)
	}
	if _, err := store.Inspect(context.Background(), ""); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Inspect(empty) error = %v", err)
	}
	if err := store.Release(context.Background(), lease.Lease{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Release(invalid) error = %v", err)
	}
	if err := store.Recover(context.Background(), "key", 0); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Recover(zero) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	owned := lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}
	if _, err := store.Heartbeat(ctx, owned, time.Minute, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Heartbeat(canceled) error = %v", err)
	}
	if _, err := store.Inspect(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Inspect(canceled) error = %v", err)
	}
	if err := store.Release(ctx, owned); !errors.Is(err, context.Canceled) {
		t.Fatalf("Release(canceled) error = %v", err)
	}
	if err := store.Recover(ctx, "key", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recover(canceled) error = %v", err)
	}
	if err := store.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check(canceled) error = %v", err)
	}
}

func TestStoreDecodingFailures(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"shape":    {"ok"},
		"token":    {"ok", "key", "owner", "bad", "1", "2"},
		"acquired": {"ok", "key", "owner", "1", "bad", "2"},
		"expires":  {"ok", "key", "owner", "1", "1", "bad"},
		"semantic": {"error", "future"},
	}
	for name, reply := range tests {
		t.Run(name, func(t *testing.T) {
			store, _ := newStore(&fakeExecutor{replies: [][]string{reply}}, "scheduler")
			if _, err := store.Inspect(context.Background(), "key"); err == nil {
				t.Fatal("Inspect() error = nil")
			}
		})
	}
}

func TestStoreMutationRepliesAndBackendErrors(t *testing.T) {
	t.Parallel()

	backend := errors.New("backend")
	owned := lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}
	tests := []struct {
		reply []string
		err   error
		want  error
	}{
		{reply: []string{"error", "not_found"}, want: lease.ErrNotFound},
		{reply: []string{"error", "stale_owner"}, want: lease.ErrStaleOwner},
		{reply: []string{"unexpected"}, want: errors.New("malformed")},
		{err: backend, want: backend},
	}
	for _, test := range tests {
		executor := &fakeExecutor{replies: [][]string{test.reply}, errors: []error{test.err}}
		store, _ := newStore(executor, "scheduler")
		err := store.Release(context.Background(), owned)
		if errors.Is(test.want, backend) {
			if !errors.Is(err, backend) {
				t.Fatalf("Release() error = %v", err)
			}
		} else if errors.Is(test.want, lease.ErrNotFound) || errors.Is(test.want, lease.ErrStaleOwner) {
			if !errors.Is(err, test.want) {
				t.Fatalf("Release() error = %v, want %v", err, test.want)
			}
		} else if err == nil {
			t.Fatal("Release(malformed) error = nil")
		}
	}
}

func TestStoreHeartbeatInspectRecoverAndCheck(t *testing.T) {
	t.Parallel()

	leasing := []string{"ok", "key", "owner", "1", "1767225600000", "1767225660000"}
	executor := &fakeExecutor{replies: [][]string{leasing, leasing, {"ok"}}}
	store, _ := newStore(executor, "scheduler")
	owned := lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}
	if _, err := store.Heartbeat(context.Background(), owned, time.Minute, time.Time{}); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if _, err := store.Inspect(context.Background(), "key"); err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if err := store.Recover(context.Background(), "key", 1); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	checkErr := errors.New("check")
	checked, _ := newStore(&fakeExecutor{check: checkErr}, "scheduler")
	if !errors.Is(checked.Check(context.Background()), checkErr) {
		t.Fatal("Check() did not propagate error")
	}
}
