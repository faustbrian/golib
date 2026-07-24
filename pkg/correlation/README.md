# correlation

`correlation` is the transport-neutral owner of correlation, request, and
causation identifiers. It carries those semantics through HTTP, JSON-RPC,
queues, scheduled work, webhooks, logs, and OpenTelemetry without creating a
global propagator or redefining trace and idempotency concepts.

## Semantics

- `CorrelationID` groups one logical interaction or workflow.
- `RequestID` identifies exactly one transport hop or delivery attempt.
- `CausationID` identifies that hop's immediate parent request or message.

These types are deliberately distinct. Trace and span IDs remain owned by
OpenTelemetry. Idempotency keys and fingerprints remain owned by
`idempotency`. None of these values is authentication, authorization,
tenancy, uniqueness, replay protection, or idempotency evidence.

## Quick start

```go
factory, err := correlation.NewFactory(correlation.FactoryOptions{})
if err != nil {
    return err
}

root, err := factory.Start()
if err != nil {
    return err
}
child, err := factory.Next(root)
```

The default factory uses the explicitly owned cryptographic UUIDv4 generator
from `identifier`. A caller-supplied generator remains instance scoped and
must return canonical text accepted by the configured policy.

Inbound metadata is never trusted by extraction alone:

```go
inbound, err := codec.Extract(carrier)
if err != nil {
    return err
}
values, err := factory.Accept(inbound, correlation.InboundPolicy{
    TrustCorrelation: true,
    TrustRequestAsCausation: true,
})
```

The application must establish that trust from an authenticated immediate
transport boundary first. Every accepted hop receives a new request ID.

## Adapters

- [`http`](http/) sanitizes inbound headers, applies explicit proxy trust,
  installs immutable context values, and injects outbound hops.
- [`http/requestidbridge`](http/requestidbridge/) explicitly adopts a trusted
  `http-middleware/requestid` value without importing its private key.
- [`jsonrpc`](jsonrpc/) reads and writes a separate metadata object without
  altering JSON-RPC envelopes.
- [`queue`](queue/) preserves workflow identity while generating a distinct
  request ID for every retry or redelivery.
- [`schedule`](schedule/) starts independent runs unless metadata is
  deliberately propagated.
- [`webhook`](webhook/) gives outbound and inbound webhook hops HTTP semantics.
- [`log`](log/) supplies redacted, keyed-hash, or explicitly raw `slog` attrs.
- [`telemetry`](telemetry/) attaches attributes to telemetry-owned links and
  exposes only fixed-cardinality presence flags to metrics.

W3C Trace Context and Baggage remain optional application-owned propagation.
They may be linked to these values, but correlation IDs never become trace or
span IDs.

## Deterministic correlation

`NewDeterministic` is an explicit opt-in for stable business workflows. It uses
HMAC-SHA-256, a versioned domain, length-delimited input, and bounded output.
Use a secret key when the input is private or comes from a small input space.
Deterministic correlation is linkable and is never the factory default.

## Security defaults

Identifiers use the canonical `[A-Za-z0-9_-]` alphabet and a default maximum
of 128 bytes. Carriers reject empty, oversized, malformed, control-bearing,
Unicode, and conflicting values. Injection refuses every populated target
field. Observability output is redacted unless disclosure is explicitly
enabled, and metrics never contain identifier values.

## Verification

Run the local release-equivalent gate:

```sh
make check-all
```

It verifies formatting, module tidiness, vet, unit and integration tests, the
race detector, 100% production statement coverage in every package, fuzz
smoke tests, mutation tests, allocation benchmarks, documentation, API
compatibility, linting, Staticcheck, vulnerability analysis, and NilAway.

See the [documentation index](docs/README.md), [security policy](SECURITY.md),
and [changelog](CHANGELOG.md).
