# Hardening Goal: ECMA-262 Regular Expressions

## Objective

Prove grammar, Unicode, matching, captures, backtracking, replacement, limits,
and interoperability correct and bounded across every supported edition.

## Required Audits

- Reconcile every Test262 RegExp case, skip, proposal-stage feature, erratum,
  and edition difference.
- Exhaust escapes, classes, sets, properties, surrogates, astral characters,
  case folding, anchors, boundaries, captures, backreferences, lookarounds,
  quantifiers, flags, and indices.
- Differential-test supported behavior against at least two JavaScript engines.
- Attack exponential/catastrophic patterns, zero-width loops, huge captures,
  replacements, nested assertions, Unicode sets, and malformed UTF-8.
- Prove step, memory, stack, time, output, and cancellation budgets on every
  execution path.
- Race immutable programs and caller-owned caches; detect goroutine/buffer
  leaks.
- Fuzz all stages and mutation-test every grammar and matcher decision.

## Release Blockers

- Spec divergence, wrong index/capture, Unicode mismatch, runaway execution,
  budget bypass, panic, race, unreported skip, or misleading compatibility
  claim.

## Completion Criteria

- Test262, JavaScript differential, JSON Schema, hostile, fuzz, race, mutation,
  leak, and benchmark suites pass with meaningful 100% coverage.

