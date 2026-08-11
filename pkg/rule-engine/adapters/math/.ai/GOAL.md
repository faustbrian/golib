# Goal: Exact Decimal Rule Operators

## Objective

Build `rule-engine/adapters/gomath` as the optional bridge from exact
`math/decimal` values to deterministic rule-engine comparison operators. It
MUST preserve decimal semantics without float coercion, expression evaluation,
global registration, or hidden I/O.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Encode decimals as canonical, versionable tagged rule values.
- Supply equality and ordered comparison operators with explicit signatures and
  fresh immutable operator sets.
- Reject wrong value kinds, malformed tags, invalid/noncanonical decimals,
  unsupported scale/magnitude, and cancellation.
- Preserve exact sign, scale normalization, comparison, and error identity.
- Make operator names collision-resistant and document registration ownership.
- Start no goroutines and use no float, clock, environment, network, or global
  mutable registry.

## Documentation And Completion

Document encoding, operators, exactness, limits, composition, API, examples,
adoption, FAQ, compatibility, and migration. CI MUST enforce race, fuzz,
property tests, API, docs, benchmarks, exactly 100% statement coverage, and
exactly 100% of viable mutants killed by meaningful tests.
