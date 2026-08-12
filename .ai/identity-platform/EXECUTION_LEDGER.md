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
package-authored commit. `Integration checkpoint` is the already-known
non-fast-forward worker merge commit, never the hash of the later
registration/status commit that records it. `Gate execution revision` is a
distinct required field in every gate evidence record. It is the exact
post-integration, post-registration committed revision whose complete inputs
were tested and MUST NOT be inferred from or replaced by the integration
checkpoint. `External evidence` records only `not-needed`, `available`,
`unavailable:{safe-profile-id}`, or an attributable
`<evidence-record-path>@<evidence-record-commit>` binding. The record commit
MUST contain the exact record bytes at that path and MUST be an ancestor of the
ledger commit that records the binding. A dash means never assigned. `available`
records preflight availability only and MUST NOT be treated as acceptance
evidence.

Every assignment and integration row is also a commit-custody claim. The
coordinator MUST validate the complete first-parent history from the recorded
base under `ORCHESTRATOR_GOAL.md`'s commit-class envelopes. In particular, an
assignment-state commit changes exactly inventory and ledger; its direct
finalization child changes exactly ledger; authorization, runtime, and release
commits have their exact control-only envelopes; and an integration checkpoint
is an exact two-parent merge `[recorded-premerge-head, worker-tip]` with package
and outside-tree projections taken from the correct parent. An ancestry-only
relationship is insufficient.

No coordinator state, evidence, registration, or gate commit may change a
worker-owned package projection. The sole package-path exception is the
byte-identical relocation of the coordinator-custody goal from its planning to
canonical path. The validator MUST reject transient edits even when a later
commit restores the final bytes.

Goal custody follows location changes. A goal moved beneath a canonical module
root remains a coordinator-owned control path and is excluded from every
worker-authored range. Its path change is byte-preserving; any goal-body digest
change requires the prior user semantic authorization defined by `PROGRAM.md`
and `PREFLIGHT_EVIDENCE.md`.

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
The post-commit half MUST include
`validate.rb --execution --clean-integration` plus the required previous
fixture quartet after the initial snapshot; the clean mode is also REQUIRED
before spawn, merge, gate execution, restart recovery, and final acceptance.
Pre-commit candidate fixtures omit that mode because their proposed bytes are
not yet a committed executable checkpoint.

Worker task and owner IDs MUST be whitespace-free safe identifiers. Worker
branches MUST be conventional and worktrees absolute. Integrated gate
fingerprints MUST use `sha256:<64-lowercase-hex>`. A verified row whose preflight
classification has no external acceptance claim MUST record `not-needed`. A
verified row with any external acceptance claim MUST record an attributable
`.ai/...@<commit>` evidence binding whose record contains the tested profile,
result, distinct `tested_revision`, `gate_execution_revision`, and
`revalidation_revision` fields, a complete input manifest and canonical input
root, tool/environment identity, and sanitized artifact hashes. The
revalidation revision MUST be null when tested and gate revisions are equal.
For fingerprint-based reuse it MUST equal the gate execution revision, while
the tested revision retains the original execution identity; the manifest and
input root MUST validate identically from both committed trees. The record MUST
NOT contain or attempt to predict its
own containing commit. The ledger binding supplies that later evidence-record
commit without a self-referential hash. It MUST NOT retain `available`, missing,
or unavailable evidence.

Active worker tasks, branches, and worktrees MUST be pairwise unique. Every
recorded commit MUST exist and have the required integration or worker ancestry.
Every active worktree MUST be a registered Git worktree below the program's
task-owned worktree parent; `/`, the repository root, the home directory
itself, and unregistered absolute paths are forbidden. `pending` is valid only
in the one assignment-state commit and MUST be finalized before a worker
starts.

Every local gate record MUST use schema `identity-platform.local-gate.v2` with
ordered fields `schema_version`, `schema`, `unit`, `tested_revision`,
`gate_execution_revision`, `revalidation_revision`, `input_manifest`,
`input_root`, `evidence_record`, `outcome`, `commands`, `artifacts`,
`tool_identity`, `environment_identity`, and `record_digest`. `schema_version`
MUST be `2`; `evidence_record` MUST contain only the canonical record `path`.
The exhaustive manifest and root MUST validate at the tested revision and,
when reused, identically at the gate execution revision. Without reuse the two
revisions are the exact clean committed integration `HEAD` captured immediately
before execution and `revalidation_revision` is null. With reuse, the tested
revision remains the original execution, the current clean committed `HEAD` is
both gate execution and revalidation revision, and identical manifests are
REQUIRED. `INVENTORY.md`, `EXECUTION_LEDGER.md`, `PREFLIGHT_EVIDENCE.md`, and
files beneath `.ai/identity-platform/evidence/` are non-behavioral provenance excluded from
the behavior-input manifest; their committed revisions and record bindings
remain mandatory provenance. The current authoritative `Requires` closure
still selects the complete module roots and `DEPENDENCIES.md` remains included,
so a dependency revision invalidates affected inputs. No other
identity-platform input is excluded.

