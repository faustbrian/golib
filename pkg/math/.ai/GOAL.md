# Goal: Unified Arbitrary-Precision Numeric Foundation

## Objective

Build `math` as the ecosystem's coherent arbitrary-precision numeric
foundation. It MUST provide immutable integer, rational, decimal, and
floating-point value APIs backed primarily by Go's `math/big`, with consistent
construction, errors, comparison, rounding, serialization, limits, testing,
and documentation.

The package exists to prevent `money`, `measurement`, rule evaluation,
and other consumers from inventing competing decimal or big-number types. It
MUST NOT replace ordinary `int`, `uint`, or `float64` where machine arithmetic
is correct and bounded.

## Type Model

- `Integer`: arbitrary-precision signed integer backed by `big.Int`.
- `Rational`: exact fraction backed by `big.Rat`, preserving exact arithmetic.
- `Decimal`: arbitrary-precision base-10 coefficient and exponent/scale with
  explicit context, precision, rounding, and condition reporting.
- `Float`: explicitly inexact arbitrary-precision binary floating point backed
  by `big.Float` for algorithms that require it.

These types share naming, errors, conversion policy, serialization policy, and
resource limits, but MUST NOT be forced behind one broad arithmetic interface.
Their closure, precision, division, ordering, exceptional values, and rounding
semantics differ materially. A unified product API is not permission to erase
those distinctions.

## Immutability And Ownership

- Public values MUST behave immutably even though `math/big` values mutate.
- Constructors MUST defensively copy caller-owned big values and byte slices.
- Operations MUST return new values and never mutate operands.
- Accessors MUST not expose internal mutable aliases.
- Zero values MUST be safe and canonical where practical.
- Equality and ordering MUST be numeric, while representation equality MAY be
  exposed separately for decimal scale-sensitive use cases.

## Arithmetic

- Exact add, subtract, multiply, negate, absolute value, sign, compare, min,
  max, clamp, power, quotient, remainder, Euclidean modulo, GCD, LCM, and
  integer roots where mathematically defined.
- Rational normalization, numerator/denominator access, exact conversion, and
  bounded decimal expansion.
- Decimal contexts with precision, exponent range, rounding mode, traps, and
  conditions such as rounded, inexact, overflow, underflow, and division by
  zero.
- Float precision and rounding MUST be explicit at construction and operation
  boundaries.
- Random integer generation MUST accept an injected cryptographic or
  deterministic source and reject biased sampling.
- No operation may silently narrow, overflow, round, or convert through
  `float64`.

## Parsing, Formatting, And Serialization

- Strict decimal and integer grammars with explicit bases, signs, exponent
  policy, underscores, whitespace, leading zeros, and maximum sizes.
- Canonical text forms and exact round trips.
- JSON support MUST avoid precision loss; arbitrary values default to strings
  or explicit codecs rather than unsafe JSON numbers.
- SQL text/numeric scanning and values MAY be optional adapters.
- Binary encoding MUST be versioned and deterministic if provided.
- Locale-aware display belongs to higher-level formatting packages, not core
  numeric identity.

## Package Shape

- Root: shared errors, conditions, rounding modes, limits, and conversion
  contracts.
- `integer`: immutable arbitrary-precision integers.
- `rational`: immutable exact fractions.
- `decimal`: immutable base-10 values and arithmetic contexts.
- `bigfloat`: explicitly inexact arbitrary-precision binary floats.
- `encoding`: optional JSON, text, SQL, and binary adapters.
- `mathtest`: reusable numeric laws, vectors, and assertions.

Avoid import cycles and keep consumers able to import only the numeric type
they need.

## Security And Resource Bounds

- Bound input digits, exponent magnitude, precision, output digits, power
  exponents, random ranges, intermediate allocation, and diagnostic size.
- Reject algorithmic-complexity attacks involving huge powers, roots, GCDs,
  decimal expansions, or crafted divisors before unbounded work.
- Support context cancellation for operations whose work can become material.
- No unsafe, cgo, mutable globals, hidden caches, or ambient randomness.

## Verification

Meaningful 100% production statement coverage is mandatory. Add:

- algebraic property tests for every exact operation;
- differential tests against `math/big` and independent decimal engines;
- official General Decimal Arithmetic vectors where applicable;
- parse/format/serialization round-trip properties;
- aliasing and immutability tests;
- fuzzing of parsers, conversions, contexts, arithmetic, and encodings;
- race tests for shared values and contexts;
- mutation tests for signs, comparisons, rounding, conditions, and limits;
- allocation and scaling benchmarks against direct `math/big`, `apd`, and
  maintained decimal libraries with equivalent semantics.

## Documentation And Automation

Provide complete API, numeric-model, precision, rounding, conditions,
conversion, serialization, security, performance, migration, cookbook, FAQ,
troubleshooting, compatibility, and changelog documentation. Clearly explain
when ordinary Go numeric types are preferable.

CI and local commands MUST run formatting, vet, strict lint, advisory NilAway,
tests, exact meaningful coverage, race, fuzz smoke, mutation, vulnerability,
API compatibility, examples, docs, and benchmarks on the latest stable Go
release used as the minimum.

## Acceptance Criteria

- All four numeric families have explicit, non-competing semantics.
- Values are immutable and alias-safe over mutable `math/big` internals.
- Decimal arithmetic reports every rounding and exceptional condition.
- No conversion silently loses precision.
- `money` and `measurement` can depend on this package without defining
  another numeric foundation.
- Meaningful 100% coverage and every blocking gate pass.
