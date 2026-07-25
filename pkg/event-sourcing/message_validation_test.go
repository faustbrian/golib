package eventsourcing_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestMessageIdentifierAndStreamValidation(t *testing.T) {
	id, err := eventsourcing.NewMessageID("message:01-A")
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != "message:01-A" || id.IsZero() {
		t.Fatalf("message ID = %q, zero=%t", id.String(), id.IsZero())
	}
	if !(eventsourcing.MessageID{}).IsZero() {
		t.Fatal("zero MessageID did not report zero")
	}

	for _, value := range []string{
		"",
		"message with spaces",
		strings.Repeat("a", eventsourcing.MaxMessageIDBytes+1),
	} {
		if _, err := eventsourcing.NewMessageID(value); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
			t.Fatalf("NewMessageID(%q) error = %v", value, err)
		}
	}

	stream, err := eventsourcing.NewStreamID("bank.account_2", "konto-å")
	if err != nil {
		t.Fatal(err)
	}
	if stream.AggregateType() != "bank.account_2" ||
		stream.AggregateID() != "konto-å" ||
		stream.String() != "bank.account_2/konto-å" ||
		stream.IsZero() {
		t.Fatalf("unexpected stream: %v", stream)
	}
	if !(eventsourcing.StreamID{}).IsZero() {
		t.Fatal("zero StreamID did not report zero")
	}

	invalidUTF8 := string([]byte{0xff})
	for _, input := range []struct {
		aggregateType string
		aggregateID   string
	}{
		{"", "account-1"},
		{"Bank.account", "account-1"},
		{"bank..account", "account-1"},
		{"bank.account.", "account-1"},
		{"bank.$account", "account-1"},
		{strings.Repeat("a", eventsourcing.MaxAggregateTypeBytes+1), "account-1"},
		{"bank.account", ""},
		{"bank.account", " \t "},
		{"bank.account", invalidUTF8},
		{"bank.account", strings.Repeat("a", eventsourcing.MaxAggregateIDBytes+1)},
	} {
		if _, err := eventsourcing.NewStreamID(input.aggregateType, input.aggregateID); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
			t.Fatalf("NewStreamID(%q, %q) error = %v", input.aggregateType, input.aggregateID, err)
		}
	}
}

