package kafka

import (
	"crypto/tls"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNewConsumerRequiresGroupIdentity(t *testing.T) {
	t.Parallel()

	consumer, err := NewConsumer(ConsumerConfig{
		Brokers:     []string{"broker.internal:9092"},
		ClientID:    "track-projection",
		Topics:      []string{"track.tracking-event.v1"},
		ResetOffset: OffsetEarliest,
	})

	if consumer != nil {
		t.Fatal("NewConsumer() returned a consumer without a group identity")
	}
	if !errors.Is(err, ErrGroupIDRequired) {
		t.Fatalf("NewConsumer() error = %v, want %v", err, ErrGroupIDRequired)
	}
}

func TestConsumerConfigAppliesBoundedDefaults(t *testing.T) {
	t.Parallel()

	config, err := normalizeConsumerConfig(validConsumerConfig())
	if err != nil {
		t.Fatalf("normalizeConsumerConfig() error = %v", err)
	}

	if config.MaxPollRecords != 100 ||
		config.MaxPausedPartitions != 256 ||
		config.MaxAssignedPartitions != 1_024 ||
		config.Limits != DefaultMessageLimits() ||
		config.BalancePolicy != BalanceCooperativeSticky ||
		config.RebalanceHandler != RebalanceCancelHandler ||
		config.MaxConcurrentFetches != 4 ||
		config.MaxConcurrentHandlers != 1 ||
		config.FetchMaxBytes != 50<<20 ||
		config.FetchMaxPartitionBytes != 1<<20 ||
		config.FetchMaxWait != 500*time.Millisecond ||
		config.SessionTimeout != 45*time.Second ||
		config.RebalanceTimeout != 60*time.Second ||
		config.HeartbeatInterval != 3*time.Second ||
		config.HandlerTimeout != 30*time.Second ||
		config.CommitTimeout != 10*time.Second ||
		config.ShutdownTimeout != 30*time.Second ||
		config.DialTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestNewConsumerConstructsAndReportsClientFactoryFailure(t *testing.T) {
	t.Parallel()

	consumer, err := NewConsumer(validConsumerConfig())
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if closeErr := consumer.Close(); closeErr != nil {
		t.Fatalf("Consumer.Close() error = %v", closeErr)
	}

	factoryErr := errors.New("client construction failed")
	latestConfig := validConsumerConfig()
	latestConfig.ResetOffset = OffsetLatest
	consumer, err = newConsumer(latestConfig, func(...kgo.Opt) (*kgo.Client, error) {
		return nil, factoryErr
	})
	if consumer != nil {
		closeErr := consumer.Close()
		t.Fatalf(
			"newConsumer() returned a consumer after client factory failure; close error = %v",
			closeErr,
		)
	}
	if !errors.Is(err, factoryErr) {
		t.Fatalf("newConsumer() error = %v, want %v", err, factoryErr)
	}
}

func TestNewConsumerOwnsPauseSubscriptionPolicy(t *testing.T) {
	t.Parallel()

	config := validConsumerConfig()
	consumer, err := NewConsumer(config)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := consumer.Close(); closeErr != nil {
			t.Errorf("Consumer.Close() error = %v", closeErr)
		}
	})
	originalTopic := config.Topics[0]
	config.Topics[0] = "commands"

	if err := consumer.PausePartitions(TopicPartition{
		Topic: originalTopic, Partition: 0,
	}); err != nil {
		t.Fatalf("PausePartitions(original topic) error = %v", err)
	}
	if err := consumer.PausePartitions(TopicPartition{
		Topic: "commands", Partition: 0,
	}); !errors.Is(err, ErrPauseTopicNotSubscribed) {
		t.Fatalf("PausePartitions(mutated topic) error = %v, want %v", err, ErrPauseTopicNotSubscribed)
	}
}

func TestNewConsumerAppliesConsumerPolicyOptions(t *testing.T) {
	t.Parallel()

	config := validConsumerConfig()
	config.InstanceID = "track-processor-01"
	config.Rack = "eu-west-1a"
	config.BalancePolicy = BalanceEagerToCooperative
	config.MaxConcurrentFetches = 3
	config.MaxConcurrentHandlers = 3
	config.FetchMaxPartitionBytes = 2 << 20
	var franzClient *kgo.Client
	consumer, err := newConsumer(config, func(options ...kgo.Opt) (*kgo.Client, error) {
		client, clientErr := kgo.NewClient(options...)
		franzClient = client

		return client, clientErr
	})
	if err != nil {
		t.Fatalf("newConsumer() error = %v", err)
	}
	defer closeConsumerForTest(t, consumer)
	if got := franzClient.OptValue(kgo.FetchMaxPartitionBytes); got != int32(2<<20) {
		t.Fatalf("FetchMaxPartitionBytes option = %#v", got)
	}
	if got := franzClient.OptValue(kgo.MaxConcurrentFetches); got != 3 {
		t.Fatalf("MaxConcurrentFetches option = %#v", got)
	}
	if consumer.maxConcurrentHandlers != 3 {
		t.Fatalf(
			"consumer MaxConcurrentHandlers = %d",
			consumer.maxConcurrentHandlers,
		)
	}
	if got := franzClient.OptValue(kgo.InstanceID); got != "track-processor-01" {
		t.Fatalf("InstanceID option = %#v", got)
	}
	if got := franzClient.OptValue(kgo.Rack); got != "eu-west-1a" {
		t.Fatalf("Rack option = %#v", got)
	}
	onAssigned, ok := franzClient.OptValue(kgo.OnPartitionsAssigned).(func(
		context.Context, *kgo.Client, map[string][]int32,
	))
	if !ok {
		t.Fatal("OnPartitionsAssigned option is not configured")
	}
	onRevoked, ok := franzClient.OptValue(kgo.OnPartitionsRevoked).(func(
		context.Context, *kgo.Client, map[string][]int32,
	))
	if !ok {
		t.Fatal("OnPartitionsRevoked option is not configured")
	}
	onLost, ok := franzClient.OptValue(kgo.OnPartitionsLost).(func(
		context.Context, *kgo.Client, map[string][]int32,
	))
	if !ok {
		t.Fatal("OnPartitionsLost option is not configured")
	}
	onBlocked, ok := franzClient.OptValue(kgo.OnPartitionsCallbackBlocked).(func(
		context.Context, *kgo.Client,
	))
	if !ok {
		t.Fatal("OnPartitionsCallbackBlocked option is not configured")
	}
	onBlocked(context.Background(), franzClient)
	onAssigned(context.Background(), franzClient, map[string][]int32{
		"track.tracking-event.v1": {0},
	})
	onRevoked(context.Background(), franzClient, map[string][]int32{})
	onLost(context.Background(), franzClient, map[string][]int32{
		"track.tracking-event.v1": {0},
	})
	if assignment, assignmentErr := consumer.Assignment(); assignmentErr != nil ||
		assignment.Epoch != 3 || !assignment.Lost {
		t.Fatalf("callback assignment = %#v, %v", assignment, assignmentErr)
	}
	balancers, ok := franzClient.OptValue(kgo.Balancers).([]kgo.GroupBalancer)
	if !ok || len(balancers) != 2 ||
		balancers[0].ProtocolName() != "sticky" ||
		balancers[1].ProtocolName() != "cooperative-sticky" {
		t.Fatalf("Balancers option = %#v", balancers)
	}
}

