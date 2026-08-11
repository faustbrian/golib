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

| Safe profile ID | Consuming units | Exact acceptance claim IDs | Classification | Credential source metadata | Evidence path or blocker | Evidence digest or blocker |
| --- | --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending | pending |

The coordinator MUST create one row per required external provider or
independent implementation. Consumer units and stable claim IDs MUST be exact,
sorted, and unique. Credential source metadata records presence and a
safe source identifier only. It MUST NOT contain the credential, its hash, a
prefix, or a provider response. A unit with any row here MUST use the lane's
attributable JSON evidence-record path before becoming `verified`; `not-needed`,
`available`, and blockers are not acceptance evidence. The digest MUST bind the
exact record bytes. Schema version 1 records MUST contain an RFC3339 time no
earlier than preflight, exact profile, claim and unit attribution, and a passing
unit result with execution revision, complete input fingerprint,
tool/environment identity, and nonempty sanitized artifact SHA-256 hashes.
Every verified unit result MUST match its ledger checkpoint and gate fingerprint.

## Existing primitive contracts

| Primitive | Consuming units | Registered module/package | API input fingerprint | Gate fingerprint and result | Evidence path |
| --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending |

The coordinator MUST derive each primitive's consuming units as the exact,
sorted, unique union of every goal's `Consumes existing primitives` section.
A fingerprint MUST cover the complete behavior-affecting inputs required by
repository evidence policy. A `pass` result permits the normal unit lifecycle.
A `failed`, `blocked`, or `stale` result permits a consumer to remain only
`proposed` with owner `—`, before assignment, or `blocked` as a paused retained
assignment with the exact owner claim
`blocker:primitive-<primitive-with-slashes-replaced-by-hyphens>`. Such a
consumer MUST NOT be `ready`, `in-progress`, `implemented-unverified`, or
`verified`, and MUST NOT receive new work or assignment until the primitive
result is `pass`.

## Task-owned resource registry

| Resource ID | Type | Owning unit/task | Exact path or safe external ID | State | Cleanup trigger | Last reconciled at | Cleanup evidence or attestation |
| --- | --- | --- | --- | --- | --- | --- | --- |
| pending | pending | coordinator | pending | pending | pending | pending | pending |

Every worktree, disposable cache, temporary directory, process, container,
image, volume, database payload, browser artifact, and provider fixture created
for the program MUST be registered before use. The integration worktree is the
only bootstrap exception: it MUST be registered immediately after creation and
workspace verification, before any operation other than completing and
committing this preflight record. `State` MUST be `active`,
`retained-for-recovery`, `removal-pending-after-final-commit`, or `removed`.
Only the integration worktree MAY use
`removal-pending-after-final-commit`, under the two-phase cleanup in
`ORCHESTRATOR_GOAL.md`. `Type` MUST be `worktree`, `go-cache`,
`temporary-directory`, `browser-artifact`, `process`, `container`, `image`,
`volume`, `database-payload`, or `provider-fixture`. Live local paths MUST
exist, removed local paths MUST be absent, and worktrees MUST additionally
match Git registration. Removed local resources MUST name an attributable
repository evidence path. External resources MUST name either an attributable
repository evidence path or the exact sanitized
`attestation:<state>:<safe-external-id>` for their current state. Evidence MUST
be captured into its durable record before the disposable source resource is
removed. Removed entries remain as sanitized audit records. A retained entry
MUST name an exact cleanup trigger; the coordinator MUST reconcile this table
after interruption and before final or blocked reporting. Once every inventory
unit is `verified`, every resource MUST be `removed` except the exact integration
worktree, which MAY remain `removal-pending-after-final-commit` until the final
record commit is created.

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