The `artifacts` field MUST contain the canonical path and exact SHA-256 digest
of a separately committed coordinator execution receipt. That receipt is part
of the gate result and MUST bind the absolute executable path, executable
version, executable-byte digest, exact argument vector, working directory,
sorted redacted environment identity, start/completion times, exit status,
exact stdout/stderr bytes with byte lengths and digests, raw-capture identity,
artifact-specific verifier execution, and final artifact digest. The
coordinator MUST resolve both blobs at their named commits and validate their
mutual path/digest binding before accepting the gate. Worker or producer
receipt authorship, a receipt reconstructed from a final artifact, or an
unbound command transcript is invalid.

The coordinator MUST derive affected packages and the complete reverse-
dependant closure from repository-native discovery at the tested revision and
bind the canonical discovery stdout and receipt to the gate record. Coverage
and mutation gates additionally require separate receipt-bound
`ACCEPTANCE_DISCOVERY=affected-packages` and
`ACCEPTANCE_DISCOVERY=viable-mutants` subprocesses. Mutation results MUST bind
the mutation tool's native machine-readable output and match every discovered
viable mutant exactly; worker manifests and producer-declared totals are not
authoritative.

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
  owner. It MUST have a matching newly committed `authorized` integrated-repair
  epoch in `PREFLIGHT_EVIDENCE.md`; that authorization commit's first parent is
  the exact current integration baseline and the row binds the prior integrated
  checkpoint, clean worker checkpoint, canonical goal, exact repair prompt, and
  reserved-root union. The later repair merge MUST replace both worker commit
  and integration checkpoint with descendants, and a separately committed
  matching `completed` row terminates only that epoch. A pre-integration
  conflict-recovery `in-progress` row MUST retain the
  same task, branch, worktree, assignment commit, and generation from its
  `blocked` parent; worker commit MAY be the clean coherent checkpoint while
  integration checkpoint and gate fingerprint remain `—`. Its exact authorized
  recovery baseline and complete-input root MUST be recorded in the matching
  `PREFLIGHT_EVIDENCE.md` conflict-recovery `authorized` row in the same
  transition commit. An immediate recovery-finalization `effective` row in
  `PREFLIGHT_EVIDENCE.md` MUST bind that
  authorization checkpoint. Before any package edit, the worker MUST merge the
  exact effective-authorization commit supplied by the coordinator and prove
  the authorization checkpoint has the recorded integration parent and worker
  checkpoint. A successful terminal row MUST bind the exact result worker and
  non-fast-forward integration commits.
- A retained-assignment `blocked` row MUST use `blocker:<safe-id>` as owner and
  retain task, branch, worktree, assignment commit, and generation. Before
  integration, worker commit MAY be `—` or the clean coherent stale-baseline
  commit and integration checkpoint and gate fingerprint MUST be `—`. A later
  `in-progress -> blocked` recovery conflict MAY replace that checkpoint with
  a new clean coherent descendant only when assignment identity and generation
  are unchanged, the old checkpoint is an ancestor of the replacement on the
  exact registered worker branch, and a new recovery epoch authorizes the
  replacement. After
  integration, worker commit and integration checkpoint MUST remain present;
  gate execution revision and fingerprint MUST remain exactly as inherited and
  MAY both be absent before verification. A partial pair is invalid. An abandoned assignment MUST transition to `ready`; a
  blocked row with cleared assignment fields is invalid.
