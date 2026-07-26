package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRollbackContextIsIndependentAndBounded(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	ctx, cancel := rollbackContext(parent)
	defer cancel()

	if ctx.Err() != nil {
		t.Fatalf("rollback context inherited cancellation: %v", ctx.Err())
	}
	deadline, exists := ctx.Deadline()
	if !exists {
		t.Fatal("rollback context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > transactionCleanupTimeout {
		t.Fatalf("rollback context remaining time = %s", remaining)
	}
}

func TestEncodeMetadataIsDeterministicJSON(t *testing.T) {
	t.Parallel()

	encoded := encodeMetadata(map[string]string{
		"z": "last",
		"a": "quoted\nvalue",
	})
	if string(encoded) != `{"a":"quoted\nvalue","z":"last"}` {
		t.Fatalf("encodeMetadata() = %s", encoded)
	}
	if string(encodeMetadata(nil)) != "{}" {
		t.Fatalf("encodeMetadata(nil) = %s", encodeMetadata(nil))
	}
}

func TestTransactionStoreAppendsAndClassifiesExpectedVersions(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	pending := testPending(t, stream, "message-1")
	for name, testCase := range map[string]struct {
		expected eventsourcing.ExpectedVersion
		current  int64
	}{
		"new":      {eventsourcing.ExpectNewStream(), 0},
		"existing": {eventsourcing.ExpectExistingStream(), 2},
		"exact":    {eventsourcing.ExpectExactVersion(2), 2},
		"any":      {eventsourcing.ExpectAnyVersion(), 7},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := appendDatabase(testCase.current, 11)
			writer := testWriter(db)
			stored, err := writer.Stage(
				context.Background(),
				stream,
				testCase.expected,
				[]eventsourcing.PendingMessage{pending},
			)
			if err != nil ||
				len(stored) != 1 ||
				stored[0].StreamVersion() !=
					uint64(testCase.current)+1 {
				t.Fatalf("Append() = %#v, %v", stored, err)
			}
			position, exists := stored[0].GlobalPosition()
			if !exists || position != 11 || db.execCalls != 2 ||
				db.rowCalls != 3 {
				t.Fatalf(
					"stored position/calls = %d, %t, %d, %d",
					position,
					exists,
					db.execCalls,
					db.rowCalls,
				)
			}
		})
	}
}

