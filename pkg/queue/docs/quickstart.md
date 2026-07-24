# Quickstart

## Install

```sh
go get github.com/faustbrian/golib/pkg/queue
```

## Choose A Backend

Redis Streams and Valkey Streams are independent durable adoption paths backed
by their native Go clients. Select one only after reading its acknowledgement,
reclaim, dead-letter, and topology guarantees in [backend support](backend-support.md).
Redis Pub/Sub, NATS, NSQ, and RabbitMQ remain explicit alternatives.

## Build A Worker

Create the backend with explicit connection options, register a task handler,
start it with a cancellable context, and treat shutdown errors as operational
failures. The runnable [Redis example](../examples/redis) shows the complete
lifecycle. The [Valkey example](../examples/valkey) shows bounded retries,
reclaim, dead-letter policy, statistics, and signal-driven shutdown.

## Production Checklist

- define idempotency for every task;
- set bounded retries and backoff;
- record attempt, latency, failure, and dead-letter metrics;
- test worker termination during processing;
- confirm backend persistence and eviction policy;
- run the backend-specific integration suite before rollout.

Continue with the [adoption guide](adoption.md) and
[delivery semantics](delivery-semantics.md).
