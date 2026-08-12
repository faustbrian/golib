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

Coordinator custody is not semantic authority. The coordinator MUST follow the
product-authority boundary in `PROGRAM.md` and MUST NOT self-authorize a change
to scope, package ownership, behavior semantics, public API, parity or protocol
disposition, acceptance claims or artifacts, reference behavior, shared
security/lifecycle/transaction contracts, or any goal body. Mechanical repair
is limited to byte-preserving moves and reproducible derived output from
unchanged user-authorized semantic sources. Every other semantic repair waits
for an exact durable `user:<safe-approval-id>` authorization while independent
lanes continue. To request it, the coordinator MUST first commit the canonical
proposal manifest and complete proposed source blobs required by
`PREFLIGHT_EVIDENCE.md`, then present the exact standalone authorization line;
it MUST NOT edit an authoritative semantic source before the user returns that
line byte-for-byte.

## Required reading

Before scheduling a worker, the coordinator MUST read completely:

1. repository `AGENTS.md`;
2. `README.md`;
3. `PROGRAM.md`;
4. `COMMON_REQUIREMENTS.md`;
5. `END_STATE.md`;
6. `END_STATE_ACCEPTANCE.json`;
7. `ACCEPTANCE_ARTIFACTS.json`;
8. `REFERENCE_PROFILE.md`;
9. `BETTER_AUTH_PARITY.md`;
10. `PARITY_DISPOSITIONS.json`;
11. `API_OPERATIONS.md`;
12. `OPERATION_SEMANTICS.json`;
13. `PUBLIC_CONTRACTS.json`;
14. `public_contracts.rb`;
15. `UPSTREAM_DISPOSITIONS.md`;
16. `UPSTREAM_SURFACE.json`;
17. `PROTOCOL_BASELINES.md`;
18. `PROTOCOL_CONFORMANCE_MANIFEST.json`;
19. `SECURITY_EVENTS.md`;
20. `TRANSACTION_CONTRACT.md`;
21. `LIFECYCLE_CASCADES.md`;
22. `LIFECYCLE_CONSUMERS.md`;
23. `REFERENCE_CONFIGURATION.md`;
24. `CONFIGURATION_CATALOGS.json`;
25. `VERIFICATION_APPLICABILITY.json`;
26. `PREFLIGHT_EVIDENCE.md`;
27. `DEPENDENCIES.md`;
28. `INVENTORY.md`;
29. `EXECUTION_LEDGER.md`;
30. `WORKER_PROMPT.md`;
31. `GOAL_MANIFEST.json`;
32. the exact goal assigned to that worker.

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
   finalization commit, commit its exact bytes and authorized assignment row in
   a third coordinator commit, and merge that attestation commit into only the
   assigned worker branch. The rendered `assignment-commit` MUST be the
   assignment-state commit; the rendered `integration-commit` MUST be the
   ledger-only finalization commit. A worker MUST NOT start before all three
   commits exist, the authorization is finalized, or from an inventory that still
   says `ready`. Spawn is the first turn of a mandatory two-turn release
   handshake. The initial worker turn may perform only read-only identity and
   workspace checks and MUST return the exact readiness receipt
   `READY-AND-WAITING <unit> g<generation> <assignment-authorization-checkpoint>`;
   the final value is the worker branch's clean current `HEAD`, proven to be
   the first commit whose tree contains the exact rendered prompt and
   assignment-authorization row. The worker MUST NOT edit,
   commit, merge, test, build, generate, or otherwise begin package work.
   Immediately after that receipt, record a distinct coordinator capture bound
   to the tool-visible worker task and agent ID plus the requested model,
   reasoning, fork-turn and subagent settings; commit it and merge that exact
   commit into the worker branch. Send the exact non-authorizing
   `PREPARE-RELEASE` directive and require the worker to remain waiting. Commit
   the exact coordinator send invocation/result, which does not prove delivery, and durable
   release-handshake row, then merge that row commit into the worker branch.
   Only then send `ACTIVATE <unit> g<generation> release=<row-commit>` to the
   same worker. The worker may begin package work only after verifying the
   capture commit, assignment checkpoint, release row, activation bytes, and
   ancestry. The validator proves the committed row boundary; the worker-side
   activation instruction governs when work begins, without claiming delivery
   proof. A spawn request, an early worker action, a
   readiness receipt with different bytes, or a coordinator message sent before
   the committed runtime checkpoint is not release authority.
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
   integration commit as the authorization commit's first-parent baseline,
   worker checkpoint, canonical complete-input root for the repaired unit at
   that baseline, and sanitized conflict-evidence path; in that same
   authorization commit it MUST transition `blocked -> in-progress` for the
   retained assignment. Because a commit cannot contain its own hash, an
   immediate recovery-finalization `effective` row in `PREFLIGHT_EVIDENCE.md`
   MUST bind the authorization
   checkpoint. The effective checkpoint, not the uncommitted row or
   authorization commit alone, is the resume authority. Each authorization MUST use the next recovery epoch
   for that unit and generation; a completed earlier epoch does not forbid a
   later epoch after the retained assignment re-enters `blocked`. If the new
   clean checkpoint replaces a prior checkpoint, the prior checkpoint MUST be
   its ancestor on the exact registered worker branch and every assignment
   identity field MUST remain unchanged. It MUST then give the same worker the
   exact resulting effective-authorization commit. Before any package edit, the
   worker MUST prove the authorization checkpoint's first parent is the row's
   integration baseline, prove the row's worker checkpoint is reachable from
   its clean worker branch, and merge the exact effective-authorization commit
   into that branch. The worker resolves only
   assigned-package behavior and returns a new reviewed commit;
   coordinator-owned conflicts are resolved only by the coordinator. The
   coordinator then retries the non-fast-forward integration merge. Other safe work MAY advance the
   integration branch during repair, but before retry the coordinator MUST
   compare the new `HEAD` with the pinned recovery baseline by recomputing the
   repaired unit's canonical complete-input manifest and root at both commits.
   If the roots are byte-identical, the effective authorization remains valid
   and the coordinator MAY retry against the advanced `HEAD`; commit ancestry
   or an unchanged path list alone is insufficient. If the roots differ, it
   MUST append `superseded`, refresh the worker baseline and invalidated
   evidence, and establish the next recovery epoch before retry. After a
   successful retry, a later `completed` row MUST bind the exact returned result
   worker commit and exact non-fast-forward result integration checkpoint.
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