func TestTransactionStoreAppendsMessageWithoutOptionalEnvelopeFields(
	t *testing.T,
) {
	t.Parallel()

	stream := testStream(t)
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.changed",
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte(`{}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-minimal",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Unix(1, 0).UTC(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	writer := testWriter(appendDatabase(0, 1))
	if _, err := writer.Stage(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatal(err)
	}
}

func TestPoolBackedAppendOwnsCommitAndRollback(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	pending := testPending(t, stream, "message-1")
	failure := errors.New("transaction failure")
	tests := map[string]struct {
		beginner *fakeBeginner
		want     error
		outcome  eventsourcing.CommitOutcome
	}{
		"success": {
			beginner: &fakeBeginner{
				tx: &fakeTx{fakeDatabase: appendDatabase(0, 1)},
			},
			outcome: eventsourcing.CommitUnknown,
		},
		"begin": {
			beginner: &fakeBeginner{err: failure},
			want:     failure,
			outcome:  eventsourcing.CommitNotCommitted,
		},
		"append": {
			beginner: &fakeBeginner{
				tx: &fakeTx{
					fakeDatabase: &fakeDatabase{
						execErrs: []error{failure},
					},
				},
			},
			want:    failure,
			outcome: eventsourcing.CommitNotCommitted,
		},
		"commit": {
			beginner: &fakeBeginner{
				tx: &fakeTx{
					fakeDatabase: appendDatabase(0, 1),
					commitErr:    failure,
				},
			},
			want:    failure,
			outcome: eventsourcing.CommitUnknown,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &Store{
				beginner: testCase.beginner,
				database: &fakeDatabase{},
				schema:   defaultSchema,
			}
			stored, err := store.Append(
				context.Background(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{pending},
			)
			if testCase.want == nil {
				if err != nil || len(stored) != 1 {
					t.Fatalf("Append() = %#v, %v", stored, err)
				}
			} else if !errors.Is(err, testCase.want) ||
				eventsourcing.AppendCommitOutcome(err) != testCase.outcome {
				t.Fatalf("Append() = %v", err)
			}
			if testCase.beginner.tx != nil &&
				testCase.beginner.tx.rollbackCalls != 1 {
				t.Fatalf(
					"rollback calls = %d",
					testCase.beginner.tx.rollbackCalls,
				)
			}
		})
	}

	var nilContext context.Context
	store := &Store{
		beginner: &fakeBeginner{},
		database: &fakeDatabase{},
		schema:   defaultSchema,
	}
	if _, err := store.Append(
		nilContext,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); eventsourcing.AppendCommitOutcome(err) !=
		eventsourcing.CommitNotCommitted {
		t.Fatalf("Append(nil context) = %v", err)
	}
}

func TestConstructorsAcceptPoolAndTransaction(t *testing.T) {
	t.Parallel()

	poolStore, err := New(&pgxpool.Pool{}, Config{})
	if err != nil || poolStore == nil {
		t.Fatalf("New() = %#v, %v", poolStore, err)
	}
	tx := &fakeTx{fakeDatabase: &fakeDatabase{}}
	if store, invalidErr := NewTx(
		tx,
		Config{Schema: "other"},
	); store != nil ||
		!errors.Is(invalidErr, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewTx(invalid config) = %#v, %v", store, invalidErr)
	}
	txWriter, err := NewTx(tx, Config{})
	if err != nil || txWriter == nil || txWriter.store.database != tx {
		t.Fatalf("NewTx() = %#v, %v", txWriter, err)
	}
	if _, implementsStore := any(txWriter).(eventsourcing.EventStore); implementsStore {
		t.Fatal("caller-owned transaction writer implements EventStore")
	}
}

func TestTransactionWriterStagesWithoutClaimingDurableCommit(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	pending := testPending(t, stream, "message-1")
	writer := &TxWriter{
		store: &Store{
			database: appendDatabase(0, 1),
			schema:   defaultSchema,
		},
	}
	staged, err := writer.Stage(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil || len(staged) != 1 {
		t.Fatalf("Stage() = %#v, %v", staged, err)
	}

	var nilWriter *TxWriter
	if _, err := nilWriter.Stage(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); eventsourcing.AppendCommitOutcome(err) !=
		eventsourcing.CommitNotCommitted {
		t.Fatalf("nil Stage() = %v", err)
	}
}

func TestAppendRejectsInvalidInputBeforeDatabaseUse(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	other, err := eventsourcing.NewStreamID("account", "other")
	if err != nil {
		t.Fatal(err)
	}
	message := testPending(t, stream, "message-1")
	foreign := testPending(t, other, "message-2")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var nilContext context.Context
	var nilStore *Store
	tests := map[string]func() error{
		"nil store": func() error {
			_, appendErr := nilStore.Append(
				context.Background(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{message},
			)

			return appendErr
		},
		"nil context": func() error {
			_, appendErr := testWriter(&fakeDatabase{}).Stage(
				nilContext,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{message},
			)

			return appendErr
		},
		"invalid pool store": func() error {
			_, appendErr := (&Store{}).Append(
				context.Background(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{message},
			)

			return appendErr
		},
		"cancelled": func() error {
			_, appendErr := testWriter(&fakeDatabase{}).Stage(
				cancelled,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{message},
			)

			return appendErr
		},
		"zero stream": func() error {
			_, appendErr := testWriter(&fakeDatabase{}).Stage(
				context.Background(),
				eventsourcing.StreamID{},
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{message},
			)

			return appendErr
		},
		"invalid expected": func() error {
			_, appendErr := testWriter(&fakeDatabase{}).Stage(
				context.Background(),
				stream,
				eventsourcing.ExpectedVersion{},
				[]eventsourcing.PendingMessage{message},
			)

			return appendErr
		},
		"empty batch": func() error {
			_, appendErr := testWriter(&fakeDatabase{}).Stage(
				context.Background(),
				stream,
				eventsourcing.ExpectNewStream(),
				nil,
			)

			return appendErr
		},
		"foreign stream": func() error {
			_, appendErr := testWriter(&fakeDatabase{}).Stage(
				context.Background(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{foreign},
			)

			return appendErr
		},
		"duplicate batch ID": func() error {
			_, appendErr := testWriter(&fakeDatabase{}).Stage(
				context.Background(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{message, message},
			)

			return appendErr
		},
	}
	for name, operation := range tests {
		operation := operation
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := operation()
			if eventsourcing.AppendCommitOutcome(err) !=
				eventsourcing.CommitNotCommitted {
				t.Fatalf("Append() = %v", err)
			}
		})
	}

	oversized := make(
		[]eventsourcing.PendingMessage,
		eventsourcing.MaxAppendMessages+1,
	)
	copy(oversized, []eventsourcing.PendingMessage{message})
	if _, err := testWriter(&fakeDatabase{}).Stage(
		context.Background(),
		stream,
		eventsourcing.ExpectAnyVersion(),
		oversized,
	); eventsourcing.AppendCommitOutcome(err) !=
		eventsourcing.CommitNotCommitted {
		t.Fatalf("oversized Append() = %v", err)
	}
}

func TestAppendPreservesDatabaseAndIntegrityFailures(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	pending := testPending(t, stream, "message-1")
	failure := errors.New("database failure")
	duplicate := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "messages_message_id_unique",
	}
	tests := map[string]struct {
		db       *fakeDatabase
		expected eventsourcing.ExpectedVersion
		want     error
	}{
		"create stream": {
			db:   &fakeDatabase{execErrs: []error{failure}},
			want: failure,
		},
		"load version": {
			db: &fakeDatabase{
				rowScans: []scanFunc{
					func([]any) error { return failure },
				},
			},
			want: failure,
		},
		"negative version": {
			db: &fakeDatabase{
				rowScans: []scanFunc{scanValues(int64(-1))},
			},
			want: eventsourcing.ErrCorruptHistory,
		},
		"version overflow": {
			db: &fakeDatabase{
				rowScans: []scanFunc{scanValues(int64(^uint64(0) >> 1))},
			},
			expected: eventsourcing.ExpectAnyVersion(),
			want:     eventsourcing.ErrVersionOverflow,
		},
		"stale expectation": {
			db: &fakeDatabase{
				rowScans: []scanFunc{scanValues(int64(2))},
			},
			expected: eventsourcing.ExpectNewStream(),
			want:     eventsourcing.ErrConcurrencyConflict,
		},
		"position overflow": {
			db: &fakeDatabase{
				rowScans: []scanFunc{
					scanValues(int64(0)),
					func([]any) error { return pgx.ErrNoRows },
				},
			},
			want: eventsourcing.ErrVersionOverflow,
		},
		"position update": {
			db: &fakeDatabase{
				rowScans: []scanFunc{
					scanValues(int64(0)),
					func([]any) error { return failure },
				},
			},
			want: failure,
		},
		"invalid position": {
			db: &fakeDatabase{
				rowScans: []scanFunc{
					scanValues(int64(0)),
					scanValues(int64(0)),
				},
			},
			want: eventsourcing.ErrCorruptHistory,
		},
		"duplicate message": {
			db: &fakeDatabase{
				rowScans: []scanFunc{
					scanValues(int64(0)),
					scanValues(int64(1)),
					func([]any) error { return duplicate },
				},
			},
			want: eventsourcing.ErrDuplicateMessageID,
		},
		"message insert": {
			db: &fakeDatabase{
				rowScans: []scanFunc{
					scanValues(int64(0)),
					scanValues(int64(1)),
					func([]any) error { return failure },
				},
			},
			want: failure,
		},
		"position mismatch": {
			db: &fakeDatabase{
				rowScans: []scanFunc{
					scanValues(int64(0)),
					scanValues(int64(1)),
					scanValues(int64(2)),
				},
			},
			want: eventsourcing.ErrCorruptHistory,
		},
		"stream update": {
			db: &fakeDatabase{
				execErrs: []error{nil, failure},
				rowScans: []scanFunc{
					scanValues(int64(0)),
					scanValues(int64(1)),
					scanValues(int64(1)),
				},
			},
			want: failure,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			expected := testCase.expected
			if !expected.Valid() {
				expected = eventsourcing.ExpectNewStream()
			}
			writer := testWriter(testCase.db)
			_, err := writer.Stage(
				context.Background(),
				stream,
				expected,
				[]eventsourcing.PendingMessage{pending},
			)
			if !errors.Is(err, testCase.want) ||
				eventsourcing.AppendCommitOutcome(err) !=
					eventsourcing.CommitNotCommitted {
				t.Fatalf("Append() = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestReadBoundariesPreserveStoreSemantics(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	streamOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	globalOptions, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("read failure")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var nilContext context.Context

	for name, operation := range map[string]func() error{
		"nil stream store": func() error {
			var store *Store
			_, readErr := store.ReadStream(
				context.Background(),
				stream,
				streamOptions,
			)

			return readErr
		},
		"nil stream context": func() error {
			_, readErr := (&Store{}).ReadStream(
				nilContext,
				stream,
				streamOptions,
			)

			return readErr
		},
		"cancelled stream": func() error {
			_, readErr := (&Store{database: &fakeDatabase{}}).ReadStream(
				cancelled,
				stream,
				streamOptions,
			)

			return readErr
		},
		"missing stream": func() error {
			_, readErr := (&Store{
				database: &fakeDatabase{
					rowScans: []scanFunc{
						func([]any) error { return pgx.ErrNoRows },
					},
				},
				schema: defaultSchema,
			}).ReadStream(
				context.Background(),
				stream,
				streamOptions,
			)

			return readErr
		},
		"stream status failure": func() error {
			_, readErr := (&Store{
				database: &fakeDatabase{
					rowScans: []scanFunc{
						func([]any) error { return failure },
					},
				},
				schema: defaultSchema,
			}).ReadStream(
				context.Background(),
				stream,
				streamOptions,
			)

			return readErr
		},
		"negative stream version": func() error {
			_, readErr := (&Store{
				database: &fakeDatabase{
					rowScans: []scanFunc{scanValues(int64(-1))},
				},
				schema: defaultSchema,
			}).ReadStream(
				context.Background(),
				stream,
				streamOptions,
			)

			return readErr
		},
		"zero stream version": func() error {
			_, readErr := (&Store{
				database: &fakeDatabase{
					rowScans: []scanFunc{scanValues(int64(0))},
				},
				schema: defaultSchema,
			}).ReadStream(
				context.Background(),
				stream,
				streamOptions,
			)

			return readErr
		},
		"stream query failure": func() error {
			_, readErr := (&Store{
				database: &fakeDatabase{
					rowScans: []scanFunc{scanValues(int64(1))},
					queryErr: failure,
				},
				schema: defaultSchema,
			}).ReadStream(
				context.Background(),
				stream,
				streamOptions,
			)

			return readErr
		},
		"nil global store": func() error {
			var store *Store
			_, readErr := store.ReadGlobal(
				context.Background(),
				globalOptions,
			)

			return readErr
		},
		"nil global context": func() error {
			_, readErr := (&Store{}).ReadGlobal(
				nilContext,
				globalOptions,
			)

			return readErr
		},
		"cancelled global": func() error {
			_, readErr := (&Store{database: &fakeDatabase{}}).ReadGlobal(
				cancelled,
				globalOptions,
			)

			return readErr
		},
		"global query failure": func() error {
			_, readErr := (&Store{
				database: &fakeDatabase{queryErr: failure},
				schema:   defaultSchema,
			}).ReadGlobal(context.Background(), globalOptions)

			return readErr
		},
	} {
		operation := operation
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := operation(); err == nil {
				t.Fatal("read unexpectedly succeeded")
			}
		})
	}

	rows := &fakeRows{}
	db := &fakeDatabase{
		rowScans: []scanFunc{scanValues(int64(1))},
		rows:     rows,
	}
	store := &Store{database: db, schema: defaultSchema}
	iterator, err := store.ReadStream(
		context.Background(),
		stream,
		streamOptions,
	)
	if err != nil || iterator == nil {
		t.Fatalf("ReadStream() = %#v, %v", iterator, err)
	}
	globalRows := &fakeRows{}
	store.database = &fakeDatabase{rows: globalRows}
	iterator, err = store.ReadGlobal(context.Background(), globalOptions)
	if err != nil || iterator == nil {
		t.Fatalf("ReadGlobal() = %#v, %v", iterator, err)
	}
}

func TestReadsMapUint64RangesWithoutOverflow(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	from := uint64(^uint64(0)>>1) + 1
	streamOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: from,
			ToVersion:   ^uint64(0),
			Limit:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	streamDB := &fakeDatabase{
		rowScans: []scanFunc{scanValues(int64(1))},
		rows:     &fakeRows{},
	}
	store := &Store{database: streamDB, schema: defaultSchema}
	if _, err := store.ReadStream(
		context.Background(),
		stream,
		streamOptions,
	); err != nil {
		t.Fatal(err)
	}
	if len(streamDB.queryArgs) != 6 ||
		streamDB.queryArgs[2] != int64(^uint64(0)>>1) ||
		streamDB.queryArgs[3] != int64(^uint64(0)>>1) ||
		streamDB.queryArgs[5] != false {
		t.Fatalf("stream query arguments = %#v", streamDB.queryArgs)
	}

	globalOptions, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: eventsourcing.GlobalPosition(from),
			ToPosition:   eventsourcing.GlobalPosition(^uint64(0)),
			Limit:        1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	globalDB := &fakeDatabase{rows: &fakeRows{}}
	store.database = globalDB
	if _, err := store.ReadGlobal(
		context.Background(),
		globalOptions,
	); err != nil {
		t.Fatal(err)
	}
	if len(globalDB.queryArgs) != 4 ||
		globalDB.queryArgs[0] != int64(^uint64(0)>>1) ||
		globalDB.queryArgs[1] != int64(^uint64(0)>>1) ||
		globalDB.queryArgs[3] != false {
		t.Fatalf("global query arguments = %#v", globalDB.queryArgs)
	}
}

func testStream(t testing.TB) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func testWriter(database database) *TxWriter {
	return &TxWriter{
		store: &Store{database: database, schema: defaultSchema},
	}
}

func testPending(
	t testing.TB,
	stream eventsourcing.StreamID,
	id string,
) eventsourcing.PendingMessage {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.changed",
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte(`{"changed":true}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            id,
			Stream:        stream,
			Event:         event,
			Metadata:      map[string]string{"source": "test"},
			RecordedAt:    time.Unix(1, 123456000).UTC(),
			CorrelationID: "correlation-1",
			CausationID:   "causation-1",
			Tenant:        "tenant-1",
			Partition:     "partition-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return message
}

type scanFunc func([]any) error

type fakeRow struct {
	scan scanFunc
}

func (row fakeRow) Scan(destinations ...any) error {
	if row.scan == nil {
		return nil
	}

	return row.scan(destinations)
}

type fakeDatabase struct {
	execErrs  []error
	execTags  []pgconn.CommandTag
	rowScans  []scanFunc
	rows      pgx.Rows
	queryErr  error
	queryArgs []any
	execCalls int
	rowCalls  int
}

type fakeBeginner struct {
	tx  *fakeTx
	err error
}

func (beginner *fakeBeginner) BeginTx(
	context.Context,
	pgx.TxOptions,
) (pgx.Tx, error) {
	if beginner.err != nil {
		return nil, beginner.err
	}

	return beginner.tx, nil
}

type fakeTx struct {
	pgx.Tx
	*fakeDatabase
	commitErr     error
	rollbackCalls int
}

func (tx *fakeTx) Exec(
	ctx context.Context,
	query string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	return tx.fakeDatabase.Exec(ctx, query, arguments...)
}

func (tx *fakeTx) Query(
	ctx context.Context,
	query string,
	arguments ...any,
) (pgx.Rows, error) {
	return tx.fakeDatabase.Query(ctx, query, arguments...)
}

func (tx *fakeTx) QueryRow(
	ctx context.Context,
	query string,
	arguments ...any,
) pgx.Row {
	return tx.fakeDatabase.QueryRow(ctx, query, arguments...)
}

func (tx *fakeTx) Commit(context.Context) error {
	return tx.commitErr
}

func (tx *fakeTx) Rollback(ctx context.Context) error {
	tx.rollbackCalls++
	if _, exists := ctx.Deadline(); !exists {
		return errors.New("rollback context is unbounded")
	}

	return nil
}

func (database *fakeDatabase) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	index := database.execCalls
	database.execCalls++
	if index < len(database.execErrs) {
		return pgconn.CommandTag{}, database.execErrs[index]
	}
	if index < len(database.execTags) {
		return database.execTags[index], nil
	}

	return pgconn.NewCommandTag("OK"), nil
}

