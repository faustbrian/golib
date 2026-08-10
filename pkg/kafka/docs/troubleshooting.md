# Troubleshooting, FAQ, and glossary

Start with the stable error category and the operation that returned it. Do not
diagnose by matching error text. Preserve `errors.Is` and `errors.As` identity
locally, but do not export an unwrapped application, provider, or broker error
without reviewing it for secrets and payload data.

## Triage table

| Symptom | First checks | Required response |
| --- | --- | --- |
| constructor fails | call `Validate`; inspect bounded identifiers, duplicates, topic policy, timeout relationships, limits, security composition | correct configuration before allocating a client |
| dependency health fails | cluster ID, endpoints, DNS, TLS/SASL mode, authorization, request deadline | keep liveness independent; use readiness hysteresis and bounded retry outside the probe |
| produce returns timeout or `ErrorAmbiguous` | whether admission occurred, delivery category, broker/ISR state, application message identity | stop blind retry and reconcile possible broker acceptance |
| producer is fatal | diagnostic fatal category, data-loss detection, transactional delivery expiry | close and replace only through the documented recovery path |
| batch partially fails | inspect every input-ordered `DeliveryResult` | preserve definite successes; reconcile ambiguous entries; do not resend the whole batch blindly |
| consumer repeats records | handler, commit, rebalance, process exit, or response ambiguity before settlement | make side effects idempotent; confirm contiguous commit state |
| later record is not committed | an earlier failure in the same partition | resolve the failed record; never commit past it |
| consumer returns no records | assignment, group state, reset policy, committed offsets, pause state, topic end offsets | verify the member owns partitions and the requested history exists |
| rebalance repeats or stalls | handler cancellation, deadline relationship, callback duration, static IDs, mixed protocols | return from handlers, fix identities, and use the two-phase balance migration |
| transaction commit is unknown | `TransactionError.OutcomeKnown`, category, coordinator/broker fault | stop the workflow and reconcile read-committed output plus source offsets |
| replay fails before handling | broker start/end offsets, compaction, truncation, checkpoint, exact range | keep checkpoint unchanged and review a new explicit range |
| memory grows under consume/replay | encoded response, decoded-batch and active-buffer limits, retained copies, concurrency | stop intake, find retained ownership, lower bounded concurrency or bytes |
| authentication fails after rotation | provider invocation/expiry, trust overlap, hostname, SASL method, server verifier, IAM policy | restore overlap or repair server policy without exposing credentials |
| readiness flaps | thresholds, probe timeout, broker latency, topic durability drift | tune from observed outage behavior; never convert to liveness restart |

## Producer questions

### Does cancellation prove that Kafka did not accept a record?

No. Cancellation can stop waiting or cancel an admitted non-transactional
record, but a broker acknowledgement may have been lost. Inspect the returned
delivery category and reconcile through an application identity before retry.

### Why are keys required by default?

A non-empty key makes the ordering identity deliberate. Kafka ordering is only
within one partition. Unkeyed production requires `UnkeyedAllowed`; it does not
gain a global ordering guarantee.

### Why can a successful batch contain multiple partitions but no global order?

Kafka appends independently per partition. The package restores results to
input order for reconciliation, but that presentation order does not create a
broker-wide sequence.

### Why did shutdown return `ErrDrainIncomplete`?

The bound expired while admitted work remained. New production is fenced, but
the client retains ownership so shutdown can be retried. Call `Abort` only
after explicitly accepting potential data loss.

## Consumer questions

### Is Kafka a queue with nack and visibility timeout?

No. Kafka retains an ordered partition log and tracks consumer-group offsets.
A failed record remains before the next committable offset. Retry topics and
dead-letter topics are separate Kafka records and publications, not nacks.

### When is an offset committed?

Only after the handler succeeds and the package establishes a contiguous
successful prefix for that partition. A handler call alone is not settlement.
Commit timeout, rebalance, process death, or lost ownership can cause
redelivery.

### Can one failed partition block every other partition?

No by policy when independent successful prefixes can be committed safely.
The failed partition stops at its first failure; successful independent
partitions may settle. A shared commit or group failure can still affect the
poll as a whole.

### Can I increase handler concurrency without changing behavior?

Only across independent partitions. One partition remains sequential. The
handler must be concurrency-safe and downstream capacity must tolerate the new
parallelism.

### Why is an unset reset policy rejected?

Starting at earliest or latest history is a data decision. Requiring an
explicit value prevents a zero-value deployment from silently skipping or
replaying retained records.

## Transaction questions