This custody follows each goal from its planning path to its canonical package
path. A moved goal remains a coordinator-owned control file even when it is
nested beneath the assigned module directory; it is removed from worker scope,
reserved from worker-authored diffs, and changed only through the user semantic
authorization lifecycle. Moving it MUST preserve its bytes and digest.

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

Any goal-body digest change MUST use the append-only authorization lifecycle in
`PREFLIGHT_EVIDENCE.md` and the product-authority boundary in `PROGRAM.md`.
Exact user authorization is recorded while the prior digest is still current;
the later goal/manifest commit records the matching applied terminal row.
Editing a goal and its digest together without that prior user authorization is
invalid and blocks assignment or continuation. The coordinator cannot approve
the change merely because it has custody of the file.

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

Every prior/current transition validation MUST supply the previous committed
inventory, ledger, execution preflight, and goal manifest together. Omitting
the previous execution or goal-manifest fixture is invalid even when the
candidate does not intend to change a goal, because the validator must prove
that no unauthorized goal or recovery-history transition entered the same
snapshot.

Immediately after every coordinator state or finalization commit, and before
worker spawn, merge, gate execution, restart recovery, or final acceptance, the
coordinator MUST run `validate.rb --execution --clean-integration` from the
registered integration worktree with the required previous fixture quartet
after the initial snapshot. Pre-commit proposed-snapshot validation MUST
use fixture inputs without `--clean-integration`, so the candidate can be
checked without pretending its uncommitted bytes are an executable checkpoint.

## Dependency revision ownership

