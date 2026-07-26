# Inspection and dependency health

`Inspector` is the package's bounded, read-only Kafka administration surface.
It does not create, delete, alter, expand, reassign, or reset Kafka resources.

## Cluster inspection

`Cluster` returns the Kafka cluster ID when the broker supplies one, controller
visibility, and brokers sorted by node ID. Hosts, optional racks, ports, node
IDs, duplicate nodes, and the controller relationship are validated before the
package returns copied state. `MaxMetadataBrokers` bounds the copied broker
list.

An unavailable cluster ID or controller is represented explicitly through
`IDVisible` or `ControllerVisible`; malformed or contradictory metadata is an
error. Broker hostnames are diagnostic data and may disclose internal network
names. Do not export them to untrusted telemetry.

## Topic inspection

`Topics` accepts 1 to 64 unique explicit Kafka topic names. One bounded call
combines:

- topic and partition metadata;
- leader and leader epoch;
- replica preference order;
- sorted ISR and offline-replica node IDs;
- log-start and exclusive high-watermark offsets; and
- the effective topic `min.insync.replicas` value, including broker defaults.

`MaxMetadataPartitions` bounds aggregate returned partitions. Replica sets are
also bounded by `MaxMetadataBrokers`. Missing topics, partial partition
metadata, invalid replica relationships, missing offsets, and missing or
malformed durability configuration fail closed. The method does not return a
partially successful topic list.

Metadata, offsets, and configuration are separate Kafka requests rather than
an atomic cluster snapshot. franz-go may satisfy metadata from its bounded
client cache, while retention, reassignment, leadership, and configuration can
change between requests. Contradictory state fails closed; callers may retry a
diagnostic read but must not treat repeated inconsistency as healthy.

Replication factor and ISR are current facts, not sufficient durability by
themselves. For an all-ISR producer, `min.insync.replicas` controls the minimum
ISR size required for a successful write. Operators must define acceptable
replication, ISR, offline-replica, unclean-election, retention, and compaction
policy for each topic.

## Authorization

Cluster inspection requires Kafka cluster describe access. Topic inspection
requires topic describe, offset-listing, and topic-configuration describe
access. Amazon MSK IAM deployments additionally require the corresponding
`DescribeCluster`, `DescribeTopic`, and
`DescribeTopicDynamicConfiguration` permissions. Authorization failure is
returned unchanged and is not interpreted as topic absence.

## Deadlines and failure

Every operation derives `InspectorConfig.RequestTimeout` from its caller
context. The caller's earlier cancellation or deadline wins. Request and Kafka
errors preserve their identities for `errors.Is` and `errors.As`; the package
adds stable errors for invalid or excessive broker-controlled results.

`Health` uses the same bound but proves only that a broker currently responds.
It is dependency health, not process liveness. It does not prove topic
existence, authorization, durability, group progress, producer delivery, or
transaction safety. Keep broker outages out of liveness-triggered restart
loops. Stateful readiness thresholds and recovery hysteresis remain pending
package policy.

## Primary contracts

- [Apache Kafka 4.3 topic configuration](https://kafka.apache.org/43/configuration/topic-configs/)
  defines `min.insync.replicas`, retention, compaction, and per-topic
  overrides.
- [Apache Kafka 4.3 monitoring](https://kafka.apache.org/43/operations/monitoring/)
  defines under-replicated partition state as ISR smaller than the complete
  replica set.
- [franz-go kadm v1.18.0](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kadm@v1.18.0)
  supplies the pinned metadata, offset-listing, configuration-description, and
  lag protocol implementation behind these owned models.