- `implemented-unverified` and `verified` MUST have owner `—` and complete task,
  branch, worktree, assignment, worker commit, and integration checkpoint
  fields. In the initial `implemented-unverified` status commit, gate execution
  revision and gate fingerprint MUST both be `—`. They remain empty until a
  gate runs or is validly reused from an exact clean committed HEAD. The later
  transition to `verified` MUST atomically record the captured gate execution
  revision and input root and append its exact local gate evidence binding. A
  partial pair is invalid. `verified` MUST retain the complete pair.
  `implemented-unverified` external evidence MUST be
  `available`, `unavailable:<safe-profile-id>`, `not-needed`, or an attributable
  path. `verified` external evidence follows the stricter rule above.
  At every current clean committed execution `HEAD`, a verified row's bound
  evidence manifest MUST reproduce the exhaustive current manifest and gate
  root. A mismatch makes the row stale and requires one deterministic
  `verified -> implemented-unverified` transition for it and every verified
  reverse dependant whose complete root changed, clearing the stale gate pair
  before revalidation. Reachability from an ancestor gate revision is
  insufficient.

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
and external-evidence disposition finalization for `implemented-unverified` or
`verified`. Local gate revision, root, and record binding are added only by the
atomic `implemented-unverified -> verified` transition.
A blocked checkpoint finalization or later recovery-conflict replacement MUST
satisfy the exact descendant and assignment-identity rules above; a sibling,
rewritten, detached, or different-assignment commit is forbidden.
Every other same-status field change is forbidden.

Pre-spawn assignment authorization, the readiness-only first turn, post-spawn
runtime attestation, and the second-turn release are distinct. A worker commit
or integration checkpoint MUST NOT be accepted unless
the current unit/generation/task has exactly one runtime row bound to a
non-dash tool-visible agent ID and the requested
`gpt-5.6-sol`/`medium`/`none`/`false` settings plus exactly one valid release-
handshake row whose runtime and row commits are ancestors of, and precede,
worker-authored work. Early
worker mutation permanently invalidates the generation. The immutable assignment row
retains its assignment-time goal path; later canonical-goal repairs use only
the separately versioned integrated-repair table.

The corresponding spawn, readiness, release-preparation, activation, and return
captures MUST follow the base-pinned capture policy. Capture
IDs and turn ordinals are unique and contiguous for that worker; the return
event binds the exact ordered worker commits and returned tip. If platform
event verification is unavailable, assignment and integration are blocked.

Every ordinary `in-progress -> ready` or `blocked -> ready` abandonment MUST
append an assignment disposition row using an
`abandonment:<safe-id>` revision marker. It MUST satisfy the same
identity-bound canonical evidence, preserved-commit, clean worktree,
pre-removal HEAD, complete task-owned resource disposition, digest, and
coordinator-authorization rules as a dependency-revision disposition.
Generation increment plus cleared ledger fields alone MUST be rejected.

## Dependency revisions

