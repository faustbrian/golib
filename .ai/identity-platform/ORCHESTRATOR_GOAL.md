# Goal: Orchestrate the Complete Identity Platform

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Invocation

Give this file as the single goal to one coordinator agent:

```text
Execute /Users/brian/Developer/go-libraries/.ai/identity-platform/ORCHESTRATOR_GOAL.md
to completion. Act only as coordinator. Use gpt-5.6-sol subagents with medium
reasoning for package implementation, maximize safe DAG parallelism, commit
coherent verified batches locally, do not push, and stop only at the completion
or blocker conditions defined by the goal.
```

This invocation authorizes local conventional feature branches, isolated Git
worktrees, package commits, and local integration into a dedicated
`feature/identity-platform` branch. It does not authorize pushing, modifying
`main` or `develop`, force operations, rebasing, or weakening a goal or gate.

## Coordinator-only role

The coordinator MUST orchestrate and MUST NOT implement package production
code, package tests, package migrations, provider adapters, or package-local
documentation. It MAY:

- inspect all program and package artifacts;
- update coordinator-owned program state;
- create branches and worktrees through the required repository skills;
- spawn and supervise package workers;
- review and integrate verified worker commits locally;
- resolve mechanical program, catalog, and manifest conflicts;
- run integration, affected-module, reverse-dependant, and end-state gates;
- make coordinator-only commits.

The coordinator MUST send behavioral conflicts, public-contract changes, and
package-local defects back to the owning worker. It MUST NOT invent package
behavior while resolving integration.

## Required reading

Before scheduling a worker, the coordinator MUST read completely:

1. repository `AGENTS.md`;
2. `README.md`;
3. `PROGRAM.md`;
4. `COMMON_REQUIREMENTS.md`;
5. `END_STATE.md`;
6. `END_STATE_ACCEPTANCE.json`;
7. `REFERENCE_PROFILE.md`;
8. `BETTER_AUTH_PARITY.md`;
9. `PARITY_DISPOSITIONS.json`;
10. `API_OPERATIONS.md`;
11. `OPERATION_SEMANTICS.json`;
12. `UPSTREAM_DISPOSITIONS.md`;
13. `UPSTREAM_SURFACE.json`;
14. `PROTOCOL_BASELINES.md`;
15. `PROTOCOL_CONFORMANCE_MANIFEST.json`;
16. `SECURITY_EVENTS.md`;
17. `TRANSACTION_CONTRACT.md`;
18. `LIFECYCLE_CASCADES.md`;
19. `LIFECYCLE_CONSUMERS.md`;
20. `REFERENCE_CONFIGURATION.md`;
21. `CONFIGURATION_CATALOGS.json`;
22. `VERIFICATION_APPLICABILITY.json`;
23. `PREFLIGHT_EVIDENCE.md`;
24. `DEPENDENCIES.md`;
25. `INVENTORY.md`;
26. `EXECUTION_LEDGER.md`;
27. `WORKER_PROMPT.md`;
28. `GOAL_MANIFEST.json`;
29. the exact goal assigned to that worker.

The coordinator MUST treat `INVENTORY.md` as the authoritative state and
dependency record. The parity and end-state documents add acceptance
requirements; they do not weaken package goals.

## Worker model and isolation

Every implementation worker MUST be spawned with:

- model `gpt-5.6-sol`;
- reasoning effort `medium`;
- `fork_turns: "none"` so the model override is effective;
- a complete rendered copy of `WORKER_PROMPT.md`;
- exactly one inventory unit;
- exactly one absolute worktree path and branch;
- no authority to spawn subagents;
- no authority to edit coordinator-owned files or another package.

The coordinator MUST use all safely available concurrency slots for independent
`ready` units while retaining one slot for itself. It MUST NOT create a new
assignment owner for a `proposed`, `blocked`, `in-progress`, or
`implemented-unverified` unit. It MAY resume or reinstantiate the same recorded
owner on the same branch/generation for repair after proving recoverability and
re-rendering the current assignment.

## Branch and worktree topology

The coordinator MUST use the repository branch and worktree skills.

