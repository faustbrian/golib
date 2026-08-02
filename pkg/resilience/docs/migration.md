# Migration

Adopt composition without changing focused policy behavior first:

1. Create bounded `Metadata` at the transport or use-case boundary.
2. Adapt existing focused policies to `Policy[T]` and preserve their public
   compatibility APIs.
3. Put logical retry and hedge policies before attempt-scoped breaker,
   bulkhead, rate, and adaptive policies.
4. Create one `WorkBudgetScope` per logical call and attach its returned
   context.
5. Remove focused retry and hedge budget state only after parity tests prove
   both draw from the shared scope.
6. Enable bounded observation and compare local rejection, downstream failure,
   and amplification metrics before rollout.

Do not replace application deadlines with a generic timeout wrapper. Do not
make a non-idempotent operation replayable merely because a retry policy can
invoke it repeatedly.
