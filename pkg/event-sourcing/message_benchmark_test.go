package eventsourcing_test

import (
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func BenchmarkPendingMessageConstruction(b *testing.B) {
	stream, err := eventsourcing.NewStreamID("bank.account", "account-42")
	if err != nil {
		b.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte(`{"owner":"customer-9"}`),
	})
	if err != nil {
		b.Fatal(err)
	}
	input := eventsourcing.PendingMessageInput{
		ID:         "message-1",
		Stream:     stream,
		Event:      event,
		Metadata:   map[string]string{"request": "request-7"},
		RecordedAt: time.Date(2026, time.July, 25, 9, 30, 45, 0, time.UTC),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := eventsourcing.NewPendingMessage(input); err != nil {
			b.Fatal(err)
		}
	}
}
