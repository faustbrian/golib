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

Static `validate.rb` snapshot validation cannot prove prior-row version
increments, assignment-generation increments, commit reachability or ancestry,
or the allowed field differences in a same-status metadata finalization. Before
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
Static validation accepts the resulting post-initial `proposed` row shape but
does not authorize a status edge; the mandatory first-parent history procedure
MUST reject every return to `proposed` except `ready -> proposed` prerequisite
invalidation with retained generation and empty assignment/evidence fields.

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
