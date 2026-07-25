# FAQ and troubleshooting

## Why was a request denied with `default-deny`?

No active policy returned an applicable outcome. Check subject kind and ID,
tenant, action, resource type and ID, activation time, and model scope. Evaluate
the model directly in a test to distinguish `NotApplicable` from an explicit
deny, then inspect the root trace.

## Why does the decision revision differ across instances?

A rolling activation is in progress or an instance is failing to reload. Check
repository reachability, compiler errors, synchronizer supervision, and the
configured poll interval. Valkey delivery is not required for eventual
convergence.

## Why did publication return a revision conflict?

Another writer updated the manifest after it was loaded. Reload, reapply the
intended semantic change, rerun diff and dry-run, and publish against the new
revision. Do not overwrite the winner or reuse a stale candidate blindly.

## Can a policy revision be rolled back?

Semantics can be rolled back; revision numbers cannot. Publish a new higher
revision containing the previous safe policy.

## Why is a global ACL or role not applying to a tenant?

Global inheritance is disabled by default. Enable it explicitly only when the
application's tenant model makes global grants safe. Explicit tenant-local
policy is easier to audit.

## Why can an ACL list operation return `ErrUnboundedResourceSet`?

A type-wide grant describes an unbounded set that the in-memory ACL cannot
enumerate. Apply a bounded database predicate or use concrete resource grants.

## Why did ABAC return a limit error?

The condition exceeded depth, cost, set, match, named-condition, or rule
limits. Simplify the policy or raise a specific limit only after benchmarking
the accepted worst case. Predicates never perform database or network I/O.

## Should application code retry an authorization error?

Not on the request path. Treat the current operation as denied. Operators may
repair or reload policy outside the decision. Retrying a deterministic invalid
request or policy does not make it safe.

## Can I cache decisions?

Usually no. Decisions depend on subject, resource, attributes, time, and
revision, making safe keys and invalidation difficult. Cache immutable manifests
advisorially by exact revision and continue to verify repository state.

## How do I debug without leaking data?

Use outcome, stable reason, revision, bounded policy IDs, trace counts, and
request correlation already owned by the application. Reproduce with sanitized
fixtures and `authorizationtest.CanonicalDecisionJSON`. Do not log full requests
or policy documents.

## Integration tests are skipped locally

This is expected without `POSTGRES_URL` or `VALKEY_ADDRESS`. Point those
variables only at disposable test services. Unit, race, fuzz, mutation, and
adapter tests do not require production services.

## Which model should I use?

Use the [model selection decision tree](model-selection.md). If the rule is a
domain invariant for every caller, keep it in application code.
