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
| Identity-platform base tree object | pending |
| Identity-platform base tree digest | pending |
| Integration branch | `feature/identity-platform` |
| Integration worktree | pending |
| Task-owned worktree parent | pending |
| Preflight input revision before the record commit | pending |
| Preflight recorded at (RFC3339) | pending |

The committed base and preflight input MUST be existing full commit hashes.
The recorded identity-platform tree object and SHA-256 digest MUST be derived
from the exact committed base. The integration branch MUST be registered, its
registered worktree MUST have the same branch and HEAD, and that HEAD MUST
descend from the exact base and contain the preflight input. The integration worktree and
task-owned parent MUST be absolute, distinct from the repository root and the
home directory itself, registered by Git, and resolved before any destructive
cleanup.

## Worker assignment attestations

| Unit | Generation | Integration baseline | Assignment commit | Rendered prompt | Prompt digest | Model | Reasoning | Fork turns | Subagents | Package scope | Reserved descendants | Goal digest | Authorized by | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

Each actual worker assignment MUST have exactly one durable `finalized` row
before spawn. The coordinator MUST commit the complete rendered prompt bytes at
the attributable repository path and bind their exact SHA-256 digest, unit,
generation, integration baseline, assignment commit, `gpt-5.6-sol`, `medium`,
`none` fork turns, `false` subagents, exact canonical package scope, complete
reserved-descendant set, pinned goal-body digest, and `coordinator`
authorization. Pending or missing attestation state blocks spawn.

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

| Safe profile ID | Consuming units | Exact acceptance claim IDs | Classification | Credential source metadata | Evidence path or blocker | Evidence record commit | Evidence digest or blocker |
| --- | --- | --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending | pending | pending |

The coordinator MUST create one row per required external provider or
independent implementation. Consumer units and stable claim IDs MUST be exact,
sorted, and unique. Credential source metadata records presence and a
safe source identifier only. It MUST NOT contain the credential, its hash, a
prefix, or a provider response. A unit with any row here MUST use the lane's
attributable JSON evidence-record path before becoming `verified`; `not-needed`,
`available`, and blockers are not acceptance evidence. The digest MUST bind the
exact committed record bytes. `Evidence record commit` MUST be an existing
commit that contains those exact bytes at the recorded repository path and is
an ancestor of the coordinator commit that marks a consumer verified.
Working-tree-only, untracked, modified-after-commit, or path-exists-only
evidence is invalid. Schema version 2 records MUST contain an RFC3339 time no
earlier than preflight, exact profile, claim and unit attribution, and a passing
unit result with distinct `tested_revision`, `gate_execution_revision`, and
`revalidation_revision` fields, a complete input manifest and canonical input root,
tool/environment identity, and nonempty sanitized artifact SHA-256 hashes. The
revalidation field MUST be null when the tested and gate revisions are equal.
When they differ, it MUST equal the gate execution revision, and validation
MUST recompute the same complete input root from both committed trees before
reusing the original result.
record MUST NOT embed its own containing commit; this table and the ledger bind
that later commit. Every verified unit result MUST match its recorded gate
execution revision and gate fingerprint, while the integration checkpoint
remains a separate worker-merge identity.

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
result is `pass`. Such a unit is explicitly excluded from dependency-frontier
promotion even when every unit in its inventory `Requires` list is `verified`;
primitive start gates are additional to, and not represented by, DAG edges.

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

## Acceptance evidence bindings

| Artifact ID | Evidence path | Evidence blob digest | Evidence record commit | Bound at |
| --- | --- | --- | --- | --- |

At final acceptance this table MUST contain exactly one row for every
`END_STATE_ACCEPTANCE.json` artifact. The path and artifact ID MUST match the
catalog, the digest MUST bind the exact blob read from the named existing
commit, and that commit MUST descend from the payload's tested revision and be
an ancestor of the binding/finalization commit. The payload MUST NOT contain or
predict its own containing commit. Rows are append-only; replacement evidence
requires a new attributable artifact generation rather than rewriting a prior
binding.

## Conflict-recovery baselines

| Recovery epoch | Unit | Generation | Integration commit | Worker checkpoint | Conflict evidence path | Status | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- |

Rows are append-only. `Recovery epoch` MUST be
`recovery:<unit-with-slashes-replaced-by-hyphens>:g<generation>:e<positive-sequence>`;
the sequence starts at one and increases by exactly one for each later recovery
of the same assignment generation. `Status` MUST be `authorized`,
`superseded`, or `completed`. An authorization row MUST be committed with the
retained assignment's `blocked -> in-progress` transition and MUST name commits
that exist with the required ancestry. A later integration-head change
supersedes the row before another recovery merge is allowed. Successful integration marks
the exact row completed. Completion is terminal only for that epoch, not for
the assignment generation: if the retained assignment later re-enters
`blocked` because a repaired tip conflicts again, the coordinator MUST append
the next epoch and MAY authorize the new clean checkpoint only when the prior
checkpoint is its ancestor on the exact worker branch. The table contains only
sanitized evidence paths, not conflict contents or secrets.
