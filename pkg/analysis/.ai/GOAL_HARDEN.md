# Hardening Goal: Organization-Grade Go Static Analysis

## Objective

Prove that `analysis` produces deterministic, precise, low-noise diagnostics
without panics, semantic conflicts, unsafe suggested fixes, suppression bypass,
secret exposure, or unacceptable local/CI overhead across the complete owned Go
repository corpus.

## Required Audits

### Diagnostic Precision Audit

- Build positive, negative, near-miss, alias, embedding, generics, interface,
  closure, build-tag, generated, and multi-package fixtures for every rule.
- Measure false positives and false negatives against manually reviewed corpus
  samples before promoting a rule to blocking.
- Mutation-test every branch controlling whether a diagnostic is emitted.
- Verify locations, related information, rule IDs, rationale, and remediation.

### Suggested Fix And Suppression Audit

- Compile and test every suggested fix; prove no behavior-changing fix is
  offered without deterministic safety evidence.
- Fuzz suppression grammar, placement, unknown IDs, reasons, expiry, generated
  headers, and malformed comments.
- Prove broad, stale, misplaced, duplicated, or forged-generated suppressions
  cannot disable findings.
- Emit and validate a complete suppression inventory.

### Conflict And Governance Audit

- Compare every rule with compiler, vet, Staticcheck, golangci-lint, gosec,
  CodeQL, govulncheck, and NilAway behavior/configuration.
- Detect contradictory prescriptions and duplicate authorities automatically
  where possible.
- Prove advisory-to-blocking promotion is versioned and cannot silently change
  consuming CI.
- Test configuration inheritance, unknown keys, precedence, and drift checks.

### Robustness And Performance Audit

- Fuzz parser/type/fact/config/report boundaries and require no analyzer panic.
- Exercise invalid packages, partial type information, build constraints,
  platform files, cgo stubs, generated code, and dependency failures.
- Prove deterministic output across concurrency, platform, path, and map order.
- Benchmark cold/warm full-corpus runs, each rule, peak memory, facts, and SARIF.
- Enforce diagnostic, fact, trace, source-snippet, and analysis cost bounds.

### Security And Supply-Chain Audit

- Threat-model target-code execution, config injection, path traversal, SARIF
  injection, source/secret disclosure, malicious generated headers, and plugin
  compromise.
- Verify analyzers never execute target code or arbitrary config programs.
- Audit release reproducibility, checksums, dependencies, action pinning, and
  least-privilege permissions.

## Required Deliverables

- Rule precision, overlap/ownership, and promotion matrices.
- Full-corpus expected finding baseline and false-positive report.
- Suggested-fix, suppression, fuzz, mutation, and determinism evidence.
- Performance budgets and cold/warm benchmark baselines.
- Threat model, supply-chain report, and updated rule/API documentation.

## Release Blockers

- Any false-positive-prone blocking rule, contradictory policy, analyzer panic,
  suppression bypass, unsafe fix, non-deterministic output, secret leak, target
  code execution, or unbounded analysis behavior.
- Any hidden blocking behavior from advisory NilAway findings.
- Any local/CI command or version mismatch.
- Missing meaningful 100% coverage, corpus proof, or green blocking CI.

## Completion Criteria

- Every blocking rule has reviewed precision and mutation-resistant fixtures.
- Complete owned repository corpus and configuration conflict suites pass.
- Fuzz, race, vulnerability, reproducibility, and performance gates pass.
- NilAway integration remains visible and advisory.
- No release blocker remains and the changelog is current.
