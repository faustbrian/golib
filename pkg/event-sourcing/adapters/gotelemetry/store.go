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
	// ErrEventStoreRequired reports a missing downstream event store.
	ErrEventStoreRequired = errors.New(
		"event-sourcing/gotelemetry: event store is required",
	)
	// ErrGlobalReaderRequired reports a missing downstream global reader.
	ErrGlobalReaderRequired = errors.New(
		"event-sourcing/gotelemetry: global reader is required",
	)
	// ErrMessageIteratorRequired reports a store returning no iterator and no
	// error.
	//
	// Deprecated: wrappers preserve a downstream nil iterator and nil error.
	ErrMessageIteratorRequired = errors.New(
		"event-sourcing/gotelemetry: message iterator is required",
	)
)

// WrapEventStore instruments append and complete bounded stream-read
// operations without recording stream or message identity.
func (instrumentation *Instrumentation) WrapEventStore(
	next eventsourcing.EventStore,
) (eventsourcing.EventStore, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrEventStoreRequired
	}
	return eventStore{instrumentation: instrumentation, next: next}, nil
}

// WrapGlobalReader instruments complete bounded global-read operations without
// recording positions, stream identity, or message identity.
func (instrumentation *Instrumentation) WrapGlobalReader(
	next eventsourcing.GlobalReader,
) (eventsourcing.GlobalReader, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrGlobalReaderRequired
	}
	return globalReader{instrumentation: instrumentation, next: next}, nil
}

type eventStore struct {
	instrumentation *Instrumentation
	next            eventsourcing.EventStore
}

func (store eventStore) Append(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) (messages []eventsourcing.Message, operationErr error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	ctx, span := startStoreSpan(
		store.instrumentation,
		ctx,
		"append",
	)
	started := time.Now()
	defer finishStoreCall(
		store.instrumentation,
		ctx,
		span,
		"append",
		started,
		int64(len(pending)),
		&operationErr,
	)
	return store.next.Append(ctx, stream, expected, pending)
}

func (store eventStore) ReadStream(
	ctx context.Context,
	stream eventsourcing.StreamID,
	options eventsourcing.ReadStreamOptions,
) (
	iterator eventsourcing.MessageIterator,
	operationErr error,
) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	ctx, span := startStoreSpan(
		store.instrumentation,
		ctx,
		"read_stream",
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			finishStoreOperation(
				store.instrumentation,
				ctx,
				span,
				"read_stream",
				started,
				0,
				nil,
				panicValue,
			)
			panic(panicValue)
		}
		if operationErr != nil {
			finishStoreOperation(
				store.instrumentation,
				ctx,
				span,
				"read_stream",
				started,
				0,
				operationErr,
				nil,
			)
		}
	}()
	iterator, operationErr = store.next.ReadStream(ctx, stream, options)
	if operationErr != nil {
		return nil, operationErr
	}
	if iterator == nil {
		finishStoreOperation(
			store.instrumentation,
			ctx,
			span,
			"read_stream",
			started,
			0,
			nil,
			nil,
		)
		return nil, nil
	}
	return &storeIterator{
		instrumentation: store.instrumentation,
		next:            iterator,
		ctx:             ctx,
		span:            span,
		operation:       "read_stream",
		started:         started,
	}, nil
}

type globalReader struct {
	instrumentation *Instrumentation
	next            eventsourcing.GlobalReader
}

func (reader globalReader) ReadGlobal(
	ctx context.Context,
	options eventsourcing.ReadGlobalOptions,
) (
	iterator eventsourcing.MessageIterator,
	operationErr error,
) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	ctx, span := startStoreSpan(
		reader.instrumentation,
		ctx,
		"read_global",
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			finishStoreOperation(
				reader.instrumentation,
				ctx,
				span,
				"read_global",
				started,
				0,
				nil,
				panicValue,
			)
			panic(panicValue)
		}
		if operationErr != nil {
			finishStoreOperation(
				reader.instrumentation,
				ctx,
				span,
				"read_global",
				started,
				0,
				operationErr,
				nil,
			)
		}
	}()
	iterator, operationErr = reader.next.ReadGlobal(ctx, options)
	if operationErr != nil {
		return nil, operationErr
	}
	if iterator == nil {
		finishStoreOperation(
			reader.instrumentation,
			ctx,
			span,
			"read_global",
			started,
			0,
			nil,
			nil,
		)
		return nil, nil
	}
	return &storeIterator{
		instrumentation: reader.instrumentation,
		next:            iterator,
		ctx:             ctx,
		span:            span,
		operation:       "read_global",
		started:         started,
	}, nil
}

