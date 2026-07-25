# Security And Hardening Audit

Audit date: 2026-07-15. Scope is the unreleased version 1 candidate at the
current `main` commit. The exhaustive surface is in
[`inventory.md`](inventory.md); compatibility is in
[`compatibility.md`](compatibility.md); the transition/crash matrix is in
[`architecture.md`](architecture.md).

## Guarantee matrix

| Property | Package guarantee | Required condition or residual |
|---|---|---|
| Application/outbox atomicity | Yes | Both writes use the exact caller-owned `pgx.Tx`, every writer call succeeds, and caller commit succeeds |
| Record durability after commit | PostgreSQL durability | Depends on database durability, backup, restore, and failover policy outside this package |
| Publication | At least once | Acceptance followed by an ambiguous delivery update can republish |
| Exactly once | No | Consumers must deduplicate; replay and disaster restore can duplicate |
| Claim ownership | Generation-token safe | Every transition requires the current token; expiry permits reclaim |
| Ordering | Optional per ordering key or topic | No global order; future-scheduled earlier records block their scope |
| Retry bound | Yes | Maximum attempts plus at most one minute of outbox backoff; publisher/downstream systems can have separate retry policy |
| Retention safety | Terminal states only | Direct prune is irreversible; mandatory archives must use archive-before-delete |
| Readiness | Writable PostgreSQL primary plus optional publisher health | A successful readiness result is connectivity, role, and session writability, not capacity |
| Payload-safe package diagnostics | Yes | Application logs, publishers, archives, and tracing policy remain application responsibilities |

## Threat model

| Threat | Mitigation and evidence | Residual responsibility |
|---|---|---|
| Record loss | Same-transaction writer; aborted/canceled/panic/connection-loss PostgreSQL matrix; no pending/leased prune API | Correct caller transaction use, PostgreSQL durability, backups |
| Premature deletion | State constraints, terminal-only bounded prune, archive callback before delete, rollback on error or panic | Archive implementation durability and idempotency |
| Duplicate explosion | Attempts, bounded backoff, lease tokens, terminal state, bounded explicit replay selection | Consumer idempotency; publisher and downstream retry budgets |
| Lease theft or stale acknowledgement | Random generation token on every claim/reclaim and token-qualified mutations | Protect database credentials; tokens are not authentication |
| Clock skew or process suspension | PostgreSQL clock owns eligibility and leases; stale tokens fail after reclaim | Database clock and failover correctness |
| Poison payload | Payload/version/metadata schema constraints, maximum attempts, dead-letter state | Codec validation and operator remediation |
| Payload or secret disclosure | Payload-free events/metrics, redacted persisted failures, panic-value containment | Restricted application logs, traces, archive, broker access |
| Replay abuse | Default-deny authorizer, bounded terminal ID selection, requester/reason, immutable audit, atomic conflict | Identity, tenant, and incident policy implemented by the application hook |
| Tenant escape | Quoted schema/table selection; no envelope field is treated as authorization | Per-tenant schema/table routing and application authorization |
| Concurrent operators | Row locks, `SKIP LOCKED`, complete-selection replay, migration advisory lock contract | Operational change control and cutoff selection |
| Replica or read-only routing | `Ping` checks session writability and recovery state; real `25006` mutation evidence | Writable endpoint and proxy/pooler validation |

## Findings

