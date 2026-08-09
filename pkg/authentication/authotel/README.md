# authotel

`authotel` is the optional OpenTelemetry adapter for
[`authentication`](https://pkg.go.dev/github.com/faustbrian/golib/pkg/authentication).
It turns completed authentication attempts into bounded traces and metrics. It
does not authenticate credentials, make authorization decisions, configure an
SDK, or own exporters.

## Quick start

```go
instrumenter, err := authotel.New(authotel.Config{
	TracerProvider: tracerProvider,
	MeterProvider:  meterProvider,
})
if err != nil {
	return err
}

authenticator, err := authentication.NewInstrumented(
	baseAuthenticator,
	instrumenter,
	clock,
)
```

Both providers are required and supplied explicitly. `authotel` never consults
OpenTelemetry global providers. Use the OpenTelemetry no-op providers to
disable emission while retaining the same wiring.

## API and ownership

- `Config` carries a caller-owned `trace.TracerProvider` and
  `metric.MeterProvider`.
- `New` creates the instruments and maps provider panics or instrument
  construction failures to fixed invalid-configuration errors without
  formatting provider error or panic values.
- `Instrumenter.Start` implements `authentication.Instrumenter` and returns the
  OpenTelemetry span context plus one completion callback.

The caller owns provider configuration, sampling, readers, exporters,
queueing, force-flush, and shutdown. Constructing or using this adapter starts
no goroutines. The adapter remains usable according to the supplied providers'
contract after their shutdown; it performs no independent shutdown work. A
provider that returns a nil context is treated as hostile and the caller's
original context is preserved.

## Telemetry convention

The instrumentation scope is
`github.com/faustbrian/golib/pkg/authentication/authotel`. The adapter telemetry
convention documented below is version `1.0.0`. That convention is not a
published OpenTelemetry schema and is therefore not written to
`InstrumentationScope.Version` or `SchemaURL`; those fields are reserved for
the instrumentation module version and a resolvable schema URL respectively.

| Signal | Name | Unit | Attributes |
| --- | --- | --- | --- |
| Span | `authentication.authenticate` | n/a | credential kind, outcome, failure kind |
| Counter | `authentication.attempts` | `{attempt}` | credential kind, outcome, failure kind |
| Histogram | `authentication.duration` | `s` | credential kind, outcome, failure kind |

The duration is recorded once at completion and negative values are clamped to
zero. Failed spans use OpenTelemetry error status with the fixed description
`authentication failed`. No application error is recorded.

### Closed attribute values

| Attribute | Values |
| --- | --- |
| `authentication.credential.kind` | `basic`, `bearer`, `api_key`, `unknown` |
| `authentication.outcome` | `authenticated`, `anonymous`, `failed`, `unknown` |
| `authentication.failure.kind` | `none`, `absent`, `invalid`, `rejected`, `unavailable`, `ambiguous`, `unknown` |

`none` is emitted for authenticated and anonymous outcomes. Missing or
unrecognized failure kinds on failed outcomes become `unknown`. Invalid or
future enum values never pass through as attributes, so the maximum attribute
combination space is the documented upper bound of 112.

Convention `1.x` preserves signal names, units, meanings, and existing
attribute values. Additive closed values require a minor convention version and
a changelog entry. Renames, removals, unit changes, or meaning changes require
a new major convention version and migration guidance. The module remains
pre-v1, so consumers should pin an exact module version independently of the
telemetry convention version.

## Privacy and security

The adapter accepts only `authentication.CredentialKind` and
`authentication.Event`; it never receives credential payloads, principals,
claims, issuers, subjects, endpoints, API keys, tokens, or arbitrary errors.
Unknown enum strings are normalized rather than recorded. Panic values from
providers or observers are recovered without being formatted into telemetry or
returned errors.

Do not add caller-controlled identity or protocol strings as attributes. If a
deployment needs such data for debugging, keep it outside this shared adapter
and apply its own redaction and cardinality policy.

## Completion, cancellation, and failure isolation

The completion callback is exactly-once: its first call records the event and
ends the span; duplicate calls emit nothing and return without waiting for the
winning completion's telemetry operations. `Instrumenter` and callbacks are
safe for concurrent use. The winning call releases the captured request context
and span before recording through stack-local references, so retaining a
completed callback does not retain request state. No lock is held across
provider or caller code.

A canceled context is preserved and completion is still attempted. This lets a
provider record the canceled attempt according to its own SDK and sampling
policy. The adapter does not retry, buffer, wait for exporters, or replace the
context.

Runtime observer panics are isolated per metric and span operation so one
failed observation does not suppress the remaining observations or escape the
completion callback. A panic while starting a span returns the original context
and a no-op completion callback. The core `authentication.Instrumented`
decorator provides an additional isolation boundary and always returns the
wrapped authenticator's original result and error.

OpenTelemetry recording APIs do not return exporter errors, so exporter health,
SDK queues, sampling decisions, and shutdown cannot become authentication
results through this adapter. Those calls are synchronous: a provider using a
synchronous or blocking span processor can delay completion. Applications MUST
use no-op, bounded batch, or otherwise non-blocking processors on request paths.
The adapter cannot contain an indefinitely blocking provider without starting
unbounded goroutines, which this contract forbids.

This is a supported-provider prerequisite: every provider and observer method
invoked by the adapter MUST return within the application's request-path bound.
An indefinitely blocking implementation is outside the adapter contract. The
hostile-provider fuzz matrix covers panics, nil contexts, invalid construction,
and observer failures; bounded batch-exporter backpressure is exercised with an
actually blocked exporter. Isolating an arbitrary implementation that never
returns would require an unbounded abandoned goroutine, contradicting the
adapter's no-goroutine and no-leak requirements.

## Hardening and allocation inventory

The production dependency surface is the core `authentication` contract and
the OpenTelemetry attribute, metric, and trace APIs. OpenTelemetry SDK packages
are used only by tests and benchmarks; applications select and own their SDK
configuration. Construction invokes the caller's meter and tracer providers
and creates exactly one counter and one histogram. Per attempt, the adapter
starts one span and returns one exactly-once callback. Completion performs two
span mutations, one counter addition, one histogram recording, and one span
end. Every provider or observer panic boundary is isolated independently and
all returned adapter errors use fixed text.

The adapter owns no goroutine, exporter, queue, cache, registry, or request
collection. Before completion, its callback state is a finite credential-kind
attribute, an atomic completion flag, request references, and the two metric
instruments needed to record the event. The winning completion clears the
request, span, and instrument references before invoking observers through
stack-local copies. Metric labels are limited to the documented 112
combinations. Exporter queues and retained exported spans or metrics belong to
the supplied SDK.

`BenchmarkAuthenticationInstrumentation` reports latency and allocations for
equivalent direct core instrumentation, OpenTelemetry no-op providers, an SDK
with tracing sampled out, and an enabled SDK. The figures are regression
evidence for the executing environment, not a cross-platform performance
promise. The module benchmark gate runs five 1,000-operation samples per path,
compares median latency with the direct-instrumentation sample from the same
process, and enforces platform-tolerant maximum ratios of 100x for no-op, 150x
for sampled-out, and 200x for enabled telemetry. It also caps those paths at
median values of 20, 22, and 24 allocations per operation respectively.

## Adoption and tradeoffs

Create one adapter from application-owned, bounded providers and reuse it
across instrumented authenticators. Prefer the no-op providers for deployments
that disable telemetry; they avoid SDK queues and exporter work while
preserving the explicit dependency graph. Prefer a bounded batch processor over
a synchronous exporter for request-path tracing.

The adapter deliberately omits credential identities, issuer and subject
dimensions, endpoints, error messages, events containing payloads, global
provider discovery, authorization decisions, and exporter lifecycle helpers.
This limits diagnostic detail in exchange for bounded cardinality and a small
secret-safe surface.

## Compatibility and migration

The public Go API is tracked by `api/baseline.txt`. Applications own the
providers, so changing exporters or SDK processors does not require an
`authotel` migration. When a telemetry convention major version changes,
migration guidance will identify query, dashboard, alert, and collector changes
in [CHANGELOG.md](CHANGELOG.md).

## FAQ

### Does `authotel` change authentication outcomes?

No. The authentication decorator preserves the wrapped result and error, and
telemetry failures are isolated.

### What happens when completion is called twice?

Only the first call emits observations and ends the span.

### Who shuts down the providers?

The application that supplied them. `authotel` owns no provider or exporter.

### Can I add a subject, issuer, key ID, route, or error message attribute?

Not through this adapter. Those values violate its privacy or finite-cardinality
contract.

### How do I disable telemetry?

Supply `trace/noop.NewTracerProvider()` and `metric/noop.NewMeterProvider()`.

## Development

From the repository root, run the affected module contract with:

```sh
make check MODULES=pkg/authentication/authotel
```

The module requires exact statement coverage and exact viable-mutant kills in
addition to formatting, analysis, race, fuzz, security, compatibility,
documentation, and benchmark gates.
