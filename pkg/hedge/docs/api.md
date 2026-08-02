# API reference

`Config[T]` requires a finite positive hedge count, an explicit replay-safety
declaration, exactly one delay strategy, total and cleanup timeouts, a clock,
shared budget, classifier, disposer, bounded resource identity, and explicit
factory-failure mode. `AttemptTimeout` is optional and cannot exceed the total.
Budgets declare a positive finite `Capacity` no greater than
`MaxBudgetCapacity`; admission behavior remains the budget implementation's
contract.

`Do` starts ordinal zero once. `AttemptFactory` owns construction of fresh
mutable request state. Its endpoint identity must be credential-free and at
most `MaxResourceLength`. `ClassificationSuccess` wins;
`ClassificationFailure` and `ClassificationCanceled` allow bounded work to
continue; `ClassificationTerminal` stops without a success.

`Report` exposes attempt and hedge counts, budget denials, selected ordinal,
bounded failure metadata, terminal reason, and `Wait`. `Wait` never starts new
work; it waits for already-started attempts and cleanup. `ExecutionError`
unwraps only the deterministic selected cause while keeping its own message
free of downstream text.
