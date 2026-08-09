package audit

import (
	"context"
	"time"
)

// ObservationKind is a fixed low-cardinality operation outcome.
type ObservationKind uint8

const (
	// ObservationAccepted counts newly accepted records.
	ObservationAccepted ObservationKind = iota + 1
	// ObservationRejected counts confirmed pre-commit rejections.
	ObservationRejected
	// ObservationBuffered counts successful fallback buffer writes.
	ObservationBuffered
	// ObservationDuplicated counts idempotent duplicate submissions.
	ObservationDuplicated
	// ObservationFailed counts operations returning errors.
	ObservationFailed
	// ObservationDelayed counts records older than the configured threshold.
	ObservationDelayed
	// ObservationExported counts successfully streamed records.
	ObservationExported
	// ObservationIntegrityInvalid counts failed integrity verification.
	ObservationIntegrityInvalid
)

// Observation deliberately contains no actor, subject, tenant, record, or
// other caller-controlled label.
type Observation struct {
	Kind     ObservationKind
	Count    int
	Duration time.Duration
	Outcome  AppendOutcome
}

// Observer is a dependency-neutral metrics and tracing hook.
type Observer interface {
	Observe(context.Context, Observation)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Observation)

// Observe invokes the adapted identifier-free observation hook.
func (observer ObserverFunc) Observe(ctx context.Context, value Observation) {
	if observer != nil {
		observer(ctx, value)
	}
}

func safeObserve(ctx context.Context, observer Observer, value Observation) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.Observe(ctx, value)
}

type observedExporter struct {
	exporter Exporter
	observer Observer
}

// NewObservedExporter decorates an exporter with identifier-free counts.
func NewObservedExporter(exporter Exporter, observer Observer) (Exporter, error) {
	if exporter == nil || observer == nil {
		return nil, invalid("observed_exporter", "requires exporter and observer")
	}
	return &observedExporter{exporter: exporter, observer: observer}, nil
}

func (exporter *observedExporter) Export(ctx context.Context, query Query, consume func(Record) error) error {
	if exporter == nil || ctx == nil || consume == nil {
		return invalid("observed_exporter", "must be assigned")
	}
	count := 0
	err := exporter.exporter.Export(ctx, query, func(record Record) error {
		if err := consume(record); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		safeObserve(ctx, exporter.observer, Observation{Kind: ObservationFailed, Count: count})
		return err
	}
	safeObserve(ctx, exporter.observer, Observation{Kind: ObservationExported, Count: count})
	return nil
}
