package valkey

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

type call struct {
	operation  operation
	leaseKey   string
	counterKey string
	args       []string
}

type fakeExecutor struct {
	replies [][]string
	errors  []error
	calls   []call
	check   error
}

func (executor *fakeExecutor) Exec(
	_ context.Context,
	op operation,
	leaseKey string,
	counterKey string,
	args []string,
) ([]string, error) {
	executor.calls = append(executor.calls, call{op, leaseKey, counterKey, append([]string(nil), args...)})
	index := len(executor.calls) - 1
	if index < len(executor.errors) && executor.errors[index] != nil {
		return nil, executor.errors[index]
	}
	return executor.replies[index], nil
}

func (executor *fakeExecutor) Check(context.Context) error { return executor.check }

func TestStoreDecodesLeaseLifecycle(t *testing.T) {
	t.Parallel()

	acquiredAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	expiresAt := acquiredAt.Add(time.Minute)
	executor := &fakeExecutor{replies: [][]string{
		{"ok", "task:report", "replica-a", "4", "1767225600000", "1767225660000"},
		{"ok", "task:report", "replica-a", "4", "1767225600000", "1767225720000"},
		{"ok", "task:report", "replica-a", "4", "1767225600000", "1767225720000"},
		{"ok"},
		{"ok"},
	}}
	store, err := newStore(executor, "scheduler")
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}

	owned, err := store.Acquire(context.Background(), "task:report", "replica-a", time.Minute, time.Time{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if owned.FencingToken != 4 || !owned.AcquiredAt.Equal(acquiredAt) || !owned.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("Acquire() = %+v", owned)
	}
	heartbeat, err := store.Heartbeat(context.Background(), owned, 2*time.Minute, time.Time{})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if !heartbeat.ExpiresAt.Equal(acquiredAt.Add(2 * time.Minute)) {
		t.Fatalf("Heartbeat().ExpiresAt = %v", heartbeat.ExpiresAt)
	}
	if _, err := store.Inspect(context.Background(), owned.Key); err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if err := store.Release(context.Background(), owned); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := store.Recover(context.Background(), owned.Key, owned.FencingToken); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	if got := executor.calls[0]; got.leaseKey == got.counterKey || got.args[0] != "replica-a" || got.args[1] != "60000" {
		t.Fatalf("Acquire call = %+v", got)
	}
}

func TestStoreMapsSemanticAndBackendErrors(t *testing.T) {
	t.Parallel()

	backend := errors.New("valkey unavailable")
	tests := map[string]struct {
		reply []string
		err   error
		want  error
	}{
		"held":      {reply: []string{"error", "held"}, want: lease.ErrHeld},
		"not found": {reply: []string{"error", "not_found"}, want: lease.ErrNotFound},
		"stale":     {reply: []string{"error", "stale_owner"}, want: lease.ErrStaleOwner},
		"backend":   {err: backend, want: backend},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			executor := &fakeExecutor{replies: [][]string{test.reply}, errors: []error{test.err}}
			store, _ := newStore(executor, "scheduler")
			_, err := store.Acquire(context.Background(), "key", "owner", time.Minute, time.Time{})
			if !errors.Is(err, test.want) {
				t.Fatalf("Acquire() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStoreValidatesConfigurationAndInputs(t *testing.T) {
	t.Parallel()

	if _, err := newStore(nil, "scheduler"); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("newStore(nil) error = %v", err)
	}
	for _, prefix := range []string{"", "bad{slot}", string(make([]byte, 65))} {
		if _, err := newStore(&fakeExecutor{}, prefix); !errors.Is(err, lease.ErrInvalid) {
			t.Fatalf("newStore(prefix %q) error = %v", prefix, err)
		}
	}
	store, _ := newStore(&fakeExecutor{}, "scheduler")
	if _, err := store.Acquire(context.Background(), "", "owner", time.Minute, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Acquire(empty key) error = %v", err)
	}
	if _, err := store.Acquire(context.Background(), "key", "", time.Minute, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Acquire(empty owner) error = %v", err)
	}
	if _, err := store.Acquire(context.Background(), "key", "owner", 0, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Acquire(zero TTL) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Acquire(ctx, "key", "owner", time.Minute, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire(canceled) error = %v", err)
	}
}

func TestStoreCapabilitiesAndCheck(t *testing.T) {
	t.Parallel()

	checkErr := errors.New("unsafe backend")
	store, _ := newStore(&fakeExecutor{check: checkErr}, "scheduler")
	if !errors.Is(store.Check(context.Background()), checkErr) {
		t.Fatal("Check() did not return backend error")
	}
	capabilities := store.Capabilities()
	if !capabilities.Persistent || !capabilities.Fencing || !capabilities.Heartbeat || !capabilities.CompareAndDelete || !capabilities.ManualRecovery {
		t.Fatalf("Capabilities() = %+v", capabilities)
	}
}
