package goidempotency

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestStoreMapsAtomicAcquireOutcomesToReplayContract(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	for name, test := range map[string]struct {
		outcome  idempotency.Outcome
		recorded bool
	}{
		"acquired":       {outcome: idempotency.OutcomeAcquired, recorded: true},
		"stale takeover": {outcome: idempotency.OutcomeStaleOwnerTakeover, recorded: true},
		"in progress":    {outcome: idempotency.OutcomeInProgress, recorded: false},
		"replayed":       {outcome: idempotency.OutcomeReplayed, recorded: false},
		"conflict":       {outcome: idempotency.OutcomeConflict, recorded: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			backend := &fakeStore{result: idempotency.AcquireResult{Outcome: test.outcome}}
			service, err := idempotency.NewService(backend)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			adapter, err := New(Config{
				Service: service, Namespace: "webhooks", Tenant: "tenant-1",
				Operation: "verify", Caller: "provider", Clock: func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			recorded, err := adapter.CheckAndRecord(context.Background(), "digest", now.Add(5*time.Minute))
			if err != nil {
				t.Fatalf("CheckAndRecord() error = %v", err)
			}
			if recorded != test.recorded {
				t.Fatalf("CheckAndRecord() = %v, want %v", recorded, test.recorded)
			}
			if backend.request.Lease != 5*time.Minute || backend.request.Key.Value() != "digest" ||
				backend.request.Key.Namespace() != "webhooks" || backend.request.Key.Tenant() != "tenant-1" {
				t.Fatalf("AcquireRequest = %#v", backend.request)
			}
		})
	}
}

func TestStoreFailsClosedForBackendAndExpiredLease(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	backendErr := errors.New("database offline")
	backend := &fakeStore{err: backendErr}
	service, err := idempotency.NewService(backend)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	adapter, err := New(Config{
		Service: service, Namespace: "webhooks", Tenant: "tenant",
		Operation: "verify", Caller: "provider", Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := adapter.CheckAndRecord(context.Background(), "digest", now.Add(time.Minute)); !errors.Is(err, backendErr) {
		t.Fatalf("backend error = %v", err)
	}
	if _, err := adapter.CheckAndRecord(context.Background(), "digest", now); !errors.Is(err, ErrInvalidExpiry) {
		t.Fatalf("expiry error = %v, want ErrInvalidExpiry", err)
	}
}

func TestNewValidatesAllScopeComponents(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
	backend := &fakeStore{}
	service, _ := idempotency.NewService(backend)
	if _, err := New(Config{Service: service, Namespace: strings.Repeat("n", idempotency.MaxKeyPartBytes+1), Tenant: "tenant", Operation: "verify", Caller: "provider"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() scope error = %v", err)
	}
	store, err := New(Config{Service: service, Namespace: "webhooks", Tenant: "tenant", Operation: "verify", Caller: "provider"})
	if err != nil {
		t.Fatalf("New() default clock error = %v", err)
	}
	if _, err := store.CheckAndRecord(context.Background(), strings.Repeat("v", idempotency.MaxKeyPartBytes+1), time.Now().Add(time.Minute)); err == nil {
		t.Fatal("CheckAndRecord() accepted an oversized replay key")
	}
}

type fakeStore struct {
	request idempotency.AcquireRequest
	result  idempotency.AcquireResult
	err     error
}

func (s *fakeStore) Acquire(_ context.Context, request idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
	s.request = request

	return s.result, s.err
}

func (*fakeStore) Inspect(context.Context, idempotency.Key) (idempotency.Record, error) {
	return idempotency.Record{}, errors.New("not implemented")
}

func (*fakeStore) Heartbeat(context.Context, idempotency.HeartbeatRequest) (idempotency.Record, error) {
	return idempotency.Record{}, errors.New("not implemented")
}

func (*fakeStore) Complete(context.Context, idempotency.CompleteRequest) (idempotency.Record, error) {
	return idempotency.Record{}, errors.New("not implemented")
}

func (*fakeStore) Fail(context.Context, idempotency.FailRequest) (idempotency.Record, error) {
	return idempotency.Record{}, errors.New("not implemented")
}

func (*fakeStore) Release(context.Context, idempotency.Ownership) (idempotency.Record, error) {
	return idempotency.Record{}, errors.New("not implemented")
}

func (*fakeStore) Expire(context.Context, idempotency.Key) (idempotency.Record, error) {
	return idempotency.Record{}, errors.New("not implemented")
}
