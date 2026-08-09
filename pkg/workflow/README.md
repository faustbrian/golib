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
retry/dead-letter decisions, and synchronous lifecycle hooks. Automatic step
scheduling, compensation execution, signals, timers, operators, and optional
integrations are not yet delivered.

`Transition` is the persistence boundary: its contiguous history events and
bounded due-work records must commit atomically. `TransitionStore` exposes that
contract without choosing a database driver, and commit failures distinguish
not-committed, committed, and unknown durable outcomes. Callers must reconcile
unknown outcomes by transition ID before retrying.

The `postgres` package is the first durable adapter. Its versioned migration
creates instance, transition, history, and due-work tables in a caller-owned
schema. A commit uses optimistic sequence checks and one PostgreSQL transaction
for the transition identity, contiguous history, due work, and current instance
position. Exact transition replay is idempotent; conflicting identity reuse is
rejected. History reads use a bounded stable forward cursor. A transport error
from `COMMIT` is deliberately classified as unknown rather than retried as if
nothing happened. Due-work claims use atomic locked admission with stable
ordering. Lease expiry never exceeds the persisted work deadline, and every
retry or crash recovery increments the attempt and fencing token so a stale
owner cannot complete or release work.

A `WorkProcessor` must honor cancellation and stop all of its goroutines before
returning. It must persist the workflow transition represented by a work item
before returning `WorkComplete`. If an external activity outcome is unknown, it
must first persist unknown-outcome/reconciliation state; returning an error does
not make an uncertain side effect safe to redispatch. Worker shutdown stops new
claims, cancels active processors, preserves any already-known disposition, and
waits for processors to exit.

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
