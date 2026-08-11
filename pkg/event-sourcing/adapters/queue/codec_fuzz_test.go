package eventqueue

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func FuzzCodecDecode(f *testing.F) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		f.Fatalf("NewCodec() error = %v", err)
	}
	for _, seed := range codecHostileSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > job.DefaultMaxMessageBytes+1 {
			t.Skip()
		}
		delivery, err := codec.Decode(input)
		if err != nil &&
			!errors.Is(err, ErrEnvelopeInvalid) &&
			!errors.Is(err, ErrEnvelopeTooLarge) {
			t.Fatalf("Decode() error = %v", err)
		}
		if err != nil {
			wantDiagnostic := ErrEnvelopeInvalid.Error()
			if errors.Is(err, ErrEnvelopeTooLarge) {
				wantDiagnostic = ErrEnvelopeTooLarge.Error()
			}
			if err.Error() != wantDiagnostic {
				t.Fatalf("Decode() diagnostic = %q", err.Error())
			}
			return
		}
		reencoded, err := codec.Encode(delivery)
		if err != nil {
			t.Fatalf("Encode(decoded) error = %v", err)
		}
		if !bytes.Equal(reencoded, input) {
			t.Fatalf("successful Decode() was not canonical")
		}
	})
}

func FuzzDispatcherRetryMetadata(f *testing.F) {
	f.Add(
		int64(0),
		int64(0),
		uint64(0),
		uint64(time.Millisecond),
		uint64(time.Second),
		uint64(time.Minute),
		uint64(1),
		false,
		[]byte("metadata"),
	)
	f.Add(
		int64(3),
		int64(time.Second),
		uint64(1_000),
		uint64(time.Second),
		uint64(time.Second),
		uint64(time.Hour),
		uint64(2),
		true,
		[]byte{},
	)
	f.Fuzz(func(
		t *testing.T,
		retryCountInput int64,
		retryDelayInput int64,
		retryFactorInput uint64,
		retryMinInput uint64,
		retryMaxSpanInput uint64,
		timeoutInput uint64,
		enqueuedAtInput uint64,
		jitter bool,
		metadataInput []byte,
	) {
		if len(metadataInput) > 120 {
			t.Skip()
		}
		codec, err := NewCodec(CodecConfig{})
		if err != nil {
			t.Fatalf("NewCodec() error = %v", err)
		}
		retryCount := int64(uint64(retryCountInput) % uint64(job.MaxRetryCount+1))
		delay := time.Duration(uint64(retryDelayInput) % uint64(time.Minute+1))
		factor := 1 + float64(retryFactorInput%9_000)/1_000
		retryMin := time.Duration(retryMinInput%uint64(time.Minute)) + 1
		retryMax := retryMin +
			time.Duration(retryMaxSpanInput%uint64(time.Minute))
		timeout := time.Duration(timeoutInput%uint64(time.Hour)) + 1
		enqueuedAt := time.Unix(
			int64(enqueuedAtInput%uint64(100*365*24*time.Hour/time.Second))+1,
			0,
		).UTC()
		encodedMetadata := base64.RawURLEncoding.EncodeToString(metadataInput)
		retryPolicy := "policy:" + encodedMetadata
		handlerType := "handler:" + encodedMetadata
		tagValue := "tag:" + encodedMetadata
		traceID := "trace:" + encodedMetadata
		producerVersion := "producer:" + encodedMetadata
		correlationValue := "correlation:" + encodedMetadata
		traceContextValue := "trace-context:" + encodedMetadata
		metadata := &job.Metadata{
			EnqueuedAt:      &enqueuedAt,
			RetryPolicy:     retryPolicy,
			HandlerType:     handlerType,
			Tags:            map[string]string{"fuzz": tagValue},
			TraceID:         traceID,
			ProducerVersion: producerVersion,
			Correlation:     map[string]string{"request_id": correlationValue},
			TraceContext:    map[string]string{"traceparent": traceContextValue},
		}
		option := job.AllowOption{
			RetryCount:  &retryCount,
			RetryDelay:  &delay,
			RetryFactor: &factor,
			RetryMin:    &retryMin,
			RetryMax:    &retryMax,
			Jitter:      &jitter,
			Timeout:     &timeout,
			Metadata:    metadata,
		}
		wantRetryCount := retryCount
		wantDelay := delay
		wantFactor := factor
		wantRetryMin := retryMin
		wantRetryMax := retryMax
		wantJitter := jitter
		wantTimeout := timeout
		wantEnqueuedAt := enqueuedAt
		queue := &fuzzCaptureQueue{}
		dispatcher, err := NewDispatcher(DispatcherConfig{
			Queue: queue,
			Codec: codec,
			Job:   option,
		})
		if err != nil {
			t.Fatalf("NewDispatcher(valid option) error = %v", err)
		}
		retryCount = -1
		delay = -1
		factor = math.NaN()
		retryMin = -1
		retryMax = -1
		jitter = !jitter
		timeout = -1
		enqueuedAt = time.Time{}
		metadata.RetryPolicy = "mutated"
		metadata.HandlerType = "mutated"
		metadata.Tags["fuzz"] = "mutated"
		metadata.TraceID = "mutated"
		metadata.ProducerVersion = "mutated"
		metadata.Correlation["request_id"] = "mutated"
		metadata.TraceContext["traceparent"] = "mutated"
		option.Metadata = &job.Metadata{RetryPolicy: "replaced"}
		delivery := minimalQueueDelivery(t)
		if err := dispatcher.Dispatch(
			context.Background(),
			[]eventsourcing.Delivery{delivery},
		); err != nil {
			t.Fatalf("Dispatch(valid option) error = %v", err)
		}
		if queue.calls != 1 ||
			queue.option.RetryCount == nil || *queue.option.RetryCount != wantRetryCount ||
			queue.option.RetryDelay == nil || *queue.option.RetryDelay != wantDelay ||
			queue.option.RetryFactor == nil || *queue.option.RetryFactor != wantFactor ||
			queue.option.RetryMin == nil || *queue.option.RetryMin != wantRetryMin ||
			queue.option.RetryMax == nil || *queue.option.RetryMax != wantRetryMax ||
			queue.option.Jitter == nil || *queue.option.Jitter != wantJitter ||
			queue.option.Timeout == nil || *queue.option.Timeout != wantTimeout ||
			queue.option.Metadata == nil ||
			queue.option.Metadata.EnqueuedAt == nil ||
			!queue.option.Metadata.EnqueuedAt.Equal(wantEnqueuedAt) ||
			queue.option.Metadata.RetryPolicy != retryPolicy ||
			queue.option.Metadata.HandlerType != handlerType ||
			queue.option.Metadata.Tags["fuzz"] != tagValue ||
			queue.option.Metadata.TraceID != traceID ||
			queue.option.Metadata.ProducerVersion != producerVersion ||
			queue.option.Metadata.Correlation["request_id"] != correlationValue ||
			queue.option.Metadata.TraceContext["traceparent"] != traceContextValue ||
			queue.option.Metadata.OriginalID != "message-1" ||
			queue.option.Metadata.PayloadSchemaVersion != "1" ||
			queue.option.Metadata.ContentType != "application/json" ||
			queue.option.Metadata.JobType != "account.opened" {
			t.Fatalf("queued retry metadata = %#v", queue.option)
		}
		decoded, err := codec.Decode(queue.message)
		if err != nil {
			t.Fatalf("Decode(queued payload) error = %v", err)
		}
		if decoded.Mode() != delivery.Mode() || !decoded.Message().Equal(delivery.Message()) {
			t.Fatalf("Decode(queued payload) = %#v", decoded)
		}
	})
}

