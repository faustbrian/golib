# Deterministic recipes and operations

## Exact call campaigns

Use `Nth(n)` for one exact eligible call, `Every(n)` for a periodic schedule,
or `Sequence(pattern, repeat)` for a checked-in golden campaign. Set `Maximum`
to the number of permitted rule activations, not the number of calls.

## Seeded campaigns

Record the complete rule configuration, seed, generation, boundary, numeric
operation mapping, and event sequence. Replaying those inputs with the same
eligible-call ordering produces the same selections. Concurrent replay must
reproduce event `Sequence` ordering; goroutine launch order alone is not an
ordering guarantee.

## Composition

Use unique stable identities and explicit integer order. `Continue` permits
lower-precedence rules to add faults until `MaxFaultsPerDecision`; `Stop`
terminates composition after a match. Keep phase ordering visible in the rule's
fault list.

## Reset and snapshots

Snapshots contain current generation, evaluation and activation totals, and
per-rule calls/activations. Reset clears those bounded counters and advances
generation. A decision or adapter operation already selected stays attributed
to the older generation; reset does not mutate in-flight behavior.

## Observation

Observers receive immutable bounded events outside engine locks. Delivery is
synchronous to the selecting goroutine and may interleave under concurrency;
sort or partition by generation and sequence for analysis. Observers cannot
veto selection and their panics are contained.

## Cleanup

Always close caller-owned HTTP bodies, files, connections, and timers exactly
as without injection. Tests should assert both the injected result and cleanup.
Use caller contexts to bound latency and blocked delegated operations.

## Removal

Remove the explicitly wired injector or pass nil. There is no ambient rule
registry or environment switch to clean up. For runtime experiments, invoke
the terminal emergency disable first, then remove the composition-root wiring.
