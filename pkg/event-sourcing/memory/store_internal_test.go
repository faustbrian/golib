package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestStoreChecksCancellationAfterWaitingForWriteOwnership(t *testing.T) {
	t.Parallel()

	store := NewStore()
	stream := internalStream(t)
	ctx := &cancelAfterChecks{allowed: 1}

	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{internalPending(t, stream)},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Append() error = %v, want context.Canceled", err)
	}
}

func TestStoreChecksCancellationWhilePreparingAtomicBatch(t *testing.T) {
	t.Parallel()

	store := NewStore()
	stream := internalStream(t)
	ctx := &cancelAfterChecks{allowed: 2}

	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{internalPending(t, stream)},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Append() error = %v, want context.Canceled", err)
	}
	if len(store.streams) != 0 || len(store.messageIDs) != 0 {
		t.Fatal("cancelled batch mutated store")
	}
}

func TestStoreRejectsGlobalPositionOverflow(t *testing.T) {
	t.Parallel()

	store := NewStore()
	store.globalPosition = eventsourcing.GlobalPosition(^uint64(0))
	stream := internalStream(t)

	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{internalPending(t, stream)},
	); !errors.Is(err, eventsourcing.ErrVersionOverflow) {
		t.Fatalf("Append() error = %v, want ErrVersionOverflow", err)
	}
}

type cancelAfterChecks struct {
	allowed int
	checks  int
}

func (*cancelAfterChecks) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*cancelAfterChecks) Done() <-chan struct{} {
	return nil
}

func (ctx *cancelAfterChecks) Err() error {
	ctx.checks++
	if ctx.checks > ctx.allowed {
		return context.Canceled
	}

	return nil
}

func (*cancelAfterChecks) Value(any) any {
	return nil
}

func internalStream(t *testing.T) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("bank.account", "account-42")
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func internalPending(
	t *testing.T,
	stream eventsourcing.StreamID,
) eventsourcing.PendingMessage {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         "message-1",
		Stream:     stream,
		Event:      event,
		RecordedAt: time.Date(2026, time.July, 25, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	return pending
}
