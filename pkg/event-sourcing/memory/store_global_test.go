package memory_test

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestStoreReadsOneStableBoundedGlobalOrder(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	first := testStream(t, "account-1")
	second := testStream(t, "account-2")
	appendGlobalMessages(t, store, first, eventsourcing.ExpectNewStream(),
		testPending(t, first, "message-1", "account.opened"),
		testPending(t, first, "message-2", "account.changed"),
	)
	appendGlobalMessages(t, store, second, eventsourcing.ExpectNewStream(),
		testPending(t, second, "message-3", "account.opened"),
	)
	appendGlobalMessages(t, store, first, eventsourcing.ExpectExactVersion(2),
		testPending(t, first, "message-4", "account.changed"),
	)

	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 2,
			ToPosition:   3,
			Limit:        2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadGlobal(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := iterator.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	var identifiers []string
	for iterator.Next(context.Background()) {
		message := iterator.Message()
		position, ok := message.GlobalPosition()
		if !ok ||
			position != eventsourcing.GlobalPosition(len(identifiers)+2) {
			t.Fatalf("GlobalPosition() = %d, %t", position, ok)
		}
		identifiers = append(identifiers, message.ID().String())
	}
	if err := iterator.Err(); err != nil {
		t.Fatal(err)
	}
	if len(identifiers) != 2 ||
		identifiers[0] != "message-2" ||
		identifiers[1] != "message-3" {
		t.Fatalf("global identifiers = %v", identifiers)
	}

	limited, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	limitedIterator, err := store.ReadGlobal(context.Background(), limited)
	if err != nil {
		t.Fatal(err)
	}
	if !limitedIterator.Next(context.Background()) ||
		limitedIterator.Message().ID().String() != "message-1" ||
		limitedIterator.Next(context.Background()) {
		t.Fatalf(
			"limited global read = %#v, %v",
			limitedIterator.Message(),
			limitedIterator.Err(),
		)
	}
	if err := limitedIterator.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreGlobalReadBeyondCurrentEndIsEmpty(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadGlobal(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if iterator.Next(context.Background()) ||
		iterator.Err() != nil ||
		!iterator.Message().ID().IsZero() {
		t.Fatalf("empty global read = %#v, %v", iterator.Message(), iterator.Err())
	}
	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreValidatesGlobalReadInputs(t *testing.T) {
	t.Parallel()

	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var nilContext context.Context
	tests := map[string]struct {
		store   *memory.Store
		ctx     context.Context
		options eventsourcing.ReadGlobalOptions
		want    error
	}{
		"zero store": {
			store:   &memory.Store{},
			ctx:     context.Background(),
			options: options,
			want:    eventsourcing.ErrInvalidArgument,
		},
		"nil context": {
			store:   memory.NewStore(),
			ctx:     nilContext,
			options: options,
			want:    eventsourcing.ErrInvalidArgument,
		},
		"cancelled": {
			store:   memory.NewStore(),
			ctx:     cancelled,
			options: options,
			want:    context.Canceled,
		},
		"invalid options": {
			store: memory.NewStore(),
			ctx:   context.Background(),
			want:  eventsourcing.ErrInvalidArgument,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			iterator, err := testCase.store.ReadGlobal(
				testCase.ctx,
				testCase.options,
			)
			if iterator != nil || !errors.Is(err, testCase.want) {
				t.Fatalf("ReadGlobal() = %#v, %v", iterator, err)
			}
		})
	}
}

func appendGlobalMessages(
	t *testing.T,
	store *memory.Store,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	messages ...eventsourcing.PendingMessage,
) {
	t.Helper()

	if _, err := store.Append(
		context.Background(),
		stream,
		expected,
		messages,
	); err != nil {
		t.Fatal(err)
	}
}
