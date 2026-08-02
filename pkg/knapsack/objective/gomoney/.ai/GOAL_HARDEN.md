# Goal Harden: Exact-Money Knapsack Objectives

## Mission

Prove exact, deterministic objective behavior across numeric extremes, hostile
configuration, concurrency, and solver search order.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Build amount/currency/scale/type/error matrices including zero, negatives,
  maximum magnitudes, missing types, and mixed currencies.
- Property-test exact total and ordering against direct `money` arithmetic for
  randomized packing candidates and map insertion order.
- Fuzz IDs, maps, money values, limits, and solver callbacks without panic,
  aliasing, overflow, nondeterminism, or unbounded work.
- Race shared immutable costs across many solver instances.
- Verify errors preserve causes and never expose mutable internal state.
- Compare identical inputs across architectures and repeated processes for
  byte-for-byte deterministic rankings and tie behavior.
- Benchmark lookup and comparison overhead against direct exact-money calls,
  excluding solver work.
- Audit dependencies so no float, rate lookup, locale, clock, or hidden I/O is
  introduced.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved exactness, determinism,
race, API, or performance finding.
