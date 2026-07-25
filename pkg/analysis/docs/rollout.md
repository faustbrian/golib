# Repository rollout and advisory promotion

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC 2119] and [RFC 8174] when, and only when, they appear in all capitals,
as shown here.

[RFC 2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC 8174]: https://www.rfc-editor.org/rfc/rfc8174

## Pin one executable and policy

Each repository MUST pin a released `golib-analysis` executable and verify its
checksum. Local and CI commands MUST use that same executable and the same
checked-in policy path. CI MUST run from a clean checkout; invocation-directory
differences must not change policy-relative path behavior.

When an organization owns canonical policy in another repository, consumers
MUST use a reproducible sync or drift check. Silently copied YAML is forbidden.
Use `make policy-update CANONICAL_POLICY=<path>` to synchronize the default
`analysis.yml`, and run `make policy-check CANONICAL_POLICY=<path>` locally
and in CI. Set `LOCAL_POLICY=<path>` for a different checked-in destination.
The check validates the canonical policy and compares exact bytes. The
consuming repository SHOULD also record the canonical policy revision beside
the synchronized file so review can identify its source revision.

## Baseline an advisory rule

1. Validate configuration with `golib-analysis validate-config`.
2. Configure direct module allowlists, blocklists, replacements, and version
   constraints in the canonical golangci-lint `gomodguard_v2` policy.
3. Run a full-module advisory scan and retain its JSON or SARIF artifact.
4. Classify every finding against source and ownership evidence.
5. Fix true violations or add exact, reviewed suppressions with reasons.
6. Run the complete owned repository corpus and record stable expected
   advisories.
7. Measure cold and warm wall time and peak memory on representative modules.

Changed-package execution MAY provide fast pull-request feedback, but it MUST
NOT replace a full-module CI scan. Corpus baselines MUST NOT discard findings
by message text alone; rule ID, repository-relative location, and reviewed
classification are required.

## Promote a rule to blocking

A promotion decision MUST identify the rule ID, owner, tool version, policy
revision, corpus revision, migration guidance, and intended release version.
The rule MUST have:

- zero unexplained findings across the complete owned corpus;
- stable expected advisory output across repeated and concurrent runs;
- no contradictory blocking tool configuration;
- meaningful 100% production statement coverage and surviving-mutant count of
  zero for its diagnostic decisions;
- acceptable cold and warm performance evidence; and
- a documented suppression and rollback procedure.

Promotion changes `rules.<id>.status` from `advisory` to `blocking` and adds a
required `promotion.version` semantic version plus non-empty
`promotion.evidence` record. Configuration validation rejects blocking status
without both fields and rejects promotion metadata on non-blocking rules.
Severity does not control process exit status.
Repositories SHOULD stage promotion in a dedicated change so a rollback does
not revert unrelated policy.

## Handle findings

Fixes SHOULD remove the policy violation rather than move it behind an
untyped wrapper. Suggested fixes are absent unless semantics can be preserved
reliably. A suppression is appropriate only when the exact construct is a
reviewed exception, not when migration is merely inconvenient.

Suppressions MUST name the exact rule ID and include a non-empty reason. Issue
and expiry metadata SHOULD be present for temporary exceptions. Reviewers MUST
reject file-wide, package-wide, malformed, duplicate, stale, or unrelated
directives. CI SHOULD retain the suppression inventory for trend review.

## Roll back safely

If a promoted rule produces an analyzer defect or unexplained corpus finding,
the organization MAY return only that rule to advisory while investigation
continues. It MUST NOT hide diagnostics, mutate their severity to control exit
status, or disable the canonical external tool. The follow-up MUST preserve the
failing fixture and classify whether code, configuration, or analyzer semantics
were wrong.
