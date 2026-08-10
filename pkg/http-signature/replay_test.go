package httpsignature

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryReplayStoreAtomicallyConsumesNonceUntilExpiration(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 2,
		MaxTTL:   10 * time.Minute,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewMemoryReplayStore() error = %v", err)
	}

	record := ReplayRecord{KeyID: "key-1", Nonce: "nonce-1", ExpiresAt: now.Add(time.Minute)}
	if err := store.Consume(context.Background(), record); err != nil {
		t.Fatalf("first Consume() error = %v", err)
	}
	if err := store.Consume(context.Background(), record); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("second Consume() error = %v, want ErrReplayDetected", err)
	}

	now = now.Add(2 * time.Minute)
	record.ExpiresAt = now.Add(time.Minute)
	if err := store.Consume(context.Background(), record); err != nil {
		t.Fatalf("Consume() after expiration error = %v", err)
	}
}

func TestMemoryReplayStoreIsBoundedAndRejectsInvalidRecords(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 1,
		MaxTTL:   time.Minute,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewMemoryReplayStore() error = %v", err)
	}

	for _, record := range []ReplayRecord{
		{},
		{KeyID: "key", Nonce: "nonce", ExpiresAt: now},
		{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute + time.Nanosecond)},
	} {
		if err := store.Consume(context.Background(), record); !errors.Is(err, ErrInvalidReplayRecord) {
			t.Fatalf("Consume(%#v) error = %v, want ErrInvalidReplayRecord", record, err)
		}
	}

	first := ReplayRecord{KeyID: "key", Nonce: "one", ExpiresAt: now.Add(time.Minute)}
	second := ReplayRecord{KeyID: "key", Nonce: "two", ExpiresAt: now.Add(time.Minute)}
	if err := store.Consume(context.Background(), first); err != nil {
		t.Fatalf("first Consume() error = %v", err)
	}
	if err := store.Consume(context.Background(), second); !errors.Is(err, ErrReplayCapacity) {
		t.Fatalf("bounded Consume() error = %v, want ErrReplayCapacity", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Consume(cancelled, second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Consume() error = %v, want context.Canceled", err)
	}
}

func TestMemoryReplayStoreRejectsEachIndependentRecordBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{Capacity: 8, MaxTTL: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	valid := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}
	for _, mutate := range []func(*ReplayRecord){
		func(record *ReplayRecord) { record.KeyID = "" },
		func(record *ReplayRecord) { record.Nonce = "" },
		func(record *ReplayRecord) { record.ExpiresAt = time.Time{} },
		func(record *ReplayRecord) { record.ExpiresAt = now },
		func(record *ReplayRecord) { record.ExpiresAt = now.Add(time.Minute + time.Nanosecond) },
	} {
		record := valid
		mutate(&record)
		if err := store.Consume(context.Background(), record); !errors.Is(err, ErrInvalidReplayRecord) {
			t.Fatalf("Consume(%#v) error = %v, want ErrInvalidReplayRecord", record, err)
		}
	}
	if err := store.Consume(context.Background(), valid); err != nil {
		t.Fatalf("exact maximum TTL error = %v", err)
	}
}

func TestMemoryReplayStoreAllowsExactlyOneConcurrentConsumer(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 1,
		MaxTTL:   time.Minute,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewMemoryReplayStore() error = %v", err)
	}

	record := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}
	var accepted atomic.Int32
	var unexpected atomic.Int32
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := store.Consume(context.Background(), record)
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, ErrReplayDetected):
			default:
				unexpected.Add(1)
			}
		}()
	}
	wait.Wait()

	if accepted.Load() != 1 || unexpected.Load() != 0 {
		t.Fatalf("accepted = %d, unexpected = %d", accepted.Load(), unexpected.Load())
	}
}

func TestNewMemoryReplayStoreRequiresExplicitBounds(t *testing.T) {
	t.Parallel()

	for _, config := range []MemoryReplayConfig{
		{},
		{Capacity: 1, MaxTTL: time.Minute},
		{Capacity: 1, Now: time.Now},
		{MaxTTL: time.Minute, Now: time.Now},
	} {
		if _, err := NewMemoryReplayStore(config); !errors.Is(err, ErrInvalidReplayConfig) {
			t.Fatalf("NewMemoryReplayStore(%#v) error = %v, want ErrInvalidReplayConfig", config, err)
		}
	}
}