func TestConsumerTracksAssignmentLifecycle(t *testing.T) {
	t.Parallel()

	consumer := consumerWithBackend(
		&recordingConsumerBackend{}, 10, time.Second, time.Second,
	)
	assigned := map[string][]int32{"events": {2, 0}}
	consumer.onPartitionsAssigned(assigned)
	assigned["events"][0] = 99

	if got, err := consumer.Assignment(); err != nil || !reflect.DeepEqual(got, ConsumerAssignment{
		Epoch: 1,
		Partitions: []TopicPartition{
			{Topic: "events", Partition: 0},
			{Topic: "events", Partition: 2},
		},
	}) {
		t.Fatalf("Assignment() after assign = %#v, %v", got, err)
	}
	snapshot, err := consumer.Assignment()
	if err != nil {
		t.Fatalf("Assignment() snapshot error = %v", err)
	}
	snapshot.Partitions[0].Partition = 99
	if got, assignmentErr := consumer.Assignment(); assignmentErr != nil ||
		got.Partitions[0].Partition != 0 {
		t.Fatalf("Assignment() retained caller mutation: %#v, %v", got, assignmentErr)
	}

	consumer.onPartitionsAssigned(map[string][]int32{"events": {1}})
	consumer.onPartitionsRevoked(map[string][]int32{"events": {0}})
	if got, err := consumer.Assignment(); err != nil || !reflect.DeepEqual(got, ConsumerAssignment{
		Epoch: 3,
		Partitions: []TopicPartition{
			{Topic: "events", Partition: 1},
			{Topic: "events", Partition: 2},
		},
	}) {
		t.Fatalf("Assignment() after cooperative rebalance = %#v, %v", got, err)
	}

	consumer.onPartitionsRevoked(map[string][]int32{})
	consumer.onPartitionsLost(map[string][]int32{"events": {1, 2}})
	if got, err := consumer.Assignment(); err != nil || !reflect.DeepEqual(got, ConsumerAssignment{
		Epoch: 5,
		Lost:  true,
	}) {
		t.Fatalf("Assignment() after loss = %#v, %v", got, err)
	}

	consumer.onPartitionsAssigned(map[string][]int32{"events": {3}})
	if got, err := consumer.Assignment(); err != nil || !reflect.DeepEqual(got, ConsumerAssignment{
		Epoch:      6,
		Partitions: []TopicPartition{{Topic: "events", Partition: 3}},
	}) {
		t.Fatalf("Assignment() after recovery = %#v, %v", got, err)
	}
}

func TestConsumerRejectsInvalidOrOversizedAssignments(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		maximum    int
		assignment map[string][]int32
		want       error
	}{
		"oversized": {
			maximum:    1,
			assignment: map[string][]int32{"events": {0, 1}},
			want:       ErrTooManyAssignedPartitions,
		},
		"unsubscribed topic": {
			maximum:    2,
			assignment: map[string][]int32{"commands": {0}},
			want:       ErrInvalidAssignment,
		},
		"negative partition": {
			maximum:    2,
			assignment: map[string][]int32{"events": {-1}},
			want:       ErrInvalidAssignment,
		},
		"duplicate partition": {
			maximum:    2,
			assignment: map[string][]int32{"events": {0, 0}},
			want:       ErrInvalidAssignment,
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			consumer := consumerWithBackend(
				&recordingConsumerBackend{}, 10, time.Second, time.Second,
			)
			consumer.assignment.maximum = test.maximum
			consumer.onPartitionsAssigned(test.assignment)
			if _, err := consumer.Assignment(); !errors.Is(err, test.want) {
				t.Fatalf("Assignment() error = %v, want %v", err, test.want)
			}
			if _, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
				context.Context,
				ConsumedMessage,
			) error {
				t.Fatal("handler called for a rejected assignment")

				return nil
			})); !errors.Is(err, test.want) {
				t.Fatalf("RunOnce() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConsumerAssignmentErrorsRemainFailClosedUntilLoss(t *testing.T) {
	t.Parallel()

	consumer := consumerWithBackend(
		&recordingConsumerBackend{}, 10, time.Second, time.Second,
	)
	consumer.assignment.maximum = 1
	consumer.onPartitionsAssigned(map[string][]int32{"events": {0, 1}})
	consumer.onPartitionsAssigned(map[string][]int32{"events": {0}})
	consumer.onPartitionsRevoked(map[string][]int32{"events": {0}})
	if _, err := consumer.Assignment(); !errors.Is(err, ErrTooManyAssignedPartitions) {
		t.Fatalf("Assignment() persistent error = %v", err)
	}

	consumer.onPartitionsLost(nil)
	if assignment, err := consumer.Assignment(); err != nil || !assignment.Lost {
		t.Fatalf("Assignment() after loss = %#v, %v", assignment, err)
	}
}

func TestConsumerRejectsInvalidRevocationAndAccumulatedAssignment(t *testing.T) {
	t.Parallel()

	invalidRevocation := consumerWithBackend(
		&recordingConsumerBackend{}, 10, time.Second, time.Second,
	)
	invalidRevocation.onPartitionsAssigned(map[string][]int32{"events": {0}})
	invalidRevocation.onPartitionsRevoked(map[string][]int32{"commands": {0}})
	if _, err := invalidRevocation.Assignment(); !errors.Is(err, ErrInvalidAssignment) {
		t.Fatalf("Assignment() invalid revocation error = %v", err)
	}

	accumulated := consumerWithBackend(
		&recordingConsumerBackend{}, 10, time.Second, time.Second,
	)
	accumulated.assignment.maximum = 1
	accumulated.onPartitionsAssigned(map[string][]int32{"events": {0}})
	accumulated.onPartitionsAssigned(map[string][]int32{"events": {1}})
	if _, err := accumulated.Assignment(); !errors.Is(err, ErrTooManyAssignedPartitions) {
		t.Fatalf("Assignment() accumulated error = %v", err)
	}
}

func TestConsumerAssignmentSortsTopicsAndPartitions(t *testing.T) {
	t.Parallel()

	state := newConsumerAssignmentState(3, []string{"z-events", "a-events"})
	state.assigned(map[string][]int32{
		"z-events": {0},
		"a-events": {2, 1},
	})
	assignment, err := state.snapshot()
	if err != nil || !reflect.DeepEqual(assignment.Partitions, []TopicPartition{
		{Topic: "a-events", Partition: 1},
		{Topic: "a-events", Partition: 2},
		{Topic: "z-events", Partition: 0},
	}) {
		t.Fatalf("snapshot() = %#v, %v", assignment, err)
	}
}

func TestConsumerFencesSettlementAfterAssignmentEpochChanges(t *testing.T) {
	t.Parallel()

	first := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	second := &kgo.Record{Topic: "events", Partition: 1, Offset: 2}
	backend := &recordingConsumerBackend{fetches: recordFetches(first, second)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.onPartitionsAssigned(map[string][]int32{"events": {0, 1}})

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		_ context.Context,
		message ConsumedMessage,
	) error {
		if message.Partition == 0 {
			consumer.onPartitionsRevoked(map[string][]int32{"events": {0}})
		}

		return nil
	}))

	if !errors.Is(err, ErrConsumerOwnershipLost) {
		t.Fatalf("RunOnce() error = %v, want %v", err, ErrConsumerOwnershipLost)
	}
	if result != (PollResult{Polled: 2, Processed: 1}) || len(backend.committed) != 0 {
		t.Fatalf("result/backend = %#v/%#v", result, backend)
	}
}

