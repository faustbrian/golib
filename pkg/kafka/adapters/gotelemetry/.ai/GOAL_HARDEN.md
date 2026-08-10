# Goal Harden: Kafka OpenTelemetry Adapter

## Mission

Prove Kafka telemetry is bounded, truthful, failure-isolated, and payload-free
under high throughput and hostile providers.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Compare every observation mapping with the pinned OpenTelemetry messaging
  convention and document intentional adapter metrics.
- Capture all emitted data and assert no key, value, header, credential,
  endpoint, error text, panic value, or unallowlisted identity is present.
- Fuzz observations, allowlists, times, counts, sizes, identities, and hostile
  providers with strict bounds and no panics.
- Race observer calls, construction failures, SDK shutdown, sampling, and
  cancellation; verify no hidden goroutines or retained record state.
- Stress provider slowness and exporter backpressure under Kafka rebalance and
  shutdown deadlines; prove caller policy bounds impact.
- Test unknown future observations and convention-version upgrades fail
  deliberately instead of silently changing telemetry.
- Validate metric units, monotonicity, histogram values, span kind/status,
  parentage, and absence of false propagation claims.
- Prove injected W3C fields survive a pinned real Kafka broker and extract as
  the same remote span context after ordinary producer and consumer policy.
- Benchmark no-op, sampled-out, allowlist-hit, and recording paths.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved privacy, cardinality,
semantic, race, lifecycle, or performance finding.
