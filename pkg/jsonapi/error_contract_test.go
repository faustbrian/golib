package jsonapi

import (
	"errors"
	"testing"
)

func TestDecodeErrorContract(t *testing.T) {
	t.Parallel()

	cause := errors.New("invalid token")
	tests := []struct {
		err  *DecodeError
		want string
	}{
		{
			err:  &DecodeError{Message: "invalid JSON", Cause: cause},
			want: "decode JSON:API document: invalid JSON",
		},
		{
			err:  &DecodeError{Path: "/data/id", Message: "value must be a string"},
			want: `decode JSON:API document at "/data/id": value must be a string`,
		},
	}
	for _, test := range tests {
		if got := test.err.Error(); got != test.want {
			t.Fatalf("unexpected error text: got %q, want %q", got, test.want)
		}
	}
	if !errors.Is(tests[0].err, cause) {
		t.Fatal("decode error did not expose its cause")
	}
}

func TestAtomicExecutionErrorContract(t *testing.T) {
	t.Parallel()

	cause := errors.New("apply failed")
	rollback := errors.New("rollback failed")
	operation := &AtomicExecutionError{
		Phase:          "apply",
		OperationIndex: 2,
		Cause:          cause,
		RollbackCause:  rollback,
	}
	if got, want := operation.Error(),
		"execute Atomic operation 2 during apply"; got != want {
		t.Fatalf("unexpected operation error: got %q, want %q", got, want)
	}
	if !errors.Is(operation, cause) || !errors.Is(operation, rollback) {
		t.Fatal("atomic execution error did not expose both failures")
	}

	transaction := &AtomicExecutionError{
		Phase:          "commit",
		OperationIndex: -1,
		Cause:          cause,
	}
	if got, want := transaction.Error(),
		"execute Atomic transaction during commit"; got != want {
		t.Fatalf("unexpected transaction error: got %q, want %q", got, want)
	}

	panicCause := errors.New("private panic value")
	panicError := &AtomicPanicError{Phase: "apply", Value: panicCause}
	if got, want := panicError.Error(),
		"Atomic transaction callback panicked during apply"; got != want {
		t.Fatalf("unexpected panic error: got %q, want %q", got, want)
	}
	if !errors.Is(panicError, panicCause) {
		t.Fatal("panic error did not expose its error-valued cause")
	}
	if (&AtomicPanicError{Value: "not an error"}).Unwrap() != nil {
		t.Fatal("non-error panic value unexpectedly unwraps")
	}
}

func TestProtocolErrorContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want string
	}{
		{
			err: &CursorPaginationError{
				Parameter: "page[size]",
				Message:   "must be positive",
			},
			want: `invalid cursor pagination parameter "page[size]": must be positive`,
		},
		{
			err: &NegotiationError{
				Status:  406,
				Code:    "not-acceptable",
				Message: "no supported representation",
			},
			want: "JSON:API negotiation failed (406 not-acceptable): no supported representation",
		},
		{
			err: &QueryError{
				Parameter: "include",
				Message:   "path is invalid",
			},
			want: `invalid JSON:API query parameter "include": path is invalid`,
		},
	}
	for _, test := range tests {
		if got := test.err.Error(); got != test.want {
			t.Fatalf("unexpected protocol error: got %q, want %q", got, test.want)
		}
	}
}

func TestValidationErrorContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  *ValidationError
		want string
	}{
		{
			err:  &ValidationError{},
			want: "JSON:API validation failed",
		},
		{
			err: &ValidationError{Violations: []Violation{{
				Path: "/data/type", Message: "resource type is required",
			}}},
			want: `JSON:API validation failed at "/data/type": resource type is required`,
		},
		{
			err: &ValidationError{Violations: []Violation{
				{Path: "/data/type", Message: "resource type is required"},
				{Path: "/data/id", Message: "resource id is required"},
			}},
			want: `JSON:API validation failed at "/data/type": resource type is required (and 1 more violations)`,
		},
	}
	for _, test := range tests {
		if got := test.err.Error(); got != test.want {
			t.Fatalf("unexpected validation error: got %q, want %q", got, test.want)
		}
	}
}