func TestConsumerRejectsFetchedRecordWithoutCurrentOwnership(t *testing.T) {
	t.Parallel()

	record := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.onPartitionsAssigned(map[string][]int32{"events": {0}})
	backend.poll = func(context.Context, int) kgo.Fetches {
		consumer.onPartitionsRevoked(map[string][]int32{"events": {0}})

		return recordFetches(record)
	}

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called without current partition ownership")

		return nil
	}))
	if !errors.Is(err, ErrConsumerOwnershipLost) ||
		result != (PollResult{Polled: 1}) ||
		len(backend.committed) != 0 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestNewConsumerAppliesExplicitBalancePolicies(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		policy GroupBalancePolicy
		want   string
	}{
		"cooperative": {policy: BalanceCooperativeSticky, want: "cooperative-sticky"},
		"eager":       {policy: BalanceEagerSticky, want: "sticky"},
	} {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := validConsumerConfig()
			config.BalancePolicy = test.policy
			var franzClient *kgo.Client
			consumer, err := newConsumer(config, func(options ...kgo.Opt) (*kgo.Client, error) {
				client, clientErr := kgo.NewClient(options...)
				franzClient = client

				return client, clientErr
			})
			if err != nil {
				t.Fatalf("newConsumer() error = %v", err)
			}
			defer closeConsumerForTest(t, consumer)
			balancers, ok := franzClient.OptValue(kgo.Balancers).([]kgo.GroupBalancer)
			if !ok || len(balancers) != 1 || balancers[0].ProtocolName() != test.want {
				t.Fatalf("Balancers option = %#v", balancers)
			}
		})
	}
}

