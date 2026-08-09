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
join their persisted outcomes, and persist known terminal outcomes. Race and
child orchestration, broader operator stores, and optional integrations are not
yet delivered.

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

The `postgres` package is the first durable adapter. Its versioned migration
creates instance, transition, history, and due-work tables in a caller-owned
schema. A commit uses optimistic sequence checks and one PostgreSQL transaction
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
release work.

A `WorkProcessor` must honor cancellation and stop all of its goroutines before
returning. It must persist the workflow transition represented by a work item
before returning `WorkComplete`. If an external activity outcome is unknown, it
must first persist unknown-outcome/reconciliation state; returning an error does
not make an uncertain side effect safe to redispatch. Worker shutdown stops new
claims, cancels active processors, preserves any already-known disposition, and
waits for processors to exit.

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
migration := postgres.SchemaMigration()
if _, err := pool.Exec(ctx, migration.Up); err != nil {
	return err
}

store, err := postgres.New(pool, postgres.Config{}) // schema: workflow
```

The caller creates and owns the schema, applies migrations, owns the pool, and
decides how migration rollback is authorized. The adapter does not publish or
acknowledge external messages.

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
