# Integration evidence

Integration tests use the `integration` build tag and Testcontainers 0.42.0.
Images are pinned by version and digest so a tag cannot silently change the
evidence environment.

| Backend | Server evidence | Go client |
| --- | --- | --- |
| Redis Pub/Sub and Streams | Redis 6.2.22, `sha256:3b477d...0bce` | `go-redis/v9` 9.19.0 |
| Redis Cluster | Single-node, all-slot Redis 6.2.22 cluster with host-reachable discovery | `go-redis/v9` 9.19.0 |
| Valkey Streams | Valkey 9.1.0, `sha256:8e8d64...e09411`, standalone | `valkey-go` 1.0.76 |
| NATS | NATS Server 2.10.29, `sha256:5498ba...6e2f`; hermetic server 2.11.15 | `nats.go` 1.52.0 |
| NSQ | nsqd 1.3.0, `sha256:1a369c...c78a` | `go-nsq` 1.1.0 |
| RabbitMQ | RabbitMQ 3.13.7 management, `sha256:e582c0...1f69` | `amqp091-go` 1.11.0 |

The local audit ran with Go 1.26.5 on Darwin arm64 while the supported minimum
remains Go 1.25.12. Container tests cover enqueue, consume, handler failure,
timeout, shutdown, backend-specific settlement, and same-endpoint broker
interruption/restart. Core NATS and NSQ reconnect live. Redis Pub/Sub rejects
an outage publish and resubscribes after restart. Redis Streams retains backlog
across restart. RabbitMQ channels are terminal after connection loss, so the
test proves that a replacement worker is required and restores delivery. Unit
fault injection covers partial initialization, closed channels, malformed
delivery, settlement error, and publish confirmation failure.

Valkey runs in its own release-blocking job and proves server version, enqueue,
group consume, post-handler ack, exact group stats, verified TLS, rejected TLS,
ACL success/failure, restart persistence, pending crash recovery, paused-network
settlement ambiguity, reconnect, concurrent reclaim ownership, bounded handler
retry, handler panic, malformed/oversized delivery, dead-letter failure, and
graceful connection cleanup. The same suite passes with `-race` locally.

Redis Sentinel runs hermetically in the pinned Redis image. The master and
Sentinel use equal container and reserved host loopback ports, so the address
returned by Sentinel is routable from the test process. No integration scenario
is skipped by default.

Container stop/start closes live connections and proves same-endpoint outage
and restart behavior. It does not model every packet-drop, half-open TCP,
firewall, DNS, proxy, or load-balancer partition. The package makes no broader
partition-recovery guarantee: bounded operations return client/backend errors,
lossy transports may drop work, and RabbitMQ requires worker replacement.
Deployments must inject their actual network intermediary failure modes before
relying on a recovery objective.

The Valkey pause scenario specifically demonstrates that a timed-out `XACK`
may be applied after the server resumes. The safe outcome is either settled
work or a still-pending entry eligible for reclaim; the package does not claim
the client can distinguish those outcomes from the timeout alone.

```sh
go test -tags=integration -count=1 -timeout=15m ./...
```