func TestNewConsumerValidatesIdentityTopicsAndOffsetPolicy(t *testing.T) {
	t.Parallel()

	manyTopics := make([]string, 65)
	for index := range manyTopics {
		manyTopics[index] = "topic-" + strings.Repeat("x", index+1)
	}

	tests := []struct {
		name   string
		change func(*ConsumerConfig)
		want   error
	}{
		{
			name:   "no brokers",
			change: func(config *ConsumerConfig) { config.Brokers = nil },
			want:   ErrBrokersRequired,
		},
		{
			name:   "invalid broker",
			change: func(config *ConsumerConfig) { config.Brokers = []string{" broker:9092 "} },
			want:   ErrInvalidBroker,
		},
		{
			name:   "blank client ID",
			change: func(config *ConsumerConfig) { config.ClientID = " " },
			want:   ErrClientIDRequired,
		},
		{
			name:   "blank group ID",
			change: func(config *ConsumerConfig) { config.GroupID = " " },
			want:   ErrGroupIDRequired,
		},
		{
			name:   "oversized group ID",
			change: func(config *ConsumerConfig) { config.GroupID = strings.Repeat("g", 256) },
			want:   ErrGroupIDTooLarge,
		},
		{
			name:   "invalid UTF-8 group ID",
			change: func(config *ConsumerConfig) { config.GroupID = string([]byte{0xff}) },
			want:   ErrInvalidGroupID,
		},
		{
			name:   "control character group ID",
			change: func(config *ConsumerConfig) { config.GroupID = "group\nid" },
			want:   ErrInvalidGroupID,
		},
		{
			name:   "blank instance ID",
			change: func(config *ConsumerConfig) { config.InstanceID = " " },
			want:   ErrInvalidInstanceID,
		},
		{
			name:   "oversized instance ID",
			change: func(config *ConsumerConfig) { config.InstanceID = strings.Repeat("i", 256) },
			want:   ErrInvalidInstanceID,
		},
		{
			name:   "invalid UTF-8 instance ID",
			change: func(config *ConsumerConfig) { config.InstanceID = string([]byte{0xff}) },
			want:   ErrInvalidInstanceID,
		},
		{
			name:   "NUL instance ID",
			change: func(config *ConsumerConfig) { config.InstanceID = "instance\x00id" },
			want:   ErrInvalidInstanceID,
		},
		{
			name:   "control character instance ID",
			change: func(config *ConsumerConfig) { config.InstanceID = "instance\tid" },
			want:   ErrInvalidInstanceID,
		},
		{
			name:   "blank rack",
			change: func(config *ConsumerConfig) { config.Rack = " " },
			want:   ErrInvalidRack,
		},
		{
			name:   "oversized rack",
			change: func(config *ConsumerConfig) { config.Rack = strings.Repeat("r", 256) },
			want:   ErrInvalidRack,
		},
		{
			name:   "invalid UTF-8 rack",
			change: func(config *ConsumerConfig) { config.Rack = string([]byte{0xff}) },
			want:   ErrInvalidRack,
		},
		{
			name:   "NUL rack",
			change: func(config *ConsumerConfig) { config.Rack = "rack\x00id" },
			want:   ErrInvalidRack,
		},
		{
			name:   "control character rack",
			change: func(config *ConsumerConfig) { config.Rack = "rack\nid" },
			want:   ErrInvalidRack,
		},
		{
			name:   "unknown balance policy",
			change: func(config *ConsumerConfig) { config.BalancePolicy = 255 },
			want:   ErrInvalidBalancePolicy,
		},
		{
			name: "unknown rebalance handler policy",
			change: func(config *ConsumerConfig) {
				config.RebalanceHandler = 255
			},
			want: ErrInvalidRebalanceHandlerPolicy,
		},
		{
			name:   "no topics",
			change: func(config *ConsumerConfig) { config.Topics = nil },
			want:   ErrTopicsRequired,
		},
		{
			name:   "too many topics",
			change: func(config *ConsumerConfig) { config.Topics = manyTopics },
			want:   ErrTooManyTopics,
		},
		{
			name:   "blank topic",
			change: func(config *ConsumerConfig) { config.Topics = []string{" "} },
			want:   ErrInvalidTopic,
		},
		{
			name:   "oversized topic",
			change: func(config *ConsumerConfig) { config.Topics = []string{strings.Repeat("t", 250)} },
			want:   ErrInvalidTopic,
		},
		{
			name:   "broker-invalid topic",
			change: func(config *ConsumerConfig) { config.Topics = []string{"events/commands"} },
			want:   ErrInvalidTopic,
		},
		{
			name: "topic exceeds record policy",
			change: func(config *ConsumerConfig) {
				config.Limits = DefaultMessageLimits()
				config.Limits.MaxTopicBytes = 5
			},
			want: ErrInvalidTopic,
		},
		{
			name:   "duplicate topic",
			change: func(config *ConsumerConfig) { config.Topics = []string{"events", "events"} },
			want:   ErrDuplicateTopic,
		},
		{
			name:   "missing offset policy",
			change: func(config *ConsumerConfig) { config.ResetOffset = 0 },
			want:   ErrInvalidOffsetPolicy,
		},
		{
			name:   "unknown offset policy",
			change: func(config *ConsumerConfig) { config.ResetOffset = 255 },
			want:   ErrInvalidOffsetPolicy,
		},
		{
			name: "insecure TLS",
			change: func(config *ConsumerConfig) {
				config.Security.TLS = &tls.Config{InsecureSkipVerify: true}
			},
			want: ErrInvalidSecurityConfig,
		},
		{
			name:   "partial record limits",
			change: func(config *ConsumerConfig) { config.Limits.MaxHeaders = 1 },
			want:   ErrInvalidMessageLimits,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validConsumerConfig()
			test.change(&config)

			consumer, err := NewConsumer(config)
			if consumer != nil {
				closeConsumerForTest(t, consumer)
				t.Fatal("NewConsumer() returned a consumer with invalid configuration")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("NewConsumer() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNewConsumerRejectsUnboundedConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*ConsumerConfig)
	}{
		{name: "negative poll records", change: func(config *ConsumerConfig) { config.MaxPollRecords = -1 }},
		{name: "excessive poll records", change: func(config *ConsumerConfig) { config.MaxPollRecords = 1_001 }},
		{name: "negative paused partitions", change: func(config *ConsumerConfig) { config.MaxPausedPartitions = -1 }},
		{name: "excessive paused partitions", change: func(config *ConsumerConfig) { config.MaxPausedPartitions = 1_025 }},
		{name: "negative assigned partitions", change: func(config *ConsumerConfig) { config.MaxAssignedPartitions = -1 }},
		{name: "excessive assigned partitions", change: func(config *ConsumerConfig) { config.MaxAssignedPartitions = 65_537 }},
		{name: "negative concurrent fetches", change: func(config *ConsumerConfig) { config.MaxConcurrentFetches = -1 }},
		{name: "excessive concurrent fetches", change: func(config *ConsumerConfig) { config.MaxConcurrentFetches = 65 }},
		{name: "negative fetch bytes", change: func(config *ConsumerConfig) { config.FetchMaxBytes = -1 }},
		{name: "excessive fetch bytes", change: func(config *ConsumerConfig) { config.FetchMaxBytes = 101 << 20 }},
		{name: "small partition fetch bytes", change: func(config *ConsumerConfig) { config.FetchMaxPartitionBytes = 1<<20 - 1 }},
		{name: "partition fetch exceeds aggregate", change: func(config *ConsumerConfig) {
			config.FetchMaxBytes = 2 << 20
			config.FetchMaxPartitionBytes = 3 << 20
		}},
		{name: "negative fetch wait", change: func(config *ConsumerConfig) { config.FetchMaxWait = -1 }},
		{name: "excessive fetch wait", change: func(config *ConsumerConfig) { config.FetchMaxWait = 31 * time.Second }},
		{name: "short session timeout", change: func(config *ConsumerConfig) { config.SessionTimeout = 999 * time.Millisecond }},
		{name: "excessive session timeout", change: func(config *ConsumerConfig) { config.SessionTimeout = 6*time.Minute + time.Nanosecond }},
		{name: "short rebalance timeout", change: func(config *ConsumerConfig) { config.RebalanceTimeout = 999 * time.Millisecond }},
		{name: "excessive rebalance timeout", change: func(config *ConsumerConfig) { config.RebalanceTimeout = 11 * time.Minute }},
		{name: "short heartbeat interval", change: func(config *ConsumerConfig) { config.HeartbeatInterval = 99 * time.Millisecond }},
		{name: "heartbeat exceeds session", change: func(config *ConsumerConfig) {
			config.SessionTimeout = time.Second
			config.HeartbeatInterval = 2 * time.Second
		}},
		{name: "short handler timeout", change: func(config *ConsumerConfig) { config.HandlerTimeout = 999 * time.Millisecond }},
		{name: "excessive handler timeout", change: func(config *ConsumerConfig) { config.HandlerTimeout = 31 * time.Minute }},
		{name: "short commit timeout", change: func(config *ConsumerConfig) { config.CommitTimeout = 99 * time.Millisecond }},
		{name: "excessive commit timeout", change: func(config *ConsumerConfig) { config.CommitTimeout = 3 * time.Minute }},
		{name: "rebalance cannot contain handler and commit", change: func(config *ConsumerConfig) {
			config.HeartbeatInterval = time.Second
			config.HandlerTimeout = 4 * time.Second
			config.CommitTimeout = 5 * time.Second
			config.RebalanceTimeout = 10 * time.Second
		}},
		{name: "short shutdown timeout", change: func(config *ConsumerConfig) { config.ShutdownTimeout = 99 * time.Millisecond }},
		{name: "excessive shutdown timeout", change: func(config *ConsumerConfig) { config.ShutdownTimeout = 16 * time.Minute }},
		{name: "short dial timeout", change: func(config *ConsumerConfig) { config.DialTimeout = 99 * time.Millisecond }},
		{name: "excessive dial timeout", change: func(config *ConsumerConfig) { config.DialTimeout = 3 * time.Minute }},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validConsumerConfig()
			test.change(&config)

			consumer, err := NewConsumer(config)
			if consumer != nil {
				closeConsumerForTest(t, consumer)
				t.Fatal("NewConsumer() returned a consumer with invalid bounded configuration")
			}
			if !errors.Is(err, ErrInvalidConsumerConfig) {
				t.Fatalf("NewConsumer() error = %v, want %v", err, ErrInvalidConsumerConfig)
			}
		})
	}
}

func TestConsumerRunOnceProcessesThenCommitsBoundedPoll(t *testing.T) {
	t.Parallel()

	records := []*kgo.Record{
		{
			Topic: "events", Partition: 1, Offset: 7, Key: []byte("first"),
			Value: []byte("one"), Headers: []kgo.RecordHeader{{Key: "trace", Value: []byte("abc")}},
		},
		{Topic: "events", Partition: 1, Offset: 8, Key: []byte("second"), Value: []byte("two")},
	}
	backend := &recordingConsumerBackend{fetches: recordFetches(records...)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	var handled []ConsumedMessage

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		_ context.Context,
		message ConsumedMessage,
	) error {
		handled = append(handled, message)

		return nil
	}))

	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result != (PollResult{Polled: 2, Processed: 2, Committed: 2}) {
		t.Fatalf("RunOnce() result = %#v", result)
	}
	if len(handled) != 2 ||
		handled[0].Topic != "events" ||
		handled[0].Partition != 1 ||
		handled[0].Offset != 7 ||
		string(handled[0].Key) != "first" ||
		string(handled[0].Value) != "one" ||
		len(handled[0].Headers) != 1 ||
		handled[0].Headers[0].Key != "trace" ||
		string(handled[0].Headers[0].Value) != "abc" ||
		string(handled[1].Key) != "second" {
		t.Fatalf("handled messages = %#v", handled)
	}
	if len(backend.committed) != 1 ||
		backend.committed[0] != records[1] ||
		backend.allowed != 1 ||
		backend.lastPollLimit != 10 {
		t.Fatalf("backend state = %#v", backend)
	}
}

