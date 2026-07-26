package kafka

import (
	"context"
	"errors"
	"sync"

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
		if cause := context.Cause(ctx); cause != nil {
			result.err = cause

			return result
		}
		if !consumer.assignment.owns(token, batch.partition) {
			result.err = ErrConsumerOwnershipLost

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

			return result
		}
		result.processed++
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
	if cause := context.Cause(ctx); cause != nil {
		return consumerPartitionResult{err: cause}
	}
	if !consumer.assignment.owns(token, batch.partition) {
		return consumerPartitionResult{err: ErrConsumerOwnershipLost}
	}

	consumed, err := consumer.consumedBatch(batch)
	if err == nil {
		handlerCtx, finish, admitted := consumer.rebalance.handlerContext(
			ctx,
			consumer.handlerTimeout,
		)
		if !admitted {
			return consumerPartitionResult{}
		}
		err = callBatchHandler(handlerCtx, handler, consumed)
		if cause := finish(); cause != nil {
			err = errors.Join(err, cause)
		}
	}
	if err != nil {
		return consumerPartitionResult{err: err}
	}

	result := consumerPartitionResult{processed: len(batch.records)}
	if !consumer.assignment.owns(token, batch.partition) {
		result.err = ErrConsumerOwnershipLost

		return result
	}
	result.lastSuccessful = batch.records[len(batch.records)-1]
	result.successful = len(batch.records)

	return result
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
		if handlerErr == nil && partitionResult.err != nil {
			handlerErr = partitionResult.err
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
	err := consumer.client.CommitRecords(commitCtx, committable...)
	cancel()
	if err != nil {
		if handlerErr == nil {
			return result, err
		}

		return result, errors.Join(handlerErr, err)
	}
	result.Committed = committed

	return result, handlerErr
}
