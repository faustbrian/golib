# Composition and timing

For policies supplied as `logicalA, logicalB, attemptA, attemptB`, invocation is:

```text
logicalA enter
  logicalB enter
    attemptA enter
      attemptB enter
        operation
      attemptB exit
    attemptA exit
  logicalB exit
logicalA exit
```

When `logicalB` retries, it invokes `attemptA(attemptB(operation))` again. The
constructor rejects an attempt policy outside a later logical policy because
that order would make attempt scope misleading.

The caller context remains the total boundary even if a custom policy passes
`context.Background()` to the next stage. Child contexts may shorten queue,
admission, or attempt deadlines. They cannot extend the total deadline.

Local denial must use `LocalRejection`; classifier, observer, or policy
mechanism failure must use `PolicyFailure`; only an invoked downstream
operation may produce `OutcomeOperationFailure`. This prevents breakers and
adaptive controls from learning from local saturation.
