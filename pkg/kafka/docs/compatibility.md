# Compatibility and execution matrix

This matrix separates pinned implementation inputs from support claims. It was
recorded through 2026-07-30. Upstream protocol support is not package evidence.

The independently versioned `kafkaservice` module is additive and pre-v1.
Existing direct producer and consumer construction remains supported. The
adapter uses the public Kafka record, handler, health, run, and shutdown
contracts and does not change their wire or settlement semantics.

## Pinned inputs

| Input | Exact version or identity | Verification |
| --- | --- | --- |
| Go toolchain and module language | Go 1.26.5, `go 1.26.5` | `go version`, `go env`, and `go.mod` |
| Host used for baseline | Darwin arm64, Apple M4 Max | Go environment and benchmark output |
| Container runtime | Docker Desktop engine 29.6.2, API 1.55 | Benchmark environment capture on 2026-07-30 |
| franz-go | v1.21.5, tag target `1ba5fd24f949a335dbc7caaef1d6037e132ef23e` | Go module proxy plus upstream tag; latest stable found on 2026-07-27 |
| kadm | v1.18.0, tag target `a7255a3f2bc7247e70a15b18080cc4e5cd1e42d6` | Go module proxy plus upstream tag; latest stable found on 2026-07-27 |
| Comparison clients | kafka-go v0.4.51; IBM/Sarama v1.60.1 | Go module proxy on 2026-07-30; isolated non-releasable benchmark module |
| testcontainers-go core and Kafka module | v0.43.0 | Go module proxy |
| Testcontainers resource reaper | `testcontainers/ryuk:0.14.0`, locally resolved digest `sha256:7c1a8a9a47c780ed0f983770a662f80deb115d95cce3e2daa3d12115b8cd28f0` | Runtime log and local Docker image metadata; testcontainers invokes the tag, so an immutable source-level pin remains a release blocker |
| Existing broker fixture | Confluent Local 7.5.0 digest `sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd` | Source-pinned single-node integration test and benchmark harness both reject a runtime version other than `7.5.0-ccs` |
| Current Apache Kafka fixture | `apache/kafka:4.3.1`, multi-platform index `sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837` | Source-pinned three-node combined KRaft and isolated secured single-node fixtures; every fixture reports exactly `4.3.1`; the secured fixtures report OpenSSL 3.5.7 and run the broker as image UID 1000; arm64 manifest `sha256:c2b5172ab20d66381ec1729796a410fd611135821994526d4d42d2f256054af3` |
| Mutation tool | patched Gremlins v0.6.0 | The current-tree module gate requires and records exact 100% efficacy and mutator coverage for the root package and bundled slog adapter; transient mutant counts remain in its attributable evidence artifact rather than this version matrix |
| Lint/static analysis | golangci-lint v2.12.2, Staticcheck v0.7.0, NilAway `9fd1b8d7bac8` | Repository tool pins |
| Security/release tools | govulncheck v1.6.0, Gitleaks v8.30.1, go-licenses v2.0.1, CycloneDX v1.10.0 | Repository tool pins |
| OpenTelemetry semantic conventions and Go API | Semantic conventions 1.43.0; Go v1.44.0 | Independently versioned adapter provenance and module pins |
| MSK IAM Go signer and AWS SDK for Go v2 | Signer v1.0.4 at `53637de1b411b2a2c8b2ccb8f103fc1d6b761c07`; SDK v1.43.0 and config v1.32.31 at `4fef3455fe2dcb5ea3de4e9fbacf889b84c8a255` | Upstream tags, Go module proxy, and adapter provenance |

Apache Kafka 4.3.1 was the latest supported Apache release found at execution
time. The executed three-node fixture establishes compatibility only for the
explicit plaintext KRaft producer, inspection, and producer-transaction
scenario below. Separate single-node fixtures establish only the listed TLS,
mTLS, PLAIN, SCRAM, and signed-JWT OAUTHBEARER paths. Neither establishes a
minimum broker version or the complete support matrix.
The zero `ProtocolPolicy` negotiates request versions with each connection.
`MinimumVersion` is only a request downgrade floor recognized by franz-go; it
does not prove or constrain the broker release and does not change this matrix.
The transaction processor is stricter: its empty policy becomes a Kafka 2.5
request floor and lower explicit values are rejected for KIP-447 safety. That
floor still does not establish operational support for an untested broker.