Only the coordinator MAY record dependency revisions. It MAY approve only a
mechanical correction already entailed by unchanged user-authorized semantic
sources; any behavioral dependency change requires the exact prior user
semantic authorization in `PREFLIGHT_EVIDENCE.md`. When a worker
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

1. prove `.ai/identity-platform/PLATFORM_EVIDENCE_TRUST.json` exists on the
   recorded committed `main` base and validates as the immutable evidence-
   capture policy. Despite its retained filename, it defines no cryptographic
   platform trust: the collaboration API exports no signatures or immutable
   delivery receipts. It instead pins the honest trust limitations and required
   Git custody, exact-byte digest, independent-worker, coordinator-verification,
   and explicit-user-authority controls. An absent, integration-authored,
   replaced, or invalid policy blocks assignment;
2. run `validate.rb` for planning-tree structure, complete and commit
   `PREFLIGHT_EVIDENCE.md`, then run
   `validate.rb --execution --clean-integration` and stop on any strict
   pre-assignment failure;
3. record the committed base revision and its exact identity-platform Git tree
   object and digest; prove the base/input commits exist, the registered
   integration branch and worktree share one HEAD, and that HEAD descends from
   the exact base;
4. inventory required Go and tool versions, PostgreSQL and Valkey profiles,
   mutation, fuzz, and race tooling, browser and interoperability harnesses,
   and external provider evidence requirements;
5. classify every required credential or external environment as available,
   unavailable, or not yet needed without printing secret values;
6. record which acceptance claims each unavailable dependency can block; and
7. continue all independent local work even when an external evidence lane is
   unavailable.

The coordinator MUST write these results to `PREFLIGHT_EVIDENCE.md`, including
the committed base, integration workspace, tool/environment identities,
primitive API and gate fingerprints, external claim classifications, and the
task-owned resource registry. It MUST commit the completed preflight record on
the integration branch before creating the first assignment-state commit.
Chat, an uncommitted file, or a ledger `available` value is not durable
preflight evidence.

## Commit custody and laundering prevention

Before spawn, merge, gate execution, every state transition, restart recovery,
and final acceptance, the coordinator MUST audit every integration-branch
first-parent commit from the recorded base through current `HEAD`. Each commit
must match exactly one registered class and its exact parent and path envelope:

- assignment state: parent is the current integration baseline and changed
  paths are exactly `INVENTORY.md` plus `EXECUTION_LEDGER.md`;
- assignment finalization: parent is the assignment-state commit and the only
  changed path is `EXECUTION_LEDGER.md`;
- assignment authorization: parent is finalization and changed paths are
  exactly `PREFLIGHT_EVIDENCE.md` plus that assignment's rendered prompt;
- runtime capture: parent is authorization and only preflight changes;
- release recording: parent is runtime capture and only preflight changes;
- ordinary worker integration: exact parents are current pre-merge integration
  `HEAD` first and the recorded returned worker tip second; the assigned
  writable projection equals the worker-tip tree, while every path outside it
  equals the first-parent tree;
- registration/status: parent is that merge and only the byte-preserving goal
  relocation, root manifests/catalogs, and coordinator state/evidence paths
  required by the registered transition may change; the package projection is
  unchanged except for removing/adding the identical coordinator-custody goal;
- recovery and integrated-repair commits: exact parents, projections, and
  control paths are those pinned by their effective authorization lifecycle;
  and
- all other coordinator commits: only the exact coordinator-control paths
  allowed for their registered transition and no worker-owned projection.

The writable projection is the assigned canonical module minus every reserved
descendant root and current coordinator-custody goal path. The audit MUST use
the union of canonical roots and goal paths resolved from each commit's parent
and current `GOAL_MANIFEST.json`, `INVENTORY.md`, `modules.json`, and
`packages.json`. A never-integrated unit's writable projection at assignment
baseline MUST equal the recorded base projection. A previously integrated
unit's baseline projection MUST equal its latest registered integration or
verified repair checkpoint. No unclassified commit, ancestry-only match,
intermediate commit, coordinator-authored string, or equal final tree can hide
a transient package edit.