func TestConsumerCancelsActiveHandlerForBlockedRebalance(t *testing.T) {
	t.Parallel()

	first := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	backend := &recordingConsumerBackend{fetches: recordFetches(
		first,
		&kgo.Record{Topic: "events", Partition: 0, Offset: 2},
		&kgo.Record{Topic: "events", Partition: 0, Offset: 3},
	)}
	consumer := consumerWithBackend(backend, 10, time.Minute, time.Second)
	handlerErr := errors.New("handler stopped after cancellation")
	handlerStarted := make(chan struct{})
	runDone := make(chan struct {
		result PollResult
		err    error
	}, 1)
	go func() {
		result, err := consumer.RunOnce(
			context.Background(),
			HandlerFunc(func(ctx context.Context, message ConsumedMessage) error {
				if message.Offset == 1 {
					return nil
				}
				if message.Offset != 2 {
					t.Errorf("handler received offset %d after rebalance signal", message.Offset)
				}
				close(handlerStarted)
				<-ctx.Done()

				return handlerErr
			}),
		)
		runDone <- struct {
			result PollResult
			err    error
		}{result: result, err: err}
	}()
	<-handlerStarted
	consumer.onRebalanceBlocked()
	got := <-runDone

	if !errors.Is(got.err, ErrConsumerRebalance) || !errors.Is(got.err, handlerErr) ||
		got.result != (PollResult{Polled: 3, Processed: 1, Committed: 1}) ||
		len(backend.committed) != 1 || backend.committed[0] != first ||
		backend.allowed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", got.result, got.err, backend)
	}
}

func TestConsumerDrainsActiveHandlerForBlockedRebalance(t *testing.T) {
	t.Parallel()

	first := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	backend := &recordingConsumerBackend{fetches: recordFetches(
		first,
		&kgo.Record{Topic: "events", Partition: 0, Offset: 2},
	)}
	consumer := consumerWithBackend(backend, 10, time.Minute, time.Second)
	consumer.rebalance = newConsumerRebalanceState(RebalanceDrainHandler)
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	runDone := make(chan struct {
		result PollResult
		err    error
	}, 1)
	go func() {
		result, err := consumer.RunOnce(
			context.Background(),
			HandlerFunc(func(_ context.Context, message ConsumedMessage) error {
				if message.Offset != 1 {
					t.Errorf("handler received offset %d after rebalance signal", message.Offset)
				}
				close(handlerStarted)
				<-releaseHandler

				return nil
			}),
		)
		runDone <- struct {
			result PollResult
			err    error
		}{result: result, err: err}
	}()
	<-handlerStarted
	consumer.onRebalanceBlocked()
	close(releaseHandler)
	got := <-runDone

	if got.err != nil ||
		got.result != (PollResult{Polled: 2, Processed: 1, Committed: 1}) ||
		len(backend.committed) != 1 || backend.committed[0] != first ||
		backend.allowed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", got.result, got.err, backend)
	}
}

func TestConsumerRebalanceSignalBeforeHandlerContextStopsAdmission(t *testing.T) {
	t.Parallel()

	rebalance := newConsumerRebalanceState(RebalanceCancelHandler)
	rebalance.beginPoll()
	rebalance.blocked()
	handlerCtx, cleanup, admitted := rebalance.handlerContext(
		context.Background(),
		time.Minute,
	)
	defer rebalance.endPoll()

	if admitted || handlerCtx != nil || cleanup != nil {
		t.Fatalf(
			"handler admission = %t, context nil = %t, cleanup nil = %t",
			admitted,
			handlerCtx == nil,
			cleanup == nil,
		)
	}
}

func TestConsumerStopsAdmissionWhenRebalanceSignalPrecedesPollReturn(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	backend.poll = func(context.Context, int) kgo.Fetches {
		consumer.onRebalanceBlocked()

		return recordFetches(&kgo.Record{Topic: "events", Partition: 0, Offset: 1})
	}

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called after rebalance signal")

		return nil
	}))
	if err != nil || result != (PollResult{Polled: 1}) ||
		len(backend.committed) != 0 || backend.allowed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunOnceCommitsOnlyContiguousPartitionSuccess(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("projection failed")
	partitionZeroFirst := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	partitionOneFirst := &kgo.Record{Topic: "events", Partition: 1, Offset: 4}
	partitionOneFailed := &kgo.Record{Topic: "events", Partition: 1, Offset: 5}
	partitionZeroSecond := &kgo.Record{Topic: "events", Partition: 0, Offset: 2}
	partitionOneSkipped := &kgo.Record{Topic: "events", Partition: 1, Offset: 6}
	backend := &recordingConsumerBackend{fetches: kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic: "events",
			Partitions: []kgo.FetchPartition{
				{Partition: 1, Records: []*kgo.Record{
					partitionOneFirst,
					partitionOneFailed,
					partitionOneSkipped,
				}},
				{Partition: 0, Records: []*kgo.Record{partitionZeroFirst, partitionZeroSecond}},
			},
		}},
	}}}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	var handled []int64

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		_ context.Context,
		message ConsumedMessage,
	) error {
		handled = append(handled, message.Offset)
		if message.Partition == 1 && message.Offset == 5 {
			return handlerErr
		}

		return nil
	}))

	if !errors.Is(err, handlerErr) {
		t.Fatalf("RunOnce() error = %v, want %v", err, handlerErr)
	}
	if result != (PollResult{Polled: 5, Processed: 3, Committed: 3}) ||
		!reflect.DeepEqual(handled, []int64{4, 5, 1, 2}) ||
		len(backend.committed) != 2 ||
		backend.committed[0] != partitionOneFirst ||
		backend.committed[1] != partitionZeroSecond ||
		backend.allowed != 1 {
		t.Fatalf("result/backend = %#v/%#v", result, backend)
	}
}

