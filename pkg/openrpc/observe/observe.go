// Package observe provides optional, payload-free operation hooks. It is a
// leaf adapter: core OpenRPC packages neither import it nor install exporters.
package observe

import (
	"context"
	"errors"
	"time"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/diff"
	"github.com/faustbrian/golib/pkg/openrpc/discovery"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

// ErrInvalidContext reports a nil operation context.
var ErrInvalidContext = errors.New("observe: invalid context")

// Phase is a finite operation label safe for metrics dimensions.
type Phase string

const (
	PhaseParse    Phase = "parse"
	PhaseValidate Phase = "validate"
	PhaseResolve  Phase = "resolve"
	PhaseBundle   Phase = "bundle"
	PhaseDiff     Phase = "diff"
	PhaseDiscover Phase = "discover"
)

// Outcome is a finite operation result label safe for metrics dimensions.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"
	OutcomeInvalid  Outcome = "invalid"
	OutcomeFailure  Outcome = "failure"
	OutcomeCanceled Outcome = "canceled"
)

// Event contains only bounded metadata. It deliberately excludes payloads,
// schemas, method names, references, resource URIs, and error strings.
type Event struct {
	Phase           Phase
	Outcome         Outcome
	DiagnosticCount int
	ReferenceCount  int
	Duration        time.Duration
}

// Observer receives completed operation metadata. Implementations must return
// promptly; panics are contained and never change operation results.
type Observer interface {
	Observe(context.Context, Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Event)

// Observe implements Observer.
func (function ObserverFunc) Observe(ctx context.Context, event Event) {
	function(ctx, event)
}

// Parse performs bounded parsing and reports one payload-free event.
func Parse(ctx context.Context, input []byte, options parse.Options, observer Observer) (parse.Result, error) {
	started := time.Now()
	if ctx == nil {
		return parse.Result{}, ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		emit(ctx, observer, Event{Phase: PhaseParse, Outcome: OutcomeCanceled, Duration: time.Since(started)})
		return parse.Result{}, err
	}
	result, err := parse.Decode(input, options)
	outcome := OutcomeSuccess
	if err != nil {
		outcome = OutcomeInvalid
	}
	emit(ctx, observer, Event{Phase: PhaseParse, Outcome: outcome, Duration: time.Since(started)})
	return result, err
}

// Validate performs semantic validation and reports bounded diagnostic count.
func Validate(ctx context.Context, document openrpc.Document, options validate.Options, observer Observer) validate.Report {
	started := time.Now()
	report := validate.Document(ctx, document, options)
	diagnostics := report.Diagnostics()
	outcome := OutcomeSuccess
	if hasDiagnostic(diagnostics, validate.CodeCanceled) {
		outcome = OutcomeCanceled
	} else if !report.Valid() {
		outcome = OutcomeInvalid
	}
	if ctx != nil {
		emit(ctx, observer, Event{
			Phase: PhaseValidate, Outcome: outcome,
			DiagnosticCount: len(diagnostics), Duration: time.Since(started),
		})
	}
	return report
}

// Resolve follows references and reports only the bounded input count.
func Resolve(
	ctx context.Context,
	resolver *reference.Resolver,
	root jsonvalue.Value,
	base string,
	references []string,
	observer Observer,
) ([]reference.Target, error) {
	started := time.Now()
	if ctx == nil {
		return nil, ErrInvalidContext
	}
	targets, err := resolver.ResolveMany(ctx, root, base, references)
	emit(ctx, observer, Event{
		Phase: PhaseResolve, Outcome: errorOutcome(err),
		ReferenceCount: len(references), Duration: time.Since(started),
	})
	return targets, err
}

// Bundle collects an offline resource graph and reports loaded resource count.
func Bundle(
	ctx context.Context,
	resolver *reference.Resolver,
	root jsonvalue.Value,
	base string,
	observer Observer,
) (reference.ResourceBundle, error) {
	started := time.Now()
	if ctx == nil {
		return reference.ResourceBundle{}, ErrInvalidContext
	}
	bundle, err := reference.Bundle(ctx, resolver, root, base)
	emit(ctx, observer, Event{
		Phase: PhaseBundle, Outcome: errorOutcome(err),
		ReferenceCount: len(bundle.Resources()), Duration: time.Since(started),
	})
	return bundle, err
}

// Diff compares two documents and reports bounded change count as diagnostics.
func Diff(
	ctx context.Context,
	before openrpc.Document,
	after openrpc.Document,
	options diff.Options,
	observer Observer,
) diff.Report {
	started := time.Now()
	report := diff.Compare(ctx, before, after, options)
	changes := report.Changes()
	outcome := errorOutcome(report.Err())
	emitIfContext(ctx, observer, Event{
		Phase: PhaseDiff, Outcome: outcome,
		DiagnosticCount: len(changes), Duration: time.Since(started),
	})
	return report
}

// Discover produces one snapshot and reports only its outcome and duration.
func Discover(ctx context.Context, service *discovery.Service, observer Observer) (discovery.Snapshot, error) {
	started := time.Now()
	if ctx == nil {
		return discovery.Snapshot{}, ErrInvalidContext
	}
	snapshot, err := service.Discover(ctx)
	emit(ctx, observer, Event{
		Phase: PhaseDiscover, Outcome: errorOutcome(err), Duration: time.Since(started),
	})
	return snapshot, err
}

func hasDiagnostic(diagnostics []validate.Diagnostic, code validate.Code) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func errorOutcome(err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return OutcomeCanceled
	}
	return OutcomeFailure
}

func emitIfContext(ctx context.Context, observer Observer, event Event) {
	if ctx != nil {
		emit(ctx, observer, event)
	}
}

func emit(ctx context.Context, observer Observer, event Event) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.Observe(ctx, event)
}
