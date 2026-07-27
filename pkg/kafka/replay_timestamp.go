package kafka

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

var (
	ErrInvalidReplayTimestampWindow = errors.New(
		"kafka: replay timestamp window is invalid",
	)
	ErrReplayTimestampRangeIncomplete = errors.New(
		"kafka: replay timestamp window may precede retained records",
	)
)

// ReplayTimestampRequest selects explicit partitions within one inclusive
// start and exclusive end timestamp window. Kafka resolves timestamps at
// millisecond precision, so both boundaries must be exact milliseconds at or
// after the Unix epoch.
type ReplayTimestampRequest struct {
	StartInclusive time.Time
	EndExclusive   time.Time
	Partitions     []TopicPartition
}

// Validate reports whether the timestamp window and explicit partition set
// can be resolved through bounded exact-partition Kafka requests.
func (request ReplayTimestampRequest) Validate() error {
	start, validStart := exactReplayTimestampMilli(request.StartInclusive)
	end, validEnd := exactReplayTimestampMilli(request.EndExclusive)
	if !validStart || !validEnd || end <= start {
		return ErrInvalidReplayTimestampWindow
	}
	if len(request.Partitions) == 0 {
		return ErrReplayRangesRequired
	}
	if len(request.Partitions) > 1_024 {
		return ErrTooManyReplayRanges
	}

	seenPartitions := make(map[replayPartition]struct{}, len(request.Partitions))
	seenTopics := make(map[string]struct{})
	for _, partition := range request.Partitions {
		if !validKafkaTopicName(partition.Topic, 249) ||
			partition.Partition < 0 {
			return ErrInvalidReplayRange
		}
		key := replayPartition{
			topic: partition.Topic, partition: partition.Partition,
		}
		if _, duplicate := seenPartitions[key]; duplicate {
			return ErrDuplicateReplayRange
		}
		seenPartitions[key] = struct{}{}
		seenTopics[partition.Topic] = struct{}{}
	}
	if len(seenTopics) > 64 {
		return ErrTooManyInspectionTargets
	}

	return nil
}

// ReplayTimestampPartition is one resolved partition range. A zero Remaining
// count is a valid empty time window and is omitted by ReplayRanges.
type ReplayTimestampPartition struct {
	Topic       string
	Partition   int32
	StartOffset int64
	EndOffset   int64
	Remaining   int64
}

// ReplayTimestampPlan is an owned broker-resolved timestamp plan. Boundaries
// are canonical UTC millisecond values and partitions are sorted.
type ReplayTimestampPlan struct {
	StartInclusive time.Time
	EndExclusive   time.Time
	Partitions     []ReplayTimestampPartition
	TotalRemaining int64
}

// ReplayRanges returns independently owned non-empty exact offset ranges that
// can be supplied to ReplayConfig. An empty result means the timestamp window
// currently contains no records in the selected partitions.
func (plan ReplayTimestampPlan) ReplayRanges() []ReplayRange {
	ranges := make([]ReplayRange, 0, len(plan.Partitions))
	for _, partition := range plan.Partitions {
		if partition.Remaining == 0 {
			continue
		}
		ranges = append(ranges, ReplayRange{
			Topic:       partition.Topic,
			Partition:   partition.Partition,
			StartOffset: partition.StartOffset,
			EndOffset:   partition.EndOffset,
		})
	}

	return ranges
}

// PlanReplayByTimestamp resolves one explicit timestamp window to exact Kafka
// offsets without polling records, joining a group, changing group offsets, or
// invoking replay handlers. Any error returns a zero plan.
func (inspector *Inspector) PlanReplayByTimestamp(
	ctx context.Context,
	request ReplayTimestampRequest,
) (ReplayTimestampPlan, error) {
	if err := request.Validate(); err != nil {
		return ReplayTimestampPlan{}, err
	}
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return ReplayTimestampPlan{}, err
	}
	defer cancel()

	partitions := append([]TopicPartition(nil), request.Partitions...)
	if len(partitions) > inspector.metadataPartitionLimit() {
		return ReplayTimestampPlan{}, ErrTooManyInspectionTargets
	}
	slices.SortFunc(partitions, func(left, right TopicPartition) int {
		if topicOrder := cmp.Compare(left.Topic, right.Topic); topicOrder != 0 {
			return topicOrder
		}

		return cmp.Compare(left.Partition, right.Partition)
	})
	startMilli := request.StartInclusive.UnixMilli()
	endMilli := request.EndExclusive.UnixMilli()
	queries := []int64{-2, -1, startMilli, endMilli}
	responses := make([]kadm.ListedOffsets, len(queries))
	for index, timestamp := range queries {
		responses[index], err = inspector.admin.ListPartitionOffsets(
			requestCtx,
			timestamp,
			partitions,
		)
		if err != nil {
			return ReplayTimestampPlan{}, inspectionRequestError(
				requestCtx,
				err,
			)
		}
		if cause := context.Cause(requestCtx); cause != nil {
			return ReplayTimestampPlan{}, cause
		}
	}

	return inspector.buildReplayTimestampPlan(
		startMilli,
		endMilli,
		partitions,
		responses[0],
		responses[1],
		responses[2],
		responses[3],
	)
}

