package nsq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	nsqgo "github.com/nsqio/go-nsq"
	"github.com/stretchr/testify/require"
)

type messageDelegate struct {
	finished int
	requeued int
}

func (d *messageDelegate) OnFinish(*nsqgo.Message)                       { d.finished++ }
func (d *messageDelegate) OnRequeue(*nsqgo.Message, time.Duration, bool) { d.requeued++ }
func (d *messageDelegate) OnTouch(*nsqgo.Message)                        {}

func TestRequestDefersNSQSettlement(t *testing.T) {
	delegate := &messageDelegate{}
	queued := job.NewTask(nil)
	message := nsqgo.NewMessage(nsqgo.MessageID{}, queued.Bytes())
	message.Delegate = delegate
	tasks := make(chan *nsqgo.Message, 1)
	tasks <- message
	worker := &Worker{opts: newOptions(), tasks: tasks}
	worker.startOnce.Do(func() {})

	task, err := worker.Request()
	require.NoError(t, err)
	require.Zero(t, delegate.finished)
	require.Zero(t, delegate.requeued)

	delivery := task.(*job.Message)
	require.NoError(t, delivery.Ack())
	require.Equal(t, 1, delegate.finished)

	requeueDelegate := &messageDelegate{}
	requeueMessage := nsqgo.NewMessage(nsqgo.MessageID{}, queued.Bytes())
	requeueMessage.Delegate = requeueDelegate
	requeueTasks := make(chan *nsqgo.Message, 1)
	requeueTasks <- requeueMessage
	requeueWorker := &Worker{opts: newOptions(), tasks: requeueTasks}
	requeueWorker.startOnce.Do(func() {})
	requeueTask, err := requeueWorker.Request()
	require.NoError(t, err)

	require.NoError(t, requeueTask.(*job.Message).Nack())
	require.Equal(t, 1, requeueDelegate.requeued)
}

func TestRequestFinishesMalformedNSQMessages(t *testing.T) {
	delegate := &messageDelegate{}
	message := nsqgo.NewMessage(nsqgo.MessageID{}, []byte("not-json"))
	message.Delegate = delegate
	tasks := make(chan *nsqgo.Message, 1)
	tasks <- message
	var publishedTopic string
	var published []byte
	worker := &Worker{
		opts: newOptions(), tasks: tasks,
		publish: func(topic string, body []byte) error {
			publishedTopic = topic
			published = append([]byte(nil), body...)
			return nil
		},
	}
	worker.startOnce.Do(func() {})

	task, err := worker.Request()
	require.Nil(t, task)
	require.ErrorContains(t, err, "decode NSQ message")
	require.Equal(t, 1, delegate.finished)
	require.Zero(t, delegate.requeued)
	require.Equal(t, "gorush-dead", publishedTopic)
	record, err := decodeNSQDeadLetter(published)
	require.NoError(t, err)
	require.Equal(t, management.ClassificationMalformed, record.Classification)
	require.Equal(t, "malformed_delivery", record.FailureCode)
	hidden, err := DecodeDeadLetter(published, management.PayloadHidden)
	require.NoError(t, err)
	require.Empty(t, hidden.Payload.Data)
	require.Equal(t, int64(len(message.Body)), hidden.Payload.Size)
	revealed, err := DecodeDeadLetter(published, management.PayloadRevealed)
	require.NoError(t, err)
	require.Equal(t, message.Body, revealed.Payload.Data)
}

func TestRequestDeadLettersClassifiedAndExhaustedNSQFailures(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		failure        error
		attempts       uint16
		finished       int
		requeued       int
		classification management.Classification
		code           string
	}{
		"retryable": {failure: errors.New("temporary"), attempts: 1, requeued: 1},
		"canceled":  {failure: context.Canceled, attempts: 5, requeued: 1},
		"exhausted": {
			failure: management.NewFailure(
				management.ClassificationRetryable, "handler_failed", errors.New("temporary"),
			), attempts: 5, finished: 1,
			classification: management.ClassificationRetryable, code: "attempts_exhausted",
		},
		"permanent": {
			failure: management.NewFailure(
				management.ClassificationPermanent, "invalid_order", errors.New("invalid"),
			), attempts: 1, finished: 1,
			classification: management.ClassificationPermanent, code: "invalid_order",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			delegate := &messageDelegate{}
			queued := job.NewTask(nil)
			message := nsqgo.NewMessage(nsqgo.MessageID{1}, queued.Bytes())
			message.Attempts = test.attempts
			message.Delegate = delegate
			tasks := make(chan *nsqgo.Message, 1)
			tasks <- message
			var published []byte
			worker := &Worker{
				opts: newOptions(), tasks: tasks,
				publish: func(topic string, body []byte) error {
					require.Equal(t, "gorush-dead", topic)
					published = append([]byte(nil), body...)
					return nil
				},
			}
			worker.startOnce.Do(func() {})
			task, err := worker.Request()
			require.NoError(t, err)
			require.NoError(t, task.(*job.Message).NackFailure(test.failure))
			require.Equal(t, test.finished, delegate.finished)
			require.Equal(t, test.requeued, delegate.requeued)
			if test.finished == 0 {
				require.Empty(t, published)
				return
			}
			record, err := decodeNSQDeadLetter(published)
			require.NoError(t, err)
			require.Equal(t, test.classification, record.Classification)
			require.Equal(t, test.code, record.FailureCode)
			require.Equal(t, test.attempts, record.Attempts)
			require.NotEmpty(t, record.SourceID)
		})
	}
}

