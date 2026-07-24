package jsonapi

import (
	"context"
	"fmt"
)

// AtomicTransaction applies operations within one application-owned
// transaction. Implementations must not commit from ApplyAtomic.
type AtomicTransaction interface {
	ApplyAtomic(context.Context, AtomicOperation) (AtomicResult, error)
	CommitAtomic(context.Context) error
	RollbackAtomic(context.Context) error
}

// AtomicTransactionBeginner starts an application-owned transaction.
type AtomicTransactionBeginner interface {
	BeginAtomic(context.Context) (AtomicTransaction, error)
}

// AtomicExecutionError identifies the failed transaction phase and operation.
// OperationIndex is -1 for begin and commit failures.
type AtomicExecutionError struct {
	Phase          string
	OperationIndex int
	Cause          error
	RollbackCause  error
}

// AtomicPanicError identifies a panic raised by an application transaction
// callback. Value is retained for explicit diagnostics but omitted from Error.
type AtomicPanicError struct {
	Phase string
	Value any
}

// Error implements error without formatting the potentially sensitive panic
// value.
func (err *AtomicPanicError) Error() string {
	return "Atomic transaction callback panicked during " + err.Phase
}

// Unwrap exposes a panic value that is itself an error.
func (err *AtomicPanicError) Unwrap() error {
	cause, _ := err.Value.(error)
	return cause
}

// Error implements error.
func (err *AtomicExecutionError) Error() string {
	if err.OperationIndex >= 0 {
		return fmt.Sprintf(
			"execute Atomic operation %d during %s",
			err.OperationIndex,
			err.Phase,
		)
	}
	return fmt.Sprintf("execute Atomic transaction during %s", err.Phase)
}

// Unwrap exposes both the primary and rollback failures to errors.Is/As.
func (err *AtomicExecutionError) Unwrap() []error {
	errors := []error{err.Cause}
	if err.RollbackCause != nil {
		errors = append(errors, err.RollbackCause)
	}
	return errors
}

// ExecuteAtomic validates an Atomic request, applies operations in document
// order, and commits only after every operation succeeds. Any failure after a
// transaction begins triggers exactly one rollback attempt.
func ExecuteAtomic(
	ctx context.Context,
	beginner AtomicTransactionBeginner,
	document AtomicDocument,
) (AtomicDocument, error) {
	if err := document.ValidateWith(AtomicValidationOptions{Context: AtomicRequestContext}); err != nil {
		return AtomicDocument{}, err
	}
	if beginner == nil {
		return AtomicDocument{}, &AtomicExecutionError{
			Phase: "begin", OperationIndex: -1,
			Cause: fmt.Errorf("transaction beginner is required"),
		}
	}
	if ctx == nil {
		return AtomicDocument{}, &AtomicExecutionError{
			Phase: "begin", OperationIndex: -1,
			Cause: fmt.Errorf("context is required"),
		}
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return AtomicDocument{}, &AtomicExecutionError{
			Phase: "begin", OperationIndex: -1, Cause: contextErr,
		}
	}

	transaction, err := beginAtomicSafely(ctx, beginner)
	if err != nil {
		return AtomicDocument{}, &AtomicExecutionError{
			Phase: "begin", OperationIndex: -1, Cause: err,
		}
	}
	if transaction == nil {
		return AtomicDocument{}, &AtomicExecutionError{
			Phase: "begin", OperationIndex: -1,
			Cause: fmt.Errorf("transaction beginner returned nil transaction"),
		}
	}

	results := make([]AtomicResult, len(document.Operations))
	for index, operation := range document.Operations {
		if contextErr := ctx.Err(); contextErr != nil {
			return AtomicDocument{}, atomicExecutionFailure(
				ctx, transaction, "apply", index, contextErr,
			)
		}
		result, applyErr := applyAtomicSafely(ctx, transaction, operation)
		if applyErr != nil {
			return AtomicDocument{}, atomicExecutionFailure(
				ctx, transaction, "apply", index, applyErr,
			)
		}
		results[index] = result
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return AtomicDocument{}, atomicExecutionFailure(
			ctx, transaction, "commit", -1, contextErr,
		)
	}
	response := AtomicDocument{Results: results}
	if validationErr := response.ValidateWith(AtomicValidationOptions{
		Context:             AtomicResponseContext,
		ExpectedResultCount: len(document.Operations),
		ExpectedOperations:  document.Operations,
	}); validationErr != nil {
		return AtomicDocument{}, atomicExecutionFailure(
			ctx, transaction, "result-validation", -1, validationErr,
		)
	}
	if commitErr := commitAtomicSafely(ctx, transaction); commitErr != nil {
		return AtomicDocument{}, atomicExecutionFailure(
			ctx, transaction, "commit", -1, commitErr,
		)
	}
	return response, nil
}

func atomicExecutionFailure(
	ctx context.Context,
	transaction AtomicTransaction,
	phase string,
	operationIndex int,
	cause error,
) error {
	return &AtomicExecutionError{
		Phase:          phase,
		OperationIndex: operationIndex,
		Cause:          cause,
		RollbackCause:  rollbackAtomicSafely(context.WithoutCancel(ctx), transaction),
	}
}

func beginAtomicSafely(
	ctx context.Context,
	beginner AtomicTransactionBeginner,
) (transaction AtomicTransaction, err error) {
	defer func() {
		if value := recover(); value != nil {
			transaction = nil
			err = &AtomicPanicError{Phase: "begin", Value: value}
		}
	}()
	return beginner.BeginAtomic(ctx)
}

func applyAtomicSafely(
	ctx context.Context,
	transaction AtomicTransaction,
	operation AtomicOperation,
) (result AtomicResult, err error) {
	defer func() {
		if value := recover(); value != nil {
			result = AtomicResult{}
			err = &AtomicPanicError{Phase: "apply", Value: value}
		}
	}()
	return transaction.ApplyAtomic(ctx, operation)
}

func commitAtomicSafely(ctx context.Context, transaction AtomicTransaction) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = &AtomicPanicError{Phase: "commit", Value: value}
		}
	}()
	return transaction.CommitAtomic(ctx)
}

func rollbackAtomicSafely(ctx context.Context, transaction AtomicTransaction) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = &AtomicPanicError{Phase: "rollback", Value: value}
		}
	}()
	return transaction.RollbackAtomic(ctx)
}