1. Resolve and record the exact committed `main` revision. Create
   `feature/identity-platform` from that revision in an isolated integration
   worktree. Uncommitted files in another worktree MUST NOT enter the program.
2. Create every worker branch from the recorded `main` revision, as repository
   policy requires.
3. Commit the assignment state on the integration branch: mark the unit
   `in-progress`, record worker task, branch and worktree in both state files,
   set ledger `Assignment commit` to `pending`, and record the exact verified
   prerequisite revisions for rendering. This is the assignment-state commit.
4. In an immediate ledger-only finalization commit, replace `pending` with the
   now-known assignment-state commit hash. Merge that finalization commit into
   only this newly assigned unit's worker branch, including when the unit is a
   dependency-free root. Render the complete prompt against that exact
   finalization commit, commit its exact bytes and finalized attestation row in
   a third coordinator commit, and merge that attestation commit into only the
   assigned worker branch. The rendered `assignment-commit` MUST be the
   assignment-state commit; the rendered `integration-commit` MUST be the
   ledger-only finalization commit. A worker MUST NOT start before all three
   commits exist, the attestation is finalized, or from an inventory that still
   says `ready`.
5. For dependent work, the assignment commit MUST also contain every verified
   prerequisite implementation and coordinator registration commit.
6. Integrate a returned worker with `git merge --no-ff` from its named branch.
   Before starting the merge, prove the integration worktree is clean and
   record its exact `HEAD` as the abort target.
   The coordinator MUST NOT cherry-pick, squash or recreate the worker change.
   On a package-owned conflict, the coordinator MUST record the conflicting
   paths and merge heads without secret-bearing content, abort the integration
   merge, and prove the integration worktree is clean at its pre-merge commit.
   It MUST atomically transition the unit `in-progress -> blocked` with
   `blocker:integration-conflict`. After the worker reports a clean coherent
   checkpoint, the coordinator MUST append an `authorized` conflict-recovery
   row to `PREFLIGHT_EVIDENCE.md` containing the unit, generation, exact current
   integration commit as the resume commit's first-parent pre-resume baseline,
   worker checkpoint, and sanitized conflict-evidence path;
   in that same commit it MUST transition `blocked -> in-progress` for the
   retained assignment. Each authorization MUST use the next recovery epoch
   for that unit and generation; a completed earlier epoch does not forbid a
   later epoch after the retained assignment re-enters `blocked`. If the new
   clean checkpoint replaces a prior checkpoint, the prior checkpoint MUST be
   its ancestor on the exact registered worker branch and every assignment
   identity field MUST remain unchanged. It MUST then give the same worker the
   exact resulting resume/authorization commit. Before any package edit, the worker MUST prove
   that commit's first parent is the row's integration baseline, prove the row's
   checkpoint is reachable from its clean worker branch, and merge the exact
   resume/authorization commit into that branch. The worker resolves only
   assigned-package behavior and returns a new reviewed commit;
   coordinator-owned conflicts are resolved only by the coordinator. The
   coordinator then retries the non-fast-forward integration merge. Other safe work MAY advance the
   integration branch during repair, but before retry the coordinator MUST
   compare the new `HEAD` with the pinned recovery baseline. If any complete
   input for the repaired unit changed, it MUST repeat baseline refresh and
   invalidated evidence; otherwise it MAY retry against the advanced `HEAD`.
   It MUST NOT leave the integration worktree in a conflicted state while
   scheduling or integrating another unit.
7. Never rebase a worker or integration branch.
8. Never reuse a worktree for two concurrent workers.
9. Never run package commands outside the assigned worker worktree.
10. Never switch a worktree containing uncommitted changes.

A package is not a satisfied prerequisite merely because its worker branch
contains code. Its integrated inventory state MUST be `verified`.

## Coordinator-owned state and shared files

Only the coordinator MAY edit:

- files directly under `.ai/identity-platform/`;
- planning goals under `.ai/identity-platform/goals/`;
- root `modules.json`, `packages.json`, `go.work`, and `go.work.sum`;
- generated package catalogs and dependency documents;
- root goal traceability and integration evidence.

