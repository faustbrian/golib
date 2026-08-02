# Composition

- **Bulkhead:** hedging outside admission makes every attempt compete for a
  permit and keeps total dependency concurrency visible. Hedging inside one
  logical permit can bypass the intended dependency bound unless the bulkhead
  separately accounts each attempt.
- **Circuit breaker:** place the breaker inside to observe each downstream
  attempt, or outside for one logical observation. Document which health signal
  is intended; loser cancellation is not automatically dependency failure.
- **Rate limits:** per-attempt permits measure actual load. One shared logical
  permit hides amplification and is safe only under an explicit downstream
  contract.
- **Deadlines:** the total timeout bounds the logical operation; an optional
  shorter attempt timeout prevents any one attempt from consuming the entire
  window.
- **Cache:** perform an eligible cache lookup before hedging so a hit starts no
  duplicate downstream work.
- **Retry:** retry is sequential after completion; hedge is concurrent before
  completion. A composition needs one hard amplification budget and no preset
  multiplies the two. Enable `UseResilienceBudget` on both policies and pass one
  scoped context through the complete composition. The current attempt is
  reused across the nested boundary and only newly created physical work draws
  another permit.
- **Adaptive throttle:** local throttle rejection is an admission decision, not
  a hedgeable downstream failure by default. Starting another attempt evades
  the protection and increases load.
