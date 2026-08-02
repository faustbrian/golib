# Goal Harden: Outbox OpenTelemetry Adapter

## Mission

Prove telemetry cannot leak envelope data, alter relay outcomes, amplify
retries, or delay Kubernetes shutdown without bounds.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Inventory wrappers, callbacks, spans, metrics, attributes, error paths, and
  provider lifecycle.
- Prove exact returned values/errors and one underlying publish call with
  no-op, failing, panicking, sampled, canceled, and shutdown providers.
- Capture all telemetry and assert envelope payload, metadata, identifiers,
  destinations, SQL, arbitrary errors, panic values, and credentials are absent.
- Fuzz observations, timing, outcomes, limits, and hostile providers.
- Race publication, callback completion, cancellation, and SDK shutdown; detect
  duplicate completion, goroutine leaks, and retained envelopes.
- Stress slow providers/exporters and prove caller-configured time budgets keep
  telemetry from extending relay leases or termination unboundedly.
- Verify convention upgrades and new outcomes require deliberate mappings.
- Benchmark no-op, sampled-out, and recording overhead.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved privacy, behavior, race,
lifecycle, cardinality, or performance finding.
