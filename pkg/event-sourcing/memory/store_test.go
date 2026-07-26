package memory_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestStoreAppendsAndReadsOneAtomicOrderedStream(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream := testStream(t, "account-42")
	pending := []eventsourcing.PendingMessage{
		testPending(t, stream, "message-1", "account.opened"),
		testPending(t, stream, "message-2", "account.email-changed"),
	}

	stored, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if len(stored) != 2 ||
		stored[0].StreamVersion() != 1 ||
		stored[1].StreamVersion() != 2 {
		t.Fatalf("stored versions = %v", messageVersions(stored))
	}
	for index, message := range stored {
		position, ok := message.GlobalPosition()
		if !ok || position != eventsourcing.GlobalPosition(index+1) {
			t.Fatalf("message %d global position = (%d, %t)", index, position, ok)
		}
	}

	options, err := eventsourcing.NewReadStreamOptions(eventsourcing.ReadStreamOptionsInput{
		FromVersion: 2,
		Limit:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadStream(context.Background(), stream, options)
	if err != nil {
		t.Fatalf("ReadStream() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := iterator.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	if !iterator.Next(context.Background()) {
		t.Fatalf("Next() = false, error = %v", iterator.Err())
	}
	if iterator.Message().ID().String() != "message-2" {
		t.Fatalf("Message().ID() = %s, want message-2", iterator.Message().ID())
	}
	if iterator.Next(context.Background()) {
		t.Fatal("Next() = true after bounded range")
	}
	if err := iterator.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestStoreRejectsConflictsAndDuplicateIDsWithoutPartialAppend(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream := testStream(t, "account-42")
	first := testPending(t, stream, "message-1", "account.opened")
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first},
	); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		expected eventsourcing.ExpectedVersion
		pending  []eventsourcing.PendingMessage
		want     error
	}{
		"new stream conflict": {
			expected: eventsourcing.ExpectNewStream(),
			pending: []eventsourcing.PendingMessage{
				testPending(t, stream, "message-2", "account.email-changed"),
			},
			want: eventsourcing.ErrConcurrencyConflict,
		},
		"exact conflict": {
			expected: eventsourcing.ExpectExactVersion(2),
			pending: []eventsourcing.PendingMessage{
				testPending(t, stream, "message-3", "account.email-changed"),
			},
			want: eventsourcing.ErrConcurrencyConflict,
		},
		"stored duplicate": {
			expected: eventsourcing.ExpectExactVersion(1),
			pending:  []eventsourcing.PendingMessage{first},
			want:     eventsourcing.ErrDuplicateMessageID,
		},
		"batch duplicate": {
			expected: eventsourcing.ExpectExactVersion(1),
			pending: []eventsourcing.PendingMessage{
				testPending(t, stream, "message-4", "account.email-changed"),
				testPending(t, stream, "message-4", "account.email-changed"),
			},
			want: eventsourcing.ErrDuplicateMessageID,
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			_, err := store.Append(
				context.Background(),
				stream,
				test.expected,
				test.pending,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("Append() error = %v, want %v", err, test.want)
			}
			if errors.Is(test.want, eventsourcing.ErrConcurrencyConflict) {
				var conflict *eventsourcing.ConcurrencyError
				if !errors.As(err, &conflict) {
					t.Fatalf("Append() error = %v, want ConcurrencyError", err)
				}
				if conflict.Stream != stream ||
					conflict.ActualVersion != 1 ||
					conflict.Expected != test.expected {
					t.Fatalf("concurrency details = %#v", conflict)
				}
			}
			if eventsourcing.AppendCommitOutcome(err) != eventsourcing.CommitNotCommitted {
				t.Fatalf("AppendCommitOutcome() = %d", eventsourcing.AppendCommitOutcome(err))
			}
		})
	}

	options, err := eventsourcing.NewReadStreamOptions(eventsourcing.ReadStreamOptionsInput{
		FromVersion: 1,
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadStream(context.Background(), stream, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := iterator.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	count := 0
	for iterator.Next(context.Background()) {
		count++
	}
	if iterator.Err() != nil || count != 1 {
		t.Fatalf("stream after rejected appends = count %d, error %v", count, iterator.Err())
	}
}

func TestStoreSerializesConcurrentExpectedVersionWriters(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream := testStream(t, "account-42")
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			testPending(t, stream, "message-1", "account.opened"),
		},
	); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	concurrent := []eventsourcing.PendingMessage{
		testPending(t, stream, "concurrent-a", "account.email-changed"),
		testPending(t, stream, "concurrent-b", "account.email-changed"),
	}
	for index := 0; index < 2; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.Append(
				context.Background(),
				stream,
				eventsourcing.ExpectExactVersion(1),
				[]eventsourcing.PendingMessage{concurrent[index]},
			)
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, eventsourcing.ErrConcurrencyConflict):
			conflicts++
		default:
			t.Fatalf("Append() error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results = %d successes, %d conflicts", successes, conflicts)
	}
}

