# Goal: Exact-Money Knapsack Objectives

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `knapsack/objective/gomoney` as the optional exact-money objective
adapter for `knapsack`. It MUST let packing decisions compare configured
container costs without floating-point arithmetic, currency ambiguity, hidden
conversion, or I/O.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Accept immutable bounded mappings from container type IDs to exact
  `money.Money` values.
- Reject empty/duplicate/oversized IDs, invalid money, mixed currencies,
  unsupported scale, negative costs unless explicitly modeled, and excessive
  map size.
- Produce deterministic objective costs and comparisons independent of map
  iteration, locale, process, architecture, and solver order.
- Preserve exact amount/currency semantics and overflow/error identity through
  the knapsack objective contract.
- Defensively copy inputs; start no goroutines; perform no network, clock,
  environment, global registry, or exchange-rate access.
- Document missing-cost behavior and deterministic tie handling.

## Documentation And Completion

Document API, currency/scale policy, examples, adoption, solver composition,
limits, errors, FAQ, compatibility, and migration. CI MUST enforce race, fuzz,
property tests, API, docs, benchmarks, exactly 100% statement coverage, and
exactly 100% viable mutation kills.
