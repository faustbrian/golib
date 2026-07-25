# Backend guide

## Memory

The memory backend is a concurrency-safe LRU bounded by both entry count and
retained bytes. Size includes backend key bytes and payload bytes. It evicts the
least recently used records until both bounds are satisfied and reports
eviction/expiration events when configured with an observer.

Expiration is lazy: reads and conditional mutations remove expired entries.
There is no janitor goroutine or timer. `MaxBytes` excludes Go runtime, map, and
list overhead, so production limits need headroom.

```go
backend, err := memory.New(memory.Config{
	MaxEntries: 50_000,
	MaxBytes:   128 << 20,
	Clock:      cache.SystemClock{},
})
```

It is process-local and makes no durability or cross-process consistency claim.

## Redis with go-redis/v9

Construct and own the native client in the application, including TLS,
authentication, pooling, retries, and timeouts.

```go
client := redis.NewClient(&redis.Options{Addr: "redis:6379"})
backend, err := redisbackend.New(redisbackend.Config{
	Client: client, Clock: cache.SystemClock{}, MaxRecordSize: 1 << 20,
})
```

The adapter bounds reads server-side before retrieving bytes, stores one
versioned record envelope, applies `NX`/`XX` atomically, and sets server expiry
to a relative duration ending at the record's stale deadline. Server expiry has
millisecond precision and positive sub-millisecond durations use 1 ms.
Portable records omit Go's process-local monotonic clock reading, matching the
Redis wire representation.

## Valkey with valkey-go

Construct the native client with the desired authentication, TLS, routing, and
pool options.

```go
client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{"valkey:6379"}})
backend, err := valkeybackend.New(valkeybackend.Config{
	Client: client, Clock: cache.SystemClock{}, MaxRecordSize: 1 << 20,
})
```

The Valkey adapter uses valkey-go's command builder and binary-safe values. Its
wire and conditional semantics match the Redis adapter, including the relative
hard deadline and 1 ms minimum.

## Ownership and shutdown

The application owns native Redis and Valkey clients and closes them after the
semantic cache. `Cache.Close` stops cache loaders; it does not close a supplied
backend or network client. The memory backend has its own `Close` method.

## Supported integration matrix

CI runs the shared contract and adapter failure tests against Redis 7.2, 7.4,
and 8.0, plus Valkey 9.0. Each matrix job covers standalone behavior,
authentication, certificate-verified TLS, bounded reads, expiry, atomic
conditions, outage errors, and client recovery. Docker is required locally:

```sh
make integration
CACHE_REDIS_IMAGE=redis:7.4 make integration-redis
CACHE_VALKEY_IMAGE=valkey/valkey:9.0 make integration-valkey
```

Standalone is the only release-supported network topology. Supplying a native
cluster, Sentinel, failover, or replica client is possible at the type level but
is not a support claim. Redirects, replica staleness, script routing, and
cross-node TLS remain unverified. Bulk cache methods are sequential one-key
operations and do not use pipelines or transactions.
