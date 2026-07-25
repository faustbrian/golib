# Policy lifecycle and migration

One `policy.Manifest` is the portable source-of-truth unit. It names the format,
revision, combining algorithm, and versioned ACL, RBAC, ABAC, or composite
records. Activation compiles the complete manifest into one immutable snapshot;
partial activation is not supported.

## Model and review

Write the invariant in plain language first, choose a model with the
[decision tree](model-selection.md), assign stable policy IDs, and make
precedence explicit. IDs should describe the durable rule rather than a ticket
or deployment. Avoid sensitive metadata because IDs and metadata may appear in
administrative views and explanations.

Reviewers should verify tenant scope, explicit-deny interaction, attribute
authority, activation windows, work limits, and the behavior when no rule
matches. A policy that cannot be explained from these inputs is not ready to
publish.

## Candidate validation

Decode strictly, reject unknown fields and trailing data, compile with bounded
document size, and run model validation. Use `policy.Diff` to review structural
changes and `policy.DryRun` with representative allowed, denied, boundary, and
cross-tenant requests to review semantic changes.

Dry-run does not install the candidate. Store its report with deployment
evidence only after removing application-sensitive request data from the
surrounding logs.

## Publish and activate

1. Load the authoritative manifest and record its revision.
2. Build and validate a candidate with a strictly greater revision.
3. Persist it using the loaded revision as the optimistic expectation.
4. Publish the new revision hint to Valkey after persistence succeeds.
5. Let every instance reload the repository, compile, and atomically replace
   its snapshot.
6. Observe revision convergence and decision-error telemetry.

Valkey failure does not invalidate the PostgreSQL write. The synchronizer polls
the repository as a correctness path and eventually discovers the revision.
Keep the last verified snapshot active when a reload fails; never synthesize an
empty or allow-all snapshot.

## Rolling deployments

Old and new processes may briefly evaluate different complete revisions. Each
decision reports its revision, and no decision mixes definitions from both.
Before deploying a format or semantic change, ensure every running binary can
decode the candidate. Publish the new manifest only after the compatible binary
has reached the required fleet percentage.

Monitor the minimum and maximum active revision across instances. Convergence
outside the normal polling interval indicates repository, compiler, or
invalidation failure. Roll back by publishing a new higher revision containing
the previous policy semantics; revisions never decrease.

## Format migration

Portable format identifiers and model document versions are semantic-versioned
contracts. To introduce an incompatible document:

1. add a new decoder/version while retaining the old decoder;
2. deploy readers that accept both versions;
3. translate and dry-run stored policies;
4. publish a higher manifest revision using the new version;
5. wait for fleet convergence; and
6. remove the old decoder only in a release whose compatibility policy permits
   it and after no stored document uses it.

Do not rewrite persisted revisions in place. Preserve an audit record of who
approved the candidate, its diff, dry-run evidence, stored revision, and
activation outcome.

## Schema migration

Apply `postgres.GoMigration` through `migrations` or adopt the SQL from
`postgres.SchemaMigration` into the application's migration system. Run schema
migrations before code that requires the repository. Initialize the first
manifest with expected revision zero; every later update uses the current
non-zero revision.
