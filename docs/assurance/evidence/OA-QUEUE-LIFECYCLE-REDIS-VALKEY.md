# Queue Lifecycle Redis And Valkey Evidence

Observed at `2026-08-13T01:42:02Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, Redis `8.6.4-alpine`, and Valkey `9.1.0-alpine`.

## Executed Proof

Four focused `queueservice` integration scenarios ran through the public
queue, queue-service, and service-lifecycle APIs against fresh task-owned Redis
and Valkey containers:

- a handler deadline retained unacknowledged work until lease expiry, after
  which a replacement worker redelivered and settled the same work;
- an unavailable dead-letter destination prevented source acknowledgement,
  retained the delivery, and allowed a replacement worker to dead-letter it
  after the destination recovered;
- two old and two new worker runtimes overlapped during scale-up, admitted
  work drained before each old process stopped, and all 96 unique tasks had one
  successful settlement owner; and
- processes were terminated before the handler effect, after the handler
  effect but before settlement, and after settlement. Two replacement
  processes recovered pending work, exposed the expected at-least-once
  duplicate window after an unconfirmed side effect, and left no pending work.

Every scenario passed independently for Redis Streams and Valkey Streams. The
containers and cold task-owned Go caches were removed after the bounded run.

## Claim Boundary

This proves local queue-worker lifecycle, redelivery, poison-work retention,
scale-up, rolling replacement, process termination, and truthful at-least-once
semantics for the queue-service integration. It does not prove broker process
failover, cluster topology, Kubernetes or ECS orchestration, image rollback,
mixed queue serialization versions, network partitions, or managed Redis and
Valkey behavior. The three associated operational-assurance scenarios remain
pending.
