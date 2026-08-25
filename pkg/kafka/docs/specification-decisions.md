# Kafka specification decisions

This register records observable choices where Apache Kafka, implemented Kafka
Improvement Proposals, franz-go, or compatible-service behavior permits more
than one safe interpretation. Kafka protocol and broker semantics outrank
client convenience. Exact source revisions, image digests, and tool versions
are pinned in [`specification/manifest.json`](../specification/manifest.json).

Statuses are `resolved`, `unresolved`, or `superseded`. A resolved decision is
part of the pre-v1 compatibility contract and requires public API, security,
resource, wire, broker-evidence, adapter, and changelog review when changed.

## KAFKA-DEC-001: Protocol implementation and version negotiation boundary

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; protocol-boundary and compatibility policy. |
| Source and issue | [franz-go v1.21.5](https://github.com/twmb/franz-go/tree/1ba5fd24f949a335dbc7caaef1d6037e132ef23e) implements Kafka requests, while Kafka's [protocol guide](https://kafka.apache.org/protocol.html) permits per-broker API negotiation. Reimplementing requests creates a second protocol stack, but raw franz-go options bypass package policy. |
| Credible interpretations and known peer behavior | Wrap every upstream type, expose raw options, pin one broker version, or own a narrow policy and translate internally. Peer clients expose different negotiation floors and retry defaults; upstream feature availability is not operational support. |
| Selected behavior | Public APIs own Kafka concepts and never expose `kgo.Client`, `kgo.Record`, `kgo.Opt`, or `kadm` responses. franz-go owns wire negotiation. `MinimumVersion` is a request downgrade floor, not a broker-support claim, and transactions require the KIP-447-safe floor. |
| Security and resource consequences | The security consequence is that no raw option can disable verified transport or validation. The resource consequence is one bounded client implementation rather than duplicated encoders, connections, and retry loops. |
| Compatibility and wire consequences | The compatibility consequence is a stable package surface across reviewed franz-go upgrades. The wire consequence remains the pinned franz-go Kafka encoding and negotiated API set. |
| Executable evidence and public surface | `TestClientRolesApplyProtocolAndBoundedEOFRecoveryPolicy` and `TestClientRolesRejectInvalidProtocolPolicy` cover `ProtocolPolicy` and every client constructor. |
| Upstream record and reconsideration | The upstream record is the pinned franz-go source and Kafka protocol guide. Reconsider when franz-go removes a required bounded control or Kafka changes negotiation semantics. |

## KAFKA-DEC-002: Producer durability, partitioning, and byte ownership

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; producer delivery, ordering, and ownership policy. |
| Source and issue | Kafka's [producer configuration](https://kafka.apache.org/43/configuration/producer-configs/) distinguishes acknowledgements, idempotence, retries, in-flight requests, partitioning, and delivery timeout. A lost response cannot reveal acceptance, and poll-owned or caller-owned bytes can alias after return. |
| Credible interpretations and known peer behavior | Treat timeout as definite failure, retry in application code, always permit null keys, copy nothing, or make ambiguity, key policy, and retention explicit. Peer defaults and unkeyed partitioners differ. |
| Selected behavior | Idempotence, all-ISR acknowledgement, ordering-safe in-flight policy, bounded buffering, batching, retries, and delivery time are mandatory defaults. Key-required production is safe by default; unkeyed and explicit partitions require policy. Pre-admission cancellation prevents delivery; admitted work may outlive the caller wait and ambiguous outcomes remain explicit. Producer input is owned; consumed bytes are borrowed only during the handler and `Retain` copies them. |
| Security and resource consequences | The security consequence is no alias-based payload disclosure or value rendering. The resource consequence is exact topic, record, header, batch, buffer, retry, callback, drain, and shutdown bounds before allocation or retention. |
| Compatibility and wire consequences | The compatibility consequence is no silent weakening when franz-go defaults change. The wire consequence is Kafka-native keyed or explicit partition records, ordered headers, timestamps, and per-record delivery metadata with partition-local ordering only. |
| Executable evidence and public surface | `TestNewProducerAppliesBoundedIdempotentDeliveryPolicy`, `TestProducerBoundsDeliveryContextsAndDetachesAdmittedAsyncRecord`, `TestProducerPublishRecordReturnsDeliveryMetadataAndOwnsInput`, and `TestConsumedRecordRetainOwnsAllRecordBytes` cover `Producer`, `ProducerConfig`, `ProducerRecord`, `ConsumedRecord`, and `DeliveryResult`. |
| Upstream record and reconsideration | The upstream record is Kafka's producer and partition-order contract plus franz-go record ownership. Reconsider when Kafka supplies definitive acceptance identity or a versioned public partitioner is added. |

## KAFKA-DEC-003: Consumer settlement and rebalance ownership

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; at-least-once group and rebalance policy. |
| Source and issue | Kafka's [consumer design](https://kafka.apache.org/43/design/#theconsumer) and [KIP-429](https://cwiki.apache.org/confluence/display/KAFKA/KIP-429%3A%2BKafka%2BConsumer%2BIncremental%2BRebalance%2BProtocol) separate fetch, handler success, group ownership, and committed offsets. A later success cannot make an earlier partition failure safe. |
| Credible interpretations and known peer behavior | Auto-commit fetched offsets, commit every success independently, block every partition after one failure, or settle each partition's contiguous successful prefix. Peer clients expose different callback and commit timing. |
| Selected behavior | Durable handlers disable automatic commits. Processing is sequential within a partition and bounded across partitions. Handler success precedes settlement, and no local assignment epoch settles after ownership loss. Cooperative balancing is default, eager balancing explicit, and blocked callbacks cancel or drain under a bounded policy. |
| Security and resource consequences | The security consequence is that stale members cannot acknowledge records they no longer own. The resource consequence is bounded polling, fetch decoding, workers, handler time, rebalance wait, commit, pause state, drain, and shutdown. |
| Compatibility and wire consequences | The compatibility consequence is explicit at-least-once redelivery on handler, process, timeout, commit, or rebalance ambiguity. The wire consequence is monotonic contiguous committed offsets per owned topic partition. |
| Executable evidence and public surface | `TestConsumerRunOnceCommitsOnlyContiguousPartitionSuccess`, `TestConsumerFencesSettlementAfterAssignmentEpochChanges`, and `TestApacheKafkaConsumerOwnershipTransitions` cover `Consumer`, `ConsumerConfig`, `Handler`, `BatchHandler`, and balance policies. |
| Upstream record and reconsideration | The upstream record includes KIP-429, KIP-345 static membership, KIP-392 rack-aware fetches, and franz-go callbacks. Reconsider when the package adopts a separately proven KIP-848 runner. |

## KAFKA-DEC-004: Retry-topic and dead-letter effects

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; failure-handling and settlement policy. |
| Source and issue | Kafka retains records and has no queue-style nack or visibility timeout. Kafka's [delivery semantics](https://kafka.apache.org/43/design/#deliverysemantics) do not make retry/dead-letter publication atomic with source offset settlement unless both occur in one Kafka transaction. |
| Credible interpretations and known peer behavior | Retry forever in process, skip poison records, publish then always commit, or expose each effect and its duplicate/loss window. Queue libraries often import a nack model that Kafka does not provide. |
| Selected behavior | Stop, bounded in-process retry, versioned retry topic, dead-letter topic, and application delegation are explicit. Publication needs exact definite per-record success before settlement. Partial, failed, canceled, or ambiguous publication leaves the source unsettled; transaction users may compose both effects atomically. |
| Security and resource consequences | The security consequence is payload-free error metadata and explicit sensitive-header policy. The resource consequence is bounded attempts, backoff, batch bytes, publication, callbacks, and diagnostics with no poison loop. |
| Compatibility and wire consequences | The compatibility consequence is a documented at-least-once duplicate window outside Kafka transactions. The wire consequence preserves source topic, partition, offset, key, timestamp, ordered headers, attempts, classification, and bounded correlation metadata. |
| Executable evidence and public surface | `TestFailureHandlerPublishFailureDoesNotResolveSource`, `TestFailureHandlerPublishesOwnedDeadLetterMetadata`, and `TestConsumerSettlesOnlyDefiniteFailurePublication` cover `FailureHandlerConfig`, `BatchFailureHandlerConfig`, and narrow publisher contracts. |
| Upstream record and reconsideration | The upstream record is Kafka's retention and transaction model. Reconsider only through a versioned Kafka transaction composition, never a generic nack abstraction. |

## KAFKA-DEC-005: Kafka-scoped transactions and exactly-once claims

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; transaction and exactly-once boundary policy. |
| Source and issue | [KIP-98](https://cwiki.apache.org/confluence/display/KAFKA/KIP-98%2B-%2BExactly%2BOnce%2BDelivery%2Band%2BTransactional%2BMessaging) and [KIP-447](https://cwiki.apache.org/confluence/display/KAFKA/KIP-447%3A%2BProducer%2Bscalability%2Bfor%2Bexactly%2Bonce%2Bsemantics) define producer fencing and atomic source-offset/output commit, but exclude databases, HTTP, object storage, and notifications. |
| Credible interpretations and known peer behavior | Call idempotent production exactly-once, expose a generic transaction, hide commit ambiguity, or restrict the claim to read-process-write inside Kafka. Framework peers sometimes overstate external atomicity. |
| Selected behavior | Transactions require unique IDs, single ownership, bounded begin/produce/send-offsets/commit/abort/close, read-committed inputs, and callback lifetime fencing. Unknown commit or delivery outcomes remain ambiguous and fatal where safe reuse is unproved. Exactly-once language is restricted to compatible Kafka read-process-write effects. |
| Security and resource consequences | The security consequence is transactional-ID authorization and redacted fencing identity. The resource consequence is bounded transaction duration, output bytes, polling, cleanup, and single-owner concurrency. |
| Compatibility and wire consequences | The compatibility consequence is no cross-system atomicity claim. The wire consequence is Kafka transaction markers plus source offsets sent with owned group metadata. |
| Executable evidence and public surface | `TestApacheKafkaMinimumSupportedTransactions`, `TestRunTransactionRedactsUnknownCommitOutcome`, and `TestTransactionProcessorCommitsCompletePollAtomically` cover `Transaction`, `Producer.RunTransaction`, and `TransactionProcessor`. |
| Upstream record and reconsideration | The upstream record is KIP-98, KIP-447, and franz-go transaction source. Reconsider when Kafka changes fencing, group metadata, or commit-outcome semantics. |

## KAFKA-DEC-006: Exact direct-partition replay

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; replay range and side-effect policy. |
| Source and issue | Kafka's [log design](https://kafka.apache.org/43/design/#thelog) permits retention, compaction, and truncation, so an in-bounds numeric range may contain a missing offset. Group offset mutation would also alter an independent application history. |
| Credible interpretations and known peer behavior | Reset a group, skip missing offsets, report only the final offset, or directly assign exact inclusive-start/exclusive-end ranges and fail closed. Administrative tools often trade exactness for convenience. |
| Selected behavior | Replay never joins or mutates a group. Plans use explicit partitions, offsets or broker timestamps, external checkpoints, exact range preflight, no-reset fetching, partition-local ascending order, optional bounded cross-partition concurrency, and explicit side-effect opt-in. Any unsatisfied offset returns incomplete progress. |
| Security and resource consequences | The security consequence is explicit authorization and side-effect consent without payload telemetry. The resource consequence is bounded ranges, fetches, decoded records, handlers, progress, diagnostics, cancellation, and shutdown. |
| Compatibility and wire consequences | The compatibility consequence is no global-order or exactly-once side-effect claim. The wire consequence is ordinary Kafka fetches at exact topic-partition offsets without commits. |
| Executable evidence and public surface | `TestInspectorPlansReplayTimestampWindowAsOwnedExactRanges`, `TestApacheKafkaReplayCompactionGapCompatibility`, and `TestApacheKafkaReplayFailsClosedAfterUncleanElectionTruncation` cover `ReplayPlan`, `ReplayReader`, `ReplayCheckpoint`, and `ReplayResult`. |
| Upstream record and reconsideration | The upstream record is Kafka log, compaction, retention, and fetch behavior. Reconsider when Kafka exposes an authoritative deletion-cause API for individual offsets. |

## KAFKA-DEC-007: Read-only inspection and health separation

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; metadata, group-protocol, and health policy. |
| Source and issue | Kafka's [administrative protocol APIs](https://kafka.apache.org/41/design/protocol/) expose partial resource results, while [KIP-848](https://cwiki.apache.org/confluence/display/KAFKA/KIP-848%3A%2BThe%2BNext%2BGeneration%2Bof%2Bthe%2BConsumer%2BRebalance%2BProtocol) uses state distinct from classic groups. Dependency outage, local liveness, and readiness are different signals. |
| Credible interpretations and known peer behavior | Collapse every error, normalize classic and KIP-848 groups, fail liveness on broker outage, or keep typed partial inspection and health roles separate. Admin clients often expose mutation beside reads. |
| Selected behavior | Inspection returns bounded owned Kafka metadata, preserves input-ordered partial failures, separates classic and KIP-848 APIs, and exposes liveness, dependency health, and hysteretic readiness independently. Production APIs perform no topic, ACL, group, offset, partition, or broker mutation. |
| Security and resource consequences | The security consequence is least-privilege read-only access and redacted authorization failures. The resource consequence is bounded fan-out, targets, metadata, assignments, offsets, lag arithmetic, copying, deadlines, and diagnostics. |
| Compatibility and wire consequences | The compatibility consequence is no invented common state across group protocols. The wire consequence preserves Kafka cluster, controller, leader, replica, ISR, offline, configuration, committed-offset, and lag semantics. |
| Executable evidence and public surface | `TestApacheKafkaConsumerProtocolInspection`, `TestInspectorReadinessUsesHysteresisWithoutAffectingLiveness`, and `TestInspectorPreservesRequestAndPartitionFailures` cover `Inspector`, group states, and health policies. |
| Upstream record and reconsideration | The upstream record is Kafka admin protocol behavior and KIP-848. Reconsider when Kafka adds another group protocol or authoritative health primitive. |

## KAFKA-DEC-008: Verified transport and rotating authentication

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; transport, authentication, and credential-lifetime policy. |
| Source and issue | Kafka's [authentication guidance](https://kafka.apache.org/43/security/authentication-using-sasl/) supports TLS, PLAIN, SCRAM, and OAUTHBEARER with different refresh behavior. PLAIN over plaintext exposes passwords, and process-global credentials make rotation unsafe. |
| Credible interpretations and known peer behavior | Default to plaintext, accept static secrets only, permit PLAIN anywhere, or require verified TLS and bounded caller-owned providers. Compatible services implement different subsets. |
| Selected behavior | Verified TLS 1.2+ is the production default; plaintext is explicit development-only policy; PLAIN requires verified TLS; mTLS, SCRAM-SHA-256/512, and OAUTHBEARER use bounded refreshing providers. Material is copied or borrowed under documented lifetime. MSK IAM remains an AWS-dependent nested adapter. |
| Security and resource consequences | The security consequence is hostname verification, explicit roots, least privilege, rotation without globals, and no secret rendering. The resource consequence is bounded certificate, anchor, password, token, provider-call, refresh, and error work. |
| Compatibility and wire consequences | The compatibility consequence is support only for directly exercised broker/auth combinations. The wire consequence is Kafka `SASL_SSL` or TLS/mTLS with the selected mechanism. |
| Executable evidence and public surface | `TestClientSecurityDefaultsToVerifiedTLSAndRequiresExplicitPlaintext`, `TestApacheKafkaTLSAndMutualTLSCompatibility`, and `TestApacheKafkaPlainRollingCredentialRotationCompatibility` cover `ClientSecurity`, provider interfaces, and authentication constructors. |
| Upstream record and reconsideration | The upstream record is Kafka's security guide, TLS behavior, and mechanism implementations. Reconsider on every mechanism, signer, broker, or provider upgrade. |

## KAFKA-DEC-009: Payload-free synchronous observation

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; observability and callback policy. |
| Source and issue | OpenTelemetry's [Kafka messaging conventions](https://opentelemetry.io/docs/specs/semconv/messaging/kafka/) describe telemetry, while franz-go hooks expose client internals and records may contain secrets or high-cardinality identities. Observers can block, panic, or reenter a client. |
| Credible interpretations and known peer behavior | Expose franz-go hooks, log complete records, run unbounded callbacks, or emit stable bounded events with optional adapters. Vendors differ in cardinality and semantic-convention stability. |
| Selected behavior | Root observations are synchronous-by-contract, immutable, bounded, payload-free, panic-contained, cooperatively timed, and fenced against same-client reentry. Topic and group identity are opt-in and bounded. slog and OpenTelemetry remain optional adapters. |
| Security and resource consequences | The security consequence is no key, value, arbitrary header, credential, or retained error disclosure. The resource consequence is bounded strings, counts, callback duration, failure reporting, and no observer-owned background goroutine. |
| Compatibility and wire consequences | The compatibility consequence is a stable event model independent of vendors. The wire consequence is none: observation cannot alter Kafka requests, delivery, settlement, or transactions. |
| Executable evidence and public surface | `TestObserverFailuresAreContainedAndReportedInOrder`, `TestBrokerObserverReportsRequestLatencyAndRedactedFailure`, and `TestObservationValidationEnforcesEventRecordCardinality` cover `ObserverPolicy`, `Observation`, and `ObservationFailure`. |
| Upstream record and reconsideration | The upstream record is the selected OpenTelemetry convention and pinned franz-go hook source. Reconsider only with a versioned observation or adapter migration. |

## KAFKA-DEC-010: Operational compatibility requires direct evidence

| Field | Decision |
| --- | --- |
| Status, owner, and classification | `resolved`; Kafka maintainers; broker and compatible-service support policy. |
| Source and issue | Apache Kafka publishes [versioned downloads](https://kafka.apache.org/community/downloads/), while [Amazon MSK documentation](https://docs.aws.amazon.com/msk/) describes modes, versions, authentication, quotas, and network behavior. Protocol reachability alone does not prove operations. |
| Credible interpretations and known peer behavior | Inherit franz-go compatibility, advertise every Kafka-like service, test one local broker, or publish only exact exercised matrices. Clients commonly describe protocol support more broadly than deployment evidence. |
| Selected behavior | Apache support is limited to exact versions, topology, features, security profiles, operating systems, and architectures with retained evidence. Every compatible service and mode requires its own producer, consumer, transaction, replay, inspection, authentication, failure, rotation, lifecycle, and cleanup matrix. MSK Provisioned and Serverless remain unverified and unsupported until run. |
| Security and resource consequences | The security consequence is no inferred authentication or authorization safety. The resource consequence is that capacity, quota, reconnect, memory, goroutine, and connection claims require measured service-specific evidence. |
| Compatibility and wire consequences | The compatibility consequence is an explicit non-claim for untested services. The wire consequence is that a successful Kafka handshake does not establish feature or operational equivalence. |
| Executable evidence and public surface | `TestApacheKafkaCurrentMultiBrokerKRaftCompatibility`, `TestApacheKafkaMinimumSupportedTransactions`, and `TestKafkaProducerConsumerCompatibility` cover the current Apache matrix and public producer, consumer, transaction, replay, and inspector contracts. |
| Upstream record and reconsideration | The upstream record is each exact broker or managed-service release and its primary documentation. Reconsider only after direct content-addressed evidence for the proposed support row. |

## Unresolved decisions

None. Unsupported broker, managed-service, identity-provider, and production
tiered-storage profiles are explicit non-claims, not unresolved interpretations.
