# Goal: Cross-Transport Correlation And Causation

## Objective

Build `correlation` as the transport-neutral package for correlation,
request, and causation identifiers across HTTP, JSON-RPC, queues, scheduled
work, webhooks, logs, and telemetry.

## Semantic Model

- `CorrelationID`: groups work belonging to one logical interaction or
  workflow across process boundaries.
- `RequestID`: identifies one transport request or delivery attempt.
- `CausationID`: identifies the immediate parent request, message, or event.
- Optional typed external identifiers with trust/source metadata.

OpenTelemetry trace/span IDs remain owned by telemetry. Idempotency keys and
fingerprints remain owned by `idempotency`. These values MAY be linked but
MUST NOT be treated as interchangeable.

## Generation And Trust

- Secure random generation by default through `identifier`.
- Strict length, alphabet, canonicalization, and inbound trust policies.
- Preserve trusted inbound correlation only when explicitly configured.
- Generate a new request ID for every hop/attempt while preserving correlation
  and setting causation where available.
- Deterministic hash strategies MAY derive correlation for explicitly stable
  business workflows, using versioned domain separation and keyed hashing when
  privacy requires it.
- Deterministic correlation MUST NOT be the default because it creates
  linkability and can disclose a small input space.

## Propagation

- Immutable context helpers with private collision-safe keys.
- Explicit carrier interface for inject/extract without global propagators.
- HTTP adapter and integration with `http-middleware/requestid`.
- JSON-RPC metadata adapter without changing protocol envelopes implicitly.
- Queue and webhook adapters preserving correlation and causation across
  retries/redelivery.
- `log` attributes and `telemetry` links with bounded cardinality and
  redaction policy.
- W3C Trace Context/Baggage integration remains optional and does not redefine
  trace semantics.

## Security And Bounds

- Reject control characters, oversized values, duplicate conflicting carriers,
  malformed encodings, and untrusted overwrite attempts.
- Never use correlation as authentication, authorization, tenancy, uniqueness,
  replay protection, or idempotency evidence.
- Do not emit raw business-derived deterministic IDs to metrics.
- No global current request, goroutine-local state, hidden middleware, or
  ambient mutable generator.

## Verification And Documentation

Require meaningful 100% production coverage, propagation matrices, trust and
precedence tests, multi-hop integration, queue retries, HTTP proxies, JSON-RPC,
webhooks, deterministic strategy vectors, privacy tests, fuzzing, race,
mutation, and allocation benchmarks.

Document semantics, generation, trust, propagation, correlation versus tracing
and idempotency, privacy, adapters, adoption, migration from Cline Correlation,
operations, FAQ, compatibility, and changelog. CI/local gates follow ecosystem
standards.

## Acceptance Criteria

- IDs retain distinct semantics across every transport and retry.
- Track can correlate HTTP ingestion, queue processing, and outbound webhooks.
- No identifier is trusted or derived implicitly.
- Meaningful 100% coverage and every blocking gate pass.