## Current support status

| Dimension | Planned matrix | Current status |
| --- | --- | --- |
| Go | Minimum and current repository-supported releases | Only Go 1.26.5 on Darwin arm64 executed locally |
| Apache Kafka | Minimum reviewed release and current 4.3.1, KRaft, three brokers | Current 4.3.1 executes as three combined KRaft broker/controller nodes with exact runtime assertion. RF=3, `min.insync.replicas=2`, clean leader election, continued acks-all delivery at ISR=2, endpoint and ISR recovery, bounded ambiguous non-transactional delivery after every matching `Produce` response is lost, committed/aborted producer transactions before and after broker-process recovery, same-transactional-ID producer fencing, broker-enforced transaction expiry, a committed transaction whose matching `EndTxn` responses are lost to the producer, consume-transform-produce recovery after terminating a real child process between output acknowledgement and commit, and two-process eager plus cooperative rebalance abort and redelivery pass locally. Isolated combined-mode nodes additionally prove the secured client-listener paths listed below. Minimum-version, separated-role, broader fault, live rotation, and managed-service evidence remains unverified. |
| Confluent Platform/Local | Only explicitly exercised versions | One single-node 7.5.0 compatibility test with exact `7.5.0-ccs` runtime assertion; not a production support claim |
| Amazon MSK Provisioned | Selected Kafka versions, TLS/mTLS/SCRAM/IAM | Unverified |
| Amazon MSK Serverless | TLS and IAM with documented service limits | Unverified |
| Redpanda, Confluent Cloud, Event Hubs, other compatible services | Add only after direct testing | Unverified and unsupported |
| TLS and mTLS | TLS 1.2/1.3, hostname/root failures, rotation | The pinned Apache 4.3.1 broker negotiates exact TLS 1.2 and 1.3, rejects an unknown root and wrong hostname, and requires a client certificate. Provider-backed mTLS producer delivery, consumer settlement, and inspector health pass; the same broker rejects a client without a certificate. Live certificate rollover remains unverified. |
| SASL/PLAIN | Verified TLS only | The pinned Apache 4.3.1 `SASL_SSL` listener accepts provider-backed production and rejects an incorrect password. With KRaft `StandardAuthorizer` enabled, a separately authenticated principal with no matching ACL receives a classifiable topic-authorization delivery failure and unchanged inspector authorization identity without password disclosure. Reauthentication during credential rollover remains unverified. |
| SCRAM-SHA-256/512 | Verified TLS only | The pinned Apache 4.3.1 KRaft metadata log is initialized with independently generated SHA-256 and SHA-512 credentials. Provider-backed production succeeds for each mechanism over `SASL_SSL`, incorrect credentials fail, SCRAM-SHA-512 consumption settles the full record set, and SCRAM-SHA-256 inspection succeeds. After live SHA-256 credential replacement, one existing producer crosses the broker-enforced reauthentication lifetime, invokes its provider again, resumes successful delivery, and rejects the retired credential on a new connection. SHA-512 replacement and prolonged multi-client rollover stress remain unverified. |
| OAUTHBEARER | Refreshing provider over verified TLS | The pinned Apache 4.3.1 `SASL_SSL` listener uses its production JWT validator with an RS256 JWKS plus exact issuer and audience checks. Provider-backed production and consumption succeed; correctly signed tokens for the wrong issuer or audience fail without appearing in the returned error. HTTP token acquisition, HTTPS JWKS refresh, signing-key rollover, and specific identity providers remain unverified. |
| MSK IAM | Optional AWS signer adapter | The independently versioned adapter uses the supported Go signer, refreshing SDK v2 default chain or explicit provider, bounded cancellation and refresh, effective expiry capped by signing credentials, and redacted failures. Local contract, race, fuzz, and allocation evidence exists; no Provisioned or Serverless broker has been exercised, so operational compatibility remains unverified. |
| Producer | Single, batch, async, ordering, failure, shutdown | Policy APIs and deterministic tests exist. The pinned single-broker fixture proves ordered batch and asynchronous delivery metadata, exact broker-visible values, graceful shutdown draining an admitted asynchronous record, rejection after shutdown, and successful delivery under a 1 KiB/s client-ID quota with a positive post-response throttle event. Throttling remains request-level because Kafka produce responses can cover many records. The Apache fixture proves a two-topic batch whose first input is acknowledged and persisted while the second is rejected by a topic-level `max.message.bytes` policy; results remain in input order with an `ErrorOversized` delivery only on the rejected record. It also proves explicit-partition synchronous delivery before failure, after clean leader failover at ISR=2, after ISR=3 recovery, and after a fault forwards the record while dropping every matching `Produce` response. The response-loss case returns within the configured delivery plus retry-backoff bound, reports an ambiguous timeout, and a separate consumer observes exactly one broker record. |
| Consumer group | Classic cooperative/eager and reviewed next-generation protocol | Explicit cooperative-sticky, eager-sticky, migration, static-membership, rack, bounded cross-partition handling, partition pause/resume, and per-record plus whole-batch failure policy exists. The single-node fixture proves eager membership, same-ID static restart, duplicate-live-instance fencing into a terminal package state, pause/resume, concurrent independent-partition handling with sequential partition order, per-record retry/dead-letter publication followed by source settlement, redelivery after failed publication, and whole-partition-batch retry-topic publication with exact source coordinates and batch positions before source settlement. Unit evidence additionally proves partial target results leave the complete source batch unsettled and remain input-ordered for reconciliation. A separate three-broker Apache Kafka 4.3.1 fixture uses three operating-system child processes to prove an eager-only plus migration member negotiates `sticky`, the migration member retains both partitions after the eager member leaves, a migration plus cooperative-only member negotiates `cooperative-sticky`, and the cooperative member retains both partitions after the migration member leaves. Every stable state verifies exact client identities and one-copy ownership of both partitions. With one rack per broker and Kafka's rack-aware replica selector enabled, a separate consumer process configured for a non-leader replica's rack handles and commits a source record after its single in-flight fetch completes on that follower. Nontransactional handler overlap during every rebalance phase remains unverified. |
| Transactions | Produce and consume-transform-produce | Producer-only and source-offset consume-transform-produce commit/abort isolation are exercised against the pinned single-node fixture. Producer-only committed/aborted isolation also passes before and after one broker process is recovered in the three-node Apache fixture with the transaction-state ISR restored. While one broker process is unavailable and required replicated topics remain at ISR two, the fixture additionally proves atomic source-offset/output commit, abort invisibility, source redelivery, and successful retry. The same fixture proves that a replacement producer claiming the same transactional ID fences the older producer, aborts its pending transaction, and leaves only the replacement record visible to read-committed consumers. A one-second producer transaction is held open until the coordinator reports `CompleteAbort`; the expired record remains read-uncommitted visible and read-committed invisible, while the producer receives ambiguous `INVALID_TXN_STATE` because that response alone cannot prove the final outcome. A response-loss fault forwards a real `EndTxn` commit, drops every matching response, proves the record is visible through a separate read-committed consumer, and proves the producer returns a non-abortable unknown commit outcome. Separate lost-`Produce` response scenarios prove standalone and processor calls return within the delivery bound, close and fatally fence the client, reject reuse, keep output read-committed invisible, and leave the processor source offset unsettled. A real child process is terminated after broker-acknowledged output but before commit; the replacement processor reuses the transactional ID, reprocesses the unsettled source record, commits its output and source offset atomically, and leaves the interrupted output visible only at read-uncommitted isolation. A second live group member in another operating-system process proves eager rebalance abort after broker-acknowledged output, exact non-settlement of the interrupted source, read-committed invisibility, and successful post-rebalance redelivery. A cooperative two-process scenario additionally proves incremental revocation abort, atomic progress on the reassigned partition, and exact recovery of the remaining unsettled source after stabilization. Older-broker transaction behavior remains unverified. |
| Replay | Offset/timestamp planning, exact ranges, gaps, resume | Local and broker-validated checkpoint-aware dry-run plans, exact-partition timestamp-window plans, explicit side-effect opt-in, replay-specific range and resume metadata, broker-boundary preflight, exact no-reset ranges, independently bounded fetch and cross-partition handler concurrency, resumable per-range progress, cancellation, bounded shutdown, and payload-free plan, record, exact-progress, shutdown, and broker observations are implemented. The single-node fixture proves an executable timestamp-derived plan, broker-validated planning without consuming execution, interrupted replay, external-checkpoint resume, cancellation after real handler admission without settlement, bounded cross-partition handler overlap with sequential per-partition order, future out-of-range rejection, and fail-closed rejection after Kafka `DeleteRecords` advances the log start. A separate pinned Apache 4.3.1 fixture keeps the log start at offset 0 while compaction removes that exact offset, then proves `[0,1)` fails as `ErrReplayOffsetGap` without handler admission and with an unchanged checkpoint. Leader-recovery or unclean-election log-truncation evidence remains incomplete. |
| Inspection/health | Cluster/topic/group/durability and separated health signals | Single-broker cluster/controller, topic replica/ISR/offline, beginning/end offset, effective `min.insync.replicas`, non-default cleanup/retention/compaction/segment/unclean-election policy, classic-group lag/member assignment, dependency health, local liveness, and readiness-hysteresis evidence. The Apache fixture additionally proves exact three-broker cluster identity, RF=3/ISR=3, clean leader failover at ISR=2, and ISR=3 recovery; tiered-storage local retention, KIP-848 inspection, multi-target partial failures, and complete service health composition remain unverified. |
| Observability | Producer, consumer, rebalance, transaction, replay, inspection, and lifecycle events plus optional adapters | Root payload-free producer delivery, nontransactional consumer record/batch/commit/poll and group lifecycle, producer and consume-transform-produce transaction lifecycle, producer/consumer/transaction-processor/replay/inspector shutdown, inspector cluster/topic/group/dependency/readiness, and every client role's broker events are deterministically tested. The single-broker fixture proves producer and consume-transform-produce begin/commit/abort events plus transaction-processor broker requests. The standard-library slog adapter emits fixed bounded scalar fields, denies every Kafka identity by default, contains handler panics, and has exact local statement, fuzz, race, concurrency, and allocation evidence. The independently versioned OpenTelemetry adapter maps every current event, pins development-status messaging semantic conventions 1.43.0, defaults every identity attribute off, and has exact local statement, fuzz, race, concurrency, and allocation evidence. Standalone authentication, complete rebalance timing, and cross-message propagation remain unimplemented. |
| Performance | Equivalent producer, consumer, transaction, replay, inspection, reconnect, resource, and policy-overhead workloads | The isolated client harness has separate real-broker producer single-record, batch, bounded asynchronous, eight-partition, producer-transaction, consume-transform-produce, direct replay, and read-only topic-inspection correctness evidence. Timed producer evidence covers equivalent idempotent all-ISR synchronous single-record, 10/100-record batch, bounded 10/100-record asynchronous-window, balanced Murmur2-keyed plus explicitly assigned 80-record eight-partition, and one/ten-record transaction contracts for the package policy, raw franz-go, and Sarama. Kafka-go is an unranked ordinary-producer correctness control because v0.4.51 cannot match idempotence and is excluded from transactional production because its writer exposes no Kafka transaction. Automatic unkeyed multi-partition production is excluded because franz-go and Sarama do not expose equivalent distribution strategies. Separate consumer evidence proves exact record and 10/100-record batch handling followed by broker-verified manual commits for the policy, raw franz-go, kafka-go, and Sarama. A further policy/raw-franz-go workload proves sequential and bounded-parallel handling, exact per-partition order, and broker-verified commits across eight partitions; kafka-go and Sarama are excluded because their group APIs do not expose an equivalent bounded multi-partition poll-and-commit cycle. A policy/raw-franz-go consume-transform-produce capture proves exact transformed outputs, source-offset advancement, abort invisibility, and source redelivery across one/ten-record transactions. Four-client direct replay evidence measures complete single-use lifecycle plus exact broker-bound ranges for 10/100 records, while four-client topic inspection measures the same metadata, offset, configuration, and normalized durability state across one/eight partitions. Every capture retains raw results, a workload-specific environment fingerprint, and variance-preserving analysis. Rebalance cost, reconnect and idle-resource evidence, deployment-representative TLS cost, the previous released version, and controlled-runner results remain unverified. |
| Operating systems/architectures | Linux amd64/arm64 plus repository-supported developer platforms | Local Darwin arm64 only; CI matrix not yet established |

