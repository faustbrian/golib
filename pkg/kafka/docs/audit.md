# Pre-v1 implementation audit

This audit records the starting point and current disposition of the
production-policy redesign. A passing check against the draft is not evidence
that the draft has the right contract. Status describes executable behavior in
the current tree on 2026-07-26, not intended behavior.

## Current implementation inventory

| Area | Current executable behavior | Required disposition |
| --- | --- | --- |
| Configuration | Separate producer, consumer, replay, and inspector structs validate some identities, durations, and byte limits. Security configuration now owns explicit transport/authentication policy, redacted formatting, and defensive copies. | Add allowlists/resolvers, protocol version policy, and complete incompatible-option validation across every concern. |
| Records | Stable producer and consumed-record models expose explicit automatic or exact partition selection, timestamp type, and leader epoch, copy producer input, and provide `Retain` for borrowed consumed bytes. | Bound diagnostic copies separately from transport limits. |
| Producer | Synchronous single, synchronous batch, and bounded asynchronous methods return per-record delivery metadata. Keyed production is the safe default; exact partitions require an explicit record policy; delivery errors are redacted and classified; drain, abort, and shutdown are explicit. | Add byte-level client buffering, ambiguity/throttling/fatal categories, observer callbacks, and real-broker failure and shutdown evidence. |
| Transactions | A producer callback can begin, synchronously produce, commit, or abort. Calls are serialized and callback lifetime is fenced. | Add typed fatal, abortable, fenced, and unknown outcomes; source offsets; read-committed consume-transform-produce; explicit ownership; bounded close; and real fencing/recovery evidence. |
| Consumer groups | A classic cooperative-sticky group polls up to a record count, runs one record at a time, then commits the complete poll. Rebalances are blocked during the poll. | Replace poll-wide settlement with per-partition contiguous settlement, explicit generation ownership, assignment/revocation/loss handling, record and batch handlers, parallel partitions, pause/drain/shutdown, retry and dead-letter strategies, lifecycle observations, and bounded fetched bytes per partition. |
| Replay | Explicit partition offsets are directly assigned and checked for consecutive offsets. Replay is serialized and does not join a group. | Add broker-validated dry-run plans, timestamp planning, external checkpoints, multi-partition concurrency, exact incomplete reports, retention/truncation/compaction distinctions, side-effect opt-in, cancellation, and bounded shutdown. |
| Inspection and health | Topic partition counts and group lag are read through kadm; ping is the only health signal. | Add cluster/controller, beginning/end offsets, group assignments, topic durability configuration, offline/under-replicated state, typed partial errors, and distinct liveness/readiness/dependency/diagnostic policy. |
| Security | Verified TLS 1.2 with system roots is the zero value. Plaintext is an explicit development-only policy. Owned, bounded mTLS, PLAIN, SCRAM-SHA-256/512, and OAUTHBEARER providers support rotation and redacted failures. | Add secured-broker authentication evidence, expiry/rotation stress evidence, and an independently versioned MSK IAM adapter before making support claims. |
| Hooks and telemetry | No stable policy hook surface exists. | Add bounded synchronous observers with copied metadata and panic/reentrancy rules; keep OpenTelemetry and slog adapters optional. |
| Evidence | Unit, race, exact statement and mutation coverage, two fuzz targets, docs, and a microbenchmark pass. One integration test uses one Confluent Local 7.5.0 broker. | Add multi-broker Apache Kafka, failures, auth, transactions, multiprocess rebalances, replay faults, stress/leaks, adapters, clean consumer, compatibility, and equivalent-client benchmarks. |

## Public boundary violations

The draft does not expose `kgo.Client`, `kgo.Record`, `kgo.Opt`, kadm response
types, or franz-go SASL mechanisms through ordinary public APIs. Security
mechanism translation is internal to the franz-go-backed implementation.

`Publish` remains the error-only compatibility method. `PublishRecord`,
`PublishBatch`, and `PublishAsync` now expose assigned partition, offset, and
timestamp. Broker throttle metadata and a proven ambiguous-outcome category
remain absent.

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

- `make check`: format, vet, unit, race, exact statement coverage, two
  10,000-execution fuzz targets, the existing microbenchmark, and documentation
  checks passed;
- integration, mutation, root release gates, auth, chaos, compatibility, and
  reverse-dependent adapter gates were not part of that command; and
- the existing integration fixture is
  `confluentinc/confluent-local:7.5.0@sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd`.

This baseline proves only the behavior already present in the draft.

The security-policy batch subsequently killed all 839 viable mutants with
100% test efficacy and mutator coverage. That result proves the deterministic
package assertions detect the generated mutations; it does not substitute for
the secured-broker and compatibility evidence still listed above.
