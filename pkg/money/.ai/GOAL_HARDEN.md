# Hardening Goal: Money Arithmetic

## Objective

Prove all monetary invariants, rounding policies, encodings, and integrations
correct under hostile values, concurrency, persistence, and migration.

## Required Audits

- Exhaust every ISO currency minor unit, historic code, unknown code, huge and
  negative amount, zero, context, and cash step.
- Property-test conservation across equal, weighted, positive, negative, and
  adversarial allocations.
- Verify tax-inclusive/exclusive and discount calculations across every
  rounding mode and operation order.
- Prove currency/context mismatch rejection and exact conversion-rate handling.
- Fuzz decimal input, rates, currency codes, JSON, SQL, locale formatting, and
  persisted versions.
- Differential-test equivalent behavior against mature money packages and
  independent hand calculations.
- Race shared immutable values and formatters; detect aliasing of numeric or
  currency internals.
- Bound allocation counts, digits, scale, output, and diagnostic amplification.
- Mutation-test every mismatch, rounding, remainder, sign, rate, and context
  decision.

## Release Blockers

- Lost or created monetary value, wrong currency, silent precision loss,
  nondeterministic remainder, float contamination, mutable alias, race,
  unbounded operation, or incompatible persistence.
- Missing meaningful 100% coverage or executable conservation evidence.

## Completion Criteria

- Currency, allocation, tax, discount, formatting, persistence, fuzz, race,
  mutation, and benchmark suites pass.
- Every supported monetary policy is documented and reproducible.