The coordinator-captured tool-visible sequence is an additional custody
condition. The validator MUST verify capture identities for worker creation,
readiness `worker-message`, the `PREPARE-RELEASE` coordinator send result, the
committed release-row boundary, and implementation-return `worker-message`;
the returned capture binds exact report bytes, ordered worker commits, and
worker tip. These captures are coordinator-authored and MUST NOT be described
as platform-signed or independently authentic. Anti-self-certification instead
comes from isolated worker custody, exact committed bytes and ancestry, a
separate coordinator review, and coordinator-owned reruns at the returned
committed revision.

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

### Gate classes

The coordinator MUST keep three gate classes distinct:

1. A `worker-local gate` runs in the worker worktree against package-local
   inputs. It is return evidence only and cannot change inventory status.
2. A `per-unit verification gate` runs from a clean committed integration
   revision after that unit is integrated. Its exhaustive input manifest
   contains the unit, its current `Requires` closure, selected coordinator
   contracts, shared gate tooling, and every other behavior-affecting input in
   the evidence model. A passing current result is required for that unit's
   `implemented-unverified -> verified` transition and may unlock dependants;
   it does not prove program completion.
3. A `program-final-input gate` runs or is fingerprint-reused only after all 67
   implementations are integrated and the semantic contract set is frozen for
   final acceptance. At one clean committed revision it recomputes all 67
   complete-input roots, demotes every stale unit and affected reverse
   dependant, proves every acceptance artifact and journey, runs every affected
   release gate, and reviews the complete diff. Only this class can satisfy the
   final-input clauses of `PROGRAM-COMPLETE`.

The terms `per-unit`, `verified`, and `final-input` MUST NOT be used
interchangeably. Any behavior-affecting edit after a program-final-input gate
invalidates that gate and all roots it changes; bookkeeping-only evidence
commits remain subject to the explicit exclusions and ancestry bindings.

### Authoritative execution and evidence custody

For every per-unit verification gate, external-provider gate, protocol or
security conformance gate, and program-final-input artifact, the coordinator
MUST use the coordinator-owned runner to launch the exact authoritative
command directly. The runner MUST resolve the executable before launch and
invoke its absolute path with an exact argument vector; a package-worker
process, shell transcript, copied log, caller-supplied final artifact, or
producer-owned wrapper MUST NOT be accepted as the authoritative execution.
Repository-owned launchers such as `make` remain valid only when the receipt
also binds the launcher and every repository script, tool, configuration, and
input selected by the complete-input manifest.

Before launch, the runner MUST capture the tested revision and input root, the
absolute executable path, executable version output, SHA-256 digest of the
executable bytes, exact argument vector, working directory, and a sorted
allowlisted environment with secret values redacted to stable presence-only
identities. It MUST capture start and completion times, exit status, and exact
stdout and stderr bytes, byte lengths, and SHA-256 digests directly from the
subprocess boundary. These values are immutable after process completion. A
retry is a new execution identity and receipt; it MUST NOT replace or rewrite
the failed attempt.

The runner MUST give a result producer only a fresh, previously absent,
task-owned raw-capture path. The producer MUST NOT receive a writable final
artifact, receipt, affected-package manifest, reverse-dependant manifest, or
viable-mutant manifest. After successful producer completion, the runner MUST
launch a distinct artifact-specific verifier command. That verifier MUST be
coordinator selected, MUST NOT be authored or executed by the package worker
that produced the implementation or raw result, and MUST recompute the exact
artifact contract from the immutable raw capture and repository sources. The
runner MUST apply the same direct-launch, executable-identity, environment, and
subprocess-capture requirements to the verifier command. Only
the runner MAY derive final execution, transcript, provider, protocol,
coverage, mutation, and outcome fields and atomically write the receipt and
final artifact. Producer-authored versions of those fields are invalid.

For protocol and security claims, independence is part of the claim. The
runner MUST execute every selected external suite or independent
implementation in `PROTOCOL_CONFORMANCE_MANIFEST.json`; where no external
suite applies, it MUST use a separately owned artifact-specific verifier over
raw wire, state, audit, or security-event observations. Package tests, mocks,
producer-declared expected values, and the implementation checking its own
output MUST NOT satisfy independent evidence.

