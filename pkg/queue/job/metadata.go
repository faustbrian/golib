package job

import (
	"fmt"
	"strings"
	"time"
)

const (
	// MaxMetadataValueBytes bounds every user-supplied metadata identity.
	MaxMetadataValueBytes = 256
	// MaxMetadataTags bounds user-supplied metadata dimensions.
	MaxMetadataTags = 32
	// MaxCorrelationFields bounds the transport-neutral correlation carrier.
	MaxCorrelationFields = 3
	// MaxCorrelationFieldBytes matches correlation codec field-name bounds.
	MaxCorrelationFieldBytes = 64
	// MaxCorrelationValueBytes matches the largest supported correlation ID.
	MaxCorrelationValueBytes = 1024
	// MaxTraceContextFields bounds caller-owned telemetry propagation carriers.
	MaxTraceContextFields = 16
	// MaxTraceContextFieldBytes bounds telemetry propagation field names.
	MaxTraceContextFieldBytes = 64
	// MaxTraceContextValueBytes bounds one telemetry propagation field value.
	MaxTraceContextValueBytes = 8192
	// MaxTraceContextBytes bounds the complete telemetry propagation carrier.
	MaxTraceContextBytes = 8192
)

// Metadata carries optional, backend-neutral job identity into failure and
// dead-letter records. Empty fields remain unknown and must not be fabricated.
type Metadata struct {
	OriginalID           string            `json:"original_id,omitempty" msgpack:"original_id,omitempty"`
	PayloadSchemaVersion string            `json:"payload_schema_version,omitempty" msgpack:"payload_schema_version,omitempty"`
	ContentType          string            `json:"content_type,omitempty" msgpack:"content_type,omitempty"`
	EnqueuedAt           *time.Time        `json:"enqueued_at,omitempty" msgpack:"enqueued_at,omitempty"`
	RetryPolicy          string            `json:"retry_policy,omitempty" msgpack:"retry_policy,omitempty"`
	HandlerType          string            `json:"handler_type,omitempty" msgpack:"handler_type,omitempty"`
	JobType              string            `json:"job_type,omitempty" msgpack:"job_type,omitempty"`
	Tags                 map[string]string `json:"tags,omitempty" msgpack:"tags,omitempty"`
	TraceID              string            `json:"trace_id,omitempty" msgpack:"trace_id,omitempty"`
	TenantID             string            `json:"tenant_id,omitempty" msgpack:"tenant_id,omitempty"`
	ProducerVersion      string            `json:"producer_version,omitempty" msgpack:"producer_version,omitempty"`
	Correlation          map[string]string `json:"correlation,omitempty" msgpack:"correlation,omitempty"`
	TraceContext         map[string]string `json:"trace_context,omitempty" msgpack:"trace_context,omitempty"`
}

// Validate rejects unbounded identity, tag, and time metadata.
func (m Metadata) Validate() error {
	for field, value := range map[string]string{
		"original_id":            m.OriginalID,
		"payload_schema_version": m.PayloadSchemaVersion,
		"content_type":           m.ContentType,
		"retry_policy":           m.RetryPolicy,
		"handler_type":           m.HandlerType,
		"job_type":               m.JobType,
		"trace_id":               m.TraceID,
		"tenant_id":              m.TenantID,
		"producer_version":       m.ProducerVersion,
	} {
		if value != "" && (strings.TrimSpace(value) == "" || len(value) > MaxMetadataValueBytes) {
			return fmt.Errorf("%w: metadata %s must be bounded", ErrInvalidMessage, field)
		}
	}
	if m.EnqueuedAt != nil && m.EnqueuedAt.IsZero() {
		return fmt.Errorf("%w: metadata enqueued_at must be non-zero", ErrInvalidMessage)
	}
	if len(m.Tags) > MaxMetadataTags {
		return fmt.Errorf("%w: metadata tags exceed the tag limit", ErrInvalidMessage)
	}
	for key, value := range m.Tags {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" ||
			len(key) > MaxMetadataValueBytes || len(value) > MaxMetadataValueBytes {
			return fmt.Errorf("%w: metadata tag keys and values must be bounded", ErrInvalidMessage)
		}
	}
	if len(m.Correlation) > MaxCorrelationFields {
		return fmt.Errorf(
			"%w: metadata correlation fields exceed the field limit",
			ErrInvalidMessage,
		)
	}
	for key, value := range m.Correlation {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" ||
			len(key) > MaxCorrelationFieldBytes ||
			len(value) > MaxCorrelationValueBytes {
			return fmt.Errorf(
				"%w: metadata correlation keys and values must be bounded",
				ErrInvalidMessage,
			)
		}
	}
	if len(m.TraceContext) > MaxTraceContextFields {
		return fmt.Errorf(
			"%w: metadata trace context fields exceed the field limit",
			ErrInvalidMessage,
		)
	}
	traceContextBytes := 0
	for key, value := range m.TraceContext {
		traceContextBytes += len(key) + len(value)
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" ||
			len(key) > MaxTraceContextFieldBytes ||
			len(value) > MaxTraceContextValueBytes ||
			traceContextBytes > MaxTraceContextBytes {
			return fmt.Errorf(
				"%w: metadata trace context keys and values must be bounded",
				ErrInvalidMessage,
			)
		}
	}

	return nil
}

func cloneMetadata(metadata *Metadata) *Metadata {
	if metadata == nil {
		return nil
	}

	clone := *metadata
	if metadata.EnqueuedAt != nil {
		enqueuedAt := *metadata.EnqueuedAt
		clone.EnqueuedAt = &enqueuedAt
	}
	if metadata.Tags != nil {
		clone.Tags = make(map[string]string, len(metadata.Tags))
		for key, value := range metadata.Tags {
			clone.Tags[key] = value
		}
	}
	if metadata.Correlation != nil {
		clone.Correlation = make(map[string]string, len(metadata.Correlation))
		for key, value := range metadata.Correlation {
			clone.Correlation[key] = value
		}
	}
	if metadata.TraceContext != nil {
		clone.TraceContext = make(map[string]string, len(metadata.TraceContext))
		for key, value := range metadata.TraceContext {
			clone.TraceContext[key] = value
		}
	}

	return &clone
}
