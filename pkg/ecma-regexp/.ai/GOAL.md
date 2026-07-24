# Goal: ECMA-262 Regular Expression Engine

## Objective

Build `ecma-regexp` as a serious, specification-backed ECMA-262 regular
expression parser and matcher for JSON Schema and other standards requiring
JavaScript-compatible regex semantics. It MUST NOT pretend Go's RE2-based
`regexp` package is behaviorally equivalent.

## Specification Scope

- Pin exact supported ECMAScript editions and Test262 provenance.
- Implement the complete RegExp grammar, pattern semantics, character classes,
  escapes, assertions, captures, backreferences, lookahead, lookbehind,
  Unicode modes, properties, sets, flags, and replacement behavior claimed.
- Make edition and feature support explicit; reject unknown future syntax.
- Provide a documented JSON Schema profile matching the specification's regex
  expectations and portability recommendations.
- Maintain a normative requirement and ambiguity/errata decision register.

## Architecture

- Lexer/tokenizer with source spans and bounded diagnostics.
- Version-aware parser producing immutable typed AST.
- Compiler producing an immutable executable program.
- Matcher with explicit input, start position, flags, capture results, and
  cancellation/resource budget.
- Deterministic parse/format round trips where a canonical form is promised.
- No embedded JavaScript runtime requirement for core use.
- Optional adapters for `json-schema`, `rule-engine`, and
  `validation`.

## Safety

ECMA regex permits constructs incompatible with RE2's linear-time guarantees.
The engine MUST therefore expose and enforce:

- input byte/rune limits;
- pattern length, AST depth, capture, class, and program limits;
- match step/backtrack, recursion/stack, allocation, and wall-time budgets;
- context cancellation;
- bounded result and replacement output;
- explicit timeout/limit errors distinct from no-match and invalid pattern.

No panic, runaway goroutine, unbounded recursion, unsafe code, hidden worker,
or global mutable cache is permitted.

## API And Semantics

- Strict `Compile`, `Match`, `Find`, `FindAll`, `Replace`, `Split`, and capture
  APIs only where fully specified.
- Match/capture indices MUST state UTF-16 code-unit, rune, and byte semantics;
  ECMA-visible indices must remain correct.
- Preserve missing versus unmatched captures.
- Define `lastIndex`, global, sticky, multiline, dotAll, ignoreCase, Unicode,
  and Unicode Sets behavior exactly where exposed.
- Custom caches are caller-owned, bounded, and optional.

## Verification

Meaningful 100% production statement coverage is mandatory. Add:

- complete applicable Test262 RegExp corpus with exact skip accounting;
- JSON Schema regex vectors;
- differential tests against conforming JavaScript engines;
- differential diagnostics against regexp2/goja where behavior overlaps;
- Unicode-version provenance and exhaustive generated character properties;
- fuzzing of lexer, parser, compiler, matcher, replacement, and round trips;
- mutation tests for grammar, flags, captures, backtracking, Unicode, and
  limits;
- race/leak/cancellation tests and catastrophic-backtracking adversarial cases;
- fair parse/compile/match benchmarks with correctness gates.

## Documentation And Automation

Provide support matrices, conformance evidence, syntax, flags, Unicode,
captures, replacement, JSON Schema profile, limits, security, performance,
migration from Go regexp/PCRE, API, cookbook, FAQ, compatibility, and changelog.
CI/local gates follow ecosystem standards and pin all specification fixtures.

## Acceptance Criteria

- Every claimed ECMA feature maps to normative text and executable evidence.
- JSON Schema can use the package without semantic approximation.
- Catastrophic patterns terminate under explicit budgets.
- Unsupported syntax fails explicitly rather than falling back to RE2.
- Meaningful 100% coverage and every blocking gate pass.
