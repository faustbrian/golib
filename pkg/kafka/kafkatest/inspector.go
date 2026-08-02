package kafkatest

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
)

// RunInspectorConformance proves bounded cluster, topic, consumer-group lag,
// dependency, readiness, liveness, and post-close inspection contracts.
func RunInspectorConformance(t *testing.T, harness BrokerHarness) {
	t.Helper()
	if err := harness.Validate(); err != nil {
		t.Fatal(err)
	}

	t.Run("read-only state is complete and lifecycle signals stay distinct", func(t *testing.T) {
		topic := harness.NewTopic(t, 2)
		producer := newConformanceProducer(t, harness, topic, "inspector-fixture")
		for partition := int32(0); partition < 2; partition++ {
			result := producer.PublishRecord(t.Context(), kafka.ProducerRecord{
				Topic: topic, Partition: kafka.ExplicitPartition(partition),
				Key: []byte("inspection"), Value: []byte{byte(partition)},
			})
			if result.Err != nil {
				t.Fatalf("fixture PublishRecord(%d) error = %v", partition, result.Err)
			}
		}
		group := nextConformanceGroup("inspection")
		consumer := newConformanceConsumer(t, harness, topic, group)
		result, err := runConsumerUntilRecords(t, consumer, 2, kafka.HandlerFunc(func(
			context.Context,
			kafka.ConsumedRecord,
		) error {
			return nil
		}))
		if err != nil || result.Processed != 2 || result.Committed != 2 {
			t.Fatalf("fixture RunOnce() = %#v, %v", result, err)
		}

		inspector, err := kafka.NewInspector(kafka.InspectorConfig{
			Brokers:  append([]string(nil), harness.Brokers...),
			ClientID: nextConformanceGroup("inspector"),
			Security: harness.Security,
		})
		if err != nil {
			t.Fatalf("NewInspector() error = %v", err)
		}
		cluster, err := inspector.Cluster(t.Context())
		if err != nil || len(cluster.Brokers) == 0 || !cluster.ControllerVisible ||
			cluster.ControllerID < 0 {
			t.Fatalf("Cluster() = %#v, %v", cluster, err)
		}
		topics, err := inspector.Topics(t.Context(), topic)
		if err != nil || len(topics) != 1 || topics[0].Name != topic ||
			len(topics[0].Partitions) != 2 {
			t.Fatalf("Topics() = %#v, %v", topics, err)
		}
		for index, partition := range topics[0].Partitions {
			if partition.Partition != int32(index) || partition.Leader < 0 ||
				partition.ReplicationFactor < 1 || partition.InSyncReplicas < 1 ||
				partition.BeginningOffset != 0 || partition.EndOffset != 1 {
				t.Fatalf("topic partition[%d] = %#v", index, partition)
			}
		}
		groups, err := inspector.ConsumerGroupLag(t.Context(), group)
		if err != nil || len(groups) != 1 || groups[0].Group != group ||
			len(groups[0].Partitions) != 2 {
			t.Fatalf("ConsumerGroupLag() = %#v, %v", groups, err)
		}
		for index, partition := range groups[0].Partitions {
			if partition.Topic != topic || partition.Partition != int32(index) ||
				partition.CommittedOffset != 1 || partition.EndOffset != 1 || partition.Lag != 0 {
				t.Fatalf("group partition[%d] = %#v", index, partition)
			}
		}
		if err := inspector.DependencyHealth(t.Context()); err != nil {
			t.Fatalf("DependencyHealth() error = %v", err)
		}
		first, err := inspector.Readiness(t.Context())
		if err != nil || first.Ready || !first.DependencyHealthy || first.ConsecutiveSuccesses != 1 {
			t.Fatalf("first Readiness() = %#v, %v", first, err)
		}
		second, err := inspector.Readiness(t.Context())
		if err != nil || !second.Ready || !second.DependencyHealthy ||
			second.ConsecutiveSuccesses != 2 {
			t.Fatalf("second Readiness() = %#v, %v", second, err)
		}
		if liveness := inspector.Liveness(); !liveness.Live {
			t.Fatalf("Liveness() = %#v", liveness)
		}
		if err := inspector.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if liveness := inspector.Liveness(); liveness.Live {
			t.Fatalf("post-close Liveness() = %#v", liveness)
		}
		if _, err := inspector.Cluster(t.Context()); !errors.Is(err, kafka.ErrInspectorClosed) {
			t.Fatalf("post-close Cluster() error = %v", err)
		}
	})
}
