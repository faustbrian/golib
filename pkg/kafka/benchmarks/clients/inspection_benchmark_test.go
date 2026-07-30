//go:build integration

package clients_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
	policy "github.com/faustbrian/golib/pkg/kafka"
	segmentkafka "github.com/segmentio/kafka-go"
	segmentmetadata "github.com/segmentio/kafka-go/protocol/metadata"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const benchmarkInspectionOperationTimeout = 30 * time.Second

type benchmarkInspectionPartition struct {
	partition       int32
	leader          int32
	leaderEpoch     int32
	replicas        []int32
	inSyncReplicas  []int32
	offlineReplicas []int32
	startOffset     int64
	endOffset       int64
}

type benchmarkInspectionConfig struct {
	minInSyncReplicas                int
	cleanupPolicy                    policy.TopicCleanupPolicy
	retentionMilliseconds            int64
	retentionBytesPerPartition       int64
	deleteRetentionMilliseconds      int64
	minimumCompactionLagMilliseconds int64
	maximumCompactionLagMilliseconds int64
	minimumCleanableDirtyRatio       float64
	segmentBytes                     int64
	segmentMilliseconds              int64
	uncleanLeaderElectionEnabled     bool
}

type benchmarkInspectionTopic struct {
	name       string
	internal   bool
	config     benchmarkInspectionConfig
	partitions []benchmarkInspectionPartition
}

type benchmarkInspector interface {
	Topic(context.Context, string) (benchmarkInspectionTopic, error)
	Close() error
}

type benchmarkInspectionCandidate struct {
	name string
	new  func(testing.TB, []string) benchmarkInspector
}

var benchmarkInspectionCandidates = []benchmarkInspectionCandidate{
	{name: "golib-policy", new: newPolicyBenchmarkInspector},
	{name: "raw-franz-go", new: newFranzBenchmarkInspector},
	{name: "kafka-go", new: newKafkaGoBenchmarkInspector},
	{name: "sarama", new: newSaramaBenchmarkInspector},
}

func BenchmarkEquivalentInspection(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, partitionCount := range []int32{1, 8} {
		benchmark.Run(
			fmt.Sprintf("%d-partitions", partitionCount),
			func(benchmark *testing.B) {
				topic := createBenchmarkTopicWithPartitions(
					benchmark,
					brokers,
					partitionCount,
				)
				for _, candidate := range benchmarkInspectionCandidates {
					benchmark.Run(candidate.name, func(benchmark *testing.B) {
						inspector := candidate.new(benchmark, brokers)
						benchmark.Cleanup(func() {
							if err := inspector.Close(); err != nil {
								benchmark.Errorf("close inspector: %v", err)
							}
						})
						warmupCtx, warmupCancel := context.WithTimeout(
							context.Background(),
							benchmarkInspectionOperationTimeout,
						)
						_, err := inspector.Topic(warmupCtx, topic)
						warmupCancel()
						if err != nil {
							benchmark.Fatalf("warm inspection: %v", err)
						}
						var inspectedPartitions int
						benchmark.ReportAllocs()
						benchmark.ResetTimer()
						for benchmark.Loop() {
							ctx, cancel := context.WithTimeout(
								context.Background(),
								benchmarkInspectionOperationTimeout,
							)
							state, err := inspector.Topic(ctx, topic)
							cancel()
							if err != nil {
								benchmark.Fatalf("inspect topic: %v", err)
							}
							inspectedPartitions += len(state.partitions)
						}
						benchmark.StopTimer()
						benchmark.ReportMetric(
							float64(partitionCount),
							"partitions/op",
						)
						if inspectedPartitions != benchmark.N*int(partitionCount) {
							benchmark.Fatalf(
								"inspected partitions = %d, want %d",
								inspectedPartitions,
								benchmark.N*int(partitionCount),
							)
						}
					})
				}
			},
		)
	}
}

func TestEquivalentInspectionOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	topic := createBenchmarkTopicWithPartitions(t, brokers, 3)
	var want benchmarkInspectionTopic
	for index, candidate := range benchmarkInspectionCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			inspector := candidate.new(t, brokers)
			t.Cleanup(func() {
				if err := inspector.Close(); err != nil {
					t.Errorf("close inspector: %v", err)
				}
			})
			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkInspectionOperationTimeout,
			)
			defer cancel()
			got, err := inspector.Topic(ctx, topic)
			if err != nil {
				t.Fatalf("inspect topic: %v", err)
			}
			if index == 0 {
				want = got

				return
			}
			if !equalBenchmarkInspectionTopic(got, want) {
				t.Fatalf("inspection = %#v, want %#v", got, want)
			}
		})
	}
	if want.name != topic ||
		want.config.minInSyncReplicas != 1 ||
		want.config.cleanupPolicy != policy.TopicCleanupDelete ||
		len(want.partitions) != 3 {
		t.Fatalf("inspection = %#v, want topic %q with 3 partitions", want, topic)
	}
	for partitionID, partition := range want.partitions {
		if partition.partition != int32(partitionID) ||
			partition.leader < 0 ||
			len(partition.replicas) != 1 ||
			!slices.Equal(partition.replicas, partition.inSyncReplicas) ||
			len(partition.offlineReplicas) != 0 ||
			partition.startOffset != 0 ||
			partition.endOffset != 0 {
			t.Fatalf("partition %d inspection = %#v", partitionID, partition)
		}
	}
}

type policyBenchmarkInspector struct {
	inspector *policy.Inspector
}

func newPolicyBenchmarkInspector(
	t testing.TB,
	brokers []string,
) benchmarkInspector {
	t.Helper()
	inspector, err := policy.NewInspector(policy.InspectorConfig{
		Brokers:               brokers,
		ClientID:              "golib-policy-inspection-benchmark",
		Security:              policy.DevelopmentPlaintextSecurity(),
		DialTimeout:           benchmarkRequestTimeout,
		RequestTimeout:        benchmarkInspectionOperationTimeout,
		MaxMetadataBrokers:    100,
		MaxMetadataPartitions: 1_000,
		MaxGroupMembers:       1_000,
	})
	if err != nil {
		t.Fatalf("construct policy inspector: %v", err)
	}

	return &policyBenchmarkInspector{inspector: inspector}
}

func (inspector *policyBenchmarkInspector) Topic(
	ctx context.Context,
	topic string,
) (benchmarkInspectionTopic, error) {
	topics, err := inspector.inspector.Topics(ctx, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	if len(topics) != 1 {
		return benchmarkInspectionTopic{}, errors.New(
			"policy inspection response is incomplete",
		)
	}
	state := topics[0]
	result := benchmarkInspectionTopic{
		name:     state.Name,
		internal: state.Internal,
		config: benchmarkInspectionConfig{
			minInSyncReplicas:                state.MinInSyncReplicas,
			cleanupPolicy:                    state.CleanupPolicy,
			retentionMilliseconds:            state.RetentionMilliseconds,
			retentionBytesPerPartition:       state.RetentionBytesPerPartition,
			deleteRetentionMilliseconds:      state.DeleteRetentionMilliseconds,
			minimumCompactionLagMilliseconds: state.MinimumCompactionLagMilliseconds,
			maximumCompactionLagMilliseconds: state.MaximumCompactionLagMilliseconds,
			minimumCleanableDirtyRatio:       state.MinimumCleanableDirtyRatio,
			segmentBytes:                     state.SegmentBytes,
			segmentMilliseconds:              state.SegmentMilliseconds,
			uncleanLeaderElectionEnabled:     state.UncleanLeaderElectionEnabled,
		},
		partitions: make(
			[]benchmarkInspectionPartition,
			len(state.Partitions),
		),
	}
	for index, partition := range state.Partitions {
		result.partitions[index] = benchmarkInspectionPartition{
			partition:       partition.Partition,
			leader:          partition.Leader,
			leaderEpoch:     partition.LeaderEpoch,
			replicas:        slices.Clone(partition.Replicas),
			inSyncReplicas:  slices.Clone(partition.InSyncReplicaIDs),
			offlineReplicas: slices.Clone(partition.OfflineReplicaIDs),
			startOffset:     partition.BeginningOffset,
			endOffset:       partition.EndOffset,
		}
	}

	return result, nil
}

func (inspector *policyBenchmarkInspector) Close() error {
	return inspector.inspector.Close()
}

type franzBenchmarkInspector struct {
	client *kgo.Client
	admin  *kadm.Client
}

func newFranzBenchmarkInspector(
	t testing.TB,
	brokers []string,
) benchmarkInspector {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("raw-franz-go-inspection-benchmark"),
		kgo.DialTimeout(benchmarkRequestTimeout),
	)
	if err != nil {
		t.Fatalf("construct raw franz-go inspector: %v", err)
	}

	return &franzBenchmarkInspector{
		client: client,
		admin:  kadm.NewClient(client),
	}
}