Before any package, reverse-dependant, coverage, or mutation result producer,
the runner MUST independently invoke repository-owned discovery at the tested
revision. It MUST derive affected production packages and complete reverse
dependants from `modules.json`, `packages.json`, `go.work`, module imports, and
the repository gate's native selection output, not from inventory status or a
worker list. For coverage and mutation it MUST invoke the exact gate in two
separate subprocesses with `ACCEPTANCE_DISCOVERY=affected-packages` and
`ACCEPTANCE_DISCOVERY=viable-mutants`, respectively. It MUST capture each
canonical stdout manifest before launching the result producer and require the
result identities to equal those manifests exactly. Mutation outcomes MUST
also bind the mutation tool's native machine-readable output; a producer-
normalized list, declared total, or worker-authored mutant manifest is not
evidence that every viable mutant was discovered or killed.

Every local or external gate evidence record MUST bind the separately
committed coordinator execution receipt by canonical path and exact byte
digest in its artifact hashes. The receipt MUST in turn bind every command,
discovery subprocess, raw capture, stdout/stderr capture, tool/environment
identity, artifact-specific verifier, and final artifact used by the result.
The ledger or preflight binding MUST resolve the receipt and evidence blobs
from their named commits and prove the ancestry to the later status or final
acceptance commit. A result without that two-way receipt binding is
provenance-only and MUST NOT authorize `verified` or `PROGRAM-COMPLETE`.

When a package worker returns a completed package-local commit, the coordinator
MUST:

1. independently inspect the complete package diff and evidence;
2. verify the returned tip is reachable from the assigned worker branch and the
   assignment authorization, runtime-capture, and release-handshake row
   checkpoints are its ancestors. Validate the rendered integration-baseline-
   to-assignment-authorization-to-runtime-capture-to-release-row envelope
   separately as coordinator-owned state; then validate the release-row-
   checkpoint-to-tip worker-authored range changes only the assigned module
   root after subtracting the unit's coordinator-custody goal path and every
   reserved nested root derived from the union of inventory,
   root `modules.json`, and root `packages.json`. Inspect every worker-authored
   commit and every merge parent in that latter range rather than assuming the tip is the only package
   commit. Reject the return unless its task/agent-bound coordinator capture
   exists, matches the tool-visible spawn result and requested settings, and
   binds the exact preparation directive digest;
3. integrate the branch with the required non-fast-forward merge;
4. move the planning goal to the unit's declared canonical goal path;
5. register the module and packages in every root manifest;
6. regenerate only required catalogs through repository tooling;
7. record the already-known worker merge commit as the integration checkpoint,
   mark the unit `implemented-unverified`, clear its active inventory owner,
   and commit this recoverable state transition with gate execution revision,
   gate fingerprint, and local gate evidence binding all absent. The integration
   checkpoint MUST remain the earlier worker merge commit;
8. run `validate.rb --execution --clean-integration` with the required previous
   fixture quartet, capture that exact
   committed integration `HEAD` immediately before execution as both the tested
   and gate execution revision, derive and retain the exhaustive sorted gate
   input manifest and canonical root from that revision, then run structural
   validation, the package gate and only input-invalidated reverse-dependant
   gates from the integration worktree. Execution bookkeeping under
   `INVENTORY.md`, `EXECUTION_LEDGER.md`, `PREFLIGHT_EVIDENCE.md`, and
   `.ai/identity-platform/evidence/` is explicit non-behavioral provenance and
   is excluded from the behavioral input root. The authoritative current
   `Requires` closure still selects the complete module-root set and
   `DEPENDENCIES.md` remains a behavior input; every other selected input
   remains included. For fingerprint reuse, capture the current clean committed
   `HEAD` as the gate execution and revalidation revision, retain the original
   tested revision, and prove the exhaustive manifest and root identical at
   both revisions before accepting the prior result;