## Release evidence

On 2026-07-29, commit `2dc6459` passed the repository release dry-run for
`pkg/kafka`. After module-local tidy, test, and API-compatibility gates, the
release tool created a fresh temporary module with `GOWORK=off`, resolved
`github.com/faustbrian/golib/pkg/kafka@v0.0.0` through the local source proxy,
and listed the public package successfully. This proves the committed module is
independently consumable from the repository's release artifact. It does not
prove a published tag or public Go module proxy resolution.

## Primary sources

Design and implementation are checked against:

- [Apache Kafka documentation](https://kafka.apache.org/documentation/) and
  [supported downloads](https://kafka.apache.org/community/downloads/), plus
  the broker-compatible rules in Kafka's
  [`Topic` source](https://github.com/apache/kafka/blob/4.3.1/clients/src/main/java/org/apache/kafka/common/internals/Topic.java);
- [Apache Kafka producer configuration](https://kafka.apache.org/43/configuration/producer-configs/),
  [broker configuration](https://kafka.apache.org/43/configuration/broker-configs/),
  [topic configuration](https://kafka.apache.org/43/configuration/topic-configs/),
  [consumer configuration](https://kafka.apache.org/43/configuration/consumer-configs/),
  [consumer rebalance protocol](https://kafka.apache.org/43/operations/consumer-rebalance-protocol/),
  [transaction protocol](https://kafka.apache.org/43/operations/transaction-protocol/),
  and [client quota operations](https://kafka.apache.org/43/operations/basic-kafka-operations/#setting-quotas);
- [Apache Kafka TLS](https://kafka.apache.org/43/security/encryption-and-authentication-using-ssl/),
  [SASL authentication](https://kafka.apache.org/43/security/authentication-using-sasl/),
  [authorization and ACLs](https://kafka.apache.org/43/security/authorization-and-acls/),
  [listener configuration](https://kafka.apache.org/43/security/listener-configuration/),
  and the OAUTHBEARER
  [URL allowlist system property](https://kafka.apache.org/43/configuration/system-properties/);
- [KIP-98](https://cwiki.apache.org/confluence/display/KAFKA/KIP-98%2B-%2BExactly%2BOnce%2BDelivery%2Band%2BTransactional%2BMessaging)
  defines the single-active-producer guarantee for one transactional ID and
  fencing of an older generation when its replacement starts;
- [KIP-219 quota communication](https://cwiki.apache.org/confluence/pages/viewpage.action?pageId=75962388)
  and [KIP-546 client quota APIs](https://cwiki.apache.org/confluence/display/KAFKA/KIP-546%3A+Add+Client+Quota+APIs+to+the+Admin+Client);
- [franz-go v1.21.5 package documentation](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo@v1.21.5),
  including its tag-pinned
  [`RecordDeliveryTimeout`](https://github.com/twmb/franz-go/blob/v1.21.5/pkg/kgo/config.go)
  and idempotent in-flight cancellation contracts, plus the tag-pinned
  [`ProduceSync`](https://github.com/twmb/franz-go/blob/v1.21.5/pkg/kgo/producer.go)
  callback aggregation used to verify cross-partition completion ordering;
- the tag-pinned [kafka-go v0.4.51](https://github.com/segmentio/kafka-go/tree/v0.4.51)
  writer source and [IBM/Sarama v1.60.1](https://github.com/IBM/sarama/tree/v1.60.1)
  producer configuration used to establish the benchmark capability boundary;
- [Amazon MSK documentation](https://docs.aws.amazon.com/msk/), including
  [IAM client configuration](https://docs.aws.amazon.com/msk/latest/developerguide/configure-clients-for-iam-access-control.html);
- [OpenTelemetry Kafka semantic conventions](https://opentelemetry.io/docs/specs/semconv/messaging/kafka/);
- [RFC 7517](https://www.rfc-editor.org/rfc/rfc7517) for JWKS and
  [RFC 7519](https://www.rfc-editor.org/rfc/rfc7519) for signed JWT claims; and
- [Go 1.26 release documentation](https://go.dev/doc/go1.26), the Go memory
  model, `context`, `crypto/tls`, fuzzing, race detector, and module docs.

The exact source revision or version used for an implemented capability must be
recorded beside its executable evidence, not inferred later from `HEAD`.
