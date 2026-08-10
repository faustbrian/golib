package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
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

func oversizedStoredMetadataJSON(t testing.TB) []byte {
	t.Helper()

	metadata := make(map[string]string, 31)
	for index := range 31 {
		metadata[fmt.Sprintf("k%02d", index)] = strings.Repeat("\\", 2048)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= eventsourcing.MaxMetadataBytes {
		t.Fatalf("oversized metadata fixture = %d bytes", len(encoded))
	}

	return encoded
}

func maximumStoredMetadataJSON(t testing.TB) []byte {
	t.Helper()

	metadata := make(map[string]string, 15)
	for index := range 15 {
		metadata[fmt.Sprintf("k%02d", index)] = strings.Repeat("\\", 2180)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != maximumStoredMetadataJSONBytes {
		t.Fatalf("maximum metadata fixture = %d bytes", len(encoded))
	}

	return encoded
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
		"maximum":  {eventsourcing.ExpectAnyVersion(), math.MaxInt64 - 1},
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

func TestInsertMessageRejectsZeroAssignedPosition(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	_, err := insertMessage(
		context.Background(),
		&fakeDatabase{rowScans: []scanFunc{scanValues(int64(0))}},
		"messages",
		testPending(t, stream, "message-1"),
		1,
		0,
	)
	if err != eventsourcing.ErrCorruptHistory {
		t.Fatalf("insertMessage(zero position) error = %v", err)
	}
}

func TestTransactionWriterStagesPreparedSavePlan(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	plan := testAppendPlan{
		stream:   stream,
		expected: eventsourcing.ExpectNewStream(),
		pending: []eventsourcing.PendingMessage{
			testPending(t, stream, "planned-message"),
		},
	}
	database := appendDatabase(0, 17)
	messages, err := testWriter(database).StagePlan(
		context.Background(),
		plan,
	)
	if err != nil || len(messages) != 1 ||
		messages[0].StreamVersion() != 1 {
		t.Fatalf("StagePlan() = %#v, %v", messages, err)
	}

	if _, err := testWriter(&fakeDatabase{}).StagePlan(
		context.Background(),
		testAppendPlan{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("StagePlan(empty) error = %v", err)
	}
	var nilPlan AppendPlan
	if _, err := testWriter(&fakeDatabase{}).StagePlan(
		context.Background(),
		nilPlan,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("StagePlan(nil) error = %v", err)
	}
	var nilWriter *TxWriter
	if _, err := nilWriter.StagePlan(context.Background(), plan); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil StagePlan() error = %v", err)
	}
}

type testAppendPlan struct {
	stream   eventsourcing.StreamID
	expected eventsourcing.ExpectedVersion
	pending  []eventsourcing.PendingMessage
}

func (plan testAppendPlan) Stream() eventsourcing.StreamID {
	return plan.stream
}

func (plan testAppendPlan) ExpectedVersion() eventsourcing.ExpectedVersion {
	return plan.expected
}

func (plan testAppendPlan) PreparedMessages() []eventsourcing.PendingMessage {
	return append([]eventsourcing.PendingMessage(nil), plan.pending...)
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
	writer := newTxWriter(
		&Store{
			database: appendDatabase(0, 1),
			schema:   defaultSchema,
		},
	)
	staged, err := writer.Stage(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil || len(staged) != 1 {
		t.Fatalf("Stage() = %#v, %v", staged, err)
	}
	assertOperationPermitAvailable(t, writer.operation)

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

	waiting := testWriter(&fakeDatabase{})
	<-waiting.operation
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	result := make(chan error, 1)
	go func() {
		_, err := waiting.Stage(
			cancelled,
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{pending},
		)
		result <- err
	}()
	var waitErr error
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case waitErr = <-result:
	case <-timer.C:
		t.Fatal("waiting Stage() did not observe cancellation promptly")
	}
	if !errors.Is(waitErr, context.Canceled) ||
		eventsourcing.AppendCommitOutcome(waitErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("waiting Stage() = %v", waitErr)
	}
	released := false
	select {
	case <-waiting.operation:
		released = true
	default:
	}
	waiting.operation <- struct{}{}
	if released {
		t.Fatal("canceled waiter released the active operation permit")
	}
}

func assertOperationPermitAvailable(t testing.TB, permit operationPermit) {
	t.Helper()

	select {
	case <-permit:
		permit <- struct{}{}
	default:
		t.Fatal("completed operation retained its serialization permit")
	}
}

func assertDriverErrorRedacted(t testing.TB, err, cause error) {
	t.Helper()

	if !errors.Is(err, cause) {
		t.Fatalf("driver cause was not preserved: %v", err)
	}
	if !errors.Is(err, ErrDatabaseOperationFailed) {
		t.Fatalf("driver error was not classified: %v", err)
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("driver diagnostic was disclosed: %q", err)
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
	if err := validateAppend(
		&Store{},
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{message},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("validateAppend(nil database) error = %v", err)
	}
	validStore := &Store{database: &fakeDatabase{}}
	if err := validateAppend(
		validStore,
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{foreign},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("validateAppend(foreign stream) error = %v", err)
	}
	maximum := make([]eventsourcing.PendingMessage, eventsourcing.MaxAppendMessages)
	for index := range maximum {
		maximum[index] = testPending(
			t,
			stream,
			fmt.Sprintf("message-%d", index+1),
		)
	}
	if err := validateAppend(
		validStore,
		context.Background(),
		stream,
		eventsourcing.ExpectAnyVersion(),
		maximum,
	); err != nil {
		t.Fatalf("validateAppend(maximum batch) error = %v", err)
	}
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
			if name == "invalid position" && testCase.db.rowCalls != 2 {
				t.Fatalf("Append(invalid position) row calls = %d", testCase.db.rowCalls)
			}
		})
	}
}

func TestReconcileAppendClassifiesDurableIdentityAndFailures(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	pending := testPending(t, stream, "reconcile-1")
	expected := eventsourcing.ExpectNewStream()
	failure := errors.New("reconciliation read failure")
	storeWithTransaction := func(database *fakeDatabase) (*Store, *fakeTx) {
		tx := &fakeTx{fakeDatabase: database}

		return &Store{
			beginner: &fakeBeginner{tx: tx},
			database: database,
			schema:   defaultSchema,
		}, tx
	}
	var nilStore *Store
	if messages, outcome, err := nilStore.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("invalid reconciliation = %#v, %d, %v", messages, outcome, err)
	}
	invalid, _ := storeWithTransaction(&fakeDatabase{})
	if messages, outcome, err := invalid.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		nil,
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("invalid batch reconciliation = %#v, %d, %v", messages, outcome, err)
	}

	beginFailure := &Store{
		beginner: &fakeBeginner{err: failure},
		database: &fakeDatabase{},
		schema:   defaultSchema,
	}
	if messages, outcome, err := beginFailure.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		failure,
	) || !errors.Is(
		err,
		ErrAppendReconciliationFailed,
	) {
		t.Fatalf("begin failure = %#v, %d, %v", messages, outcome, err)
	} else if strings.Contains(err.Error(), failure.Error()) {
		t.Fatalf("begin failure disclosed driver diagnostic: %q", err)
	}

	barrierDatabase := &fakeDatabase{rowScans: []scanFunc{
		func([]any) error { return failure },
	}}
	barrierFailure, barrierTx := storeWithTransaction(barrierDatabase)
	if messages, outcome, err := barrierFailure.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		failure,
	) {
		t.Fatalf("barrier failure = %#v, %d, %v", messages, outcome, err)
	}
	if barrierTx.rollbackCalls != 1 {
		t.Fatalf("barrier failure rollback calls = %d", barrierTx.rollbackCalls)
	}

	corruptDatabase := &fakeDatabase{rowScans: []scanFunc{scanValues(int64(-1))}}
	corrupt, _ := storeWithTransaction(corruptDatabase)
	if messages, outcome, err := corrupt.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		eventsourcing.ErrCorruptHistory,
	) {
		t.Fatalf("corrupt barrier = %#v, %d, %v", messages, outcome, err)
	}
	for name, identity := range map[string][2]int64{
		"zero stream version":  {0, 1},
		"zero global position": {1, 0},
	} {
		corruptIdentityDatabase := &fakeDatabase{
			rowScans: []scanFunc{scanValues(int64(0))},
			rows: &fakeRows{scans: []scanFunc{
				reconciliationIdentityScan(
					pending,
					identity[0],
					identity[1],
				),
			}},
		}
		corruptIdentity, _ := storeWithTransaction(corruptIdentityDatabase)
		if messages, outcome, err := corruptIdentity.ReconcileAppend(
			context.Background(),
			stream,
			expected,
			[]eventsourcing.PendingMessage{pending},
		); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
			err,
			eventsourcing.ErrCorruptHistory,
		) {
			t.Fatalf(
				"%s corrupt identity = %#v, %d, %v",
				name,
				messages,
				outcome,
				err,
			)
		}
	}

	queryDatabase := &fakeDatabase{
		rowScans: []scanFunc{scanValues(int64(0))},
		queryErr: failure,
	}
	queryFailure, _ := storeWithTransaction(queryDatabase)
	if messages, outcome, err := queryFailure.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		failure,
	) {
		t.Fatalf("query failure = %#v, %d, %v", messages, outcome, err)
	}

	emptyRows := &fakeRows{}
	emptyDatabase := &fakeDatabase{
		rowScans:  []scanFunc{scanValues(int64(0))},
		queryRows: []pgx.Rows{emptyRows},
	}
	empty, _ := storeWithTransaction(emptyDatabase)
	if messages, outcome, err := empty.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitNotCommitted || err != nil {
		t.Fatalf("empty reconciliation = %#v, %d, %v", messages, outcome, err)
	}
	if !emptyRows.closed {
		t.Fatal("empty reconciliation did not close rows")
	}
	rollbackFailureRows := &fakeRows{}
	rollbackFailureDatabase := &fakeDatabase{
		rowScans:  []scanFunc{scanValues(int64(0))},
		queryRows: []pgx.Rows{rollbackFailureRows},
	}
	rollbackFailure, rollbackFailureTx := storeWithTransaction(
		rollbackFailureDatabase,
	)
	rollbackFailureTx.rollbackErr = failure
	if messages, outcome, err := rollbackFailure.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		failure,
	) {
		t.Fatalf("rollback failure = %#v, %d, %v", messages, outcome, err)
	}

	scanFailureRows := &fakeRows{scans: []scanFunc{
		func([]any) error { return failure },
	}}
	scanFailureDatabase := &fakeDatabase{
		rowScans:  []scanFunc{scanValues(int64(0))},
		queryRows: []pgx.Rows{scanFailureRows},
	}
	scanFailure, _ := storeWithTransaction(scanFailureDatabase)
	if messages, outcome, err := scanFailure.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		failure,
	) {
		t.Fatalf("scan failure = %#v, %d, %v", messages, outcome, err)
	}
	if !scanFailureRows.closed {
		t.Fatal("scan failure did not close rows")
	}

	rowFailureRows := &fakeRows{err: failure}
	rowFailureDatabase := &fakeDatabase{
		rowScans:  []scanFunc{scanValues(int64(0))},
		queryRows: []pgx.Rows{rowFailureRows},
	}
	rowFailure, _ := storeWithTransaction(rowFailureDatabase)
	if messages, outcome, err := rowFailure.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		failure,
	) {
		t.Fatalf("rows failure = %#v, %d, %v", messages, outcome, err)
	}

	identityRows := &fakeRows{scans: []scanFunc{
		reconciliationIdentityScan(pending, 1, 1),
	}}
	matchingRows := &fakeRows{scans: []scanFunc{
		messageScan(pending, 1, 1),
	}}
	database := &fakeDatabase{
		rowScans:  []scanFunc{scanValues(int64(0))},
		queryRows: []pgx.Rows{identityRows, matchingRows},
	}
	matching, matchingTx := storeWithTransaction(database)
	messages, outcome, err := matching.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil || outcome != eventsourcing.CommitCommitted || len(messages) != 1 {
		t.Fatalf("matching reconciliation = %#v, %d, %v", messages, outcome, err)
	}
	if !matchingRows.closed {
		t.Fatal("matching reconciliation did not close rows")
	}
	if matchingTx.rollbackCalls != 1 {
		t.Fatalf("matching reconciliation rollback calls = %d", matchingTx.rollbackCalls)
	}
	if len(database.queryArgs) != 1 || !reflect.DeepEqual(
		database.queryArgs[0],
		[]string{pending.ID().String()},
	) {
		t.Fatalf("reconciliation query arguments = %#v", database.queryArgs)
	}

	mismatchDatabase := &fakeDatabase{
		rowScans: []scanFunc{scanValues(int64(0))},
		queryRows: []pgx.Rows{&fakeRows{scans: []scanFunc{
			reconciliationIdentityScan(pending, 2, 2),
		}}},
	}
	mismatch, _ := storeWithTransaction(mismatchDatabase)
	if messages, outcome, err := mismatch.ReconcileAppend(
		context.Background(),
		stream,
		expected,
		[]eventsourcing.PendingMessage{pending},
	); messages != nil || outcome != eventsourcing.CommitUnknown || !errors.Is(
		err,
		ErrAppendReconciliationMismatch,
	) {
		t.Fatalf("mismatched reconciliation = %#v, %d, %v", messages, outcome, err)
	}
}

