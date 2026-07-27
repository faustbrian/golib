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
- effective topic configuration, including broker defaults, for
  `min.insync.replicas`, `cleanup.policy`, `retention.ms`, `retention.bytes`,
  `delete.retention.ms`, `min.compaction.lag.ms`,
  `max.compaction.lag.ms`, `min.cleanable.dirty.ratio`, `segment.bytes`,
  `segment.ms`, and `unclean.leader.election.enable`.

`MaxMetadataPartitions` bounds aggregate returned partitions. Replica sets are
also bounded by `MaxMetadataBrokers`. Missing topics, partial partition
metadata, a topic with no partitions, invalid replica relationships, missing
offsets, and missing or
malformed selected configuration fail closed. At most 1,024 broker-returned
configuration entries are accepted per topic, and each selected value is
limited to 64 valid UTF-8 bytes before parsing. The method does not return a
partially successful topic list.

`TopicCleanupPolicy` preserves Kafka's delete and compact policies as flags;
zero means that neither policy is active. Both policies may be active together.
Retention, compaction-lag, tombstone-retention, and segment durations are
returned as raw Kafka milliseconds rather than `time.Duration`: Kafka permits
values, including `math.MaxInt64`, that a Go duration cannot represent.
`RetentionMilliseconds` and `RetentionBytesPerPartition` use Kafka's `-1`
sentinel for no limit. The byte limit is per partition, not a topic-wide total.

These values describe effective topic policy, not a promise that a particular
record still exists or will be removed at an exact instant. Kafka removes
eligible closed segments asynchronously; compaction preserves key history only
according to the cleaner's current state and policy. Beginning offsets remain
the broker evidence for the currently readable range. Tiered-storage
`local.retention.ms` and `local.retention.bytes` are not exposed yet, so this
surface does not completely describe local-versus-remote retention. The
unclean-election field reports configured permission, not whether an unclean
election occurred.

Metadata, offsets, and configuration are separate Kafka requests rather than
an atomic cluster snapshot. franz-go may satisfy metadata from its bounded
client cache, while retention, reassignment, leadership, and configuration can
change between requests. Contradictory state fails closed; callers may retry a
diagnostic read but must not treat repeated inconsistency as healthy.

Replication factor and ISR are current facts, not sufficient durability by
themselves. For an all-ISR producer, `min.insync.replicas` controls the minimum
ISR size required for a successful write. Evaluate it with current ISR,
replication factor, unclean-election permission, cleanup, segment, retention,
and compaction policy. The package reports these facts but does not decide
whether they satisfy an application's durability or recovery objectives.

## Authorization

Cluster inspection requires Kafka cluster describe access. Topic inspection
requires topic describe, offset-listing, and topic-configuration describe
access. Amazon MSK IAM deployments additionally require the corresponding
`DescribeCluster`, `DescribeTopic`, and
`DescribeTopicDynamicConfiguration` permissions. Authorization failure is
returned unchanged and is not interpreted as topic absence.

## Consumer groups

`ConsumerGroupLag` accepts 1 to 64 explicit classic consumer-group names. It
returns coordinator node ID, group state, protocol type, selected assignor,
members sorted by member ID, member and optional static-instance identity,
client identity and host, assignments sorted by topic and partition, committed
offsets, log bounds, and lag.

`MaxGroupMembers` bounds members copied across the call.
`MaxMetadataPartitions` bounds combined lag partitions plus assignment topic
and partition entries copied from broker-controlled state. Duplicate member or
instance IDs, overlapping partition ownership, invalid text or topics, negative
partitions, non-consumer assignment encodings, invalid parsed metadata, and
partial responses fail closed. Member IDs, client IDs, instance IDs, and client
hosts are diagnostic identifiers. Client hosts may disclose internal addresses
and must not become untrusted telemetry.

The current franz-go-backed path uses classic `DescribeGroups`. Apache Kafka
4.3 also supports the KIP-848 `consumer` group protocol through a different
description API. This package does not yet claim KIP-848 group inspection;
such groups are unverified rather than silently reported as classic groups.
Group description, committed offsets, and end offsets are separate requests,
so membership or lag can change during one call.

## Deadlines and health signals

Every operation derives `InspectorConfig.RequestTimeout` from its caller
context. The caller's earlier cancellation or deadline wins. Request and Kafka
errors preserve their identities for `errors.Is` and `errors.As`; the package
adds stable errors for invalid or excessive broker-controlled results.

`DependencyHealth` uses the same bound and proves only that a broker currently
responds. `Health` is its compatibility alias. `Readiness` applies configurable
consecutive-failure and recovery thresholds; defaults are three failures and
two successes. Initial readiness also requires the recovery threshold. Use
`ReadinessState.Ready` for service composition and treat the separately
returned error as the latest dependency diagnostic. Nil and caller-canceled
probes do not mutate state.

`Liveness` reports only whether this inspector remains locally open. Kafka
outages do not fail it. It is not complete process liveness and does not prove
that an application runner is making progress. Closing the inspector is
idempotent, immediately makes local liveness and readiness false, and fences
later calls with `ErrInspectorClosed`. `Close` returns an error so an observer
attempting same-inspector lifecycle reentry receives `ErrObserverReentry`
instead of deadlocking or silently closing the client.

`InspectorConfig.Observers` uses the shared copied, ordered, bounded
`ObserverPolicy`. Cluster, topic, and group observations export only bounded
aggregate counts, never broker hosts, cluster IDs, topic names, group names,
member identities, assignments, or lag coordinates. Dependency-health and
readiness observations keep probe success separate from the stateful readiness
decision. A conclusive `Readiness` call emits both its dependency probe and its
post-hysteresis decision; inconclusive nil, canceled, closed, or reentrant calls
do not mutate state or emit a readiness decision. Inspector broker events use
the same private franz-go hook as other clients.

No health signal proves topic existence, authorization, durability, group
progress, producer delivery, or transaction safety. Compose those requirements
from bounded diagnostic inspection and application policy, and keep broker
outages out of liveness-triggered restart loops.

## Primary contracts

- [Apache Kafka 4.3 topic configuration](https://kafka.apache.org/43/configuration/topic-configs/)
  defines `min.insync.replicas`, retention, compaction, and per-topic
  overrides.
- [Apache Kafka 4.3 monitoring](https://kafka.apache.org/43/operations/monitoring/)
  defines under-replicated partition state as ISR smaller than the complete
  replica set.
- [Apache Kafka 4.3 consumer-group operations](https://kafka.apache.org/43/operations/basic-kafka-operations/)
  distinguishes group state, members, assignments, committed offsets, and lag.
- [Apache Kafka 4.3 consumer rebalance protocol](https://kafka.apache.org/43/operations/consumer-rebalance-protocol/)
  distinguishes classic and KIP-848 consumer-protocol groups.
- [franz-go kadm v1.18.0](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kadm@v1.18.0)
  supplies the pinned metadata, offset-listing, configuration-description, and
  lag protocol implementation behind these owned models.
