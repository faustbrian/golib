# Goal Harden: Kafka Service Lifecycle Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Mission

Prove Kafka resources start and terminate safely under partial failure,
rebalance, delivery ambiguity, repeated signals, and Kubernetes replacement.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Model every constructor/start/readiness/run/drain/stop/failed state for
  producer and consumer adapters.
- Inject failure and panic at each callback and lifecycle transition; prove
  reverse-order rollback and stable aggregate errors.
- Race start, run exit, health checks, repeated stop, canceled stop, and process
  signals under `-race`; detect leaks and double close.
- Use real Kafka to test rebalance during shutdown, slow handlers, commit
  timeout, producer flush ambiguity, broker loss, credential expiry, and
  recovery after partial startup.
- Exercise concrete root producer and consumer resources through a pinned real
  Kafka broker, including producer readiness, admitted-handler drain ordering,
  pre-cancellation offset settlement, in-flight cancellation redelivery,
  resource shutdown, and post-stop admission fencing.
- Test Kubernetes readiness removal and termination grace so no new work enters
  after drain begins and unacknowledged work remains redeliverable.
- Verify one total shutdown budget is propagated rather than reset per step.
- Assert lifecycle diagnostics exclude Kafka records, callback causes, endpoints,
  and credentials.
- Benchmark adapter overhead and shutdown latency separately from broker work.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed meaningfully with no unresolved lifecycle, delivery, race,
security, or real-broker finding.
