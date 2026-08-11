# Identity Platform Execution Ledger

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

This coordinator-owned ledger records recoverable orchestration identity.
`INVENTORY.md` remains authoritative for dependency, status and current
owner/blocker. Every inventory status or owner/blocker change MUST update the
corresponding ledger row in the same coordinator commit. Ledger-only metadata
such as a newly known prior commit hash or evidence pointer MAY be finalized in
a later ledger-only commit because the hash of a commit cannot be embedded in
that same commit. Secret values and provider credentials MUST NOT appear here.

`Generation` is incremented whenever an abandoned assignment is safely returned
to `ready`. `Assignment commit` is `pending` only in the assignment-state
commit; the immediately following ledger-only finalization MUST replace it with
that prior commit's hash before the worker starts. `Worker commit` is the latest
reviewed worker-branch tip returned for the assignment; after a recovery it MAY
be downstream of an authorized baseline merge and is not assumed to be the only
package-authored commit. `Integration checkpoint` is
the already-known non-fast-forward worker merge commit, never the hash of the
status commit that records it. `External evidence` records only `not-needed`,
`available`, `unavailable:{safe-profile-id}`, or an attributable evidence-record
path. A dash means never assigned. `available` records preflight availability
only and MUST NOT be treated as acceptance evidence.

After `initial`, `Last transition` MUST use
`v<positive-integer> <status> owner=<owner-or-dash> at=<RFC3339>` and mirror
the inventory status and owner/blocker exactly. Every later ledger update MUST
increment that version. Git history remains the transition journal.

## Transition history validation

When supplied the prior inventory and ledger snapshots, `validate.rb` MUST
prove every row's exact status edge, single-step transition-version increment,
generation rule, and permitted field delta. Git reachability, first-parent
commit identity, and live task state still require the coordinator's before/after
commit procedure. Before
every state or ledger-only commit, the coordinator MUST run a transition-check
procedure against the integration branch's first-parent and proposed current
states. That procedure MUST prove the status edge is allowed by
`DEPENDENCIES.md`, transition version changes by exactly one, generation changes
only and exactly when an assignment is abandoned or replaced, all recorded
commits exist with the required ancestry, and a same-status update changes only
the metadata explicitly permitted for that finalization. A failed or unavailable
history check blocks the state commit; passing static validation is not a
substitute.

The procedure MUST use the current integration `HEAD` as the candidate commit's
expected first parent, parse the prior inventory and ledger from that commit,
compare only the affected rows with the proposed files, and validate the whole
proposed snapshot. For every recorded hash it MUST prove object existence and
the ancestry required by the assignment or integration topology. It MUST also
compare active task, branch, and worktree identities with `git worktree list`
and all other active rows. Immediately after committing, it MUST prove the new
commit's first parent equals the recorded expected parent and that its actual
file diff equals the checked candidate. The coordinator MUST stop before a
worker spawn, merge, gate, or next state transition when either half fails.

Worker task and owner IDs MUST be whitespace-free safe identifiers. Worker
branches MUST be conventional and worktrees absolute. Integrated gate
fingerprints MUST use `sha256:<64-lowercase-hex>`. A verified row whose preflight
classification has no external acceptance claim MUST record `not-needed`. A
verified row with any external acceptance claim MUST record an attributable
`.ai/...` evidence path whose record contains the tested profile, result,
execution revision, complete input fingerprint, tool/environment identity, and
sanitized artifact hashes. It MUST NOT retain `available`, missing, or
unavailable evidence.

Active worker tasks, branches, and worktrees MUST be pairwise unique. Every
recorded commit MUST exist and have the required integration or worker ancestry.
Every active worktree MUST be a registered Git worktree below the program's
task-owned worktree parent; `/`, the repository root, the home directory
itself, and unregistered absolute paths are forbidden. `pending` is valid only
in the one assignment-state commit and MUST be finalized before a worker
starts.

## Per-status row schema

- An initial `proposed` row MUST have generation `0`. A post-initial `proposed`
  row MAY retain generation greater than `0` only after a history-validated
  `ready -> proposed` prerequisite-invalidation transition. Every `proposed`
  row MUST have owner `—` and every assignment and evidence field `—`.
- `ready` MUST have owner `—` and every assignment and evidence field `—`.
  Its generation is `0` before first assignment and increases by exactly one
  only when a prior assignment is safely abandoned.