func TestConsumerRunOnceRejectsFetchedRecordOutsideLimits(t *testing.T) {
	t.Parallel()

	partitionZeroFirst := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	partitionZeroInvalid := &kgo.Record{
		Topic: "events", Partition: 0, Offset: 2,
		Headers: []kgo.RecordHeader{{Key: "one"}, {Key: "two"}},
	}
	partitionZeroSkipped := &kgo.Record{Topic: "events", Partition: 0, Offset: 3}
	partitionOne := &kgo.Record{Topic: "events", Partition: 1, Offset: 4}
	backend := &recordingConsumerBackend{fetches: recordFetches(
		partitionZeroFirst,
		partitionZeroInvalid,
		partitionZeroSkipped,
		partitionOne,
	)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.limits.MaxHeaders = 1
	var handled []int64

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		_ context.Context,
		message ConsumedMessage,
	) error {
		handled = append(handled, message.Offset)

		return nil
	}))

	if !errors.Is(err, ErrTooManyHeaders) ||
		result != (PollResult{Polled: 4, Processed: 2, Committed: 2}) ||
		!reflect.DeepEqual(handled, []int64{1, 4}) ||
		len(backend.committed) != 2 ||
		backend.committed[0] != partitionZeroFirst ||
		backend.committed[1] != partitionOne {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunOnceRejectsFetchedBytesBeforeHandler(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{fetches: recordFetches(&kgo.Record{
		Topic: "events", Partition: 0, Offset: 1, Key: []byte("oversized"),
	})}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.limits.MaxKeyBytes = 1

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called for fetched record outside limits")

		return nil
	}))

	if !errors.Is(err, ErrKeyTooLarge) ||
		result != (PollResult{Polled: 1}) ||
		len(backend.committed) != 0 || backend.allowed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunOnceRejectsMissingHandler(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)

	result, err := consumer.RunOnce(context.Background(), nil)

	if !errors.Is(err, ErrHandlerRequired) || result != (PollResult{}) ||
		backend.pollCalls != 0 || backend.allowed != 0 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerPauseResumePartitions(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	partitions := []TopicPartition{
		{Topic: "other", Partition: 2},
		{Topic: "events", Partition: 1},
	}
	consumer.subscribedTopics["other"] = struct{}{}

	if err := consumer.PausePartitions(partitions...); err != nil {
		t.Fatalf("PausePartitions() error = %v", err)
	}
	paused := consumer.PausedPartitions()
	if !reflect.DeepEqual(paused, []TopicPartition{
		{Topic: "events", Partition: 1},
		{Topic: "other", Partition: 2},
	}) {
		t.Fatalf("PausedPartitions() = %#v", paused)
	}
	paused[0] = TopicPartition{Topic: "mutated", Partition: 99}
	if got := consumer.PausedPartitions(); got[0] != (TopicPartition{Topic: "events", Partition: 1}) {
		t.Fatalf("mutated PausedPartitions() = %#v", got)
	}
	if !reflect.DeepEqual(backend.pauseCalls, []map[string][]int32{{
		"events": {1},
		"other":  {2},
	}}) {
		t.Fatalf("pause calls = %#v", backend.pauseCalls)
	}

	if err := consumer.PausePartitions(partitions[0]); err != nil {
		t.Fatalf("repeated PausePartitions() error = %v", err)
	}
	if err := consumer.ResumePartitions(partitions[1]); err != nil {
		t.Fatalf("ResumePartitions() error = %v", err)
	}
	if got := consumer.PausedPartitions(); !reflect.DeepEqual(got, []TopicPartition{
		{Topic: "other", Partition: 2},
	}) {
		t.Fatalf("resumed PausedPartitions() = %#v", got)
	}
	if !reflect.DeepEqual(backend.resumeCalls, []map[string][]int32{{
		"events": {1},
	}}) {
		t.Fatalf("resume calls = %#v", backend.resumeCalls)
	}
}

func TestConsumerPauseResumeRejectInvalidPartitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		partitions []TopicPartition
		want       error
	}{
		{name: "empty", want: ErrPausePartitionsRequired},
		{
			name:       "invalid topic",
			partitions: []TopicPartition{{Topic: "bad/topic", Partition: 0}},
			want:       ErrInvalidPausePartition,
		},
		{
			name:       "unsubscribed topic",
			partitions: []TopicPartition{{Topic: "commands", Partition: 0}},
			want:       ErrPauseTopicNotSubscribed,
		},
		{
			name:       "negative partition",
			partitions: []TopicPartition{{Topic: "events", Partition: -1}},
			want:       ErrInvalidPausePartition,
		},
		{
			name: "duplicate",
			partitions: []TopicPartition{
				{Topic: "events", Partition: 1},
				{Topic: "events", Partition: 1},
			},
			want: ErrDuplicatePausePartition,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingConsumerBackend{}
			consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
			if err := consumer.PausePartitions(test.partitions...); !errors.Is(err, test.want) {
				t.Fatalf("PausePartitions() error = %v, want %v", err, test.want)
			}
			if err := consumer.ResumePartitions(test.partitions...); !errors.Is(err, test.want) {
				t.Fatalf("ResumePartitions() error = %v, want %v", err, test.want)
			}
			if len(backend.pauseCalls) != 0 || len(backend.resumeCalls) != 0 {
				t.Fatalf("backend calls = %#v/%#v", backend.pauseCalls, backend.resumeCalls)
			}
		})
	}
}

func TestConsumerPauseBoundsAccumulatedPartitions(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.maxPausedPartitions = 2
	if err := consumer.PausePartitions(
		TopicPartition{Topic: "events", Partition: 0},
		TopicPartition{Topic: "events", Partition: 1},
	); err != nil {
		t.Fatalf("first PausePartitions() error = %v", err)
	}
	if err := consumer.PausePartitions(
		TopicPartition{Topic: "events", Partition: 2},
	); !errors.Is(err, ErrTooManyPausedPartitions) {
		t.Fatalf("bounded PausePartitions() error = %v, want %v", err, ErrTooManyPausedPartitions)
	}
	if len(backend.pauseCalls) != 1 || len(consumer.PausedPartitions()) != 2 {
		t.Fatalf("backend/state = %#v/%#v", backend.pauseCalls, consumer.PausedPartitions())
	}
}

func TestConsumerPauseRejectsOversizedRequestBeforeInspectingPartitions(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.maxPausedPartitions = 2
	invalid := []TopicPartition{
		{Topic: "bad/topic", Partition: -1},
		{Topic: "events", Partition: 1},
		{Topic: "events", Partition: 2},
	}

	if err := consumer.PausePartitions(invalid...); !errors.Is(err, ErrTooManyPausedPartitions) {
		t.Fatalf("PausePartitions() error = %v, want %v", err, ErrTooManyPausedPartitions)
	}
	if err := consumer.ResumePartitions(invalid...); !errors.Is(err, ErrTooManyPausedPartitions) {
		t.Fatalf("ResumePartitions() error = %v, want %v", err, ErrTooManyPausedPartitions)
	}
	if len(backend.pauseCalls) != 0 || len(backend.resumeCalls) != 0 {
		t.Fatalf("backend calls = %#v/%#v", backend.pauseCalls, backend.resumeCalls)
	}
}

func TestConsumerPauseRejectsLifecycleStates(t *testing.T) {
	t.Parallel()

	partition := TopicPartition{Topic: "events", Partition: 0}
	closingBackend := &recordingConsumerBackend{leaveErr: errors.New("leave failed")}
	closingConsumer := consumerWithBackend(closingBackend, 10, time.Second, time.Second)
	if err := closingConsumer.Shutdown(context.Background()); err == nil {
		t.Fatal("Shutdown() error = nil")
	}
	if err := closingConsumer.PausePartitions(partition); !errors.Is(err, ErrConsumerClosing) {
		t.Fatalf("closing PausePartitions() error = %v, want %v", err, ErrConsumerClosing)
	}
	if err := closingConsumer.ResumePartitions(partition); !errors.Is(err, ErrConsumerClosing) {
		t.Fatalf("closing ResumePartitions() error = %v, want %v", err, ErrConsumerClosing)
	}

	closedBackend := &recordingConsumerBackend{}
	closedConsumer := consumerWithBackend(closedBackend, 10, time.Second, time.Second)
	if err := closedConsumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := closedConsumer.PausePartitions(partition); !errors.Is(err, ErrConsumerClosed) {
		t.Fatalf("closed PausePartitions() error = %v, want %v", err, ErrConsumerClosed)
	}
	if err := closedConsumer.ResumePartitions(partition); !errors.Is(err, ErrConsumerClosed) {
		t.Fatalf("closed ResumePartitions() error = %v, want %v", err, ErrConsumerClosed)
	}
}

