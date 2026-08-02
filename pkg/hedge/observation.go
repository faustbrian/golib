package hedge

import "time"

// Outcome identifies one bounded lifecycle event. It is safe to use as a
// metric label after mapping unknown values to a bounded fallback.
type Outcome uint8

const (
	// OutcomeNoHedgeNeeded records an original winner before any hedge started.
	OutcomeNoHedgeNeeded Outcome = iota + 1
	// OutcomeHedgeStarted records one admitted additional attempt.
	OutcomeHedgeStarted
	// OutcomeBudgetDenied records rejected additional work.
	OutcomeBudgetDenied
	// OutcomeWinnerSelected records a winner after additional work started.
	OutcomeWinnerSelected
	// OutcomeAllAttemptsFailed records deterministic failed-result selection.
	OutcomeAllAttemptsFailed
	// OutcomeCallerCanceled records caller cancellation.
	OutcomeCallerCanceled
	// OutcomeTotalDeadline records total timeout expiry.
	OutcomeTotalDeadline
	// OutcomeAttemptCompleted records one result published to the coordinator.
	OutcomeAttemptCompleted
	// OutcomeCleanupFailed records one disposer failure.
	OutcomeCleanupFailed
)

// String returns a bounded label; unknown values collapse to "unknown".
func (outcome Outcome) String() string {
	switch outcome {
	case OutcomeNoHedgeNeeded:
		return "no_hedge_needed"
	case OutcomeHedgeStarted:
		return "hedge_started"
	case OutcomeBudgetDenied:
		return "budget_denied"
	case OutcomeWinnerSelected:
		return "winner_selected"
	case OutcomeAllAttemptsFailed:
		return "all_attempts_failed"
	case OutcomeCallerCanceled:
		return "caller_canceled"
	case OutcomeTotalDeadline:
		return "total_deadline"
	case OutcomeAttemptCompleted:
		return "attempt_completed"
	case OutcomeCleanupFailed:
		return "cleanup_failed"
	default:
		return "unknown"
	}
}

// Observation contains bounded metadata and deliberately excludes results,
// request data, URLs, and raw errors.
type Observation struct {
	Outcome  Outcome
	Ordinal  uint
	Delay    time.Duration
	Duration time.Duration
	Resource string
	Endpoint string
	// Classification is set for OutcomeAttemptCompleted and zero otherwise.
	Classification Classification
	Winner         bool
	Loser          bool
}