Only the coordinator MAY record an inventory `Requires` change. A worker MUST
report the discovered dependency change and stop; it MUST NOT edit inventory,
goals, or the dependency graph. A behavior-changing dependency revision MUST
have a prior exact user semantic authorization; coordinator approval alone is
valid only for a mechanical correction already entailed by unchanged
user-authorized sources. Before accepting a revision, the coordinator
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
The first execution snapshot MAY omit prior fixtures only while every ledger
row is `initial` and every append-only execution-history table is empty. Every
later execution validation MUST receive the previous committed inventory,
ledger, `PREFLIGHT_EVIDENCE.md`, and `GOAL_MANIFEST.json` together through the
four `--previous-*-fixture` options. This requirement applies even when the
candidate does not intend to change history, so deleted or substituted rows
cannot masquerade as valid state.

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
| `dep-identity-apikey-organization-v1` | `identity/apikey` | `identity` | `identity`<br>`organization` | `identity/apikey`<br>`identity/apikey/postgres`<br>`identity/apikey/valkey`<br>`identity/http`<br>`identity/identitytest`<br>`identity/reference` | `reason:membership-authority` | `sha256:389d4330ca388af9829979a93d8011e2578b25a7a143db3ef83a95b7ddcbc219` | `coordinator` | `2026-08-11T18:09:01Z` |
| `dep-identity-apikey-postgres-organization-v1` | `identity/apikey/postgres` | `identity/apikey`<br>`identity/postgres` | `identity/apikey`<br>`identity/postgres`<br>`organization/postgres` | `identity/apikey/postgres`<br>`identity/identitytest`<br>`identity/reference` | `reason:membership-authority` | `sha256:3b6874b6af4d41ffffc127876c393bb6e2dfc2aad2255e981abfcebeab33b7b4` | `coordinator` | `2026-08-11T18:09:01Z` |
| `dep-identity-capability-v1` | `identity` | `primitive/authorization-identity-contracts` | `primitive/authorization-identity-contracts`<br>`primitive/capability-identity-contracts` | `identity`<br>`identity/anonymous`<br>`identity/anonymous/postgres`<br>`identity/apikey`<br>`identity/apikey/postgres`<br>`identity/apikey/valkey`<br>`identity/delivery/postgres`<br>`identity/email`<br>`identity/http`<br>`identity/i18n`<br>`identity/identitytest`<br>`identity/impersonation`<br>`identity/impersonation/postgres`<br>`identity/magiclink`<br>`identity/mfa`<br>`identity/mfa/postgres`<br>`identity/oauth`<br>`identity/oauth/onetap`<br>`identity/oauth/postgres`<br>`identity/oauth/providers`<br>`identity/oauth/proxy`<br>`identity/otp`<br>`identity/otp/postgres`<br>`identity/password`<br>`identity/password/postgres`<br>`identity/phone`<br>`identity/postgres`<br>`identity/reference`<br>`identity/risk`<br>`identity/risk/captcha`<br>`identity/risk/captcha/captchafox`<br>`identity/risk/captcha/hcaptcha`<br>`identity/risk/captcha/recaptcha`<br>`identity/risk/captcha/turnstile`<br>`identity/risk/hibp`<br>`identity/risk/postgres`<br>`identity/risk/valkey`<br>`identity/session`<br>`identity/session/postgres`<br>`identity/session/valkey`<br>`identity/username`<br>`oauth-server`<br>`oauth-server/device`<br>`oauth-server/oidc`<br>`oauth-server/postgres`<br>`organization`<br>`organization/postgres`<br>`passkey`<br>`passkey/postgres`<br>`scim`<br>`scim/organization`<br>`scim/postgres`<br>`sso`<br>`sso/domain-verification`<br>`sso/oauth2`<br>`sso/oidc`<br>`sso/postgres`<br>`sso/saml`<br>`webauthn/postgres` | `reason:capability-contract-closure` | `sha256:bbe08ea26f727e815e2cd440117a657e5446922e5fa3e3390b7c3fae3be3f16d` | `coordinator` | `2026-08-11T21:01:04Z` |
| `dep-identity-session-capability-v1` | `identity/session` | `identity`<br>`primitive/authorization-identity-contracts` | `identity`<br>`primitive/authorization-identity-contracts`<br>`primitive/capability-identity-contracts` | `identity/anonymous`<br>`identity/anonymous/postgres`<br>`identity/apikey`<br>`identity/apikey/postgres`<br>`identity/apikey/valkey`<br>`identity/email`<br>`identity/http`<br>`identity/identitytest`<br>`identity/impersonation`<br>`identity/impersonation/postgres`<br>`identity/magiclink`<br>`identity/mfa`<br>`identity/mfa/postgres`<br>`identity/oauth`<br>`identity/oauth/onetap`<br>`identity/oauth/postgres`<br>`identity/oauth/providers`<br>`identity/oauth/proxy`<br>`identity/otp`<br>`identity/otp/postgres`<br>`identity/password`<br>`identity/password/postgres`<br>`identity/phone`<br>`identity/reference`<br>`identity/session`<br>`identity/session/postgres`<br>`identity/session/valkey`<br>`identity/username`<br>`oauth-server`<br>`oauth-server/device`<br>`oauth-server/oidc`<br>`oauth-server/postgres`<br>`organization`<br>`organization/postgres`<br>`passkey`<br>`passkey/postgres`<br>`scim`<br>`scim/organization`<br>`scim/postgres`<br>`sso`<br>`sso/domain-verification`<br>`sso/oauth2`<br>`sso/oidc`<br>`sso/postgres`<br>`sso/saml` | `reason:capability-contract-closure` | `sha256:d3b00a3de02636435fd32c22aab528503b1cae3c184cd85f79910443de374a04` | `coordinator` | `2026-08-11T21:01:04Z` |
| `dep-oauth-server-capability-v1` | `oauth-server` | `identity`<br>`identity/session`<br>`identity/risk` | `identity`<br>`identity/session`<br>`identity/risk`<br>`primitive/capability-identity-contracts` | `identity/http`<br>`identity/identitytest`<br>`identity/reference`<br>`oauth-server`<br>`oauth-server/device`<br>`oauth-server/oidc`<br>`oauth-server/postgres` | `reason:capability-contract-closure` | `sha256:f69f51a29ab31722fe80d88783997488882d86904910cbc0bafd4c66d27f560e` | `coordinator` | `2026-08-11T21:01:04Z` |
| `dep-webauthn-capability-v1` | `webauthn` | `primitive/authentication-identity-contracts`<br>`primitive/identifier-identity-contracts` | `primitive/authentication-identity-contracts`<br>`primitive/capability-identity-contracts`<br>`primitive/identifier-identity-contracts` | `identity/http`<br>`identity/identitytest`<br>`identity/mfa`<br>`identity/mfa/postgres`<br>`identity/reference`<br>`passkey`<br>`passkey/postgres`<br>`webauthn`<br>`webauthn/postgres` | `reason:capability-contract-closure` | `sha256:925edf0db02abfaeadc24d9b12a27076aa6c4f9eef15ee21378393d7b945229a` | `coordinator` | `2026-08-11T21:01:04Z` |

