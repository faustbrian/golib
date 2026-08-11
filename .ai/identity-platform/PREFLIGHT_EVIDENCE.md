# Identity Platform Preflight Evidence

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

This coordinator-owned file is the durable preflight and resource registry for
one identity-platform execution. The coordinator MUST replace every `pending`
value, add rows as requirements are discovered, and commit the completed
record on the integration branch before the first worker assignment. It MUST
NOT contain credentials, tokens, provider payloads, secret-shaped values, or
unsanitized logs.

## Execution identity

| Field | Value |
| --- | --- |
| Recorded committed `main` base | pending |
| Integration branch | `feature/identity-platform` |
| Integration worktree | pending |
| Task-owned worktree parent | pending |
| Preflight input revision before the record commit | pending |
| Preflight recorded at (RFC3339) | pending |

The committed base MUST be a full commit hash. The integration worktree and
task-owned parent MUST be absolute, distinct from the repository root and the
home directory itself, registered by Git, and resolved before any destructive
cleanup.

## Tool and environment lanes

| Requirement/profile | Required by units or claims | Classification | Version/environment identity | Evidence path or blocking claim |
| --- | --- | --- | --- | --- |
| Go toolchain and repository gate tools | all units | pending | pending | pending |
| PostgreSQL profile | pending | pending | pending | pending |
| Valkey profile | pending | pending | pending | pending |
| race, fuzz, leak, and mutation tooling | pending | pending | pending | pending |
| browser and interoperability harnesses | pending | pending | pending | pending |

`Classification` MUST be exactly `available`, `unavailable`, or
`not-yet-needed`. Each unavailable row MUST name every acceptance claim it can
block. Availability is preflight information only and MUST NOT be reused as a
passing interoperability result.

## External evidence lanes

| Safe profile ID | Consuming units | Exact acceptance claims | Classification | Credential source metadata | Evidence path or blocker |
| --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending |

The coordinator MUST create one row per required external provider or
independent implementation. Credential source metadata records presence and a
safe source identifier only. It MUST NOT contain the credential, its hash, a
prefix, or a provider response. A unit with any row here MUST use an
attributable evidence-record path, not `available`, before becoming `verified`.

## Existing primitive contracts

| Primitive | Consuming units | Registered module/package | API input fingerprint | Gate fingerprint and result | Evidence path |
| --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending |

The coordinator MUST derive this table from every goal's `Consumes existing
primitives` section. A fingerprint MUST cover the complete behavior-affecting
inputs required by repository evidence policy. Missing, incompatible, stale,
or unregistered primitives MUST name the exact blocked consumers.

## Task-owned resource registry

| Resource ID | Type | Owning unit/task | Exact path or safe external ID | State | Cleanup trigger | Last reconciled at |
| --- | --- | --- | --- | --- | --- | --- |
| pending | pending | coordinator | pending | pending | pending | pending |

Every worktree, disposable cache, temporary directory, process, container,
image, volume, database payload, browser artifact, and provider fixture created
for the program MUST be registered before use. The integration worktree is the
only bootstrap exception: it MUST be registered immediately after creation and
workspace verification, before any operation other than completing and
committing this preflight record. `State` MUST be `active`,
`retained-for-recovery`, `removal-pending-after-final-commit`, or `removed`.
Only the integration worktree MAY use
`removal-pending-after-final-commit`, under the two-phase cleanup in
`ORCHESTRATOR_GOAL.md`. Evidence MUST be captured into its durable record before
the disposable source resource is removed. Removed
entries remain as sanitized audit records. A retained entry MUST name an exact
cleanup trigger; the coordinator MUST reconcile this table after interruption
and before final or blocked reporting.

## Conflict-recovery baselines

| Unit | Generation | Integration commit | Worker checkpoint | Conflict evidence path | Status | Recorded at |
| --- | --- | --- | --- | --- | --- | --- |

Rows are append-only. `Status` MUST be `authorized`, `superseded`, or
`completed`. An authorization row MUST be committed with the retained
assignment's `blocked -> in-progress` transition and MUST name commits that
exist with the required ancestry. A later integration-head change supersedes
the row before another recovery merge is allowed. Successful integration marks
the exact row completed. The table contains only sanitized evidence paths, not
conflict contents or secrets.
