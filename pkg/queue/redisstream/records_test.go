package redisdb

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordReaderListsAndInspectsRedisDeadLetters(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-1"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithDeadLetter("jobs-dead", 5),
		WithFailureStream("jobs-failures"),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "1.4.0", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	queued := job.NewMessage(rawMessage("permanent"), job.AllowOption{Metadata: &job.Metadata{
		OriginalID: "job-123", PayloadSchemaVersion: "order.v2",
		ContentType: "application/json", EnqueuedAt: &enqueuedAt,
		RetryPolicy: "critical-v1", HandlerType: "CreateOrder",
		JobType: "order.created", Tags: map[string]string{"region": "eu"},
		TraceID: "trace-123", TenantID: "tenant-123", ProducerVersion: "1.2.3",
	}})
	require.NoError(t, worker.Queue(&queued))
	delivery, err := worker.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).NackFailure(management.NewFailure(
		management.ClassificationPermanent, "invalid_order", errors.New("invalid order"),
	)))

	request := management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	}
	failures, err := worker.ListFailures(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, failures.Items, 1)
	deadLetters, err := worker.ListDeadLetters(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, deadLetters.Items, 1)
	record := deadLetters.Items[0]
	assert.Equal(t, management.CurrentEnvelopeVersion, record.EnvelopeVersion)
	assert.Equal(t, management.ClassificationPermanent, record.Classification)
	assert.Equal(t, "invalid_order", record.FailureCode)
	assert.Equal(t, "jobs", record.Stream)
	assert.Equal(t, "workers", record.ConsumerGroup)
	assert.Equal(t, "job-123", record.OriginalID)
	assert.NotEqual(t, record.OriginalID, record.SourceRecordID)
	assert.Equal(t, "order.v2", record.PayloadSchemaVersion)
	assert.Equal(t, "application/json", record.Payload.ContentType)
	assert.Equal(t, enqueuedAt, *record.EnqueuedAt)
	assert.Equal(t, "critical-v1", record.RetryPolicy)
	assert.Equal(t, "CreateOrder", record.HandlerType)
	assert.Equal(t, "order.created", record.JobType)
	assert.Equal(t, map[string]string{"region": "eu"}, record.Tags)
	assert.Equal(t, "trace-123", record.TraceID)
	assert.Equal(t, "tenant-123", record.TenantID)
	assert.Equal(t, "1.2.3", record.ProducerVersion)
	assert.Equal(t, "1.4.0", record.WorkerVersion)
	require.NotNil(t, record.LastDeliveryAt)
	assert.Equal(t, record.OccurredAt, *record.LastDeliveryAt)
	assert.Equal(t, management.PayloadHidden, record.Payload.Visibility)
	assert.Empty(t, record.Payload.Data)

	revealed, err := worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: record.ID,
		Visibility: management.PayloadRevealed,
	})
	require.NoError(t, err)
	assert.Equal(t, queued.Bytes(), revealed.Payload.Data)
}

func TestRecordReaderBoundsPaginationFiltersAndMalformedData(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	for index, code := range []string{"code-a", "code-b", "code-c"} {
		require.NoError(t, worker.appendRecord(
			context.Background(), "failures", string(rune('1'+index))+"000-0",
			[]byte(code), int64(index+1), streamqueue.FailureMetadata{
				Classification: management.ClassificationRetryable, Code: code,
			},
		))
	}
	request := management.PageRequest{
		Limit: 2, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	}
	first, err := worker.ListFailures(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	assert.NotEmpty(t, first.NextCursor)
	request.Cursor = first.NextCursor
	second, err := worker.ListFailures(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	assert.Empty(t, second.NextCursor)

	searched, err := worker.ListFailures(context.Background(), management.PageRequest{
		Limit: 2, Search: "code-c", Sort: management.SortOccurredAt,
		Direction: management.SortDescending,
	})
	require.NoError(t, err)
	require.Len(t, searched.Items, 1)
	assert.Equal(t, "code-c", searched.Items[0].FailureCode)

	_, err = worker.ListFailures(context.Background(), management.PageRequest{
		Limit: 1, Sort: management.SortAttempts, Direction: management.SortAscending,
	})
	assert.ErrorIs(t, err, management.ErrInvalidFilter)
	_, err = worker.ListFailures(context.Background(), management.PageRequest{
		Limit: 1, Cursor: "not-canonical", Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	assert.ErrorIs(t, err, management.ErrMalformedCursor)
	_, err = worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "999-0", Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, management.ErrRecordNotFound)

	malformedID, err := worker.rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: "dead", Values: map[string]any{"unexpected": "value"},
	}).Result()
	require.NoError(t, err)
	_, err = worker.ListDeadLetters(context.Background(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	})
	assert.Error(t, err)
	_, err = worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: malformedID,
		Visibility: management.PayloadHidden,
	})
	assert.Error(t, err)
	legacy := management.JobRecord{}
	worker.enrichManagementRecord(&legacy)
	assert.Nil(t, legacy.LastDeliveryAt)
}

