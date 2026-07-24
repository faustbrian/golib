# Security and abuse resistance

Every enqueue caller and broker delivery is untrusted. The library validates
message envelopes before handler execution, but deployment authentication,
authorization, network isolation, and broker retention limits remain operator
responsibilities.

## Library-enforced bounds

- Encoded network deliveries are limited to one mebibyte before JSON decoding.
- Retry metadata is finite, non-negative, and limited to 100 attempts.
- Operational job metadata is allowlisted, limited to 256 bytes per identity
  or tag key/value and 32 tags per message, and remains inside the one-mebibyte
  encoded-envelope limit.
- The in-memory ring defaults to 10,000 queued jobs. `WithQueueSize(0)` is an
  explicit unbounded compatibility mode and is unsafe for untrusted admission.
- Handler concurrency is bounded by the configured worker count.
- Connection, request, publish, touch, and shutdown waits are bounded where the
  corresponding client exposes control.
- There is no decompression layer. Base64 payload decoding remains inside the
  encoded-envelope limit.

Redis Pub/Sub, Core NATS, NSQ, and RabbitMQ do not expose durable depth or
retention controls through this package. Redis Streams pending entries are
bounded only by broker and operational policy. Valkey bounds individual reads,
reclaim scans, payloads, buffers, pools, waits, attempts, and approximate stream
length, but server memory and retention remain operator responsibilities.
Configure broker-side quotas, retention, dead-lettering, and connection limits.

## Credentials and transport security

Do not put credentials in logs, observer metadata, queue names, subjects, or
payloads. Redis debug output is redacted. Redis, NATS, and RabbitMQ constructor
errors expose sanitized text when a client parser or dialer rejects a
credential-bearing URI; the original cause remains available to
`errors.Is`/`errors.As`, so application code must not separately log an
unwrapped client error.

Redis TLS is opt-in in both Redis packages. `WithSkipTLSVerify` is deliberately
named and MUST NOT be used in production. NATS uses the transport selected by
the configured client URL, and RabbitMQ uses `amqps` when supplied; this wrapper
does not expose every client TLS option. NSQ authentication and TLS are not
configured by this package. Use authenticated, certificate-verified endpoints
or a trusted private transport boundary, and verify those controls in the
deployment environment.

Valkey accepts an explicit cloned `tls.Config`, enforces TLS 1.2 or later, and
does not provide a skip-verification option. ACL credentials are separate from
the endpoint. Native Valkey errors retain their cause but expose fixed safe
text, and worker logs omit endpoints, credentials, payloads, and metadata.

Treat broker destination names as authorization boundaries. Give producers
publish-only access and consumers consume/settle access where the broker can
express that split. Rotate credentials outside this library and avoid embedding
long-lived secrets directly in source configuration.

## Hostile input and failure behavior

Malformed, oversized, or invalid-state envelopes are rejected before handler
execution. Redis Streams and Valkey Streams append malformed work to their
dead-letter streams before acknowledging the source and strip an
oversized poison body before terminal transfer; NSQ omits it from the terminal
envelope before `FIN`; RabbitMQ
confirms it to the terminal exchange before source acknowledgement when manual
acknowledgement is enabled. Redis Pub/Sub and Core NATS have no durable poison
message to settle.

Handlers must cooperate with context cancellation and bound their own network
and storage calls. Callback, metric, logger, and settlement panics are isolated,
but repeated hostile failures can still consume log, broker, and retry capacity.
Rate-limit admission and alert on decode, retry, pending, redelivery, and
settlement-error signals.
Observer exporters may aggregate only the stable event kind, classification,
and safe failure code. Never turn `Event.Err`, payloads, record IDs, tenant data,
or arbitrary backend/queue names into metric labels.

Run `govulncheck`, Staticcheck, the race suite, decoder fuzzing, and all pinned
backend integrations before release. See the [threat and failure model](failure-model.md)
for residual failure modes and [delivery semantics](delivery-semantics.md) for
the exact loss and redelivery boundaries.
