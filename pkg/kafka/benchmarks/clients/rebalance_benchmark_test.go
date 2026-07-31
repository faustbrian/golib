//go:build integration

package clients_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const benchmarkRebalancePartitionCount = 2

type benchmarkRebalanceMeasurement struct {
	join  time.Duration
	leave time.Duration
}

type benchmarkRebalanceFixture struct {
	t           testing.TB
	brokers     []string
	topic       string
	groupID     string
	candidate   benchmarkConsumerCandidate
	first       benchmarkConsumer
	adminClient *kgo.Client
	admin       *kadm.Client
}

var benchmarkRebalanceCandidates = []benchmarkConsumerCandidate{
	{name: "golib-policy", new: newPolicyBenchmarkConsumer},
	{name: "raw-franz-go", new: newFranzBenchmarkConsumer},
	{name: "sarama", new: newSaramaBenchmarkConsumer},
}

func BenchmarkEquivalentConsumerRebalance(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, candidate := range benchmarkRebalanceCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			fixture := newBenchmarkRebalanceFixture(
				benchmark,
				brokers,
				candidate,
				benchmark.N+2,
			)
			benchmark.Cleanup(func() {
				if err := fixture.Close(); err != nil {
					benchmark.Errorf("close rebalance fixture: %v", err)
				}
			})
			benchmark.StopTimer()
			var total benchmarkRebalanceMeasurement
			for range benchmark.N {
				measurement, handled, err := fixture.Cycle()
				if err != nil {
					benchmark.Fatalf("rebalance cycle: %v", err)
				}
				if len(handled) != 1 ||
					handled[0].topic != fixture.topic ||
					handled[0].partition < 0 ||
					handled[0].partition >= benchmarkRebalancePartitionCount {
					benchmark.Fatalf("handled rebalance records = %#v", handled)
				}
				total.join += measurement.join
				total.leave += measurement.leave
			}
			operations := float64(benchmark.N)
			benchmark.ReportMetric(
				float64((total.join+total.leave).Nanoseconds())/operations,
				"ns/op",
			)
			benchmark.ReportMetric(
				float64(total.join.Nanoseconds())/operations,
				"join-through-commit-ns/op",
			)
			benchmark.ReportMetric(
				float64(total.leave.Nanoseconds())/operations,
				"leave-through-stable-ns/op",
			)
			benchmark.ReportMetric(
				benchmarkRebalancePartitionCount,
				"partitions/op",
			)
			runtime.KeepAlive(total)
		})
	}
}

func TestEquivalentConsumerRebalanceOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	for _, candidate := range benchmarkRebalanceCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			fixture := newBenchmarkRebalanceFixture(
				t,
				brokers,
				candidate,
				3,
			)
			defer func() {
				if err := fixture.Close(); err != nil {
					t.Errorf("close rebalance fixture: %v", err)
				}
			}()
			measurement, handled, err := fixture.Cycle()
			if err != nil {
				t.Fatalf("rebalance cycle: %v", err)
			}
			if measurement.join <= 0 || measurement.leave <= 0 {
				t.Fatalf("rebalance measurement = %#v", measurement)
			}
			if len(handled) != 1 {
				t.Fatalf("handled rebalance records = %#v", handled)
			}
			record := handled[0]
			if record.topic != fixture.topic ||
				record.partition < 0 ||
				record.partition >= benchmarkRebalancePartitionCount ||
				len(record.key) == 0 ||
				len(record.value) == 0 {
				t.Fatalf("handled rebalance record = %#v", record)
			}
		})
	}
}

