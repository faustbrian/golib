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
	// ErrPayloadCodecRequired reports a missing downstream payload codec.
	ErrPayloadCodecRequired = errors.New(
		"event-sourcing/gotelemetry: payload codec is required",
	)
	// ErrUpcasterRequired reports a missing downstream upcaster.
	ErrUpcasterRequired = errors.New(
		"event-sourcing/gotelemetry: upcaster is required",
	)
)

// WrapPayloadCodec instruments payload encoding and decoding without recording
// event identity, schema, content type, payload, decoded values, or failures.
func (instrumentation *Instrumentation) WrapPayloadCodec(
	next eventsourcing.PayloadCodec,
) (eventsourcing.ContextPayloadCodec, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrPayloadCodecRequired
	}

	return payloadCodec{
		instrumentation: instrumentation,
		next:            next,
	}, nil
}

// WrapUpcaster instruments read-boundary evolution without recording event
// identity, schema, payload, metadata, output values, or failures.
func (instrumentation *Instrumentation) WrapUpcaster(
	next eventsourcing.Upcaster,
) (eventsourcing.ContextUpcaster, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrUpcasterRequired
	}

	return upcaster{
		instrumentation: instrumentation,
		next:            next,
	}, nil
}

type payloadCodec struct {
	instrumentation *Instrumentation
	next            eventsourcing.PayloadCodec
}

func (codec payloadCodec) Encode(
	event eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	return codec.encode(context.Background(), event, false)
}

func (codec payloadCodec) EncodeContext(
	ctx context.Context,
	event eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	if ctx == nil {
		return eventsourcing.EncodedEvent{}, ErrContextRequired
	}

	return codec.encode(ctx, event, true)
}

func (codec payloadCodec) encode(
	ctx context.Context,
	event eventsourcing.DecodedEvent,
	contextual bool,
) (
	output eventsourcing.EncodedEvent,
	operationErr error,
) {
	ctx, span := codec.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.codec.encode",
	)
	started := time.Now()
	defer finishSerializationOperation(
		codec.instrumentation,
		ctx,
		span,
		"codec_encode",
		"event-sourcing payload encode failed",
		started,
		&operationErr,
	)
	if contextual {
		if next, ok := codec.next.(eventsourcing.ContextPayloadCodec); ok {
			return next.EncodeContext(ctx, event)
		}
	}

	return codec.next.Encode(event)
}

func (codec payloadCodec) Decode(
	event eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	return codec.decode(context.Background(), event, false)
}

func (codec payloadCodec) DecodeContext(
	ctx context.Context,
	event eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	if ctx == nil {
		return eventsourcing.DecodedEvent{}, ErrContextRequired
	}

	return codec.decode(ctx, event, true)
}

func (codec payloadCodec) decode(
	ctx context.Context,
	event eventsourcing.EncodedEvent,
	contextual bool,
) (
	output eventsourcing.DecodedEvent,
	operationErr error,
) {
	ctx, span := codec.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.codec.decode",
	)
	started := time.Now()
	defer finishSerializationOperation(
		codec.instrumentation,
		ctx,
		span,
		"codec_decode",
		"event-sourcing payload decode failed",
		started,
		&operationErr,
	)
	if contextual {
		if next, ok := codec.next.(eventsourcing.ContextPayloadCodec); ok {
			return next.DecodeContext(ctx, event)
		}
	}

	return codec.next.Decode(event)
}

type upcaster struct {
	instrumentation *Instrumentation
	next            eventsourcing.Upcaster
}

func (upcaster upcaster) Upcast(
	event eventsourcing.UpcastEvent,
) ([]eventsourcing.UpcastEvent, error) {
	return upcaster.upcast(context.Background(), event, false)
}

func (upcaster upcaster) UpcastContext(
	ctx context.Context,
	event eventsourcing.UpcastEvent,
) ([]eventsourcing.UpcastEvent, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	return upcaster.upcast(ctx, event, true)
}

func (upcaster upcaster) upcast(
	ctx context.Context,
	event eventsourcing.UpcastEvent,
	contextual bool,
) (
	output []eventsourcing.UpcastEvent,
	operationErr error,
) {
	ctx, span := upcaster.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.upcast",
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		outcome := operationOutcome(operationErr, panicValue)
		if outcome == "success" {
			span.SetAttributes(
				attribute.Int(
					"event_sourcing.upcast.output_count",
					len(output),
				),
			)
		} else {
			span.SetStatus(codes.Error, "event-sourcing upcast failed")
		}
		upcaster.instrumentation.recordOperation(
			ctx,
			"upcast",
			outcome,
			time.Since(started),
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()
	if contextual {
		if next, ok := upcaster.next.(eventsourcing.ContextUpcaster); ok {
			return next.UpcastContext(ctx, event)
		}
	}

	return upcaster.next.Upcast(event)
}

func finishSerializationOperation(
	instrumentation *Instrumentation,
	ctx context.Context,
	span trace.Span,
	operation string,
	failureStatus string,
	started time.Time,
	operationErr *error,
) {
	panicValue := recover()
	outcome := operationOutcome(*operationErr, panicValue)
	if outcome != "success" {
		span.SetStatus(codes.Error, failureStatus)
	}
	instrumentation.recordOperation(
		ctx,
		operation,
		outcome,
		time.Since(started),
	)
	span.End()
	if panicValue != nil {
		panic(panicValue)
	}
}
