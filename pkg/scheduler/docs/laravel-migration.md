# Laravel scheduler migration

Treat migration as a behavior inventory, not a syntax translation. Record each
Laravel event's command or closure, parameters, time zone, filters, maintenance
policy, mutex name and TTL, background behavior, output handling, and hooks.
Run old and new schedulers against non-production durable backends before a
rolling cutover.

## API mapping

| Laravel | scheduler | Intentional difference or action |
|---|---|---|
| named closure or command | `NewSchedule(name, task, interval)` | stable name and task are required; no anonymous production identity |
| `cron`, `daily`, `hourly` | `Cron`, `Daily`, `Hourly` | five-field, minute-resolution expressions only |
| sub-minute helpers | no equivalent | keep on Laravel or use a purpose-built worker loop |
| `timezone` | `WithTimezone` | IANA zone is compiled at startup; folds are physical instants |
| `between`, `unlessBetween` | condition or absolute `WithDateBounds` | date bounds are absolute instants, not recurring daily windows |
| `when` | `WithCondition` | bounded to 32 and a runner callback deadline |
| `skip` | inverse condition | return false to omit the occurrence |
| `environments` | `WithEnvironments` | runner environment is explicit configuration |
| `evenInMaintenanceMode` | `MaintenanceRun` | runner maintenance mode is supplied by the application |
| `onOneServer` | `WithOneServer` | fenced occurrence lease, not a Laravel cache mutex |
| `withoutOverlapping` | `WithoutOverlap(OverlapSkip, ttl)` | renewable task lease with fencing and server time |
| `schedule:clear-cache` | CLI or HTTP fenced recovery | no bulk unlock; current token and owner isolation are required |
| `runInBackground` | durable `queue.Dispatcher` | no child-process or shell execution in core |
| queued job or command | queue envelope task and parameters | workers own business execution and retry policy |
| `before`, success, failure | `WithHooks` | six fixed hooks, panic-contained and deadline-bounded |
| `pingBefore`, `pingAfter` | application hook or telemetry | core does not make network callbacks |
| output append, email, or storage | worker-owned output policy | core captures no command output |
| `schedule:list` | CLI `list` | registry is immutable after startup |
| `schedule:test` | CLI `test` | calculates boundaries; control surfaces execute no shell command |
| schedule groups | Go construction helpers | group defaults are application code, not mutable runtime state |
| custom cache store | PostgreSQL or Valkey adapter | every replica must share one backend and namespace |

## Identity and rollout

Every schedule must have a stable name and task identity. Parameterized
schedules should use `WithParameters`; canonical JSON produces stable parameter
identity. Bump `WithVersion` for semantic task changes even when cron is
unchanged. Version and timing affect revision identity but not coordination
identity, so matching physical occurrences remain deduplicated during rollout.

Changing the name, task, or parameters creates a new coordination identity.
Changing cron, time zone, or jitter can create distinct old and new physical
boundaries even though matching instants still deduplicate. Use the
[rolling deployment matrix](hardening.md#rolling-deployment-matrix) to choose a
drain, feature gate, or staged activation.

## Execution and overlap

Laravel commands often perform work in the scheduler process. Prefer a
`queue.Dispatcher` backed by durable `queue` storage. A successful dispatch
does not mean the job completed, and a lease does not remove the need for job
idempotency. Copy the occurrence key and fencing token into every durable
envelope and downstream protected write.

`OverlapReplace` requires a `lease.ReplacementStore`. Its `Replace` operation
must cancel the prior owner, transfer ownership atomically, and fence
downstream effects before returning. Built-in stores intentionally do not
claim that capability. Do not translate `withoutOverlapping` into forced
unlock during normal execution.

## Migration verification matrix

For every migrated schedule, record and test:

| Field | Required comparison |
|---|---|
| boundary corpus | normal day, DST gap, DST fold, month end, leap day |
| delayed tick | skip, newest-once, or bounded catch-up intent |
| identity | old mutex name versus new name, task, and parameters |
| maintenance and environment | allowed and rejected executions |
| overlap | active owner, expiry, heartbeat, stale release, manual recovery |
| dispatch | queue durability, occurrence deduplication, retry and output owner |
| crash | before dispatch, after queue submission, after completion |
| rollout | old-only, mixed old/new, new-only, rollback |

Do not remove the Laravel schedule until mixed-version evidence shows no silent
double-run and no lost intended boundary. Keep application-specific deviations
beside this matrix in the migration change set.
