# Goal Harden: Event Sourcing OpenTelemetry Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Mission

Prove instrumentation cannot alter event processing, leak data, create
unbounded cardinality, or block lifecycle transitions.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Inventory each wrapper, method, instrument, attribute, context transition,
  error path, callback, and lifecycle owner.
- Test exact pass-through of values, errors, cancellation, ordering, and
  transaction semantics with no-op, failing, panicking, and shutdown providers.
- Capture telemetry and assert payloads, metadata, identities, arbitrary errors,
  panic values, SQL, headers, and credentials cannot leak.
- Fuzz propagation headers, metadata, names, positions, timestamps, and provider
  behavior under strict cardinality and byte limits.
- Race all wrappers, projection controls, process managers, propagation, and
  shutdown; detect duplicate span completion and retained contexts.
- Stress slow/exporter-backpressured SDKs and prove caller-configured bounds
  prevent telemetry from stalling event processing or Kubernetes drain.
- Test propagation collisions, invalid carrier data, sampling, linked spans,
  replay mode, and cross-version semantic conventions.
- Benchmark no-op, sampled-out, and recording paths for allocations and latency.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed meaningfully with no unresolved privacy, cardinality, behavior,
race, lifecycle, or interoperability finding.
