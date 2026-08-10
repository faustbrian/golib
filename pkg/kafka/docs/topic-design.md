# Topic, keying, partitioning, and durability design

Kafka topic design is an application and operator contract. This package
validates client-side topic, key, partition, record, and buffer policy, but it
does not create topics, add partitions, change replication, alter retention,
or repair replicas. Provision those settings through reviewed infrastructure
automation and verify the effective broker state through `Inspector` before
rollout.

Record one decision for every production topic before a producer or consumer
is deployed:

| Decision | Required evidence |
| --- | --- |
| logical data contract | owners, producers, consumer histories, value format, compatibility policy |
| ordering identity | exact non-empty key bytes or reviewed unkeyed/explicit-partition policy |
| partitions | current count, peak ordered concurrency, hot-key distribution, expansion consequences |
| durability | replication factor, `min.insync.replicas`, unclean-election policy, producer acknowledgements |
| lifecycle | cleanup policy, retention time and bytes, segment behavior, compaction and tombstone policy |
| size | maximum record batch, package record/header/batch limits, consumer fetch and decoded limits |
| recovery | replay horizon, backup or cross-cluster recovery owner, retention-loss response |
| security | producer, consumer-group, transaction, replay, and inspection authorization boundaries |

The package's topic allowlists restrict application intent; they do not replace
Kafka ACLs, IAM authorization, or infrastructure ownership.

## Topic identity and evolution

Topic names are durable routing and authorization identities. The package
accepts Kafka-compatible names of 1 through 249 ASCII bytes containing letters,
digits, `.`, `_`, or `-`, while rejecting `.` and `..`. Prefer one naming
convention across the cluster because Kafka metrics can make names containing
both periods and underscores collide.

Include a contract version when incompatible consumers must coexist, for
example `tracking.events.v2`. Do not encode a deployment version, transient
host, credential, tenant secret, or unbounded customer identifier in a topic
name. A new topic version requires an explicit dual-write, backfill, consumer
cutover, retention, rollback, and retirement plan. Kafka does not atomically
rename a topic or move consumer-group history to a replacement.

Automatic topic creation is outside this library and is unsafe as a production
deployment mechanism: broker defaults can silently choose the wrong partition
count, replication, retention, or authorization. Provision the topic first,
then inspect its exact metadata and effective configuration.

## Keys are the ordering contract

Kafka orders records only within one partition. Applications that require all
updates for one entity in order must derive one stable byte key from that
entity and preserve it for the lifetime of the ordering contract. The key is
transport metadata, not an application schema owned by this package.

The producer defaults to `KeyRequired`. For automatic keyed records, the
current policy uses franz-go's `UniformBytesPartitioner` with Kafka-compatible
Murmur2 key hashing, adaptive backlog selection for unkeyed records, and a
64-KiB partition-batch target. Equal non-nil key bytes select the same partition
for a stable partition count. Ordering still depends on one live producer path,
idempotence, bounded retries, and the broker accepting the same partition.

Define key encoding precisely:

- use a canonical stable identifier, not a display string or mutable name;
- define byte encoding, normalization, case, and versioning outside this
  package;
- distinguish a nil key from a non-nil zero-length key deliberately;
- avoid sensitive or high-cardinality key material in logs and telemetry;
- measure key distribution and hot partitions with payload-free metrics; and
- make every producer for the topic use the same routing contract.

Changing key bytes or key encoding can split one logical entity across old and
new partitions and destroy its historical ordering scope. Treat the change as
a topic migration unless the application can prove the old and new bytes are
identical for every record.

### Unkeyed records

Unkeyed production is rejected unless the producer selects
`UnkeyedAllowed`. The adaptive partitioner may change the selected partition as
batch and broker backlog state changes. Unkeyed records therefore have no
stable entity-ordering identity and must not be used where replay or consumers
depend on per-entity order.

### Explicit partitions

`ExplicitPartition(n)` bypasses key-based selection. The key is still
transported, but the application now owns partition discovery, range
validation, topology changes, and routing compatibility. A nonexistent
partition fails through the delivery result. Use explicit selection only for a
reviewed Kafka-specific contract, never to simulate global order.

## Partition-count planning

Partition count bounds useful consumer parallelism and the number of
independent ordered logs. More handlers than assigned partitions do not add
ordered throughput. More partitions also increase metadata, open files,
replication work, group assignment work, recovery time, and operational cost.

Plan from measured inputs:

1. peak encoded bytes and records per second in both directions;
2. maximum sustainable throughput for one partition under the required
   acknowledgement, compression, TLS, and handler policy;
3. number and skew of independent ordering keys;
4. required consumer parallelism and slowest downstream capacity;
5. retention bytes per partition, replication, and recovery time; and
6. expected growth plus an explicit expansion threshold.

Do not assume uniform keys. Benchmark the actual distribution and alert on hot
partitions, throttle time, oldest-unprocessed age, and persistent lag.

Kafka can increase but not reduce a topic's partition count. Expansion is not
a transparent capacity toggle:

- key-to-partition mapping can change for new records;
- existing records are not redistributed;
- one key can therefore have history in more than one partition;
- producers and consumers learn the new topology asynchronously; and
- a latest-reset consumer can miss records written to a new partition before
  it discovers that partition.

Freeze writes or introduce a versioned routing/topic migration when continuity
of key order matters. Never alter Kafka internal-topic partition counts.

## Replication, ISR, and acknowledgements

Replication factor is the number of replicas assigned to each partition. The
in-sync replica set (ISR) is the current subset eligible for Kafka's normal
durability and leadership rules. Replication factor is desired topology; ISR
is live health. Neither value alone proves a particular record was accepted.