Workers MUST limit edits to their canonical package directory. This avoids
parallel conflicts in shared manifests and status files.

`INVENTORY.md` owns status, owner/blocker and dependencies. `EXECUTION_LEDGER.md` owns
assignment generation, worker task, branch/worktree, assignment/worker/
integration commits, gate fingerprint, external-evidence pointer and transition
time. Every inventory status or owner/blocker change MUST update the
corresponding ledger mirror in the same coordinator commit. Newly knowable
prior commit hashes and evidence pointers MAY use the explicitly defined
ledger-only finalization commit. The ledger MUST contain no credential, token,
provider body or secret-shaped value.

## Transition history validation

Before every inventory state commit or ledger-only finalization, the
coordinator MUST execute the complete first-parent transition-check procedure
defined in `EXECUTION_LEDGER.md`. It MUST validate the proposed snapshot before
commit and the actual commit immediately afterward. Static `validate.rb`
success cannot prove prior-row versions, assignment generations, allowed
same-status field changes, commit ancestry, or live worktree uniqueness and
MUST NOT substitute for this history check. A failed or unavailable pre-commit
or post-commit half blocks worker spawn, merge, gate execution, and the next
state transition until corrected.

## Dependency revision ownership

Only the coordinator MAY approve and record dependency revisions. When a worker
reports a missing, obsolete, or incorrect dependency, the coordinator MUST
validate the proposed goals and complete DAG, reject cycles, append the exact
`EXECUTION_LEDGER.md` dependency-revision record, and atomically reset the
changed unit plus its full prior/current reverse-dependent closure under the
generation, evidence invalidation, readiness, and unchanged-row rules defined
there. Prior/current dependency lists MUST retain their exact inventory order.
Before clearing an affected active assignment, the coordinator MUST first
commit its transition to `blocked`, obtain a clean checkpoint/baseline or an
attributable safe-abandonment proof, reconcile every registered worker resource,
write the identity-bound canonical disposition evidence, and append the exact
assignment-disposition row required by the ledger. A
retained worktree MUST remain registered and clean at that proof commit; every
safe-abandoned resource MUST be removed, and every removed worktree or other
resource MUST have exact cleanup evidence plus any required captured
pre-removal clean state. Before removing any worker worktree, including under
safe abandonment, the coordinator MUST prove it is clean at the exact preserved
worker checkpoint or assignment baseline. Safe abandonment MUST NOT authorize
discarding dirty or unintegrated work. The
coordinator MUST NOT authorize a worker to edit `Requires`, goal dependency
metadata, or the dependency graph.

## Program preflight

Before the first assignment, the coordinator MUST:

1. run `validate.rb` for planning-tree structure, complete and commit
   `PREFLIGHT_EVIDENCE.md`, then run `validate.rb --execution` and stop on any
   strict pre-assignment failure;
2. record the committed base revision and its exact identity-platform Git tree
   object and digest; prove the base/input commits exist, the registered
   integration branch and worktree share one HEAD, and that HEAD descends from
   the exact base;
3. inventory required Go and tool versions, PostgreSQL and Valkey profiles,
   mutation, fuzz, and race tooling, browser and interoperability harnesses,
   and external provider evidence requirements;
4. classify every required credential or external environment as available,
   unavailable, or not yet needed without printing secret values;
5. record which acceptance claims each unavailable dependency can block; and
6. continue all independent local work even when an external evidence lane is
   unavailable.

The coordinator MUST write these results to `PREFLIGHT_EVIDENCE.md`, including
the committed base, integration workspace, tool/environment identities,
primitive API and gate fingerprints, external claim classifications, and the
task-owned resource registry. It MUST commit the completed preflight record on
the integration branch before creating the first assignment-state commit.
Chat, an uncommitted file, or a ledger `available` value is not durable
preflight evidence.