func TestRedisRecordReaderClassifiesUnavailableBackendSafely(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
		WithRequestTimeout(100*time.Millisecond),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	address := server.Addr()
	server.Close()

	_, err = worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	})
	assert.ErrorIs(t, err, management.ErrManagementUnavailable)
	assert.NotContains(t, err.Error(), address)

	_, err = worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: "1-0",
	})
	assert.ErrorIs(t, err, management.ErrManagementUnavailable)
	assert.NotContains(t, err.Error(), address)
}

func TestRedisRecordReadErrorPreservesCallerCancellation(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := redisRecordReadError("read records", cause)
		assert.ErrorIs(t, err, cause)
		assert.NotErrorIs(t, err, management.ErrManagementUnavailable)
	}
}

func TestRecordRetentionIsIndependentAndExplicit(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		options  []Option
		expected int64
	}{
		"disabled": {
			options: []Option{WithMaxLength(1)}, expected: 3,
		},
		"configured": {
			options: []Option{WithMaxLength(10), WithRecordRetention(2)}, expected: 2,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := miniredis.RunT(t)
			workerOptions := append([]Option{
				WithAddr(server.Addr()), WithStreamName("jobs"),
				WithFailureStream("failures"), WithDeadLetter("dead", 5),
			}, test.options...)
			worker, err := NewWorkerE(workerOptions...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = worker.Shutdown() })
			for index := range 3 {
				require.NoError(t, worker.appendRecord(
					context.Background(), "dead", fmt.Sprintf("%d-0", index+1),
					[]byte("payload"), 1, streamqueue.FailureMetadata{
						Classification: management.ClassificationPermanent, Code: "invalid",
					},
				))
			}
			length, err := worker.rdb.XLen(context.Background(), "dead").Result()
			require.NoError(t, err)
			assert.Equal(t, test.expected, length)
		})
	}
}

func TestRedisRecordReaderFailsClosedAtUntrustedBoundaries(t *testing.T) {
	t.Parallel()

	request := management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	}
	_, err := (&Worker{}).ListFailures(t.Context(), request)
	assert.ErrorIs(t, err, management.ErrUnsupportedCapability)
	_, err = (&Worker{}).ListFailures(t.Context(), management.PageRequest{})
	assert.Error(t, err)
	_, err = (&Worker{}).Inspect(t.Context(), management.InspectRequest{})
	assert.Error(t, err)
	_, err = (&Worker{}).Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "1-0", Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, management.ErrUnsupportedCapability)

	worker, _ := newFaultControlWorker(t)
	addRedisFault(t, worker, "xrange", 1)
	_, err = worker.ListFailures(t.Context(), request)
	assert.Error(t, err)

	worker, recordID := newFaultControlWorker(t)
	addRedisFault(t, worker, "xrange", 1)
	_, err = worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: recordID,
		Visibility: management.PayloadHidden,
	})
	assert.Error(t, err)

	worker, _ = newFaultControlWorker(t)
	page, err := worker.readRecordPage(
		t.Context(), "dead", recordID, 1, management.SortDescending,
	)
	require.NoError(t, err)
	assert.Empty(t, page)
}

func TestRedisManagementRecordRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()

	valid := redis.XMessage{ID: "1000-0", Values: map[string]any{
		streamBodyField: []byte("body"), originalIDField: "500-0",
		deliveryAttemptsField: "1", envelopeVersionField: "1",
		classificationField: string(management.ClassificationPermanent),
		failureCodeField:    "invalid", sourceStreamField: "jobs",
		consumerGroupField: "workers",
	}}
	record, err := redisManagementRecord(
		valid, management.RecordDeadLetter, management.PayloadHidden,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(4), record.Payload.Size)

	for name, mutate := range map[string]func(*redis.XMessage){
		"missing field": func(message *redis.XMessage) {
			delete(message.Values, streamBodyField)
		},
		"invalid id": func(message *redis.XMessage) { message.ID = "invalid" },
		"invalid lineage": func(message *redis.XMessage) {
			message.Values[replayGenerationField] = "invalid"
		},
		"invalid visibility": func(message *redis.XMessage) {},
	} {
		t.Run(name, func(t *testing.T) {
			message := valid
			message.Values = make(map[string]any, len(valid.Values))
			for key, value := range valid.Values {
				message.Values[key] = value
			}
			mutate(&message)
			visibility := management.PayloadHidden
			if name == "invalid visibility" {
				visibility = management.PayloadVisibility("public")
			}
			_, recordErr := redisManagementRecord(
				message, management.RecordDeadLetter, visibility,
			)
			assert.Error(t, recordErr)
		})
	}

	_, ok := redisRecordString(1)
	assert.False(t, ok)
	_, err = redisRecordTime("invalid")
	assert.Error(t, err)
	_, err = redisRecordTime("invalid-0")
	assert.Error(t, err)
	invalidID := base64.RawURLEncoding.EncodeToString([]byte("invalid"))
	_, err = decodeRedisRecordCursor(invalidID)
	assert.ErrorIs(t, err, management.ErrMalformedCursor)
}
