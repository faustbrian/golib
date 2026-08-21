# Changelog

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

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
- Harden integer parsing, arithmetic preflights, random range limits, and root
  search boundaries against overflow and runaway work.
- Harden rational construction, powers, decimal expansion, parsing, and
  rounding at exact resource boundaries.
- Reject decimal digit input as soon as its configured budget is exhausted and
  reject repeated separators without attacker-sized allocation.