9. for every confirmed package-local defect, allocate the next integrated-repair
   epoch and commit an `implemented-unverified -> in-progress` transition that
   restores the same worker task as owner and retains its branch, worktree,
   assignment, and generation. In that same authorization commit, append the
   exact `authorized` integrated-repair row, bind the current integration
   baseline as that commit's first parent, the prior integrated checkpoint, the
   clean worker checkpoint, canonical current goal path and goal-body digest, complete reserved-root
   union, and committed rendered repair-prompt bytes and digest. Merge that exact
   authorization commit into the worker branch before any edit. Validate only
   its checkpoint-to-repaired-tip range as worker-authored, integrate the repair
   by non-fast-forward merge, record that new merge as the integration
   checkpoint, and append a matching `completed` row in a later coordinator
   commit. A stale or changed binding MUST be superseded and reauthorized under
   the next epoch; it MUST NOT be repaired under the original assignment prompt;
10. after every per-unit verification gate passes, commit each canonical local gate
    evidence v2 record and its artifacts against the captured revisions and
    exhaustive manifest, then commit the append-only local-gate binding plus
    ledger/status transition that records the gate execution revision and input
    root. Mark the unit `verified` only in that later transition; an evidence
    record MUST name its canonical path but MUST NOT predict or embed the commit
    that contains itself; and
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
   `proposed` to `in-progress` in durable history. An eligible unit MUST NOT
   remain `proposed` for a later scheduling cycle merely because worker slots
   are full; readiness is durable independently of immediate assignment.
5. Fill available worker slots with distinct `ready` units.
6. Before spawning, complete the assignment-state/finalization protocol and
   render both exact commits plus generation into its worker prompt. After
   spawn, complete the distinct runtime-capture and activation handshake before
   allowing work or accepting a return.
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
`primitive/authentication-identity-contracts`,
`primitive/authorization-identity-contracts`,
`primitive/capability-identity-contracts`,
`primitive/identifier-identity-contracts`,
`primitive/password-secret-contracts`, and `identity/delivery`. Five primitive extensions and `identity/delivery` are dependency-free;
`primitive/capability-postgres-identity-contracts` follows both
`primitive/capability-identity-contracts` and `identity/postgres`. The identity-platform scope remains 61 units, with 67
schedulable units total. `identity` remains `proposed` until
`primitive/authorization-identity-contracts` is verified; `webauthn` remains
`proposed` until both `primitive/authentication-identity-contracts` and
`primitive/identifier-identity-contracts` are verified.

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
Before any execution-mode acceptance and before final completion, it MUST
recompute every verified unit's exhaustive input manifest and root at the exact
current clean committed integration `HEAD`. Any mismatch MUST deterministically
demote that unit and every verified reverse dependant whose complete root
changed to `implemented-unverified` in one ancestry-preserving transition,
clear their gate revision/root and stale external binding as required, and
revalidate from that exact `HEAD`; an ancestor gate revision alone is not
current evidence.
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
recovery row that pins the authorization commit's first-parent pre-resume
integration baseline, worker checkpoint, and complete-input root. It MUST then
append the immediate recovery-finalization `effective` row and give the worker the exact
resulting effective-authorization commit. Before any package edit, the worker
MUST verify that checkpoint and both ancestry relationships, merge that exact commit, re-render
the prompt with the same generation and current goal path, and require all
invalidated acceptance evidence again. If the worker cannot reach a coherent clean
checkpoint without discarding work, it remains inventory `blocked` while other
lanes continue. A conflict or
incompatible public contract returns to the affected owner; it MUST NOT be
resolved by accepting stale evidence.

## Worker acceptance

A worker return MUST include:

- unit and canonical module;
- branch, worktree, assignment generation, assignment-state commit,
  release-handshake row commit, current worker-branch tip, and ordered
  worker-authored commit hashes;
- changed package-local paths;
- the exact requirement-to-evidence closure derived from every normative BCP 14
  block in the committed unit goal, every operation catalog row owned by the
  unit, and every acceptance-artifact/claim row produced by or covering an
  operation owned by the unit;
