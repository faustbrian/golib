package eventsourcing_test

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

type typedAggregateID [16]byte

type typedAggregate struct {
	id        typedAggregateID
	label     string
	lifecycle eventsourcing.Lifecycle
}

type typedAggregateCreated struct {
	Label string `json:"label"`
}

func TestRepositoryUsesCanonicalApplicationIdentifierEncoding(t *testing.T) {
	t.Parallel()

	id := typedAggregateID{
		0x01, 0x23, 0x45, 0x67,
		0x89, 0xab,
		0x4d, 0xef,
		0x80, 0x00,
		0x10, 0x20, 0x30, 0x40, 0x50, 0x60,
	}
	store := memory.NewStore()
	repository := typedIdentifierRepository(t, store)
	aggregate := &typedAggregate{id: id}
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "typed.created",
			Version: 1,
			Value:   typedAggregateCreated{Label: "canonical"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := aggregate.lifecycle.Record(event, aggregate.apply); err != nil {
		t.Fatal(err)
	}

	result, err := repository.Save(context.Background(), aggregate)
	if err != nil {
		t.Fatal(err)
	}
	messages := result.Messages()
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	const canonical = "01234567-89ab-4def-8000-102030405060"
	if messages[0].Stream().AggregateID() != canonical {
		t.Fatalf(
			"AggregateID() = %q, want %q",
			messages[0].Stream().AggregateID(),
			canonical,
		)
	}

	loaded, err := repository.Load(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.id != id || loaded.label != "canonical" {
		t.Fatalf("loaded aggregate = %#v", loaded)
	}
}

func TestCanonicalIdentifierCodecRejectsInvalidAndPreservesByteOrder(
	t *testing.T,
) {
	t.Parallel()

	first := typedAggregateID{15: 0x01}
	second := typedAggregateID{15: 0x02}
	firstText, err := encodeTypedAggregateID(first)
	if err != nil {
		t.Fatal(err)
	}
	secondText, err := encodeTypedAggregateID(second)
	if err != nil {
		t.Fatal(err)
	}
	encoded := []string{secondText, firstText}
	sort.Strings(encoded)
	if encoded[0] != firstText || encoded[1] != secondText {
		t.Fatalf("canonical text order = %v", encoded)
	}
	decoded, err := decodeTypedAggregateID(firstText)
	if err != nil || decoded != first {
		t.Fatalf("decode canonical ID = %#v, %v", decoded, err)
	}

	invalid := []string{
		"",
		"00000000-0000-0000-0000-00000000000g",
		"000000000000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000001 ",
		"00000000-0000-0000-0000-00000000000A",
	}
	for _, value := range invalid {
		if _, err := decodeTypedAggregateID(value); err == nil {
			t.Fatalf("decodeTypedAggregateID(%q) error = nil", value)
		}
	}
	if _, err := encodeTypedAggregateID(typedAggregateID{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("encode zero ID error = %v", err)
	}
}

func TestEventSauceUUIDMigrationMapsBinaryAndStringSources(t *testing.T) {
	t.Parallel()

	want := typedAggregateID{
		0x12, 0x34, 0x56, 0x78,
		0x9a, 0xbc,
		0x4e, 0xf0,
		0x80, 0x00,
		0x10, 0x20, 0x30, 0x40, 0x50, 0x60,
	}
	fromBinary, err := decodeEventSauceBinaryUUID(want[:])
	if err != nil {
		t.Fatal(err)
	}
	fromString, err := decodeTypedAggregateID(
		"12345678-9abc-4ef0-8000-102030405060",
	)
	if err != nil {
		t.Fatal(err)
	}
	if fromBinary != want || fromString != want {
		t.Fatalf(
			"migrated IDs = binary %#v, string %#v, want %#v",
			fromBinary,
			fromString,
			want,
		)
	}
	binaryText, err := encodeTypedAggregateID(fromBinary)
	if err != nil {
		t.Fatal(err)
	}
	stringText, err := encodeTypedAggregateID(fromString)
	if err != nil {
		t.Fatal(err)
	}
	if binaryText != stringText {
		t.Fatalf("canonical migration outputs = %q, %q", binaryText, stringText)
	}
	messageID, err := eventsourcing.NewMessageID(binaryText)
	if err != nil || messageID.String() != binaryText {
		t.Fatalf("migrated message ID = %q, %v", messageID.String(), err)
	}

	for _, value := range [][]byte{nil, make([]byte, 15), make([]byte, 17)} {
		if _, err := decodeEventSauceBinaryUUID(value); err == nil {
			t.Fatalf("decode binary UUID length %d error = nil", len(value))
		}
	}
	if _, err := decodeEventSauceBinaryUUID(make([]byte, 16)); err == nil {
		t.Fatal("decode zero binary UUID error = nil")
	}
}

func (aggregate *typedAggregate) apply(
	event eventsourcing.DecodedEvent,
) error {
	value, ok := event.Value().(typedAggregateCreated)
	if !ok || value.Label == "" {
		return eventsourcing.ErrUnknownEvent
	}
	aggregate.label = value.Label

	return nil
}

func typedIdentifierRepository(
	t *testing.T,
	store eventsourcing.EventStore,
) *eventsourcing.AggregateRepository[typedAggregateID, *typedAggregate] {
	t.Helper()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[typedAggregateCreated]("typed.created", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	upcasters, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	decorators, err := eventsourcing.NewMessageDecoratorChain()
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher()
	if err != nil {
		t.Fatal(err)
	}
	repository, err := eventsourcing.NewRepository(
		eventsourcing.RepositoryConfig[typedAggregateID, *typedAggregate]{
			AggregateType: "typed",
			EncodeID:      encodeTypedAggregateID,
			Identify: func(aggregate *typedAggregate) typedAggregateID {
				return aggregate.id
			},
			NewAggregate: func(id typedAggregateID) (*typedAggregate, error) {
				return &typedAggregate{id: id}, nil
			},
			Lifecycle: func(aggregate *typedAggregate) *eventsourcing.Lifecycle {
				return &aggregate.lifecycle
			},
			Apply: func(
				aggregate *typedAggregate,
				event eventsourcing.DecodedEvent,
			) error {
				return aggregate.apply(event)
			},
			Store:     store,
			Codec:     codec,
			Upcasters: upcasters,
			Clock:     eventsourcing.SystemClock{},
			MessageIDs: eventsourcing.MessageIDGeneratorFunc(
				func(context.Context) (eventsourcing.MessageID, error) {
					return eventsourcing.NewMessageID("identifier-message")
				},
			),
			Decorators: decorators,
			Dispatcher: dispatcher,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return repository
}

func encodeTypedAggregateID(id typedAggregateID) (string, error) {
	if id == (typedAggregateID{}) {
		return "", eventsourcing.ErrInvalidArgument
	}

	var encoded [32]byte
	hex.Encode(encoded[:], id[:])

	return string(encoded[0:8]) + "-" +
		string(encoded[8:12]) + "-" +
		string(encoded[12:16]) + "-" +
		string(encoded[16:20]) + "-" +
		string(encoded[20:32]), nil
}

func decodeTypedAggregateID(value string) (typedAggregateID, error) {
	var id typedAggregateID
	if len(value) != 36 ||
		value[8] != '-' ||
		value[13] != '-' ||
		value[18] != '-' ||
		value[23] != '-' ||
		strings.ToLower(value) != value {
		return id, eventsourcing.ErrInvalidArgument
	}
	raw := strings.ReplaceAll(value, "-", "")
	if _, err := hex.Decode(id[:], []byte(raw)); err != nil {
		return typedAggregateID{}, eventsourcing.ErrInvalidArgument
	}
	canonical, err := encodeTypedAggregateID(id)
	if err != nil || canonical != value {
		return typedAggregateID{}, eventsourcing.ErrInvalidArgument
	}

	return id, nil
}

func decodeEventSauceBinaryUUID(value []byte) (typedAggregateID, error) {
	var id typedAggregateID
	if len(value) != len(id) {
		return id, eventsourcing.ErrInvalidArgument
	}
	copy(id[:], value)
	if id == (typedAggregateID{}) {
		return typedAggregateID{}, eventsourcing.ErrInvalidArgument
	}

	return id, nil
}