type storeIterator struct {
	instrumentation *Instrumentation
	next            eventsourcing.MessageIterator
	ctx             context.Context
	span            trace.Span
	operation       string
	started         time.Time
	count           int64
	err             error
	finished        bool
}

func (iterator *storeIterator) Next(ctx context.Context) (available bool) {
	if ctx == nil {
		iterator.err = ErrContextRequired
		iterator.finish(iterator.err, nil)
		return false
	}
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			iterator.finish(nil, panicValue)
			panic(panicValue)
		}
	}()
	available = iterator.next.Next(trace.ContextWithSpan(ctx, iterator.span))
	if available {
		iterator.count++
		return true
	}
	if err := iterator.next.Err(); err != nil {
		iterator.err = err
		iterator.finish(err, nil)
	}
	return false
}

func (iterator *storeIterator) Message() (message eventsourcing.Message) {
	defer iterator.finishOnPanic()
	return iterator.next.Message()
}

func (iterator *storeIterator) Err() (operationErr error) {
	defer iterator.finishOnPanic()
	if iterator.err != nil {
		return iterator.err
	}
	return iterator.next.Err()
}

func (iterator *storeIterator) Close() (operationErr error) {
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			iterator.finish(nil, panicValue)
			panic(panicValue)
		}
		iterator.finish(operationErr, nil)
	}()
	return iterator.next.Close()
}

func (iterator *storeIterator) finish(operationErr error, panicValue any) {
	if iterator.finished {
		return
	}
	iterator.finished = true
	finishStoreOperation(
		iterator.instrumentation,
		iterator.ctx,
		iterator.span,
		iterator.operation,
		iterator.started,
		iterator.count,
		operationErr,
		panicValue,
	)
	iterator.instrumentation = nil
	iterator.ctx = context.Background()
	iterator.span = trace.SpanFromContext(iterator.ctx)
}

func (iterator *storeIterator) finishOnPanic() {
	panicValue := recover()
	if panicValue == nil {
		return
	}
	iterator.finish(nil, panicValue)
	panic(panicValue)
}

func startStoreSpan(
	instrumentation *Instrumentation,
	ctx context.Context,
	operation string,
) (context.Context, trace.Span) {
	return instrumentation.tracer.Start(
		ctx,
		"event_sourcing.store."+operation,
		trace.WithAttributes(
			attribute.String("event_sourcing.operation", operation),
		),
	)
}

func finishStoreCall(
	instrumentation *Instrumentation,
	ctx context.Context,
	span trace.Span,
	operation string,
	started time.Time,
	count int64,
	operationErr *error,
) {
	panicValue := recover()
	finishStoreOperation(
		instrumentation,
		ctx,
		span,
		operation,
		started,
		count,
		*operationErr,
		panicValue,
	)
	if panicValue != nil {
		panic(panicValue)
	}
}

func finishStoreOperation(
	instrumentation *Instrumentation,
	ctx context.Context,
	span trace.Span,
	operation string,
	started time.Time,
	count int64,
	operationErr error,
	panicValue any,
) {
	outcome := operationOutcome(operationErr, panicValue)
	span.SetAttributes(attribute.Int64("event_sourcing.message.count", count))
	if outcome != "success" {
		span.SetStatus(codes.Error, "event-sourcing store operation failed")
	}
	instrumentation.recordOperation(
		ctx,
		operation,
		outcome,
		time.Since(started),
	)
	span.End()
}
