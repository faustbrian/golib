package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/jackc/pgx/v5"
)

func TestPostgresStoreConformance(t *testing.T) {
	executor := newFakeExecutor()
	idempotencytest.RunStoreConformance(t, func(t testing.TB) idempotencytest.StoreFixture {
		key, err := idempotency.NewKey("postgres", "tenant", "operation", "caller", t.Name())
		if err != nil {
			t.Fatalf("NewKey() error = %v", err)
		}
		fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
		if err != nil {
			t.Fatalf("NewFingerprint() error = %v", err)
		}
		store, err := newStore(executor, Options{
			Retention:   time.Hour,
			OwnerTokens: idempotencytest.NewTokenSource("postgres-owner").Next,
		})
		if err != nil {
			t.Fatalf("newStore() error = %v", err)
		}
		return idempotencytest.StoreFixture{
			Store: store, Key: key, Fingerprint: fingerprint,
			Advance: executor.advance,
		}
	})
}

func TestStoreUsesBackendClockForLeaseAuthority(t *testing.T) {
	executor := newFakeExecutor()
	executor.now = time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	store, err := newStore(executor, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("clock-owner").Next,
	})
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	key, fingerprint := storeIdentity(t, t.Name())
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired.Record.CreatedAt.Equal(executor.now) ||
		!acquired.Record.LeaseExpiresAt.Equal(executor.now.Add(time.Minute)) {
		t.Fatalf("Acquire() timestamps = %#v, backend clock = %v", acquired.Record, executor.now)
	}

	executor.advance(time.Minute)
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(),
	}); err == nil {
		t.Fatal("Complete() at backend lease boundary error = nil")
	} else {
		var semantic *idempotency.Error
		if !errors.As(err, &semantic) || semantic.Reason != idempotency.ReasonLeaseExpired {
			t.Fatalf("Complete() error = %v", err)
		}
	}
	takeover, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil || takeover.Outcome != idempotency.OutcomeStaleOwnerTakeover {
		t.Fatalf("Acquire() at backend lease boundary = %#v, %v", takeover, err)
	}
}

func TestStoreValidatesOptionsAndNativePool(t *testing.T) {
	tests := map[string]Options{
		"retention": {},
		"retention too long": {
			Retention:   maxRetention + time.Second,
			OwnerTokens: func() (string, error) { return "owner", nil },
		},
		"tokens": {Retention: time.Hour},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := newStore(newFakeExecutor(), options); err == nil {
				t.Fatal("newStore() error = nil")
			}
		})
	}
	if _, err := New(nil, Options{
		Retention: time.Hour, OwnerTokens: func() (string, error) { return "owner", nil },
	}); err == nil {
		t.Fatal("New() nil pool error = nil")
	}
}

func TestCompleteTxRequiresTransactionAndValidReplayData(t *testing.T) {
	store, err := newStore(newFakeExecutor(), Options{
		Retention:   time.Hour,
		OwnerTokens: func() (string, error) { return "owner", nil },
	})
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	if _, err := store.CompleteTx(
		context.Background(), nil, idempotency.CompleteRequest{},
	); err == nil {
		t.Fatal("CompleteTx() nil transaction error = nil")
	}
	if _, err := store.CompleteTx(context.Background(), nil, idempotency.CompleteRequest{
		Result: make([]byte, idempotency.MaxResultBytes+1),
	}); err == nil {
		t.Fatal("CompleteTx() oversized result error = nil")
	}
	transaction := &fakeTransaction{
		rows: []pgx.Row{
			lockRow(time.Unix(1_700_000_000, 0).UTC()), errorRow(pgx.ErrNoRows),
		},
	}
	if _, err := store.completeInTransaction(
		context.Background(), transaction, idempotency.CompleteRequest{},
	); err == nil {
		t.Fatal("completeInTransaction() missing record error = nil")
	}
}

