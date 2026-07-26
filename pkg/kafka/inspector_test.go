package kafka

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestInspectorReturnsSortedTopicAndLagState(t *testing.T) {
	t.Parallel()

	backend := &recordingInspectorBackend{
		topics: kadm.TopicDetails{
			"events": {
				Topic: "events",
				Partitions: kadm.PartitionDetails{
					1: {
						Topic: "events", Partition: 1, Leader: 4,
						Replicas: []int32{4, 5, 6}, ISR: []int32{4, 5},
						OfflineReplicas: []int32{6},
					},
					0: {
						Topic: "events", Partition: 0, Leader: 1,
						Replicas: []int32{1, 2, 3}, ISR: []int32{1, 2, 3},
					},
				},
			},
		},
		lags: kadm.DescribedGroupLags{
			"track-consumer": {
				Group: "track-consumer",
				State: "Stable",
				Lag: kadm.GroupLag{
					"events": {
						0: {
							Topic: "events", Partition: 0,
							Commit: kadm.Offset{
								Topic: "events", Partition: 0, At: 10,
							},
							Start: kadm.ListedOffset{
								Topic: "events", Partition: 0, Offset: 0,
							},
							End: kadm.ListedOffset{
								Topic: "events", Partition: 0, Offset: 15,
							},
							Lag: 5,
						},
					},
				},
			},
		},
	}
	inspector := &Inspector{admin: backend, client: backend}

	topics, err := inspector.Topics(context.Background(), "events")
	if err != nil {
		t.Fatalf("Topics() error = %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "events" ||
		len(topics[0].Partitions) != 2 ||
		topics[0].Partitions[0].Partition != 0 ||
		topics[0].Partitions[1].ReplicationFactor != 3 ||
		topics[0].Partitions[1].InSyncReplicas != 2 ||
		topics[0].Partitions[1].OfflineReplicas != 1 {
		t.Fatalf("Topics() = %#v", topics)
	}

	lags, err := inspector.ConsumerGroupLag(context.Background(), "track-consumer")
	if err != nil {
		t.Fatalf("ConsumerGroupLag() error = %v", err)
	}
	if len(lags) != 1 ||
		lags[0].Group != "track-consumer" ||
		lags[0].State != "Stable" ||
		len(lags[0].Partitions) != 1 ||
		lags[0].Partitions[0].CommittedOffset != 10 ||
		lags[0].Partitions[0].EndOffset != 15 ||
		lags[0].Partitions[0].Lag != 5 {
		t.Fatalf("ConsumerGroupLag() = %#v", lags)
	}
}

func TestInspectorValidatesBoundedRequests(t *testing.T) {
	t.Parallel()

	backend := &recordingInspectorBackend{}
	inspector := &Inspector{admin: backend, client: backend}
	many := make([]string, 65)
	for index := range many {
		many[index] = "name-" + strings.Repeat("x", index+1)
	}

	for _, test := range []struct {
		name string
		call func() error
		want error
	}{
		{
			name: "topics required",
			call: func() error {
				_, err := inspector.Topics(context.Background())

				return err
			},
			want: ErrInspectionTargetsRequired,
		},
		{
			name: "too many topics",
			call: func() error {
				_, err := inspector.Topics(context.Background(), many...)

				return err
			},
			want: ErrTooManyInspectionTargets,
		},
		{
			name: "invalid topic",
			call: func() error {
				_, err := inspector.Topics(context.Background(), " events ")

				return err
			},
			want: ErrInvalidInspectionTarget,
		},
		{
			name: "broker-invalid topic",
			call: func() error {
				_, err := inspector.Topics(context.Background(), ".")

				return err
			},
			want: ErrInvalidInspectionTarget,
		},
		{
			name: "groups required",
			call: func() error {
				_, err := inspector.ConsumerGroupLag(context.Background())

				return err
			},
			want: ErrInspectionTargetsRequired,
		},
		{
			name: "duplicate group",
			call: func() error {
				_, err := inspector.ConsumerGroupLag(
					context.Background(),
					"group",
					"group",
				)

				return err
			},
			want: ErrDuplicateInspectionTarget,
		},
		{
			name: "invalid UTF-8 group",
			call: func() error {
				_, err := inspector.ConsumerGroupLag(
					context.Background(),
					string([]byte{0xff}),
				)

				return err
			},
			want: ErrInvalidInspectionTarget,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.call(); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	if backend.topicCalls != 0 || backend.lagCalls != 0 {
		t.Fatalf("backend calls = %d/%d", backend.topicCalls, backend.lagCalls)
	}
}

func TestInspectorPreservesRequestAndPartitionFailures(t *testing.T) {
	t.Parallel()

	requestErr := errors.New("metadata unavailable")
	backend := &recordingInspectorBackend{topicErr: requestErr, lagErr: requestErr}
	inspector := &Inspector{admin: backend, client: backend}
	if _, err := inspector.Topics(context.Background(), "events"); !errors.Is(err, requestErr) {
		t.Fatalf("Topics() request error = %v, want %v", err, requestErr)
	}
	if _, err := inspector.ConsumerGroupLag(context.Background(), "group"); !errors.Is(err, requestErr) {
		t.Fatalf("ConsumerGroupLag() request error = %v, want %v", err, requestErr)
	}

	partitionErr := errors.New("partition unavailable")
	backend = &recordingInspectorBackend{
		topics: kadm.TopicDetails{"events": {Topic: "events", Err: partitionErr}},
		lags: kadm.DescribedGroupLags{"group": {
			Group: "group",
			Lag:   kadm.GroupLag{"events": {0: {Err: partitionErr}}},
		}},
	}
	inspector = &Inspector{admin: backend, client: backend}
	if _, err := inspector.Topics(context.Background(), "events"); !errors.Is(err, partitionErr) {
		t.Fatalf("Topics() partition error = %v, want %v", err, partitionErr)
	}
	if _, err := inspector.ConsumerGroupLag(context.Background(), "group"); !errors.Is(err, partitionErr) {
		t.Fatalf("ConsumerGroupLag() partition error = %v, want %v", err, partitionErr)
	}

	backend = &recordingInspectorBackend{
		lags: kadm.DescribedGroupLags{"group": {
			Group: "group", DescribeErr: partitionErr,
		}},
	}
	inspector = &Inspector{admin: backend, client: backend}
	if _, err := inspector.ConsumerGroupLag(context.Background(), "group"); !errors.Is(err, partitionErr) {
		t.Fatalf("ConsumerGroupLag() group error = %v, want %v", err, partitionErr)
	}
}

func TestInspectorConstructsHealthChecksClosesAndPreservesFactoryFailure(t *testing.T) {
	t.Parallel()

	inspector, err := NewInspector(InspectorConfig{
		Brokers:     []string{"broker.internal:9092"},
		ClientID:    "track-inspector",
		DialTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewInspector() error = %v", err)
	}
	inspector.Close()

	factoryErr := errors.New("client construction failed")
	inspector, err = newInspector(
		InspectorConfig{
			Brokers:  []string{"broker.internal:9092"},
			ClientID: "track-inspector",
		},
		func(...kgo.Opt) (*kgo.Client, error) { return nil, factoryErr },
		func(*kgo.Client, InspectorConfig) inspectorBackend {
			t.Fatal("admin factory called after client construction failure")

			return nil
		},
	)
	if inspector != nil {
		inspector.Close()
		t.Fatal("newInspector() returned inspector after factory failure")
	}
	if !errors.Is(err, factoryErr) {
		t.Fatalf("newInspector() error = %v, want %v", err, factoryErr)
	}

	healthErr := errors.New("broker unavailable")
	backend := &recordingInspectorBackend{healthErr: healthErr}
	inspector = &Inspector{admin: backend, client: backend}
	if err := inspector.Health(context.Background()); !errors.Is(err, healthErr) {
		t.Fatalf("Health() error = %v, want %v", err, healthErr)
	}
	inspector.Close()
	if backend.closed != 1 {
		t.Fatalf("Close() calls = %d, want 1", backend.closed)
	}
}

func TestInspectorConfigRejectsInvalidIdentitySecurityAndTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config InspectorConfig
		want   error
	}{
		{
			name: "identity",
			config: InspectorConfig{
				ClientID: "track-inspector",
			},
			want: ErrBrokersRequired,
		},
		{
			name: "invalid client ID",
			config: InspectorConfig{
				Brokers:  []string{"broker.internal:9092"},
				ClientID: "track\ninspector",
			},
			want: ErrInvalidClientID,
		},
		{
			name: "security",
			config: InspectorConfig{
				Brokers:  []string{"broker.internal:9092"},
				ClientID: "track-inspector",
				Security: ClientSecurity{TLS: insecureTLSConfig()},
			},
			want: ErrInvalidSecurityConfig,
		},
		{
			name: "timeout",
			config: InspectorConfig{
				Brokers:     []string{"broker.internal:9092"},
				ClientID:    "track-inspector",
				DialTimeout: time.Millisecond,
			},
			want: ErrInvalidInspectorConfig,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inspector, err := NewInspector(test.config)
			if inspector != nil {
				inspector.Close()
				t.Fatal("NewInspector() returned inspector for invalid config")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("NewInspector() error = %v, want %v", err, test.want)
			}
		})
	}
}

type recordingInspectorBackend struct {
	topics     kadm.TopicDetails
	lags       kadm.DescribedGroupLags
	groupLags  inspectorGroupLags
	lagFn      func(context.Context) (kadm.DescribedGroupLags, error)
	topicErr   error
	lagErr     error
	healthErr  error
	healthFn   func(context.Context) error
	topicCalls int
	lagCalls   int
	closed     int
}

func (backend *recordingInspectorBackend) ListTopics(
	context.Context,
	...string,
) (kadm.TopicDetails, error) {
	backend.topicCalls++

	return backend.topics, backend.topicErr
}

func (backend *recordingInspectorBackend) Lag(
	ctx context.Context,
	_ ...string,
) (inspectorGroupLags, error) {
	backend.lagCalls++
	if backend.groupLags != nil {
		return backend.groupLags, backend.lagErr
	}
	if backend.lagFn != nil {
		lags, err := backend.lagFn(ctx)
		translated, translateErr := translateDescribedGroupLags(
			lags,
			10_000,
			100_000,
		)

		return translated, errors.Join(err, translateErr)
	}

	translated, err := translateDescribedGroupLags(
		backend.lags,
		10_000,
		100_000,
	)

	return translated, errors.Join(backend.lagErr, err)
}

func (backend *recordingInspectorBackend) BrokerMetadata(
	context.Context,
) (kadm.Metadata, error) {
	return kadm.Metadata{}, nil
}

func (backend *recordingInspectorBackend) Metadata(
	context.Context,
	...string,
) (kadm.Metadata, error) {
	return kadm.Metadata{Topics: backend.topics}, backend.topicErr
}

func (backend *recordingInspectorBackend) ListStartOffsets(
	context.Context,
	...string,
) (kadm.ListedOffsets, error) {
	return inspectorOffsetsForTopics(backend.topics), nil
}

func (backend *recordingInspectorBackend) ListEndOffsets(
	context.Context,
	...string,
) (kadm.ListedOffsets, error) {
	return inspectorOffsetsForTopics(backend.topics), nil
}

func (backend *recordingInspectorBackend) DescribeTopicConfigs(
	context.Context,
	...string,
) (kadm.ResourceConfigs, error) {
	configs := make(kadm.ResourceConfigs, 0, len(backend.topics))
	for _, topic := range backend.topics.Sorted() {
		configs = append(
			configs,
			validTopicInspectionResource(topic.Topic, "1"),
		)
	}

	return configs, nil
}

func inspectorOffsetsForTopics(topics kadm.TopicDetails) kadm.ListedOffsets {
	offsets := make(kadm.ListedOffsets, len(topics))
	for _, topic := range topics {
		partitions := make(map[int32]kadm.ListedOffset, len(topic.Partitions))
		for _, partition := range topic.Partitions {
			partitions[partition.Partition] = kadm.ListedOffset{
				Topic: topic.Topic, Partition: partition.Partition,
			}
		}
		offsets[topic.Topic] = partitions
	}

	return offsets
}

func (backend *recordingInspectorBackend) Ping(ctx context.Context) error {
	if backend.healthFn != nil {
		return backend.healthFn(ctx)
	}

	return backend.healthErr
}

func (backend *recordingInspectorBackend) Close() {
	backend.closed++
}