- for every returned report, its artifact-specific observation IDs and exact
  expected/actual behavioral outcomes, tested revision, gate execution
  revision, nullable revalidation revision, complete sorted tracked input plus
  all required environment-input identities, canonical input root, and
  sanitized artifact hashes; every observation MUST name the exact artifact
  path and SHA-256 digest that proves it, and the report MUST NOT
  claim or predict the later coordinator evidence-record commit;
- exact checks and outcomes;
- coverage and mutation totals;
- race, fuzz, leak, interoperability, benchmark, API, docs, security, and
  supply-chain outcomes required by the goal;
- external provider evidence and exact unavailable boundaries;
- review outcome;
- coordinator registration or integration work still required.

This list is the canonical return manifest. `WORKER_PROMPT.md` MUST render the
same fields in the same order and the coordinator MUST reject any return with a
missing, extra, renamed, or reordered field. Every check and report MUST name
the exact committed worker tip it tested. If any post-commit verification
changes package bytes, the worker MUST commit the correction and rerun every
affected check against the new clean tip before returning.

The human-readable return is not machine authority. Before integration, the
coordinator MUST create canonical compact JSON under
`.ai/identity-platform/evidence/worker-returns/` with ordered fields
`schema_version`, `schema`, `unit`, `canonical_module`, `branch`, `worktree`,
`generation`, `assignment_state_commit`, `release_handshake_commit`,
`worker_tip`, `ordered_worker_commits`, `changed_paths`, `requirements`,
`reports`, `checks`, `coverage_mutation`, `specialized_outcomes`,
`external_boundaries`, `review`, and `coordinator_work`. `schema` is
`identity-platform.worker-return.v1`. Every report object MUST bind its exact
artifact ID, tested revision, gate-execution revision, nullable revalidation
revision, input manifest/root, observations, and artifact hashes; every check
object MUST bind argv, outcome, and tested revision. Arrays are exhaustive,
bytewise sorted where order has no semantics, and duplicate-free. The
validator derives requirement IDs from the exact committed goal and catalogs;
missing or invented IDs are invalid. Artifact, observation, and check IDs MUST
be globally unique and use `artifact.<unit-namespace>.`,
`observation.<unit-namespace>.`, and `check.<unit-namespace>.`, where slash in
the unit name becomes a dot. Returned artifacts MUST be changed, committed
files inside the assigned package projection, not coordinator `.ai` evidence
or an unrelated pre-existing blob. Each observation has the exact ordered
fields `observation_id`, `expected_outcome`, `actual_outcome`, `result`,
`artifact_path`, and `artifact_sha256`; its path/digest MUST equal one returned
artifact row and the bytes at `worker_tip`. Every input manifest MUST contain
the exact tracked behavior inputs and all required `environment:*` rows, and
its root MUST cover the complete canonical array. The coordinator recomputes
the environment identities from its recorded tool and external lanes before
accepting the return.

The
coordinator MUST validate exact keys/types, require all report/check tested
revisions to equal `worker_tip`, recompute changed paths and ordered worker
commits from Git, commit the JSON unchanged, and use its exact binding in the
return row. Prose alone MUST NOT satisfy worker-return acceptance.

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

Every per-unit verification gate and program-final-input acceptance gate MUST
use the complete-input manifest and root contract in
`END_STATE_ACCEPTANCE.json`; a digest over only coordinator
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
removed. The integration worktree is the execution output, not a disposable
worker resource: the final coordinator commit MUST mark it
`removal-pending-after-final-commit` and the terminal validator treats that
exact clean coordinator-owned state as reconciled. The report MUST say it is
retained for handoff, not removed, with cleanup trigger exactly
`user-authorized-post-report-removal`. Physical removal is a later user-authorized
operation and is outside `PROGRAM-COMPLETE`; requiring it before validation
would make the final committed report impossible to produce or revalidate.
Before a blocked stop, it MUST perform the same sweep except for resources
strictly required to resume a recorded assignment; every retained resource MUST
name its unit, blocker, owner, exact path or safe resource ID, and cleanup
trigger in the final report. An interruption MUST begin with the same registry
reconciliation before new resources are created.