func (inspector *franzBenchmarkInspector) Topic(
	ctx context.Context,
	topic string,
) (benchmarkInspectionTopic, error) {
	metadata, err := inspector.admin.Metadata(ctx, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	starts, err := inspector.admin.ListStartOffsets(ctx, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	ends, err := inspector.admin.ListEndOffsets(ctx, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	configs, err := inspector.admin.DescribeTopicConfigs(ctx, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	detail, exists := metadata.Topics[topic]
	if !exists || detail.Err != nil {
		return benchmarkInspectionTopic{}, errors.Join(
			errors.New("raw franz-go topic metadata is incomplete"),
			detail.Err,
		)
	}
	configValues, err := franzInspectionConfigValues(configs, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	config, err := parseBenchmarkInspectionConfig(configValues)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	result := benchmarkInspectionTopic{
		name:       detail.Topic,
		internal:   detail.IsInternal,
		config:     config,
		partitions: make([]benchmarkInspectionPartition, 0, len(detail.Partitions)),
	}
	for _, partition := range detail.Partitions.Sorted() {
		start, startExists := starts.Lookup(topic, partition.Partition)
		end, endExists := ends.Lookup(topic, partition.Partition)
		if !startExists || !endExists ||
			start.Err != nil ||
			end.Err != nil ||
			partition.Err != nil {
			return benchmarkInspectionTopic{}, errors.Join(
				errors.New("raw franz-go partition state is incomplete"),
				start.Err,
				end.Err,
				partition.Err,
			)
		}
		result.partitions = append(
			result.partitions,
			benchmarkInspectionPartition{
				partition:       partition.Partition,
				leader:          partition.Leader,
				leaderEpoch:     partition.LeaderEpoch,
				replicas:        slices.Clone(partition.Replicas),
				inSyncReplicas:  slices.Clone(partition.ISR),
				offlineReplicas: slices.Clone(partition.OfflineReplicas),
				startOffset:     start.Offset,
				endOffset:       end.Offset,
			},
		)
	}

	return result, nil
}

func (inspector *franzBenchmarkInspector) Close() error {
	inspector.client.Close()

	return nil
}

func franzInspectionConfigValues(
	configs kadm.ResourceConfigs,
	topic string,
) (map[string]string, error) {
	resource, err := configs.On(topic, nil)
	if err != nil {
		return nil, err
	}
	if resource.Err != nil {
		return nil, resource.Err
	}
	values := make(map[string]string, len(resource.Configs))
	for _, config := range resource.Configs {
		if config.Value != nil {
			values[config.Key] = *config.Value
		}
	}

	return values, nil
}

type kafkaGoBenchmarkInspector struct {
	address   net.Addr
	transport *segmentkafka.Transport
	client    *segmentkafka.Client
}

func newKafkaGoBenchmarkInspector(
	_ testing.TB,
	brokers []string,
) benchmarkInspector {
	address := segmentkafka.TCP(brokers...)
	transport := &segmentkafka.Transport{
		ClientID:    "kafka-go-inspection-benchmark",
		DialTimeout: benchmarkRequestTimeout,
		MetadataTTL: benchmarkRetryMin,
	}

	return &kafkaGoBenchmarkInspector{
		address:   address,
		transport: transport,
		client: &segmentkafka.Client{
			Addr:      address,
			Timeout:   benchmarkInspectionOperationTimeout,
			Transport: transport,
		},
	}
}

func (inspector *kafkaGoBenchmarkInspector) Topic(
	ctx context.Context,
	topic string,
) (benchmarkInspectionTopic, error) {
	rawMetadata, err := inspector.transport.RoundTrip(
		ctx,
		inspector.address,
		&segmentmetadata.Request{TopicNames: []string{topic}},
	)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	metadata, ok := rawMetadata.(*segmentmetadata.Response)
	if !ok || len(metadata.Topics) != 1 {
		return benchmarkInspectionTopic{}, errors.New(
			"kafka-go topic metadata response is incomplete",
		)
	}
	topicMetadata := metadata.Topics[0]
	if topicMetadata.ErrorCode != 0 || topicMetadata.Name != topic {
		return benchmarkInspectionTopic{}, errors.Join(
			errors.New("kafka-go topic metadata is invalid"),
			segmentkafka.Error(topicMetadata.ErrorCode),
		)
	}
	offsetRequests := make([]segmentkafka.OffsetRequest, 0, len(topicMetadata.Partitions))
	for _, partition := range topicMetadata.Partitions {
		offsetRequests = append(
			offsetRequests,
			segmentkafka.FirstOffsetOf(int(partition.PartitionIndex)),
		)
	}
	starts, err := inspector.client.ListOffsets(
		ctx,
		&segmentkafka.ListOffsetsRequest{
			Topics: map[string][]segmentkafka.OffsetRequest{
				topic: offsetRequests,
			},
		},
	)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	offsetRequests = offsetRequests[:0]
	for _, partition := range topicMetadata.Partitions {
		offsetRequests = append(
			offsetRequests,
			segmentkafka.LastOffsetOf(int(partition.PartitionIndex)),
		)
	}
	ends, err := inspector.client.ListOffsets(
		ctx,
		&segmentkafka.ListOffsetsRequest{
			Topics: map[string][]segmentkafka.OffsetRequest{
				topic: offsetRequests,
			},
		},
	)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	configs, err := inspector.client.DescribeConfigs(
		ctx,
		&segmentkafka.DescribeConfigsRequest{
			Resources: []segmentkafka.DescribeConfigRequestResource{{
				ResourceType: segmentkafka.ResourceTypeTopic,
				ResourceName: topic,
			}},
		},
	)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	configValues, err := kafkaGoInspectionConfigValues(configs, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	config, err := parseBenchmarkInspectionConfig(configValues)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	startOffsets, err := kafkaGoInspectionOffsets(starts, topic, true)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	endOffsets, err := kafkaGoInspectionOffsets(ends, topic, false)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	result := benchmarkInspectionTopic{
		name:       topicMetadata.Name,
		internal:   topicMetadata.IsInternal,
		config:     config,
		partitions: make([]benchmarkInspectionPartition, 0, len(topicMetadata.Partitions)),
	}
	for _, partition := range topicMetadata.Partitions {
		if partition.ErrorCode != 0 {
			return benchmarkInspectionTopic{}, segmentkafka.Error(
				partition.ErrorCode,
			)
		}
		start, startExists := startOffsets[partition.PartitionIndex]
		end, endExists := endOffsets[partition.PartitionIndex]
		if !startExists || !endExists {
			return benchmarkInspectionTopic{}, errors.New(
				"kafka-go partition offsets are incomplete",
			)
		}
		result.partitions = append(
			result.partitions,
			benchmarkInspectionPartition{
				partition:       partition.PartitionIndex,
				leader:          partition.LeaderID,
				leaderEpoch:     partition.LeaderEpoch,
				replicas:        slices.Clone(partition.ReplicaNodes),
				inSyncReplicas:  slices.Clone(partition.IsrNodes),
				offlineReplicas: slices.Clone(partition.OfflineReplicas),
				startOffset:     start,
				endOffset:       end,
			},
		)
	}
	sortBenchmarkInspectionPartitions(result.partitions)

	return result, nil
}

func (inspector *kafkaGoBenchmarkInspector) Close() error {
	inspector.transport.CloseIdleConnections()

	return nil
}

func kafkaGoInspectionConfigValues(
	response *segmentkafka.DescribeConfigsResponse,
	topic string,
) (map[string]string, error) {
	if len(response.Resources) != 1 ||
		response.Resources[0].ResourceName != topic {
		return nil, errors.New("kafka-go topic configs are incomplete")
	}
	resource := response.Resources[0]
	if resource.Error != nil {
		return nil, resource.Error
	}
	values := make(map[string]string, len(resource.ConfigEntries))
	for _, config := range resource.ConfigEntries {
		if !config.IsSensitive {
			values[config.ConfigName] = config.ConfigValue
		}
	}

	return values, nil
}

func kafkaGoInspectionOffsets(
	response *segmentkafka.ListOffsetsResponse,
	topic string,
	first bool,
) (map[int32]int64, error) {
	partitions, exists := response.Topics[topic]
	if !exists {
		return nil, errors.New("kafka-go topic offsets are missing")
	}
	offsets := make(map[int32]int64, len(partitions))
	for _, partition := range partitions {
		if partition.Error != nil {
			return nil, partition.Error
		}
		offset := partition.LastOffset
		if first {
			offset = partition.FirstOffset
		}
		offsets[int32(partition.Partition)] = offset
	}

	return offsets, nil
}

type saramaBenchmarkInspector struct {
	client sarama.Client
	admin  sarama.ClusterAdmin
}

func newSaramaBenchmarkInspector(
	t testing.TB,
	brokers []string,
) benchmarkInspector {
	t.Helper()
	config := sarama.NewConfig()
	config.ClientID = "sarama-inspection-benchmark"
	config.Version = sarama.V3_5_0_0
	config.Net.DialTimeout = benchmarkRequestTimeout
	config.Net.ReadTimeout = benchmarkInspectionOperationTimeout
	config.Net.WriteTimeout = benchmarkInspectionOperationTimeout
	config.Metadata.AllowAutoTopicCreation = false
	config.Metadata.Retry.Backoff = benchmarkRetryMin
	client, err := sarama.NewClient(brokers, config)
	if err != nil {
		t.Fatalf("construct Sarama inspection client: %v", err)
	}
	admin, err := sarama.NewClusterAdminFromClient(client)
	if err != nil {
		_ = client.Close()
		t.Fatalf("construct Sarama inspector: %v", err)
	}

	return &saramaBenchmarkInspector{client: client, admin: admin}
}

func (inspector *saramaBenchmarkInspector) Topic(
	ctx context.Context,
	topic string,
) (benchmarkInspectionTopic, error) {
	if cause := context.Cause(ctx); cause != nil {
		return benchmarkInspectionTopic{}, cause
	}
	topics, err := inspector.admin.DescribeTopics([]string{topic})
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	if len(topics) != 1 || topics[0].Name != topic || topics[0].Err != 0 {
		var topicErr error
		if len(topics) == 1 && topics[0].Err != 0 {
			topicErr = topics[0].Err
		}

		return benchmarkInspectionTopic{}, errors.Join(
			errors.New("Sarama topic metadata is incomplete"),
			topicErr,
		)
	}
	detail := topics[0]
	starts, err := saramaInspectionOffsets(
		inspector.client,
		topic,
		detail.Partitions,
		sarama.OffsetOldest,
	)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	ends, err := saramaInspectionOffsets(
		inspector.client,
		topic,
		detail.Partitions,
		sarama.OffsetNewest,
	)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	configs, err := inspector.admin.DescribeConfigs(
		[]*sarama.ConfigResource{{
			Type: sarama.TopicResource,
			Name: topic,
		}},
		sarama.DescribeConfigsOptions{},
	)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	configValues, err := saramaInspectionConfigValues(configs, topic)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	config, err := parseBenchmarkInspectionConfig(configValues)
	if err != nil {
		return benchmarkInspectionTopic{}, err
	}
	result := benchmarkInspectionTopic{
		name:       detail.Name,
		internal:   detail.IsInternal,
		config:     config,
		partitions: make([]benchmarkInspectionPartition, 0, len(detail.Partitions)),
	}
	for _, partition := range detail.Partitions {
		if partition.Err != 0 {
			return benchmarkInspectionTopic{}, partition.Err
		}
		start, startExists := starts[partition.ID]
		end, endExists := ends[partition.ID]
		if !startExists || !endExists {
			return benchmarkInspectionTopic{}, errors.New(
				"Sarama partition offsets are incomplete",
			)
		}
		result.partitions = append(
			result.partitions,
			benchmarkInspectionPartition{
				partition:       partition.ID,
				leader:          partition.Leader,
				leaderEpoch:     partition.LeaderEpoch,
				replicas:        slices.Clone(partition.Replicas),
				inSyncReplicas:  slices.Clone(partition.Isr),
				offlineReplicas: slices.Clone(partition.OfflineReplicas),
				startOffset:     start,
				endOffset:       end,
			},
		)
	}
	sortBenchmarkInspectionPartitions(result.partitions)

	return result, context.Cause(ctx)
}

func (inspector *saramaBenchmarkInspector) Close() error {
	return inspector.admin.Close()
}

func saramaInspectionOffsets(
	client sarama.Client,
	topic string,
	partitions []*sarama.PartitionMetadata,
	timestamp int64,
) (map[int32]int64, error) {
	requests := make(map[*sarama.Broker]*sarama.OffsetRequest)
	for _, partition := range partitions {
		leader, err := client.Leader(topic, partition.ID)
		if err != nil {
			return nil, err
		}
		request := requests[leader]
		if request == nil {
			request = sarama.NewOffsetRequest(sarama.V3_5_0_0)
			requests[leader] = request
		}
		request.AddBlock(topic, partition.ID, timestamp, 1)
	}
	offsets := make(map[int32]int64, len(partitions))
	for broker, request := range requests {
		response, err := broker.GetAvailableOffsets(request)
		if err != nil {
			return nil, err
		}
		responsePartitions, exists := response.Blocks[topic]
		if !exists {
			return nil, errors.New("Sarama topic offsets are missing")
		}
		for partitionID, block := range responsePartitions {
			if block == nil || block.Err != 0 {
				var blockErr error
				if block != nil {
					blockErr = block.Err
				}

				return nil, errors.Join(
					errors.New("Sarama partition offset is unavailable"),
					blockErr,
				)
			}
			offsets[partitionID] = block.Offset
		}
	}

	return offsets, nil
}

func saramaInspectionConfigValues(
	resources []*sarama.ConfigResourceResult,
	topic string,
) (map[string]string, error) {
	if len(resources) != 1 ||
		resources[0].Name != topic ||
		resources[0].ErrorCode != 0 {
		var resourceErr error
		if len(resources) == 1 && resources[0].ErrorCode != 0 {
			resourceErr = resources[0].ErrorCode
		}

		return nil, errors.Join(
			errors.New("Sarama topic configs are incomplete"),
			resourceErr,
		)
	}
	values := make(map[string]string, len(resources[0].Configs))
	for _, config := range resources[0].Configs {
		if !config.Sensitive {
			values[config.Name] = config.Value
		}
	}

	return values, nil
}

var benchmarkInspectionConfigNames = []string{
	"min.insync.replicas",
	"cleanup.policy",
	"retention.ms",
	"retention.bytes",
	"delete.retention.ms",
	"min.compaction.lag.ms",
	"max.compaction.lag.ms",
	"min.cleanable.dirty.ratio",
	"segment.bytes",
	"segment.ms",
	"unclean.leader.election.enable",
}

func parseBenchmarkInspectionConfig(
	values map[string]string,
) (benchmarkInspectionConfig, error) {
	for _, name := range benchmarkInspectionConfigNames {
		if _, exists := values[name]; !exists {
			return benchmarkInspectionConfig{}, fmt.Errorf(
				"topic config %q is missing",
				name,
			)
		}
	}
	minInSyncReplicas, err := strconv.Atoi(values["min.insync.replicas"])
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	cleanupPolicy, err := parseBenchmarkCleanupPolicy(values["cleanup.policy"])
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	retentionMilliseconds, err := strconv.ParseInt(
		values["retention.ms"],
		10,
		64,
	)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	retentionBytes, err := strconv.ParseInt(values["retention.bytes"], 10, 64)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	deleteRetentionMilliseconds, err := strconv.ParseInt(
		values["delete.retention.ms"],
		10,
		64,
	)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	minimumCompactionLagMilliseconds, err := strconv.ParseInt(
		values["min.compaction.lag.ms"],
		10,
		64,
	)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	maximumCompactionLagMilliseconds, err := strconv.ParseInt(
		values["max.compaction.lag.ms"],
		10,
		64,
	)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	minimumCleanableDirtyRatio, err := strconv.ParseFloat(
		values["min.cleanable.dirty.ratio"],
		64,
	)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	segmentBytes, err := strconv.ParseInt(values["segment.bytes"], 10, 64)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	segmentMilliseconds, err := strconv.ParseInt(values["segment.ms"], 10, 64)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}
	uncleanLeaderElectionEnabled, err := strconv.ParseBool(
		values["unclean.leader.election.enable"],
	)
	if err != nil {
		return benchmarkInspectionConfig{}, err
	}

	return benchmarkInspectionConfig{
		minInSyncReplicas:                minInSyncReplicas,
		cleanupPolicy:                    cleanupPolicy,
		retentionMilliseconds:            retentionMilliseconds,
		retentionBytesPerPartition:       retentionBytes,
		deleteRetentionMilliseconds:      deleteRetentionMilliseconds,
		minimumCompactionLagMilliseconds: minimumCompactionLagMilliseconds,
		maximumCompactionLagMilliseconds: maximumCompactionLagMilliseconds,
		minimumCleanableDirtyRatio:       minimumCleanableDirtyRatio,
		segmentBytes:                     segmentBytes,
		segmentMilliseconds:              segmentMilliseconds,
		uncleanLeaderElectionEnabled:     uncleanLeaderElectionEnabled,
	}, nil
}

func parseBenchmarkCleanupPolicy(
	value string,
) (policy.TopicCleanupPolicy, error) {
	var result policy.TopicCleanupPolicy
	for _, entry := range strings.Split(value, ",") {
		switch strings.TrimSpace(entry) {
		case "compact":
			result |= policy.TopicCleanupCompact
		case "delete":
			result |= policy.TopicCleanupDelete
		default:
			return 0, fmt.Errorf("unknown cleanup policy %q", entry)
		}
	}
	if result == 0 {
		return 0, errors.New("cleanup policy is empty")
	}

	return result, nil
}

func sortBenchmarkInspectionPartitions(
	partitions []benchmarkInspectionPartition,
) {
	slices.SortFunc(
		partitions,
		func(left, right benchmarkInspectionPartition) int {
			switch {
			case left.partition < right.partition:
				return -1
			case left.partition > right.partition:
				return 1
			default:
				return 0
			}
		},
	)
}

func equalBenchmarkInspectionTopic(
	left benchmarkInspectionTopic,
	right benchmarkInspectionTopic,
) bool {
	return left.name == right.name &&
		left.internal == right.internal &&
		left.config == right.config &&
		slices.EqualFunc(
			left.partitions,
			right.partitions,
			func(
				left benchmarkInspectionPartition,
				right benchmarkInspectionPartition,
			) bool {
				return left.partition == right.partition &&
					left.leader == right.leader &&
					left.leaderEpoch == right.leaderEpoch &&
					slices.Equal(left.replicas, right.replicas) &&
					slices.Equal(left.inSyncReplicas, right.inSyncReplicas) &&
					slices.Equal(left.offlineReplicas, right.offlineReplicas) &&
					left.startOffset == right.startOffset &&
					left.endOffset == right.endOffset
			},
		)
}

func createBenchmarkTopicWithPartitions(
	t testing.TB,
	brokers []string,
	partitionCount int32,
) string {
	t.Helper()
	topic, err := createBenchmarkTopicWithPartitionsOnce(
		brokers,
		partitionCount,
	)
	if err != nil {
		t.Fatalf("create benchmark topic with %d partitions: %v", partitionCount, err)
	}

	return topic
}