func newBenchmarkRebalanceFixture(
	t testing.TB,
	brokers []string,
	candidate benchmarkConsumerCandidate,
	recordsPerPartition int,
) *benchmarkRebalanceFixture {
	t.Helper()
	topic := createIsolatedBenchmarkTopicWithPartitions(
		t,
		brokers,
		benchmarkRebalancePartitionCount,
	)
	produceBenchmarkRebalanceRecords(
		t,
		brokers,
		topic,
		recordsPerPartition,
	)
	groupID := fmt.Sprintf(
		"golib-client-rebalance-benchmark-%s-%d",
		candidate.name,
		time.Now().UnixNano(),
	)
	first := candidate.new(
		t,
		brokers,
		topic,
		groupID,
		benchmarkConsumeRecord,
		1,
		benchmarkConsumerSink{
			record: func(benchmarkConsumedRecord) error { return nil },
		},
	)
	adminClient, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-client-rebalance-benchmark-inspector"),
		kgo.DialTimeout(benchmarkRequestTimeout),
	)
	if err != nil {
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkConsumerOperationTimeout,
		)
		_ = first.Close(closeCtx)
		closeCancel()
		t.Fatalf("construct rebalance inspector: %v", err)
	}
	fixture := &benchmarkRebalanceFixture{
		t:           t,
		brokers:     slices.Clone(brokers),
		topic:       topic,
		groupID:     groupID,
		candidate:   candidate,
		first:       first,
		adminClient: adminClient,
		admin:       kadm.NewClient(adminClient),
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	consumed, consumeErr := first.Consume(ctx)
	cancel()
	if consumeErr != nil {
		_ = fixture.Close()
		t.Fatalf("warm first rebalance member: %v", consumeErr)
	}
	if consumed != 1 {
		_ = fixture.Close()
		t.Fatalf("warm first rebalance member consumed = %d, want 1", consumed)
	}
	stableCtx, stableCancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	stableErr := fixture.waitForStableMembers(stableCtx, 1)
	stableCancel()
	if stableErr != nil {
		_ = fixture.Close()
		t.Fatalf("wait for first rebalance member: %v", stableErr)
	}

	return fixture
}

func (fixture *benchmarkRebalanceFixture) Cycle() (
	benchmarkRebalanceMeasurement,
	[]benchmarkConsumedRecord,
	error,
) {
	handled := make([]benchmarkConsumedRecord, 0, 1)
	joinStarted := time.Now()
	second := fixture.candidate.new(
		fixture.t,
		fixture.brokers,
		fixture.topic,
		fixture.groupID,
		benchmarkConsumeRecord,
		1,
		benchmarkConsumerSink{
			record: func(record benchmarkConsumedRecord) error {
				handled = append(handled, retainBenchmarkConsumedRecord(record))

				return nil
			},
		},
	)
	consumeCtx, consumeCancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	consumed, err := second.Consume(consumeCtx)
	consumeCancel()
	if err != nil {
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkConsumerOperationTimeout,
		)
		_ = second.Close(closeCtx)
		closeCancel()

		return benchmarkRebalanceMeasurement{}, nil,
			fmt.Errorf("consume with joining member: %w", err)
	}
	if consumed != 1 {
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkConsumerOperationTimeout,
		)
		_ = second.Close(closeCtx)
		closeCancel()

		return benchmarkRebalanceMeasurement{}, nil, fmt.Errorf(
			"joining member consumed = %d, want 1",
			consumed,
		)
	}
	stableCtx, stableCancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	err = fixture.waitForStableMembers(stableCtx, 2)
	stableCancel()
	if err != nil {
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkConsumerOperationTimeout,
		)
		_ = second.Close(closeCtx)
		closeCancel()

		return benchmarkRebalanceMeasurement{}, nil, err
	}
	joinDuration := time.Since(joinStarted)
	leaveStarted := time.Now()
	closeCtx, closeCancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	err = second.Close(closeCtx)
	closeCancel()
	if err != nil {
		return benchmarkRebalanceMeasurement{}, nil, err
	}
	stableCtx, stableCancel = context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	err = fixture.waitForStableMembers(stableCtx, 1)
	stableCancel()
	if err != nil {
		return benchmarkRebalanceMeasurement{}, nil, err
	}
	leaveDuration := time.Since(leaveStarted)
	if len(handled) != 1 {
		return benchmarkRebalanceMeasurement{}, nil, fmt.Errorf(
			"joining member handled %d records, want 1",
			len(handled),
		)
	}
	offsetCtx, offsetCancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	err = fixture.validateCommittedRecord(offsetCtx, handled[0])
	offsetCancel()
	if err != nil {
		return benchmarkRebalanceMeasurement{}, nil, err
	}

	return benchmarkRebalanceMeasurement{
		join:  joinDuration,
		leave: leaveDuration,
	}, handled, nil
}

func (fixture *benchmarkRebalanceFixture) Close() error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkConsumerOperationTimeout,
	)
	err := fixture.first.Close(ctx)
	cancel()
	fixture.adminClient.Close()

	return err
}

func (fixture *benchmarkRebalanceFixture) waitForStableMembers(
	ctx context.Context,
	memberCount int,
) error {
	retry := time.NewTicker(benchmarkRetryMin)
	defer retry.Stop()
	var stateErr error
	for {
		stateErr = fixture.validateStableMembers(ctx, memberCount)
		if stateErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), stateErr)
		case <-retry.C:
		}
	}
}

