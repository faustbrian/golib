package bulkhead

import "time"

// EventKind identifies a bounded lifecycle transition.
type EventKind string

const (
	EventAdmitted EventKind = "admitted"
	EventQueued   EventKind = "queued"
	EventRejected EventKind = "rejected"
	EventCanceled EventKind = "canceled"
	EventReleased EventKind = "released"
	EventExecuted EventKind = "executed"
	EventDraining EventKind = "draining"
	EventDrained  EventKind = "drained"
)

// RejectionReason distinguishes terminal admission outcomes.
type RejectionReason string

const (
	RejectionCapacity RejectionReason = "capacity"
	RejectionQueue    RejectionReason = "queue_full"
	RejectionTimeout  RejectionReason = "wait_timeout"
	RejectionCaller   RejectionReason = "caller_canceled"
	RejectionShutdown RejectionReason = "shutdown"
	RejectionWeight   RejectionReason = "invalid_weight"
	RejectionReentry  RejectionReason = "reentrant"
)

// Event is bounded immutable lifecycle metadata. It never retains contexts,
// operation values, errors, or attacker-controlled labels beyond Resource.
type Event struct {
	Resource       string
	PolicyRevision string
	Kind           EventKind
	Reason         RejectionReason
	Weight         int64
	QueueDepth     int
	ActiveWeight   int64
	WaitDuration   time.Duration
	ExecutionTime  time.Duration
	At             time.Time
}

// Snapshot is an immutable point-in-time view of policy and lifetime counters.
type Snapshot struct {
	Resource               string
	PolicyRevision         string
	Capacity               int64
	ActiveWeight           int64
	AvailableWeight        int64
	QueueDepth             int
	Admissions             uint64
	Rejections             uint64
	Cancellations          uint64
	Executions             uint64
	TotalWaitDuration      time.Duration
	TotalExecutionDuration time.Duration
	RejectionCounts        map[RejectionReason]uint64
	Draining               bool
	Drained                bool
}
