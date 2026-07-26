package eventsourcing_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

type childOrder struct {
	id        string
	lines     map[string]*childOrderLine
	lifecycle eventsourcing.Lifecycle
}

type childOrderLine struct {
	id       string
	quantity uint32
	record   func(eventsourcing.DecodedEvent) error
}

type childLineAdded struct {
	LineID   string `json:"line_id"`
	Quantity uint32 `json:"quantity"`
}

type childLineQuantityChanged struct {
	LineID   string `json:"line_id"`
	Quantity uint32 `json:"quantity"`
}

func TestChildEntityRecordsThroughRootIdentityAndReconstitutes(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	repository := childOrderRepository(t, store)
	order := newChildOrder("order-42")

	if err := order.addLine("line-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := order.lines["line-a"].changeQuantity(3); err != nil {
		t.Fatal(err)
	}

	result, err := repository.Save(context.Background(), order)
	if err != nil {
		t.Fatal(err)
	}
	assertChildMessages(
		t,
		result.Messages(),
		[]string{"order.line-added", "order.line-quantity-changed"},
		1,
	)

	loaded, err := repository.Load(context.Background(), "order-42")
	if err != nil {
		t.Fatal(err)
	}
	line := loaded.lines["line-a"]
	if line == nil || line.quantity != 3 {
		t.Fatalf("reconstituted line = %#v", line)
	}
	if loaded.lifecycle.CommittedVersion() != 2 {
		t.Fatalf(
			"CommittedVersion() = %d, want 2",
			loaded.lifecycle.CommittedVersion(),
		)
	}

	if err := line.changeQuantity(5); err != nil {
		t.Fatal(err)
	}
	result, err = repository.Save(context.Background(), loaded)
	if err != nil {
		t.Fatal(err)
	}
	assertChildMessages(
		t,
		result.Messages(),
		[]string{"order.line-quantity-changed"},
		3,
	)
}

func TestChildEntityRejectsInvariantBreakingHistory(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("order", "order-corrupt")
	if err != nil {
		t.Fatal(err)
	}
	pending := mustPendingForRepository(
		t,
		"corrupt-child-message",
		stream,
		mustEncodedEvent(
			t,
			"order.line-added",
			1,
			[]byte(`{"line_id":"line-a","quantity":0}`),
		),
	)
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatal(err)
	}

	repository := childOrderRepository(t, store)
	loaded, err := repository.Load(context.Background(), "order-corrupt")
	if loaded != nil || !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Load() = %#v, %v, want poisoned reconstitution", loaded, err)
	}
}

func newChildOrder(id string) *childOrder {
	return &childOrder{id: id, lines: make(map[string]*childOrderLine)}
}

func (order *childOrder) addLine(id string, quantity uint32) error {
	if id == "" || quantity == 0 {
		return errors.New("line requires an identifier and quantity")
	}
	if _, exists := order.lines[id]; exists {
		return errors.New("line already exists")
	}

	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "order.line-added",
			Version: 1,
			Value: childLineAdded{
				LineID:   id,
				Quantity: quantity,
			},
		},
	)
	if err != nil {
		return err
	}

	return order.record(event)
}

func (order *childOrder) record(event eventsourcing.DecodedEvent) error {
	return order.lifecycle.Record(event, order.apply)
}

func (order *childOrder) apply(event eventsourcing.DecodedEvent) error {
	switch value := event.Value().(type) {
	case childLineAdded:
		if value.LineID == "" || value.Quantity == 0 {
			return errors.New("line requires an identifier and quantity")
		}
		if _, exists := order.lines[value.LineID]; exists {
			return errors.New("line already exists")
		}
		order.lines[value.LineID] = &childOrderLine{
			id:       value.LineID,
			quantity: value.Quantity,
			record:   order.record,
		}
	case childLineQuantityChanged:
		line := order.lines[value.LineID]
		if line == nil {
			return errors.New("line does not exist")
		}
		if value.Quantity == 0 {
			return errors.New("quantity must be positive")
		}
		line.quantity = value.Quantity
	default:
		return eventsourcing.ErrUnknownEvent
	}

	return nil
}

func (line *childOrderLine) changeQuantity(quantity uint32) error {
	if quantity == 0 || quantity == line.quantity {
		return errors.New("quantity must be positive and changed")
	}
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "order.line-quantity-changed",
			Version: 1,
			Value: childLineQuantityChanged{
				LineID:   line.id,
				Quantity: quantity,
			},
		},
	)
	if err != nil {
		return err
	}

	return line.record(event)
}

func childOrderRepository(
	t *testing.T,
	store eventsourcing.EventStore,
) *eventsourcing.AggregateRepository[string, *childOrder] {
	t.Helper()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[childLineAdded]("order.line-added", 1),
		eventsourcing.JSONEvent[childLineQuantityChanged](
			"order.line-quantity-changed",
			1,
		),
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
	nextID := 0
	repository, err := eventsourcing.NewRepository(
		eventsourcing.RepositoryConfig[string, *childOrder]{
			AggregateType: "order",
			EncodeID: func(id string) (string, error) {
				if id == "" {
					return "", eventsourcing.ErrInvalidArgument
				}

				return id, nil
			},
			Identify: func(order *childOrder) string {
				return order.id
			},
			NewAggregate: func(id string) (*childOrder, error) {
				return newChildOrder(id), nil
			},
			Lifecycle: func(order *childOrder) *eventsourcing.Lifecycle {
				return &order.lifecycle
			},
			Apply: func(
				order *childOrder,
				event eventsourcing.DecodedEvent,
			) error {
				return order.apply(event)
			},
			Store:     store,
			Codec:     codec,
			Upcasters: upcasters,
			Clock:     eventsourcing.SystemClock{},
			MessageIDs: eventsourcing.MessageIDGeneratorFunc(
				func(context.Context) (eventsourcing.MessageID, error) {
					nextID++

					return eventsourcing.NewMessageID(
						"child-message-" + strconv.Itoa(nextID),
					)
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

func assertChildMessages(
	t *testing.T,
	messages []eventsourcing.Message,
	names []string,
	firstVersion uint64,
) {
	t.Helper()

	if len(messages) != len(names) {
		t.Fatalf("messages = %d, want %d", len(messages), len(names))
	}
	for index, message := range messages {
		stream := message.Stream()
		if stream.AggregateType() != "order" ||
			stream.AggregateID() != "order-42" ||
			message.StreamVersion() != firstVersion+uint64(index) ||
			message.EventName().String() != names[index] {
			t.Fatalf("message %d = %#v", index, message)
		}
	}
}