- A first-assignment `in-progress` row MUST have owner equal to worker task;
  task, branch, worktree, and assignment commit MUST be present; and worker
  commit, integration checkpoint, and gate fingerprint MUST be `—`.
  `pending` is permitted only in its assignment-state commit. An integrated
  repair `in-progress` row MUST retain task, branch, worktree, assignment
  commit, worker commit, integration checkpoint, gate fingerprint, and
  generation from `implemented-unverified` while restoring worker task as
  owner. A pre-integration conflict-recovery `in-progress` row MUST retain the
  same task, branch, worktree, assignment commit, and generation from its
  `blocked` parent; worker commit MAY be the clean coherent checkpoint while
  integration checkpoint and gate fingerprint remain `—`. Its exact authorized
  recovery baseline MUST be recorded in the matching
  `PREFLIGHT_EVIDENCE.md` conflict-recovery row in the same resume/authorization
  commit. That row's integration commit is the resume commit's first parent,
  not the resume commit itself. Before any package edit, the worker MUST merge
  the exact resume/authorization commit supplied by the coordinator and prove
  that commit has the recorded integration parent and worker checkpoint.
- A retained-assignment `blocked` row MUST use `blocker:<safe-id>` as owner and
  retain task, branch, worktree, assignment commit, and generation. Before
  integration, worker commit MAY be `—` or the clean coherent stale-baseline
  commit and integration checkpoint and gate fingerprint MUST be `—`. After
  integration, worker commit, integration checkpoint, and gate fingerprint
  MUST remain present. An abandoned assignment MUST transition to `ready`; a
  blocked row with cleared assignment fields is invalid.
- `implemented-unverified` and `verified` MUST have owner `—` and complete task,
  branch, worktree, assignment, worker commit, integration checkpoint, and gate
  fingerprint fields. `implemented-unverified` external evidence MUST be
  `available`, `unavailable:<safe-profile-id>`, `not-needed`, or an attributable
  path. `verified` external evidence follows the stricter rule above.

Assignment generation MUST NOT change during assignment finalization, pause,
same-owner repair, integration, evidence recording, or verification. Every
`ready -> proposed` prerequisite-invalidation transition MUST retain generation
and empty assignment/evidence fields. Every
ledger row update after `initial`, including a permitted same-status metadata
finalization, MUST increment transition version by exactly one.
Prior/current ordinary validation authorizes only the edges in
`DEPENDENCIES.md`; the coordinator-owned dependency-revision reset below is the
only additional transition family.
Same-status updates are limited to assignment-commit finalization for
`in-progress`, a clean worker checkpoint or evidence disposition for `blocked`,
and evidence-record finalization for `implemented-unverified` or `verified`.
Every other same-status field change is forbidden.

## Dependency revisions

Only the coordinator MAY change an inventory `Requires` set. A worker MUST
report the discovered dependency change and stop; it MUST NOT edit inventory,
goals, or the dependency graph. Before accepting a revision, the coordinator
MUST update and validate the affected goal metadata and complete Mermaid DAG,
prove the graph is acyclic, and append one exact row below for every unit whose
`Requires` set changes. Rows are append-only.

The affected set is the changed unit plus its complete reverse-dependent
closure across the union of the prior and current graphs. Every affected unit
MUST be reset to `ready` only when all of its current prerequisites are
`verified`, otherwise to `proposed`. Its owner, assignment, checkpoint,
fingerprint, and external evidence MUST be cleared. If it had any prior
assignment identity, its generation MUST increment exactly once; otherwise the
generation is retained. Its transition version MUST increment exactly once.
Every row outside that closure MUST remain byte-for-byte unchanged. The change
digest binds the revision ID, unit, prior/current Requires lists, closure,
reason, coordinator approver, and timestamp.

`Previous Requires` and `Current Requires` MUST preserve the exact order used
by the corresponding prior and current inventory rows; that inventory-list
order is canonical and the digest is order-sensitive. `Affected reverse
closure` MUST be lexically sorted and unique.

