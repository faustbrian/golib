# Goal Harden: Exact Decimal Rule Operators

## Mission

Prove decimal rule evaluation is exact, deterministic, bounded, and safe for
hostile persisted values and concurrent engines.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Test all relation truth tables over signs, zero forms, scales, extremes, and
  mathematically equal alternate encodings.
- Property-test every operator against direct decimal comparison.
- Fuzz tagged values, Unicode, huge magnitudes/scales, wrong kinds, truncation,
  operator slices, and canceled contexts under strict bounds.
- Race shared operator values across engines and prove returned slices cannot
  mutate another caller's behavior.
- Verify canonical persistence and compatibility across versions.
- Assert no float conversion, panic, global mutation, hidden I/O, or
  caller-controlled diagnostic leakage.
- Benchmark parsing and evaluation against direct decimal comparison using
  equivalent canonical inputs.
- Mutation-test boundary comparisons so replacing `<`, `<=`, `>`, `>=`,
  or equality is always detected.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved exactness, determinism,
fuzz, race, or compatibility finding.