func exactReplayTimestampMilli(timestamp time.Time) (int64, bool) {
	if timestamp.IsZero() ||
		timestamp.Nanosecond()%int(time.Millisecond) != 0 {
		return 0, false
	}
	millisecond := timestamp.UnixMilli()
	if millisecond < 0 ||
		!time.UnixMilli(millisecond).Equal(timestamp) {
		return 0, false
	}

	return millisecond, true
}

func (inspector *Inspector) buildReplayTimestampPlan(
	startMilli int64,
	endMilli int64,
	partitions []TopicPartition,
	logStarts kadm.ListedOffsets,
	highWatermarks kadm.ListedOffsets,
	starts kadm.ListedOffsets,
	ends kadm.ListedOffsets,
) (ReplayTimestampPlan, error) {
	queries := []struct {
		timestamp int64
		offsets   kadm.ListedOffsets
	}{
		{timestamp: -2, offsets: logStarts},
		{timestamp: -1, offsets: highWatermarks},
		{timestamp: startMilli, offsets: starts},
		{timestamp: endMilli, offsets: ends},
	}
	for _, query := range queries {
		if err := inspector.validateReplayTimestampOffsets(
			query.timestamp,
			partitions,
			query.offsets,
		); err != nil {
			return ReplayTimestampPlan{}, err
		}
	}

	plan := ReplayTimestampPlan{
		StartInclusive: time.UnixMilli(startMilli).UTC(),
		EndExclusive:   time.UnixMilli(endMilli).UTC(),
		Partitions: make(
			[]ReplayTimestampPartition,
			0,
			len(partitions),
		),
	}
	for _, partition := range partitions {
		logStart, _ := logStarts.Lookup(partition.Topic, partition.Partition)
		highWatermark, _ := highWatermarks.Lookup(
			partition.Topic,
			partition.Partition,
		)
		start, _ := starts.Lookup(partition.Topic, partition.Partition)
		end, _ := ends.Lookup(partition.Topic, partition.Partition)
		startOffset, startFound := resolvedReplayTimestampOffset(
			start,
			highWatermark.Offset,
		)
		endOffset, _ := resolvedReplayTimestampOffset(
			end,
			highWatermark.Offset,
		)
		if highWatermark.Offset < logStart.Offset ||
			startOffset < logStart.Offset ||
			endOffset < startOffset ||
			endOffset > highWatermark.Offset ||
			(startFound && startOffset >= highWatermark.Offset) ||
			(end.Offset != -1 && endOffset >= highWatermark.Offset) {
			return ReplayTimestampPlan{}, ErrInvalidInspectionResponse
		}
		if logStart.Offset > 0 &&
			startFound &&
			startOffset == logStart.Offset &&
			start.Timestamp >= startMilli {
			return ReplayTimestampPlan{}, ErrReplayTimestampRangeIncomplete
		}
		remaining := endOffset - startOffset
		if remaining > math.MaxInt64-plan.TotalRemaining {
			return ReplayTimestampPlan{}, ErrInvalidInspectionResponse
		}
		plan.Partitions = append(
			plan.Partitions,
			ReplayTimestampPartition{
				Topic: partition.Topic, Partition: partition.Partition,
				StartOffset: startOffset, EndOffset: endOffset,
				Remaining: remaining,
			},
		)
		plan.TotalRemaining += remaining
	}

	return plan, nil
}

