# Budget accounting

One `BudgetScope` represents one logical call. It records exactly one original
attempt and all retry or hedge attempts by stable ordinal and parent ordinal.
Additional work is admitted only after its parent was admitted by the same
scope.

The built-in budget enforces:

- a finite additional-attempt total per logical execution;
- a finite number of concurrent additional attempts per resource;
- a finite rolling-window additional-attempt count per resource;
- a finite number of retained resource identities; and
- permit expiry that lazily recovers abandoned capacity.

Totals and rolling admissions are not refunded. Concurrent capacity is
released exactly once by `Permit.Complete` or recovered at `PermitTTL`.
Completion after expiry returns `ErrPermitExpired`; duplicate completion
returns `ErrPermitCompleted`.

The scope must be attached by `Budget.Start`. Starting another scope from an
already scoped context returns `ErrBudgetAlreadyAttached`, preventing nested
executors from replacing the owner and bypassing per-execution limits. A
detached context, a foreign scope, mismatched execution metadata, duplicate
ordinal, missing original, or unknown parent is rejected before operation
invocation.

Clocks are injected for deterministic tests. The process-local implementation
does not persist accounting across restart and does not coordinate replicas.
Custom process-local or distributed implementations attach their scope with
`WithBudgetScope`; the context key remains private so callers cannot replace a
scope without passing through the same ownership checks.