func TestStorePropagatesExecutorAndTokenFailures(t *testing.T) {
	backendErr := errors.New("postgres unavailable")
	executor := newFakeExecutor()
	executor.err = backendErr
	store, err := newStore(executor, Options{
		Retention: time.Hour, OwnerTokens: func() (string, error) { return "owner", nil },
	})
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	key, fingerprint := storeIdentity(t, "failure")
	if _, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	}); !errors.Is(err, backendErr) {
		t.Fatalf("Acquire() error = %v", err)
	}

	executor.err = nil
	store.ownerTokens = func() (string, error) { return "", backendErr }
	if _, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	}); err == nil {
		t.Fatal("Acquire() token error = nil")
	}
}

func TestCleanupValidatesBatchAndDelegates(t *testing.T) {
	executor := newFakeExecutor()
	store, err := newStore(executor, Options{
		Retention: time.Hour, OwnerTokens: func() (string, error) { return "owner", nil },
	})
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	if _, err := store.Cleanup(context.Background(), 0); err == nil {
		t.Fatal("Cleanup() invalid batch error = nil")
	}
	executor.cleanupCount = 7
	count, err := store.Cleanup(context.Background(), 10)
	if err != nil || count != 7 {
		t.Fatalf("Cleanup() = %d, %v", count, err)
	}
}

func TestStoreRejectsMissingInactiveExpiredAndOversizedTransitions(t *testing.T) {
	executor := newFakeExecutor()
	store, err := newStore(executor, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("transition-owner").Next,
	})
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	key, fingerprint := storeIdentity(t, "transitions")
	missing := idempotency.Ownership{Key: key, OwnerToken: "missing", FencingToken: 1}
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: missing,
	}); err == nil {
		t.Fatal("Complete() missing error = nil")
	}
	if _, err := store.Expire(context.Background(), key); err == nil {
		t.Fatal("Expire() missing error = nil")
	}
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err := store.Expire(context.Background(), key); err == nil {
		t.Fatal("Expire() unexpired error = nil")
	}
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(),
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(),
	}); err == nil {
		t.Fatal("Complete() inactive error = nil")
	}

	expiredKey, expiredFingerprint := storeIdentity(t, "lease-expired")
	expired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: expiredKey, Fingerprint: expiredFingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() expired error = %v", err)
	}
	executor.advance(time.Minute)
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: expired.Record.Ownership(),
	}); err == nil {
		t.Fatal("Complete() expired lease error = nil")
	}
	if _, err := store.Fail(context.Background(), idempotency.FailRequest{
		Ownership: expired.Record.Ownership(), Result: make([]byte, idempotency.MaxResultBytes+1),
	}); err == nil {
		t.Fatal("Fail() oversized error = nil")
	}
}

func storeIdentity(t *testing.T, value string) (idempotency.Key, idempotency.Fingerprint) {
	t.Helper()
	key, err := idempotency.NewKey("postgres", "tenant", "operation", "caller", value)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	return key, fingerprint
}

type fakePersistedRecord struct {
	record  idempotency.Record
	purgeAt time.Time
}

type fakeExecutor struct {
	mu           sync.Mutex
	now          time.Time
	records      map[string]fakePersistedRecord
	err          error
	cleanupCount int64
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		now: time.Unix(1_700_000_000, 0).UTC(), records: make(map[string]fakePersistedRecord),
	}
}

func (e *fakeExecutor) withRecord(
	ctx context.Context,
	digest []byte,
	mutate recordMutation,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	key := string(digest)
	persisted, exists := e.records[key]
	if exists && !e.now.Before(persisted.purgeAt) {
		delete(e.records, key)
		exists = false
	}
	var current *idempotency.Record
	if exists {
		record := cloneRecord(persisted.record)
		current = &record
	}
	next, purgeAt, write, err := mutate(e.now, current)
	if err != nil {
		return err
	}
	if write {
		e.records[key] = fakePersistedRecord{record: cloneRecord(next), purgeAt: purgeAt}
	}
	return nil
}

func (e *fakeExecutor) cleanup(context.Context, int) (int64, error) {
	if e.err != nil {
		return 0, e.err
	}
	return e.cleanupCount, nil
}

func (e *fakeExecutor) advance(duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = e.now.Add(duration)
}
