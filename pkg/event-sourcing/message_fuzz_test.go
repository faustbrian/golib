package eventsourcing_test

import (
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func FuzzMessageConstruction(f *testing.F) {
	f.Add(
		"bank.account",
		"account-42",
		"account.opened",
		"message-1",
		"request",
		"value",
		[]byte("{}"),
	)
	f.Add("", "", "", "", "ES.secret", "secret", []byte{0xff})

	f.Fuzz(func(
		t *testing.T,
		aggregateType string,
		aggregateID string,
		eventName string,
		messageID string,
		metadataKey string,
		metadataValue string,
		payload []byte,
	) {
		stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
		if err != nil {
			return
		}
		event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
			Name:        eventName,
			Version:     1,
			ContentType: "application/octet-stream",
			Payload:     payload,
		})
		if err != nil {
			return
		}
		message, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
			ID:         messageID,
			Stream:     stream,
			Event:      event,
			Metadata:   map[string]string{metadataKey: metadataValue},
			RecordedAt: time.Unix(1, 1),
		})
		if err != nil {
			return
		}

		if len(payload) > 0 {
			original := message.Event().Payload()[0]
			payload[0] ^= 0xff
			if message.Event().Payload()[0] != original {
				t.Fatal("message retained caller-owned payload")
			}
		}
	})
}
