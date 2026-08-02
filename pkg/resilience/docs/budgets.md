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

## Focused executors

`AdmitAttempt` is the compatibility seam for focused retry and hedge executors.
It allocates one unique ordinal from context-shared state, validates the origin
and parent ordinal, calls the attached `WorkBudgetScope`, and returns a child
context containing the admitted attempt. The caller must complete the returned
permit exactly once after physical work finishes.

An inner executor first calls `AttemptFromContext`. If an attempt exists, that
attempt is already owned by its outer executor and must not be admitted again.
Additional work uses `AdmitAttempt` with that attempt or another previously
admitted attempt as parent. Failed admissions may consume an ordinal but never
consume budget capacity or invoke downstream work.
