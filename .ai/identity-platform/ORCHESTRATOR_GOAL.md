# Goal: Orchestrate the Complete Identity Platform

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

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
6. `REFERENCE_PROFILE.md`;
7. `BETTER_AUTH_PARITY.md`;
8. `DEPENDENCIES.md`;
9. `INVENTORY.md`;
10. `EXECUTION_LEDGER.md`;
11. `WORKER_PROMPT.md`;
12. the exact goal assigned to that worker.

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
   every worker branch, including dependency-free roots. The rendered
   `assignment-commit` MUST be the assignment-state commit; the rendered
   `integration-commit` MUST be the finalization commit now at the worker
   baseline. A worker MUST NOT start before both commits exist or from an
   inventory that still says `ready`.
5. For dependent work, the assignment commit MUST also contain every verified
   prerequisite implementation and coordinator registration commit.
6. Integrate a returned worker with `git merge --no-ff` from its named branch.
   The coordinator MUST NOT cherry-pick, squash or recreate the worker change.
   A merge conflict in package-owned behavior MUST be returned to the worker;
   only coordinator-owned mechanical files may be resolved by the coordinator.
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

## Program preflight

Before the first assignment, the coordinator MUST:

1. run `validate.rb` and stop on any program-structure failure;
2. record the committed base revision and prove the identity-platform tree is
   unchanged from that revision;
3. inventory required Go/tool versions, PostgreSQL and Valkey profiles,
   mutation/fuzz/race tooling, browser/interoperability harnesses, and external
   provider evidence requirements;
4. classify every required credential or external environment as available,
   unavailable, or not yet needed without printing secret values;
5. record which acceptance claims each unavailable dependency can block; and
6. continue all independent local work even when an external evidence lane is
   unavailable.

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
2. verify the returned commit is reachable only from the assigned worker branch
   and changes only the assigned module root after subtracting every other
   inventory module root nested beneath it;
3. integrate the branch with the required non-fast-forward merge;
4. move the planning goal to the unit's declared canonical goal path;
5. register the module and packages in every root manifest;
6. regenerate only required catalogs through repository tooling;
7. record the already-known worker merge commit as the integration checkpoint,
   mark the unit `implemented-unverified`, record the pre-gate input
   fingerprint, and commit this recoverable state transition;
8. run structural validation, the package gate and only input-invalidated
   reverse-dependant gates from the integration worktree;
9. return every confirmed package-local defect to the same worker after
   merging the latest integration checkpoint into that worker branch;
10. mark the unit `verified` only after every final-input gate passes and
    commit that status/evidence transition; and
11. remove the worker's task-owned worktree and disposable resources only
    after its commits are integrated and recoverable.

The worker's package-local checks are pre-integration evidence. Root manifest,
catalog, `make check`, reverse-dependant and end-state gates are coordinator
checks and MUST NOT be delegated back merely because workers cannot edit shared
files.

## Scheduling algorithm

The coordinator MUST repeat this loop until completion:

1. Re-read inventory state from the integration branch.
2. For each `proposed` unit, check whether every `Requires` unit is
   `verified`.
3. Re-evaluate every `blocked` unit. When its recorded blocker is resolved,
   return a pre-integration unit to `ready`, or an integrated unit to
   `in-progress` under its same assignment, in a committed transition.
   Increment generation and clear active assignment fields only when the prior
   assignment is proven safely abandoned.
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
   - implementation complete but evidence missing:
     `implemented-unverified`;
   - correct work cannot continue:
     retain `blocked` only under repository blocked-status rules;
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

When corrected work changes a verified unit's complete inputs, the coordinator
MUST recompute transitive fingerprints and atomically demote every verified
dependant whose complete fingerprint changed to `implemented-unverified`.
Active dependent workers on the changed baseline MUST pause and atomically
transition to `blocked` with a safe stale-baseline blocker; their commits MUST
NOT be integrated from the stale baseline. After the prerequisite is
reverified, the coordinator MUST merge the new integration baseline into each
paused worker branch, transition it back to `in-progress`, re-render its prompt
with the same generation and current goal path, and require all invalidated
acceptance evidence again. A conflict or
incompatible public contract returns to the affected owner; it MUST NOT be
resolved by accepting stale evidence.

## Worker acceptance

A worker return MUST include:

- inventory unit and canonical module;
- branch, worktree, assignment generation, assignment-state commit and worker
  commit hash;
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

Explicit exclusions remain exclusions. The coordinator MUST NOT implement
billing/payment plugins, SIWE, MCP authentication, agent authentication,
lead-tracking integrations, JavaScript framework clients, or additional
database engines merely to increase a parity percentage.

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
