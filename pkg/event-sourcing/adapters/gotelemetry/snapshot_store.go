package gotelemetry

import (
	"context"
	"errors"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrSnapshotStoreRequired reports a missing downstream snapshot store.
	ErrSnapshotStoreRequired = errors.New(
		"event-sourcing/gotelemetry: snapshot store is required",
	)
)

// WrapSnapshotStore instruments explicit snapshot load, refresh, and deletion
// without recording aggregate identity, state, metadata, versions, or failure
// diagnostics.
func (instrumentation *Instrumentation) WrapSnapshotStore(
	next eventsourcing.SnapshotStore,
) (eventsourcing.SnapshotStore, error) {
	if instrumentation == nil || !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrSnapshotStoreRequired
	}

	return snapshotStore{instrumentation: instrumentation, next: next}, nil
}

type snapshotStore struct {
	instrumentation *Instrumentation
	next            eventsourcing.SnapshotStore
}

func (store snapshotStore) Load(
	ctx context.Context,
	stream eventsourcing.StreamID,
) (
	snapshot eventsourcing.Snapshot,
	operationErr error,
) {
	if ctx == nil {
		return eventsourcing.Snapshot{}, ErrContextRequired
	}
	ctx, span := startSnapshotSpan(store.instrumentation, ctx, "load")
	started := time.Now()
	defer func() {
		panicValue := recover()
		finishSnapshotOperation(
			store.instrumentation,
			ctx,
			span,
			"snapshot_load",
			snapshotLoadOutcome(operationErr, panicValue),
			started,
			panicValue,
		)
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return store.next.Load(ctx, stream)
}

func (store snapshotStore) Save(
	ctx context.Context,
	snapshot eventsourcing.Snapshot,
) (operationErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	ctx, span := startSnapshotSpan(store.instrumentation, ctx, "save")
	started := time.Now()
	defer func() {
		panicValue := recover()
		finishSnapshotOperation(
			store.instrumentation,
			ctx,
			span,
			"snapshot_save",
			snapshotSaveOutcome(operationErr, panicValue),
			started,
			panicValue,
		)
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return store.next.Save(ctx, snapshot)
}

func (store snapshotStore) Delete(
	ctx context.Context,
	stream eventsourcing.StreamID,
) (operationErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	ctx, span := startSnapshotSpan(store.instrumentation, ctx, "delete")
	started := time.Now()
	defer func() {
		panicValue := recover()
		finishSnapshotOperation(
			store.instrumentation,
			ctx,
			span,
			"snapshot_delete",
			operationOutcome(operationErr, panicValue),
			started,
			panicValue,
		)
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return store.next.Delete(ctx, stream)
}

func startSnapshotSpan(
	instrumentation *Instrumentation,
	ctx context.Context,
	operation string,
) (context.Context, trace.Span) {
	return instrumentation.tracer.Start(
		ctx,
		"event_sourcing.snapshot."+operation,
		trace.WithAttributes(
			attribute.String(
				"event_sourcing.operation",
				"snapshot_"+operation,
			),
		),
	)
}

func finishSnapshotOperation(
	instrumentation *Instrumentation,
	ctx context.Context,
	span trace.Span,
	operation string,
	outcome string,
	started time.Time,
	panicValue any,
) {
	span.SetAttributes(attribute.String("event_sourcing.outcome", outcome))
	if outcome == "error" || outcome == "stale" || panicValue != nil {
		span.SetStatus(codes.Error, "event-sourcing snapshot operation failed")
	}
	instrumentation.recordOperation(
		ctx,
		operation,
		outcome,
		time.Since(started),
	)
	span.End()
}

func snapshotLoadOutcome(operationErr error, panicValue any) string {
	if panicValue != nil {
		return "panic"
	}
	if errors.Is(operationErr, eventsourcing.ErrSnapshotNotFound) {
		return "miss"
	}
	if operationErr != nil {
		return "error"
	}

	return "hit"
}

func snapshotSaveOutcome(operationErr error, panicValue any) string {
	if panicValue != nil {
		return "panic"
	}
	if errors.Is(operationErr, eventsourcing.ErrSnapshotStale) {
		return "stale"
	}
	if operationErr != nil {
		return "error"
	}

	return "success"
}

var _ eventsourcing.SnapshotStore = snapshotStore{}
