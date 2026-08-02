# API reference

The canonical generated reference is:

```sh
go doc -all github.com/faustbrian/golib/pkg/resilience
```

Primary entry points:

- `NewExecutor[T](policies...)` validates and freezes outer-to-inner policy
  composition;
- `Executor.Execute` runs one logical call synchronously;
- `Executor.WithClock`, `WithTimeline`, and `WithObserver` return immutable
  executor copies;
- `Policy`, `Stage`, and `Execution.WithAttempt` are the focused-policy seam;
- `NewMetadata` and `NewAttempt` validate bounded logical and physical identity;
- `Success`, `Failure`, `LocalRejection`, `Ignored`, and `PolicyFailure` build
  typed results;
- `NewBudget`, `WorkBudget`, `WorkBudgetScope`, and `Permit` own shared retry
  and hedge accounting;
- `WithBudgetScope` lets a custom budget implementation attach its scope;
- `BudgetScopeFromContext` exposes only an explicitly attached logical scope;
- `AdmitAttempt` coordinates unique retry and hedge lineage and returns its
  exactly-once permit; and
- `AttemptFromContext` exposes the physical attempt already owned by an outer
  executor so an inner executor cannot double-account it.

`Budget.Start` rejects an already scoped context with
`ErrBudgetAlreadyAttached`; nested execution must reuse the attached scope
rather than create a second accounting owner.

`PolicyDescriptor.Scope` is executable ordering metadata: logical policies
must wrap attempt policies. A repeatable policy identity may occur more than
once only when every occurrence explicitly declares the same scope and
repeatability.
