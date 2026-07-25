package postgres

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrorKind is a stable policy category derived from an original error.
type ErrorKind string

const (
	// ErrorNone is the classification of a nil error.
	ErrorNone ErrorKind = ""
	// ErrorUnknown indicates that no supported category matched.
	ErrorUnknown ErrorKind = "unknown"
	// ErrorUniqueViolation identifies SQLSTATE 23505.
	ErrorUniqueViolation ErrorKind = "unique_violation"
	// ErrorForeignKeyViolation identifies SQLSTATE 23503.
	ErrorForeignKeyViolation ErrorKind = "foreign_key_violation"
	// ErrorCheckViolation identifies SQLSTATE 23514.
	ErrorCheckViolation ErrorKind = "check_violation"
	// ErrorExclusionViolation identifies SQLSTATE 23P01.
	ErrorExclusionViolation ErrorKind = "exclusion_violation"
	// ErrorSerializationFailure identifies SQLSTATE 40001.
	ErrorSerializationFailure ErrorKind = "serialization_failure"
	// ErrorDeadlock identifies SQLSTATE 40P01.
	ErrorDeadlock ErrorKind = "deadlock"
	// ErrorTimeout identifies context deadlines.
	ErrorTimeout ErrorKind = "timeout"
	// ErrorCancellation identifies caller context cancellation.
	ErrorCancellation ErrorKind = "cancellation"
	// ErrorQueryCanceled identifies SQLSTATE 57014. PostgreSQL uses it for
	// explicit cancellation and statement_timeout, so policy must inspect its
	// own operation context rather than assume one cause.
	ErrorQueryCanceled ErrorKind = "query_canceled"
	// ErrorLockUnavailable identifies SQLSTATE 55P03. PostgreSQL uses it for
	// both NOWAIT and lock_timeout failures.
	ErrorLockUnavailable ErrorKind = "lock_unavailable"
	// ErrorConnectivity identifies connection-class and network errors.
	ErrorConnectivity ErrorKind = "connectivity"
	// ErrorPoolExhaustion identifies a bounded pool acquisition timeout.
	ErrorPoolExhaustion ErrorKind = "pool_exhaustion"
)

// ErrorInfo classifies an error without replacing it. Cause is the exact input
// error, Postgres is the matched native PgError, and server metadata remains
// available for policy code that is allowed to inspect it.
type ErrorInfo struct {
	Kind      ErrorKind
	SQLState  string
	Retryable bool
	Cause     error
	Postgres  *pgconn.PgError

	Severity   string
	Constraint string
	Schema     string
	Table      string
	Column     string
	Detail     string
	Hint       string
}

// Classify returns stable policy metadata while preserving the original error
// graph for errors.Is and errors.As. Detail and Hint may contain sensitive
// values and must not be logged without an application redaction policy.
func Classify(err error) ErrorInfo {
	if err == nil {
		return ErrorInfo{}
	}

	info := ErrorInfo{Kind: ErrorUnknown, Cause: err}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		info.Kind = kindForSQLState(pgErr.Code)
		info.SQLState = pgErr.Code
		info.Postgres = pgErr
		info.Severity = pgErr.Severity
		info.Constraint = pgErr.ConstraintName
		info.Schema = pgErr.SchemaName
		info.Table = pgErr.TableName
		info.Column = pgErr.ColumnName
		info.Detail = pgErr.Detail
		info.Hint = pgErr.Hint
		info.Retryable = retryableKind(info.Kind)

		return info
	}
	if errors.Is(err, ErrPoolExhausted) {
		info.Kind = ErrorPoolExhaustion

		return info
	}
	if errors.Is(err, context.Canceled) {
		info.Kind = ErrorCancellation

		return info
	}
	if errors.Is(err, context.DeadlineExceeded) {
		info.Kind = ErrorTimeout

		return info
	}

	var networkErr net.Error
	if errors.As(err, &networkErr) {
		info.Kind = ErrorConnectivity
		info.Retryable = pgconn.SafeToRetry(err)
	}

	return info
}

// SQLState returns the first native PostgreSQL SQLSTATE in the error graph.
func SQLState(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}

	return pgErr.Code, true
}

// IsKind reports whether err classifies as kind.
func IsKind(err error, kind ErrorKind) bool {
	return Classify(err).Kind == kind
}

// IsUniqueViolation reports SQLSTATE 23505.
func IsUniqueViolation(err error) bool { return IsKind(err, ErrorUniqueViolation) }

// IsForeignKeyViolation reports SQLSTATE 23503.
func IsForeignKeyViolation(err error) bool { return IsKind(err, ErrorForeignKeyViolation) }

// IsCheckViolation reports SQLSTATE 23514.
func IsCheckViolation(err error) bool { return IsKind(err, ErrorCheckViolation) }

// IsExclusionViolation reports SQLSTATE 23P01.
func IsExclusionViolation(err error) bool { return IsKind(err, ErrorExclusionViolation) }

// IsSerializationFailure reports SQLSTATE 40001.
func IsSerializationFailure(err error) bool {
	return IsKind(err, ErrorSerializationFailure)
}

// IsDeadlock reports SQLSTATE 40P01.
func IsDeadlock(err error) bool { return IsKind(err, ErrorDeadlock) }

// IsTimeout reports a context deadline.
func IsTimeout(err error) bool { return IsKind(err, ErrorTimeout) }

// IsCancellation reports caller context cancellation.
func IsCancellation(err error) bool { return IsKind(err, ErrorCancellation) }

// IsQueryCanceled reports SQLSTATE 57014. The state alone does not distinguish
// explicit cancellation from statement_timeout.
func IsQueryCanceled(err error) bool { return IsKind(err, ErrorQueryCanceled) }

// IsLockUnavailable reports SQLSTATE 55P03. The state alone does not
// distinguish NOWAIT from lock_timeout.
func IsLockUnavailable(err error) bool { return IsKind(err, ErrorLockUnavailable) }

// IsConnectivity reports PostgreSQL connection-class or network errors.
func IsConnectivity(err error) bool { return IsKind(err, ErrorConnectivity) }

// IsPoolExhaustion reports a bounded acquisition timeout.
func IsPoolExhaustion(err error) bool { return IsKind(err, ErrorPoolExhaustion) }

// IsRetryable is advisory classification only. It never executes or retries a
// closure because callers must account for external side effects themselves.
func IsRetryable(err error) bool {
	return Classify(err).Retryable
}

func kindForSQLState(code string) ErrorKind {
	switch code {
	case "23505":
		return ErrorUniqueViolation
	case "23503":
		return ErrorForeignKeyViolation
	case "23514":
		return ErrorCheckViolation
	case "23P01":
		return ErrorExclusionViolation
	case "40001":
		return ErrorSerializationFailure
	case "40P01":
		return ErrorDeadlock
	case "57014":
		return ErrorQueryCanceled
	case "55P03":
		return ErrorLockUnavailable
	case "57P01", "57P02", "57P03", "53300":
		return ErrorConnectivity
	}

	if strings.HasPrefix(code, "08") {
		return ErrorConnectivity
	}

	return ErrorUnknown
}

func retryableKind(kind ErrorKind) bool {
	return kind == ErrorSerializationFailure ||
		kind == ErrorDeadlock
}