func (fixture *benchmarkRebalanceFixture) validateStableMembers(
	ctx context.Context,
	memberCount int,
) error {
	describeCtx, cancel := context.WithTimeout(ctx, benchmarkRequestTimeout)
	defer cancel()
	groups, err := fixture.admin.DescribeGroups(describeCtx, fixture.groupID)
	if err != nil {
		return err
	}
	group, exists := groups[fixture.groupID]
	if !exists {
		return errors.New("rebalance group is absent")
	}
	if group.Err != nil {
		return group.Err
	}
	if group.State != "Stable" {
		return fmt.Errorf("rebalance group state = %q", group.State)
	}
	if group.ProtocolType != "consumer" ||
		group.Protocol != "cooperative-sticky" {
		return fmt.Errorf(
			"rebalance group protocol = %q/%q",
			group.ProtocolType,
			group.Protocol,
		)
	}
	if len(group.Members) != memberCount {
		return fmt.Errorf(
			"rebalance group members = %d, want %d",
			len(group.Members),
			memberCount,
		)
	}
	partitionsPerMember := benchmarkRebalancePartitionCount / memberCount
	seen := make(map[int32]struct{}, benchmarkRebalancePartitionCount)
	for _, member := range group.Members {
		assignment, ok := member.Assigned.AsConsumer()
		if !ok || len(assignment.Topics) != 1 {
			return errors.New("rebalance member assignment is not one consumer topic")
		}
		assignedTopic := assignment.Topics[0]
		if assignedTopic.Topic != fixture.topic ||
			len(assignedTopic.Partitions) != partitionsPerMember {
			return fmt.Errorf(
				"rebalance member assignment = %#v",
				assignedTopic,
			)
		}
		for _, partition := range assignedTopic.Partitions {
			if partition < 0 ||
				partition >= benchmarkRebalancePartitionCount {
				return fmt.Errorf(
					"rebalance partition = %d",
					partition,
				)
			}
			if _, duplicate := seen[partition]; duplicate {
				return fmt.Errorf(
					"duplicate rebalance partition = %d",
					partition,
				)
			}
			seen[partition] = struct{}{}
		}
	}
	if len(seen) != benchmarkRebalancePartitionCount {
		return fmt.Errorf(
			"rebalance assigned partitions = %d, want %d",
			len(seen),
			benchmarkRebalancePartitionCount,
		)
	}

	return nil
}

func (fixture *benchmarkRebalanceFixture) validateCommittedRecord(
	ctx context.Context,
	record benchmarkConsumedRecord,
) error {
	offsets, err := fixture.admin.FetchOffsets(ctx, fixture.groupID)
	if err != nil {
		return err
	}
	offset, exists := offsets.Lookup(fixture.topic, record.partition)
	if !exists {
		return fmt.Errorf(
			"rebalance committed offset for %s[%d] is absent",
			fixture.topic,
			record.partition,
		)
	}
	if offset.Err != nil {
		return offset.Err
	}
	if offset.At != record.offset+1 {
		return fmt.Errorf(
			"rebalance committed offset for %s[%d] = %d, want %d",
			fixture.topic,
			record.partition,
			offset.At,
			record.offset+1,
		)
	}

	return nil
}

func produceBenchmarkRebalanceRecords(
	t testing.TB,
	brokers []string,
	topic string,
	recordsPerPartition int,
) {
	t.Helper()
	records := make(
		[]multiPartitionRecord,
		0,
		recordsPerPartition*benchmarkRebalancePartitionCount,
	)
	for index := range recordsPerPartition {
		for partition := range int32(benchmarkRebalancePartitionCount) {
			records = append(records, multiPartitionRecord{
				partition: partition,
				key: []byte(fmt.Sprintf(
					"rebalance-%d-%d",
					partition,
					index,
				)),
				value: []byte(fmt.Sprintf(
					"rebalance-value-%d-%d",
					partition,
					index,
				)),
			})
		}
	}
	producer := newFranzMultiPartitionProducer(
		t,
		brokers,
		topic,
		multiPartitionExplicit,
		compressionNone,
	)
	ctx, cancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout+benchmarkRetryMax,
	)
	err := producer.ProduceBatch(ctx, records)
	cancel()
	if err != nil {
		closeCtx, closeCancel := context.WithTimeout(
			context.Background(),
			benchmarkDeliveryTimeout+benchmarkRetryMax,
		)
		_ = producer.Close(closeCtx)
		closeCancel()
		t.Fatalf("produce rebalance records: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(
		context.Background(),
		benchmarkDeliveryTimeout+benchmarkRetryMax,
	)
	err = producer.Close(closeCtx)
	closeCancel()
	if err != nil {
		t.Fatalf("close rebalance producer: %v", err)
	}
}
