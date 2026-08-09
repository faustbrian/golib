package gooutbox

import (
	"context"
	"errors"
	"strconv"
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
	if transaction.commitCalls != 0 || transaction.rollbackCalls != 0 {
		t.Fatalf(
			"transaction completion calls = commit %d rollback %d",
			transaction.commitCalls,
			transaction.rollbackCalls,
		)
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

func TestStagerRollsBackSavepointAfterEveryStatementFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sensitive statement failure")
	tests := map[string]struct {
		execErrors map[int]error
		rowErrors  map[int]error
		want       error
	}{
		"create stream": {
			execErrors: map[int]error{0: sentinel},
			want:       sentinel,
		},
		"lock stream": {
			rowErrors: map[int]error{0: sentinel},
			want:      sentinel,
		},
		"allocate positions": {
			rowErrors: map[int]error{1: sentinel},
			want:      sentinel,
		},
		"insert event": {
			rowErrors: map[int]error{2: sentinel},
			want:      sentinel,
		},
		"advance stream": {
			execErrors: map[int]error{1: sentinel},
			want:       sentinel,
		},
		"insert outbox": {
			execErrors: map[int]error{2: sentinel},
			want:       ErrOutboxWrite,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transaction := &fakeTransaction{
				rows:       []int64{0, 1, 1},
				execErrors: test.execErrors,
				rowErrors:  test.rowErrors,
			}
			stager := mustStager(
				t,
				transaction,
				FixedTopic("account-events"),
			)
			stream, pending := internalPending(t)
			messages, err := stager.Stage(
				t.Context(),
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{pending},
			)
			if messages != nil || !errors.Is(err, test.want) ||
				eventsourcing.AppendCommitOutcome(err) !=
					eventsourcing.CommitNotCommitted {
				t.Fatalf("Stage() = %#v, %v; want %v", messages, err, test.want)
			}
			if transaction.savepointBegins != 1 ||
				transaction.savepointRollbacks != 1 ||
				transaction.savepointReleases != 0 ||
				transaction.rollbackCalls != 0 ||
				!transaction.savepointRollbackBound {
				t.Fatalf("transaction lifecycle = %#v", transaction)
			}
		})
	}
}

func TestStagerContainsSavepointLifecycleFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sensitive savepoint failure")
	t.Run("begin", func(t *testing.T) {
		t.Parallel()

		transaction := &fakeTransaction{beginErr: sentinel}
		stager := mustStager(t, transaction, FixedTopic("account-events"))
		stream, pending := internalPending(t)
		_, err := stager.Stage(
			t.Context(),
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{pending},
		)
		if !errors.Is(err, ErrTransactionStaging) ||
			!errors.Is(err, sentinel) || err.Error() != ErrTransactionStaging.Error() ||
			transaction.rollbackCalls != 0 {
			t.Fatalf("Stage error = %v; transaction = %#v", err, transaction)
		}
	})
	t.Run("release", func(t *testing.T) {
		t.Parallel()

		transaction := &fakeTransaction{
			rows:                []int64{0, 1, 1},
			savepointReleaseErr: sentinel,
		}
		stager := mustStager(t, transaction, FixedTopic("account-events"))
		stream, pending := internalPending(t)
		_, err := stager.Stage(
			t.Context(),
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{pending},
		)
		if !errors.Is(err, ErrTransactionStaging) ||
			!errors.Is(err, sentinel) || transaction.savepointReleases != 1 ||
			transaction.savepointRollbacks != 1 ||
			transaction.rollbackCalls != 0 || transaction.execCalls != 3 {
			t.Fatalf("Stage error = %v; transaction = %#v", err, transaction)
		}
	})
	t.Run("invalid internal event configuration", func(t *testing.T) {
		t.Parallel()

		transaction := &fakeTransaction{}
		stager := mustStager(t, transaction, FixedTopic("account-events"))
		stager.eventConfig.Schema = "unsupported"
		stream, pending := internalPending(t)
		_, err := stager.Stage(
			t.Context(),
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{pending},
		)
		if err == nil || transaction.savepointRollbacks != 1 ||
			transaction.rollbackCalls != 0 {
			t.Fatalf("Stage error = %v; transaction = %#v", err, transaction)
		}
	})
	t.Run("rollback", func(t *testing.T) {
		t.Parallel()

		outboxFailure := errors.New("sensitive outbox failure")
		transaction := &fakeTransaction{
			rows:                  []int64{0, 1, 1},
			execErrors:            map[int]error{2: outboxFailure},
			savepointRollbackErr:  sentinel,
			savepointRollbackWait: true,
		}
		stager := mustStager(t, transaction, FixedTopic("account-events"))
		stager.cleanupTimeout = time.Millisecond
		stream, pending := internalPending(t)
		_, err := stager.Stage(
			t.Context(),
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{pending},
		)
		if !errors.Is(err, ErrOutboxWrite) ||
			!errors.Is(err, ErrTransactionStaging) ||
			!errors.Is(err, outboxFailure) || !errors.Is(err, sentinel) ||
			!errors.Is(err, context.DeadlineExceeded) ||
			err.Error() != ErrTransactionStaging.Error() ||
			transaction.savepointRollbacks != 1 ||
			transaction.rollbackCalls != 0 || transaction.execCalls != 4 ||
			transaction.transactionFailureContext ==
				transaction.savepointRollbackContext ||
			!transaction.transactionFailureBound {
			t.Fatalf("Stage error = %v; transaction = %#v", err, transaction)
		}
	})
}