| ID | Severity | Finding and impact | Disposition |
|---|---|---|---|
| H-01 | High | Connectivity-only readiness was healthy on a read-only route while claims failed, allowing silent backlog growth. | Closed by `ErrNotWritable` and real read-only PostgreSQL evidence. |
| H-02 | High | The documented `migrations` integration rejected split migration filenames, so a clean production install through that runner was impossible. | Closed by the canonical source plus green real parser and concurrent-runner tests through `make migration-integration`. |
| M-01 | Medium | Arbitrary publisher error text was persisted and exposed through inspect/archive, allowing payload or credential disclosure and unbounded rows. | Closed by fixed safe diagnostics and 4096-byte schema ceiling. |
| M-02 | Medium | Public `uint16` payload versions exceeded `smallint`; nil/array metadata could become incompatible poison rows. | Closed by integer range, object constraint, empty-object encoding, and live regressions. |
| M-03 | Medium | Direct SQL bypassed payload, metadata, lease, replay, and diagnostic resource bounds. | Closed with schema constraints and matching writer/store preflight. |
| M-04 | Medium | Archive hook panic escaped while a transaction and row locks were held. | Closed by payload-safe `ErrArchiverPanic`; PostgreSQL proves rollback retains the row. |
| M-05 | Medium | Classifier/backoff panics could terminate worker goroutines and custom backoff could exceed the package bound. | Closed by safe transient fallback, typed errors, durable retry, and one-minute clamp. |
| M-06 | Medium | The changelog and README described version 1 as released before hardening was complete. | Closed; all work remains under `Unreleased` and release-candidate wording. |
| M-07 | Medium | Replay recorded requester text but had no authorization seam, so any holder of a Store could reset terminal records and produce duplicates. | Closed by a default-deny, copied-request, panic-contained `ReplayAuthorizer` and live no-mutation/no-audit evidence. |
| M-08 | Medium | Store claim configuration could be arbitrarily large and `Claim` preallocated to the requested limit, allowing an empty-table call to trigger an enormous allocation. Relay also trusted oversized custom Store responses. | Closed by absolute store/relay ceilings and `ErrClaimBatchOverflow` before relay-owned job-buffer allocation. |
| M-09 | Medium | Retry delay was capped but converted to an absolute relay-host timestamp, so positive host clock skew could strand a message far beyond one minute. | Closed by a duration-based Store contract and PostgreSQL-relative scheduling; live +24-hour skew evidence retains a 30-second delay. |
| M-10 | Medium | Deferred replay rollback used an unbounded background context and archive rollback reused a possibly canceled caller context, risking hung or ineffective cleanup during network failure. | Closed by detached five-second rollback cleanup for both internal transaction paths. |
| M-11 | Medium | The schema required a metadata object but allowed non-string values; direct SQL could create a row that claim leased and then failed to decode repeatedly. | Closed by a strict JSONPath string-value constraint with live number/array/object/boolean/null regressions. |
| M-12 | Medium | Attempts had no upper schema bound; direct SQL could create integer-overflow or relay-policy poison rows, and claim incremented a boundary value beyond the relay ceiling. | Closed by a 10,000 constraint and saturating claim increment with live rejection and terminal boundary evidence. |
| M-13 | Medium | PostgreSQL accepts infinite timestamps that pgx cannot decode into `time.Time`; direct SQL could therefore poison claims, inspection, retention, or replay-audit reads. | Closed by finite-time constraints on every message and replay-audit timestamp with live PostgreSQL 14 and 18 rejection evidence. |
| M-14 | Medium | A custom heartbeat panic escaped a relay-owned goroutine and terminated the process while a message remained leased. | Closed by payload-safe `ErrHeartbeatPanic`; live PostgreSQL 14 and 18 evidence proves publication cancellation, no transition, and expiry-safe lease retention. |
| M-15 | Medium | Finite PostgreSQL timestamps can precede year 0000 or exceed year 9999; direct SQL could create claimed envelopes whose canonical timestamps were outside the package's RFC3339 range. | Closed by schema range constraints on every message and replay-audit timestamp with live rejection and valid-boundary evidence on PostgreSQL 14 and 18. |
| M-16 | Medium | Panics from relay and store diagnostic clocks escaped containment and could terminate a process before delivery or pruning. | Closed by construction-time safe clock wrappers; unit and live PostgreSQL 14/18 evidence proves durable operations continue with zero-time diagnostics. |
| L-01 | Low | `SKIP LOCKED` is not strict FIFO and does not skip table locks. | Accepted and documented; live tests prove temporary oldest-row starvation/recovery and server timeout codes. |
| L-02 | Low | Pinned `queue` cannot cancel an in-flight synchronous producer call. | Accepted upstream contract; deterministic test prevents cancellation after acceptance from manufacturing a duplicate. Worker network/request timeouts must bound the call. |
| L-03 | Low | An out-of-range replay schedule reached authorization and PostgreSQL before the schema rejected it, exposing a database-specific error for invalid public input. | Closed by pre-authorization year validation with unit and live PostgreSQL 14/18 no-mutation/no-audit evidence. |
| L-04 | Low | Go cannot safely preempt an injected callback that ignores context; a stuck publisher can delay shutdown and a stuck archive hook can retain row locks. | Accepted language/runtime boundary; every callback contract now requires cancellation cooperation and finite application I/O deadlines. |

All identified high and medium findings are resolved. Every local and remote
release gate was rerun cleanly on 2026-07-15.

## Deterministic evidence map

