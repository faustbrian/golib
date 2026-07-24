# Goal: Exact Money And Currency Arithmetic

## Objective

Build `money` as a production-grade immutable monetary value package using
`math` for exact decimal/rational arithmetic and `international/currency`
for currency identity and ISO metadata.

## Core Scope

- Immutable `Money`, `RationalMoney`, `MoneyBag`, amount, rate, tax, discount,
  allocation, and result types.
- Currency equality, mismatch protection, historic currency support, and
  explicit minor-unit metadata.
- Construction from major units, minor units, exact decimal text, database
  values, and validated wire representations without float conversion.
- Add, subtract, multiply, divide, compare, absolute, negate, ratios, weighted
  allocation, equal split, tax inclusion/exclusion, discounts, and cash
  rounding.
- Explicit contexts for default minor units, cash steps, custom scales, and
  automatic precision where safe.
- Deterministic remainder distribution preserving the original total.
- Locale formatting through optional `international` integration without
  mixing display with monetary identity.
- Currency conversion only with an injected exact rate, timestamp/source
  metadata where required, and explicit rounding. No built-in live FX service.

## Invariants

- Arithmetic across different currencies fails explicitly.
- Context and scale differences never silently normalize away value.
- No operation accepts or produces `float32`/`float64` by default.
- Allocation and tax calculations preserve totals according to documented
  rounding policy.
- Currency identity remains owned by `international`; money arithmetic never
  edits currency metadata.
- Values are immutable, comparable, serializable, and alias-safe.

## Package Shape

- Root: money values, contexts, arithmetic, errors, allocation, tax, discount.
- `format`: locale and exact formatters.
- `encoding`: JSON, text, SQL, PostgreSQL numeric, and versioned persistence.
- `moneytest`: laws, fixtures, and assertions.

## Security And Bounds

- Bound digits, scales, rates, allocation parts, ratios, output, and diagnostics.
- Reject unknown currencies, impossible contexts, zero/negative allocation
  ratios, excessive rates, and precision-losing input.
- Never log monetary source records or customer identifiers by default.

## Verification And Documentation

Require meaningful 100% production coverage, property tests for conservation,
official currency fixtures, historic/zero/three-minor-unit currencies, tax and
cash-rounding matrices, fuzzing, race tests, mutation tests, SQL/JSON round
trips, and correctness-gated benchmarks against maintained money packages.

Document complete API, contexts, rounding, tax, discounts, allocations,
formatting, persistence, migration from PHP money libraries, security,
performance, cookbook, FAQ, compatibility, and changelog. All local and CI
quality gates must match ecosystem standards.

## Acceptance Criteria

- Every monetary operation is exact until an explicit rounding boundary.
- Currency and context mismatches cannot pass silently.
- Allocation, tax, discount, and conversion invariants are executable.
- No competing decimal implementation exists outside `math`.
- Meaningful 100% coverage and every blocking gate pass.
