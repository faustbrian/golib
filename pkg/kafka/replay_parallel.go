package kafka

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type replayPartitionResult struct {
	progress   ReplayRangeResult
	deadline   time.Time
	processed  int64
	skipped    int64
	failed     int64
	completed  bool
	progressed bool
	err        error
}

func (reader *ReplayReader) processReplayRecordsSerial(
	ctx context.Context,
	handler Handler,
	records []*kgo.Record,
	result *ReplayResult,
	indexes map[replayPartition]int,
	deadlines map[replayPartition]time.Time,
) error {
	for _, record := range records {
		if replayProgressExpired(deadlines, reader.now()) {
			return ErrReplayStalled
		}
		partition := replayPartition{
			topic: record.Topic, partition: record.Partition,
		}
		index, exists := indexes[partition]
		if !exists {
			return ErrUnexpectedReplayRecord
		}
		partitionResult := reader.processReplayPartition(
			ctx,
			handler,
			consumerPartitionBatch{
				partition: TopicPartition{
					Topic: record.Topic, Partition: record.Partition,
				},
				records: []*kgo.Record{record},
			},
			result.Ranges[index],
			deadlines[partition],
		)
		reader.applyReplayPartitionResult(
			result,
			index,
			partition,
			partitionResult,
			deadlines,
		)
		if partitionResult.err != nil {
			return partitionResult.err
		}
	}

	return nil
}

func (reader *ReplayReader) processReplayRecordsParallel(
	ctx context.Context,
	handler Handler,
	records []*kgo.Record,
	result *ReplayResult,
	indexes map[replayPartition]int,
	deadlines map[replayPartition]time.Time,
) error {
	batches := partitionBatches(records)
	for _, batch := range batches {
		if _, exists := indexes[replayPartition{
			topic: batch.partition.Topic, partition: batch.partition.Partition,
		}]; !exists {
			return ErrUnexpectedReplayRecord
		}
	}

	partitionResults := runReplayPartitionWorkers(
		batches,
		reader.maxConcurrentHandlers,
		func(batch consumerPartitionBatch) replayPartitionResult {
			partition := replayPartition{
				topic:     batch.partition.Topic,
				partition: batch.partition.Partition,
			}
			index := indexes[partition]

			return reader.processReplayPartition(
				ctx,
				handler,
				batch,
				result.Ranges[index],
				deadlines[partition],
			)
		},
	)

	var resultErr error
	for index, batch := range batches {
		partition := replayPartition{
			topic: batch.partition.Topic, partition: batch.partition.Partition,
		}
		reader.applyReplayPartitionResult(
			result,
			indexes[partition],
			partition,
			partitionResults[index],
			deadlines,
		)
		resultErr = errors.Join(resultErr, partitionResults[index].err)
	}

	return resultErr
}

func runReplayPartitionWorkers(
	batches []consumerPartitionBatch,
	maximum int,
	process func(consumerPartitionBatch) replayPartitionResult,
) []replayPartitionResult {
	results := make([]replayPartitionResult, len(batches))
	workers := min(maximum, len(batches))
	if workers <= 1 {
		for index, batch := range batches {
			results[index] = process(batch)
		}

		return results
	}

	// This invocation owns and closes jobs. Workers only receive from it and
	// are joined before borrowed records or application errors can escape.
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

func (reader *ReplayReader) processReplayPartition(
	ctx context.Context,
	handler Handler,
	batch consumerPartitionBatch,
	progress ReplayRangeResult,
	deadline time.Time,
) replayPartitionResult {
	result := replayPartitionResult{progress: progress, deadline: deadline}
	for _, record := range batch.records {
		if result.progress.Complete {
			result.progress.Skipped++
			result.skipped++

			continue
		}
		if cause := context.Cause(ctx); cause != nil {
			result.err = cause

			return result
		}
		if !reader.now().Before(result.deadline) {
			result.err = ErrReplayStalled

			return result
		}
		if record.Offset < result.progress.StartOffset {
			result.progress.Failed++
			result.failed++
			result.err = ErrReplayOffsetGap

			return result
		}
		if record.Offset < result.progress.NextOffset {
			result.progress.Skipped++
			result.skipped++

			continue
		}
		if record.Offset != result.progress.NextOffset ||
			record.Offset >= result.progress.EndOffset {
			result.progress.Failed++
			result.failed++
			result.err = ErrReplayOffsetGap

			return result
		}

		message, err := consumedMessageWithinLimits(record, reader.limits)
		if err == nil {
			handlerCtx, cancel := context.WithTimeout(ctx, reader.handlerTimeout)
			err = callHandler(handlerCtx, handler, message)
			if cause := context.Cause(handlerCtx); cause != nil {
				err = errors.Join(err, cause)
			}
			cancel()
		}
		if err != nil {
			result.progress.Failed++
			result.failed++
			result.err = err

			return result
		}

		result.progress.Processed++
		result.processed++
		result.progress.NextOffset++
		result.deadline = reader.now().Add(reader.progressTimeout)
		result.progressed = true
		if result.progress.NextOffset == result.progress.EndOffset {
			result.progress.Complete = true
			result.completed = true
		}
	}

	return result
}

func (reader *ReplayReader) applyReplayPartitionResult(
	result *ReplayResult,
	index int,
	partition replayPartition,
	partitionResult replayPartitionResult,
	deadlines map[replayPartition]time.Time,
) {
	result.Ranges[index] = partitionResult.progress
	result.Processed += partitionResult.processed
	result.Skipped += partitionResult.skipped
	result.Failed += partitionResult.failed
	if partitionResult.progressed {
		deadlines[partition] = partitionResult.deadline
	}
	if partitionResult.completed {
		result.CompletedRanges++
		result.IncompleteRanges--
		delete(deadlines, partition)
		reader.client.PauseFetchPartitions(map[string][]int32{
			partitionResult.progress.Topic: {
				partitionResult.progress.Partition,
			},
		})
	}
}
