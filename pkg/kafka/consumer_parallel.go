package kafka

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type consumerPartitionResult struct {
	lastSuccessful *kgo.Record
	processed      int
	successful     int
	err            error
}

func runConsumerPartitionWorkers(
	batches []consumerPartitionBatch,
	maximum int,
	process func(consumerPartitionBatch) consumerPartitionResult,
) []consumerPartitionResult {
	results := make([]consumerPartitionResult, len(batches))
	workers := min(maximum, len(batches))
	if workers == 1 {
		for index, batch := range batches {
			results[index] = process(batch)
		}

		return results
	}

	// This invocation owns and closes jobs. Workers only receive from it and
	// are joined before any result or borrowed record can escape the poll.
	jobs := make(chan int)
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				results[index] = process(batches[index])
			}
		}()
	}
	for index := range batches {
		jobs <- index
	}
	close(jobs)
	waitGroup.Wait()

	return results
}

func (consumer *Consumer) processRecordPartition(
	ctx context.Context,
	token consumerAssignmentToken,
	handler Handler,
	batch consumerPartitionBatch,
) consumerPartitionResult {
	var result consumerPartitionResult
	for _, record := range batch.records {
		var startedAt time.Time
		if consumer.observers.enabled() {
			startedAt = time.Now()
		}
		if cause := context.Cause(ctx); cause != nil {
			result.err = cause
			consumer.observeConsumedRecord(ctx, startedAt, record, result.err)

			return result
		}
		if !consumer.assignment.owns(token, batch.partition) {
			result.err = ErrConsumerOwnershipLost
			consumer.observeConsumedRecord(ctx, startedAt, record, result.err)

			return result
		}
		message, err := consumedMessageWithinLimits(record, consumer.limits)
		if err == nil {
			handlerCtx, finish, admitted := consumer.rebalance.handlerContext(
				ctx,
				consumer.handlerTimeout,
			)
			if !admitted {
				return result
			}
			err = callHandler(handlerCtx, handler, message)
			if cause := finish(); cause != nil {
				err = errors.Join(err, cause)
			}
		}
		if err != nil {
			result.err = err
			consumer.observeConsumedRecord(ctx, startedAt, record, err)

			return result
		}
		result.processed++
		consumer.observeConsumedRecord(ctx, startedAt, record, nil)
		if !consumer.assignment.owns(token, batch.partition) {
			result.err = ErrConsumerOwnershipLost
			result.lastSuccessful = nil
			result.successful = 0

			return result
		}
		result.lastSuccessful = record
		result.successful++
	}

	return result
}

func (consumer *Consumer) processBatchPartition(
	ctx context.Context,
	token consumerAssignmentToken,
	handler BatchHandler,
	batch consumerPartitionBatch,
) consumerPartitionResult {
	var startedAt time.Time
	if consumer.observers.enabled() {
		startedAt = time.Now()
	}
	if cause := context.Cause(ctx); cause != nil {
		consumer.observeConsumedBatch(ctx, startedAt, batch, cause)

		return consumerPartitionResult{err: cause}
	}
	if !consumer.assignment.owns(token, batch.partition) {
		consumer.observeConsumedBatch(
			ctx,
			startedAt,
			batch,
			ErrConsumerOwnershipLost,
		)

		return consumerPartitionResult{err: ErrConsumerOwnershipLost}
	}

	consumed, err := consumer.consumedBatch(batch)
	if err == nil {
		handlerCtx, finish, admitted := consumer.rebalance.handlerContext(
			ctx,
			consumer.handlerTimeout,
		)
		if !admitted {
			consumer.observeConsumedBatch(
				ctx,
				startedAt,
				batch,
				ErrConsumerRebalance,
			)

			return consumerPartitionResult{}
		}
		err = callBatchHandler(handlerCtx, handler, consumed)
		if cause := finish(); cause != nil {
			err = errors.Join(err, cause)
		}
	}
	if err != nil {
		consumer.observeConsumedBatch(ctx, startedAt, batch, err)

		return consumerPartitionResult{err: err}
	}

	result := consumerPartitionResult{processed: len(batch.records)}
	consumer.observeConsumedBatch(ctx, startedAt, batch, nil)
	if !consumer.assignment.owns(token, batch.partition) {
		result.err = ErrConsumerOwnershipLost

		return result
	}
	result.lastSuccessful = batch.records[len(batch.records)-1]
	result.successful = len(batch.records)

	return result
}

func (consumer *Consumer) observeConsumedBatch(
	ctx context.Context,
	startedAt time.Time,
	batch consumerPartitionBatch,
	err error,
) {
	if !consumer.observers.enabled() {
		return
	}
	observation := Observation{
		Kind:        ObservationConsumeBatch,
		StartedAt:   startedAt,
		Duration:    time.Since(startedAt),
		ClientID:    consumer.clientID,
		GroupID:     consumer.groupID,
		RecordCount: len(batch.records),
		Succeeded:   err == nil,
	}
	var bytes int64
	valid := len(batch.records) != 0
	for _, record := range batch.records {
		if _, validationErr := consumedMessageWithinLimits(
			record,
			consumer.limits,
		); validationErr != nil {
			valid = false
		} else {
			bytes = bytes + consumedRecordSize(record)
		}
	}
	if valid {
		last := batch.records[len(batch.records)-1]
		observation.Topic = strings.Clone(batch.partition.Topic)
		observation.Partition = batch.partition.Partition
		observation.PartitionKnown = true
		observation.PartitionCount = 1
		observation.Offset = last.Offset
		observation.OffsetKnown = true
		observation.RecordBytes = bytes
	}
	if err == nil {
		observation.ProcessedCount = len(batch.records)
	} else {
		observation.Category = classifyConsumerObservationError(err)
	}
	consumer.dispatchObservation(ctx, observation)
}

