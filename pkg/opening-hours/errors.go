package openinghours

import (
	"errors"
	"fmt"
)

// Code identifies a stable, safe-to-expose failure category.
type Code string

const (
	// CodeInvalidTime reports a wall-clock value outside its valid domain.
	CodeInvalidTime Code = "invalid_time"
	// CodeInvalidDate reports a civil date outside its valid domain.
	CodeInvalidDate Code = "invalid_date"
	// CodeInvalidRange reports a malformed or zero-length time range.
	CodeInvalidRange Code = "invalid_range"
	// CodeInvalidTimezone reports an absent or unknown IANA timezone.
	CodeInvalidTimezone Code = "invalid_timezone"
	// CodeInvalidWeekday reports a weekday outside Sunday through Saturday.
	CodeInvalidWeekday Code = "invalid_weekday"
	// CodeInvalidState reports an unsupported enum or state combination.
	CodeInvalidState Code = "invalid_state"
	// CodeOverlap reports overlapping ranges under a rejecting policy.
	CodeOverlap Code = "overlap"
	// CodeLimitExceeded reports a configured resource bound was exceeded.
	CodeLimitExceeded Code = "limit_exceeded"
	// CodeAmbiguousException reports equal-priority exception ambiguity.
	CodeAmbiguousException Code = "ambiguous_exception"
	// CodeDuplicateRevision reports duplicate source revision identity.
	CodeDuplicateRevision Code = "duplicate_revision"
	// CodeAmbiguousLocalTime reports a local time in a timezone fold.
	CodeAmbiguousLocalTime Code = "ambiguous_local_time"
	// CodeNonexistentLocalTime reports a local time in a timezone gap.
	CodeNonexistentLocalTime Code = "nonexistent_local_time"
	// CodeInvalidHorizon reports an unbounded or excessive search horizon.
	CodeInvalidHorizon Code = "invalid_horizon"
	// CodeSearchExhausted reports no transition in the bounded horizon.
	CodeSearchExhausted Code = "search_exhausted"
	// CodeTimezoneMismatch reports composition across different timezones.
	CodeTimezoneMismatch Code = "timezone_mismatch"
	// CodeInvalidEncoding reports malformed or noncanonical input structure.
	CodeInvalidEncoding Code = "invalid_encoding"
	// CodeUnsupportedVersion reports an unknown canonical wire version.
	CodeUnsupportedVersion Code = "unsupported_version"
	// CodeInvalidInterval reports a reversed, empty, or excessive interval.
	CodeInvalidInterval Code = "invalid_interval"
	// CodeOutsideEffectiveRange reports a query outside configured dates.
	CodeOutsideEffectiveRange Code = "outside_effective_range"
	// CodeInvalidClock reports a missing injected clock capability.
	CodeInvalidClock Code = "invalid_clock"
	// CodeAdjacent reports adjacent ranges under a rejecting policy.
	CodeAdjacent Code = "adjacent"
	// CodeDayBoundaryOverflow reports normalized ownership beyond one day.
	CodeDayBoundaryOverflow Code = "day_boundary_overflow"
)

// Error is a bounded package error. It never embeds schedule or input data.
type Error struct {
	Code Code
	Op   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("openinghours: %s: %s", e.Op, e.Code)
}

// IsCode reports whether err or an error in its chain has code.
func IsCode(err error, code Code) bool {
	var target *Error

	return errors.As(err, &target) && target.Code == code
}

func newError(op string, code Code) error {
	return &Error{Code: code, Op: op}
}
