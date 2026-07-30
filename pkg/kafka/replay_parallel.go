package kafka

import (
	"context"
	"errors"
	"strings"
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
	handler ReplayHandler,
	records []*kgo.Record,
	result *ReplayResult,
	indexes map[replayPartition]int,
	metadata map[replayPartition]ReplayMetadata,
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
			metadata[partition],
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
	handler ReplayHandler,
	records []*kgo.Record,
	result *ReplayResult,
	indexes map[replayPartition]int,
	metadata map[replayPartition]ReplayMetadata,
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
				metadata[partition],
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
	if workers == 0 {
		return results
	}
	if workers == 1 {
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
	handler ReplayHandler,
	batch consumerPartitionBatch,
	progress ReplayRangeResult,
	metadata ReplayMetadata,
	deadline time.Time,
) replayPartitionResult {
	result := replayPartitionResult{progress: progress, deadline: deadline}
	for _, record := range batch.records {
		var startedAt time.Time
		if reader.observers.enabled() {
			startedAt = time.Now()
		}
		if result.progress.Complete {
			result.progress.Skipped++
			result.skipped++
			reader.observeReplayRecord(
				ctx,
				startedAt,
				record,
				0,
				1,
				0,
				nil,
			)

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
			reader.observeReplayRecord(
				ctx,
				startedAt,
				record,
				0,
				0,
				1,
				result.err,
			)

			return result
		}
		if record.Offset < result.progress.NextOffset {
			result.progress.Skipped++
			result.skipped++
			reader.observeReplayRecord(
				ctx,
				startedAt,
				record,
				0,
				1,
				0,
				nil,
			)

			continue
		}
		if record.Offset != result.progress.NextOffset ||
			record.Offset >= result.progress.EndOffset {
			result.progress.Failed++
			result.failed++
			result.err = ErrReplayOffsetGap
			reader.observeReplayRecord(
				ctx,
				startedAt,
				record,
				0,
				0,
				1,
				result.err,
			)

			return result
		}

		message, err := consumedMessageWithinLimits(record, reader.limits)
		if err == nil {
			handlerCtx, cancel := context.WithTimeout(ctx, reader.handlerTimeout)
			err = callReplayHandler(handlerCtx, handler, ReplayRecord{
				ConsumedRecord: message,
				Metadata:       metadata,
			})
			if cause := context.Cause(handlerCtx); cause != nil {
				err = errors.Join(err, cause)
			}
			cancel()
		}
		if err != nil {
			result.progress.Failed++
			result.failed++
			result.err = err
			reader.observeReplayRecord(
				ctx,
				startedAt,
				record,
				0,
				0,
				1,
				err,
			)

			return result
		}

		result.progress.Processed++
		result.processed++
		result.progress.NextOffset++
		reader.observeReplayRecord(
			ctx,
			startedAt,
			record,
			1,
			0,
			0,
			nil,
		)
		result.deadline = reader.now().Add(reader.progressTimeout)
		result.progressed = true
		if result.progress.NextOffset == result.progress.EndOffset {
			result.progress.Complete = true
			result.completed = true
		}
	}

	return result
}

func callReplayHandler(
	ctx context.Context,
	handler ReplayHandler,
	record ReplayRecord,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrHandlerPanic
		}
	}()

	return handler.HandleReplay(ctx, record)
}

func (reader *ReplayReader) observeReplayRecord(
	ctx context.Context,
	startedAt time.Time,
	record *kgo.Record,
	processed int64,
	skipped int64,
	failed int64,
	err error,
) {
	if !reader.observers.enabled() {
		return
	}
	observation := Observation{
		Kind:            ObservationReplayRecord,
		StartedAt:       startedAt,
		Duration:        time.Since(startedAt),
		ClientID:        reader.clientID,
		RecordCount:     1,
		ProcessedCount:  int(processed),
		ReplayProcessed: processed,
		ReplaySkipped:   skipped,
		ReplayFailed:    failed,
		Succeeded:       err == nil,
	}
	if _, validationErr := consumedMessageWithinLimits(
		record,
		reader.limits,
	); validationErr == nil {
		observation.Topic = strings.Clone(record.Topic)
		observation.Partition = record.Partition
		observation.PartitionKnown = true
		observation.PartitionCount = 1
		observation.Offset = record.Offset
		observation.OffsetKnown = true
		observation.Timestamp = record.Timestamp
		observation.RecordBytes = consumedRecordSize(record)
	}
	if err != nil {
		observation.Category = classifyConsumerObservationError(err)
	}
	reader.dispatchObservation(ctx, observation)
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