## Stop conditions

The sole successful stop is `PROGRAM-COMPLETE` as exhaustively defined in
`PROGRAM.md`; this section does not define a shorter alternative. Until that
predicate is true, the coordinator MUST continue every safe in-scope scheduling,
integration, repair, evidence, review, cleanup, or user-authorized semantic
lane that can make progress.

The sole non-success terminal predicate is `PROGRAM-BLOCKED`.
`PROGRAM-BLOCKED` is true only when all of the following hold simultaneously:

1. `PROGRAM-COMPLETE` is false;
2. no unit, verification lane, repair, review, cleanup, or evidence lane can
   make safe progress;
3. every remaining path is stopped by a specifically recorded absence of user
   authority, credential, external infrastructure, or product decision;
4. all independent units and claims not dependent on those absences have been
   exhausted;
5. every active assignment and task-owned resource is durably reconciled for
   recovery; and
6. the repository's persistent-goal blocked-audit threshold is satisfied.

An inventory `blocked` row, an unavailable provider, an exhausted worker slot,
or a temporarily idle frontier does not by itself make `PROGRAM-BLOCKED` true.
If neither terminal predicate is true, stopping is forbidden.

A provider credential gap blocks only claims requiring that provider. It MUST
NOT stop unrelated packages.

## Final report

The final report MUST be one canonical JSON payload at
`.ai/identity-platform/evidence/final-report.json` conforming exactly to
`FINAL_REPORT.schema.json`. That schema is the sole field, type, ordering,
catalog-closure, outcome, and nullability authority; this goal MUST NOT define
or accept a second prose schema. The coordinator MUST run
`ruby .ai/identity-platform/final_report.rb --check` against the current
catalogs and then run the final
`ruby .ai/identity-platform/validate.rb --execution ...` invocation against
the exact execution snapshot. Direct `final_report.rb --validate` is
intentionally fail-closed because caller JSON and CLI arguments cannot supply
trusted repository or runtime facts; the execution validator derives trusted
state and invokes `IdentityPlatformFinalReport.validate_report` internally.
A missing tool, stale generated schema, duplicate key, non-canonical
payload, extra or missing field, catalog-set mismatch, unresolved evidence
binding, or failed predicate check blocks the report.

The payload MUST close every unit, parity row, journey, cross-cutting claim,
acceptance artifact, final gate, provider boundary, deployment boundary,
cleanup resource, semantic authorization, terminal requirement, and blocker
required by the schema. Its `integration.final_input_revision` MUST be the
exact clean committed semantic and gate-input revision against which all
current roots reproduce. The later canonical report/evidence-binding commit is
derived by the validator and MUST NOT be embedded recursively in its own
payload. Every
`upstream_baseline` row MUST enumerate the canonical Better Auth repository,
pinned revision, object format, and exact source objects from
`UPSTREAM_SURFACE.json`; the final-gate evidence MUST include the live
`pinned-upstream-validation` check against that locally obtainable repository.
Every
evidence/receipt binding MUST resolve byte-for-byte at its named commit with
valid ancestry. `PROGRAM-COMPLETE` is valid only when the validator proves all
nine `PROGRAM.md` clauses, no blocker, no unreconciled resource, and all
required current outcomes; the exact pending integration-worktree handoff is
reconciled rather than removed. The no-push prohibition remains mandatory behavior, but the
available tooling cannot independently prove complete command history or
remote non-delivery. The final report therefore carries only the coordinator
assertion, bounded local Git-observable state, `assertion_verified=false`, and
the explicit unverified limitation; that object MUST NOT satisfy any stronger
verified claim or terminal clause. `PROGRAM-BLOCKED` is valid only when the validator
proves every clause in the blocked predicate above and enumerates every exact
remaining blocker and required user action. Any payload that validates neither
terminal predicate forbids stopping. Only after validation passes MAY the
coordinator render the human-readable final report from that exact payload.

The coordinator MUST NOT describe the program as complete, ready, equivalent,
or production-safe while any required row, journey, review, or gate remains
unproved.
