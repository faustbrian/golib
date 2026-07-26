# Consumer groups and rebalances

The package consumer is Kafka-specific and at least once. It joins one explicit
consumer group, subscribes to explicit topics, disables automatic commits, and
blocks franz-go rebalances while one bounded poll is handled and settled. It is
not a queue-style worker with nack or visibility-timeout semantics.

## Configuration

`ConsumerConfig` requires brokers, client ID, group ID, topics, and an earliest
or latest reset policy. Construction validates all policy before franz-go
allocates the client. Fetch concurrency, aggregate bytes, per-partition bytes,
poll records, fetch wait, session, rebalance, heartbeat, handler, commit, and
dial durations are bounded.

`InstanceID` and `Rack` are optional UTF-8 identifiers of at most 255 bytes.
Whitespace padding, control characters, invalid UTF-8, and oversized values
fail construction.
Instance IDs must be deployment-unique within a group. Rack values must match
the broker deployment's rack naming convention.

## Balance policy and rolling deployment

`BalanceCooperativeSticky` is the zero value and safe default for a new group.
It incrementally revokes moved partitions rather than requiring every member to
revoke every assignment. This follows
[KIP-429](https://cwiki.apache.org/confluence/display/KAFKA/KIP-429%3A%2BKafka%2BConsumer%2BIncremental%2BRebalance%2BProtocol).

Do not introduce a cooperative-only member directly into an existing eager
group. Use two complete rolling deployments:

1. configure every member with `BalanceEagerToCooperative`, which advertises
   eager sticky first and cooperative sticky second;
2. after every old eager-only member has gone, configure every member with
   `BalanceCooperativeSticky`.

`BalanceEagerSticky` keeps a group on eager rebalancing when compatibility
requires full revocation. Reversing an established cooperative group to eager
is not presented as a safe rolling operation.

## Static membership and rack awareness

Setting `InstanceID` opts into Kafka static membership. A normal client close
does not explicitly leave that static member, so a restart using the same ID
can rejoin within the session timeout without forcing an immediate rebalance.
Removing a static member is an explicit Kafka administrative operation. A
duplicate live ID can be fenced by the broker.
See [KIP-345](https://cwiki.apache.org/confluence/display/KAFKA/KIP-345%3A%2BIntroduce%2Bstatic%2Bmembership%2Bprotocol%2Bto%2Breduce%2Bconsumer%2Brebalances)
for the broker protocol and administrative model.

Setting `Rack` asks compatible brokers to prefer an eligible replica in that
rack. It does not create rack metadata, place replicas, or guarantee a local
replica. Infrastructure owns broker rack configuration and replica placement.
See [KIP-392](https://cwiki.apache.org/confluence/display/KAFKA/KIP-392%3A%20Allow%20consumers%20to%20fetch%20from%20closest%20replica).

## Processing, settlement, and redelivery

Records remain sequential within a partition. A handler success is required
before settlement. At the first failure in one partition, later records from
that partition in the current poll are skipped. Its successful prefix and
successful independent partitions are committed together. A failed commit has
an ambiguous per-partition broker outcome, leaves `PollResult.Committed` at
zero, and may redeliver records whose side effects already completed.

Handlers must be idempotent and honor their context deadline. Retain a consumed
record before storing its bytes beyond the handler call. The current runner is
single-threaded and does not yet expose pause, drain, batch handling, or bounded
cross-partition worker concurrency; these remain pre-v1 completion work.

## Ownership boundaries

Kafka owns the group generation, assignments, offsets, retention, and broker
acknowledgements. franz-go implements the group protocol and heartbeats. This
package owns configuration, bounded polling, handler invocation, contiguous
settlement, and the exposed lifecycle policy. The application owns durable side
effects, idempotency, poison-record decisions, and deployment-specific IDs.