func TestNSQDeadLetterPublishFailureRequeuesSource(t *testing.T) {
	t.Parallel()

	delegate := &messageDelegate{}
	message := nsqgo.NewMessage(nsqgo.MessageID{1}, []byte("payload"))
	message.Attempts = 5
	message.Delegate = delegate
	worker := &Worker{
		opts:    newOptions(),
		publish: func(string, []byte) error { return errors.New("unavailable") },
	}
	err := worker.settleNSQFailure(message, errors.New("exhausted"))
	require.Error(t, err)
	resolution := management.ResolveFailure(err)
	require.Equal(t, management.ClassificationInfrastructure, resolution.Classification)
	require.Equal(t, management.FailureCodeDeadLetterDestinationUnavailable, resolution.Code)
	require.Zero(t, delegate.finished)
	require.Equal(t, 1, delegate.requeued)
}

func TestNSQDeadLetterBoundariesRejectMalformedRecords(t *testing.T) {
	t.Parallel()

	worker := &Worker{opts: newOptions()}
	message := nsqgo.NewMessage(nsqgo.MessageID{1}, []byte("payload"))
	message.Attempts = 5
	valid, err := worker.encodeNSQDeadLetter(
		message, management.ClassificationRetryable, "attempts_exhausted", 5,
	)
	require.NoError(t, err)

	for name, encoded := range map[string][]byte{
		"empty":        nil,
		"oversized":    bytes.Repeat([]byte("x"), maxNSQDeadLetterEnvelopeBytes+1),
		"trailing":     append(append([]byte(nil), valid...), []byte("{}")...),
		"invalid json": []byte("{"),
	} {
		t.Run(name, func(t *testing.T) {
			_, decodeErr := decodeNSQDeadLetter(encoded)
			require.Error(t, decodeErr)
		})
	}

	var invalid map[string]any
	require.NoError(t, json.Unmarshal(valid, &invalid))
	invalid["backend"] = "other"
	encoded, err := json.Marshal(invalid)
	require.NoError(t, err)
	_, err = decodeNSQDeadLetter(encoded)
	require.Error(t, err)

	invalid["backend"] = "nsq"
	invalid["metadata"] = map[string]any{"original_id": " "}
	encoded, err = json.Marshal(invalid)
	require.NoError(t, err)
	_, err = decodeNSQDeadLetter(encoded)
	require.Error(t, err)

	_, err = DecodeDeadLetter([]byte("{"), management.PayloadHidden)
	require.Error(t, err)
	_, err = DecodeDeadLetter(valid, management.PayloadVisibility("public"))
	require.Error(t, err)

	future := nsqgo.NewMessage(nsqgo.MessageID{1}, []byte("payload"))
	future.Timestamp = time.Now().Add(time.Hour).UnixNano()
	delegate := &messageDelegate{}
	future.Delegate = delegate
	err = worker.settleNSQFailure(future, management.NewFailure(
		management.ClassificationPermanent, "invalid", errors.New("invalid"),
	))
	require.Error(t, err)
	require.Equal(t, 1, delegate.requeued)
}

func TestNSQDeadLetterPreservesSuppliedJobMetadata(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	queued := job.NewTask(nil, job.AllowOption{Metadata: &job.Metadata{
		OriginalID: "job-123", PayloadSchemaVersion: "order.v2",
		ContentType: "application/json", EnqueuedAt: &enqueuedAt,
		RetryPolicy: "critical-v1", HandlerType: "CreateOrder",
		JobType: "order.created", Tags: map[string]string{"region": "eu"},
		TraceID: "trace-123", TenantID: "tenant-123", ProducerVersion: "1.2.3",
	}})
	message := nsqgo.NewMessage(nsqgo.MessageID{1}, queued.Bytes())
	message.Timestamp = enqueuedAt.Add(time.Second).UnixNano()
	worker := &Worker{opts: newOptions()}
	encoded, err := worker.encodeNSQDeadLetter(
		message, management.ClassificationPermanent, "invalid_order", 1,
	)
	require.NoError(t, err)
	record, err := DecodeDeadLetter(encoded, management.PayloadHidden)
	require.NoError(t, err)

	require.Equal(t, "job-123", record.OriginalID)
	require.NotEqual(t, record.OriginalID, record.SourceRecordID)
	require.Equal(t, "order.v2", record.PayloadSchemaVersion)
	require.Equal(t, "application/json", record.Payload.ContentType)
	require.Equal(t, enqueuedAt, *record.EnqueuedAt)
	require.Equal(t, "critical-v1", record.RetryPolicy)
	require.Equal(t, "CreateOrder", record.HandlerType)
	require.Equal(t, "order.created", record.JobType)
	require.Equal(t, map[string]string{"region": "eu"}, record.Tags)
	require.Equal(t, "trace-123", record.TraceID)
	require.Equal(t, "tenant-123", record.TenantID)
	require.Equal(t, "1.2.3", record.ProducerVersion)
}
