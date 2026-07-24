package memory_test

import (
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/backend/memory"
)

func FuzzBackendConformanceOperations(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 2})
	f.Add([]byte{0, 8, 16, 24, 32, 40})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			operations = operations[:256]
		}
		clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
		backend, err := memory.New(memory.Config{
			MaxEntries: 16,
			MaxBytes:   4096,
			Clock:      clock,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = backend.Close() })
		model := make(map[string]cache.Record)

		for index, operation := range operations {
			key := string(rune('a' + operation%8))
			removeExpired(model, key, clock.Now())
			switch operation % 6 {
			case 0:
				record := fuzzRecord(byte(index), clock.Now())
				written, err := backend.Set(t.Context(), key, record, cache.Unconditional)
				if err != nil || !written {
					t.Fatalf("unconditional Set: written=%t err=%v", written, err)
				}
				model[key] = record
			case 1:
				_, expected := model[key]
				deleted, err := backend.Delete(t.Context(), key)
				if err != nil || deleted != expected {
					t.Fatalf("Delete: deleted=%t want=%t err=%v", deleted, expected, err)
				}
				delete(model, key)
			case 2:
				got, found, err := backend.Get(t.Context(), key)
				want, expected := model[key]
				if err != nil || found != expected || found && string(got.Payload) != string(want.Payload) {
					t.Fatalf("Get: record=%#v found=%t want=%#v expected=%t err=%v", got, found, want, expected, err)
				}
			case 3:
				record := fuzzRecord(byte(index), clock.Now())
				_, exists := model[key]
				written, err := backend.Set(t.Context(), key, record, cache.IfAbsent)
				if err != nil || written == exists {
					t.Fatalf("IfAbsent: written=%t exists=%t err=%v", written, exists, err)
				}
				if written {
					model[key] = record
				}
			case 4:
				record := fuzzRecord(byte(index), clock.Now())
				_, exists := model[key]
				written, err := backend.Set(t.Context(), key, record, cache.IfPresent)
				if err != nil || written != exists {
					t.Fatalf("IfPresent: written=%t exists=%t err=%v", written, exists, err)
				}
				if written {
					model[key] = record
				}
			case 5:
				clock.now = clock.now.Add(30 * time.Second)
			}
		}
	})
}

func fuzzRecord(value byte, now time.Time) cache.Record {
	return cache.Record{
		Payload:   []byte{value},
		ExpiresAt: now.Add(10 * time.Second),
		StaleAt:   now.Add(20 * time.Second),
	}
}

func removeExpired(model map[string]cache.Record, key string, now time.Time) {
	if record, found := model[key]; found && !now.Before(record.StaleAt) {
		delete(model, key)
	}
}
