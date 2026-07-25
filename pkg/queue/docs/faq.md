# FAQ

## Is this exactly-once?

No. Durable backends target at-least-once processing. Applications must handle
duplicates.

## Redis Pub/Sub or Redis Streams?

Pub/Sub for transient notifications; Streams for work that must survive
disconnects and be acknowledged after processing.

## Redis Streams or Valkey Streams?

Both are first-class native backends. Keep Redis when its deployment and
operator model fit; choose Valkey 9 when you want that server and native
`valkey-go` client. Valkey adds package-managed reclaim and dead-letter policy;
Redis's existing API and operator-managed PEL policy remain unchanged. No
migration is mandatory.

## Does Valkey cluster work?

No support claim is made. The current Valkey backend forces one standalone
client and one explicit endpoint. Cluster and managed failover require exact
topology integration evidence before they can become public options.

## Why are backend packages explicit?

Ack, ordering, redelivery, and delayed-delivery guarantees differ. A generic
facade would make correctness harder to see.

## Why does `NewWorker` panic?

It preserves the upstream return signature without allowing nil initialized
workers. New applications should use `NewWorkerE` and handle the error.

## Does the module ship Prometheus metrics?

Not in v1. Implement `Metric` and/or translate `Observer` events in the
application so the queue core remains vendor-neutral.
