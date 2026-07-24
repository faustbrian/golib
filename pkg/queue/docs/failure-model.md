# Threat and failure model

The trust boundary begins at every enqueue caller and broker delivery. Broker
availability, network continuity, handler correctness, and observer code are
not assumed.

| Threat or failure | Library behavior | Deployment responsibility |
| --- | --- | --- |
| Malformed or oversized JSON | Rejects before execution; encoded limit is 1 MiB | Alert on repeated poison deliveries |
| Hostile retry metadata | Rejects negative, non-finite, or over-100 retry state | Choose job deadlines below service shutdown budget |
| Slow or blocked handler | Cancels its context at deadline/shutdown | Handler must cooperate and bound downstream calls |
| Observer/logger/metric panic | Recovers and preserves worker accounting | Repair the integration; callback observations may be lost |
| Ack/nack failure or panic | Returns a failed settlement event | Reconcile duplicates or pending work |
| Redis Pub/Sub/Core NATS disconnect | Work can be silently lost by protocol design | Use only for transient work |
| Redis Streams process loss | Unacked entry remains in the PEL | Monitor, claim, and dead-letter pending entries |
| Valkey Streams process loss | Unacked entry remains pending and is reclaimed after bounded idle time | Set idle above valid handler runtime and make handlers idempotent |
| Valkey ack timeout | Ack may be applied or remain pending | Inspect group state; never infer replay safety from timeout alone |
| Valkey dead-letter ack failure | Append may succeed while source remains pending | Deduplicate dead letters by original stream ID |
| NSQ process loss | Unfinished work is eligible for redelivery | Make handlers idempotent |
| RabbitMQ process loss | Before confirm the source remains recoverable; after terminal confirm but before source ack a duplicate may remain | Reconcile by job identity and bounded terminal headers; use durable queues |
| Broker unavailable at startup | Error-returning constructor fails and cleans partial state | Supervise restart with bounded backoff |
| Runtime connection loss | NATS, NSQ, Redis, and Valkey reconnect on later commands; RabbitMQ worker is terminal | Accept lossy gaps or replace the worker as documented |
| Credential-bearing broker URI | Debug output and constructor error text are redacted | Do not log raw options or separately unwrapped client errors |
| TLS disabled or verification skipped | Redis TLS is opt-in; skip-verify is explicit; Valkey accepts only caller-supplied verified TLS configuration | Require verified TLS and broker authentication in policy |
| Process termination | In-memory and lossy transports lose work | Use a durable backend and idempotent side effects |

There is no compression layer, so decompression bombs do not apply. JSON base64
decoding of `Body` remains within the encoded-message limit. No backend or core
API promises exactly-once execution.

## Failure classification

`management.NewFailure` attaches one stable classification and a bounded safe
code while preserving the original cause through `errors.Is` and `errors.As`.
The classifications are retryable, permanent, malformed, canceled, and
infrastructure. Unknown or invalid classified errors resolve to retryable so a
bad marker cannot discard work.

`management.ResolveFailure` examines wrapped and joined errors and returns the
winning classification and safe code. `management.ClassifyFailure` is the
classification-only convenience. Precedence is
infrastructure, canceled, malformed, permanent, then retryable. Infrastructure
wins because an acknowledgement, lease, broker, or dead-letter destination
failure makes durable outcome uncertain. Cancellation wins over handler
terminality because shutdown and deadline interruption do not become dead
letters without explicit backend policy. Plain handler errors are retryable.
Context cancellation and deadline expiration are canceled, never permanent.

Panic, retry exhaustion, unsupported payload version, lease loss,
acknowledgement failure, dead-letter destination failure, and administrative
quarantine use stable failure codes in addition to the classification. The
code is operator-safe metadata, not an arbitrary error string or stack trace.
Classified error strings never include their wrapped cause, while `errors.Is`
and `errors.As` still permit programmatic diagnosis. Recovered handler panics
are permanent `handler_panic` failures. Acknowledgement and failure-settlement
errors are infrastructure failures; raw callback panic values are discarded.
The public `management.FailureCodeUnsupportedPayloadVersion`,
`management.FailureCodeLeaseLost`,
`management.FailureCodeDeadLetterDestinationUnavailable`, and
`management.FailureCodeAdministrativeQuarantine` constants are the canonical
codes for package backends, custom adapters, and administrative integrations.
Redis Streams, Valkey Streams, NSQ, and RabbitMQ produce the destination code
when terminal persistence fails; stream settlement with no remaining owned
delivery produces the lease-loss code.

Protocol claims are grounded in the official documentation for
[Redis Pub/Sub](https://redis.io/docs/latest/develop/pubsub/),
[Redis Streams](https://redis.io/docs/latest/develop/data-types/streams/),
[Valkey Streams](https://valkey.io/topics/streams-intro/),
[Core NATS](https://docs.nats.io/nats-concepts/what-is-nats),
[NSQ](https://nsq.io/overview/design.html), and
[RabbitMQ acknowledgements and confirms](https://www.rabbitmq.com/docs/confirms).
