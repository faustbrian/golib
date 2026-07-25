# Pool construction and lifecycle

## Connection strings

`ParseConfig` accepts the same PostgreSQL URL and keyword/value connection
strings as pgx. Regression coverage includes percent-encoded credentials,
quoted keyword values, IPv6, Unix sockets, and multi-host fallback lists. The
package never returns the input DSN in its own validation errors. Treat the
native parsed configuration and `Database.DSN()` test helper output as secrets.

## Sizing

Start from the PostgreSQL connection budget, subtract administration,
migration, replication, and failover headroom, then divide the remainder among
maximum concurrently deployed application replicas. `MaxConns=10` is a finite
library default, not a universal recommendation. `MinIdleConns` reduces cold
acquisition latency but creates connections proactively.

## Timeouts

`ConnectTimeout` bounds each network connection. `AcquireTimeout` bounds queue
wait plus any connection establishment performed for acquisition. Request
deadlines earlier than these values win. `PingTimeout` applies to startup and
readiness. Query deadlines remain caller-owned and must be applied to every
request or job context.

## Startup

`StartupPing` is the default and proves DNS, transport, TLS, authentication,
server acceptance, session initialization, and one pool acquisition before the
application announces startup. `StartupLazy` defers all of that and should be
reserved for systems whose orchestrator or worker loop owns retry behavior.
Unavailable endpoints, wrong-protocol listeners, rejected authentication, and
strict-TLS mismatches have bounded secret-safe startup regressions.

DNS lookup and caching remain native Go/pgx behavior and occur when pgx opens a
connection; this package keeps no resolver cache. Stop/restart recovery is
proven on a stable endpoint. Deployments that change DNS answers must test their
resolver TTL, network, and failover policy explicitly.

## Session initialization

`SessionInit` runs once for every newly established connection, after a native
`AfterConnect` hook. A native hook error skips session initialization. Any
session initialization error rejects the connection and is preserved through
`errors.Is` and `errors.As`. Keep the hook idempotent, bounded by its context,
and free of process-external side effects.

## Native hook lifecycle

`Config.Configure` exposes the native `pgxpool.Config` after typed defaults are
applied. Hook ownership and failure behavior remain explicit:

| Hook | Failure behavior |
| --- | --- |
| `Configure` | returned errors become secret-safe `ConfigError` values with the original cause |
| `BeforeConnect` / `AfterConnect` | errors reject connection creation and remain inspectable from fail-fast startup |
| `PrepareConn` | an error fails the acquire; false with nil destroys the connection and lets pgx retry |
| `AfterRelease` | false destroys the released connection instead of returning it to the pool |
| `BeforeClose` / `ShouldPing` / tracers | native pgx behavior is preserved |

Hook panics are trusted-code failures and are deliberately not converted into
database errors. Depending on the pgx execution path, a panic can terminate the
calling or a pool-owned goroutine and therefore the process. Hooks must return
errors for expected rejection. Prefer `PrepareConn`; pgx deprecates
`BeforeAcquire`. Subprocess integration tests exercise panic propagation through
all four connection-lifecycle hooks without allowing an expected process crash
to terminate the surrounding suite.

## Saturation

When the configured acquisition deadline expires, the error matches
`ErrAcquireTimeout` and `context.DeadlineExceeded`. It also matches
`ErrPoolExhausted`, and classifies as `ErrorPoolExhaustion`, only when the
contemporaneous pool snapshot shows every slot acquired or constructing. An
earlier caller deadline or cancellation retains its context classification.
Inspect `Stats.AcquiredConns`, `IdleConns`, `EmptyAcquireCount`, and
`EmptyAcquireWaitTime`. Do not increase pool size before ruling out leaked
connections, long transactions, slow queries, and excessive concurrency.
Canceled waiters are removed and do not prevent later acquisition. The package
inherits pgxpool scheduling and does not promise waiter ordering or fairness.

## Shutdown

`Close` starts native shutdown exactly once. pgxpool must wait for borrowed
connections, but the wrapper stops waiting at the caller or configured
deadline and returns `ErrShutdownTimeout`. Native shutdown continues in one
background goroutine. Stop accepting work, cancel workers, wait for handlers,
then close the pool. Returning borrowed connections is mandatory.
