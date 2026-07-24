# NSQ setup

Configure the nsqd address, topic, channel, maximum in-flight messages, and log
level. Each received message disables NSQ automatic response; the queue sends
`FIN` after successful handling.

`WithDeadLetter(topic, attempts)` configures the package-owned terminal topic
and a maximum of 2 through 101 broker delivery attempts. The default terminal
topic is `<source>-dead` with five attempts. Retryable failures send `REQ`
until the bound is reached. Canceled and infrastructure failures remain
recoverable without becoming terminal. Permanent and malformed failures are
terminal immediately.

A terminal delivery is encoded as a bounded version-1 envelope containing the
source topic, channel, NSQ message identity and timestamp, attempts,
classification, stable failure code, payload size, and payload. Oversized
poison bodies are omitted while their size remains recorded. The synchronous
nsqd publish response is received before the source receives `FIN`. Publish or
encoding failure sends `REQ`, so the source remains recoverable.
Validated producer-supplied `job.Metadata` is copied into the terminal envelope
and converted to v1 management fields without replacing the NSQ source ID.
Malformed and legacy payloads leave optional metadata explicitly unknown.

Publish and `FIN` are not atomic. Process death after terminal publication but
before `FIN` can produce a duplicate terminal record. Reconcile duplicates by
the stable source ID; exactly-once transfer is not claimed.
`DecodeDeadLetter` converts a terminal envelope into `management.JobRecord`
without exposing an NSQ client type. Payloads are hidden unless the caller
explicitly requests `management.PayloadRevealed`. NSQ cannot list or mutate a
topic without consuming it, so management list, inspect, retry, replay, delete,
and purge remain explicitly unsupported.

`WithConnectTimeout` bounds broker connection attempts, `WithRequestTimeout`
bounds idle requests, and `WithTouchInterval` controls in-flight heartbeats.
NSQ does not promise global ordering. Monitor consumer stats and redelivery
attempts, and size `MaxInFlight` with the queue worker count. Integration uses
nsqd 1.3.0 with `go-nsq` 1.1.0 and proves terminal transfer plus consumer
reconnection after a same-endpoint broker restart.