This package keeps producer idempotence enabled and requests `acks=all`. Kafka
then requires acknowledgements from the current ISR and rejects a write when
the ISR cannot satisfy effective `min.insync.replicas`. A common three-broker
durability profile is replication factor 3 with `min.insync.replicas=2`, but
operators must derive the actual values from failure-domain, availability,
loss, maintenance, and cost requirements.

Review these consequences explicitly:

| State | Client-visible consequence |
| --- | --- |
| RF 3, ISR 3, minimum 2 | all three current ISR replicas acknowledge an `acks=all` write |
| RF 3, ISR 2, minimum 2 | writes can continue with reduced failure margin |
| RF 3, ISR 1, minimum 2 | Kafka rejects writes until ISR recovers or an operator changes policy |
| response lost after append | the package reports an ambiguous outcome; retry requires reconciliation |
| unclean leader elected | acknowledged history can be truncated; exact replay may fail closed |

Keep unclean leader election disabled for durable topics unless the reviewed
availability decision explicitly accepts possible acknowledged-record loss.
Do not lower `min.insync.replicas` automatically during an incident. That is an
operator durability decision, not client retry policy.

Place replicas across real failure domains and monitor leaderlessness, offline
replicas, ISR shrinkage, under-replication, reassignment, and controller state.
The inspector reports bounded snapshots; metadata can change immediately after
the response.

## Retention is not backup

With `cleanup.policy=delete`, Kafka removes eligible closed log segments after
time or per-partition size limits. `retention.ms` is a minimum eligibility age,
not an exact deletion schedule. `retention.bytes` applies independently to each
partition, so total retained storage scales with partition count and
replication. Segment size, roll time, deletion delay, compaction, remote
storage, and broker activity affect when bytes actually disappear.

Choose retention from the longest reviewed recovery and replay interval plus
operational margin. Include expected ingest, compression, partition skew,
replication, maintenance, restore time, and reprocessing capacity. A retained
Kafka log is still not an independent backup: cluster loss, operator deletion,
credential misuse, corruption, or a bad retention change can remove it.

After retention advances a partition's beginning offset, an exact replay that
requests older offsets fails closed. Consumer groups whose committed offset is
gone apply their explicit earliest or latest reset policy; that reset is a data
decision and can duplicate or skip retained work.

## Compaction and tombstones

With `cleanup.policy=compact`, Kafka eventually retains the latest value for
each key in the compacted portion of the log. It does not retain every event,
compact immediately, renumber offsets, provide a point-in-time snapshot by
itself, or make keys unique at write time. Removed records leave observable
offset gaps.

A record with a non-nil key and nil value is a tombstone. `delete.retention.ms`
bounds how long tombstones remain available to a consumer that reconstructs a
state snapshot from the beginning. Consumers must complete that scan within
the reviewed window and still handle duplicates, gaps, and concurrent newer
records.

Use compaction only when the topic contract is latest-value-by-key or another
explicit compacted-log model. Do not enable it on an audit/event-history topic
that promises every offset remains replayable. `cleanup.policy=compact,delete`
combines both behaviors: compaction can remove superseded keyed records and
retention can remove old segments.

The replay reader treats a missing requested in-range offset as
`ErrReplayOffsetGap` and does not advance the checkpoint. Kafka does not expose
whether that individual gap came from compaction or another deletion cause, so
operators must inspect topic policy and history rather than infer a cause.

## Align broker and package size policy

Kafka's `max.message.bytes` limits the encoded record batch accepted by the
topic. The package separately bounds record bytes, headers, batch count,
aggregate batch bytes, encoded fetch responses, decoded batches, and active
decoded buffers. Compression can make a small encoded batch expand to a much
larger decoded allocation, so the broker limit is not a consumer memory bound.

Review together:

- package key, value, header, record, and producer batch limits;
- topic and broker maximum record-batch bytes;
- producer request and delivery bounds;
- consumer per-partition and aggregate fetch targets;
- hard encoded response, decoded batch, and active decoded-buffer limits; and
- downstream retained-copy and handler-concurrency budgets.

Reject oversized records at the earliest owned boundary, but retain broker
rejection as authoritative because encoding and compression affect the final
batch.

## Deployment and inspection checklist

Before enabling traffic:

1. verify topic existence, exact partition count, leaders, replicas, ISR, and
   offline replicas;
2. inspect effective `min.insync.replicas`, cleanup, retention, local
   retention, compaction, segment, remote-storage, and unclean-election policy;
3. confirm the producer allowlist, key policy, record limits, acknowledgements,
   idempotence, compression, and delivery bounds;
4. confirm every consumer group, reset policy, isolation level, fetch limit,
   handler bound, and settlement strategy;
5. exercise one acknowledged record and one durable settlement through the
   actual security identity;
6. dry-run replay ranges against broker beginning and end offsets; and
7. record rollback criteria for ISR loss, hot partitions, authorization,
   ambiguity, lag, and retention drift.

Topic creation, deletion, partition expansion, replica reassignment, ACL
mutation, and configuration changes remain operator responsibilities. The
package's read-only inspector must not be turned into an administrative
control plane.

Primary Apache Kafka references:

- [Topic configuration](https://kafka.apache.org/43/configuration/topic-configs/)
- [Producer configuration](https://kafka.apache.org/43/configuration/producer-configs/)
- [Basic Kafka operations](https://kafka.apache.org/43/operations/basic-kafka-operations/)
- [Kafka design](https://kafka.apache.org/43/design/design/)
- [Tiered storage operations](https://kafka.apache.org/43/operations/tiered-storage/)
