package gooutbox

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreAppendClassifiesTransactionOutcomes(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("driver diagnostic")
	tests := map[string]struct {
		beginner transactionBeginner
		config   eventpostgres.Config
		outcome  eventsourcing.CommitOutcome
		want     error
	}{
		"begin": {
			beginner: &fakeBeginner{err: sentinel},
			outcome:  eventsourcing.CommitNotCommitted,
			want:     sentinel,
		},
		"configuration": {
			beginner: &fakeBeginner{
				transaction: &fakeTransaction{},
			},
			config:  eventpostgres.Config{Schema: "unsupported"},
			outcome: eventsourcing.CommitNotCommitted,
		},
		"stage": {
			beginner: &fakeBeginner{
				transaction: &fakeTransaction{
					execErrors: map[int]error{0: sentinel},
				},
			},
			outcome: eventsourcing.CommitNotCommitted,
			want:    sentinel,
		},
		"commit": {
			beginner: &fakeBeginner{
				transaction: &fakeTransaction{
					rows:      []int64{0, 1, 1},
					commitErr: sentinel,
				},
			},
			outcome: eventsourcing.CommitUnknown,
			want:    sentinel,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := internalStore(t, test.beginner)
			store.eventConfig = test.config
			stream, pending := internalPending(t)
			_, err := store.Append(
				t.Context(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{pending},
			)
			if eventsourcing.AppendCommitOutcome(err) != test.outcome {
				t.Fatalf("Append error = %v, outcome = %d", err, test.outcome)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Append error = %v, want cause", err)
			}
		})
	}
}

func TestNewStoreAcceptsValidatedDependencies(t *testing.T) {
	t.Parallel()

	stager := mustStager(
		t,
		&fakeTransaction{},
		FixedTopic("account-events"),
	)
	store, err := NewStore(
		new(pgxpool.Pool),
		eventpostgres.Config{},
		stager.outbox,
		stager.codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("NewStore returned nil")
	}
}

func TestStoreAppendCommitsAndCleansUpTransaction(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{rows: []int64{0, 1, 1}}
	store := internalStore(
		t,
		&fakeBeginner{transaction: transaction},
	)
	stream, pending := internalPending(t)
	messages, err := store.Append(
		t.Context(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || transaction.commitCalls != 1 ||
		transaction.rollbackCalls != 1 {
		t.Fatalf(
			"messages=%d commits=%d rollbacks=%d",
			len(messages),
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
	}
}

func TestInvalidStoreOperationsFailWithoutDelegation(t *testing.T) {
	t.Parallel()

	var store *Store
	if _, err := store.Append(
		t.Context(),
		eventsourcing.StreamID{},
		eventsourcing.ExpectedVersion{},
		nil,
	); eventsourcing.AppendCommitOutcome(err) !=
		eventsourcing.CommitNotCommitted {
		t.Fatalf("nil Append error = %v", err)
	}
	if _, err := store.ReadStream(
		t.Context(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil ReadStream error = %v", err)
	}
	if _, err := store.ReadGlobal(
		t.Context(),
		eventsourcing.ReadGlobalOptions{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil ReadGlobal error = %v", err)
	}
}

func TestStoreDelegatesReadOperations(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("read failed")
	reader := &fakeEventReader{err: sentinel}
	store := &Store{events: reader}
	if _, err := store.ReadStream(
		t.Context(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	); !errors.Is(err, sentinel) {
		t.Fatalf("ReadStream error = %v", err)
	}
	if _, err := store.ReadGlobal(
		t.Context(),
		eventsourcing.ReadGlobalOptions{},
	); !errors.Is(err, sentinel) {
		t.Fatalf("ReadGlobal error = %v", err)
	}
	if reader.streamCalls != 1 || reader.globalCalls != 1 {
		t.Fatalf(
			"read calls = (%d, %d)",
			reader.streamCalls,
			reader.globalCalls,
		)
	}
}

func internalStore(
	t *testing.T,
	beginner transactionBeginner,
) *Store {
	t.Helper()

	stager := mustStager(
		t,
		&fakeTransaction{},
		FixedTopic("account-events"),
	)

	return &Store{
		beginner: beginner,
		events:   &fakeEventReader{},
		outbox:   stager.outbox,
		codec:    stager.codec,
	}
}

type fakeBeginner struct {
	transaction pgx.Tx
	err         error
}

func (beginner *fakeBeginner) BeginTx(
	context.Context,
	pgx.TxOptions,
) (pgx.Tx, error) {
	return beginner.transaction, beginner.err
}

type fakeEventReader struct {
	err         error
	streamCalls int
	globalCalls int
}

func (reader *fakeEventReader) ReadStream(
	context.Context,
	eventsourcing.StreamID,
	eventsourcing.ReadStreamOptions,
) (eventsourcing.MessageIterator, error) {
	reader.streamCalls++

	return nil, reader.err
}

func (reader *fakeEventReader) ReadGlobal(
	context.Context,
	eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	reader.globalCalls++

	return nil, reader.err
}

var (
	_ transactionBeginner = (*fakeBeginner)(nil)
	_ eventReader         = (*fakeEventReader)(nil)
)
