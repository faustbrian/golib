package eventsourcing_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestPendingMessageOwnsMutableInputs(t *testing.T) {
	stream, err := eventsourcing.NewStreamID("bank.account", "account-42")
	if err != nil {
		t.Fatalf("create stream ID: %v", err)
	}

	payload := []byte(`{"amount":100}`)
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.money-deposited",
		Version:     1,
		ContentType: "application/json",
		Payload:     payload,
	})
	if err != nil {
		t.Fatalf("create encoded event: %v", err)
	}

	metadata := map[string]string{"request": "request-7"}
	recordedAt := time.Date(2026, time.July, 25, 12, 30, 45, 123456789, time.FixedZone("EEST", 3*60*60))

	message, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:            "message-1",
		Stream:        stream,
		Event:         event,
		Metadata:      metadata,
		RecordedAt:    recordedAt,
		CorrelationID: "message-parent",
		CausationID:   "message-command",
		Tenant:        "tenant-a",
		Partition:     "partition-3",
	})
	if err != nil {
		t.Fatalf("create pending message: %v", err)
	}

	payload[0] = '!'
	metadata["request"] = "changed"

	gotPayload := message.Event().Payload()
	if string(gotPayload) != `{"amount":100}` {
		t.Fatalf("payload = %q, want original bytes", gotPayload)
	}
	gotPayload[0] = '!'
	if string(message.Event().Payload()) != `{"amount":100}` {
		t.Fatal("payload getter exposed mutable message state")
	}

	gotMetadata := message.Metadata()
	if gotMetadata["request"] != "request-7" {
		t.Fatalf("metadata = %q, want original value", gotMetadata["request"])
	}
	gotMetadata["request"] = "changed-again"
	if message.Metadata()["request"] != "request-7" {
		t.Fatal("metadata getter exposed mutable message state")
	}

	wantRecordedAt := time.Date(2026, time.July, 25, 9, 30, 45, 123456000, time.UTC)
	if !message.RecordedAt().Equal(wantRecordedAt) || message.RecordedAt().Location() != time.UTC {
		t.Fatalf("recorded at = %s, want %s", message.RecordedAt(), wantRecordedAt)
	}

	correlationID, hasCorrelationID := message.CorrelationID()
	causationID, hasCausationID := message.CausationID()
	tenant, hasTenant := message.Tenant()
	partition, hasPartition := message.Partition()
	if message.ID().String() != "message-1" ||
		message.Stream() != stream ||
		!hasCorrelationID || correlationID.String() != "message-parent" ||
		!hasCausationID || causationID.String() != "message-command" ||
		!hasTenant || tenant != "tenant-a" ||
		!hasPartition || partition != "partition-3" {
		t.Fatalf("message identity fields were not preserved: %v", message)
	}
}

func TestPendingMessageRejectsReservedMetadataWithoutDisclosingValues(t *testing.T) {
	stream, err := eventsourcing.NewStreamID("bank.account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte(`{"secret":"payload-secret"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         "message-1",
		Stream:     stream,
		Event:      event,
		Metadata:   map[string]string{"ES.secret": "metadata-secret"},
		RecordedAt: time.Now(),
	})
	if !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	if strings.Contains(err.Error(), "metadata-secret") ||
		strings.Contains(err.Error(), "payload-secret") {
		t.Fatalf("error disclosed protected data: %v", err)
	}

	var validationError *eventsourcing.ValidationError
	if !errors.As(err, &validationError) || validationError.Field != "metadata" {
		t.Fatalf("error = %v, want metadata ValidationError", err)
	}
}

func TestPersistedMessageAssignsPositionsAndComparesByValue(t *testing.T) {
	stream, err := eventsourcing.NewStreamID("bank.account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte(`{"owner":"customer-9"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         "message-1",
		Stream:     stream,
		Event:      event,
		Metadata:   map[string]string{"request": "request-7"},
		RecordedAt: time.Date(2026, time.July, 25, 9, 30, 45, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  4,
		GlobalPosition: 17,
	})
	if err != nil {
		t.Fatalf("create persisted message: %v", err)
	}
	same, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  4,
		GlobalPosition: 17,
	})
	if err != nil {
		t.Fatal(err)
	}
	different, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  5,
		GlobalPosition: 18,
	})
	if err != nil {
		t.Fatal(err)
	}

	position, hasPosition := message.GlobalPosition()
	if message.StreamVersion() != 4 ||
		message.EventName().String() != "account.opened" ||
		!hasPosition ||
		position != 17 {
		t.Fatalf(
			"positions = stream %d, global %d/%t",
			message.StreamVersion(),
			position,
			hasPosition,
		)
	}
	if !message.Equal(same) || message.Equal(different) {
		t.Fatal("message value equality ignored persisted positions")
	}

	payload := message.Event().Payload()
	payload[0] = '!'
	metadata := message.Metadata()
	metadata["request"] = "changed"
	if !message.Equal(same) {
		t.Fatal("message accessors exposed mutable state")
	}
}
