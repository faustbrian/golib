# Goal: Add JSON Schema Decisions And Competitor Benchmarks

## Objective

Add explicit specification-decision records and correctness-gated comparative
benchmarks after the original JSON Schema goal was written. Keep the original
goal and hardening files unchanged.

## Specification Decisions

- Implement `docs/specification-decisions.md` under the root
  `.ai/GOAL_SPECIFICATION_DECISIONS.md` contract.
- Cover dialect selection, unknown vocabularies and keywords, format assertion,
  content behavior, annotations, unevaluated behavior, dynamic references,
  reference siblings, duplicate JSON members, numeric precision, regex
  semantics, URI normalization, output formats, remote loading, and optional
  official fixtures.
- Link every resolved choice to normative text, alternatives, tests, official
  fixtures, interoperability results, security impact, and compatibility
  impact.
- Keep unresolved choices visible and release-blocking when they affect a
  compliance claim.

## Competitor Matrix

- Use `github.com/santhosh-tekuri/jsonschema/v6` as the primary mature Go
  comparison.
- Evaluate qri-io/jsonschema and xeipuuv/gojsonschema for current maintenance,
  supported drafts, and comparable features before including them.
- Use Bowtie only as cross-language conformance/performance context.
- Use `encoding/json` only as a raw parsing-overhead baseline.
- Separate load, parse, compile, first validation, warm validation, valid and
  invalid instances, diagnostics, local and remote references, formats, and
  concurrent reuse.
- Match draft, fixture subset, registry, references, format policy, output mode,
  limits, schema reuse, and expectations.
- Disqualify candidates from ranked tracks when shared correctness fixtures
  fail; do not silently omit unsupported cases.
- Pin versions and store adapters, fixture hashes, raw results, profiles,
  environment metadata, and statistical analysis.

## Completion Criteria

- Specification choices are auditable and executable.
- Comparative results are reproducible, correctness-gated, and honest about
  unsupported capabilities.
- Documentation, changelog, CI, and local benchmark commands are current.

