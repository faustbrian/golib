# FAQ

## Is a hedge a retry?

No. A hedge starts while an earlier attempt can still be active. A retry starts
after the prior attempt finishes.

## Does cancellation prove the loser stopped?

No. It is cooperative. Use `Report.Wait` with a drain deadline.

## Why is a disposer mandatory?

Every attempt can return a resource-owning value. The selected value transfers
to the caller; all others must be closed exactly once.

## Does an idempotency key make hedging safe?

Only if every side-effecting downstream hop preserves it and suppresses
duplicates for the required window. The package does not infer that proof.

## Why did a budget denial return a downstream error?

The denial itself did not create that error. It prevented extra work; the error
came from the deterministic selected existing attempt.