func FuzzDispatcherRejectsInvalidRetryMetadata(f *testing.F) {
	f.Add(uint8(0), uint64(1))
	f.Add(uint8(1), uint64(2))
	f.Add(uint8(2), uint64(3))
	f.Add(uint8(3), uint64(4))
	f.Add(uint8(4), uint64(5))
	f.Add(uint8(5), uint64(6))
	f.Add(uint8(6), uint64(7))
	f.Add(uint8(7), uint64(8))
	f.Add(uint8(8), uint64(9))
	f.Add(uint8(9), uint64(10))
	f.Fuzz(func(t *testing.T, invalidField uint8, magnitude uint64) {
		codec, err := NewCodec(CodecConfig{})
		if err != nil {
			t.Fatalf("NewCodec() error = %v", err)
		}
		retryCount := int64(1)
		delay := time.Duration(0)
		factor := float64(2)
		retryMin := time.Second
		retryMax := 2 * time.Second
		timeout := time.Minute
		enqueuedAt := time.Unix(1, 0).UTC()
		metadata := &job.Metadata{
			EnqueuedAt:  &enqueuedAt,
			RetryPolicy: "policy",
		}
		switch invalidField % 10 {
		case 0:
			retryCount = -int64(magnitude%1_000) - 1
		case 1:
			retryCount = job.MaxRetryCount + int64(magnitude%1_000) + 1
		case 2:
			delay = -time.Duration(magnitude%uint64(time.Minute)) - 1
		case 3:
			factor = math.NaN()
		case 4:
			retryMin = 0
		case 5:
			retryMax = retryMin - 1
		case 6:
			timeout = -time.Duration(magnitude%uint64(time.Minute)) - 1
		case 7:
			enqueuedAt = time.Time{}
		case 8:
			metadata.RetryPolicy = strings.Repeat("x", job.MaxMetadataValueBytes+1)
		case 9:
			factor = math.Inf(1)
		}
		dispatcher, constructErr := NewDispatcher(DispatcherConfig{
			Queue: &fuzzCaptureQueue{},
			Codec: codec,
			Job: job.AllowOption{
				RetryCount:  &retryCount,
				RetryDelay:  &delay,
				RetryFactor: &factor,
				RetryMin:    &retryMin,
				RetryMax:    &retryMax,
				Timeout:     &timeout,
				Metadata:    metadata,
			},
		})
		if dispatcher != nil || !errors.Is(constructErr, ErrInvalidJobOption) {
			t.Fatalf("NewDispatcher(invalid field %d) = %#v, %v", invalidField%10, dispatcher, constructErr)
		}
	})
}

