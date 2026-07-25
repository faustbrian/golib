package history

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"
)

func TestSealUsesStableV1Encoding(t *testing.T) {
	t.Parallel()

	entry := Seal(HashBytes([]byte("retained-prefix")), Event{
		Sequence:       41,
		OccurredAt:     time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		IdempotencyKey: "request-123",
		Actor:          "operator@example.test",
		Action:         "drain",
		Target:         "worker_group:payments",
		Result:         "accepted",
	})
	const expected = "48324e6f13493468d2ae76db0f506a31d5902cf4030f9a7f7a49c4948b2876aa"
	if actual := fmt.Sprintf("%x", entry.Hash); actual != expected {
		t.Fatalf("Seal() hash = %s, want %s", actual, expected)
	}
}

func TestVerifyAcceptsHashLinkedAuditEntries(t *testing.T) {
	t.Parallel()

	anchor := HashBytes([]byte("retained-prefix"))
	first := Seal(anchor, Event{
		Sequence:       41,
		OccurredAt:     time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		IdempotencyKey: "request-123",
		Actor:          "operator@example.test",
		Action:         "drain",
		Target:         "worker_group:payments",
		Result:         "accepted",
	})
	second := Seal(first.Hash, Event{
		Sequence:       42,
		OccurredAt:     time.Date(2026, time.July, 16, 12, 0, 1, 0, time.UTC),
		IdempotencyKey: "request-123",
		Actor:          "operator@example.test",
		Action:         "drain",
		Target:         "worker_group:payments",
		Result:         "succeeded",
	})

	if err := Verify(anchor, []Entry{first, second}); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestV2AuditDigestProtectsCommandIdentifier(t *testing.T) {
	t.Parallel()

	anchor := HashBytes([]byte("anchor"))
	entry := Seal(anchor, Event{
		Sequence: 1, HashVersion: 2,
		CommandID: "78891f07-55ff-4f2f-a9b2-a4c4b756d31f",
		Actor:     "operator-1", Action: "payload_view", Result: "authorized",
	})
	if err := Verify(anchor, []Entry{entry}); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	tampered := entry
	tampered.Event.CommandID = "d7fdbec4-96ce-4227-ac32-0030fc05c3cb"
	if err := Verify(anchor, []Entry{tampered}); !errors.Is(err, ErrAuditTampered) {
		t.Fatalf("tampered Verify() error = %v", err)
	}

	unsupported := entry
	unsupported.Event.HashVersion = 3
	if err := Verify(anchor, []Entry{unsupported}); !errors.Is(err, ErrAuditTampered) {
		t.Fatalf("unsupported Verify() error = %v", err)
	}
}

func TestVerifyRejectsTamperingAndReordering(t *testing.T) {
	t.Parallel()

	anchor := HashBytes([]byte("anchor"))
	first := Seal(anchor, Event{Sequence: 1, Actor: "alice", Result: "accepted"})
	second := Seal(first.Hash, Event{Sequence: 2, Actor: "alice", Result: "succeeded"})

	tampered := first
	tampered.Event.Actor = "mallory"

	tests := map[string][]Entry{
		"content change": {tampered, second},
		"reordered":      {second, first},
		"missing entry":  {second},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := Verify(anchor, entries)
			if !errors.Is(err, ErrAuditTampered) {
				t.Fatalf("Verify() error = %v, want ErrAuditTampered", err)
			}
		})
	}
}

func TestVerifyAcceptsEmptyRetainedPage(t *testing.T) {
	t.Parallel()

	if err := Verify(HashBytes([]byte("anchor")), nil); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyFromRequiresContiguousSequenceAfterRetentionAnchor(t *testing.T) {
	t.Parallel()

	anchor := HashBytes([]byte("retained-prefix"))
	first := Seal(anchor, Event{Sequence: 5, Actor: "operator"})
	second := Seal(first.Hash, Event{Sequence: 6, Actor: "operator"})
	if err := VerifyFrom(4, anchor, []Entry{first, second}); err != nil {
		t.Fatalf("VerifyFrom() error = %v", err)
	}

	gap := Seal(first.Hash, Event{Sequence: 7, Actor: "operator"})
	err := VerifyFrom(4, anchor, []Entry{first, gap})
	if !errors.Is(err, ErrAuditTampered) {
		t.Fatalf("VerifyFrom(gap) error = %v, want ErrAuditTampered", err)
	}
}

func TestVerifyFromRejectsSequenceOverflow(t *testing.T) {
	t.Parallel()

	anchor := HashBytes([]byte("retained-prefix"))
	err := VerifyFrom(math.MaxUint64, anchor, []Entry{{}})
	if !errors.Is(err, ErrAuditTampered) {
		t.Fatalf("VerifyFrom() error = %v, want ErrAuditTampered", err)
	}
}

func TestVerifyAvoidsPerEntryAllocations(t *testing.T) {
	anchor := HashBytes([]byte("retained-prefix"))
	previous := anchor
	entries := make([]Entry, 100)
	for index := range entries {
		entries[index] = Seal(previous, Event{
			Sequence:       uint64(index + 1),
			OccurredAt:     time.Unix(int64(index), 0).UTC(),
			IdempotencyKey: "request-123",
			Actor:          "operator-1",
			Action:         "retry",
			Target:         "failure-123",
			Result:         "succeeded",
		})
		previous = entries[index].Hash
	}

	allocations := testing.AllocsPerRun(10, func() {
		if err := VerifyFrom(0, anchor, entries); err != nil {
			t.Fatalf("VerifyFrom() error = %v", err)
		}
	})
	if allocations > 1 {
		t.Fatalf("VerifyFrom() allocations = %.0f, want at most 1", allocations)
	}
}
