# Goal: Exact Measurement Rule Operators

## Objective

Build `rule-engine/adapters/gomeasurement` as the optional bridge from exact
measurement quantities to deterministic rule operators. It MUST compare only
dimensionally compatible quantities through explicit exact conversion and MUST
NOT invent implicit units or lossy coercions.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Encode quantities as canonical, versionable tagged values containing exact
  amount and unambiguous unit identity.
- Supply equality and ordered operators with explicit signatures and fresh
  immutable operator sets.
- Reject wrong kinds, malformed/noncanonical values, unknown units,
  incompatible dimensions, unrepresentable conversion, extremes, and canceled
  contexts.
- Use the measurement package's exact conversion policy; never silently round.
- Keep registration caller-owned and start no goroutines or hidden I/O.
- Preserve stable causes while distinguishing invalid input from incompatible
  quantity errors.

## Documentation And Completion

Document encoding, conversion, dimensions, operators, limits, API, examples,
adoption, FAQ, compatibility, and migration. CI MUST enforce race, fuzz,
property tests, API, docs, benchmarks, exactly 100% statement coverage, and
exactly 100% of viable mutants killed by meaningful tests.
