# Operational runbooks

These runbooks separate application actions from Kafka operator actions. The
package never creates topics, changes ACLs, moves replicas, resets group
offsets, or performs cluster failover. Every incident command and mutation
remains owned by the cluster operator and must use the deployment's reviewed
procedure.

## Before deployment

Record and review:

1. the exact application, Go, package, franz-go, broker, authentication
   adapter, and container versions;
2. the expected cluster ID, bootstrap endpoints, topics, consumer groups,
   transactional IDs, security mode, and credential source;
3. topic replication factor, `min.insync.replicas`, cleanup policy, retention,
   compaction, unclean-election policy, and current beginning/end offsets;
4. producer delivery, transaction, consumer handler, commit, rebalance, and
   shutdown bounds;
5. expected peak record rate and bytes, partition count, maximum record and
   batch sizes, consumer concurrency, and replay concurrency;
6. rollback criteria and the last compatible application version; and
7. owners for application rollback, Kafka operations, credentials, network,
   and downstream side effects.

Run configuration validation before constructing clients. Readiness must probe
the dependency with hysteresis and inspect every required topic; liveness must
remain independent of Kafka availability. Do not proceed when the cluster ID,
topic durability, authentication mode, or tested compatibility identity differs
from the reviewed deployment.

## Rolling application deployment

### Producers

1. Give every process a stable `ClientID`. Give every transactional producer
   or transaction processor a unique instance-scoped `TransactionalID`.
2. Start the replacement and wait for bounded dependency readiness before
   sending it traffic.
3. Stop new work on the old process. Let its service boundary drain admitted
   publishes.
4. Call `Producer.Shutdown` with the configured bound and require success. An
   incomplete drain is a deployment failure, not permission to exit.
5. For an ambiguous delivery or transaction outcome, stop the affected
   workflow and reconcile Kafka state before retrying or replacing the client.

Never run two live transactional producers with the same identity as a normal
rollout technique. The replacement fences the old producer; use that behavior
only as the documented recovery boundary.

### Consumer groups

For an existing eager group moving to cooperative balancing, use two complete
deployments:

1. deploy every member with `BalanceEagerToCooperative`;
2. wait until every old eager-only member has left and assignments are stable;
3. deploy every member with `BalanceCooperativeSticky`; and
4. verify one-copy partition ownership and advancing committed offsets after
   each phase.

Do not mix eager-only and cooperative-only members directly. For ordinary
cooperative deployments, stop intake, let admitted handlers finish or honor
the selected rebalance cancellation policy, settle only successful owned
prefixes, and then close. A stale member must never be allowed to turn a
generation error into a successful settlement claim.

Static `InstanceID` values must be unique per live process and stable only for
the intended member identity. A duplicate live identity is terminal and
requires the old member to leave or expire before replacement.

## Graceful shutdown

Use this order:

1. fail new application admissions;
2. cancel foreground consumer, transaction-processor, or replay contexts;
3. join their admitted callbacks and capture incomplete replay checkpoints;
4. drain producer admissions and deliveries;
5. close inspector and authentication-owned resources; and
6. exit only after every required close succeeds or an explicit operator
   decision accepts the documented loss or duplication risk.

`Close` is bounded but not a success-only convenience. Record and surface its
error. A producer shutdown timeout leaves admitted records owned by the client
so the caller can retry shutdown or explicitly call `Abort`. Do not abandon
that process while claiming a clean deployment.

## Broker outage or leader election

Symptoms include dependency-health failures, request timeouts, retryable
delivery failures, ISR shrinkage, offline replicas, leader changes, and rising
lag.

1. Keep process liveness healthy. Let readiness hysteresis absorb brief
   failures rather than creating a restart storm.
2. Inspect cluster identity, controller visibility, topic leaders, replicas,
   ISR, offline replicas, and beginning/end offsets with bounded calls.
3. Stop or shed new application work when bounded producer buffers or consumer
   lag approach reviewed limits.