## Dependency assignment dispositions

| Disposition ID | Revision IDs | Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Preservation proof | Preserved commit | Disposition evidence | Evidence digest | Resource dispositions | Recorded at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

This append-only table contains both dependency-revision dispositions and
ordinary abandonment dispositions. `Revision IDs` MUST name exact dependency
revision IDs or one `abandonment:<safe-id>` marker.

## Per-unit verification-gate evidence bindings

| Unit | Generation | Gate execution revision | Evidence path | Evidence record commit | Evidence blob digest | Bound at |
| --- | --- | --- | --- | --- | --- | --- |

This append-only table binds each coordinator-run per-unit verification-gate
evidence v2 record after the
record has been committed. The record cannot predict its own containing commit.
The binding MUST use the unit's current generation, exact gate execution
revision, the canonical path
`.ai/identity-platform/evidence/gates/<unit-with-slashes-replaced-by-hyphens>.json`,
the first committed record containing the exact canonical bytes, their SHA-256
digest, and an RFC3339 timestamp. The record commit MUST descend from the gate
execution revision and MUST be an ancestor of the later transition to
`verified`. A hash-shaped ledger value is not evidence: validation MUST resolve
the assignment, worker, checkpoint, gate-execution, and evidence-record commits
on current integration history, read the exact record blob from its bound
commit, match it byte-for-byte to the canonical local record, and validate that
record's input root and artifacts before accepting `verified`.

Resolving `artifacts` includes resolving the coordinator execution receipt and
proving its immutable process capture, repository discovery manifests, native
mutation output when selected, and independent artifact-specific verifier.
For protocol or security claims, the binding MUST also prove the selected
independent conformance suite or separately owned raw-observation verifier; the
producing package worker MUST NOT be that verifier.

These records prove current unit status and may unlock dependants. They are not
program-final-input evidence. After all implementations are integrated, the
coordinator MUST recompute all 67 roots at one clean committed revision and run
or validly reuse every changed per-unit result as part of the distinct
program-final-input gate defined by `ORCHESTRATOR_GOAL.md`.

## Unit execution ledger

| Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Worker commit | Integration checkpoint | Gate execution revision | Gate fingerprint | External evidence | Last transition |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `primitive/authentication-identity-contracts` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `primitive/authorization-identity-contracts` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `primitive/capability-identity-contracts` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `primitive/capability-postgres-identity-contracts` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `primitive/identifier-identity-contracts` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `primitive/password-secret-contracts` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/session` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/session/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/session/valkey` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/delivery` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/delivery/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/valkey` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/recaptcha` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/turnstile` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/hcaptcha` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/captchafox` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/risk/hibp` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/password` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/password/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/username` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/email` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/magiclink` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/otp` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/otp/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/phone` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/anonymous` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/anonymous/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/mfa` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/mfa/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `webauthn` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `webauthn/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `passkey` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `passkey/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/oauth` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/providers` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/onetap` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/proxy` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/apikey` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/apikey/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/apikey/valkey` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/impersonation` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/impersonation/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `organization` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `organization/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `sso` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `sso/domain-verification` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `sso/oidc` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `sso/oauth2` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `sso/saml` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `sso/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `scim` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `scim/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `scim/organization` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `oauth-server` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `oauth-server/oidc` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `oauth-server/device` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `oauth-server/postgres` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/i18n` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/http` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/reference` | 0 | — | — | — | — | — | — | — | — | — | initial |
| `identity/identitytest` | 0 | — | — | — | — | — | — | — | — | — | initial |
