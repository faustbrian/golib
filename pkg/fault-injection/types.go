package faultinject

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	maxIdentityLength = 64
	defaultMaxRules   = 64
	defaultMaxFaults  = 16
	defaultMaxLatency = 30 * time.Second
	defaultMaxBytes   = 64 * 1024
)

// ErrInvalidConfig identifies configuration rejected before activation.
var ErrInvalidConfig = errors.New("invalid fault-injection configuration")

// InvalidConfigError identifies the invalid configuration field without
// including caller data or fault values.
type InvalidConfigError struct {
	Field  string
	Reason string
}

func (e *InvalidConfigError) Error() string {
	return fmt.Sprintf("faultinject: invalid %s: %s", e.Field, e.Reason)
}

func (e *InvalidConfigError) Unwrap() error { return ErrInvalidConfig }

func invalid(field, reason string) error {
	return &InvalidConfigError{Field: field, Reason: reason}
}

// Boundary identifies the adapter seam at which a rule can apply. Values are
// safe identifiers, not request or tenant data.
type Boundary string

const (
	BoundaryFunction       Boundary = "function"
	BoundaryHTTP           Boundary = "http"
	BoundaryHTTPBody       Boundary = "http_body"
	BoundaryConn           Boundary = "conn"
	BoundaryDial           Boundary = "dial"
	BoundaryListen         Boundary = "listen"
	BoundaryReader         Boundary = "reader"
	BoundaryWriter         Boundary = "writer"
	BoundaryClock          Boundary = "clock"
	BoundaryFilesystemOpen Boundary = "filesystem_open"
	BoundaryFilesystemRead Boundary = "filesystem_read"
)

// Metadata is the complete caller-controlled rule input. Numeric operation
// identifiers keep request bodies, headers, database values, credentials,
// tenant identifiers, and arbitrary errors out of predicates and events.
type Metadata struct {
	Boundary  Boundary
	Operation uint32
	Attempt   uint64
}

// Predicate selects calls using bounded typed metadata. Predicates must be
// deterministic and safe for concurrent invocation. They run without an
// Injector lock held.
type Predicate func(Metadata) bool

// Activation states whether a fully configured rule participates.
type Activation uint8

const (
	Inactive Activation = iota + 1
	Active
)

// Terminal controls whether lower-precedence rules may compose after a match.
type Terminal uint8

const (
	Continue Terminal = iota + 1
	Stop
)

// Observation controls whether a matched rule emits events.
type Observation uint8

const (
	Suppress Observation = iota + 1
	Observe
)

// Phase defines when an adapter applies a selected fault.
type Phase uint8

const (
	PhaseBefore Phase = iota + 1
	PhaseDuring
	PhaseAfter
)

// Kind identifies an injected behavior.
type Kind string

const (
	KindError      Kind = "error"
	KindLatency    Kind = "latency"
	KindCancel     Kind = "cancel"
	KindDeadline   Kind = "deadline"
	KindPanic      Kind = "panic"
	KindDrop       Kind = "drop"
	KindTruncate   Kind = "truncate"
	KindDuplicate  Kind = "duplicate"
	KindReorder    Kind = "reorder"
	KindCorrupt    Kind = "corrupt"
	KindShortRead  Kind = "short_read"
	KindShortWrite Kind = "short_write"
	KindTemporary  Kind = "temporary_network"
	KindPermanent  Kind = "permanent_network"
	KindReset      Kind = "connection_reset"
	KindHalfClose  Kind = "half_close"
	KindInterrupt  Kind = "stream_interruption"
)

// Clock provides attributable event time without coupling selection to the
// process clock.
type Clock interface {
	Now() time.Time
}

// Sleeper performs bounded context-aware delays selected by an Injector.
type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

// Observer consumes already-selected fault events. It cannot veto selection.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe calls function with event.
func (function ObserverFunc) Observe(event Event) { function(event) }

// Event is bounded attribution for one selected fault.
type Event struct {
	RuleID       string
	Boundary     Boundary
	Kind         Kind
	Sequence     uint64
	Injection    uint64
	SeedIdentity uint64
	Generation   uint64
	At           time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type systemSleeper struct{}

func (systemSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
