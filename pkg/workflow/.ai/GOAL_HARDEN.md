# Goal Harden: `workflow`

## Mission

Prove workflow safety under duplicate delivery, process death, network
partition, database failover, lease loss, retries, compensation failure,
version skew, operator races, and long-running execution.

## Required Audit

1. Inventory definitions, versions, state transitions, stores, workers,
   activities, timers, signals, leases, adapters, operator commands, schemas,
   metrics, and ownership boundaries.
2. Model-check legal transitions and prove invalid, repeated, stale, and
   out-of-order transitions fail deterministically.
3. Inject failure before and after every persistence, activity, publication,
   acknowledgement, lease, timer, signal, and compensation boundary.
4. Verify idempotency and distinguish known failure from unknown external
   outcome; prove retries cannot widen side effects.
5. Stress lease renewal, fencing, worker death, clock skew, long pauses,
   starvation, bounded fan-out, backpressure, and shutdown.
6. Verify compensation order, partial compensation, retry exhaustion, manual
   resolution, audit history, and truthful terminal states.
7. Exercise definition upgrades, old workers, new workers, rolling deploys,
   incompatible versions, migration interruption, and continue-as-new.
8. Verify PostgreSQL deadlocks, failover, backup/restore, archive, retention,
   timer recovery, and reconciliation of every derived queue.
9. Test Kafka/queue/outbox/CloudEvents duplicate, reorder, delay, poison,
   partition, and dead-letter behavior without acknowledgement gaps.
10. Run multitenant isolation, security, redaction, resource exhaustion, race,
    leak, fuzz, stress, and multi-day soak audits.

## Required Evidence

- exhaustive/model-based transition proof and deterministic replay fixtures;
- process-kill fault matrix across every durable boundary;
- mixed-version rolling-upgrade and backup/restore exercises;
- exact 100% meaningful statement coverage and 100% viable mutation kills;
- PostgreSQL and messaging interoperability tests;
- race, fuzz, leak, stress, soak, chaos, and capacity results;
- comparable benchmarks against equivalent durable-workflow scenarios;
- operator runbook, recovery drills, docs, and clean-consumer proof.

No passing state may conceal an unresolved activity outcome, failed
compensation, lost signal, expired lease, or unprocessed durable transition.
