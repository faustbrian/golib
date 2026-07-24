# Troubleshooting

## Unexpected misses

Check key-space namespace, name, and version; codec version; native endpoint
and database routing; TTL; and whether another code path invalidated the
key. A stale deadline at or before the injected clock is a hard miss and is
cleaned up.

## Schema mismatch after deploy

Confirm all readers use the intended codec version. For incompatible changes,
bump both codec and key-space versions. Do not overwrite the error as a miss;
track it and let old keys expire.

## Loader runs more than once

Coalescing is per process and encoded key. Verify callers use the same key space
and deterministic encoder. Multiple service replicas each run their own flight.

## A deleted value reappears

Same-instance deletion wins over an active load. If another process performed
the delete, there is no distributed generation fence. Include a source version
in the key/value or coordinate invalidation at the application boundary.

## Backend returns after an outage

Native clients reconnect at a stable standalone endpoint, but retries and
timeouts remain client configuration. This release does not prove DNS changes,
failover promotion, cluster redirects, or replica convergence.

## `ErrWaiterLimit`

A hot key exceeded `MaxWaitersPerKey`. Reduce source latency, shed callers,
serve explicitly permitted stale data, or raise the bound only after measuring
memory and latency impact.

## Slow shutdown

Loaders must select on their supplied context and native clients need bounded
timeouts. `Close` waits for loader cleanup so it cannot safely abandon a
goroutine that may still write shared state.

## Integration tests do not start

Ensure Docker is running and the current user can access it. Override images
with `CACHE_REDIS_IMAGE` or `CACHE_VALKEY_IMAGE`. CI-proven versions are listed
in [the backend guide](backends.md).

## Coverage below 100%

Run `make coverage` to identify the package, then use a profile directly:

```sh
go test -coverprofile=coverage.out .
go tool cover -func=coverage.out
go tool cover -html=coverage.out
```

Add a semantic assertion for the uncovered behavior; do not add line-execution
tests without a contract.
