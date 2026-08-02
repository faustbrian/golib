# Errors and observation

Use `errors.Is` for stable categories and `errors.As` for bounded details:

- `ErrInvalidComposition`, `ErrInvalidMetadata`, `ErrInvalidAttempt`, and
  `ErrNilOperation`;
- `ErrLocalRejection`, `ErrIgnored`, and `ErrPolicyFailure`;
- `ErrBudgetRejected`, `ErrBudgetClosed`, `ErrBudgetScopeMismatch`, and
  `ErrBudgetAlreadyAttached`;
- `ErrPermitCompleted` and `ErrPermitExpired`.

`ConfigurationError`, `LocalRejectionError`, `PolicyExecutionError`, and
`BudgetRejectionError` expose safe fields. Error strings deliberately omit the
arbitrary cause. The original cause remains available through `errors.Is` and
`errors.As`.

Events identify execution, policy, admission, attempt, cancellation, and
completion transitions. Identity and reason strings are bounded to 128 bytes.
Events do not retain results or arbitrary errors. A timeline is caller-owned
and bounded; modifying one result timeline cannot affect an executor or later
call. `EventExecutionCanceled` records both caller cancellation and total
deadline expiry, with the corresponding outcome kind as its bounded reason.
Observers receive the bounded timeline after execution settles, so callback
latency cannot alter operation behavior or budget accounting.
