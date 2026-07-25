# Evaluation

Evaluation applies the compiled order to one immutable context. The result
contains `Decision`, ordered matched rule IDs, a bounded explanation, bounded
redacted errors, duration, and derived facts.

`Matched` means at least one selected rule matched and no evaluation error
occurred. `Unmatched` means no selected rule matched. `Indeterminate` means
cancellation, timeout, operator failure, conflict, or a resource bound stopped
a reliable decision. Integrations must not treat `Indeterminate` as matched.

Forward chaining is literal and monotonic: a match may add an absent derived
fact. Re-deriving the same value is stable; changing an existing value is a
conflict. The compiler rejects static dependency cycles and evaluation stops
at `MaxIterations` even when a custom predicate obscures dependencies.

`EvaluateResolved` requests only required missing paths, in lexical order,
through the explicit resolver passed by the caller. Ordinary `Evaluate` never
performs I/O.