func TestConsumerShutdownFencesRunsAndCanResumeAfterTimeout(t *testing.T) {
	t.Parallel()

	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	backend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	runDone := make(chan error, 1)
	go func() {
		_, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
			context.Context,
			ConsumedMessage,
		) error {
			close(handlerEntered)
			<-releaseHandler

			return nil
		}))
		runDone <- err
	}()
	<-handlerEntered

	if err := consumer.Run(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("concurrent handler called")

		return nil
	})); !errors.Is(err, ErrConsumerBusy) {
		t.Fatalf("concurrent Run() error = %v, want %v", err, ErrConsumerBusy)
	}

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := consumer.Shutdown(shutdownCtx)
	if !errors.Is(err, ErrConsumerShutdownIncomplete) ||
		!errors.Is(err, context.Canceled) ||
		backend.leaveCalls != 0 || backend.closed != 0 {
		t.Fatalf("Shutdown() error/backend = %v/%#v", err, backend)
	}
	if _, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called after shutdown fenced new runs")

		return nil
	})); !errors.Is(err, ErrConsumerClosing) {
		t.Fatalf("fenced RunOnce() error = %v, want %v", err, ErrConsumerClosing)
	}

	close(releaseHandler)
	if err := <-runDone; err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if backend.leaveCalls != 1 || backend.closed != 1 {
		t.Fatalf("closed backend = %#v", backend)
	}
	if _, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called after close")

		return nil
	})); !errors.Is(err, ErrConsumerClosed) {
		t.Fatalf("closed RunOnce() error = %v, want %v", err, ErrConsumerClosed)
	}
}

func TestConsumerShutdownRejectsConcurrentShutdown(t *testing.T) {
	t.Parallel()

	backend := &blockingLeaveConsumerBackend{
		recordingConsumerBackend: &recordingConsumerBackend{},
		leaveStarted:             make(chan struct{}),
		releaseLeave:             make(chan struct{}),
	}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- consumer.Shutdown(context.Background())
	}()
	<-backend.leaveStarted

	if err := consumer.Shutdown(context.Background()); !errors.Is(err, ErrConsumerShutdownActive) {
		t.Fatalf("concurrent Shutdown() error = %v, want %v", err, ErrConsumerShutdownActive)
	}
	close(backend.releaseLeave)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("first Shutdown() error = %v", err)
	}
}

func TestConsumerShutdownCanRetryLeaveFailure(t *testing.T) {
	t.Parallel()

	leaveErr := errors.New("leave failed")
	backend := &recordingConsumerBackend{leaveErr: leaveErr}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)

	err := consumer.Shutdown(context.Background())
	if !errors.Is(err, ErrConsumerShutdownIncomplete) ||
		!errors.Is(err, leaveErr) || backend.leaveCalls != 1 || backend.closed != 0 {
		t.Fatalf("first Shutdown() error/backend = %v/%#v", err, backend)
	}
	backend.leaveErr = nil
	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if backend.leaveCalls != 2 || backend.closed != 1 {
		t.Fatalf("retried backend = %#v", backend)
	}
}

func TestConsumerShutdownPreservesStaticMembership(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.staticMembership = true

	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if backend.leaveCalls != 0 || backend.closed != 1 {
		t.Fatalf("backend = %#v", backend)
	}
}

func TestConsumerCloseUsesConfiguredShutdown(t *testing.T) {
	t.Parallel()

	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	backend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.shutdownTimeout = time.Nanosecond
	runDone := make(chan error, 1)
	go func() {
		_, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
			context.Context,
			ConsumedMessage,
		) error {
			close(handlerEntered)
			<-releaseHandler

			return nil
		}))
		runDone <- err
	}()
	<-handlerEntered

	err := consumer.Close()
	if !errors.Is(err, ErrConsumerShutdownIncomplete) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		backend.leaveCalls != 0 || backend.closed != 0 {
		t.Fatalf("timed Close() error/backend = %v/%#v", err, backend)
	}
	close(releaseHandler)
	if err := <-runDone; err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if backend.leaveCalls != 1 || backend.closed != 1 {
		t.Fatalf("backend = %#v", backend)
	}
}

func TestConsumerRunOnceReportsFetchAndCommitFailures(t *testing.T) {
	t.Parallel()

	fetchErr := errors.New("fetch failed")
	fetchBackend := &recordingConsumerBackend{fetches: kgo.NewErrFetch(fetchErr)}
	fetchConsumer := consumerWithBackend(fetchBackend, 10, time.Second, time.Second)
	result, err := fetchConsumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called after fetch failure")

		return nil
	}))
	if !errors.Is(err, fetchErr) || result != (PollResult{}) ||
		len(fetchBackend.committed) != 0 || fetchBackend.allowed != 1 {
		t.Fatalf("fetch result/error/backend = %#v/%v/%#v", result, err, fetchBackend)
	}

	commitErr := errors.New("commit failed")
	commitBackend := &recordingConsumerBackend{
		fetches:   recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
		commitErr: commitErr,
	}
	commitConsumer := consumerWithBackend(commitBackend, 10, time.Second, time.Second)
	result, err = commitConsumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		return nil
	}))
	if err != commitErr ||
		result != (PollResult{Polled: 1, Processed: 1}) ||
		commitBackend.allowed != 1 {
		t.Fatalf("commit result/error/backend = %#v/%v/%#v", result, err, commitBackend)
	}

	handlerErr := errors.New("handler failed")
	combinedBackend := &recordingConsumerBackend{
		fetches: recordFetches(
			&kgo.Record{Topic: "events", Offset: 1},
			&kgo.Record{Topic: "events", Offset: 2},
		),
		commitErr: commitErr,
	}
	combinedConsumer := consumerWithBackend(combinedBackend, 10, time.Second, time.Second)
	result, err = combinedConsumer.RunOnce(context.Background(), HandlerFunc(func(
		_ context.Context,
		message ConsumedMessage,
	) error {
		if message.Offset == 2 {
			return handlerErr
		}

		return nil
	}))
	if !errors.Is(err, handlerErr) || !errors.Is(err, commitErr) ||
		result != (PollResult{Polled: 2, Processed: 1}) ||
		len(combinedBackend.committed) != 1 ||
		combinedBackend.committed[0].Offset != 1 {
		t.Fatalf("combined result/error/backend = %#v/%v/%#v", result, err, combinedBackend)
	}
}

