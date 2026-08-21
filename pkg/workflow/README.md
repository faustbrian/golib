# workflow

`workflow` provides explicit building blocks for durable business workflows and
sagas. Definitions have stable names and immutable versions, and instances are
expected to persist the exact definition name, version, and fingerprint that
created them.

The module is under active development. The current API covers definition
compilation, explicit version migrations, immutable lifecycle history, and
deterministic replay. It also defines bounded explicit activity attempts and
unknown-outcome semantics, including replay of persisted attempt starts,
outcomes, and bounded retry admission times. Durable work can be atomically
claimed with bounded leases, monotonically increasing fencing tokens, crash
recovery after lease expiry, renewal, retry admission, completion, and explicit
dead-letter handling. Bounded workers add tenant-fair admission, lease renewal,
stale-owner cancellation, graceful draining, deterministic clocks, explicit
retry/dead-letter decisions, and synchronous lifecycle hooks. Durable timer
schedules atomically create due work, timer workers persist firing before lease
completion, and bounded inbound signals become idempotent transitions that must
commit before acknowledgement. Audited lifecycle operator commands atomically
record the authorized caller identity and reason before pause, resume, cancel,
or terminate. Fenced activity and compensation processors persist attempt
starts before handlers and preserve unknown outcomes across redelivery.
Ordered orchestration can schedule activities and timers, wait for signals and
audited human approvals, atomically admit bounded parallel activity branches,
join their persisted outcomes, select and persist bounded signal or approval
race winners, schedule version-pinned child workflows, and persist known
terminal outcomes. A fenced child-start processor persists each creation
attempt before invoking a caller-owned idempotent adapter, records known
creation, known absence, or uncertainty, and durably admits policy retries only
after a known-absent failure. The PostgreSQL adapter exposes stable unresolved
dead-letter pages and audited, idempotent, token-fenced retry or discard
commands. Optional composition uses the sibling CloudEvents adapter, outbox
PostgreSQL writer and Kafka or queue publishers; core workflow code imports none
of them.

`Transition` is the persistence boundary: its contiguous history events and
bounded due-work records must commit atomically. `TransitionStore` exposes that
contract without choosing a database driver, and commit failures distinguish
not-committed, committed, and unknown durable outcomes. Callers must reconcile
unknown outcomes by transition ID before retrying.

`NewActivitySchedule` atomically persists bounded activity input with the first
due-work record. A worker commits `NewActivityAttemptStart` before invoking the
external handler, then commits an explicit success, known failure, or unknown
outcome. `NewActivityRetry` records the deterministic backoff decision and the
next semantic attempt together; work redelivery retains the same attempt
idempotency key while a policy retry receives a new one.

The `postgres` package is the first durable adapter. Its immutable ordered
migrations create instance, transition, history, due-work, and dead-letter
resolution tables in a caller-owned schema. A commit uses optimistic sequence
checks and one PostgreSQL transaction
for the transition identity, contiguous history, due work, and current instance
position. Exact transition replay is idempotent; conflicting identity reuse is
rejected. History reads use a bounded stable forward cursor. A transport error
from `COMMIT` is deliberately classified as unknown rather than retried as if
nothing happened. Instance lists use immutable creation-time and identity
cursors across active or archived views, and uncertain transitions can be
reconciled as missing, exact committed, or conflicting identities. Due-work
claims use atomic locked admission with stable ordering. Lease expiry never
exceeds the persisted work deadline, and every retry or crash recovery
increments the attempt and fencing token so a stale owner cannot complete or
release work. A terminal transition archives its instance in the same database
transaction, so active and archived list views cannot lag the durable outcome.
Unresolved dead letters use failure-time and work-identity keyset pagination.
Retry or discard locks the exact work fencing token and writes the authorized
actor, reason, action, and complete command fingerprint in the same transaction
as retry readmission. Exact command replay is idempotent; conflicting command
reuse or a stale work token is rejected, and an uncertain commit must be
reconciled by replaying that command identity.

`postgres.Store.Stage` writes a transition through a caller-owned `pgx.Tx`
without committing or rolling it back. This is the explicit composition point
for application state and optional transactional outbox records: stage every
record in one transaction, commit it once, and allow no externally observable
progression before commit succeeds. A commit transport error remains unknown;
reconcile the transition identity before deciding whether the same transaction
intent is safe to retry. `Stage` returning nil means staged or exact replay, not
durably committed.

```go
tx, err := pool.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(ctx)

if err := store.Stage(ctx, tx, transition); err != nil {
	return err
}
if err := outboxWriter.Insert(ctx, tx, envelope); err != nil {
	return err
}
if err := tx.Commit(ctx); err != nil {
	// The outcome is unknown: reconcile transition.ID() before retrying.
	return err
}
```

A `WorkProcessor` must honor cancellation and stop all of its goroutines before
returning. It must persist the workflow transition represented by a work item
before returning `WorkComplete`. If an external activity outcome is unknown, it
must first persist unknown-outcome/reconciliation state; returning an error does
not make an uncertain side effect safe to redispatch. Worker shutdown stops new
claims, cancels active processors, preserves any already-known disposition, and
waits for processors to exit. Synchronous worker hooks report bounded claim,
readmission, processing, lease-heartbeat, completion, retry, dead-letter, and
failure kinds. A readmission may follow an explicit retry or lease-expiry
recovery; the package exposes the durable attempt rather than guessing the
cause. Work and tenant identities remain event data and must not become metric
labels.

