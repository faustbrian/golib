# resilience

`resilience` is the small composition foundation for the focused resilience
libraries in `golib`. It provides deterministic generic policy composition,
typed outcomes, caller-owned total deadlines, bounded observation, and one
process-local work budget shared by retry and hedge. It does not hide a default
policy stack or implement focused resilience algorithms.

## Quick start

```go
metadata, err := resilience.NewMetadata(
    requestID,
    "app.search_postal_codes",
    "postal:FI",
)
if err != nil {
    return err
}

executor, err := resilience.NewExecutor[Response](
    retryPolicy,   // logical scope
    breakerPolicy, // attempt scope
    bulkheadPolicy,
)
if err != nil {
    return err
}

result := executor.Execute(ctx, metadata,
    func(ctx context.Context, attempt resilience.Attempt) (Response, error) {
        return client.Search(ctx, request)
    },
)
return result.Err
```

Policies are supplied outer-to-inner. All logical policies must precede all
attempt policies. This makes retries or hedges invoke the complete attempt
stack for every physical attempt instead of accidentally applying an
attempt-scoped policy only once.

## Shared work budget

Retry and hedge must use the same `WorkBudgetScope` for a logical call:

```go
budget, err := resilience.NewBudget(resilience.BudgetConfig{
    MaxResources:                 1_024,
    MaxAdditionalPerExecution:    2,
    MaxConcurrentAdditional:      100,
    MaxAdditionalPerWindow:       1_000,
    AdditionalWindow:             time.Minute,
    PermitTTL:                    30 * time.Second,
    Clock:                        clock,
})
scope, budgetContext, err := budget.Start(ctx, metadata)
defer scope.Close()

result := executor.Execute(budgetContext, metadata, operation)
```

The terminal stage admits every physical attempt centrally. Original work is
recorded once; retry and hedge attempts draw from the same per-execution,
concurrent, and rolling-window limits. A policy cannot classify budget denial
as downstream failure because it receives `OutcomeLocalRejection` and an error
matching `ErrBudgetRejected`.

The built-in budget is process-local. `N` additional permits on each of `R`
pods allow up to `N * R` concurrent additional attempts. Use a separate,
explicit distributed implementation of `WorkBudget` only when cluster-wide
coordination is actually required.

## Cancellation and ownership

The caller context is the total execution boundary. Policies may pass a
shorter child context but cannot extend or detach from the caller deadline.
The executor does not race operations against timers and does not create a
goroutine to invoke synchronous work. If an operation ignores cancellation,
the call remains blocked until that operation returns and a successful return
is reported honestly.

Operation panics release acquired work permits, emit terminal observer events,
and preserve the original panic. Observer panics are recovered after state
changes. Bounded observer events are delivered synchronously after the outcome
and accounting settle, outside policy locks. A slow observer delays return to
the caller but cannot consume the operation deadline or change its result.

## Outcomes

The common taxonomy is:

- `OutcomeSuccess`;
- `OutcomeOperationFailure`;
- `OutcomeLocalRejection`;
- `OutcomeCancellation`;
- `OutcomeDeadline`;
- `OutcomeIgnored`;
- `OutcomePolicyFailure`.

Constructors preserve typed values and causes. Errors remain usable through
`errors.Is` and `errors.As`; error strings contain only bounded policy, stage,
and reason identifiers.

## Diagnostics

Plain execution retains no timeline and currently has a zero-allocation fast
path. Enable bounded events explicitly with `WithTimeline` or `WithObserver`.
Events never retain operation values, arbitrary errors, context values, URLs,
credentials, tenant IDs, or caller-provided maps.

## Documentation

- [API reference](docs/api.md)
- [Composition and timing](docs/composition.md)
- [Budget accounting](docs/budgets.md)
- [Design and ownership](docs/design.md)
- [Errors and observation](docs/errors.md)
- [Kubernetes semantics](docs/kubernetes.md)
- [Operations and security](docs/operations.md)
- [Migration](docs/migration.md)
- [Performance](docs/performance.md)
- [Hardening evidence](docs/hardening.md)
- [FAQ](docs/faq.md)
- [Security policy](SECURITY.md)
- [Release notes](CHANGELOG.md)