func TestStagerDoesNotOpenSavepointForPreflightCancellation(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{rows: []int64{0, 1, 1}}
	stager := mustStager(t, transaction, FixedTopic("account-events"))
	stream, pending := internalPending(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if !errors.Is(err, context.Canceled) || transaction.savepointBegins != 0 ||
		transaction.savepointRollbacks != 0 {
		t.Fatalf("Stage error = %v; transaction = %#v", err, transaction)
	}
}

func TestStagerRejectsNilContextBeforeTransaction(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{}
	stager := mustStager(t, transaction, FixedTopic("account-events"))
	stream, pending := internalPending(t)
	var nilContext context.Context
	_, err := stager.Stage(
		nilContext,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		transaction.savepointBegins != 0 {
		t.Fatalf("Stage error = %v; transaction = %#v", err, transaction)
	}
}

func TestStageInputAcceptsMaximumBatchSize(t *testing.T) {
	t.Parallel()

	stream, template := internalPending(t)
	pending := make(
		[]eventsourcing.PendingMessage,
		eventsourcing.MaxAppendMessages,
	)
	for index := range pending {
		message, err := eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID:         "maximum-message-" + strconv.Itoa(index),
				Stream:     stream,
				Event:      template.Event(),
				RecordedAt: template.RecordedAt(),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		pending[index] = message
	}
	if err := validateStageInput(
		t.Context(),
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	); err != nil {
		t.Fatalf("validate maximum batch: %v", err)
	}
}

func TestStagerRejectsInvalidBatchBeforeResolverOrTransaction(t *testing.T) {
	t.Parallel()

	stream, pending := internalPending(t)
	otherStream, err := eventsourcing.NewStreamID("account", "account-2")
	if err != nil {
		t.Fatal(err)
	}
	tooLarge := make(
		[]eventsourcing.PendingMessage,
		eventsourcing.MaxAppendMessages+1,
	)
	for index := range tooLarge {
		tooLarge[index] = pending
	}
	tests := map[string]struct {
		stream   eventsourcing.StreamID
		expected eventsourcing.ExpectedVersion
		pending  []eventsourcing.PendingMessage
		want     error
	}{
		"stream": {
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{pending},
			want:     eventsourcing.ErrInvalidArgument,
		},
		"expectation": {
			stream:  stream,
			pending: []eventsourcing.PendingMessage{pending},
			want:    eventsourcing.ErrInvalidArgument,
		},
		"empty batch": {
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			want:     eventsourcing.ErrInvalidArgument,
		},
		"oversized batch": {
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			pending:  tooLarge,
			want:     eventsourcing.ErrInvalidArgument,
		},
		"zero message": {
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{{}},
			want:     eventsourcing.ErrInvalidArgument,
		},
		"wrong message stream": {
			stream:   otherStream,
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{pending},
			want:     eventsourcing.ErrInvalidArgument,
		},
		"duplicate message": {
			stream:   stream,
			expected: eventsourcing.ExpectNewStream(),
			pending:  []eventsourcing.PendingMessage{pending, pending},
			want:     eventsourcing.ErrDuplicateMessageID,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transaction := &fakeTransaction{}
			resolverCalls := 0
			stager := mustStager(
				t,
				transaction,
				TopicResolverFunc(func(TopicMessage) (string, error) {
					resolverCalls++

					return "account-events", nil
				}),
			)
			_, err := stager.Stage(
				t.Context(),
				test.stream,
				test.expected,
				test.pending,
			)
			if !errors.Is(err, test.want) || resolverCalls != 0 ||
				transaction.savepointBegins != 0 {
				t.Fatalf(
					"Stage error = %v; resolver calls = %d; transaction = %#v",
					err,
					resolverCalls,
					transaction,
				)
			}
		})
	}
}

func TestStagerResolvesTopicsBeforeOpeningSavepoint(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{rows: []int64{0, 1, 1}}
	resolver := TopicResolverFunc(func(TopicMessage) (string, error) {
		if transaction.savepointBegins != 0 || transaction.execCalls != 0 ||
			transaction.rowIndex != 0 {
			return "", errors.New("resolver ran while transaction work was active")
		}

		return "account-events", nil
	})
	stager := mustStager(t, transaction, resolver)
	stream, pending := internalPending(t)
	if _, err := stager.Stage(
		t.Context(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatal(err)
	}
}

func TestStagerRollsBackWhenResolvedEnvelopeExceedsLimits(t *testing.T) {
	t.Parallel()

	transaction := &fakeTransaction{rows: []int64{0, 1, 1}}
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	limits := outbox.DefaultLimits()
	limits.MaxIDBytes = 1
	codec, err := NewEnvelopeCodec(FixedTopic("account-events"), limits)
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
	stream, pending := internalPending(t)
	_, err = stager.Stage(
		t.Context(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if !errors.Is(err, ErrEnvelopeEncoding) ||
		transaction.savepointRollbacks != 1 || transaction.rollbackCalls != 0 {
		t.Fatalf("Stage error = %v; transaction = %#v", err, transaction)
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
	rows                      []int64
	rowIndex                  int
	rowErrors                 map[int]error
	execErrors                map[int]error
	execCalls                 int
	beginErr                  error
	commitErr                 error
	commitCalls               int
	rollbackCalls             int
	rollbackUntil             time.Time
	rollbackBound             bool
	savepointBegins           int
	savepointReleases         int
	savepointReleaseErr       error
	savepointRollbacks        int
	savepointRollbackErr      error
	savepointRollbackWait     bool
	savepointRollbackUntil    time.Time
	savepointRollbackBound    bool
	savepointRollbackContext  context.Context
	transactionFailureContext context.Context
	transactionFailureUntil   time.Time
	transactionFailureBound   bool
}

func (transaction *fakeTransaction) Begin(
	context.Context,
) (pgx.Tx, error) {
	transaction.savepointBegins++
	if transaction.beginErr != nil {
		return nil, transaction.beginErr
	}

	return &fakeSavepoint{fakeTransaction: transaction}, nil
}

func (transaction *fakeTransaction) Commit(context.Context) error {
	transaction.commitCalls++

	return transaction.commitErr
}

func (transaction *fakeTransaction) Rollback(ctx context.Context) error {
	transaction.rollbackCalls++
	transaction.rollbackUntil, transaction.rollbackBound = ctx.Deadline()

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
	ctx context.Context,
	_ string,
	_ ...any,
) (pgconn.CommandTag, error) {
	index := transaction.execCalls
	transaction.execCalls++
	if index == 3 && transaction.savepointRollbackErr != nil {
		transaction.transactionFailureContext = ctx
		transaction.transactionFailureUntil,
			transaction.transactionFailureBound = ctx.Deadline()
	}
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
	index := transaction.rowIndex
	value := transaction.rows[transaction.rowIndex]
	transaction.rowIndex++

	return fakeRow{value: value, err: transaction.rowErrors[index]}
}

func (*fakeTransaction) Conn() *pgx.Conn {
	return nil
}

type fakeSavepoint struct {
	*fakeTransaction
}

func (savepoint *fakeSavepoint) Commit(context.Context) error {
	savepoint.savepointReleases++

	return savepoint.savepointReleaseErr
}

func (savepoint *fakeSavepoint) Rollback(ctx context.Context) error {
	savepoint.savepointRollbacks++
	savepoint.savepointRollbackContext = ctx
	savepoint.savepointRollbackUntil,
		savepoint.savepointRollbackBound = ctx.Deadline()
	if savepoint.savepointRollbackWait {
		<-ctx.Done()

		return errors.Join(savepoint.savepointRollbackErr, ctx.Err())
	}

	return savepoint.savepointRollbackErr
}

type fakeRow struct {
	value int64
	err   error
}

func (row fakeRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(*int64) = row.value

	return nil
}

var _ pgx.Tx = (*fakeTransaction)(nil)
var _ pgx.Tx = (*fakeSavepoint)(nil)
