package concurrencylimit

import (
	"context"
	"time"
)

// Outcome is the exactly-once terminal classification of an admitted permit.
type Outcome uint8

const (
	OutcomeSuccess Outcome = iota
	OutcomeDependencyFailure
	OutcomeLocalDrop
	OutcomeIgnored
	OutcomeOverload
)

func (outcome Outcome) valid() bool { return outcome <= OutcomeOverload }

// Metadata is optional bounded diagnostic admission metadata. Priority and
// partition do not alter FIFO admission; authorization remains with callers.
type Metadata struct {
	Priority  int
	Partition string
}

// Completion is the ephemeral input to an execution classifier.
type Completion struct {
	Context  context.Context
	Result   any
	Err      error
	Duration time.Duration
}

// OutcomeCounts is a value-only terminal outcome summary.
type OutcomeCounts struct {
	Success           uint64
	DependencyFailure uint64
	LocalDrop         uint64
	Ignored           uint64
	Overload          uint64
}

// Snapshot is an immutable, bounded point-in-time limiter description.
type Snapshot struct {
	Limit            int
	InFlight         int
	Queued           int
	Samples          uint64
	RecentSamples    int
	Baseline         time.Duration
	Rejections       uint64
	QueueTimeouts    uint64
	ExpiredPermits   uint64
	ObserverPanics   uint64
	ClassifierPanics uint64
	AlgorithmErrors  uint64
	ClockErrors      uint64
	Generation       uint64
	Draining         bool
	Outcomes         OutcomeCounts
	Algorithm        AlgorithmState
}

// EventType identifies a stable limiter event.
type EventType uint8

const (
	EventAdmitted EventType = iota
	EventRejected
	EventCompleted
	EventLimitChanged
	EventReset
	EventDrainStarted
)

// Event is an immutable value-only state transition observation.
type Event struct {
	Type      EventType
	Outcome   Outcome
	Duration  time.Duration
	Metadata  Metadata
	Before    Snapshot
	After     Snapshot
	Timestamp time.Time
}
