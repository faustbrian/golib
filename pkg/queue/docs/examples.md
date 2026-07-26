# Examples

## In-Memory Development

The [in-memory example](../examples/inmemory) demonstrates worker lifecycle
without an external broker. It is useful for API evaluation, not durability
testing.

## Redis

The [Redis example](../examples/redis) demonstrates the primary backend.
Production deployments must configure Redis persistence, memory policy,
authentication, network security, and monitoring independently of this
package.

## Valkey Streams

The [Valkey example](../examples/valkey) is runnable against standalone Valkey
9 and demonstrates enqueue, handler retry, reclaim, terminal dead-lettering,
monotonic stats, and graceful signal shutdown. TLS, ACL, Kubernetes, failure
recovery, and dual-backend rollout examples are in the
[Valkey Streams guide](backends/valkey-streams.md).

## Failure Scenarios

Before adoption, exercise:

- handler failure followed by bounded retry;
- worker termination before acknowledgement;
- cancellation during backoff;
- poison-message exhaustion and dead-letter handling;
- broker disconnect and reconnect;
- duplicate delivery against an idempotent handler.

Backend guarantees differ. Use [backend support](backend-support.md) and
[integration evidence](integration-evidence.md) when defining acceptance
criteria.
