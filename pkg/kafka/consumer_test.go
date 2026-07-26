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
		config.BalancePolicy != BalanceCooperativeSticky ||
		config.MaxConcurrentFetches != 4 ||
		config.FetchMaxBytes != 50<<20 ||
		config.FetchMaxPartitionBytes != 1<<20 ||
		config.FetchMaxWait != 500*time.Millisecond ||
		config.SessionTimeout != 45*time.Second ||
		config.RebalanceTimeout != 60*time.Second ||
		config.HeartbeatInterval != 3*time.Second ||
		config.HandlerTimeout != 30*time.Second ||
		config.CommitTimeout != 10*time.Second ||
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
	consumer.Close()

	factoryErr := errors.New("client construction failed")
	latestConfig := validConsumerConfig()
	latestConfig.ResetOffset = OffsetLatest
	consumer, err = newConsumer(latestConfig, func(...kgo.Opt) (*kgo.Client, error) {
		return nil, factoryErr
	})
	if consumer != nil {
		consumer.Close()
		t.Fatal("newConsumer() returned a consumer after client factory failure")
	}
	if !errors.Is(err, factoryErr) {
		t.Fatalf("newConsumer() error = %v, want %v", err, factoryErr)
	}
}

func TestNewConsumerAppliesConsumerPolicyOptions(t *testing.T) {
	t.Parallel()

	config := validConsumerConfig()
	config.InstanceID = "track-processor-01"
	config.Rack = "eu-west-1a"
	config.BalancePolicy = BalanceEagerToCooperative
	config.MaxConcurrentFetches = 3
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
	defer consumer.Close()
	if got := franzClient.OptValue(kgo.FetchMaxPartitionBytes); got != int32(2<<20) {
		t.Fatalf("FetchMaxPartitionBytes option = %#v", got)
	}
	if got := franzClient.OptValue(kgo.MaxConcurrentFetches); got != 3 {
		t.Fatalf("MaxConcurrentFetches option = %#v", got)
	}
	if got := franzClient.OptValue(kgo.InstanceID); got != "track-processor-01" {
		t.Fatalf("InstanceID option = %#v", got)
	}
	if got := franzClient.OptValue(kgo.Rack); got != "eu-west-1a" {
		t.Fatalf("Rack option = %#v", got)
	}
	balancers, ok := franzClient.OptValue(kgo.Balancers).([]kgo.GroupBalancer)
	if !ok || len(balancers) != 2 ||
		balancers[0].ProtocolName() != "sticky" ||
		balancers[1].ProtocolName() != "cooperative-sticky" {
		t.Fatalf("Balancers option = %#v", balancers)
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
			defer consumer.Close()
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
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validConsumerConfig()
			test.change(&config)

			consumer, err := NewConsumer(config)
			if consumer != nil {
				consumer.Close()
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
				consumer.Close()
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
	consumer.Close()

	if err != nil || result != (PollResult{}) || backend.allowed != 1 || backend.closed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunProcessesUntilCancellation(t *testing.T) {
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

	if err != nil || len(backend.committed) != 1 || backend.allowed != 1 {
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

func consumerWithBackend(
	backend consumerBackend,
	maxPollRecords int,
	handlerTimeout time.Duration,
	commitTimeout time.Duration,
) *Consumer {
	return &Consumer{
		client:         backend,
		maxPollRecords: maxPollRecords,
		handlerTimeout: handlerTimeout,
		commitTimeout:  commitTimeout,
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

func (backend *recordingConsumerBackend) Close() {
	backend.closed++
}
