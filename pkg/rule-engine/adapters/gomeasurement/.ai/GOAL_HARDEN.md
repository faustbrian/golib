# Goal Harden: Exact Measurement Rule Operators

## Mission

Prove unit-aware comparisons remain exact, deterministic, and bounded across
conversion extremes, incompatible dimensions, and hostile persisted values.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Build dimension/unit/scale/sign/boundary matrices for every supported exact
  conversion and incompatible pair.
- Property-test operator results against direct `Quantity.Compare` with exact
  conversion, including transitivity and antisymmetry.
- Fuzz tags, unit symbols, Unicode, exponents, huge amounts, wrong kinds,
  malformed values, and cancellation under strict bounds.
- Race shared operators and prove returned slices and parsed quantities cannot
  mutate cross-call behavior.
- Verify persistence compatibility and canonical round trips for equivalent
  units and values.
- Assert no implicit default unit, rounding, float path, panic, global registry
  mutation, or hidden I/O.
- Benchmark parse/convert/evaluate against equivalent direct measurement work.
- Mutation-test every relation and dimension-compatibility branch.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved conversion, exactness,
fuzz, race, or compatibility finding.