func FuzzCodecPayloadLimits(f *testing.F) {
	f.Add(uint32(0), uint8(0))
	f.Add(uint32(1), uint8(1))
	f.Add(uint32(4_096), uint8(2))
	f.Fuzz(func(t *testing.T, payloadBytes uint32, boundary uint8) {
		delivery := queueDeliveryWithPayload(t, int(payloadBytes%4_096)+1)
		unbounded, err := NewCodec(CodecConfig{})
		if err != nil {
			t.Fatalf("NewCodec() error = %v", err)
		}
		encoded, err := unbounded.Encode(delivery)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
		limit := len(encoded)
		wantTooLarge := false
		switch boundary % 3 {
		case 0:
		case 1:
			limit--
			wantTooLarge = true
		case 2:
			limit++
		}
		codec, err := NewCodec(CodecConfig{MaxEnvelopeBytes: limit})
		if err != nil {
			t.Fatalf("NewCodec(limit %d) error = %v", limit, err)
		}
		reencoded, encodeErr := codec.Encode(delivery)
		_, decodeErr := codec.Decode(encoded)
		if wantTooLarge {
			if !errors.Is(encodeErr, ErrEnvelopeTooLarge) ||
				!errors.Is(decodeErr, ErrEnvelopeTooLarge) {
				t.Fatalf("under-limit encode/decode errors = %v/%v", encodeErr, decodeErr)
			}
			return
		}
		if encodeErr != nil || decodeErr != nil || !bytes.Equal(reencoded, encoded) {
			t.Fatalf(
				"accepted limit encode/decode = %v/%v, equal = %t",
				encodeErr,
				decodeErr,
				bytes.Equal(reencoded, encoded),
			)
		}
	})
}

type fuzzCaptureQueue struct {
	calls   int
	message []byte
	option  job.AllowOption
}

func (queue *fuzzCaptureQueue) Queue(
	message core.QueuedMessage,
	options ...job.AllowOption,
) error {
	if len(options) != 1 {
		panic("expected exactly one job option")
	}
	queue.calls++
	queue.message = message.Bytes()
	queue.option = options[0]
	return nil
}

func codecHostileSeeds() [][]byte {
	const minimal = `{"format":"golib.event-sourcing.queue.v1","delivery_mode":"live","message_id":"message-1","aggregate_type":"account","aggregate_id":"42","stream_version":1,"event_name":"account.opened","event_schema_version":1,"content_type":"application/json","payload":"e30=","recorded_at":"2026-07-25T12:34:56Z"}`
	valid := []byte(minimal)
	seeds := [][]byte{
		valid,
		{},
		{0xff, 0xfe, 0xfd},
		[]byte(strings.Replace(minimal, `"format":`, `"format":"duplicate","format":`, 1)),
		[]byte(strings.Replace(minimal, `"format":`, `"unknown":true,"format":`, 1)),
		[]byte(strings.Replace(minimal, `"stream_version":1`, `"stream_version":-1`, 1)),
		[]byte(strings.Replace(minimal, `"stream_version":1`, `"stream_version":1e999`, 1)),
		[]byte(strings.Replace(minimal, `"recorded_at":"2026-07-25T12:34:56Z"`, `"recorded_at":"bad"`, 1)),
		[]byte(strings.Replace(minimal, `"recorded_at":"2026-07-25T12:34:56Z"`, `"recorded_at":"2026-07-25T15:34:56+03:00"`, 1)),
		[]byte(strings.Replace(minimal, `"recorded_at":"2026-07-25T12:34:56Z"`, `"recorded_at":"2026-07-25T12:34:56.000000001Z"`, 1)),
		[]byte(strings.Replace(minimal, `"payload":"e30="`, `"payload":"%%%"`, 1)),
		[]byte(strings.Replace(minimal, `"recorded_at":`, `"metadata":{"secret":"value"},"recorded_at":`, 1)),
		[]byte(strings.Replace(minimal, envelopeFormat, "golib.event-sourcing.queue.v0", 1)),
		append(append([]byte(nil), valid...), 0),
		bytes.Repeat([]byte("x"), job.DefaultMaxMessageBytes+1),
	}
	for limit := 0; limit < len(valid); limit++ {
		seeds = append(seeds, append([]byte(nil), valid[:limit]...))
	}
	return seeds
}
