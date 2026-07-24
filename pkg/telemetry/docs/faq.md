# FAQ

## Does this replace OpenTelemetry?

No. It constructs and returns standard OpenTelemetry providers, tracers,
meters, propagators, exporters, samplers, and views.

## Does it support a specific vendor?

It supports vendors through a standard OTLP Collector pipeline. Vendor SDK
wrappers and proprietary direct-ingestion defaults are out of scope.

## Why is initialization explicit?

Configuration values should be safe to build in tests and dependency wiring.
Network clients, goroutines, SDK providers, and process globals appear only
after `Init` succeeds.

## Must I register globals?

No. Set `RegisterGlobal = false` and pass returned providers explicitly. Global
registration is convenient for compatible libraries but requires one process
owner.

## Why is baggage disabled?

Baggage crosses service boundaries and can leak or amplify untrusted data.
Enable it only for authenticated peers with allow-listed keys and hard bounds.

## Can I record raw errors or SQL in development?

The supplied adapters do not. Add application-owned diagnostic logging only
with deliberate redaction and access controls; do not change production
telemetry contracts for convenience.

## Why not retry forever?

An unavailable telemetry backend must not consume unbounded memory, network,
CPU, or shutdown time. Collector queues handle longer buffering with explicit
operational limits.

## Are logs supported?

Not as a stable signal. See [log stability](logs.md). Structured application
logging remains independent.

## How do I test without a Collector?

Use `testtelemetry` for deterministic providers. OTLP protocol tests already
run in-process; use `make integration` when changing transport behavior.

## Why exactly 100% statement coverage?

The gate ensures every production statement is exercised, but it is only one
part of quality. Race, fuzz, protocol, privacy, failure, vulnerability, and
benchmark gates cover properties statement execution cannot prove.
