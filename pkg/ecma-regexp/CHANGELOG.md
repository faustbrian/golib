# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project intends to use semantic versioning after its first release.

## [Unreleased]

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- Closed ECMAScript 2025 tokenizer, parser, immutable AST, compiler, and
  bounded matcher.
- Captures, backreferences, lookaround, scoped modifiers, Annex B grammar,
  Unicode 16.0.0 properties, and Unicode Sets.
- UTF-16-exact matching, search, replacement, split, and stateful execution.
- JSON Schema Draft 2020-12 pattern profile.
- Differential Node.js and Deno vectors plus fuzz surfaces.
- Complete applicable pinned Test262 accounting with delegated matcher runs
  and structural generated-data verification.
- A canonical interoperability target that provisions and verifies the pinned
  Test262 corpus before running conformance and differential checks.
- Separate official Test262 conformance from independent-engine differential
  interoperability so both results remain attributable.

### Security

- Finite parse, compile, execution, output, and wall-time budgets.
- Synchronous context cancellation without hidden worker goroutines.
- Meaningful 100% production statement coverage and exact 100% mutation
  efficacy for every viable mutant.