The coordinator MUST also derive the union of every goal's `Consumes existing
primitives` entries, resolve each to its registered module/package, and verify
current public API and gate fingerprints before assigning its first consumer.
Existing evidence MAY be reused only under repository fingerprint rules. A
missing or incompatible primitive contract MUST block the exact consumers and
be reported as a required dependency-goal change; neither coordinator nor
package worker may silently patch or copy the primitive outside the inventory.

Missing credentials MUST NOT be replaced with fake interoperability evidence.
The coordinator MUST request credentials only when the corresponding unit has
no remaining credential-independent work or when the missing evidence would
otherwise stop the next dependency frontier.

When a package worker returns a completed package-local commit, the coordinator
MUST:

1. independently inspect the complete package diff and evidence;
2. verify the returned tip is reachable from the assigned worker branch, the
   rendered integration baseline is its ancestor, and the complete baseline-to-
   tip diff changes only the assigned module root after subtracting every other
   inventory module root nested beneath it; inspect every worker-authored commit
   in that range rather than assuming the tip is the only package commit;
3. integrate the branch with the required non-fast-forward merge;
4. move the planning goal to the unit's declared canonical goal path;
5. register the module and packages in every root manifest;
6. regenerate only required catalogs through repository tooling;
7. record the already-known worker merge commit as the integration checkpoint,
   mark the unit `implemented-unverified`, clear its active inventory owner,
   and commit this recoverable state transition with gate execution revision
   and fingerprint both `—`. The resulting commit is the distinct gate
   execution revision; immediately follow it with one ledger-only
   `implemented-unverified -> implemented-unverified` finalization that records
   that now-known revision and its exhaustive sorted gate-input manifest root.
   The integration checkpoint MUST remain the earlier worker merge commit;
8. run structural validation, the package gate and only input-invalidated
   reverse-dependant gates from the integration worktree;
9. for every confirmed package-local defect, commit an
   `implemented-unverified -> in-progress` transition that restores the same
   worker task as owner and retains its branch, worktree, assignment, and
   generation; merge that exact transition commit into the clean worker branch,
   re-render the repair prompt with the canonical goal path and current
   baseline, and return the finding to that worker;
10. after every final-input gate passes, commit each attributable evidence
    record against the captured gate execution revision, then commit the
    ledger/status transition that binds its path and exact evidence-record
    commit. Mark the unit `verified` only in that later transition; an evidence
    record MUST NOT predict or embed the commit that contains itself; and
11. remove the worker's task-owned worktree and disposable resources only after
    the unit is `verified` or the assignment is safely abandoned. An
    `implemented-unverified` unit retains its clean repair worktree unless the
    coordinator has proved it can recreate that exact branch, assignment, and
    evidence state before repair.

The worker's package-local checks are pre-integration evidence. Root manifest,
catalog, `make check`, reverse-dependant and end-state gates are coordinator
checks and MUST NOT be delegated back merely because workers cannot edit shared
files.

## Scheduling algorithm

The coordinator MUST repeat this loop until completion:

1. Re-read inventory state from the integration branch.
2. For each `proposed` unit, check whether every `Requires` unit is `verified`
   and every additional start gate passes. In particular, exclude any unit
   whose primitive row is `failed`, `blocked`, or `stale`, even when its DAG
   prerequisites are all verified.
3. Re-evaluate every `blocked` unit. When its recorded blocker is resolved,
   return a unit retaining a recoverable assignment to `in-progress` under that
   exact task, branch, worktree, and generation. Return it to `ready` only when
   the prior assignment is proven safely abandoned; in that transition,
   increment generation and clear active assignment fields.
4. Mark every newly eligible unit `ready` and commit the complete newly ready
   frontier before assigning any of it. A unit MUST NOT jump directly from
   `proposed` to `in-progress` in durable history.
5. Fill available worker slots with distinct `ready` units.
6. Before spawning, complete the assignment-state/finalization protocol and
   render both exact commits plus generation into its worker prompt.
7. Supervise active workers without duplicating their work.
8. On worker return, classify the result:
   - package-local requirements complete and focused gates pass:
     integration review;
   - implementation complete but evidence missing: integrate it through the
     checkpoint sequence and retain `implemented-unverified` until the missing
     evidence is proven;
   - correct work cannot continue: use inventory `blocked` with an explicit
     safe blocker while preserving any recoverable assignment; this inventory
     state is distinct from marking the coordinator's system goal blocked;
   - confirmed defect:
     return the exact finding to the same worker.
9. Integrate successful units one at a time using the checkpoint sequence.
10. Recompute newly eligible units immediately after each verified integration.
11. Continue unrelated ready work when one provider or environment is blocked.

Initial scheduling MUST start only the currently ready roots:
`identity`, `identity/delivery`, and `webauthn`.

## Interruption and worker-failure recovery

On restart or context compaction, the coordinator MUST reconstruct state from
the integration branch, inventory, execution ledger and Git
reachability before spawning anything. It MUST inspect whether each recorded
worker is live, returned, interrupted or missing. It MUST NOT create a second
owner for an in-progress unit.

An interrupted worker with a clean recoverable branch SHOULD be resumed with
the same assignment. An unrecoverable worker MUST have its branch and commits
inspected before the coordinator returns the unit to `ready`. A unit may return
to `ready` only in a committed coordinator transition that proves no
unintegrated package work is being discarded. The coordinator MUST preserve
completed evidence checkpoints across restarts and MUST invalidate only units
whose complete gate-input fingerprint changed.

An inventory `blocked` transition is program state, not an `update_goal` call.
The coordinator MUST follow repository blocked-audit rules only when deciding
whether its own persistent system goal may stop as blocked.

When corrected work changes a verified unit's complete inputs, the coordinator
MUST recompute transitive fingerprints and atomically demote every verified
dependant whose complete fingerprint changed to `implemented-unverified`.
Every unassigned `ready` transitive dependant whose start gate is no longer
satisfied MUST return to `proposed` in that same commit, retaining its
generation and empty assignment/evidence fields. This prerequisite-invalidation
transition is not abandonment and MUST NOT increment generation.
Active dependent workers on the changed baseline MUST pause and atomically
transition to `blocked` with a safe stale-baseline blocker; their commits MUST
NOT be integrated from the stale baseline. After the prerequisite is
reverified, the coordinator MUST perform the pause and baseline-refresh
handshake in `WORKER_PROMPT.md`. It MUST NOT merge into a dirty worker
worktree. Once the worker has a coherent clean checkpoint, the coordinator MUST
commit `blocked -> in-progress` for the same assignment and an `authorized`
recovery row that pins the resume commit's first-parent pre-resume integration
baseline and the worker checkpoint. It MUST give the worker the exact resulting
resume/authorization commit. Before any package edit, the worker MUST verify
that commit and both ancestry relationships, merge that exact commit, re-render
the prompt with the same generation and current goal path, and require all
invalidated acceptance evidence again. If the worker cannot reach a coherent clean
checkpoint without discarding work, it remains inventory `blocked` while other
lanes continue. A conflict or
incompatible public contract returns to the affected owner; it MUST NOT be
resolved by accepting stale evidence.

## Worker acceptance

A worker return MUST include:

- inventory unit and canonical module;
- branch, worktree, assignment generation, assignment-state commit, current
  worker-branch tip, and ordered worker-authored commit hashes;
- exact package-local paths changed;
- expected behavior and executable acceptance mapping;
- focused red-green evidence for behavioral changes;
- exact package checks and complete outcomes;
- coverage, mutation, race, fuzz, interoperability, documentation, API,
  security, supply-chain, and benchmark outcomes required by the goal;
- skipped or unavailable evidence;
- review findings and their resolution;
- coordinator-owned registration work still required.

The coordinator MUST reject claims based on partial logs, stale fingerprints,
aggregate success without attributable package results, source-text assertions
for runtime behavior, provider fakes presented as live interoperability, or
unreviewed mutation exclusions.

The coordinator MUST apply the repository's requesting-code-review workflow to
each integrated package diff and again to the complete final integrated diff.
Review must cover the assigned goal, parity rows, reference-profile values,
end-state consumers, public API, dependency direction, migration and security
boundaries. Every confirmed finding returns to the owning worker; coordinator
review is not authority to implement the correction.

## Better Auth parity control

`BETTER_AUTH_PARITY.md` is pinned to one upstream Better Auth revision.
Every in-scope parity row MUST name an owner and executable acceptance
contract. The coordinator MUST NOT mark the program complete when a row is
missing, marked partial, depends on undocumented application glue, or has no
verified owner.

If upstream Better Auth changes during execution, the coordinator MUST NOT
silently expand scope. It MUST finish the pinned baseline and record a new
follow-up parity audit unless the user explicitly updates the baseline.

Product exclusions remain exclusions. Non-capability, client-surface,
deployment-profile, and security-divergence dispositions MUST retain their
exact category. The coordinator MUST NOT implement billing/payment plugins,
SIWE, MCP authentication, agent authentication, lead-tracking integrations,
CLI scaffolding, community catalogs, personal SCIM, database-less OAuth token
cookies, JavaScript framework clients, or additional database engines merely
to increase a parity percentage.

## End-state integration proof

After every unit is verified, the coordinator MUST execute the complete
`END_STATE.md` journeys through `identity/reference` against the final
integrated branch. This
includes real PostgreSQL and Valkey profiles, supported provider
interoperability, standard `net/http` endpoints, cookies, OpenAPI 3.1.1,
multi-account sessions, administration, organizations, SSO, SCIM, OAuth/OIDC
provider behavior, device authorization, abuse controls, localization, and the
test harness.

The coordinator MUST run only input-invalidated package and reverse-dependant
gates during development. At final completion it MUST run every affected
release gate required by the repository and persist evidence under the
repository evidence model.

Every unit and final acceptance gate MUST use the complete-input manifest and
root contract in `END_STATE_ACCEPTANCE.json`; a digest over only coordinator
manifests is not sufficient. The exact tested revision, gate execution
revision, and nullable revalidation revision MUST be captured before execution.
A revalidation revision MUST be null without reuse; for reuse it MUST equal the
gate execution revision and the complete input root MUST validate identically
at both committed revisions. Report bytes are committed only
after execution, and the later status/finalization commit binds their paths,
blob digests, and evidence-record commits without requiring a record to contain
its own commit hash.

## Resource cleanup

The coordinator MUST maintain the task-owned resource registry in
`PREFLIGHT_EVIDENCE.md` throughout execution. After each assignment is
integrated, abandoned, or proven unrecoverable, it MUST remove every worker
worktree, disposable Go cache, temporary directory, process, container, image,
volume, database payload, browser artifact, and provider fixture that is no
longer required for an active recovery or attributable evidence record. It MUST
resolve each exact target before removal and MUST NOT delete branches or
user-owned resources without authority.

Before a successful final report, the coordinator MUST remove every worker
worktree and all remaining disposable resources and mark those registry entries
removed. In the final coordinator commit it MUST mark the integration worktree
`removal-pending-after-final-commit`, retain the local integration branch and
durable evidence, then leave that worktree, remove it from a different safe
repository location, and prove its path and registration are absent before
reporting. This two-phase rule avoids claiming that the worktree containing the
final commit was already removed.
Before a blocked stop, it MUST perform the same sweep except for resources
strictly required to resume a recorded assignment; every retained resource MUST
name its unit, blocker, owner, exact path or safe resource ID, and cleanup
trigger in the final report. An interruption MUST begin with the same registry
reconciliation before new resources are created.

## Stop conditions

The coordinator MUST continue while any safe in-scope work is ready. It MAY
stop only when:

1. every inventory unit is `verified`, every in-scope parity row is proven,
   every end-state journey passes, and final review has no unresolved finding;
   or
2. no unit can make progress because required user authority, credentials,
   external infrastructure, or a product decision is unavailable.

A provider credential gap blocks only claims requiring that provider. It MUST
NOT stop unrelated packages.

## Final report

The final report MUST state:

- integration branch and final commit;
- every verified, implemented-unverified, and blocked unit;
- parity baseline revision and row outcomes;
- end-state journey outcomes;
- exact final gates and whether they passed, failed, or were unavailable;
- provider and deployment boundaries not proven;
- confirmation that no push occurred;
- the precise user action required for every remaining blocker.

The coordinator MUST NOT describe the program as complete, ready, equivalent,
or production-safe while any required row, journey, review, or gate remains
unproved.
