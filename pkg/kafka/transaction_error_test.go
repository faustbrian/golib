package kafka

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kerr"
)

func TestRunTransactionClassifiesDefinitiveCommitFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cause    error
		category ErrorCategory
	}{
		{
			name:     "fenced",
			cause:    kerr.ProducerFenced,
			category: ErrorFenced,
		},
		{
			name:     "authorization",
			cause:    kerr.TransactionalIDAuthorizationFailed,
			category: ErrorAuthorization,
		},
		{
			name:     "fatal producer state",
			cause:    kerr.UnknownProducerID,
			category: ErrorFatal,
		},
		{
			name:     "invalid producer ID mapping",
			cause:    kerr.InvalidProducerIDMapping,
			category: ErrorFatal,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{endErr: test.cause}
			err := transactionalProducer(backend).RunTransaction(
				context.Background(),
				func(Transaction) error { return nil },
			)
			var transactionErr *TransactionError
			if !errors.As(err, &transactionErr) ||
				transactionErr.Operation() != TransactionOperationCommit ||
				transactionErr.Category() != test.category ||
				transactionErr.Abortable() || !transactionErr.OutcomeKnown() ||
				!errors.Is(err, test.cause) ||
				errors.Is(err, ErrTransactionOutcomeUnknown) ||
				backend.aborts != 0 || len(backend.endTries) != 1 {
				t.Fatalf("error/backend = %v/%#v", err, backend)
			}
		})
	}
}

func TestRunTransactionClassifiesAbortableCommitFailure(t *testing.T) {
	t.Parallel()

	for name, cause := range map[string]error{
		"operation not attempted": kerr.OperationNotAttempted,
		"transaction abortable":   kerr.TransactionAbortable,
	} {
		cause := cause
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{
				endErrors: []error{cause, nil},
			}
			err := transactionalProducer(backend).RunTransaction(
				context.Background(),
				func(Transaction) error { return nil },
			)

			var transactionErr *TransactionError
			if !errors.As(err, &transactionErr) ||
				transactionErr.Operation() != TransactionOperationCommit ||
				transactionErr.Category() != ErrorRetryable ||
				!transactionErr.Abortable() || !transactionErr.OutcomeKnown() ||
				!errors.Is(err, cause) ||
				errors.Is(err, ErrTransactionOutcomeUnknown) ||
				backend.aborts != 1 || len(backend.endTries) != 2 {
				t.Fatalf("error/backend = %v/%#v", err, backend)
			}
		})
	}
}

func TestRunTransactionRedactsUnknownCommitOutcome(t *testing.T) {
	t.Parallel()

	cause := errors.New("commit failed through user:password@broker.internal")
	backend := &recordingProducerBackend{endErr: cause}
	err := transactionalProducer(backend).RunTransaction(
		context.Background(),
		func(Transaction) error { return nil },
	)
	if err == nil {
		t.Fatal("RunTransaction() error = nil")
	}
	rendered := err.Error()

	var transactionErr *TransactionError
	if !errors.As(err, &transactionErr) ||
		transactionErr.Operation() != TransactionOperationCommit ||
		transactionErr.Category() != ErrorAmbiguous ||
		transactionErr.Abortable() || transactionErr.OutcomeKnown() ||
		!errors.Is(err, cause) || !errors.Is(err, ErrTransactionOutcomeUnknown) ||
		strings.Contains(rendered, "password") ||
		strings.Contains(rendered, "broker.internal") {
		t.Fatalf("transaction error = %v", err)
	}
}

