package queueservice

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/job"
	"go.opentelemetry.io/otel/propagation"
)

func FuzzTransportMetadataIsolation(f *testing.F) {
	f.Add(
		"source", "api", "correlation_id", "workflow",
		"traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		int64(10),
	)
	f.Fuzz(func(
		t *testing.T,
		tagKey string,
		tagValue string,
		correlationKey string,
		correlationValue string,
		traceKey string,
		traceValue string,
		unixSeconds int64,
	) {
		tagKey = boundedFuzzString(tagKey)
		tagValue = boundedFuzzString(tagValue)
		correlationKey = boundedFuzzString(correlationKey)
		correlationValue = boundedFuzzString(correlationValue)
		traceKey = boundedFuzzString(traceKey)
		traceValue = boundedFuzzString(traceValue)

		enqueuedAt := time.Unix(unixSeconds, 0).UTC()
		metadata := &job.Metadata{
			EnqueuedAt:   &enqueuedAt,
			Tags:         map[string]string{tagKey: tagValue},
			TraceContext: map[string]string{"original": "trace"},
		}
		options := []job.AllowOption{{Metadata: metadata}, {}}
		correlationCarrier := map[string]string{
			correlationKey: correlationValue,
		}
		traceCarrier := propagation.MapCarrier{traceKey: traceValue}

		copied := withTransportMetadata(
			options,
			correlationCarrier,
			traceCarrier,
		)
		if len(copied) != len(options) || copied[0].Metadata == nil {
			t.Fatalf("copied options = %#v", copied)
		}
		if copied[0].Metadata == metadata || copied[0].Metadata.EnqueuedAt == metadata.EnqueuedAt {
			t.Fatal("transport metadata retained caller-owned pointers")
		}
		if copied[0].Metadata.Tags[tagKey] != tagValue ||
			copied[0].Metadata.Correlation[correlationKey] != correlationValue ||
			copied[0].Metadata.TraceContext[traceKey] != traceValue {
			t.Fatal("transport metadata did not preserve supplied values")
		}

		copied[0].Metadata.Tags[tagKey] = "changed"
		copied[0].Metadata.Correlation[correlationKey] = "changed"
		copied[0].Metadata.TraceContext[traceKey] = "changed"
		*copied[0].Metadata.EnqueuedAt = time.Unix(0, 0).UTC()
		copied[1].Metadata = &job.Metadata{}
		if metadata.Tags[tagKey] != tagValue ||
			!metadata.EnqueuedAt.Equal(enqueuedAt) ||
			options[1].Metadata != nil ||
			correlationCarrier[correlationKey] != correlationValue ||
			traceCarrier[traceKey] != traceValue {
			t.Fatal("transport metadata mutation escaped into caller-owned input")
		}
	})
}

func boundedFuzzString(value string) string {
	const maximumBytes = 256
	if len(value) > maximumBytes {
		return value[:maximumBytes]
	}

	return value
}
