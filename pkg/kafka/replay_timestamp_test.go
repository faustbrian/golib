package kafka

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestInspectorPlansReplayTimestampWindowAsOwnedExactRanges(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	partitions := []TopicPartition{
		{Topic: "events", Partition: 2},
		{Topic: "accounts", Partition: 0},
		{Topic: "events", Partition: 1},
	}
	backend := &timestampInspectorBackend{
		offsets: map[int64]kadm.ListedOffsets{
			-2: listedTimestampOffsets(
				timestampListedOffset("accounts", 0, 4, -1),
				timestampListedOffset("events", 1, 8, -1),
				timestampListedOffset("events", 2, 3, -1),
			),
			-1: listedTimestampOffsets(
				timestampListedOffset("accounts", 0, 12, -1),
				timestampListedOffset("events", 1, 8, -1),
				timestampListedOffset("events", 2, 30, -1),
			),
			start.UnixMilli(): listedTimestampOffsets(
				timestampListedOffset(
					"accounts",
					0,
					5,
					start.Add(time.Minute).UnixMilli(),
				),
				timestampListedOffset("events", 1, -1, -1),
				timestampListedOffset(
					"events",
					2,
					10,
					start.Add(time.Minute).UnixMilli(),
				),
			),
			end.UnixMilli(): listedTimestampOffsets(
				timestampListedOffset("accounts", 0, -1, -1),
				timestampListedOffset("events", 1, -1, -1),
				timestampListedOffset(
					"events",
					2,
					15,
					end.Add(time.Minute).UnixMilli(),
				),
			),
		},
	}
	inspector := &Inspector{
		admin:                 backend,
		requestTimeout:        time.Second,
		maxMetadataPartitions: 100,
	}
	request := ReplayTimestampRequest{
		StartInclusive: start.In(time.FixedZone("test", 2*60*60)),
		EndExclusive:   end.In(time.FixedZone("test", 2*60*60)),
		Partitions:     partitions,
	}

	plan, err := inspector.PlanReplayByTimestamp(context.Background(), request)
	if err != nil {
		t.Fatalf("PlanReplayByTimestamp() error = %v", err)
	}
	want := ReplayTimestampPlan{
		StartInclusive: start,
		EndExclusive:   end,
		Partitions: []ReplayTimestampPartition{
			{
				Topic: "accounts", Partition: 0,
				StartOffset: 5, EndOffset: 12, Remaining: 7,
			},
			{
				Topic: "events", Partition: 1,
				StartOffset: 8, EndOffset: 8, Remaining: 0,
			},
			{
				Topic: "events", Partition: 2,
				StartOffset: 10, EndOffset: 15, Remaining: 5,
			},
		},
		TotalRemaining: 12,
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("PlanReplayByTimestamp() = %#v, want %#v", plan, want)
	}
	if !reflect.DeepEqual(
		backend.timestamps,
		[]int64{-2, -1, start.UnixMilli(), end.UnixMilli()},
	) {
		t.Fatalf("offset timestamps = %v", backend.timestamps)
	}
	for _, requested := range backend.partitions {
		if !reflect.DeepEqual(requested, []TopicPartition{
			{Topic: "accounts", Partition: 0},
			{Topic: "events", Partition: 1},
			{Topic: "events", Partition: 2},
		}) {
			t.Fatalf("offset partitions = %#v", requested)
		}
	}

	ranges := plan.ReplayRanges()
	if !reflect.DeepEqual(ranges, []ReplayRange{
		{
			Topic: "accounts", Partition: 0,
			StartOffset: 5, EndOffset: 12,
		},
		{
			Topic: "events", Partition: 2,
			StartOffset: 10, EndOffset: 15,
		},
	}) {
		t.Fatalf("ReplayRanges() = %#v", ranges)
	}
	ranges[0].Topic = "mutated"
	partitions[0].Topic = "also-mutated"
	if plan.Partitions[0].Topic != "accounts" ||
		backend.partitions[0][0].Topic != "accounts" {
		t.Fatal("timestamp plan or backend input aliases caller-owned slices")
	}
}

