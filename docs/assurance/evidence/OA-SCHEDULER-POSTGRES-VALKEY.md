# Scheduler PostgreSQL And Valkey Evidence

Observed at `2026-08-13T01:52:23Z` on `darwin/arm64` with Go `1.26.5`,
Docker Engine `29.6.2`, PostgreSQL `18.4-alpine`, and Valkey
`9.1.0-alpine`.

## Executed Proof

The scheduler lease-store integration suites ran against fresh task-owned
PostgreSQL and Valkey containers with cold task-owned Go caches:

- both stores allowed one active owner, selected one winner under simultaneous
  acquisition, fenced expired owners, extended only the current owner's lease,
  and supported release and recovery;
- canceled operations did not mutate either store;
- PostgreSQL qualified every operation against a caller-owned schema, used
  server time rather than a replica's local clock, left no partial lease after
  latency cancellation, failed after its pool closed, and recovered through a
  new pool;
- Valkey used server time rather than a replica's local clock, exposed a
  delayed timeout as an ambiguous outcome that could be reconciled through the
  fencing token, preserved the fence after lease expiry, failed after its
  client closed, and recovered through a new client; and
- both adapters passed the same generic lease-store conformance contract.

All selected integration tests passed. The task-owned containers and cold Go
caches were removed after the bounded run.

## Claim Boundary

This proves local scheduler lease-store conformance, fencing, cancellation,
ambiguity reconciliation, and reconnect recovery against single-node
PostgreSQL and Valkey containers. It does not prove scheduler runner dispatch,
queue delivery, process supervision, managed-service failover, cluster
topologies, network partitions, storage exhaustion, Kubernetes or ECS
`on-one-server` behavior, or rolling application-version compatibility. The
associated operational-assurance scenarios remain pending.
