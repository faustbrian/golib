# Redis Pub/Sub setup

Use package `redisdb`. Configure `WithAddr`, `WithUsername`, `WithPassword`,
`WithDB`, `WithChannel`, and optional TLS. Cluster and Sentinel modes are
explicit options. `WithConnectTimeout` bounds startup validation and
`WithRequestTimeout` bounds an idle request.

Pub/Sub is non-durable: messages published while a consumer is disconnected are
lost, there is no ack, and depth/job age are unavailable. Use it only when those
semantics are acceptable. For durable work use [Redis Streams](redis-streams.md).
Constructor success does guarantee that Redis acknowledged the subscription;
this only closes the healthy-start race and does not make reconnects durable.

Integration evidence uses Redis 6.2.22 with `go-redis/v9` 9.19.0. Standalone and
single-node all-slot Cluster run in pinned containers. Sentinel runs
hermetically with equal container/host ports so discovered master addresses are
routable. Restart evidence proves the client resubscribes, but outage messages
remain lost by protocol design.

TLS is opt-in. `WithSkipTLSVerify` is intentionally conspicuous and MUST NOT be
used in production. Debug mode and constructor error text redact credentials
and connection strings.
