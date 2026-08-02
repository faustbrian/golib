# Goal: Temporal Rule Operators

## Objective

Build `rule-engine/adapters/gotemporal` as the optional bridge from exact
temporal instants and periods to deterministic interval-relation operators. It
MUST preserve boundary inclusion and UTC instant semantics without consulting
the system clock, guessing time zones, or performing calendar coercion.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Canonically encode UTC RFC3339-nanosecond instants and periods with explicit
  open/closed bounds.
- Supply equal, before, after, overlap, contains-period, and contains-instant
  operators with explicit signatures and immutable operator sets.
- Reject wrong kinds, malformed tags, invalid timestamps, impossible periods,
  unsupported bounds, trailing data, and cancellation.
- Preserve nanosecond precision and interval algebra at equal endpoints.
- Make leap-second and out-of-range policy explicit; never use `time.Now`,
  local timezone, locale, or ambient clock.
- Keep registration caller-owned and perform no I/O or hidden scheduling.

## Documentation And Completion

Document encoding, interval relations, boundaries, UTC policy, limitations,
API, examples, adoption, FAQ, compatibility, and migration. CI MUST enforce
race, fuzz, property tests, API, docs, benchmarks, exactly 100% statement
coverage, and exactly 100% of viable mutants killed by meaningful tests.