func TestReconciliationRequiresExactAtomicBatch(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	first := testPending(t, stream, "reconcile-1")
	second := testPending(t, stream, "reconcile-2")
	other := testPending(t, stream, "reconcile-other")
	message := func(
		pending eventsourcing.PendingMessage,
		version uint64,
		position eventsourcing.GlobalPosition,
	) eventsourcing.Message {
		result, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
			Pending:        pending,
			StreamVersion:  version,
			GlobalPosition: position,
		})
		if err != nil {
			t.Fatal(err)
		}

		return result
	}

	tests := map[string]struct {
		expected  eventsourcing.ExpectedVersion
		pending   []eventsourcing.PendingMessage
		persisted []eventsourcing.Message
		want      bool
	}{
		"exact batch": {
			expected:  eventsourcing.ExpectExactVersion(4),
			pending:   []eventsourcing.PendingMessage{first, second},
			persisted: []eventsourcing.Message{message(first, 5, 8), message(second, 6, 9)},
			want:      true,
		},
		"different lengths": {
			expected:  eventsourcing.ExpectNewStream(),
			pending:   []eventsourcing.PendingMessage{first, second},
			persisted: []eventsourcing.Message{message(first, 1, 1)},
		},
		"new stream starts later": {
			expected:  eventsourcing.ExpectNewStream(),
			pending:   []eventsourcing.PendingMessage{first},
			persisted: []eventsourcing.Message{message(first, 2, 2)},
		},
		"existing stream starts at one": {
			expected:  eventsourcing.ExpectExistingStream(),
			pending:   []eventsourcing.PendingMessage{first},
			persisted: []eventsourcing.Message{message(first, 1, 1)},
		},
		"stream version gap": {
			expected: eventsourcing.ExpectAnyVersion(),
			pending:  []eventsourcing.PendingMessage{first, second},
			persisted: []eventsourcing.Message{
				message(first, 2, 2),
				message(second, 4, 3),
			},
		},
		"global position gap": {
			expected: eventsourcing.ExpectAnyVersion(),
			pending:  []eventsourcing.PendingMessage{first, second},
			persisted: []eventsourcing.Message{
				message(first, 2, 2),
				message(second, 3, 4),
			},
		},
		"missing global position": {
			expected:  eventsourcing.ExpectAnyVersion(),
			pending:   []eventsourcing.PendingMessage{first},
			persisted: []eventsourcing.Message{message(first, 1, 0)},
		},
		"different envelope": {
			expected:  eventsourcing.ExpectAnyVersion(),
			pending:   []eventsourcing.PendingMessage{first},
			persisted: []eventsourcing.Message{message(other, 1, 1)},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := reconciliationMatches(
				test.expected,
				test.pending,
				test.persisted,
			); got != test.want {
				t.Fatalf("reconciliationMatches() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestReconcileAppendReleasesBarrierBeforeEnvelopeRead(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	pending := testPending(t, stream, "reconcile-release")
	barrierRows := &fakeRows{scans: []scanFunc{
		reconciliationIdentityScan(pending, 1, 1),
	}}
	barrierDatabase := &fakeDatabase{
		rowScans: []scanFunc{scanValues(int64(0))},
		rows:     barrierRows,
	}
	tx := &fakeTx{fakeDatabase: barrierDatabase}
	envelopeRows := &fakeRows{scans: []scanFunc{
		messageScan(pending, 1, 1),
	}}
	envelopeDatabase := &fakeDatabase{rows: envelopeRows}
	store := &Store{
		beginner: &fakeBeginner{tx: tx},
		database: envelopeDatabase,
		schema:   defaultSchema,
	}

	messages, outcome, err := store.ReconcileAppend(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil || outcome != eventsourcing.CommitCommitted || len(messages) != 1 {
		t.Fatalf("reconciliation = %#v, %d, %v", messages, outcome, err)
	}
	if tx.rollbackCalls != 1 {
		t.Fatalf("barrier rollback calls = %d", tx.rollbackCalls)
	}
	if !barrierRows.closed || !envelopeRows.closed {
		t.Fatalf(
			"row closure: barrier=%t envelope=%t",
			barrierRows.closed,
			envelopeRows.closed,
		)
	}
	if len(envelopeDatabase.queryArgs) != 1 {
		t.Fatalf("envelope query calls = %#v", envelopeDatabase.queryArgs)
	}
}

func TestReconcileAppendRejectsUnprovenEnvelopes(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	pending := testPending(t, stream, "reconcile-envelope")
	failure := errors.New("envelope read failure")
	tests := map[string]struct {
		database *fakeDatabase
		want     error
	}{
		"query failure": {
			database: &fakeDatabase{queryErr: failure},
			want:     failure,
		},
		"scan failure": {
			database: &fakeDatabase{rows: &fakeRows{scans: []scanFunc{
				func([]any) error { return failure },
			}}},
			want: failure,
		},
		"rows failure": {
			database: &fakeDatabase{rows: &fakeRows{err: failure}},
			want:     failure,
		},
		"envelope changed after identity proof": {
			database: &fakeDatabase{rows: &fakeRows{scans: []scanFunc{
				messageScan(pending, 2, 1),
			}}},
			want: ErrAppendReconciliationMismatch,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			barrierRows := &fakeRows{scans: []scanFunc{
				reconciliationIdentityScan(pending, 1, 1),
			}}
			tx := &fakeTx{fakeDatabase: &fakeDatabase{
				rowScans: []scanFunc{scanValues(int64(0))},
				rows:     barrierRows,
			}}
			store := &Store{
				beginner: &fakeBeginner{tx: tx},
				database: test.database,
				schema:   defaultSchema,
			}

			messages, outcome, err := store.ReconcileAppend(
				context.Background(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{pending},
			)
			if messages != nil || outcome != eventsourcing.CommitUnknown ||
				!errors.Is(err, test.want) {
				t.Fatalf("reconciliation = %#v, %d, %v", messages, outcome, err)
			}
			if tx.rollbackCalls != 1 || !barrierRows.closed {
				t.Fatalf(
					"barrier cleanup: rollbacks=%d rows_closed=%t",
					tx.rollbackCalls,
					barrierRows.closed,
				)
			}
			if test.database.rows != nil && !test.database.rows.(*fakeRows).closed {
				t.Fatal("envelope rows were not closed")
			}
		})
	}
}

func TestReconciliationIdentitiesRequireExactAtomicBatch(t *testing.T) {
	t.Parallel()

	stream := testStream(t)
	first := testPending(t, stream, "reconcile-identity-1")
	second := testPending(t, stream, "reconcile-identity-2")
	exact := []appendReconciliationIdentity{
		{id: first.ID().String(), streamVersion: 4, globalPosition: 8},
		{id: second.ID().String(), streamVersion: 5, globalPosition: 9},
	}
	tests := map[string]struct {
		pending    []eventsourcing.PendingMessage
		identities []appendReconciliationIdentity
	}{
		"different lengths": {
			pending:    []eventsourcing.PendingMessage{first, second},
			identities: exact[:1],
		},
		"different message ID": {
			pending: []eventsourcing.PendingMessage{second, first},
			identities: []appendReconciliationIdentity{
				exact[0],
				exact[1],
			},
		},
		"stream version gap": {
			pending: []eventsourcing.PendingMessage{first, second},
			identities: []appendReconciliationIdentity{
				exact[0],
				{id: second.ID().String(), streamVersion: 6, globalPosition: 9},
			},
		},
		"global position gap": {
			pending: []eventsourcing.PendingMessage{first, second},
			identities: []appendReconciliationIdentity{
				exact[0],
				{id: second.ID().String(), streamVersion: 5, globalPosition: 10},
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if reconciliationIdentitiesMatch(
				eventsourcing.ExpectExactVersion(3),
				test.pending,
				test.identities,
			) {
				t.Fatal("reconciliation identities unexpectedly matched")
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
			readErr := operation()
			if readErr == nil {
				t.Fatal("read unexpectedly succeeded")
			}
			if strings.Contains(name, "failure") {
				assertDriverErrorRedacted(t, readErr, failure)
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

	bounded, possible := postgresBound(math.MaxInt64)
	if !possible || bounded != math.MaxInt64 {
		t.Fatalf("postgresBound(maximum) = %d, %t", bounded, possible)
	}

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
	return newTxWriter(&Store{database: database, schema: defaultSchema})
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
	execErrs   []error
	execTags   []pgconn.CommandTag
	rowScans   []scanFunc
	rows       pgx.Rows
	queryErr   error
	queryArgs  []any
	queryRows  []pgx.Rows
	execCalls  int
	queryCalls int
	rowCalls   int
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
	rollbackErr   error
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
	if tx.rollbackErr != nil {
		return tx.rollbackErr
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
	index := database.queryCalls
	database.queryCalls++
	database.queryArgs = append([]any(nil), arguments...)
	if database.queryErr != nil {
		return nil, database.queryErr
	}
	if index < len(database.queryRows) {
		return database.queryRows[index], nil
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

func messageScan(
	pending eventsourcing.PendingMessage,
	streamVersion int64,
	globalPosition int64,
) scanFunc {
	correlationID, hasCorrelationID := pending.CorrelationID()
	causationID, hasCausationID := pending.CausationID()
	tenant, hasTenant := pending.Tenant()
	partition, hasPartition := pending.Partition()
	optional := func(value string, exists bool) *string {
		if !exists {
			return nil
		}

		return &value
	}
	event := pending.Event()

	return scanValues(
		globalPosition,
		pending.ID().String(),
		pending.Stream().AggregateType(),
		pending.Stream().AggregateID(),
		streamVersion,
		event.Name().String(),
		int64(event.Version()),
		event.ContentType(),
		event.Payload(),
		encodeMetadata(pending.Metadata()),
		pending.RecordedAt(),
		optional(correlationID.String(), hasCorrelationID),
		optional(causationID.String(), hasCausationID),
		optional(tenant, hasTenant),
		optional(partition, hasPartition),
	)
}

func reconciliationIdentityScan(
	pending eventsourcing.PendingMessage,
	streamVersion int64,
	globalPosition int64,
) scanFunc {
	return scanValues(
		pending.ID().String(),
		streamVersion,
		globalPosition,
	)
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
