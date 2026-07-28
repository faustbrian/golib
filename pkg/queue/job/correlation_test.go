package job_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/queue/job"
)

type correlationMessage string

func (message correlationMessage) Bytes() []byte { return []byte(message) }

func TestCorrelationMetadataIsClonedValidatedAndEncoded(t *testing.T) {
	metadata := &job.Metadata{
		Correlation: map[string]string{
			"correlation_id": "workflow",
			"request_id":     "message",
		},
	}
	message := job.NewMessage(
		correlationMessage("payload"),
		job.AllowOption{Metadata: metadata},
	)
	metadata.Correlation["request_id"] = "mutated"

	decoded, err := job.DecodeE(message.Bytes(), job.DefaultMaxMessageBytes)
	if err != nil {
		t.Fatalf("DecodeE() error = %v", err)
	}
	got := decoded.CorrelationMetadata()
	if got == nil {
		t.Fatal("CorrelationMetadata() returned nil")
	}
	if got["correlation_id"] != "workflow" || got["request_id"] != "message" {
		t.Fatalf("CorrelationMetadata() = %v", got)
	}
	got["request_id"] = "caller-mutation"
	if decoded.CorrelationMetadata()["request_id"] != "message" {
		t.Fatal("CorrelationMetadata() exposed mutable message state")
	}
}

func TestCorrelationMetadataRejectsUnboundedCarriers(t *testing.T) {
	metadata := &job.Metadata{Correlation: map[string]string{
		"correlation_id": "workflow",
		"request_id":     "message",
		"causation_id":   "parent",
		"unexpected":     "extra",
	}}
	message := job.NewMessage(
		correlationMessage("payload"),
		job.AllowOption{Metadata: metadata},
	)
	if err := message.Validate(); err == nil {
		t.Fatal("Validate() accepted too many correlation fields")
	}

	message.Metadata.Correlation = map[string]string{"": "value"}
	if err := message.Validate(); err == nil {
		t.Fatal("Validate() accepted an invalid correlation field")
	}
}

func TestAbsentCorrelationMetadataIsNil(t *testing.T) {
	var message *job.Message
	if message.CorrelationMetadata() != nil {
		t.Fatal("nil message returned correlation metadata")
	}
	message = &job.Message{}
	if message.CorrelationMetadata() != nil {
		t.Fatal("empty message returned correlation metadata")
	}
}

func TestTraceContextMetadataIsClonedValidatedAndEncoded(t *testing.T) {
	metadata := &job.Metadata{
		TraceContext: map[string]string{
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"tracestate":  "vendor=value",
		},
	}
	message := job.NewMessage(
		correlationMessage("payload"),
		job.AllowOption{Metadata: metadata},
	)
	metadata.TraceContext["tracestate"] = "mutated=value"

	decoded, err := job.DecodeE(message.Bytes(), job.DefaultMaxMessageBytes)
	if err != nil {
		t.Fatalf("DecodeE() error = %v", err)
	}
	got := decoded.TraceContextMetadata()
	if got == nil {
		t.Fatal("TraceContextMetadata() returned nil")
	}
	if got["tracestate"] != "vendor=value" {
		t.Fatalf("TraceContextMetadata() = %v", got)
	}
	got["tracestate"] = "caller=value"
	if decoded.TraceContextMetadata()["tracestate"] != "vendor=value" {
		t.Fatal("TraceContextMetadata() exposed mutable message state")
	}
}

func TestTraceContextMetadataRejectsUnboundedCarriers(t *testing.T) {
	tests := map[string]map[string]string{
		"blank key":       {"": "value"},
		"blank value":     {"traceparent": " "},
		"oversized key":   {strings.Repeat("k", job.MaxTraceContextFieldBytes+1): "value"},
		"oversized value": {"traceparent": strings.Repeat("x", job.MaxTraceContextValueBytes+1)},
		"oversized total": {
			"traceparent": strings.Repeat("x", job.MaxTraceContextBytes/2),
			"tracestate":  strings.Repeat("y", job.MaxTraceContextBytes/2),
		},
	}
	for name, traceContext := range tests {
		message := job.NewMessage(
			correlationMessage("payload"),
			job.AllowOption{Metadata: &job.Metadata{TraceContext: traceContext}},
		)
		if err := message.Validate(); err == nil {
			t.Fatalf("Validate() accepted %s trace context", name)
		}
	}

	tooManyFields := make(map[string]string, job.MaxTraceContextFields+1)
	for index := 0; index <= job.MaxTraceContextFields; index++ {
		tooManyFields[string(rune('a'+index))] = "value"
	}
	message := job.NewMessage(
		correlationMessage("payload"),
		job.AllowOption{Metadata: &job.Metadata{TraceContext: tooManyFields}},
	)
	if err := message.Validate(); err == nil {
		t.Fatal("Validate() accepted too many trace context fields")
	}
}

func TestAbsentTraceContextMetadataIsNil(t *testing.T) {
	var message *job.Message
	if message.TraceContextMetadata() != nil {
		t.Fatal("nil message returned trace context metadata")
	}
	message = &job.Message{}
	if message.TraceContextMetadata() != nil {
		t.Fatal("empty message returned trace context metadata")
	}
}
