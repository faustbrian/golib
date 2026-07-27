# Pre-v1 implementation audit

This audit records the starting point and current disposition of the
production-policy redesign. A passing check against the draft is not evidence
that the draft has the right contract. Status describes executable behavior in
the current tree on 2026-07-27, not intended behavior.

## Current implementation inventory

| Area | Current executable behavior | Required disposition |
| --- | --- | --- |
| Configuration | Separate producer, consumer, replay, and inspector structs validate identities, durations, byte limits, and Kafka broker-compatible topic names. Producers require a copied bounded topic allowlist; consumers, replay, and inspectors have explicit topic or group target sets. A shared owned protocol policy preserves `ApiVersions` negotiation and optionally applies a validated minimum request-version floor. Security configuration owns explicit transport/authentication policy, redacted formatting, and defensive copies. | Add optional reviewed resolver policies and complete incompatible-option validation across every concern. |
| Records | Stable producer and consumed-record models expose explicit automatic or exact partition selection, timestamp type, and leader epoch, copy producer input, and provide `Retain` for borrowed consumed bytes. | Bound diagnostic copies separately from transport limits. |
| Producer | Synchronous single, synchronous batch, and bounded asynchronous methods return per-record delivery metadata. Keyed production is the safe default; exact partitions require an explicit record policy; detected idempotent-producer data loss stops the producer; delivery errors include redacted fatal and ambiguous classifications, including retryable broker-restart dial failures; drain, abort, shutdown, and error-returning bounded close preserve preexisting admissions. Producer admission has independent record-count and byte limits, and the byte limit cannot be smaller than one allowed batch. The three-broker Apache fixture proves acks-all explicit-partition delivery before failure, with RF=3/ISR=2 after leader failover, and after ISR recovery. | Add delivery-result broker-throttling metadata and real-broker batch, asynchronous, ambiguous-outcome, and shutdown evidence. |
| Transactions | A producer callback can begin, synchronously produce, commit, or abort. A separate processor owns one read-committed group member and transactional producer, treats one bounded source poll as one all-or-nothing transaction, commits source offsets only with successful Kafka outputs, fences callback lifetime and unsafe lifecycle failures, and closes through a bounded retryable lifecycle. The single-broker fixture proves producer visibility, aborted-source filtering, plus atomic source-offset/output commit, abort, and redelivery. The three-broker Apache fixture proves committed/aborted producer isolation before and after one broker process is stopped and recovered with the transaction-state ISR restored. | Add real multi-broker consume-transform-produce rebalance, fencing, timeout, unknown-outcome, and process-termination recovery evidence. |
| Consumer groups | A classic group explicitly selects cooperative-sticky, eager-sticky, or an eager-to-cooperative rollout policy; optional static membership and rack identity are validated. It bounds concurrent fetches plus aggregate and per-partition fetch bytes, rejects fetched records outside explicit record limits before the package copies header metadata, and uses an explicit fixed-size worker limit across independent partitions while keeping each partition sequential. Record handling stops each partition at its first failure and commits only contiguous successful prefixes; batch handling is all-or-nothing per partition. Rebalances are blocked during the poll; a fast blocked callback stops admission and either requests cooperative cancellation of every active handler or drains only handlers already active before settlement. Bounded internal callbacks track incremental assignments, revocations, fatal loss, and a package-local settlement epoch without exposing franz-go or claiming Kafka's broker generation ID. Explicit subscribed partitions can be paused and resumed within a bounded accumulated set; shutdown fences new work and waits for the active runner. A per-record decorator adds bounded category-selected retry, versioned retry-topic and dead-letter publication, stop, and application delegation; the broker fixture proves publish-then-commit and failed-publication redelivery. | Add batch failure strategies, multiprocess broker evidence for rolling balancing/fencing/rack behavior, and a policy for decompressed or broker-oversized batches rejected before franz-go decodes them. |
| Replay | Explicit partition offsets are directly assigned without a group. Local and broker-validated owned plans apply external checkpoints; exact-partition timestamp-window planning rejects retention-ambiguous starts and produces owned executable ranges. Handler execution requires explicit side-effect opt-in and a replay-specific handler whose retained record carries the complete requested range and checkpoint-derived effective start. Planning and execution validate effective starts and exclusive ends against bounded broker offset lookups before using no-reset fetches. Fetch and handler concurrency are independently bounded; handler execution is serial by default and optionally overlaps only across partitions while retaining exact independent progress after failure. Record limits, handler deadlines, cancellation, gap/out-of-range failure, exact per-range incomplete progress, completed-partition pausing, single-run ownership, and bounded retriable shutdown fail closed. Payload-free observers report broker-validated planning, each record outcome, exact resumable aggregate progress, shutdown, and replay broker activity with same-reader reentrancy fencing. The pinned broker fixture proves an executable timestamp-derived plan, broker-validated planning without consuming execution, interruption, external-checkpoint resume, cancellation after real handler admission without settlement, bounded cross-partition handler overlap with sequential partition order, future out-of-range rejection, and rejection before polling after Kafka record deletion advances the log start. | Add broker-proven truncation evidence and deterministic compacted-gap evidence; Kafka does not expose the deletion cause of one absent offset, so the public gap category must remain conservative. |
| Inspection and health | Explicit bounded calls expose cluster identity, controller visibility, sorted brokers, exact replica/ISR/offline sets, leader epochs, beginning/end offsets, effective `min.insync.replicas`, cleanup, retention, compaction, segment and unclean-election policy, and classic group lag, member identity, and assignments. Calls derive owned request timeouts and fail closed on incomplete, inconsistent, excessive, unauthorized, or unavailable state. Dependency health, local inspector liveness, and readiness with consecutive-failure/recovery hysteresis are distinct. The pinned single-broker fixture proves cluster, controller, non-default topic policy, offsets, and one active static classic-group assignment. The three-broker Apache fixture proves exact cluster identity, three brokers, RF=3/ISR=3, clean leader failover to ISR=2, and restored ISR=3. | Add KIP-848 group inspection, tiered-storage local-retention configuration, typed multi-target partial results, transaction/producer diagnostics, multi-target partial-failure evidence, and service-runner liveness composition. |
| Security | Verified TLS 1.2 with system roots is the zero value. Plaintext is an explicit development-only policy. Owned, bounded mTLS, PLAIN, SCRAM-SHA-256/512, and OAUTHBEARER providers support rotation and redacted failures. The independently versioned MSK IAM adapter uses AWS's supported Go signer with a bounded refreshing SDK v2 credential provider and caps effective token expiry at the signing credential expiry without adding AWS to the root module. | Add secured-broker authentication evidence, expiry/rotation stress evidence, and direct MSK Provisioned or Serverless evidence before making support claims. |
| Hooks and telemetry | An optional root `ObserverPolicy` reports copied, payload-free synchronous producer delivery; consumer record, batch, commit, poll, and group lifecycle; producer and consume-transform-produce transaction lifecycle; producer, consumer, transaction-processor, replay, and inspector shutdown lifecycle; replay plan, record, and run lifecycle; inspector cluster, topic, group, dependency-health, and readiness lifecycle; and every client role's broker activity in registration order. It bounds callback count and a shared cooperative deadline, contains and reports observer errors and panics, and fences same-client mutating and lifecycle reentrancy. The standard-library slog adapter emits only fixed bounded fields with deny-by-default copied Kafka identity allowlists and handler-panic containment. The independently versioned OpenTelemetry adapter maps every current event with the same deny-by-default identity posture and pins messaging semantic conventions 1.43.0 without adding OpenTelemetry to the root module. | Add standalone authentication, retry, complete rebalance timing, and cross-message propagation only through separately reviewed record-header policy. |
| Evidence | Unit, race, exact statement and mutation coverage, root fuzz targets, docs, and microbenchmarks pass. The slog adapter adds exact statement coverage, hostile-input fuzz targets, concurrent observer evidence, and an allocation benchmark. The OpenTelemetry adapter adds exact statement coverage, two hostile-input fuzz targets, race evidence, and an allocation benchmark. The MSK IAM adapter adds deterministic signer, default-chain, expiry, cancellation, panic, redaction, concurrency, fuzz, and allocation checks. One fixture uses one Confluent Local 7.5.0 broker. A second fixture asserts Apache Kafka 4.3.1 at runtime across three combined KRaft broker/controller nodes and proves one broker-process failure/recovery path for inspection, production, and producer transactions. | Add secured brokers, authentication failures, multiprocess rebalances, transactional fencing/unknown outcomes, replay faults, stress/leaks, clean consumer, broader compatibility, and equivalent-client benchmarks. |