func (consumer *Consumer) settlePartitionResults(
	ctx context.Context,
	token consumerAssignmentToken,
	result PollResult,
	partitionResults []consumerPartitionResult,
) (PollResult, error) {
	committable := make([]*kgo.Record, 0, len(partitionResults))
	committed := 0
	var handlerErr error
	for _, partitionResult := range partitionResults {
		result.Processed += partitionResult.processed
		if handlerErr == nil {
			if partitionResult.err != nil {
				handlerErr = partitionResult.err
			}
		}
		if partitionResult.lastSuccessful != nil {
			committable = append(
				committable,
				partitionResult.lastSuccessful,
			)
			committed += partitionResult.successful
		}
	}

	if ownershipErr := consumer.assignment.validate(token); ownershipErr != nil {
		return result, errors.Join(handlerErr, ownershipErr)
	}
	if len(committable) == 0 {
		return result, handlerErr
	}
	commitCtx, cancel := context.WithTimeout(ctx, consumer.commitTimeout)
	var startedAt time.Time
	if consumer.observers.enabled() {
		startedAt = time.Now()
	}
	err := consumer.client.CommitRecords(commitCtx, committable...)
	cancel()
	err = consumer.groupError(newConsumerError(ConsumerOperationCommit, err))
	consumer.observeConsumerCommit(
		ctx,
		startedAt,
		committable,
		committed,
		err,
	)
	if err != nil {
		if handlerErr == nil {
			return result, err
		}

		return result, errors.Join(handlerErr, err)
	}
	result.Committed = committed

	return result, handlerErr
}

func (consumer *Consumer) observeConsumedRecord(
	ctx context.Context,
	startedAt time.Time,
	record *kgo.Record,
	err error,
) {
	if !consumer.observers.enabled() {
		return
	}
	observation := Observation{
		Kind:        ObservationConsumeRecord,
		StartedAt:   startedAt,
		Duration:    time.Since(startedAt),
		ClientID:    consumer.clientID,
		GroupID:     consumer.groupID,
		RecordCount: 1,
		Succeeded:   err == nil,
	}
	if _, validationErr := consumedMessageWithinLimits(record, consumer.limits); validationErr == nil {
		observation.Topic = strings.Clone(record.Topic)
		observation.Partition = record.Partition
		observation.PartitionKnown = true
		observation.PartitionCount = 1
		observation.Offset = record.Offset
		observation.OffsetKnown = true
		observation.RecordBytes = consumedRecordSize(record)
	}
	if err == nil {
		observation.ProcessedCount = 1
	} else {
		observation.Category = classifyConsumerObservationError(err)
	}
	consumer.dispatchObservation(ctx, observation)
}

func (consumer *Consumer) observeConsumerCommit(
	ctx context.Context,
	startedAt time.Time,
	records []*kgo.Record,
	committed int,
	err error,
) {
	if !consumer.observers.enabled() {
		return
	}
	topic := ""
	if len(records) != 0 {
		topic = records[0].Topic
		for _, record := range records[1:] {
			if record.Topic != topic {
				topic = ""
			}
		}
	}
	observation := Observation{
		Kind:           ObservationConsumeCommit,
		StartedAt:      startedAt,
		Duration:       time.Since(startedAt),
		ClientID:       consumer.clientID,
		GroupID:        consumer.groupID,
		Topic:          strings.Clone(topic),
		RecordCount:    committed,
		PartitionCount: len(records),
		ProcessedCount: committed,
		Succeeded:      err == nil,
	}
	if err == nil {
		observation.CommittedCount = committed
	} else {
		observation.Category = classifyConsumerObservationError(err)
	}
	consumer.dispatchObservation(ctx, observation)
}

func consumedRecordSize(record *kgo.Record) int64 {
	size := int64(len(record.Topic))
	size = size + int64(len(record.Key))
	size = size + int64(len(record.Value))
	size = size + 32
	for _, header := range record.Headers {
		size = size + int64(len(header.Key))
		size = size + int64(len(header.Value))
		size = size + 8
	}

	return size
}

func classifyConsumerObservationError(err error) (category ErrorCategory) {
	defer func() {
		if recover() != nil {
			category = ErrorPermanent
		}
	}()

	var categorized interface{ Category() ErrorCategory }
	if errors.As(err, &categorized) {
		category = categorized.Category()
		if validErrorCategory(category) {
			return category
		}
	}

	return classifyError(err)
}

func (consumer *Consumer) dispatchObservation(
	ctx context.Context,
	observation Observation,
) {
	consumer.beginObservation()
	defer consumer.finishObservation()
	consumer.observers.observe(ctx, observation)
}

func (consumer *Consumer) beginObservation() {
	consumer.lifecycleMu.Lock()
	consumer.observerCallbacks++
	consumer.lifecycleMu.Unlock()
}

func (consumer *Consumer) finishObservation() {
	consumer.lifecycleMu.Lock()
	consumer.observerCallbacks--
	consumer.lifecycleMu.Unlock()
}
