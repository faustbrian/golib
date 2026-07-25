package idempotencyoutbox_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyoutbox"
	"github.com/jackc/pgx/v5"
)

func TestInsertAndCompleteUsesOneTransactionInOrder(t *testing.T) {
	ctx := context.Background()
	envelope := testEnvelope{ID: "event-1"}
	request := idempotency.CompleteRequest{Result: []byte("replay")}
	want := idempotency.Record{State: idempotency.StateCompleted}
	var calls []string
	writer := writerFunc[testEnvelope](func(
		gotCtx context.Context,
		gotTx pgx.Tx,
		gotEnvelope testEnvelope,
	) error {
		if gotCtx != ctx || gotTx != nil || gotEnvelope != envelope {
			t.Fatalf("Insert() arguments = %#v, %#v, %#v", gotCtx, gotTx, gotEnvelope)
		}
		calls = append(calls, "insert")
		return nil
	})
	completer := completerFunc(func(
		gotCtx context.Context,
		gotTx pgx.Tx,
		gotRequest idempotency.CompleteRequest,
	) (idempotency.Record, error) {
		if gotCtx != ctx || gotTx != nil || !reflect.DeepEqual(gotRequest, request) {
			t.Fatalf("CompleteTx() arguments = %#v, %#v, %#v", gotCtx, gotTx, gotRequest)
		}
		calls = append(calls, "complete")
		return want, nil
	})

	got, err := idempotencyoutbox.InsertAndComplete(
		ctx, nil, writer, envelope, completer, request,
	)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("InsertAndComplete() = %#v, %v", got, err)
	}
	if !reflect.DeepEqual(calls, []string{"insert", "complete"}) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestInsertAndCompleteStopsAtFailedBoundary(t *testing.T) {
	insertErr := errors.New("insert failed")
	completeErr := errors.New("completion failed")
	tests := map[string]struct {
		writerErr    error
		completeErr  error
		wantErr      error
		wantComplete bool
	}{
		"insert": {
			writerErr: insertErr,
			wantErr:   insertErr,
		},
		"completion": {
			completeErr:  completeErr,
			wantErr:      completeErr,
			wantComplete: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			completed := false
			writer := writerFunc[testEnvelope](func(context.Context, pgx.Tx, testEnvelope) error {
				return test.writerErr
			})
			completer := completerFunc(func(
				context.Context,
				pgx.Tx,
				idempotency.CompleteRequest,
			) (idempotency.Record, error) {
				completed = true
				return idempotency.Record{}, test.completeErr
			})

			record, err := idempotencyoutbox.InsertAndComplete(
				context.Background(), nil, writer, testEnvelope{}, completer,
				idempotency.CompleteRequest{},
			)
			if !reflect.DeepEqual(record, idempotency.Record{}) ||
				!errors.Is(err, test.wantErr) {
				t.Fatalf("InsertAndComplete() = %#v, %v", record, err)
			}
			if completed != test.wantComplete {
				t.Fatalf("CompleteTx() called = %t, want %t", completed, test.wantComplete)
			}
		})
	}
}

type testEnvelope struct {
	ID string
}

type writerFunc[E any] func(context.Context, pgx.Tx, E) error

func (write writerFunc[E]) Insert(ctx context.Context, tx pgx.Tx, envelope E) error {
	return write(ctx, tx, envelope)
}

type completerFunc func(
	context.Context,
	pgx.Tx,
	idempotency.CompleteRequest,
) (idempotency.Record, error)

func (complete completerFunc) CompleteTx(
	ctx context.Context,
	tx pgx.Tx,
	request idempotency.CompleteRequest,
) (idempotency.Record, error) {
	return complete(ctx, tx, request)
}