### Does idempotent production provide exactly once processing?

No. Producer idempotence controls duplicate appends within the producer's Kafka
session. It does not atomically include a consumer offset or external side
effect.

### What does the transaction processor make atomic?

Only Kafka source offsets and Kafka output records in one compatible
read-process-write transaction, observed through `read_committed`. PostgreSQL,
HTTP, object storage, email, and other systems are outside that boundary.

### Can I reuse one transactional ID across replicas?

Not concurrently. Kafka fences the older producer. Assign one stable unique ID
to each live producer or processor instance and retain it only for the intended
recovery identity.

## Replay questions

### Does replay join or change a consumer group?

No. It directly assigns explicit topic partitions and never commits, resets, or
deletes group offsets.

### Why does replay fail on one missing in-range offset?

The requested exact range cannot be proven after retention, compaction, or
truncation. The reader fails closed so an audit does not silently omit history.

### Is replay globally ordered?

No. It is ascending within each partition. Cross-partition handlers may run in
parallel when explicitly configured.

### Where is replay progress stored?

The package returns exact progress and an owned checkpoint. The application
must persist it externally and supply it to a new reader when resuming.

## Inspection and security questions

### Why can inspection return successful and failed targets together?

Typed batch methods isolate targets under one deadline so operators can retain
valid state while seeing explicit partial failure. The aggregate error requires
the caller to inspect every input-ordered result.

### Is readiness proof that producing will succeed?

No. It is a bounded dependency policy with hysteresis. Topic ISR can change
immediately, authorization can differ by operation, and producer acknowledgement
remains authoritative for a record.

### Why is plaintext configuration named development-only?

The production zero value is verified TLS. A visibly separate constructor
prevents an empty or forgotten field from silently disabling encryption.

### Can I log an unwrapped provider error for diagnosis?

Only after application-owned redaction and review. Provider errors may contain
tokens, URLs, credential IDs, certificate data, or server response text. Stable
package categories are the safe default diagnostic surface.

## Common diagnostic sequence

1. Identify the exact client role, operation, topic partition or group, and
   bounded context deadline.
2. Capture the stable package error category and local diagnostic snapshot.
3. Probe dependency health and inspect cluster/topic/group state separately.
4. Compare current broker, module, franz-go, security adapter, and deployment
   identities with the compatibility record.
5. Check delivery or settlement at Kafka and the application-owned durable
   identity boundary.
6. Choose the relevant runbook. Do not mutate topics, ACLs, offsets, or broker
   configuration from application recovery code.

## Glossary

**Acknowledgement (ack)**
The broker response for a produce request under the configured replica policy.
It does not prove consumer processing.

**Ambiguous outcome**
An operation may have completed at Kafka, but the client cannot establish the
result. Absence of an acknowledgement is not proof of absence.

**Assignment**
The topic partitions currently owned by one consumer-group member.

**Beginning offset**
The earliest offset Kafka currently exposes for a partition. It can advance
with retention or deletion.

**Commit**
For a consumer group, storing the next offset to process. For a Kafka
transaction, making its records and offsets visible to `read_committed` readers.

**Compaction**
Kafka cleanup that may remove older records for a key while preserving offset
positions as gaps.

**Contiguous settlement**
Committing only the successful prefix before the first failed record in one
partition.

**Consumer group**
An independent named consumption history whose members share partition
ownership.

**End offset / high watermark**
The exclusive replicated log boundary reported for a partition. A
`read_committed` consumer can temporarily stop earlier at the last stable
offset while a transaction remains unresolved.

**Fencing**
Kafka or package rejection of an obsolete producer, member, generation, or
lifecycle owner.

**ISR**
The in-sync replica set eligible for the configured acknowledgement and
leadership policy.

**Leader epoch**
Kafka metadata identifying a partition leadership generation; it is not a
consumer-group generation.

**Offset**
A partition-local record position. Offsets do not create order across
partitions.

**Read committed**
Consumer isolation that hides aborted and still-pending transactional records.

**Rebalance**
Consumer-group partition ownership transition. It is a correctness boundary
for handler cancellation, draining, and settlement.

**Replay**
Direct, explicit reading of reviewed partition ranges without consumer-group
offset mutation.

**Retention**
Broker policy that eventually removes eligible log segments. It is not an exact
deletion schedule.

**Static membership**
A consumer-group member identity intended to survive process restarts. A
duplicate live instance ID is fenced.

**Transactional ID**
Kafka producer identity used for transaction coordination and fencing. It must
be unique to one live owner.
