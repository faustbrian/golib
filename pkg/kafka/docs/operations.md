# Operations

## Readiness

`Inspector.DependencyHealth` is a bounded connectivity probe, and `Health` is
its compatibility alias. `Inspector.Readiness` adds consecutive-failure and
recovery hysteresis; use its `Ready` field rather than treating the latest
dependency error as the readiness decision. The default keeps a ready instance
ready through two consecutive failures and requires two consecutive successes
for initial or recovered readiness.

Do not wire a broker outage to process liveness or an automatic restart.
`Inspector.Liveness` only reports whether that inspector is locally open.
Complete process liveness remains application-owned. Readiness must also
inspect every required topic and evaluate partition leadership, offline and
under-replicated state, offsets, replication factor, and
`min.insync.replicas` against deployment policy.

Use `Inspector.Cluster` for cluster/controller/broker identity and
`Inspector.Topics` with explicit targets for topic state. Treat any inspection
error as unknown diagnostic state rather than silently healthy.

The pinned Apache Kafka fixture proves that an RF=3 topic with
`min.insync.replicas=2` remains writable with acks-all after one broker process
stops and an in-sync leader is elected. That evidence does not make the same
claim for an operator's topic, rack placement, network partition, storage
failure, or managed service; readiness must inspect the actual deployment.

## Topic durability and retention

Evaluate `Inspector.Topics` as a combined snapshot of current replica/ISR
state, log bounds, and effective topic configuration. A durable topic policy
normally requires the application to compare replication factor,
`MinInSyncReplicas`, offline replicas, and
`UncleanLeaderElectionEnabled` against its deployment standard.

Cleanup and retention values do not predict an exact deletion instant.
`RetentionBytesPerPartition` is not a topic-wide capacity limit, `-1` means
unlimited, and Kafka deletes eligible closed segments asynchronously.
Compaction settings describe cleaner eligibility rather than a complete key
history. Alert on unexpected policy drift, but use beginning offsets to decide
whether a replay range remains readable. For tiered-storage topics, compare
`LocalRetentionMilliseconds` and `LocalRetentionBytesPerPartition` with their
topic-wide counterparts, and check `RemoteStorageEnabled` plus
`RemoteLogCopyDisabled` before interpreting them. Check each corresponding
visibility field first because older brokers can omit version-dependent
configuration. These fields do not prove that a segment was copied to, retained
in, or deleted from remote storage.

## Lag

Use `Inspector.ConsumerGroupLag` for classic groups and
`Inspector.ConsumerProtocolGroupLag` for KIP-848 consumer-protocol groups, with
explicit group names. Alert on lag, oldest-unprocessed age, handler failure
rate, commit failure rate, and rebalance frequency. Classic assignments or
KIP-848 current, target, and epoch state can identify stalled or unexpectedly
unbalanced members, but membership and lag are a non-atomic snapshot. Do not
export record keys or values.

## Shutdown

Use `Producer.Shutdown` with the service's bounded shutdown context, or handle
the error returned by `Producer.Close`, whose bound is `ShutdownTimeout`.
Timeout fences new production but leaves the client open and admitted records
owned for a retry or explicit `Abort`; do not exit as if shutdown succeeded.

Cancel consumer and replay contexts, wait for the foreground operation, then
close those clients. A canceled consumer exits cleanly; replay cancellation is
an incomplete operator action and must be recorded.

When using `kafkaservice`, place the returned producer component after every
facility it needs so reverse shutdown drains producer calls first. Add the
consumer plan directly to a long-running service command: service task
cancellation stops polling and joins admitted handlers before the consumer
component invokes its transferred shutdown callback. Startup checks,
readiness checks, and shutdown callbacks receive the platform's bounded
contexts; they must not add unbounded retries.

## Recovery

Consumers replay through normal at-least-once group behavior. Audited historical
replay uses `ReplayReader`; never reset production group offsets as a substitute
for an explicit replay plan.

The Apache compatibility fixture stops the Kafka process without replacing its
container, waits for observable leader/ISR changes, and requires every
advertised client endpoint plus the application and transaction-state ISRs to
recover before post-restart delivery. Operational automation should likewise
wait for broker and topic state rather than using a fixed sleep.
