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
letters, migration order, rollback, and caller-owned transaction composition.

## Release gates

The scoped release contract includes formatting, tidy, safety, vet, tests,
race, exact statement coverage, lint, static analysis, vulnerability, secret,
license, SBOM, fuzz, exact viable mutation kills, documentation, API,
conformance, interoperability, benchmarks, and clean-consumer build proof. Go
commands use a disposable task-owned build cache.

Stress, soak, PostgreSQL failover, backup/restore, and broker-partition drills
depend on deployment resources. Their absence must be reported as an unverified
boundary; a unit test or local mock is not a substitute. Record environment,
duration, load, fault timing, tool versions, and resource ceilings with any such
result.