## Public boundary violations

The draft does not expose `kgo.Client`, `kgo.Record`, `kgo.Opt`, kadm response
types, or franz-go SASL mechanisms through ordinary public APIs. Security
mechanism translation is internal to the franz-go-backed implementation.

`Publish` remains the error-only compatibility method. `PublishRecord`,
`PublishBatch`, and `PublishAsync` now expose assigned partition, offset, and
timestamp. Broker throttle metadata remains absent. Missing backend delivery
results are classified as ambiguous failures.

## Reverse dependencies

The repository manifest records three owned reverse dependencies:

- `event-sourcing/adapters/gokafka`;
- event-sourcing Kafka propagation in `adapters/gotelemetry`; and
- `outbox/adapters/gokafka`.

Every breaking pre-v1 correction must compile and pass the applicable adapter
contract and real-broker tests in the same coherent change. Kafka package tests
alone cannot establish completion.

## Baseline evidence

The following local evidence was executed on Darwin arm64 with Go 1.26.5:

- `make check`: format, vet, unit, race, exact statement coverage, five
  10,000-execution fuzz targets, the existing microbenchmark, and documentation
  checks passed;
- integration, mutation, root release gates, auth, chaos, compatibility, and
  reverse-dependent adapter gates were not part of that command; and
- the existing integration fixture is
  `confluentinc/confluent-local:7.5.0@sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd`.

This baseline proves only the behavior already present in the draft.

The latest recorded package gate killed every viable mutant with 100% test
efficacy and mutator coverage. That result proves the deterministic package
assertions detect the generated mutations; it does not substitute for the
secured-broker and compatibility evidence still listed above.

Current post-baseline integration evidence additionally runs
`apache/kafka:4.3.1` by immutable multi-platform digest, asserts `4.3.1` from
the running container, and exercises three combined KRaft broker/controller
nodes. It proves only the exact RF=3, `min.insync.replicas=2`, clean
leader/ISR, acks-all delivery, and producer-transaction recovery scenario
described above.
