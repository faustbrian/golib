# Pre-v1 implementation audit

This audit records the starting point and current disposition of the
production-policy redesign. A passing check against the draft is not evidence
that the draft has the right contract. Status describes executable behavior in
the current tree on 2026-07-26, not intended behavior.

## Current implementation inventory

| Area | Current executable behavior | Required disposition |
| --- | --- | --- |
| Configuration | Separate producer, consumer, replay, and inspector structs validate identities, durations, byte limits, and Kafka broker-compatible topic names. Producers require a copied bounded topic allowlist; consumers, replay, and inspectors have explicit topic or group target sets. A shared owned protocol policy preserves `ApiVersions` negotiation and optionally applies a validated minimum request-version floor. Security configuration owns explicit transport/authentication policy, redacted formatting, and defensive copies. | Add optional reviewed resolver policies and complete incompatible-option validation across every concern. |
| Records | Stable producer and consumed-record models expose explicit automatic or exact partition selection, timestamp type, and leader epoch, copy producer input, and provide `Retain` for borrowed consumed bytes. | Bound diagnostic copies separately from transport limits. |
| Producer | Synchronous single, synchronous batch, and bounded asynchronous methods return per-record delivery metadata. Keyed production is the safe default; exact partitions require an explicit record policy; detected idempotent-producer data loss stops the producer; delivery errors include redacted fatal and ambiguous classifications; drain, abort, shutdown, and error-returning bounded close preserve preexisting admissions. | Add byte-level client buffering, broker-throttling metadata, observer callbacks, and real-broker failure and shutdown evidence. |
| Transactions | A producer callback can begin, synchronously produce, commit, or abort. A separate processor owns one read-committed group member and transactional producer, treats one bounded source poll as one all-or-nothing transaction, commits source offsets only with successful Kafka outputs, fences callback lifetime and unsafe lifecycle failures, and closes through a bounded retryable lifecycle. The pinned broker fixture proves producer visibility, aborted-source filtering, plus atomic source-offset/output commit, abort, and redelivery. | Add real multi-broker rebalance, fencing, timeout, unknown-outcome, and process-recovery evidence. |
| Consumer groups | A classic group explicitly selects cooperative-sticky, eager-sticky, or an eager-to-cooperative rollout policy; optional static membership and rack identity are validated. It bounds concurrent fetches plus aggregate and per-partition fetch bytes, rejects fetched records outside explicit record limits before the package copies header metadata, and uses an explicit fixed-size worker limit across independent partitions while keeping each partition sequential. Record handling stops each partition at its first failure and commits only contiguous successful prefixes; batch handling is all-or-nothing per partition. Rebalances are blocked during the poll; a fast blocked callback stops admission and either requests cooperative cancellation of every active handler or drains only handlers already active before settlement. Bounded internal callbacks track incremental assignments, revocations, fatal loss, and a package-local settlement epoch without exposing franz-go or claiming Kafka's broker generation ID. Explicit subscribed partitions can be paused and resumed within a bounded accumulated set; shutdown fences new work and waits for the active runner. A per-record decorator adds bounded category-selected retry, versioned retry-topic and dead-letter publication, stop, and application delegation; the broker fixture proves publish-then-commit and failed-publication redelivery. | Add batch failure strategies, lifecycle observations, multiprocess broker evidence for rolling balancing/fencing/rack behavior, and a policy for decompressed or broker-oversized batches rejected before franz-go decodes them. |
| Replay | Explicit partition offsets are directly assigned without a group. Local owned plans apply validated external checkpoints; handler execution requires explicit side-effect opt-in. Execution validates effective starts and exclusive ends against bounded broker offset lookups before using no-reset fetches. Record limits, handler deadlines, cancellation, gap/out-of-range failure, exact per-range incomplete progress, completed-partition pausing, single-run ownership, and bounded retriable shutdown fail closed. The pinned broker fixture proves interruption, external-checkpoint resume, and out-of-range rejection. | Add broker-validated dry-run and timestamp plans, optional cross-partition concurrency, broker-proven retention/truncation/compaction distinctions, replay metadata handlers, and real-broker cancellation evidence. |
| Inspection and health | Explicit bounded calls expose cluster identity, controller visibility, sorted brokers, exact replica/ISR/offline sets, leader epochs, beginning/end offsets, effective `min.insync.replicas`, cleanup, retention, compaction, segment and unclean-election policy, and classic group lag, member identity, and assignments. Calls derive owned request timeouts and fail closed on incomplete, inconsistent, excessive, unauthorized, or unavailable state. Dependency health, local inspector liveness, and readiness with consecutive-failure/recovery hysteresis are distinct. The pinned single-broker fixture proves cluster, controller, non-default topic policy, offsets, and one active static classic-group assignment. | Add KIP-848 group inspection, tiered-storage local-retention configuration, typed multi-target partial results, transaction/producer diagnostics, multi-broker failure evidence, and service-runner liveness composition. |
| Security | Verified TLS 1.2 with system roots is the zero value. Plaintext is an explicit development-only policy. Owned, bounded mTLS, PLAIN, SCRAM-SHA-256/512, and OAUTHBEARER providers support rotation and redacted failures. | Add secured-broker authentication evidence, expiry/rotation stress evidence, and an independently versioned MSK IAM adapter before making support claims. |
| Hooks and telemetry | No stable policy hook surface exists. | Add bounded synchronous observers with copied metadata and panic/reentrancy rules; keep OpenTelemetry and slog adapters optional. |
| Evidence | Unit, race, exact statement and mutation coverage, eight fuzz targets, docs, and microbenchmarks pass. One integration test uses one Confluent Local 7.5.0 broker. | Add multi-broker Apache Kafka, failures, auth, transactions, multiprocess rebalances, replay faults, stress/leaks, adapters, clean consumer, compatibility, and equivalent-client benchmarks. |

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
