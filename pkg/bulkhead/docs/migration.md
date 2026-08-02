# Migration

## From a buffered channel

Replace channel send/receive ownership with `Acquire` and `Permit.Release`.
Choose a stable resource name, decide immediate rejection versus a finite FIFO
queue, map channel capacity to bulkhead capacity, and handle typed terminal
errors. Remove ad hoc counters only after snapshots provide equivalent
operations evidence.

## From `golang.org/x/sync/semaphore`

Keep the same weight units. Add resource identity, a finite `MaxQueued`, a
finite `MaxWait`, observer policy, and application drain. Replace raw
`Release(weight)` calls with the owned permit so duplicate release cannot
inflate capacity. Document strict FIFO head-of-line behavior.

## From a worker pool

Do not move durable work into `bulkhead`. A bulkhead admits caller-owned
execution; it does not own workers, task lifetime, queue durability, retries,
or acknowledgements. Keep the queue/worker system and acquire capacity around
the actual constrained operation.

## From dynamic keyed maps

Do not translate attacker-controlled keys into automatic partitions. Declare a
finite `FixedPartitions` registry and explicitly create only reviewed resource
identities. Group tenants into bounded failure domains when per-tenant
cardinality cannot be strictly bounded.

## Rollout

Deploy metrics first, choose capacity from maximum replicas and fan-out, then
enable immediate rejection. Add a small bounded wait only when callers benefit
from short smoothing. During policy changes, account for old and new pod
capacities simultaneously and drain the old revision.
