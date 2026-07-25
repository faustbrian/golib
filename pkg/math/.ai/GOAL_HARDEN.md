# Hardening Goal: Unified Numeric Foundation

## Objective

Prove `math` correct, immutable, deterministic, bounded, interoperable, and
safe across every numeric type, operation, conversion, context, encoding, and
hostile input.

## Required Audits

### Numeric Correctness

- Exhaust signs, zero, negative zero where representable, boundaries, huge
  magnitudes, coprime/non-coprime fractions, repeating decimals, and exact or
  inexact conversions.
- Prove algebraic laws where valid and document where finite precision breaks
  them.
- Differential-test integer, rational, and float operations against `math/big`.
- Run General Decimal Arithmetic vectors and compare equivalent `apd` behavior.
- Mutation-test every sign, comparison, quotient, remainder, rounding, trap,
  and condition branch.

### Aliasing And Concurrency

- Attempt mutation through every constructor, operand, accessor, encoder, SQL
  adapter, and derived value.
- Race shared values and contexts across all operations.
- Prove caches, if any survive review, are bounded, immutable, and do not alter
  results.

### Parsing And Encoding

- Fuzz malformed signs, bases, exponents, separators, Unicode, huge digits,
  leading zeros, trailing data, JSON numbers/strings, SQL values, and binary
  payloads.
- Prove canonical text and binary encodings round-trip exactly.
- Reject precision-losing JSON and float conversions unless explicitly
  requested and reported.

### Complexity And Resources

- Benchmark and cap huge powers, roots, division, normalization, decimal
  expansion, formatting, and conversion.
- Detect memory, goroutine, and retained-buffer leaks.
- Verify cancellation and limit errors remain distinguishable from arithmetic
  errors.

## Release Blockers

- Wrong numeric result, silent rounding, missed condition, overflow, aliasing,
  race, panic, unbounded work, biased randomness, or ambiguous serialization.
- A broad interface that permits invalid cross-type arithmetic assumptions.
- Missing meaningful 100% coverage, mutation evidence, or green CI.

## Completion Criteria

- Differential, vector, property, fuzz, race, mutation, leak, and benchmark
  suites pass.
- Every precision and rounding behavior is documented and executable.
- No high or medium correctness, security, or resource finding remains.
