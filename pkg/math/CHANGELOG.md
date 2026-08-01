# Changelog

## Unreleased

- Add immutable arbitrary-precision integer, rational, decimal, and binary
  float families.
- Add shared limits, rounding, conditions, deterministic codecs, numeric laws,
  conformance vectors, fuzzing, mutation checks, and benchmarks.
- Add the MIT license for source distribution and dependency review.
- Use the canonical `math:` prefix for exported sentinel error messages after
  migration from the former standalone repository.
- Harden decimal exponent, scaling, normalization, and rounding boundaries so
  malformed limits and extreme intermediate values fail without runaway work.
- Reject malformed deterministic binary frames at exact payload, header, sign,
  length, and exponent boundaries.
