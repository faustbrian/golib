# Golib Design Language

Golib is a collection of independently adoptable Go modules that share one
consumer contract. The ecosystem is intentionally not a framework: there is no
container, global application object, mandatory bootstrap, hidden registration,
or umbrella runtime dependency.

This document defines what consumers can expect across modules. Package-specific
documentation remains authoritative for domain behavior and may record a narrow,
reviewed exception when the domain requires a different shape.

## Design Priorities

Golib APIs prefer, in order:

1. explicit ownership and observable control flow;
2. standard-library types and interfaces;
3. bounded resource use and deterministic failure;
4. independently adoptable modules and optional adapters;
5. domain-appropriate APIs over superficial uniformity.

Learning one module should make another predictable. It should not make every
module look identical.

## Module Ownership

Each module owns one semantic concern. A core module must not import an optional
backend, transport, telemetry implementation, or service runtime merely to make
integration convenient. Optional integrations belong in independently
releasable adapter modules.

Interfaces are defined at the consuming boundary and remain as small as the
consumer's actual need. Constructors normally return concrete types. Standard
interfaces such as `io.Reader`, `io.Writer`, `fs.FS`, `http.Handler`,
`http.RoundTripper`, `error`, and `*slog.Logger` are not wrapped for branding.

An adapter path uses the form `pkg/<owner>/adapters/<target>`. The owner is the
contract being adapted and the target names the actual dependency or protocol,
for example `postgres`, `valkey`, `kafka`, `queue`, `outbox`, `otel`, or
`service`. Importing an adapter must not register globals, open connections, or
start background work.

## Construction

Use the smallest construction shape that expresses the contract:

| Shape | Appropriate use | Consumer guarantee |
| --- | --- | --- |
| Plain function | Stateless value operation | No hidden I/O or retained ownership |
| `New(Config)` | Required invariants or retained state | Complete validation before use |
| `New(Options)` | One coherent set of named settings | Same ownership and validation rules as config |
| Functional options | Orthogonal, optional, additive behavior | Deterministic duplicate and conflict handling |
| Builder plus `Compile` or `Build` | Mutable startup registration followed by immutable runtime use | Registration is single-owner; the result is immutable |
| `Open`, `Connect`, `Load`, or `Init` | Construction necessarily performs I/O or acquires resources | A context, finite operation, and explicit cleanup contract |
| `Must...` | Tests, generated constants, or deliberate startup panic | A non-panicking production API also exists |

Constructors copy caller-owned maps, slices, and byte buffers unless the API
explicitly transfers ownership. They do not start goroutines before all
configuration has been validated. A constructor that performs external I/O is
named as an I/O operation rather than hiding that work behind `New`.

Functional options are not a default style. They are used only when settings
are truly independent and optional. Builders are used only when registration is
meaningful and compilation produces a safer immutable runtime.

## Configuration And Defaults

Configuration is explicit and caller-owned. Ordinary libraries do not read
environment variables, search the filesystem, or inspect process globals.
Those behaviors belong to `config` and to application composition roots.

Every configuration type documents whether its zero value is useful, invalid,
or intentionally empty. Defaults are visible through a constructor,
`DefaultConfig`, or a named profile. An API distinguishes absent, zero, empty,
disabled, and defaulted values whenever those states behave differently.

Validation happens before background work or retained resource acquisition.
Diagnostics identify safe field paths or option names and never include secret
values. Invalid combinations fail deterministically rather than allowing the
last option to win accidentally.

## Context And Time

An operation that can block on I/O, admission, a callback, a worker, or a
long-running computation accepts `context.Context` as its first parameter. Pure
value operations do not accept a context solely for uniformity.

Request contexts are not retained after the operation returns. A module does
not detach work from caller cancellation unless the caller explicitly starts a
separately owned lifecycle. A deadline bounds the complete logical operation,
including admission, retries, hedges, and cleanup where those policies are
composed.

Timeouts do not claim to terminate arbitrary caller code that ignores
cancellation. Unknown external outcomes remain distinct from known rejection or
known failure.

`time.Time` and `time.Duration` remain the public values. Modules use explicit
clock seams when deterministic time, elapsed-time correctness, or testing
requires them. Wall-clock timestamps are not used as monotonic elapsed timers.

## Lifecycle

Lifecycle method names have the following ecosystem meanings:

- `Run(ctx)` performs one caller-bounded execution and returns its terminal
  result.
- `Start(ctx)` transfers ownership only after successful startup and has a
  documented stop or join operation.
- `Drain(ctx)` stops new intake and waits for accepted work without necessarily
  releasing all infrastructure.
- `Shutdown(ctx)` performs the complete ordered shutdown owned by the object. It
  is bounded, repeatable, and safe for concurrent calls.
- `Close()` performs immediate synchronous release compatible with
  `io.Closer`. Context-aware cleanup uses a separately named method.