func TestRunTransactionClassifiesBeginAndAbortFailures(t *testing.T) {
	t.Parallel()

	beginCause := kerr.TransactionalIDAuthorizationFailed
	beginBackend := &recordingProducerBackend{beginErr: beginCause}
	err := transactionalProducer(beginBackend).RunTransaction(
		context.Background(),
		func(Transaction) error { return nil },
	)
	assertTransactionError(
		t,
		err,
		TransactionOperationBegin,
		ErrorAuthorization,
		false,
		true,
	)

	callbackErr := errors.New("application failed")
	abortCause := errors.New("abort response lost through secret.internal")
	abortBackend := &recordingProducerBackend{endErr: abortCause}
	err = transactionalProducer(abortBackend).RunTransaction(
		context.Background(),
		func(Transaction) error { return callbackErr },
	)
	if err == nil {
		t.Fatal("RunTransaction() abort error = nil")
	}
	if !errors.Is(err, callbackErr) || !errors.Is(err, abortCause) ||
		strings.Contains(err.Error(), "secret.internal") {
		t.Fatalf("abort error = %v", err)
	}
	assertTransactionError(
		t,
		err,
		TransactionOperationAbort,
		ErrorAmbiguous,
		false,
		false,
	)
}

func TestRunTransactionPrioritizesUnknownAbortCleanup(t *testing.T) {
	t.Parallel()

	abortCause := errors.New("abort outcome unavailable")
	backend := &recordingProducerBackend{
		endErrors: []error{kerr.TransactionAbortable, abortCause},
	}
	err := transactionalProducer(backend).RunTransaction(
		context.Background(),
		func(Transaction) error { return nil },
	)

	if !errors.Is(err, kerr.TransactionAbortable) || !errors.Is(err, abortCause) {
		t.Fatalf("transaction error = %v", err)
	}
	assertTransactionError(
		t,
		err,
		TransactionOperationAbort,
		ErrorAmbiguous,
		false,
		false,
	)
}

func TestRunTransactionClassifiesBufferedAndDefinitiveAbortFailures(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("application failed")
	bufferCause := errors.New("buffer did not drain")
	bufferBackend := &recordingProducerBackend{abortErr: bufferCause}
	err := transactionalProducer(bufferBackend).RunTransaction(
		context.Background(),
		func(Transaction) error { return callbackErr },
	)
	if !errors.Is(err, callbackErr) || !errors.Is(err, bufferCause) {
		t.Fatalf("buffer abort error = %v", err)
	}
	assertTransactionError(
		t,
		err,
		TransactionOperationAbort,
		ErrorFatal,
		false,
		true,
	)

	for name, cause := range map[string]error{
		"authorization": kerr.TransactionalIDAuthorizationFailed,
		"fenced":        kerr.ProducerFenced,
		"fatal":         kerr.UnknownProducerID,
	} {
		cause := cause
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{endErr: cause}
			err := transactionalProducer(backend).RunTransaction(
				context.Background(),
				func(Transaction) error { return callbackErr },
			)
			assertTransactionError(
				t,
				err,
				TransactionOperationAbort,
				classifyError(cause),
				false,
				true,
			)
		})
	}
}

func TestTransactionErrorSupportsStableOperationsAndNilReceiver(t *testing.T) {
	t.Parallel()

	if TransactionOperationBegin.String() != "begin" ||
		TransactionOperationCommit.String() != "commit" ||
		TransactionOperationAbort.String() != "abort" ||
		TransactionOperation(255).String() != "unknown" {
		t.Fatal("transaction operation names are unstable")
	}

	var err *TransactionError
	if err.Error() != "kafka: transaction failed" || err.Unwrap() != nil ||
		err.Operation() != 0 || err.Category() != 0 || err.Abortable() ||
		err.OutcomeKnown() {
		t.Fatal("nil TransactionError methods returned an unsafe result")
	}
	if got := newTransactionError(
		TransactionOperationBegin,
		nil,
		false,
		true,
	); got != nil {
		t.Fatalf("newTransactionError(nil) = %v", got)
	}
}

func assertTransactionError(
	t *testing.T,
	err error,
	operation TransactionOperation,
	category ErrorCategory,
	abortable bool,
	outcomeKnown bool,
) {
	t.Helper()

	var transactionErr *TransactionError
	if !errors.As(err, &transactionErr) ||
		transactionErr.Operation() != operation ||
		transactionErr.Category() != category ||
		transactionErr.Abortable() != abortable ||
		transactionErr.OutcomeKnown() != outcomeKnown {
		t.Fatalf("transaction error = %#v from %v", transactionErr, err)
	}
}
