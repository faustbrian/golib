package gooutbox

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/outbox"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestStagerStagesEventsBeforeOutboxEnvelopes(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{rows: []int64{0, 1, 1}}
	stager := mustStager(t, transaction, FixedTopic("account-events"))
	stream, pending := internalPending(t)

	messages, err := stager.Stage(
		t.Context(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("staged messages = %#v", messages)
	}
	position, exists := messages[0].GlobalPosition()
	if !exists ||
		position != eventsourcing.GlobalPosition(1) {
		t.Fatalf("staged messages = %#v", messages)
	}
	if transaction.execCalls != 3 {
		t.Fatalf("Exec calls = %d, want 3", transaction.execCalls)
	}
}

func TestStagerStagesPreparedSavePlan(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{rows: []int64{0, 1, 1}}
	stager := mustStager(t, transaction, FixedTopic("account-events"))
	stream, pending := internalPending(t)
	plan := stagerAppendPlan{
		stream:   stream,
		expected: eventsourcing.ExpectNewStream(),
		pending:  []eventsourcing.PendingMessage{pending},
	}
	messages, err := stager.StagePlan(t.Context(), plan)
	if err != nil || len(messages) != 1 || transaction.execCalls != 3 {
		t.Fatalf(
			"StagePlan() = %#v, %v; Exec calls = %d",
			messages,
			err,
			transaction.execCalls,
		)
	}

	var nilPlan AppendPlan
	if _, err := stager.StagePlan(t.Context(), nilPlan); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("StagePlan(nil) error = %v", err)
	}
}

type stagerAppendPlan struct {
	stream   eventsourcing.StreamID
	expected eventsourcing.ExpectedVersion
	pending  []eventsourcing.PendingMessage
}

func (plan stagerAppendPlan) Stream() eventsourcing.StreamID {
	return plan.stream
}

func (plan stagerAppendPlan) ExpectedVersion() eventsourcing.ExpectedVersion {
	return plan.expected
}

func (plan stagerAppendPlan) PreparedMessages() []eventsourcing.PendingMessage {
	return append([]eventsourcing.PendingMessage(nil), plan.pending...)
}

func TestStagerClassifiesEventMappingAndOutboxFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sensitive driver diagnostic")
	tests := map[string]struct {
		transaction *fakeTransaction
		resolver    TopicResolver
		want        error
	}{
		"event": {
			transaction: &fakeTransaction{
				execErrors: map[int]error{0: sentinel},
			},
			resolver: FixedTopic("account-events"),
			want:     sentinel,
		},
		"mapping": {
			transaction: &fakeTransaction{rows: []int64{0, 1, 1}},
			resolver:    FixedTopic(""),
			want:        ErrEnvelopeEncoding,
		},
		"outbox": {
			transaction: &fakeTransaction{
				rows:       []int64{0, 1, 1},
				execErrors: map[int]error{2: sentinel},
			},
			resolver: FixedTopic("account-events"),
			want:     ErrOutboxWrite,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stager := mustStager(t, test.transaction, test.resolver)
			stream, pending := internalPending(t)
			_, err := stager.Stage(
				t.Context(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{pending},
			)
			if !errors.Is(err, test.want) ||
				eventsourcing.AppendCommitOutcome(err) !=
					eventsourcing.CommitNotCommitted {
				t.Fatalf("Stage error = %v, want %v", err, test.want)
			}
			if name != "mapping" && !errors.Is(err, sentinel) {
				t.Fatalf("Stage error does not preserve cause: %v", err)
			}
		})
	}
}

func TestStageErrorRedactsAndPreservesCause(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("secret diagnostic")
	err := stageFailure(ErrOutboxWrite, sentinel)
	var stageErr *StageError
	if err.Error() != ErrOutboxWrite.Error() ||
		!errors.Is(err, ErrOutboxWrite) ||
		!errors.Is(err, sentinel) ||
		!errors.As(err, &stageErr) ||
		stageErr.CommitOutcome() != eventsourcing.CommitNotCommitted {
		t.Fatalf("stage error = %#v", err)
	}
}

func TestNewStagerRejectsInvalidEventConfiguration(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{}
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewEnvelopeCodec(
		FixedTopic("account-events"),
		outbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStager(
		transaction,
		eventpostgres.Config{Schema: "unsupported"},
		writer,
		codec,
	); err == nil {
		t.Fatal("NewStager accepted unsupported event schema")
	}
}

func mustStager(
	t *testing.T,
	transaction pgx.Tx,
	resolver TopicResolver,
) *Stager {
	t.Helper()

	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewEnvelopeCodec(resolver, outbox.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	stager, err := NewStager(
		transaction,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}

	return stager
}

func internalPending(
	t testing.TB,
) (eventsourcing.StreamID, eventsourcing.PendingMessage) {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.opened",
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     []byte(`{"owner":"Ada"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-1",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return stream, pending
}

type fakeTransaction struct {
	rows          []int64
	rowIndex      int
	execErrors    map[int]error
	execCalls     int
	commitErr     error
	commitCalls   int
	rollbackCalls int
}

func (transaction *fakeTransaction) Begin(
	context.Context,
) (pgx.Tx, error) {
	return nil, errors.New("unexpected Begin")
}

func (transaction *fakeTransaction) Commit(context.Context) error {
	transaction.commitCalls++

	return transaction.commitErr
}

func (transaction *fakeTransaction) Rollback(context.Context) error {
	transaction.rollbackCalls++

	return nil
}

func (transaction *fakeTransaction) CopyFrom(
	context.Context,
	pgx.Identifier,
	[]string,
	pgx.CopyFromSource,
) (int64, error) {
	return 0, errors.New("unexpected CopyFrom")
}

func (transaction *fakeTransaction) SendBatch(
	context.Context,
	*pgx.Batch,
) pgx.BatchResults {
	return nil
}

func (transaction *fakeTransaction) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (transaction *fakeTransaction) Prepare(
	context.Context,
	string,
	string,
) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare")
}

func (transaction *fakeTransaction) Exec(
	_ context.Context,
	_ string,
	_ ...any,
) (pgconn.CommandTag, error) {
	index := transaction.execCalls
	transaction.execCalls++
	if err := transaction.execErrors[index]; err != nil {
		return pgconn.CommandTag{}, err
	}

	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (transaction *fakeTransaction) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (transaction *fakeTransaction) QueryRow(
	_ context.Context,
	_ string,
	_ ...any,
) pgx.Row {
	value := transaction.rows[transaction.rowIndex]
	transaction.rowIndex++

	return fakeRow{value: value}
}

func (*fakeTransaction) Conn() *pgx.Conn {
	return nil
}

type fakeRow struct {
	value int64
}

func (row fakeRow) Scan(destinations ...any) error {
	*destinations[0].(*int64) = row.value

	return nil
}

var _ pgx.Tx = (*fakeTransaction)(nil)
