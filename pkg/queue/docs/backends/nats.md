# NATS setup

The `nats` package uses Core NATS queue subscriptions. Configure one or more
addresses, a subject, and a queue group. Core NATS is at-most-once and has no
durable ack/redelivery; this backend is not JetStream.

`WithConnectTimeout` bounds startup and `WithRequestTimeout` bounds an idle
request. Core NATS deliveries are deliberately not passed to `Msg.Ack`, because
that method requires a reply subject and is not a Core NATS settlement API.

Use it for low-latency work where loss during disconnect is acceptable. Select
a durable backend when processing must survive consumer or broker interruption.
The client reconnects after a same-endpoint broker restart, but messages sent
without a subscriber are irrecoverably lost. Shutdown uses the configured
connect timeout as the NATS drain bound and waits for the closed callback.
Credential-bearing client URL failures return sanitized constructor text.

Integration uses NATS Server 2.10.29; hermetic fault tests also use 2.11.15 with
`nats.go` 1.52.0. The package limit of one mebibyte matches the common Core NATS
default, but deployment server limits remain authoritative.