4. Preserve ambiguous producer results and unknown transaction outcomes for
   reconciliation. Do not convert them into definite failure.
5. Wait for observable endpoint, leader, ISR, and application-level recovery;
   fixed sleeps are not recovery evidence.
6. Escalate replica movement, broker restart, storage repair, or controller
   action to the Kafka operator.

An RF=3 topic with `min.insync.replicas=2` can tolerate one unavailable in-sync
replica only when the remaining topology and broker configuration actually
match that policy. The package's fixture is not proof for another cluster.

## Ambiguous producer delivery

1. Stop blind application retry for the affected logical message.
2. Retain the message's application identity and the returned stable error
   category. Do not retain or publish credentials or payload diagnostics.
3. Query the authoritative application reconciliation boundary. Kafka does not
   provide a generic lookup by producer key or application message ID.
4. If the record is known present, continue from that fact. If it is known
   absent and the producer remains safe to use, apply the application's explicit
   duplicate policy before retrying.
5. If presence cannot be proven, preserve the ambiguity and require a human or
   domain-specific idempotent recovery decision.

For an unknown transaction commit, stop the workflow and reconcile only the
Kafka read-process-write boundary. Do not infer the state of databases, HTTP
calls, object storage, email, or any other external effect.

## Consumer lag or rebalance storm

1. Compare committed lag, end offsets, oldest-unprocessed age, processing
   duration, failure rate, commit failures, poll cadence, and rebalance events.
2. Inspect assignments and member identities. Treat group membership and lag
   as non-atomic diagnostic snapshots.
3. Check whether one failed partition is blocking only its own contiguous
   settlement or whether a shared dependency is failing every handler.
4. Confirm `HandlerTimeout + CommitTimeout + HeartbeatInterval` remains below
   `RebalanceTimeout` and that handlers honor cancellation.
5. Scale only across available partitions. More handlers than independent
   partitions cannot increase ordered processing capacity.
6. Pause reviewed partitions or stop intake before memory, downstream, or
   retry budgets are exceeded. Resume only after the limiting dependency has
   recovered.
7. Never skip a poison record by committing a later offset. Select an explicit
   retry, retry-topic, dead-letter, stop, or application-delegated policy.

## Authentication or credential rotation failure

1. Distinguish TLS trust, hostname, mTLS identity, SASL authentication, and
   Kafka/IAM authorization failures; do not collapse them into connectivity.
2. Verify only credential metadata: provider invocation, expiry, selected
   authentication method, and redacted category. Never print the secret,
   token, certificate, signed URL, broker URL containing credentials, or
   provider error text.
3. Keep old and new trust roots or principals overlapping until every required
   client has reconnected successfully.
4. Roll back the provider result when the old credential remains authorized;
   otherwise stop new work and repair the server-side identity or policy.
5. For PLAIN, remember that the tested Kafka login module requires a listener
   or broker restart for verifier changes. Use a second principal or an
   operator-owned callback handler for a reviewed zero-downtime design.
6. For MSK IAM, verify task or workload role credentials, region, cluster IAM
   mode, IAM bootstrap brokers, and exact resource policy. A signed token is not
   proof of authorization.

## Replay incident and recovery

1. Cancel the replay and persist the returned `ReplayResult.Checkpoint` in an
   external durable store.
2. Record every requested range, processed, skipped, failed, incomplete, and
   next offset. Never summarize multiple partitions as globally ordered.
3. Re-plan against the broker before resuming. A local plan does not prove
   retention.
4. On a retention, compaction, or truncation gap, stop. Do not advance the
   checkpoint or substitute the next available offset for the missing one.
5. Review side effects independently. A successful Kafka replay callback does
   not imply that an external side effect is exactly once.
6. Construct a new single-use reader with the reviewed ranges and external
   checkpoint only after the operator approves resume.

## Disaster recovery and cross-cluster failover

This package does not replicate clusters, translate offsets, elect a recovery
region, or prove remote-store durability. Kafka recommends local clients per
site with operator-managed cross-cluster mirroring. A DR plan must define:

- which topics, configurations, ACLs or IAM policies, schemas, and transaction
  identities are reproduced;
- replication point objective and observed mirror lag;
- how application message identity and idempotency survive failover;
- how source offsets map to the recovery cluster, or why consumers start from
  an explicitly reviewed timestamp or offset;
- how transactional producers receive unique recovery identities;
- how DNS, bootstrap endpoints, TLS trust, credentials, and cluster-ID
  allowlists change; and
- the failback procedure and split-brain prevention rule.

Before cutover, stop or fence writers as required by the replication design,
record final source offsets, verify recovery-topic durability and log bounds,
and dry-run consumer/replay plans. After cutover, prove a new acknowledged
record, one idempotent consumed side effect, committed offsets, lag recovery,
and clean shutdown. Never copy consumer-group offsets between clusters without
an operator tool and an independently verified semantic mapping.

## Capacity planning and tuning

Measure rather than infer from defaults. At minimum record:

- peak and percentile record bytes, records per second, compression ratio, and
  partition distribution;
- producer buffer bytes and records, batch bytes, linger, request duration,
  retry rate, throttle time, and delivery latency;
- consumer fetch bytes, decoded-batch bytes, active decoded-buffer bytes,
  handler duration, commit duration, lag, and rebalance cost;
- transaction records, bytes, duration, abort rate, and coordinator failures;
- replay fetch and handler concurrency, remaining offsets, and side-effect
  capacity; and
- process heap, allocations, goroutines, connections, GC, CPU, and shutdown
  duration during steady state and reconnect.

Budget producer memory from both `MaxBufferedBytes` and `MaxBufferedRecords`.
Budget consumer/replay memory from the encoded broker response limit, maximum
decoded batch, active decoded-buffer limit, retained application copies, and
concurrent handlers. Kafka may return one record batch larger than the
per-partition fetch target to make progress, so the hard broker-read and decoded
limits remain necessary.

Partition count bounds useful ordered concurrency. Size topics and retention
from measured encoded throughput, replication, compaction, segment behavior,
replay horizon, and recovery margin; `retention.bytes` is per partition. Test
changes against equivalent acknowledgement, idempotence, compression,
partitioning, security, and commit settings before rollout.

## Dashboards, alerts, and SLOs

Use bounded identity allowlists. Do not export keys, values, arbitrary headers,
credentials, signed tokens, or unbounded topic/group labels.

Recommended dashboard groups are:

| Group | Signals |
| --- | --- |
| Dependency | readiness state, consecutive failures/recoveries, request latency, connection failures |
| Topic durability | leaders, offline replicas, ISR size, beginning/end offsets, configuration drift |
| Producer | admitted/buffered records and bytes, delivery duration/category, retry, throttle, ambiguous and fatal outcomes |
| Consumer | processing and commit duration/category, committed lag, oldest age, paused partitions, assignments, rebalance events |
| Transaction | begin/commit/abort duration, fencing, abort-required errors, unknown outcomes, processor redelivery |
| Replay | planned/processed/failed/incomplete ranges, next offsets, gaps, handler duration |
| Runtime | heap, allocations, goroutines, connections, CPU, GC, shutdown duration |

Define service-level objectives from application harm, such as acknowledged
delivery latency, oldest durable unprocessed age, bounded ambiguous outcomes,
and successful graceful shutdown. Alert immediately on offline replicas,
authorization or fencing, fatal clients, unknown transaction outcomes, replay
gaps, and sustained no-progress lag. Alert on readiness only after the configured
hysteresis threshold; a single failed probe is diagnostic, not an outage.

Primary operational references:

- [Apache Kafka monitoring](https://kafka.apache.org/43/operations/monitoring/)
- [Apache Kafka basic operations](https://kafka.apache.org/43/operations/basic-kafka-operations/)
- [Apache Kafka datacenter guidance](https://kafka.apache.org/43/operations/datacenters/)