An affected `in-progress` assignment MUST NOT be cleared by a dependency
revision. The coordinator MUST first pause it in a separate committed
`in-progress -> blocked` transition. Before the later dependency revision may
clear a blocked assignment, the coordinator MUST append exactly one assignment
disposition row below. That row MUST preserve its exact generation, task,
branch, worktree, and assignment commit; bind every dependency revision whose
closure contains the unit; and record either the clean worker checkpoint, the
clean assignment baseline when no worker checkpoint exists, or an attributable
safe-abandonment reason. Every disposition MUST reference canonical JSON
evidence bound to its disposition/revision IDs, unit, generation, worker/task
identity, worktree, assignment, preservation decision, coordinator approval,
timestamp, and complete prior/current resource identities and states. It MUST
enumerate every task-owned resource registered to that worker, including its
exact worktree, with a final state of
`retained-for-recovery` or `removed`. Retained worktrees MUST be clean at the
recorded checkpoint or baseline; safe abandonment MUST remove every task-owned
resource but MUST NOT authorize loss of dirty or unintegrated work. Every
disposition MUST name a preserved commit: the worker checkpoint when present,
otherwise the assignment baseline. Before any worker worktree is removed,
including for safe abandonment, it MUST be clean and its exact HEAD MUST equal
that preserved commit; disposition evidence MUST capture both facts. Safe
abandonment MAY explain why clean committed work is no longer pursued, but it
MUST NOT substitute for committing or otherwise preserving uncommitted work.
Removed resources MUST retain their state-specific cleanup evidence in
`PREFLIGHT_EVIDENCE.md`. The resource
registry and disposition row together MUST prove that clearing ledger ownership
does not lose unintegrated work or orphan registered resources. Disposition
rows are append-only and prior rows MUST remain byte-for-byte unchanged.
Each append-only row MUST record the SHA-256 digest of the canonical evidence
file bytes; every later validation MUST recompute it, so historical resource
identity, cleanup, authorization, and pre-removal proof cannot be rewritten.
Prior/current transition validation MUST receive the previous committed
`PREFLIGHT_EVIDENCE.md` snapshot as `--previous-execution-fixture` whenever an
affected active assignment is cleared, so deleted or substituted registry rows
cannot masquerade as cleanup.

Disposition evidence MUST be canonical JSON with `schema_version: 1` and these
ordered fields: `schema_version`, `disposition_id`, `revision_ids`, `unit`,
`generation`, `worker_task`, `branch`, `worktree`, `assignment_commit`,
`preservation`, `resources`, `authorized_by`, and `recorded_at`.
`authorized_by` MUST be `coordinator`. `preservation` MUST be exactly either
`{"kind":"clean-checkpoint","commit":"<commit>"}`,
`{"kind":"clean-baseline","commit":"<commit>"}`, or
`{"kind":"safe-abandonment","reason":"reason:<safe-id>","recoverable_commit":"<commit>"}`.
Each resource
object MUST use the ordered fields `resource_id`, `type`, `owner`, `target`,
`previous_state`, `current_state`, `cleanup_evidence`, `pre_removal_clean`, and
`pre_removal_head`; resource objects MUST be sorted by ID. The two pre-removal
fields MUST be `null` for retained resources and MUST record `true` plus the
exact preserved commit for every removed worker worktree.

| Revision ID | Unit | Previous Requires | Current Requires | Affected reverse closure | Reason | Change digest | Approver | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |

## Dependency assignment dispositions

| Disposition ID | Revision IDs | Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Preservation proof | Preserved commit | Disposition evidence | Evidence digest | Resource dispositions | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

## Unit execution ledger

| Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Worker commit | Integration checkpoint | Gate fingerprint | External evidence | Last transition |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `identity` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/session` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/session/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/session/valkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/delivery` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/delivery/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/valkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/recaptcha` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/turnstile` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/hcaptcha` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/captchafox` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/hibp` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/password` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/password/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/username` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/email` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/magiclink` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/otp` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/otp/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/phone` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/anonymous` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/anonymous/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/mfa` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/mfa/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `webauthn` | 0 | — | — | — | — | — | — | — | — | initial |
| `webauthn/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `passkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `passkey/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/providers` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/onetap` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/proxy` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/apikey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/apikey/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/apikey/valkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/impersonation` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/impersonation/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `organization` | 0 | — | — | — | — | — | — | — | — | initial |
| `organization/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/domain-verification` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/oidc` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/oauth2` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/saml` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `scim` | 0 | — | — | — | — | — | — | — | — | initial |
| `scim/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `scim/organization` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server/oidc` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server/device` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/i18n` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/http` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/reference` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/identitytest` | 0 | — | — | — | — | — | — | — | — | initial |