func (database *fakeDatabase) Query(
	_ context.Context,
	_ string,
	arguments ...any,
) (pgx.Rows, error) {
	database.queryArgs = append([]any(nil), arguments...)
	if database.queryErr != nil {
		return nil, database.queryErr
	}
	if database.rows == nil {
		database.rows = &fakeRows{}
	}

	return database.rows, nil
}

func (database *fakeDatabase) QueryRow(
	context.Context,
	string,
	...any,
) pgx.Row {
	index := database.rowCalls
	database.rowCalls++
	if index >= len(database.rowScans) {
		return fakeRow{}
	}

	return fakeRow{scan: database.rowScans[index]}
}

func appendDatabase(current, position int64) *fakeDatabase {
	return &fakeDatabase{
		rowScans: []scanFunc{
			scanValues(current),
			scanValues(position),
			scanValues(position),
		},
	}
}

func scanValues(values ...any) scanFunc {
	return func(destinations []any) error {
		if len(destinations) != len(values) {
			return fmt.Errorf(
				"destination count %d differs from value count %d",
				len(destinations),
				len(values),
			)
		}
		for index, value := range values {
			destination := reflect.ValueOf(destinations[index])
			if destination.Kind() != reflect.Pointer ||
				destination.IsNil() {
				return errors.New("scan destination is not a pointer")
			}
			source := reflect.ValueOf(value)
			if !source.Type().AssignableTo(destination.Elem().Type()) {
				return fmt.Errorf(
					"cannot assign %T to %T",
					value,
					destinations[index],
				)
			}
			destination.Elem().Set(source)
		}

		return nil
	}
}

type fakeRows struct {
	scans  []scanFunc
	index  int
	err    error
	closed bool
}

func (rows *fakeRows) Close() {
	rows.closed = true
}

func (rows *fakeRows) Err() error {
	return rows.err
}

func (*fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (*fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (rows *fakeRows) Next() bool {
	if rows.index >= len(rows.scans) {
		rows.closed = true

		return false
	}
	rows.index++

	return true
}

func (rows *fakeRows) Scan(destinations ...any) error {
	return rows.scans[rows.index-1](destinations)
}

func (*fakeRows) Values() ([]any, error) {
	return nil, errors.New("not implemented")
}

func (*fakeRows) RawValues() [][]byte {
	return nil
}

func (*fakeRows) Conn() *pgx.Conn {
	return nil
}

var (
	_ database = (*fakeDatabase)(nil)
	_ pgx.Rows = (*fakeRows)(nil)
)