func (inspector *Inspector) validateReplayTimestampOffsets(
	timestamp int64,
	partitions []TopicPartition,
	offsets kadm.ListedOffsets,
) error {
	expected := make(map[replayPartition]struct{}, len(partitions))
	for _, partition := range partitions {
		expected[replayPartition{
			topic: partition.Topic, partition: partition.Partition,
		}] = struct{}{}
	}
	partitionCount := 0
	for topic, listedPartitions := range offsets {
		if len(listedPartitions) >
			inspector.metadataPartitionLimit()-partitionCount {
			return ErrInspectionResponseTooLarge
		}
		partitionCount += len(listedPartitions)
		for partition, offset := range listedPartitions {
			key := replayPartition{topic: topic, partition: partition}
			if _, requested := expected[key]; !requested ||
				partition < 0 ||
				offset.Topic != topic ||
				offset.Partition != partition ||
				!validListedReplayTimestampOffset(timestamp, offset) {
				return errors.Join(
					ErrInvalidInspectionResponse,
					offset.Err,
				)
			}
			delete(expected, key)
		}
	}
	if len(expected) != 0 {
		return ErrInvalidInspectionResponse
	}

	return nil
}

func validListedReplayTimestampOffset(
	requested int64,
	offset kadm.ListedOffset,
) bool {
	if offset.Err != nil {
		return false
	}
	if requested < 0 {
		return offset.Offset >= 0 && offset.Timestamp == -1
	}
	if offset.Offset == -1 {
		return offset.Timestamp == -1
	}

	return offset.Offset >= 0 && offset.Timestamp >= requested
}

func resolvedReplayTimestampOffset(
	offset kadm.ListedOffset,
	highWatermark int64,
) (int64, bool) {
	if offset.Offset == -1 {
		return highWatermark, false
	}

	return offset.Offset, true
}

type replayTimestampRequester interface {
	RequestSharded(context.Context, kmsg.Request) []kgo.ResponseShard
}

func (backend *franzInspectorBackend) ListPartitionOffsets(
	ctx context.Context,
	timestamp int64,
	partitions []TopicPartition,
) (kadm.ListedOffsets, error) {
	request := kmsg.NewPtrListOffsetsRequest()
	request.ReplicaID = -1
	request.IsolationLevel = 0
	for _, partition := range partitions {
		if len(request.Topics) == 0 ||
			request.Topics[len(request.Topics)-1].Topic != partition.Topic {
			topic := kmsg.NewListOffsetsRequestTopic()
			topic.Topic = partition.Topic
			request.Topics = append(request.Topics, topic)
		}
		requestPartition := kmsg.NewListOffsetsRequestTopicPartition()
		requestPartition.Partition = partition.Partition
		requestPartition.Timestamp = timestamp
		lastTopic := len(request.Topics) - 1
		request.Topics[lastTopic].Partitions = append(
			request.Topics[lastTopic].Partitions,
			requestPartition,
		)
	}

	return parseReplayTimestampShards(
		backend.offsetRequester.RequestSharded(ctx, request),
		partitions,
	)
}

func parseReplayTimestampShards(
	shards []kgo.ResponseShard,
	partitions []TopicPartition,
) (kadm.ListedOffsets, error) {
	expected := make(map[replayPartition]struct{}, len(partitions))
	for _, partition := range partitions {
		expected[replayPartition{
			topic: partition.Topic, partition: partition.Partition,
		}] = struct{}{}
	}
	offsets := make(kadm.ListedOffsets)
	for _, shard := range shards {
		if shard.Err != nil {
			return nil, shard.Err
		}
		response, ok := shard.Resp.(*kmsg.ListOffsetsResponse)
		if !ok || response == nil {
			return nil, ErrInvalidInspectionResponse
		}
		for _, topic := range response.Topics {
			for _, partition := range topic.Partitions {
				key := replayPartition{
					topic: topic.Topic, partition: partition.Partition,
				}
				if _, requested := expected[key]; !requested {
					return nil, ErrInvalidInspectionResponse
				}
				if brokerErr := kerr.ErrorForCode(
					partition.ErrorCode,
				); brokerErr != nil {
					return nil, brokerErr
				}
				if offsets[topic.Topic] == nil {
					offsets[topic.Topic] = make(
						map[int32]kadm.ListedOffset,
					)
				}
				offsets[topic.Topic][partition.Partition] = kadm.ListedOffset{
					Topic:       topic.Topic,
					Partition:   partition.Partition,
					Timestamp:   partition.Timestamp,
					Offset:      partition.Offset,
					LeaderEpoch: partition.LeaderEpoch,
				}
				delete(expected, key)
			}
		}
	}
	if len(expected) != 0 {
		return nil, ErrInvalidInspectionResponse
	}

	return offsets, nil
}