func TestConsumerRunOnceContainsHandlerPanicAndEnforcesTimeout(t *testing.T) {
	t.Parallel()

	panicBackend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	panicConsumer := consumerWithBackend(panicBackend, 10, time.Second, time.Second)
	result, err := panicConsumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		panic("payload and internal state")
	}))
	if !errors.Is(err, ErrHandlerPanic) ||
		strings.Contains(err.Error(), "payload") ||
		result != (PollResult{Polled: 1}) ||
		len(panicBackend.committed) != 0 ||
		panicBackend.allowed != 1 {
		t.Fatalf("panic result/error/backend = %#v/%v/%#v", result, err, panicBackend)
	}

	timeoutBackend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	timeoutConsumer := consumerWithBackend(timeoutBackend, 10, time.Nanosecond, time.Second)
	result, err = timeoutConsumer.RunOnce(context.Background(), HandlerFunc(func(
		ctx context.Context,
		_ ConsumedMessage,
	) error {
		<-ctx.Done()

		return ctx.Err()
	}))
	if !errors.Is(err, context.DeadlineExceeded) ||
		result != (PollResult{Polled: 1}) ||
		len(timeoutBackend.committed) != 0 ||
		timeoutBackend.allowed != 1 {
		t.Fatalf("timeout result/error/backend = %#v/%v/%#v", result, err, timeoutBackend)
	}
}

func TestConsumerRunOnceHandlesEmptyPollAndClose(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)

	result, err := consumer.RunOnce(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called for empty poll")

		return nil
	}))
	if closeErr := consumer.Close(); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}

	if err != nil || result != (PollResult{}) || backend.allowed != 1 || backend.closed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunCancellationLeavesActiveRecordUnsettled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	backend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	backend.poll = func(ctx context.Context, _ int) kgo.Fetches {
		if backend.pollCalls == 1 {
			return backend.fetches
		}

		return kgo.NewErrFetch(ctx.Err())
	}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)

	err := consumer.Run(ctx, HandlerFunc(func(context.Context, ConsumedMessage) error {
		cancel()

		return nil
	}))

	if err != nil || len(backend.committed) != 0 || backend.allowed != 1 {
		t.Fatalf("Run() error/backend = %v/%#v", err, backend)
	}
}

func TestConsumerRunReturnsProcessingFailure(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("projection failed")
	backend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)

	err := consumer.Run(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		return handlerErr
	}))

	if !errors.Is(err, handlerErr) || len(backend.committed) != 0 || backend.allowed != 1 {
		t.Fatalf("Run() error/backend = %v/%#v", err, backend)
	}
}

func TestConsumerRunRejectsMissingHandlerAndStopsOnCanceledPoll(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	if err := consumer.Run(context.Background(), nil); !errors.Is(err, ErrHandlerRequired) {
		t.Fatalf("Run() missing handler error = %v, want %v", err, ErrHandlerRequired)
	}

	ctx, cancel := context.WithCancel(context.Background())
	backend.poll = func(context.Context, int) kgo.Fetches {
		cancel()

		return kgo.NewErrFetch(context.Canceled)
	}
	if err := consumer.Run(ctx, HandlerFunc(func(context.Context, ConsumedMessage) error {
		t.Fatal("handler called after canceled poll")

		return nil
	})); err != nil {
		t.Fatalf("Run() canceled poll error = %v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	emptyBackend := &recordingConsumerBackend{
		poll: func(context.Context, int) kgo.Fetches {
			cancel()

			return nil
		},
	}
	emptyConsumer := consumerWithBackend(
		emptyBackend,
		10,
		time.Second,
		time.Second,
	)
	if err := emptyConsumer.Run(
		ctx,
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			t.Fatal("handler called after canceled empty poll")

			return nil
		}),
	); err != nil {
		t.Fatalf("Run() canceled empty poll error = %v", err)
	}
	if emptyBackend.pollCalls != 1 || emptyBackend.allowed != 1 {
		t.Fatalf("canceled empty poll backend = %#v", emptyBackend)
	}
}

func validConsumerConfig() ConsumerConfig {
	return ConsumerConfig{
		Brokers:     []string{"broker.internal:9092"},
		ClientID:    "track-projection",
		GroupID:     "track-projection-v1",
		Topics:      []string{"track.tracking-event.v1"},
		ResetOffset: OffsetEarliest,
	}
}

func closeConsumerForTest(t *testing.T, consumer *Consumer) {
	t.Helper()
	if err := consumer.Close(); err != nil {
		t.Errorf("Consumer.Close() error = %v", err)
	}
}

func consumerWithBackend(
	backend consumerBackend,
	maxPollRecords int,
	handlerTimeout time.Duration,
	commitTimeout time.Duration,
) *Consumer {
	return &Consumer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		maxPollRecords:        maxPollRecords,
		maxConcurrentHandlers: 1,
		assignment: newConsumerAssignmentState(
			1_024,
			[]string{"events"},
		),
		rebalance:           newConsumerRebalanceState(RebalanceCancelHandler),
		handlerTimeout:      handlerTimeout,
		commitTimeout:       commitTimeout,
		shutdownTimeout:     time.Second,
		maxPausedPartitions: 1_024,
		subscribedTopics: map[string]struct{}{
			"events": {},
		},
		pausedPartitions: make(map[TopicPartition]struct{}),
	}
}

func recordFetches(records ...*kgo.Record) kgo.Fetches {
	return kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic: "events",
			Partitions: []kgo.FetchPartition{{
				Partition: 1,
				Records:   records,
			}},
		}},
	}}
}

type recordingConsumerBackend struct {
	fetches       kgo.Fetches
	commitErr     error
	committed     []*kgo.Record
	lastPollLimit int
	pollCalls     int
	allowed       int
	closed        int
	leaveCalls    int
	leaveErr      error
	pauseCalls    []map[string][]int32
	resumeCalls   []map[string][]int32
	poll          func(context.Context, int) kgo.Fetches
}

func (backend *recordingConsumerBackend) PollRecords(
	ctx context.Context,
	maxRecords int,
) kgo.Fetches {
	backend.pollCalls++
	backend.lastPollLimit = maxRecords
	if backend.poll != nil {
		return backend.poll(ctx, maxRecords)
	}

	return backend.fetches
}

func (backend *recordingConsumerBackend) CommitRecords(
	_ context.Context,
	records ...*kgo.Record,
) error {
	backend.committed = append(backend.committed, records...)

	return backend.commitErr
}

func (backend *recordingConsumerBackend) AllowRebalance() {
	backend.allowed++
}

func (backend *recordingConsumerBackend) LeaveGroupContext(context.Context) error {
	backend.leaveCalls++

	return backend.leaveErr
}

func (backend *recordingConsumerBackend) PauseFetchPartitions(
	partitions map[string][]int32,
) map[string][]int32 {
	backend.pauseCalls = append(backend.pauseCalls, clonePartitionMap(partitions))

	return nil
}

func (backend *recordingConsumerBackend) ResumeFetchPartitions(
	partitions map[string][]int32,
) {
	backend.resumeCalls = append(backend.resumeCalls, clonePartitionMap(partitions))
}

func clonePartitionMap(partitions map[string][]int32) map[string][]int32 {
	cloned := make(map[string][]int32, len(partitions))
	for topic, topicPartitions := range partitions {
		cloned[topic] = append([]int32(nil), topicPartitions...)
	}

	return cloned
}

func (backend *recordingConsumerBackend) Close() {
	backend.closed++
}

type blockingLeaveConsumerBackend struct {
	*recordingConsumerBackend
	leaveStarted chan struct{}
	releaseLeave chan struct{}
}

func (backend *blockingLeaveConsumerBackend) LeaveGroupContext(context.Context) error {
	close(backend.leaveStarted)
	<-backend.releaseLeave

	return nil
}
