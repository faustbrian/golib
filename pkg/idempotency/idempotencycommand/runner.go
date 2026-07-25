// Package idempotencycommand provides durable named command and source-record
// import execution with bounded result replay.
package idempotencycommand

import (
	"context"
	"errors"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

var (
	// ErrInProgress reports an unexpired owner for the source record.
	ErrInProgress = errors.New("idempotencycommand: operation in progress")
	// ErrConflict reports reuse of a source identity for different input.
	ErrConflict = errors.New("idempotencycommand: source fingerprint conflict")
	// ErrTerminalFailure reports a deliberately persisted permanent failure.
	ErrTerminalFailure = errors.New("idempotencycommand: operation terminally failed")
)

// Request identifies a named command or one record in an import source.
type Request struct {
	Namespace   string
	Tenant      string
	Name        string
	Caller      string
	SourceID    string
	Fingerprint idempotency.Fingerprint
}

// Handler executes one elected source-record owner and returns replay data.
type Handler func(context.Context) ([]byte, map[string]string, error)

// Options configures command leases and failed-handler cleanup.
type Options struct {
	Service           *idempotency.Service
	Lease             time.Duration
	TransitionTimeout time.Duration
}

// Result reports the semantic outcome and bounded replay data.
type Result struct {
	Outcome  idempotency.Outcome
	Result   []byte
	Metadata map[string]string
	Replayed bool
}

// Runner durably executes named commands and source-record imports.
type Runner struct {
	service           *idempotency.Service
	lease             time.Duration
	transitionTimeout time.Duration
}

// New validates options and constructs a command runner.
func New(options Options) (*Runner, error) {
	if options.Service == nil {
		return nil, configurationError("service")
	}
	if options.Lease <= 0 || options.Lease > idempotency.MaxLease {
		return nil, configurationError("lease")
	}
	if options.TransitionTimeout == 0 {
		options.TransitionTimeout = 5 * time.Second
	}
	if options.TransitionTimeout < 0 {
		return nil, configurationError("transition_timeout")
	}
	return &Runner{
		service: options.Service, lease: options.Lease,
		transitionTimeout: options.TransitionTimeout,
	}, nil
}

// Run executes handler only for a newly acquired or taken-over source record.
func (r *Runner) Run(ctx context.Context, request Request, handler Handler) (Result, error) {
	if handler == nil {
		return Result{}, configurationError("handler")
	}
	key, err := idempotency.NewKey(
		request.Namespace, request.Tenant, request.Name, request.Caller, request.SourceID,
	)
	if err != nil {
		return Result{}, err
	}
	begin, err := r.service.Begin(ctx, idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{
			Key: key, Fingerprint: request.Fingerprint, Lease: r.lease,
		},
	})
	if err != nil {
		return Result{}, err
	}
	switch begin.Outcome {
	case idempotency.OutcomeReplayed:
		return resultFromRecord(begin.Outcome, begin.Record, true), nil
	case idempotency.OutcomeInProgress:
		return Result{Outcome: begin.Outcome}, ErrInProgress
	case idempotency.OutcomeConflict:
		return Result{Outcome: begin.Outcome}, ErrConflict
	case idempotency.OutcomeTerminalFailure:
		return resultFromRecord(begin.Outcome, begin.Record, true), ErrTerminalFailure
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			_ = r.release(ctx, begin.Record.Ownership())
			panic(recovered)
		}
	}()
	handlerCtx := idempotency.WithOwnership(ctx, begin.Record.Ownership())
	value, metadata, err := handler(handlerCtx)
	if err != nil {
		return Result{}, errors.Join(err, r.release(ctx, begin.Record.Ownership()))
	}
	record, err := r.service.Complete(ctx, idempotency.CompleteRequest{
		Ownership: begin.Record.Ownership(), Result: value, Metadata: metadata,
	})
	if err != nil {
		return Result{}, err
	}
	return resultFromRecord(begin.Outcome, record, false), nil
}

func (r *Runner) release(ctx context.Context, ownership idempotency.Ownership) error {
	transitionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.transitionTimeout)
	defer cancel()
	_, err := r.service.Release(transitionCtx, ownership)
	return err
}

func resultFromRecord(outcome idempotency.Outcome, record idempotency.Record, replayed bool) Result {
	return Result{
		Outcome: outcome, Result: append([]byte(nil), record.Result...),
		Metadata: cloneMetadata(record.Metadata), Replayed: replayed,
	}
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func configurationError(field string) error {
	return &idempotency.Error{Reason: idempotency.ReasonInvalidConfiguration, Field: field}
}