func TestReplayTimestampRequestValidation(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	valid := ReplayTimestampRequest{
		StartInclusive: start,
		EndExclusive:   start.Add(time.Second),
		Partitions: []TopicPartition{{
			Topic: "events", Partition: 0,
		}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tooManyPartitions := make([]TopicPartition, 1_025)
	for index := range tooManyPartitions {
		tooManyPartitions[index] = TopicPartition{
			Topic: "events", Partition: int32(index),
		}
	}
	tooManyTopics := make([]TopicPartition, 65)
	for index := range tooManyTopics {
		tooManyTopics[index] = TopicPartition{
			Topic: "topic-" + twoDigits(index), Partition: 0,
		}
	}
	tests := []struct {
		name    string
		mutate  func(*ReplayTimestampRequest)
		wantErr error
	}{
		{
			name: "zero start",
			mutate: func(request *ReplayTimestampRequest) {
				request.StartInclusive = time.Time{}
			},
			wantErr: ErrInvalidReplayTimestampWindow,
		},
		{
			name: "pre epoch start",
			mutate: func(request *ReplayTimestampRequest) {
				request.StartInclusive = time.UnixMilli(-1)
			},
			wantErr: ErrInvalidReplayTimestampWindow,
		},
		{
			name: "sub millisecond start",
			mutate: func(request *ReplayTimestampRequest) {
				request.StartInclusive = request.StartInclusive.Add(time.Nanosecond)
			},
			wantErr: ErrInvalidReplayTimestampWindow,
		},
		{
			name: "end not after start",
			mutate: func(request *ReplayTimestampRequest) {
				request.EndExclusive = request.StartInclusive
			},
			wantErr: ErrInvalidReplayTimestampWindow,
		},
		{
			name: "no partitions",
			mutate: func(request *ReplayTimestampRequest) {
				request.Partitions = nil
			},
			wantErr: ErrReplayRangesRequired,
		},
		{
			name: "too many partitions",
			mutate: func(request *ReplayTimestampRequest) {
				request.Partitions = tooManyPartitions
			},
			wantErr: ErrTooManyReplayRanges,
		},
		{
			name: "too many topics",
			mutate: func(request *ReplayTimestampRequest) {
				request.Partitions = tooManyTopics
			},
			wantErr: ErrTooManyInspectionTargets,
		},
		{
			name: "invalid topic",
			mutate: func(request *ReplayTimestampRequest) {
				request.Partitions[0].Topic = "."
			},
			wantErr: ErrInvalidReplayRange,
		},
		{
			name: "negative partition",
			mutate: func(request *ReplayTimestampRequest) {
				request.Partitions[0].Partition = -1
			},
			wantErr: ErrInvalidReplayRange,
		},
		{
			name: "duplicate partition",
			mutate: func(request *ReplayTimestampRequest) {
				request.Partitions = append(
					request.Partitions,
					request.Partitions[0],
				)
			},
			wantErr: ErrDuplicateReplayRange,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := valid
			request.Partitions = append(
				[]TopicPartition(nil),
				valid.Partitions...,
			)
			test.mutate(&request)
			if err := request.Validate(); !errors.Is(err, test.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestInspectorRejectsIncompleteRetainedTimestampWindow(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	backend := &timestampInspectorBackend{
		offsets: map[int64]kadm.ListedOffsets{
			-2: listedTimestampOffsets(
				timestampListedOffset("events", 0, 5, -1),
			),
			-1: listedTimestampOffsets(
				timestampListedOffset("events", 0, 10, -1),
			),
			start.UnixMilli(): listedTimestampOffsets(
				timestampListedOffset(
					"events",
					0,
					5,
					start.UnixMilli(),
				),
			),
			end.UnixMilli(): listedTimestampOffsets(
				timestampListedOffset("events", 0, -1, -1),
			),
		},
	}
	inspector := &Inspector{
		admin:                 backend,
		requestTimeout:        time.Second,
		maxMetadataPartitions: 100,
	}

	plan, err := inspector.PlanReplayByTimestamp(
		context.Background(),
		ReplayTimestampRequest{
			StartInclusive: start,
			EndExclusive:   end,
			Partitions: []TopicPartition{{
				Topic: "events", Partition: 0,
			}},
		},
	)
	if !errors.Is(err, ErrReplayTimestampRangeIncomplete) {
		t.Fatalf("PlanReplayByTimestamp() error = %v", err)
	}
	if !reflect.DeepEqual(plan, ReplayTimestampPlan{}) {
		t.Fatalf("PlanReplayByTimestamp() plan = %#v", plan)
	}
}

func TestInspectorTimestampPlanningFailsClosed(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	request := ReplayTimestampRequest{
		StartInclusive: start,
		EndExclusive:   end,
		Partitions: []TopicPartition{{
			Topic: "events", Partition: 0,
		}},
	}
	validOffsets := map[int64]kadm.ListedOffsets{
		-2: listedTimestampOffsets(
			timestampListedOffset("events", 0, 0, -1),
		),
		-1: listedTimestampOffsets(
			timestampListedOffset("events", 0, 10, -1),
		),
		start.UnixMilli(): listedTimestampOffsets(
			timestampListedOffset(
				"events",
				0,
				2,
				start.Add(time.Minute).UnixMilli(),
			),
		),
		end.UnixMilli(): listedTimestampOffsets(
			timestampListedOffset("events", 0, -1, -1),
		),
	}
	backendError := errors.New("offset lookup failed")
	tests := []struct {
		name    string
		backend *timestampInspectorBackend
		closed  bool
		ctx     context.Context
		wantErr error
	}{
		{
			name: "invalid request",
			backend: &timestampInspectorBackend{
				offsets: validOffsets,
			},
			ctx:     context.Background(),
			wantErr: ErrInvalidReplayTimestampWindow,
		},
		{
			name: "nil context",
			backend: &timestampInspectorBackend{
				offsets: validOffsets,
			},
			wantErr: ErrContextRequired,
		},
		{
			name: "closed inspector",
			backend: &timestampInspectorBackend{
				offsets: validOffsets,
			},
			closed:  true,
			ctx:     context.Background(),
			wantErr: ErrInspectorClosed,
		},
		{
			name: "backend error",
			backend: &timestampInspectorBackend{
				offsets: validOffsets,
				errors:  map[int64]error{-1: backendError},
			},
			ctx:     context.Background(),
			wantErr: backendError,
		},
		{
			name: "missing partition",
			backend: &timestampInspectorBackend{
				offsets: map[int64]kadm.ListedOffsets{
					-2:                validOffsets[-2],
					-1:                validOffsets[-1],
					start.UnixMilli(): {},
					end.UnixMilli():   validOffsets[end.UnixMilli()],
				},
			},
			ctx:     context.Background(),
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name: "partition error",
			backend: &timestampInspectorBackend{
				offsets: map[int64]kadm.ListedOffsets{
					-2: validOffsets[-2],
					-1: validOffsets[-1],
					start.UnixMilli(): listedTimestampOffsets(
						kadm.ListedOffset{
							Topic: "events", Partition: 0,
							Err: kerr.NotLeaderForPartition,
						},
					),
					end.UnixMilli(): validOffsets[end.UnixMilli()],
				},
			},
			ctx:     context.Background(),
			wantErr: kerr.NotLeaderForPartition,
		},
		{
			name: "offset before log start",
			backend: &timestampInspectorBackend{
				offsets: map[int64]kadm.ListedOffsets{
					-2: listedTimestampOffsets(
						timestampListedOffset("events", 0, 3, -1),
					),
					-1: validOffsets[-1],
					start.UnixMilli(): listedTimestampOffsets(
						timestampListedOffset(
							"events",
							0,
							2,
							start.UnixMilli(),
						),
					),
					end.UnixMilli(): validOffsets[end.UnixMilli()],
				},
			},
			ctx:     context.Background(),
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name: "timestamp before requested boundary",
			backend: &timestampInspectorBackend{
				offsets: map[int64]kadm.ListedOffsets{
					-2: validOffsets[-2],
					-1: validOffsets[-1],
					start.UnixMilli(): listedTimestampOffsets(
						timestampListedOffset(
							"events",
							0,
							2,
							start.Add(-time.Millisecond).UnixMilli(),
						),
					),
					end.UnixMilli(): validOffsets[end.UnixMilli()],
				},
			},
			ctx:     context.Background(),
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name: "found start at high watermark",
			backend: &timestampInspectorBackend{
				offsets: map[int64]kadm.ListedOffsets{
					-2: validOffsets[-2],
					-1: validOffsets[-1],
					start.UnixMilli(): listedTimestampOffsets(
						timestampListedOffset(
							"events",
							0,
							10,
							start.UnixMilli(),
						),
					),
					end.UnixMilli(): validOffsets[end.UnixMilli()],
				},
			},
			ctx:     context.Background(),
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name: "found end at high watermark",
			backend: &timestampInspectorBackend{
				offsets: map[int64]kadm.ListedOffsets{
					-2:                validOffsets[-2],
					-1:                validOffsets[-1],
					start.UnixMilli(): validOffsets[start.UnixMilli()],
					end.UnixMilli(): listedTimestampOffsets(
						timestampListedOffset(
							"events",
							0,
							10,
							end.UnixMilli(),
						),
					),
				},
			},
			ctx:     context.Background(),
			wantErr: ErrInvalidInspectionResponse,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inspector := &Inspector{
				admin:                 test.backend,
				requestTimeout:        time.Second,
				maxMetadataPartitions: 100,
			}
			inspector.closed.Store(test.closed)
			testRequest := request
			if test.name == "invalid request" {
				testRequest.EndExclusive = testRequest.StartInclusive
			}
			plan, err := inspector.PlanReplayByTimestamp(
				test.ctx,
				testRequest,
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf(
					"PlanReplayByTimestamp() error = %v, want %v",
					err,
					test.wantErr,
				)
			}
			if !reflect.DeepEqual(plan, ReplayTimestampPlan{}) {
				t.Fatalf("PlanReplayByTimestamp() plan = %#v", plan)
			}
		})
	}
}

func TestInspectorTimestampPlanningHonorsLateCancellation(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	backend := &timestampInspectorBackend{
		offsets: map[int64]kadm.ListedOffsets{
			-2: listedTimestampOffsets(
				timestampListedOffset("events", 0, 0, -1),
			),
			-1: listedTimestampOffsets(
				timestampListedOffset("events", 0, 1, -1),
			),
			start.UnixMilli(): listedTimestampOffsets(
				timestampListedOffset(
					"events",
					0,
					0,
					start.UnixMilli(),
				),
			),
			end.UnixMilli(): listedTimestampOffsets(
				timestampListedOffset("events", 0, -1, -1),
			),
		},
		after: func(timestamp int64) {
			if timestamp == -2 {
				cancel()
			}
		},
	}
	inspector := &Inspector{
		admin:                 backend,
		requestTimeout:        time.Second,
		maxMetadataPartitions: 100,
	}

	plan, err := inspector.PlanReplayByTimestamp(
		ctx,
		ReplayTimestampRequest{
			StartInclusive: start,
			EndExclusive:   end,
			Partitions: []TopicPartition{{
				Topic: "events", Partition: 0,
			}},
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PlanReplayByTimestamp() error = %v", err)
	}
	if !reflect.DeepEqual(plan, ReplayTimestampPlan{}) {
		t.Fatalf("PlanReplayByTimestamp() plan = %#v", plan)
	}
	if !reflect.DeepEqual(backend.timestamps, []int64{-2}) {
		t.Fatalf("offset timestamps after cancellation = %v", backend.timestamps)
	}
}

func TestReplayTimestampPlanRejectsOverflowAndOversizedResponses(t *testing.T) {
	t.Parallel()

	inspector := &Inspector{maxMetadataPartitions: 1}
	partitions := []TopicPartition{
		{Topic: "events", Partition: 0},
		{Topic: "events", Partition: 1},
	}
	offsets := listedTimestampOffsets(
		timestampListedOffset("events", 0, 0, -1),
		timestampListedOffset("events", 1, 0, -1),
	)
	if err := inspector.validateReplayTimestampOffsets(
		-2,
		partitions,
		offsets,
	); !errors.Is(err, ErrInspectionResponseTooLarge) {
		t.Fatalf("validateReplayTimestampOffsets() error = %v", err)
	}

	inspector.maxMetadataPartitions = 10
	logStarts := listedTimestampOffsets(
		timestampListedOffset("accounts", 0, 0, -1),
		timestampListedOffset("events", 0, 0, -1),
	)
	highWatermarks := listedTimestampOffsets(
		timestampListedOffset("accounts", 0, 1, -1),
		timestampListedOffset("events", 0, math.MaxInt64, -1),
	)
	starts := listedTimestampOffsets(
		timestampListedOffset("accounts", 0, 0, 0),
		timestampListedOffset("events", 0, 0, 0),
	)
	ends := listedTimestampOffsets(
		timestampListedOffset("accounts", 0, -1, -1),
		timestampListedOffset("events", 0, -1, -1),
	)
	plan, err := inspector.buildReplayTimestampPlan(
		0,
		1,
		[]TopicPartition{
			{Topic: "events", Partition: 0},
			{Topic: "accounts", Partition: 0},
		},
		logStarts,
		highWatermarks,
		starts,
		ends,
	)
	if !errors.Is(err, ErrInvalidInspectionResponse) {
		t.Fatalf("buildReplayTimestampPlan() error = %v", err)
	}
	if !reflect.DeepEqual(plan, ReplayTimestampPlan{}) {
		t.Fatalf("buildReplayTimestampPlan() plan = %#v", plan)
	}
}

func TestInspectorRejectsTimestampTargetsAboveConfiguredBound(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	backend := &timestampInspectorBackend{}
	inspector := &Inspector{
		admin:                 backend,
		requestTimeout:        time.Second,
		maxMetadataPartitions: 1,
	}
	plan, err := inspector.PlanReplayByTimestamp(
		context.Background(),
		ReplayTimestampRequest{
			StartInclusive: start,
			EndExclusive:   start.Add(time.Second),
			Partitions: []TopicPartition{
				{Topic: "events", Partition: 0},
				{Topic: "events", Partition: 1},
			},
		},
	)
	if !errors.Is(err, ErrTooManyInspectionTargets) {
		t.Fatalf("PlanReplayByTimestamp() error = %v", err)
	}
	if !reflect.DeepEqual(plan, ReplayTimestampPlan{}) {
		t.Fatalf("PlanReplayByTimestamp() plan = %#v", plan)
	}
	if len(backend.timestamps) != 0 {
		t.Fatalf("broker requests = %v", backend.timestamps)
	}
}

func TestFranzInspectorListsOnlyRequestedPartitionOffsets(t *testing.T) {
	t.Parallel()

	response := kmsg.NewPtrListOffsetsResponse()
	response.Topics = append(
		response.Topics,
		listOffsetsResponseTopic("accounts",
			listOffsetsResponsePartition(0, 4, 100),
		),
		listOffsetsResponseTopic("events",
			listOffsetsResponsePartition(1, 8, 101),
			listOffsetsResponsePartition(2, -1, -1),
		),
	)
	requester := &recordingReplayTimestampRequester{
		shards: []kgo.ResponseShard{{Resp: response}},
	}
	backend := &franzInspectorBackend{offsetRequester: requester}
	partitions := []TopicPartition{
		{Topic: "accounts", Partition: 0},
		{Topic: "events", Partition: 1},
		{Topic: "events", Partition: 2},
	}

	offsets, err := backend.ListPartitionOffsets(
		context.Background(),
		100,
		partitions,
	)
	if err != nil {
		t.Fatalf("ListPartitionOffsets() error = %v", err)
	}
	if !reflect.DeepEqual(offsets, listedTimestampOffsets(
		timestampListedOffset("accounts", 0, 4, 100),
		timestampListedOffset("events", 1, 8, 101),
		timestampListedOffset("events", 2, -1, -1),
	)) {
		t.Fatalf("ListPartitionOffsets() = %#v", offsets)
	}
	request, ok := requester.requests[0].(*kmsg.ListOffsetsRequest)
	if !ok {
		t.Fatalf("request = %T", requester.requests[0])
	}
	if request.ReplicaID != -1 ||
		request.IsolationLevel != 0 ||
		len(request.Topics) != 2 ||
		request.Topics[0].Topic != "accounts" ||
		len(request.Topics[0].Partitions) != 1 ||
		request.Topics[0].Partitions[0].Timestamp != 100 ||
		request.Topics[1].Topic != "events" ||
		len(request.Topics[1].Partitions) != 2 ||
		request.Topics[1].Partitions[1].Partition != 2 {
		t.Fatalf("request = %#v", request)
	}
}

func TestReplayTimestampShardParsingFailsClosed(t *testing.T) {
	t.Parallel()

	partitions := []TopicPartition{{Topic: "events", Partition: 0}}
	backendError := errors.New("request failed")
	tests := []struct {
		name    string
		shards  []kgo.ResponseShard
		wantErr error
	}{
		{
			name:    "shard error",
			shards:  []kgo.ResponseShard{{Err: backendError}},
			wantErr: backendError,
		},
		{
			name:    "missing response",
			shards:  []kgo.ResponseShard{{}},
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name: "wrong response type",
			shards: []kgo.ResponseShard{{
				Resp: kmsg.NewPtrMetadataResponse(),
			}},
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name: "unexpected partition",
			shards: []kgo.ResponseShard{{
				Resp: listOffsetsResponse(
					"events",
					listOffsetsResponsePartition(1, 0, -1),
				),
			}},
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name: "partition error",
			shards: []kgo.ResponseShard{{
				Resp: listOffsetsErrorResponse(
					"events",
					0,
					kerr.NotLeaderForPartition.Code,
				),
			}},
			wantErr: kerr.NotLeaderForPartition,
		},
		{
			name: "duplicate response",
			shards: []kgo.ResponseShard{
				{
					Resp: listOffsetsResponse(
						"events",
						listOffsetsResponsePartition(0, 0, -1),
					),
				},
				{
					Resp: listOffsetsResponse(
						"events",
						listOffsetsResponsePartition(0, 0, -1),
					),
				},
			},
			wantErr: ErrInvalidInspectionResponse,
		},
		{
			name:    "missing partition",
			shards:  nil,
			wantErr: ErrInvalidInspectionResponse,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			offsets, err := parseReplayTimestampShards(
				test.shards,
				partitions,
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf(
					"parseReplayTimestampShards() error = %v, want %v",
					err,
					test.wantErr,
				)
			}
			if offsets != nil {
				t.Fatalf("parseReplayTimestampShards() = %#v", offsets)
			}
		})
	}
}

type timestampInspectorBackend struct {
	recordingInspectorBackend
	offsets    map[int64]kadm.ListedOffsets
	errors     map[int64]error
	timestamps []int64
	partitions [][]TopicPartition
	after      func(int64)
}

func (backend *timestampInspectorBackend) ListPartitionOffsets(
	_ context.Context,
	timestamp int64,
	partitions []TopicPartition,
) (kadm.ListedOffsets, error) {
	backend.timestamps = append(backend.timestamps, timestamp)
	backend.partitions = append(
		backend.partitions,
		append([]TopicPartition(nil), partitions...),
	)
	if backend.after != nil {
		backend.after(timestamp)
	}

	return backend.offsets[timestamp], backend.errors[timestamp]
}

type recordingReplayTimestampRequester struct {
	requests []kmsg.Request
	shards   []kgo.ResponseShard
}

func (requester *recordingReplayTimestampRequester) RequestSharded(
	_ context.Context,
	request kmsg.Request,
) []kgo.ResponseShard {
	requester.requests = append(requester.requests, request)

	return requester.shards
}

func timestampListedOffset(
	topic string,
	partition int32,
	offset int64,
	timestamp int64,
) kadm.ListedOffset {
	return kadm.ListedOffset{
		Topic: topic, Partition: partition,
		Offset: offset, Timestamp: timestamp, LeaderEpoch: -1,
	}
}

func listedTimestampOffsets(
	offsets ...kadm.ListedOffset,
) kadm.ListedOffsets {
	listed := make(kadm.ListedOffsets)
	for _, offset := range offsets {
		if listed[offset.Topic] == nil {
			listed[offset.Topic] = make(map[int32]kadm.ListedOffset)
		}
		listed[offset.Topic][offset.Partition] = offset
	}

	return listed
}

func listOffsetsResponse(
	topic string,
	partitions ...kmsg.ListOffsetsResponseTopicPartition,
) *kmsg.ListOffsetsResponse {
	response := kmsg.NewPtrListOffsetsResponse()
	response.Topics = append(
		response.Topics,
		listOffsetsResponseTopic(topic, partitions...),
	)

	return response
}

func listOffsetsResponseTopic(
	topic string,
	partitions ...kmsg.ListOffsetsResponseTopicPartition,
) kmsg.ListOffsetsResponseTopic {
	responseTopic := kmsg.NewListOffsetsResponseTopic()
	responseTopic.Topic = topic
	responseTopic.Partitions = append(
		responseTopic.Partitions,
		partitions...,
	)

	return responseTopic
}

func listOffsetsResponsePartition(
	partition int32,
	offset int64,
	timestamp int64,
) kmsg.ListOffsetsResponseTopicPartition {
	responsePartition := kmsg.NewListOffsetsResponseTopicPartition()
	responsePartition.Partition = partition
	responsePartition.Offset = offset
	responsePartition.Timestamp = timestamp

	return responsePartition
}

func listOffsetsErrorResponse(
	topic string,
	partition int32,
	errorCode int16,
) *kmsg.ListOffsetsResponse {
	responsePartition := listOffsetsResponsePartition(partition, -1, -1)
	responsePartition.ErrorCode = errorCode

	return listOffsetsResponse(topic, responsePartition)
}

func twoDigits(value int) string {
	return string([]byte{
		byte('0' + value/10),
		byte('0' + value%10),
	})
}
