package kafka

import (
	"context"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// ConsumedBatch contains one non-empty bounded set of records from a single
// topic partition. Records are ordered by offset. The slice is owned for the
// handler call, but record bytes remain borrowed unless Retain is used.
type ConsumedBatch struct {
	Topic     string
	Partition int32
	Records   []ConsumedRecord
}

// Retain returns a deep copy whose slice and record bytes the caller owns.
func (batch ConsumedBatch) Retain() ConsumedBatch {
	if len(batch.Records) == 0 {
		return ConsumedBatch{Topic: batch.Topic, Partition: batch.Partition}
	}
	records := make([]ConsumedRecord, len(batch.Records))
	for index := range batch.Records {
		records[index] = batch.Records[index].Retain()
	}

	return ConsumedBatch{
		Topic: batch.Topic, Partition: batch.Partition, Records: records,
	}
}

// BatchHandler durably processes one partition batch. A nil result settles the
// entire batch; any error settles none of it. Implementations must be
// concurrency-safe when the consumer permits more than one concurrent handler.
type BatchHandler interface {
	HandleBatch(context.Context, ConsumedBatch) error
}

// BatchHandlerFunc adapts a function to BatchHandler.
type BatchHandlerFunc func(context.Context, ConsumedBatch) error

// HandleBatch invokes handler.
func (handler BatchHandlerFunc) HandleBatch(
	ctx context.Context,
	batch ConsumedBatch,
) error {
	return handler(ctx, batch)
}

// RunBatchOnce polls at most the configured record limit and invokes handler
// once per non-empty partition batch, with bounded concurrency across
// partitions. A successful batch commits its last record; a failed batch
// commits no record from that partition. Independent successful partition
// batches remain committable. A nil context returns ErrContextRequired.
// Static-membership fencing returns ErrConsumerFatal and
// ErrConsumerInstanceFenced, then permanently rejects later runner calls.
func (consumer *Consumer) RunBatchOnce(
	ctx context.Context,
	handler BatchHandler,
) (PollResult, error) {
	if ctx == nil {
		return PollResult{}, ErrContextRequired
	}
	if isObserverContext(ctx) {
		return PollResult{}, ErrObserverReentry
	}
	if handler == nil {
		return PollResult{}, ErrBatchHandlerRequired
	}
	if err := consumer.beginRun(); err != nil {
		return PollResult{}, err
	}
	defer consumer.endRun()

	return consumer.runBatchOnce(ctx, handler)
}

type consumerPartitionBatch struct {
	partition TopicPartition
	records   []*kgo.Record
}

func (consumer *Consumer) runBatchOnce(
	ctx context.Context,
	handler BatchHandler,
) (result PollResult, resultErr error) {
	var startedAt time.Time
	if consumer.observers.enabled() {
		startedAt = time.Now()
	}
	consumer.rebalance.beginPoll()
	defer consumer.rebalance.endPoll()

	pollCtx, finishPoll, admitted := consumer.beginPoll(ctx)
	if !admitted {
		return PollResult{}, nil
	}
	fetches := consumer.client.PollRecords(pollCtx, consumer.maxPollRecords)
	drainInterrupted := errors.Is(
		context.Cause(pollCtx),
		errConsumerDrainRequested,
	)
	finishPoll()
	defer consumer.client.AllowRebalance()

	records := fetches.Records()
	defer recycleFetchedRecords(records)
	if consumer.observers.enabled() {
		defer func() {
			consumer.observeConsumerPoll(
				ctx,
				startedAt,
				records,
				result,
				resultErr,
			)
		}()
	}
	defer func() {
		resultErr = consumer.groupError(resultErr)
	}()
	result = PollResult{Polled: len(records)}
	if len(records) > consumer.maxPollRecords {
		return result, errors.Join(
			ErrTooManyFetchedRecords,
			newConsumerError(ConsumerOperationPoll, fetches.Err()),
		)
	}
	if err := fetches.Err(); err != nil {
		if drainInterrupted && errors.Is(err, context.Canceled) {
			return PollResult{}, nil
		}
		return PollResult{}, consumer.groupError(
			newConsumerError(ConsumerOperationPoll, err),
		)
	}
	token, err := consumer.assignment.token()
	if err != nil {
		return result, err
	}
	if len(records) == 0 {
		return PollResult{}, nil
	}

	batches := partitionBatches(records)
	partitionResults := runConsumerPartitionWorkers(
		batches,
		consumer.maxConcurrentHandlers,
		func(batch consumerPartitionBatch) consumerPartitionResult {
			return consumer.processBatchPartition(ctx, token, handler, batch)
		},
	)

	return consumer.settlePartitionResults(ctx, token, result, partitionResults)
}

func partitionBatches(records []*kgo.Record) []consumerPartitionBatch {
	indexes := make(map[TopicPartition]int)
	batches := make([]consumerPartitionBatch, 0)
	for _, record := range records {
		partition := TopicPartition{Topic: record.Topic, Partition: record.Partition}
		index, exists := indexes[partition]
		if !exists {
			index = len(batches)
			indexes[partition] = index
			batches = append(batches, consumerPartitionBatch{partition: partition})
		}
		batches[index].records = append(batches[index].records, record)
	}

	return batches
}

func (consumer *Consumer) consumedBatch(
	batch consumerPartitionBatch,
) (ConsumedBatch, error) {
	records := make([]ConsumedRecord, len(batch.records))
	for index, record := range batch.records {
		message, err := consumedMessageWithinLimits(record, consumer.limits)
		if err != nil {
			return ConsumedBatch{}, err
		}
		records[index] = message
	}

	return ConsumedBatch{
		Topic: batch.partition.Topic, Partition: batch.partition.Partition,
		Records: records,
	}, nil
}

func callBatchHandler(
	ctx context.Context,
	handler BatchHandler,
	batch ConsumedBatch,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrHandlerPanic
		}
	}()

	return handler.HandleBatch(ctx, batch)
}
