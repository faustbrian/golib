# Policy composition and explanation

An `authorization.Snapshot` installs one or more typed evaluators and one
explicit combining algorithm. A decision loads the active snapshot once, so
concurrent replacement cannot mix policy revisions during a request or batch.

## Outcomes and default deny

Model evaluators return one of three outcomes:

- `Allow` means at least one policy explicitly grants the request;
- `Deny` means at least one policy explicitly rejects the request; and
- `NotApplicable` means the evaluator has no matching policy.

The root engine converts a final `NotApplicable` result to `Deny` with
`ReasonDefaultDeny`. Model evaluators retain `NotApplicable` so applications can
compose them without losing the distinction between no match and explicit
deny.

## Combining algorithms

`DenyOverrides` returns deny when any evaluated policy denies, otherwise allow
when any allows. It short-circuits after the first deny.

`AllowOverrides` returns allow when any evaluated policy allows, otherwise deny
when any denies. It short-circuits after the first allow. This algorithm should
only be selected when an allow is intentionally authoritative over every deny.

`FirstApplicable` evaluates definitions in insertion order and returns the
first non-`NotApplicable` result.

`PriorityOrder` sorts definitions by descending priority and then stable policy
ID before returning the first applicable result.

Every algorithm rejects invalid outcome values before they can become an allow.
The test suite exhaustively checks every sequence of up to four three-valued
outcomes, in addition to model conformance tests.

## Explanation

Every root decision includes:

- the final outcome and structured reason code;
- the coherent snapshot revision;
- stable matched policy or model-rule IDs; and
- a bounded trace of top-level evaluator outcomes.

The trace never includes request attributes or their values. Matched policy IDs
and trace entries have independent cardinality limits. When either limit is
exceeded, `MatchedPolicyIDsTruncated` or `TraceTruncated` is set rather than
allocating an unbounded diagnostic. Truncation from a nested evaluator is
preserved by the engine and instrumentation. Evaluator errors expose the stable
policy ID but redact the underlying error text from the public string.

## Diff and dry-run

`policy.Diff` compares immutable snapshot metadata and reports sorted added,
removed, and changed IDs plus combining-algorithm changes.

`policy.DryRun` evaluates the same bounded request batch against current and
candidate snapshots. It reports both decisions and whether authorization
semantics changed, without installing the candidate or changing the active
engine. Decision changes compare outcome, reason, matched IDs, and whether
those IDs were truncated; snapshot revision differences alone do not count as
an access change.

Dry-run input is application data. Callers remain responsible for avoiding
sensitive values in their own logs; the report contains decisions, not request
attributes.
