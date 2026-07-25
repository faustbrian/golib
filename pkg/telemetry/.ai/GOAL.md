# Goal: Vendor-Neutral OpenTelemetry Runtime

## Objective

Build a production-grade open-source package that standardizes OpenTelemetry
provider lifecycle, OTLP export, propagation, sampling, and instrumentation for
Go services without hiding the OpenTelemetry API or coupling applications to a
telemetry vendor.

## Scope

- Configure traces and metrics using stable OpenTelemetry APIs.
- Support OTLP over gRPC and HTTP with explicit endpoints, TLS, headers,
  compression, batching, retry, and timeout settings.
- Own resource construction, service identity, environment attributes,
  propagators, sampling, provider registration, flush, and shutdown.
- Provide optional instrumentation bridges for `net/http`, `http-client`,
  `postgres`, `cache`, and `queue` without dependency cycles.
- Treat OpenTelemetry logs as isolated and experimental until their Go API and
  SDK are stable enough for the package compatibility promise.
- Target OpenTelemetry Collector deployment. Direct vendor SDK wrappers are
  out of scope unless required for functionality unavailable through OTLP.

## Public API Principles

- Return standard OpenTelemetry tracer and meter providers and APIs.
- Initialization MUST be explicit and side-effect free until called.
- Shutdown MUST be idempotent, context-bounded, and aggregate all failures.
- Defaults MUST be safe for Kubernetes but fully inspectable and overridable.
- Instrumentation MUST prevent high-cardinality labels by design.

## Package Shape

- Root package: configuration, runtime lifecycle, resources, and errors.
- `otlp`: gRPC and HTTP exporter construction.
- `trace`: sampling and trace configuration.
- `metric`: readers, views, boundaries, and cardinality controls.
- `propagation`: trusted inbound and outbound propagation policies.
- `instrumentation/*`: optional integration packages.
- `testtelemetry`: deterministic in-memory providers and assertions.

## Quality Requirements

- Meaningful 100% statement coverage is required, with behavior-focused tests.
- Race tests MUST cover initialization, registration, export, flush, shutdown,
  and concurrent instrumentation.
- Fuzz tests MUST cover resource attributes, propagation headers, configuration,
  and untrusted metadata.
- Benchmarks MUST track instrumentation overhead, allocations, and exporter
  batching behavior.

## Documentation Deliverables

- Complete API documentation and runnable service/worker examples.
- Adoption guides for Kubernetes, Collector deployment, local development,
  traces, metrics, propagation, sampling, and graceful shutdown.
- Cardinality, privacy, performance, troubleshooting, compatibility, upgrade,
  architecture, security, FAQ, and contribution documentation.
- A maintained `CHANGELOG.md` covering every user-visible change.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, unit and integration tests,
race tests, fuzz smoke tests, coverage enforcement, vulnerability scanning,
examples, compatibility checks, and supported OpenTelemetry version matrices.

## Phases

1. Specify lifecycle, configuration, compatibility, and stability boundaries.
2. Implement resources, providers, OTLP transports, propagation, and shutdown.
3. Add metrics views, cardinality controls, integration bridges, and test tools.
4. Complete failure injection, race, fuzz, benchmark, and Collector testing.
5. Publish complete adoption and operations documentation.

## Acceptance Criteria

- Services can initialize and stop telemetry through one explicit lifecycle.
- Export, propagation, sampling, cardinality, and failure behavior are tested.
- Applications retain standard OpenTelemetry APIs and vendor portability.
- Meaningful 100% coverage and all GitHub Actions gates pass.
- Documentation is sufficient for safe production adoption and `CHANGELOG.md`
  is current.
