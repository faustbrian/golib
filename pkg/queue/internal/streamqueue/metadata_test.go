package streamqueue

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageMetadataAppliesValidatedAllowlistWithoutAliasing(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	message := job.NewTask(nil, job.AllowOption{Metadata: &job.Metadata{
		OriginalID: "job-1", PayloadSchemaVersion: "order.v2",
		ContentType: "application/json", EnqueuedAt: &enqueuedAt,
		RetryPolicy: "critical-v1", HandlerType: "CreateOrder",
		JobType: "order.created", Tags: map[string]string{"region": "eu"},
		TraceID: "trace-1", TenantID: "tenant-1", ProducerVersion: "1.2.3",
	}})
	metadata := MessageMetadata(message.Bytes())
	require.NotNil(t, metadata)
	record := management.JobRecord{OriginalID: "source-1"}
	ApplyMessageMetadata(&record, metadata)
	metadata.Tags["region"] = "changed"

	assert.Equal(t, "job-1", record.OriginalID)
	assert.Equal(t, "order.v2", record.PayloadSchemaVersion)
	assert.Equal(t, "application/json", record.Payload.ContentType)
	assert.Equal(t, enqueuedAt, *record.EnqueuedAt)
	assert.Equal(t, "critical-v1", record.RetryPolicy)
	assert.Equal(t, "CreateOrder", record.HandlerType)
	assert.Equal(t, "order.created", record.JobType)
	assert.Equal(t, map[string]string{"region": "eu"}, record.Tags)
	assert.Equal(t, "trace-1", record.TraceID)
	assert.Equal(t, "tenant-1", record.TenantID)
	assert.Equal(t, "1.2.3", record.ProducerVersion)
}

func TestMessageMetadataLeavesUnknownAndMalformedValuesAlone(t *testing.T) {
	t.Parallel()

	assert.Nil(t, MessageMetadata([]byte("not-an-envelope")))
	record := management.JobRecord{OriginalID: "source-1"}
	ApplyMessageMetadata(&record, nil)
	assert.Equal(t, "source-1", record.OriginalID)

	message := job.NewTask(nil, job.AllowOption{Metadata: &job.Metadata{
		Tags: map[string]string{},
	}})
	metadata := MessageMetadata(message.Bytes())
	require.NotNil(t, metadata)
	ApplyMessageMetadata(&record, metadata)
	assert.Equal(t, "source-1", record.OriginalID)
	assert.Empty(t, record.Tags)
}
