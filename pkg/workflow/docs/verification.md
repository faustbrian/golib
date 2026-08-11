# Verification strategy

Executable evidence is scoped to the workflow module and its PostgreSQL
package. Passing adjacent repository packages does not prove workflow behavior.

## Deterministic model

Tests construct all times, identities, definitions, events, and retry inputs
explicitly. Replay tests compare durable snapshots and reject gaps, duplicates,
stale sequences, invalid lifecycle changes, version mismatches, and malformed
event-specific fields. Fuzz targets exercise history, transitions, work leases,
activities, compensation, child dispatch, waits, operators, and worker
decisions at hostile boundaries.

## Failure matrix

| Boundary | Required invariant |
| --- | --- |
| Transition write before commit | rollback exposes neither history nor work |
| Caller transaction plus outbox | rollback or commit affects both records |
| Commit response lost | exact reconciliation decides retry safety |
| Attempt start before activity | redelivery cannot silently redispatch an in-flight effect |
| Activity result persistence | unknown result remains unknown |
| Timer firing before completion | crash leaves recoverable durable work |
| Signal persistence before acknowledgement | duplicate source identity is an exact replay |
| Lease renewal and finalization | stale fencing token is rejected |
| Compensation attempt and outcome | failure never appears as successful rollback |
| Child start | uncertain creation requires reconciliation |
| Dead-letter resolution | retry or discard is audited and token-fenced |

PostgreSQL integration tests exercise atomic transition/history/work writes,
optimistic conflicts, stable pagination, archive membership, fencing, dead
letters, migration order, rollback, process death, deadlocks, restart and
snapshot restore, streaming-replica promotion, and caller-owned transaction
composition. The
interoperability target builds a clean temporary consumer and proves workflow
transitions and optional outbox envelopes share one PostgreSQL commit while
inbound signal redelivery remains exactly deduplicated. It also partitions and
recovers a live Kafka broker, preserves retryable or ambiguous publication
classification, and proves stable duplicate identity and keyed order after
recovery. Caller acknowledgement still follows the confirmed workflow commit;
the package does not own it.

## Release gates

The scoped release contract includes formatting, tidy, safety, vet, tests,
race, exact statement coverage, lint, static analysis, vulnerability, secret,
license, SBOM, fuzz, exact viable mutation kills, documentation, API,
conformance, interoperability, benchmarks, and clean-consumer build proof. Go
commands use a disposable task-owned build cache.

`make soak` runs an explicit minimum 48-hour continue-as-new replay and worker
churn audit with hourly checkpoints, deterministic workflow clocks, bounded
concurrency, and retained-heap and goroutine ceilings. It uses a disposable Go
build and module cache against an immutable archive of the clean committed
workflow tree and reports its revision and SHA-256 input digest. A local harness
smoke may set `WORKFLOW_SOAK_DURATION=2s` and
`WORKFLOW_SOAK_ALLOW_SHORT=1`; that runs the current tree only to verify the
harness and is not multi-day evidence. Record the `workflow_soak_input`, final
`workflow_soak_result`, and uninterrupted exit status before treating the
multi-day boundary as verified.
