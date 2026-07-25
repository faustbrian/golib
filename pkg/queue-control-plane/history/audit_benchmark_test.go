package history

import (
	"testing"
	"time"
)

func BenchmarkVerifyOneHundredThousandAuditEvents(b *testing.B) {
	entries := make([]Entry, 100_000)
	previous := HashBytes([]byte("retained-prefix"))
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
	anchor := HashBytes([]byte("retained-prefix"))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := VerifyFrom(0, anchor, entries); err != nil {
			b.Fatalf("VerifyFrom() error = %v", err)
		}
	}
}