`StepRace` currently accepts signal and approval branches, so selecting a
winner cannot imply cancellation of an already-started external side effect.
The earliest persisted receive time wins; definition order breaks an equal-time
tie. `EventRaceWon` must commit before later steps advance, and replay never
recomputes a different winner from signals accepted afterward.

`StepChild` pins a complete `DefinitionReference`. `NewChildSchedule` commits
the parent decision and `WorkChild` admission atomically.
`ChildWorkProcessor` then records `EventChildStartAttempted` before a
caller-owned adapter creates the child instance. `DecodeChildDispatch`
preserves the exact child identity, semantic attempt, and idempotency key
across redelivery. A redelivered in-flight attempt becomes
`EventChildStartUnknown` without calling the adapter again. Only a
known-absent retryable failure can create later `WorkChild`; `NewChildOutcome`
records a known child terminal result before parent orchestration advances.
Creating a child remains an idempotent external operation and an uncertain
start requires reconciliation; a durably observed terminal child outcome is
also sufficient evidence that the child existed.

`NewTimerSchedule` binds a timer-history decision and its `WorkTimer` record in
one transition. A timer processor persists `NewTimerFire` and only then returns
`WorkComplete`. `NewSignalAcceptance` uses the inbound message identity as the
transition idempotency boundary; a queue or broker adapter must acknowledge the
message only after `TransitionStore.Commit` succeeds or confirms an exact
idempotent replay. Optimistic conflicts require reloading history and deciding
whether the signal is already accepted or no longer applicable.

Compensation is explicit durable workflow state rather than an implied
rollback. `NewCompensationSchedule` atomically records the schedule decision
with `WorkCompensation`, and `NewCompensationAttemptStart` records the exact
attempt and idempotency key before the compensating side effect begins.
Compensation input inherits the activity step input bound.
`NewCompensationAttemptOutcome` preserves success, known failure, or an
unknown outcome, while `NewCompensationRetry` persists the independent retry
decision and its next semantic attempt together. Replay preserves schedule
order and manual resolutions. A manual resolution is reported as such; it is
never represented as a successful rollback.

`NewOperatorLifecycleCommand` accepts an already-authorized actor and produces
one idempotent optimistic transition containing the audit record followed by
the matching lifecycle decision. Replay rejects orphaned or mismatched audit
records. Authentication and authorization remain application policy; the
package does not infer privileges from actor names. `InspectInstance` performs
bounded deterministic replay over stable history pages, while `ExportHistory`
streams owned pages to a caller sink without accumulating unbounded history or
acknowledging external work.

```go
for _, migration := range postgres.SchemaMigrations() {
	if _, err := pool.Exec(ctx, migration.Up); err != nil {
		return err
	}
}

store, err := postgres.New(pool, postgres.Config{}) // schema: workflow
```

The caller creates and owns the schema, applies migrations in order, rolls them
back in reverse order when explicitly authorized, owns the pool, and authorizes
every operator actor. The adapter does not publish or acknowledge external
messages.

The package does not claim exactly-once external side effects. Applications
must make activities idempotent and treat unknown outcomes as requiring
reconciliation before retry.

## Definition example

```go
definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
	Name:    "order.fulfillment",
	Version: "1",
	Mode:    workflow.Orchestration,
	Steps: []workflow.StepSpec{{
		Name:        "reserve",
		Kind:        workflow.StepActivity,
		Target:      "inventory.reserve",
		Timeout:     time.Minute,
		InputLimit:  16 << 10,
		ResultLimit: 16 << 10,
		Retry: workflow.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		},
	}},
})
if err != nil {
	return err
}

registry, err := workflow.CompileDefinitions(definition)
```

Definitions are copied at construction and registry compilation rejects a
duplicate name/version pair. A new version never changes a running instance;
applications must select and durably persist an explicit migration edge.

## Documentation

- [Architecture and guarantees](docs/architecture.md) explains saga theory,
  orchestration, choreography, idempotency, versioning, and package boundaries.
- [Operations](docs/operations.md) covers schemas, deployment, recovery,
  operator commands, reconciliation, capacity, archival, and security.
- [Verification](docs/verification.md) maps failure boundaries to executable
  evidence and distinguishes local gates from environment-owned drills.
- [Security policy](SECURITY.md) records the trust model and reporting path.
- `Example_durableOrchestration` is a compiling end-to-end planning example.

## FAQ

### Does workflow provide exactly-once activity execution?

No. It persists attempt identity before an activity call and records known or
unknown outcomes afterward. The activity implementation must be idempotent and
an unknown outcome must be reconciled before redispatch.

### Does compensation roll back an external transaction?

No. Compensation is another explicit side effect with its own attempts,
timeouts, retry policy, unknown outcomes, and manual-resolution state. A failed
compensation never becomes a successful rollback.

### Who runs and restarts workers?

The package owns durable claim, fencing, renewal, retry, and shutdown semantics.
Kubernetes, ECS, systemd, or another process supervisor owns process lifetime.

### Must applications use the sibling queue, outbox, or scheduler modules?

No. Core contracts use explicit values and small interfaces. Applications may
compose those modules or equivalent implementations. There is no implicit event
bus, scheduler, service container, or package-initialization registration.

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
