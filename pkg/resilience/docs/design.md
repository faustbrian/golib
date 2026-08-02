# Design and ownership

The package owns only concepts that must be shared across focused resilience
algorithms: composition, common outcome vocabulary, total-context enforcement,
bounded events, and amplification accounting.

It intentionally does not own retry schedules, hedge delays, circuit state,
rate tokens, fallback selection, cache freshness, semaphore fairness,
bulkhead partitioning, adaptive algorithms, transport behavior, or business
degraded modes. Those concerns remain in focused packages.

Executors are immutable values. A call owns its execution metadata and optional
event recorder. A budget mutex owns resource, scope, and permit accounting.
Observer callbacks run after recorder mutation and without the budget lock.
Operation execution remains on the caller goroutine.

The policy API builds a finite wrapper chain, so there is no registry or graph
edge through which a composition cycle can be introduced. Construction rejects
nil policies, typed nils, invalid identities, incompatible duplicates, invalid
scope order, descriptor panic, wrapper panic, and nil stages.