func TestEncodedEventValidationAndOwnership(t *testing.T) {
	payload := []byte("null")
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened_2",
		Version:     3,
		ContentType: "application/json; charset=utf-8",
		Payload:     payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = '!'

	if event.Name().String() != "account.opened_2" ||
		event.Version() != 3 ||
		event.ContentType() != "application/json; charset=utf-8" ||
		string(event.Payload()) != "null" ||
		event.IsZero() {
		t.Fatalf("unexpected event: name=%q version=%d type=%q payload=%q",
			event.Name().String(),
			event.Version(),
			event.ContentType(),
			event.Payload(),
		)
	}
	zeroEvent := eventsourcing.EncodedEvent{}
	if !zeroEvent.IsZero() || zeroEvent.Payload() != nil {
		t.Fatal("zero EncodedEvent did not report zero")
	}

	valid := eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte("{}"),
	}
	cases := map[string]eventsourcing.EncodedEventInput{
		"name": {
			Name:        "Account.Opened",
			Version:     valid.Version,
			ContentType: valid.ContentType,
			Payload:     valid.Payload,
		},
		"version": {
			Name:        valid.Name,
			ContentType: valid.ContentType,
			Payload:     valid.Payload,
		},
		"content type missing slash": {
			Name:        valid.Name,
			Version:     valid.Version,
			ContentType: "json",
			Payload:     valid.Payload,
		},
		"content type uppercase": {
			Name:        valid.Name,
			Version:     valid.Version,
			ContentType: "Application/JSON",
			Payload:     valid.Payload,
		},
		"content type noncanonical parameter spacing": {
			Name:        valid.Name,
			Version:     valid.Version,
			ContentType: "application/json;charset=utf-8",
			Payload:     valid.Payload,
		},
		"content type control": {
			Name:        valid.Name,
			Version:     valid.Version,
			ContentType: "application/\njson",
			Payload:     valid.Payload,
		},
		"content type too long": {
			Name:        valid.Name,
			Version:     valid.Version,
			ContentType: "application/" + strings.Repeat("a", eventsourcing.MaxContentTypeBytes),
			Payload:     valid.Payload,
		},
		"empty payload": {
			Name:        valid.Name,
			Version:     valid.Version,
			ContentType: valid.ContentType,
		},
		"large payload": {
			Name:        valid.Name,
			Version:     valid.Version,
			ContentType: valid.ContentType,
			Payload:     make([]byte, eventsourcing.MaxPayloadBytes+1),
		},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := eventsourcing.NewEncodedEvent(input); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestPendingAndPersistedMessageValidation(t *testing.T) {
	valid := validPendingMessageInput(t)

	message, err := eventsourcing.NewPendingMessage(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := message.CorrelationID(); ok {
		t.Fatal("unexpected correlation ID")
	}
	if _, ok := message.CausationID(); ok {
		t.Fatal("unexpected causation ID")
	}
	if _, ok := message.Tenant(); ok {
		t.Fatal("unexpected tenant")
	}
	if _, ok := message.Partition(); ok {
		t.Fatal("unexpected partition")
	}
	if strings.Contains(message.String(), string(valid.Event.Payload())) ||
		strings.Contains(message.String(), "metadata-secret") {
		t.Fatalf("pending diagnostics disclosed protected values: %s", message)
	}

	tooManyMetadata := make(map[string]string, eventsourcing.MaxMetadataEntries+1)
	for index := range eventsourcing.MaxMetadataEntries + 1 {
		tooManyMetadata[fmt.Sprintf("key-%02d", index)] = "value"
	}
	oversizedMetadata := map[string]string{}
	for index := range eventsourcing.MaxMetadataEntries {
		oversizedMetadata[fmt.Sprintf("key-%02d", index)] =
			strings.Repeat("v", eventsourcing.MaxMetadataValueBytes)
	}

	cases := map[string]func(*eventsourcing.PendingMessageInput){
		"id":             func(input *eventsourcing.PendingMessageInput) { input.ID = "" },
		"stream":         func(input *eventsourcing.PendingMessageInput) { input.Stream = eventsourcing.StreamID{} },
		"event":          func(input *eventsourcing.PendingMessageInput) { input.Event = eventsourcing.EncodedEvent{} },
		"time":           func(input *eventsourcing.PendingMessageInput) { input.RecordedAt = time.Time{} },
		"metadata count": func(input *eventsourcing.PendingMessageInput) { input.Metadata = tooManyMetadata },
		"metadata key":   func(input *eventsourcing.PendingMessageInput) { input.Metadata = map[string]string{"bad key": "value"} },
		"metadata value": func(input *eventsourcing.PendingMessageInput) {
			input.Metadata = map[string]string{"key": string([]byte{0xff})}
		},
		"metadata total": func(input *eventsourcing.PendingMessageInput) { input.Metadata = oversizedMetadata },
		"correlation":    func(input *eventsourcing.PendingMessageInput) { input.CorrelationID = "bad id" },
		"causation":      func(input *eventsourcing.PendingMessageInput) { input.CausationID = "bad id" },
		"tenant":         func(input *eventsourcing.PendingMessageInput) { input.Tenant = "bad\nvalue" },
		"tenant blank":   func(input *eventsourcing.PendingMessageInput) { input.Tenant = " " },
		"partition":      func(input *eventsourcing.PendingMessageInput) { input.Partition = "bad\nvalue" },
		"partition blank": func(input *eventsourcing.PendingMessageInput) {
			input.Partition = " "
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := eventsourcing.NewPendingMessage(input); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}

	if _, err := eventsourcing.NewMessage(eventsourcing.MessageInput{}); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero pending error = %v", err)
	}
	if _, err := eventsourcing.NewMessage(eventsourcing.MessageInput{Pending: message}); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero version error = %v", err)
	}

	persisted, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       message,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted.GlobalPosition(); ok {
		t.Fatal("unexpected global position")
	}
	if persisted.ID() != message.ID() ||
		persisted.Stream() != message.Stream() ||
		persisted.RecordedAt() != message.RecordedAt() ||
		persisted.Event().Name() != message.Event().Name() ||
		persisted.Metadata()["request"] != "metadata-secret" {
		t.Fatalf("persisted message did not preserve pending fields: %v", persisted)
	}
	if _, ok := persisted.CorrelationID(); ok {
		t.Fatal("unexpected persisted correlation ID")
	}
	if _, ok := persisted.CausationID(); ok {
		t.Fatal("unexpected persisted causation ID")
	}
	if _, ok := persisted.Tenant(); ok {
		t.Fatal("unexpected persisted tenant")
	}
	if _, ok := persisted.Partition(); ok {
		t.Fatal("unexpected persisted partition")
	}
	if strings.Contains(persisted.String(), string(valid.Event.Payload())) ||
		strings.Contains(persisted.String(), "metadata-secret") {
		t.Fatalf("persisted diagnostics disclosed protected values: %s", persisted)
	}
}

func TestValidationErrorSupportsInspection(t *testing.T) {
	_, err := eventsourcing.NewMessageID("")
	var validationError *eventsourcing.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if validationError.Field != "message_id" ||
		validationError.Reason == "" ||
		validationError.Error() == "" ||
		!errors.Is(validationError.Unwrap(), eventsourcing.ErrInvalidArgument) {
		t.Fatalf("unexpected validation error: %#v", validationError)
	}
}

func validPendingMessageInput(t *testing.T) eventsourcing.PendingMessageInput {
	t.Helper()

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

	return eventsourcing.PendingMessageInput{
		ID:         "message-1",
		Stream:     stream,
		Event:      event,
		Metadata:   map[string]string{"request": "metadata-secret"},
		RecordedAt: time.Now(),
	}
}
