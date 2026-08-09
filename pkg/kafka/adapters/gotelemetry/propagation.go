package gotelemetry

import (
	"bytes"
	"context"
	"strings"

	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/propagation"
)

// TraceContextPropagation copies only W3C Trace Context fields between Kafka
// record headers and contexts. It deliberately excludes baggage and global
// OpenTelemetry propagator state. A constructed value is immutable and safe
// for concurrent use.
type TraceContextPropagation struct {
	limits     kafka.MessageLimits
	propagator propagation.TraceContext
}

// NewTraceContextPropagation validates the Kafka record limits used before and
// after trace-header processing. Callers should supply the same limits as the
// producer and consumer that exchange the records.
func NewTraceContextPropagation(
	limits kafka.MessageLimits,
) (TraceContextPropagation, error) {
	if err := limits.Validate(); err != nil {
		return TraceContextPropagation{}, err
	}

	return TraceContextPropagation{limits: limits}, nil
}

// Inject returns an owned record with stale W3C trace fields replaced from
// ctx. The caller's record and bytes are never mutated or retained. Baggage is
// not injected. It returns kafka.ErrContextRequired for a nil context and the
// applicable Kafka record-validation error when the input or injected output
// exceeds the configured limits. Context cancellation does not suppress this
// synchronous in-memory operation.
func (policy TraceContextPropagation) Inject(
	ctx context.Context,
	record kafka.ProducerRecord,
) (kafka.ProducerRecord, error) {
	if ctx == nil {
		return kafka.ProducerRecord{}, kafka.ErrContextRequired
	}
	if err := record.Validate(policy.limits); err != nil {
		return kafka.ProducerRecord{}, err
	}

	injected := cloneProducerRecord(record)
	fields := policy.propagator.Fields()
	injected.Headers = removePropagationHeaders(injected.Headers, fields)
	carrier := traceHeaderCarrier{headers: injected.Headers}
	policy.propagator.Inject(ctx, &carrier)
	injected.Headers = carrier.headers
	if err := injected.Validate(policy.limits); err != nil {
		return kafka.ProducerRecord{}, err
	}

	return injected, nil
}

// Extract returns a context containing a remote W3C trace context from record.
// An invalid traceparent or ASCII-case-insensitive duplicate W3C fields leave
// the supplied context unchanged. An invalid tracestate is ignored according
// to the OpenTelemetry W3C propagator contract. The borrowed record is read
// synchronously and is never mutated or retained. Baggage is not extracted.
// It returns kafka.ErrContextRequired for a nil context and the applicable
// Kafka record-validation error when the record exceeds the configured limits.
func (policy TraceContextPropagation) Extract(
	ctx context.Context,
	record kafka.ConsumedRecord,
) (context.Context, error) {
	if ctx == nil {
		return nil, kafka.ErrContextRequired
	}
	if err := (kafka.ProducerRecord{
		Topic:   record.Topic,
		Key:     record.Key,
		Value:   record.Value,
		Headers: record.Headers,
	}).Validate(policy.limits); err != nil {
		return nil, err
	}

	fields := policy.propagator.Fields()
	if hasDuplicatePropagationFields(record.Headers, fields) {
		return ctx, nil
	}
	carrier := traceHeaderCarrier{headers: record.Headers}

	return policy.propagator.Extract(ctx, &carrier), nil
}

func hasDuplicatePropagationFields(
	headers []kafka.Header,
	fields []string,
) bool {
	for _, field := range fields {
		found := false
		for _, header := range headers {
			if !equalASCIIFold(field, header.Key) {
				continue
			}
			if found {
				return true
			}
			found = true
		}
	}

	return false
}

func cloneProducerRecord(record kafka.ProducerRecord) kafka.ProducerRecord {
	cloned := record
	cloned.Key = bytes.Clone(record.Key)
	cloned.Value = bytes.Clone(record.Value)
	cloned.Headers = make([]kafka.Header, len(record.Headers))
	for index, header := range record.Headers {
		cloned.Headers[index] = kafka.Header{
			Key:   strings.Clone(header.Key),
			Value: bytes.Clone(header.Value),
		}
	}

	return cloned
}

func removePropagationHeaders(
	headers []kafka.Header,
	fields []string,
) []kafka.Header {
	retained := headers[:0]
	for _, header := range headers {
		if propagationField(fields, header.Key) {
			continue
		}
		retained = append(retained, header)
	}

	return retained
}

func propagationField(fields []string, key string) bool {
	for _, field := range fields {
		if equalASCIIFold(field, key) {
			return true
		}
	}

	return false
}

func equalASCIIFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range len(left) {
		leftByte := left[index]
		if leftByte >= 'A' && leftByte <= 'Z' {
			leftByte += 'a' - 'A'
		}
		rightByte := right[index]
		if rightByte >= 'A' && rightByte <= 'Z' {
			rightByte += 'a' - 'A'
		}
		if leftByte != rightByte {
			return false
		}
	}

	return true
}

type traceHeaderCarrier struct {
	headers []kafka.Header
}

func (carrier *traceHeaderCarrier) Get(key string) string {
	value := ""
	found := false
	for _, header := range carrier.headers {
		if equalASCIIFold(header.Key, key) {
			if found {
				return ""
			}
			value = string(header.Value)
			found = true
		}
	}

	return value
}

func (carrier *traceHeaderCarrier) Set(key, value string) {
	carrier.headers = removePropagationHeaders(carrier.headers, []string{key})
	carrier.headers = append(carrier.headers, kafka.Header{
		Key:   key,
		Value: []byte(value),
	})
}

func (carrier *traceHeaderCarrier) Keys() []string {
	keys := make([]string, len(carrier.headers))
	for index, header := range carrier.headers {
		keys[index] = header.Key
	}

	return keys
}
