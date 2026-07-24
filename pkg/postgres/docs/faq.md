# Operational FAQ

## The pool is saturated

Check acquired versus maximum connections, empty-acquire count and wait time,
query latency, transaction age, and leaked `Acquire` calls. Shed load or bound
worker concurrency before increasing connections. Increasing every replica can
move saturation into PostgreSQL.

## Connections appear leaked

Every successful `Pool.Acquire` requires `Release`, normally with an immediate
defer. Every native transaction requires commit or rollback; prefer
`RunTransaction`. During shutdown, `ErrShutdownTimeout` means native close is
still waiting for borrowed connections.

## Failover causes bursts of errors

Expect connectivity or shutdown SQLSTATEs while DNS, routing, primaries, and
connections change. Use bounded exponential backoff at the operation boundary,
honor cancellation, and retry only idempotent or otherwise safe work. Do not
hide failover inside arbitrary transaction closures.

## Deadlocks occur

PostgreSQL aborts one participant with `40P01`. Retry only a safe complete
transaction and make lock ordering consistent. Keep transactions short and
inspect the server deadlock log.

## Serialization failures occur

`40001` is expected under serializable conflicts. Retry the entire transaction
from a fresh snapshot only when all enclosed effects are safe to repeat.

## Queries are slow

Name operations with a finite allow-list, inspect server execution plans and
wait events, and apply caller/statement deadlines. Never enable raw SQL or
argument telemetry as a shortcut; it creates data leakage and cardinality risk.

## Readiness fails but liveness succeeds

That is intentional: the process is alive but cannot currently prove database
readiness. Removing it from traffic is usually safer than restart loops.

## A pool hook or tracer panicked

Hooks and tracers are trusted application code. pgx may invoke them on the
caller or a pool-owned goroutine, so a panic can terminate the process. Return
an error for expected connection rejection, keep hooks bounded, and test custom
hook composition against a real pool. Transaction helpers attempt bounded
rollback if trusted tracing code panics during commit or savepoint release, but
the original panic still propagates.

## TLS-required startup fails

Confirm the server actually accepts TLS, the copied root pool trusts its
certificate, and `ServerName` matches its identity. `TLSRequire` intentionally
does not fall back to plaintext. Authentication and TLS failures are separate;
do not weaken certificate verification to diagnose a password or role problem.
