# Compatibility Policy

Before v1, exported APIs and schema may change and every change belongs in
`CHANGELOG.md`. After v1, SemVer applies independently to core, `goqueue`, and
`gotelemetry`. Compatibility surfaces include canonical encoding, migrations,
delivery semantics, errors, metrics, observer events, and publisher behavior.

| Surface | Versions | Evidence and boundary |
|---|---|---|
| Go | 1.26.5 minimum and stable | Linux, macOS, and Windows unit jobs |
| PostgreSQL | 14, 15, 16, 17, 18 | Full migration, crash, isolation, multi-process, retention, and plan integration per major |
| pgx | v5.10.0 | Caller transaction, pool, errors, cancellation, and connection loss |
| queue | `5036902eed67` | Standalone adapter race, coverage, fuzz, acceptance, error, and cancellation tests |
| telemetry | adapter-pinned | Runtime standard providers and propagator |

## Publisher matrix

| Publisher | Acceptance result | Context behavior | Retry interaction |
|---|---|---|---|
| Application implementation | `Publish` returns `nil` | Must honor cancellation according to its transport contract | Outbox classifies returned errors and caps its retry delay at one minute |
| `goqueue.Publisher` | synchronous `Queue` returns `nil` | Rejects an already-canceled context; pinned `queue` has no context parameter, so an in-flight call cannot be interrupted | A queue error uses outbox retry policy; retries performed by a job worker after queue acceptance are downstream and do not cause outbox republish |
| `gotelemetry` wrapper | delegates the wrapped publisher result unchanged | Extracts context and passes the relay context through | Adds no retry; generic span failure status does not classify the error |

A malformed publisher response is structurally impossible: `Publisher`
returns only `error`. `nil` means accepted and every non-nil value enters the
bounded failure policy; no partial-success fields are interpreted.

For `goqueue.Publisher`, cancellation after the synchronous queue call starts
does not change a successful acceptance into an error. Doing so would cause the
outbox to retry an envelope already accepted by `queue`. The call can delay
relay shutdown until the upstream producer returns, so deployments must bound
the configured worker implementation's own network and request timeouts.
The relay's maximum attempts bound counts only outbox publication attempts.
Adapters add no retry loop. Broker, job-worker, transport, and consumer retries
are separate downstream budgets and can multiply total processing attempts, so
operators must cap and monitor them independently.

Claims require a writable primary; cloud proxies and pooler modes need their
own validation and must refresh a changed endpoint after failover. The live
restart exercise proves readiness failure during outage and recovery of a
pre-outage pending record after route refresh; it does not certify a cloud
provider's promotion or DNS behavior. Before publishing adapters, replace
repository-local core `replace` directives with a released compatible core
version.

Publisher errors and error classes are fuzzed independently from option
validation. A publisher timeout after possible acceptance is tested against
real PostgreSQL and produces the expected bounded duplicate on retry.
