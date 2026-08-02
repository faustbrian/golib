# Goal Harden: Temporal Rule Operators

## Mission

Prove interval relations are exact and deterministic at every boundary,
timestamp extreme, persisted-input failure, and concurrent use.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Exhaust all Allen-style interval relations across open/closed endpoint
  combinations, equality, adjacency, zero duration, and nanosecond differences.
- Property-test adapter operators against direct temporal package relations and
  membership checks.
- Fuzz tags, timestamps, offsets, fractional seconds, bounds, separators,
  Unicode, extremes, wrong kinds, and cancellation under strict limits.
- Verify equivalent offset timestamps canonicalize to identical UTC values and
  local timezone/environment changes cannot affect results.
- Race shared operators and inspect returned slices for mutation isolation.
- Test persisted compatibility across supported versions and explicit rejection
  of unknown encodings.
- Assert no ambient clock, timezone lookup, panic, hidden I/O, or silent
  precision loss.
- Benchmark parse/evaluate against equivalent direct temporal work.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved interval, precision,
fuzz, race, or compatibility finding.
