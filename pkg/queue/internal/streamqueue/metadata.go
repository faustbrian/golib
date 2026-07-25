package streamqueue

import (
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
)

// MessageMetadata returns validated operational metadata from the bounded job
// envelope. Malformed and legacy bodies have no trustworthy metadata.
func MessageMetadata(body []byte) *job.Metadata {
	message, err := job.DecodeE(body, job.DefaultMaxMessageBytes)
	if err != nil {
		return nil
	}

	return message.Metadata
}

// ApplyMessageMetadata copies only the public allowlist into a management
// record. Backend-derived source identity remains separate from OriginalID.
func ApplyMessageMetadata(record *management.JobRecord, metadata *job.Metadata) {
	if metadata == nil {
		return
	}
	if metadata.OriginalID != "" {
		record.OriginalID = metadata.OriginalID
	}
	record.PayloadSchemaVersion = metadata.PayloadSchemaVersion
	record.Payload.ContentType = metadata.ContentType
	if metadata.EnqueuedAt != nil {
		enqueuedAt := *metadata.EnqueuedAt
		record.EnqueuedAt = &enqueuedAt
	}
	record.RetryPolicy = metadata.RetryPolicy
	record.HandlerType = metadata.HandlerType
	record.JobType = metadata.JobType
	record.Tags = cloneMetadataTags(metadata.Tags)
	record.TraceID = metadata.TraceID
	record.TenantID = metadata.TenantID
	record.ProducerVersion = metadata.ProducerVersion
}

func cloneMetadataTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	clone := make(map[string]string, len(tags))
	for key, value := range tags {
		clone[key] = value
	}

	return clone
}