func TestStoreReadsReportMissingCancellationAndClosure(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream := testStream(t, "missing")
	options, err := eventsourcing.NewReadStreamOptions(eventsourcing.ReadStreamOptionsInput{
		FromVersion: 1,
		Limit:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadStream(context.Background(), stream, options); !errors.Is(
		err,
		eventsourcing.ErrStreamNotFound,
	) {
		t.Fatalf("ReadStream() error = %v, want ErrStreamNotFound", err)
	}

	pending := testPending(t, stream, "message-1", "account.opened")
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectAnyVersion(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadStream(context.Background(), stream, options)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if iterator.Next(cancelled) {
		t.Fatal("Next(cancelled) = true")
	}
	if !errors.Is(iterator.Err(), context.Canceled) {
		t.Fatalf("Err() = %v, want context.Canceled", iterator.Err())
	}
	if iterator.Next(context.Background()) {
		t.Fatal("Next() resumed after cancellation")
	}
	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := iterator.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if iterator.Next(context.Background()) {
		t.Fatal("Next() = true after Close")
	}
	if !errors.Is(iterator.Err(), eventsourcing.ErrIteratorClosed) {
		t.Fatalf("Err() = %v, want ErrIteratorClosed", iterator.Err())
	}
	if !iterator.Message().ID().IsZero() {
		t.Fatalf("Message() after Close = %v", iterator.Message())
	}

	invalidIterator, err := store.ReadStream(context.Background(), stream, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := invalidIterator.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	var nilContext context.Context
	if invalidIterator.Next(nilContext) {
		t.Fatal("Next(nil) = true")
	}
	if !errors.Is(invalidIterator.Err(), eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Err() = %v, want ErrInvalidArgument", invalidIterator.Err())
	}
	if invalidIterator.Next(context.Background()) {
		t.Fatal("Next() resumed after invalid context")
	}
}

func TestStoreValidatesAppendAndReadInputs(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream := testStream(t, "account-42")
	other := testStream(t, "account-43")
	valid := testPending(t, stream, "message-1", "account.opened")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := map[string]struct {
		ctx      context.Context
		stream   eventsourcing.StreamID
		expected eventsourcing.ExpectedVersion
		pending  []eventsourcing.PendingMessage
		want     error
	}{
		"nil context": {
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{valid},
			want:     eventsourcing.ErrInvalidArgument,
		},
		"cancelled": {
			ctx:      cancelled,
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{valid},
			want:     context.Canceled,
		},
		"zero stream": {
			ctx:      context.Background(),
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{valid},
			want:     eventsourcing.ErrInvalidArgument,
		},
		"invalid expected version": {
			ctx:      context.Background(),
			stream:   stream,
			expected: eventsourcing.ExpectedVersion{},
			pending:  []eventsourcing.PendingMessage{valid},
			want:     eventsourcing.ErrInvalidArgument,
		},
		"empty batch": {
			ctx:      context.Background(),
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			want:     eventsourcing.ErrInvalidArgument,
		},
		"batch too large": {
			ctx:      context.Background(),
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			pending: func() []eventsourcing.PendingMessage {
				messages := make(
					[]eventsourcing.PendingMessage,
					eventsourcing.MaxAppendMessages+1,
				)
				for index := range messages {
					messages[index] = valid
				}

				return messages
			}(),
			want: eventsourcing.ErrInvalidArgument,
		},
		"wrong stream": {
			ctx:      context.Background(),
			stream:   other,
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{valid},
			want:     eventsourcing.ErrInvalidArgument,
		},
		"zero pending message": {
			ctx:      context.Background(),
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{{}},
			want:     eventsourcing.ErrInvalidArgument,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			_, err := store.Append(test.ctx, test.stream, test.expected, test.pending)
			if !errors.Is(err, test.want) {
				t.Fatalf("Append() error = %v, want %v", err, test.want)
			}
			if eventsourcing.AppendCommitOutcome(err) != eventsourcing.CommitNotCommitted {
				t.Fatalf("AppendCommitOutcome() = %d", eventsourcing.AppendCommitOutcome(err))
			}
		})
	}

	options, err := eventsourcing.NewReadStreamOptions(eventsourcing.ReadStreamOptionsInput{
		FromVersion: 1,
		Limit:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadStream(cancelled, stream, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadStream(cancelled) error = %v", err)
	}
	var nilContext context.Context
	if _, err := store.ReadStream(nilContext, stream, options); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("ReadStream(nil) error = %v", err)
	}
	if _, err := store.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		options,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ReadStream(zero stream) error = %v", err)
	}
	if _, err := store.ReadStream(
		context.Background(),
		stream,
		eventsourcing.ReadStreamOptions{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ReadStream(zero options) error = %v", err)
	}

	var zero memory.Store
	if _, err := zero.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{valid},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero Store.Append() error = %v", err)
	}
	if _, err := zero.ReadStream(
		context.Background(),
		stream,
		options,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero Store.ReadStream() error = %v", err)
	}
}

func TestStoreReadRangesAreInclusiveAndBounded(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream := testStream(t, "account-42")
	pending := []eventsourcing.PendingMessage{
		testPending(t, stream, "message-1", "account.opened"),
		testPending(t, stream, "message-2", "account.email-changed"),
		testPending(t, stream, "message-3", "account.closed"),
	}
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		input eventsourcing.ReadStreamOptionsInput
		want  []uint64
	}{
		"inclusive end": {
			input: eventsourcing.ReadStreamOptionsInput{
				FromVersion: 1,
				ToVersion:   2,
				Limit:       3,
			},
			want: []uint64{1, 2},
		},
		"limit": {
			input: eventsourcing.ReadStreamOptionsInput{
				FromVersion: 1,
				Limit:       2,
			},
			want: []uint64{1, 2},
		},
		"after end": {
			input: eventsourcing.ReadStreamOptionsInput{
				FromVersion: 4,
				Limit:       2,
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			options, err := eventsourcing.NewReadStreamOptions(test.input)
			if err != nil {
				t.Fatal(err)
			}
			iterator, err := store.ReadStream(context.Background(), stream, options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if closeErr := iterator.Close(); closeErr != nil {
					t.Errorf("Close() error = %v", closeErr)
				}
			})

			var actual []uint64
			for iterator.Next(context.Background()) {
				actual = append(actual, iterator.Message().StreamVersion())
			}
			if !slices.Equal(actual, test.want) {
				t.Fatalf("versions = %v, want %v", actual, test.want)
			}
		})
	}
}

func TestStoreSupportsExistingStreamExpectation(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream := testStream(t, "account-42")
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectAnyVersion(),
		[]eventsourcing.PendingMessage{
			testPending(t, stream, "message-1", "account.opened"),
		},
	); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectExistingStream(),
		[]eventsourcing.PendingMessage{
			testPending(t, stream, "message-2", "account.email-changed"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].StreamVersion() != 2 {
		t.Fatalf("stored versions = %v, want [2]", messageVersions(stored))
	}
}

func testStream(t *testing.T, id string) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("bank.account", id)
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func testPending(
	t *testing.T,
	stream eventsourcing.StreamID,
	id string,
	name string,
) eventsourcing.PendingMessage {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        name,
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         id,
		Stream:     stream,
		Event:      event,
		RecordedAt: time.Date(2026, time.July, 25, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

func messageVersions(messages []eventsourcing.Message) []uint64 {
	versions := make([]uint64, len(messages))
	for index, message := range messages {
		versions[index] = message.StreamVersion()
	}

	return versions
}