- `Wait(ctx)` waits for an already owned operation and does not acquire hidden
  ownership.

Not every type exposes every method. A value type usually owns no lifecycle.
Every resource-owning API documents acquisition, ownership transfer, rollback
after partial startup, cleanup order, concurrent shutdown, and behavior after
terminal shutdown.

Goroutines, timers, tickers, channels, files, rows, transactions, response
bodies, connections, and buffers always have one visible owner and bounded
lifetime.

## Errors And Outcomes

Golib uses ordinary Go errors. It does not define a universal ecosystem error
base.

- `errors.Is` identifies stable categories.
- `errors.As` exposes structured, domain-specific detail.
- Wrapping preserves the original cause when disclosure is safe.
- Public classification never depends on matching error strings.
- Backend errors do not cross an abstraction boundary unless that adapter
  explicitly makes them part of its contract.
- Validation and configuration failures are deterministic and secret-safe.
- Partial and aggregate errors preserve item identity and relevant outcomes
  within documented bounds.

Where the domain supports the distinction, packages keep retryable, permanent,
local rejection, cancellation, deadline, conflict, unavailable, partial, and
unknown outcomes separate. A transient failure is not automatically safe to
retry. Retry safety remains an application or protocol decision.

## Concurrency And Callbacks

Public types state whether they are immutable, single-owner, or safe for
concurrent use. Packages expose finite queue, buffer, history, fan-out, and
cardinality limits rather than creating unbounded work.

Callbacks document whether they may block, re-enter the caller, run
concurrently, panic, retain arguments, or observe cancellation. Internal locks
are not held while invoking caller callbacks, performing network I/O, or doing
unbounded work.

Channels have one closure owner. Sending, receiving, and shutdown behavior are
part of the public contract when channels cross a package boundary.

## Security

Secure behavior is the default when a generally useful safe default exists.
Proxy trust, credential placement, debug disclosure, weak algorithms,
permissive fallback, and unbounded hostile input require an explicit opt-in.

Secrets and credentials do not appear in errors, logs, traces, metrics,
snapshots, fixtures, or generated artifacts. Payloads, tenant identifiers, and
high-cardinality values require an explicit observability policy. Parsers and
decoders bound input size, allocation, recursion, decompression, and integer
conversion before consuming untrusted values.

## Observability

Core modules expose bounded observations or narrow hooks. They do not require a
telemetry backend. Logging integrations accept `*slog.Logger`; tracing and
metrics integrations use OpenTelemetry directly or target the `telemetry`
module through an explicit adapter.

Applications create and own exporters, providers, flushing, and shutdown.
Importing a core package must not mutate global logger or OpenTelemetry state.
Attribute names and metric dimensions remain bounded and stable.

## Composition

Applications compose modules explicitly in a composition root:

1. load and validate configuration;
2. construct clocks, identifiers, loggers, and telemetry;
3. open external resources such as PostgreSQL, Valkey, Kafka, or OpenSearch;
4. compile domain registries and immutable plans;
5. construct adapters and application handlers;
6. register HTTP, RPC, queue, scheduler, or workflow entry points;
7. start caller-owned runtimes;
8. drain intake before shutting down workers and infrastructure;
9. flush observability last.

This is an ownership order, not a mandatory bootstrap API. Applications may use
only the modules they need.

Policy composition remains explicit. A typical outbound call uses one total
deadline and finite work budget, then applies admission, rate limiting,
bulkhead, breaker, retry or hedge, transport, and observation in a documented
order. The exact order depends on the operation; Golib does not silently apply a
default stack.

## Compatibility And Releases

Modules use independent semantic versions and directory-prefixed tags. A
module's compatibility promise covers its public API, documented behavior,
wire and persistence contracts, and specification decisions.

Deprecation is explicit and documented before removal. Source-breaking changes
require a migration path and a major version. Pre-v1 modules still avoid
gratuitous churn and record migration consequences because their first public
release is intended to be `v1.0.0`.

Known-good compatibility sets record module combinations tested together. They
are recommendations, not an umbrella dependency or lockstep release train.

## Testing Seams

Packages provide deterministic test seams only for behavior they own: fake
clocks, fixed identifiers, bounded in-memory stores, fixtures, and protocol
conformance helpers. Test helpers do not change process globals or weaken
production behavior.

The repository requires meaningful exact statement coverage, complete viable
mutation kills, race testing for concurrent code, fuzzing at hostile
boundaries, and equivalent benchmark scenarios. Those gates establish the
implementation quality; this design language establishes the consumer contract.

## Intentional Differences

A package may differ when its domain requires it. Examples include value
packages that need only pure constructors, parser packages that compile an
immutable document, and backend modules that must expose `Open` and cleanup.

Every exception must identify the affected module, the common rule, the reason
the domain cannot follow it, consumer consequences, and the condition for
reconsideration. "Designed by a different package author" is not a valid
exception.
