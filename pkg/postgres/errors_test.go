package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyRecognizesPostgreSQLErrorKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code string
		kind ErrorKind
	}{
		{code: "23505", kind: ErrorUniqueViolation},
		{code: "23503", kind: ErrorForeignKeyViolation},
		{code: "23514", kind: ErrorCheckViolation},
		{code: "23P01", kind: ErrorExclusionViolation},
		{code: "40001", kind: ErrorSerializationFailure},
		{code: "40P01", kind: ErrorDeadlock},
		{code: "57014", kind: ErrorQueryCanceled},
		{code: "55P03", kind: ErrorLockUnavailable},
		{code: "08006", kind: ErrorConnectivity},
		{code: "57P01", kind: ErrorConnectivity},
		{code: "53300", kind: ErrorConnectivity},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()

			pgErr := &pgconn.PgError{Code: tt.code}
			info := Classify(fmt.Errorf("wrapped: %w", pgErr))
			if info.Kind != tt.kind {
				t.Fatalf("Classify().Kind = %q, want %q", info.Kind, tt.kind)
			}
			if info.SQLState != tt.code {
				t.Errorf("Classify().SQLState = %q, want %q", info.SQLState, tt.code)
			}
			if info.Postgres != pgErr {
				t.Fatal("Classify().Postgres does not preserve PgError")
			}
		})
	}
}

func TestClassifyPreservesPostgreSQLErrorMetadataAndCause(t *testing.T) {
	t.Parallel()

	pgErr := &pgconn.PgError{
		Code:           "23505",
		Severity:       "ERROR",
		ConstraintName: "users_email_key",
		SchemaName:     "public",
		TableName:      "users",
		ColumnName:     "email",
		Detail:         "sensitive detail",
		Hint:           "choose another value",
	}
	original := errors.Join(errors.New("outer"), pgErr)
	info := Classify(original)

	if !errors.Is(info.Cause, original) {
		t.Fatal("Classify().Cause does not preserve original error")
	}
	if info.Constraint != pgErr.ConstraintName || info.Schema != pgErr.SchemaName ||
		info.Table != pgErr.TableName || info.Column != pgErr.ColumnName ||
		info.Detail != pgErr.Detail || info.Hint != pgErr.Hint ||
		info.Severity != pgErr.Severity {
		t.Fatalf("Classify() metadata = %#v, want PgError metadata", info)
	}
	if !IsUniqueViolation(original) {
		t.Fatal("IsUniqueViolation() = false")
	}
}

func TestClassifyRecognizesContextPoolAndNetworkErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		kind ErrorKind
	}{
		{name: "canceled", err: context.Canceled, kind: ErrorCancellation},
		{name: "deadline", err: context.DeadlineExceeded, kind: ErrorTimeout},
		{
			name: "pool exhaustion",
			err:  errors.Join(ErrPoolExhausted, ErrAcquireTimeout, context.DeadlineExceeded),
			kind: ErrorPoolExhaustion,
		},
		{
			name: "network",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")},
			kind: ErrorConnectivity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Classify(tt.err).Kind; got != tt.kind {
				t.Fatalf("Classify().Kind = %q, want %q", got, tt.kind)
			}
		})
	}
}

func TestRetryClassificationIsAdvisoryOnly(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"40001", "40P01"} {
		if !IsRetryable(&pgconn.PgError{Code: code}) {
			t.Errorf("IsRetryable(%s) = false", code)
		}
	}
	for _, code := range []string{"23505", "57014", "08006"} {
		if IsRetryable(&pgconn.PgError{Code: code}) {
			t.Errorf("IsRetryable(%s) = true", code)
		}
	}
	safeNetworkErr := retryableNetworkError{OpError: net.OpError{
		Op: "dial", Net: "tcp", Err: errors.New("refused"),
	}}
	if !IsRetryable(&safeNetworkErr) {
		t.Fatal("IsRetryable(safe network error) = false")
	}
}

func TestClassifyPrefersPostgreSQLMetadataInJoinedErrors(t *testing.T) {
	t.Parallel()

	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}
	info := Classify(errors.Join(pgErr, context.DeadlineExceeded))
	if info.Kind != ErrorUniqueViolation || info.Postgres != pgErr || info.SQLState != "23505" {
		t.Fatalf("Classify(joined error) = %#v", info)
	}
}

func TestClassifyUnknownAndNilErrors(t *testing.T) {
	t.Parallel()

	if got := Classify(nil); got.Kind != ErrorNone || got.Cause != nil {
		t.Fatalf("Classify(nil) = %#v", got)
	}
	sentinel := errors.New("unknown")
	if got := Classify(sentinel); got.Kind != ErrorUnknown || !errors.Is(got.Cause, sentinel) {
		t.Fatalf("Classify(unknown) = %#v", got)
	}
}

func TestPublicErrorPredicatesAndSQLState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code      string
		predicate func(error) bool
	}{
		{code: "23505", predicate: IsUniqueViolation},
		{code: "23503", predicate: IsForeignKeyViolation},
		{code: "23514", predicate: IsCheckViolation},
		{code: "23P01", predicate: IsExclusionViolation},
		{code: "40001", predicate: IsSerializationFailure},
		{code: "40P01", predicate: IsDeadlock},
		{code: "55P03", predicate: IsLockUnavailable},
		{code: "57014", predicate: IsQueryCanceled},
		{code: "08000", predicate: IsConnectivity},
	}
	for _, tt := range tests {
		err := &pgconn.PgError{Code: tt.code}
		if !tt.predicate(err) {
			t.Errorf("predicate(%s) = false", tt.code)
		}
		if state, ok := SQLState(err); !ok || state != tt.code {
			t.Errorf("SQLState(%s) = %q, %v", tt.code, state, ok)
		}
	}
	if !IsPoolExhaustion(ErrPoolExhausted) {
		t.Fatal("IsPoolExhaustion() = false")
	}
	if _, ok := SQLState(errors.New("not PostgreSQL")); ok {
		t.Fatal("SQLState() found state on generic error")
	}
	if got := Classify(&pgconn.PgError{Code: "ZZZZZ"}).Kind; got != ErrorUnknown {
		t.Fatalf("unknown SQLSTATE kind = %q", got)
	}
}

type retryableNetworkError struct{ net.OpError }

func (retryableNetworkError) SafeToRetry() bool { return true }