| Area | Evidence |
|---|---|
| Writer atomicity | `TestApplicationWriteAndOutboxRecordAreAtomic`; wrong-transaction mismatch and failure cases in `TestHardeningPersistenceContracts/keeps_writer_failures_atomic` |
| Schema contracts and bounds | `TestHardeningPersistenceContracts/accepts_the_full_public_payload_version_range`, metadata shape, finite/envelope-range time, resource-ceiling, writer-ceiling, and oversized-input subtests |
| Claim contention and ownership | concurrent claims in `TestApplicationWriteAndOutboxRecordAreAtomic`; fairness, timeout, read-only, SERIALIZABLE, `40001`, `40P01`, four-process contention, and actual post-claim process-death recovery in `TestHardeningPersistenceContracts` |
| Lease expiry and late updates | reclaim and stale-token assertions in `TestApplicationWriteAndOutboxRecordAreAtomic` |
| Clock skew | PostgreSQL-owned claims, leases, and transitions plus `uses_the_PostgreSQL_clock_for_retry_scheduling` with a +24-hour relay clock |
| Database outage/restart | backend termination before writer/commit plus stopped-container readiness failure and pre-outage record delivery after endpoint refresh |
| Ordering | ordering-key, topic, and concurrent ordering-race sections in `TestApplicationWriteAndOutboxRecordAreAtomic` |
| Publish ambiguity | accepted-publish/delivery-failure/reclaim and accepted-then-timeout durable-retry duplicate sections in `TestApplicationWriteAndOutboxRecordAreAtomic` |
| Cancellation | `proveCanceledTransitionsAreAtomic`; `TestRunOnceReleasesClaimsOnCancellation`; live graceful relay cancellation section |
| Publisher and callback panics | Publisher, policy, heartbeat, observer, logger, and diagnostic-clock unit cases plus live publisher, policy, heartbeat, and clock PostgreSQL evidence |
| Replay and retention | default-deny authorization, live replay/audit/archive/prune, strict cutoff, operator lock, long snapshot/VACUUM, archive/replay race, dead retention, and archive-panic cases |
| Resource cleanup | goleak relay suite, bounded relay transition cleanup, and `TestInternalTransactionsBoundRollbackCleanup` for replay/archive transactions |
| Migration | Explicit source/target/concurrency/rollback matrix; `TestMigrationsExposeReversibleSchema`; live up/down/reapply; green `TestGoMigrationsLoadsOutboxSource` and `TestGoMigrationsConcurrentCleanInstall` through the reproducible sibling gate |
| Query plans | empty, normal 150-row, and large 40,100-row claim/delivered/dead plan assertions in `TestApplicationWriteAndOutboxRecordAreAtomic` |
| Coverage/race/leaks/fuzz/bench | `make coverage`; `make test-race`; goleak relay suite; five fuzz targets; relay-1,000, writer-100, adapter, and PostgreSQL empty/1,000/100,000-row benchmarks |

## Release-gate commands

| Gate | Command | Result on 2026-07-15 |
|---|---|---|
| Complete local-equivalent suite | `make check` | Pass: format, module, safety, vet, unit, PostgreSQL 18, race/leak, meaningful 100% production coverage, five fuzzers, benchmarks, recovery, docs, and three vulnerability scans |
| PostgreSQL major matrix | `for version in 14 15 16 17 18; do make integration POSTGRES_VERSION=$version; done` | Pass on 14, 15, 16, 17, and 18 |
| Recovery major matrix | `for version in 14 15 16 17 18; do make recovery POSTGRES_VERSION=$version >/dev/null || exit $?; echo "PostgreSQL $version recovery PASS"; done` | Pass on 14, 15, 16, 17, and 18 with fail-fast per-major evidence |
| Migration runtime | `make migration-integration POSTGRES_VERSION=18` | Pass: parser and concurrent clean install |
| Standalone adapters | `make adapter` | Pass: `queue` and telemetry race suites at 100% coverage |
| Remote workflows | PostgreSQL, CI, publisher integration, fuzz, benchmark, and security workflows for the release commit | Pass: [CI and publisher adapters](https://github.com/faustbrian/golib/pkg/outbox/actions/runs/29414378391), [PostgreSQL 14-18](https://github.com/faustbrian/golib/pkg/outbox/actions/runs/29414378471), [security](https://github.com/faustbrian/golib/pkg/outbox/actions/runs/29414378352), [fuzzing](https://github.com/faustbrian/golib/pkg/outbox/actions/runs/29414394360), and [benchmarks](https://github.com/faustbrian/golib/pkg/outbox/actions/runs/29414395951) on the release tree; the pull-request-only dependency review was intentionally not applicable to the `main` push |

## Current release verdict

**GREEN.** Every local command and named remote workflow in the table is green,
all identified package high/medium findings are closed, and the PostgreSQL
integration and recovery matrices pass on every supported major. No release
blocker identified by this audit remains open.

Residual delivery risks under this green verdict remain: publisher acceptance
followed by an ambiguous PostgreSQL result can duplicate; application replay
and disaster restore can duplicate; successful archival followed by ambiguous
commit can archive twice; and consumer idempotency, tenant authorization,
credentials, TLS, broker durability, database durability, and backup policy
remain application/operator responsibilities.
