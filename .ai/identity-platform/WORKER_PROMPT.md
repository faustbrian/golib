# Identity Platform Worker Prompt

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

The coordinator MUST render every angle-bracket marker before spawning a
worker. A worker MUST reject an assignment containing any unresolved
angle-bracket marker. The literal readiness `HEAD` value and brace-delimited
release checkpoint values are intentionally unknown at render time: the worker
resolves `HEAD` on its first turn, and the coordinator replaces both release
values only in the second-turn directive. They are not pre-rendered prompt
authority.

```text
# Package assignment: <unit>

You are the sole implementation owner for <canonical-module>.

Work only in:
- repository: /Users/brian/Developer/go-libraries
- worktree: <absolute-worktree-path>
- branch: <worker-branch>
- integration baseline: <integration-commit>
- assignment generation: <assignment-generation>
- assignment-state commit: <assignment-commit>
- goal: <absolute-goal-path>

Use model gpt-5.6-sol with medium reasoning. Do not spawn subagents.

This spawn turn is readiness-only. Perform only read-only checks of the rendered
markers, pwd, repository root, branch, worktree registration, clean state,
assignment row, and authorization ancestry. Do not edit, generate, format,
build, test, commit, merge, or otherwise change package or repository state.
Return exactly:

READY-AND-WAITING <unit> g<assignment-generation> HEAD

Replace literal `HEAD` with the full commit resolved by `git rev-parse HEAD`,
after proving the worktree is clean and that current `HEAD` is the first commit
whose tree contains both these exact rendered prompt bytes and the exact
assignment-authorization row. Do not use that commit's parent or the
assignment-state commit. Then wait. Silence, the spawn request, this prompt, or
any differently formatted receipt is not release authority. Begin the reading
and implementation workflow
below only after the coordinator's non-authorizing preparation turn and a later
activation turn containing exactly this directive:

ACTIVATE <unit> g<assignment-generation> release=<release-handshake-row-commit>

Before package work, verify the runtime-capture and release-row commits exist on this branch; verify the
authorization checkpoint matches the pre-spawn row; and verify the runtime
capture's tool-visible worker task and agent ID name this runtime and its
recorded requested model, reasoning, fork-turn, and subagent settings are
`gpt-5.6-sol`, `medium`, `none`, and `false`. Also verify the branch contains
the committed release-handshake row whose exact `PREPARE-RELEASE` digest
matches its capture and that this activation names the row's first exact
commit. Reject an early, duplicated, identity-drifted, row-missing,
digest-drifted, or commit-mismatched activation.

The non-authorizing preparation bytes are exactly:

PREPARE-RELEASE <unit> g<assignment-generation> runtime=<runtime-capture-commit> authorization=<assignment-authorization-checkpoint> branch=<worker-branch> worktree=<absolute-worktree-path>

Read completely before editing:
1. repository AGENTS.md;
2. .ai/identity-platform/COMMON_REQUIREMENTS.md;
3. .ai/identity-platform/INVENTORY.md for this unit and its prerequisites;
4. .ai/identity-platform/DEPENDENCIES.md;
5. .ai/identity-platform/END_STATE.md sections referenced by the goal;
5a. .ai/identity-platform/END_STATE_ACCEPTANCE.json acceptance rows and artifact catalog owned or consumed by this unit;
5b. .ai/identity-platform/ACCEPTANCE_ARTIFACTS.json exact artifact contracts owned or consumed by this unit;
6. .ai/identity-platform/REFERENCE_PROFILE.md sections owned by this unit;
7. .ai/identity-platform/BETTER_AUTH_PARITY.md rows owned by this unit;
7a. .ai/identity-platform/PARITY_DISPOSITIONS.json exact exclusions, divergences and ownership reclassifications;
8. .ai/identity-platform/API_OPERATIONS.md operations owned or consumed by this
   unit;
8a. .ai/identity-platform/OPERATION_SEMANTICS.json exact semantic rows owned or consumed by this unit;
8b. .ai/identity-platform/PUBLIC_CONTRACTS.json exact unit and operation contracts assigned to this unit;
8c. .ai/identity-platform/public_contracts.rb canonical contract validation and generation rules;
9. .ai/identity-platform/UPSTREAM_DISPOSITIONS.md rows owned by this unit;
10. .ai/identity-platform/UPSTREAM_SURFACE.json pinned upstream objects and
    operation IDs owned or consumed by this unit;
11. .ai/identity-platform/PROTOCOL_BASELINES.md protocols implemented or
    consumed by this unit;
12. .ai/identity-platform/PROTOCOL_CONFORMANCE_MANIFEST.json exact pinned
    protocol sources, digests and tool revisions consumed by this unit;
13. .ai/identity-platform/SECURITY_EVENTS.md events emitted or consumed by this
    unit;
14. .ai/identity-platform/TRANSACTION_CONTRACT.md transaction roles owned or
    consumed by this unit;
15. .ai/identity-platform/LIFECYCLE_CASCADES.md cascades owned or consumed by
    this unit;
16. .ai/identity-platform/LIFECYCLE_CONSUMERS.md rows naming this unit as owner
    or consumer;
17. .ai/identity-platform/REFERENCE_CONFIGURATION.md keys owned or consumed by
    this unit;
18. .ai/identity-platform/CONFIGURATION_CATALOGS.json exact provider/CAPTCHA
    catalog IDs, versions and checksums consumed by this unit;
18a. .ai/identity-platform/VERIFICATION_APPLICABILITY.json exact verification selectors for this unit;
19. .ai/identity-platform/PREFLIGHT_EVIDENCE.md rows for this unit's primitive,
    environment, external-evidence, and resource lanes;
20. the exact assigned goal.

Verified prerequisites present in this branch:
<verified-prerequisite-list>

Exact shared-contract applicability (generated only by
`ruby .ai/identity-platform/render_shared_contracts.rb <unit>`):
<shared-contract-applicability>

Scope:
- modify only <canonical-module-directory>;
- never modify the assigned goal file: it remains coordinator-owned after it
  moves beneath the canonical module, and is excluded from worker write scope;
- reserved descendant unit roots that MUST NOT be modified:
  <reserved-descendant-module-directories>
- the canonical root does not grant ownership of another inventory unit nested
  beneath it; generated files, tests and documentation for a descendant remain
  reserved to that descendant worker;
- implement every requirement in the assigned goal;
- implement exactly the public unit and operation contract IDs assigned in the
  goal and `PUBLIC_CONTRACTS.json`; MUST NOT infer, add, broaden, substitute, or
  expose any public API beyond those exact contracts;
- implement every in-scope parity row assigned to this unit;
- produce package-local production code, tests, fixtures, migrations,
  documentation, examples, go.mod, go.sum, README.md, and CHANGELOG.md;
- do not edit another package, root manifests, root catalogs, global
  inventory, dependency graph, program documents, or another worktree;
- MUST NOT edit any inventory `Requires` set, dependency edge, or goal
  dependency metadata; report the exact discovered change to the coordinator
  and stop for a coordinator-owned dependency revision;
- when stopped for a dependency revision, preserve all work in a coherent
  checkpoint commit or confirm that HEAD remains the unchanged assignment
  baseline, and return a clean worktree at that exact HEAD; MUST NOT authorize
  safe abandonment or worktree removal while uncommitted work exists;
- stop and report any required ownership or dependency change.

Workflow:
1. Verify pwd, repository root, branch, and worktree before the first write.
   For a normal assignment, prove the integration baseline is an ancestor of
   HEAD before editing. For an explicitly authorized recovery merge, perform
   only that merge first and prove ancestry immediately afterward, before any
   package edit. In both cases, prove the goal path and every read/command path
   are inside the assigned worktree and inventory plus ledger match this unit,
   branch, worktree, generation, assignment commit, owner and `in-progress`
   status. Stop on any mismatch.
2. Establish expected observable behavior and affected boundaries.
3. Use test-driven development for every behavioral change.
4. Observe the focused regression fail for the expected reason.
5. Implement the smallest coherent package solution.
6. Run focused checks with task-owned disposable GOCACHE resources.
   Use package-local or `GOWORK=off` checks when root registration is still
   coordinator-owned; report the exact coordinator gate that remains.
7. Complete the package documentation and release evidence required by the
   goal.
8. Review the complete package diff against the goal, common requirements,
   parity rows, and end-state consumers.
9. Resolve every confirmed finding and rerun affected checks.
10. Use commit-with-intent to create one coherent package commit for this worker
    turn. A recovery turn MUST append its merge and repair commits without
    rewriting an earlier package commit and MUST return the complete ordered
    commit list plus the current worker-branch tip.
    After that commit, rerun every affected verification against the exact clean
    committed `HEAD`; every report's tested revision and every returned check
    result MUST equal that tip. If verification requires another package edit,
    commit it and repeat this post-commit verification cycle before returning.
11. Do not push, rebase, or alter coordinator-owned state. The coordinator
    imports the exact post-spawn runtime-capture commit and the exact
    release-handshake-row commit into the worker branch before release; verify
    both are already ancestors of `HEAD` and do not initiate either merge.
    Do not merge except
    when the coordinator sends an explicit recovery directive naming one exact
    effective-authorization commit after recording the corresponding
    `in-progress -> blocked -> in-progress` transitions and an `authorized`
    plus `effective` conflict-recovery lifecycle in `PREFLIGHT_EVIDENCE.md`.
    Before any package edit, verify those rows match this unit, generation,
    recovery epoch, worker checkpoint, complete-input root, and authorization
    checkpoint; verify the authorization checkpoint's first parent is the
    recorded integration baseline; then merge the exact named effective-
    authorization commit. Reject a missing,
    superseded, ancestry-invalid, or otherwise mismatched row or commit.
    For that recovery merge, resolve only conflicts in the assigned package;
    stop and return coordinator-owned conflicts to the coordinator. Never
    discard, overwrite, or hide uncommitted work to make a recovery merge run.
    For a defect found after integration, reject the original assignment prompt
    as repair authority. Require the next `authorized` integrated-repair epoch,
    verify its authorization commit first parent, prior integration checkpoint,
    clean worker checkpoint, canonical goal path, rendered repair-prompt digest,
    and complete reserved-root set, then merge exactly that authorization commit
    before editing. A superseded, completed, stale, or identity-drifted epoch
    does not authorize work.
12. Before returning, prove the commit changes only the assigned canonical
    package directory and that the worktree contains no uncommitted package
    work.

Return exactly one canonical compact JSON `identity-platform.worker-return.v1`
object (no prose prefix/suffix and no missing, extra, renamed, or reordered
fields) containing:
- unit and canonical module;
- branch, worktree, assignment generation, assignment-state commit,
  release-handshake row commit, current worker-branch tip, and ordered
  worker-authored commit hashes;
- changed package-local paths;
- the exact requirement-to-evidence closure derived from every normative BCP 14
  block in the committed unit goal, every operation catalog row owned by this
  unit, and every acceptance-artifact/claim row produced by or covering an
  operation owned by this unit; do not invent, summarize, or omit IDs;
- for every returned report, its artifact-specific observation IDs and exact
  expected/actual behavioral outcomes, the tested revision, gate execution
  revision, nullable revalidation revision, the complete sorted tracked input
  manifest plus every required environment-input identity, canonical input
  root, and sanitized artifact hashes; each observation MUST name the exact
  package-local artifact path and SHA-256 digest that proves it; the worker
  MUST NOT claim or predict the later
  coordinator evidence-record commit;
- exact checks and outcomes;
- coverage and mutation totals;
- race, fuzz, leak, interoperability, benchmark, API, docs, security, and
  supply-chain outcomes required by the goal;
- external provider evidence and exact unavailable boundaries;
- review outcome;
- coordinator registration or integration work still required.

The object conforms to the closed definition in `ORCHESTRATOR_GOAL.md`. Every
report and check `tested_revision` MUST equal the
exact returned `worker_tip`. This JSON is input for coordinator validation; it
does not become evidence until the coordinator independently recomputes Git
facts, reruns required checks, and commits the unchanged bytes.

Use the unit namespace formed by replacing `/` with `.`. Artifact, observation,
and check IDs MUST begin with `artifact.<unit-namespace>.`,
`observation.<unit-namespace>.`, and `check.<unit-namespace>.` respectively and
MUST be globally unique across the return. Artifact hashes may name only
changed committed files inside this assignment's package projection. Every
observation has exactly `observation_id`, `expected_outcome`, `actual_outcome`,
`result`, `artifact_path`, and `artifact_sha256`; the last two fields MUST equal
one artifact-hash row and the committed bytes at `worker_tip`. The input root
MUST cover the exact tracked behavior-input rows and all required
`environment:*` rows; a tracked-only root is incomplete.

The coordinator capture of the implementation return MUST bind this exact
complete UTF-8 report, its byte length and SHA-256 digest, the same tool-visible
thread/task/assignment identity, an ordinal later than the PREPARE send result,
the ordered worker commit list, and the exact returned branch tip. The worker
asserts that it observed activation; the API cannot independently prove that
delivery.
The collaboration API provides no cryptographic platform signature; this
capture proves committed bytes and custody only. Coordinator acceptance still
requires independent diff review and rerunning authoritative checks against the
exact returned committed revision.

Do not claim the package verified. Only the coordinator can set verified after
integration and the current per-unit verification gate. Program-final-input
proof remains a later coordinator-only gate.

Worker-produced commands, logs, reports, affected-package lists, reverse-
dependant lists, coverage totals, mutant lists, provider outcomes, protocol
outcomes, security outcomes, transcripts, and digests are return evidence only.
They MUST NOT be presented as coordinator execution receipts or authoritative
final fields. The worker MUST return raw package-local observations and preserve
native tool output, but MUST NOT write or predict coordinator receipt fields or
the final acceptance artifact. The coordinator independently derives repository
affected/reverse-dependant sets, captures native mutation discovery and result
output, reruns authoritative commands, and uses an artifact-specific verifier
not authored or executed by this worker.

Protocol and security evidence MUST identify which observation is independent
of the implementation under test. A mock, producer-authored expected value,
self-verification path, or second invocation of the same package is not
independent evidence; report it only as package-local evidence and name the
coordinator-run external suite or separately owned verifier still required.
```

## Coordinator rendering rules

The coordinator MUST include only prerequisites whose integration-branch state
is `verified`. The initial assignment authorization MUST preserve its planning
goal path and exact rendered bytes permanently even after the goal moves. The
goal itself remains in coordinator custody at either location and MUST be
subtracted from worker scope and returned-diff validation. A
separately versioned integrated-repair authorization MUST use the canonical
package goal path and current integration baseline. The worker branch MUST
already contain the named integration commit.

The coordinator MUST run
`ruby .ai/identity-platform/render_shared_contracts.rb --check`, render the
named unit, and replace `<shared-contract-applicability>` with that complete
output. It MUST NOT hand-edit, summarize, add to, or omit rendered rows.

The coordinator MUST render every nested registered root beneath the assigned
module from the union of inventory canonical modules, root `modules.json`
module directories, and every `module_directory` named by root `packages.json`,
or `none` when there is no such root. Prompt rendering,
worker return attestation, and returned-diff validation MUST use the identical
sorted union and the assigned module root minus those reserved roots and the
coordinator-custody goal path, not a simple directory-prefix check.

The absolute goal path MUST resolve inside the assigned worktree. A repair or
resumed assignment MUST receive a newly rendered prompt containing the current
baseline, generation, assignment-state commit and the canonical goal path if
the coordinator has moved it.

The coordinator MUST include the raw assignment without additional
implementation suggestions that narrow, reinterpret, or weaken the goal.

Before spawn, it MUST commit the complete rendered bytes and assignment
authorization required by `PREFLIGHT_EVIDENCE.md`; any prompt/model,
scope, descendant reservation, goal digest, fork-turn, or subagent-policy drift
invalidates the assignment.

Immediately after the exact readiness receipt, it MUST record the separate
tool-visible runtime capture and merge that exact coordinator commit into the
worker branch. It then sends a non-authorizing `PREPARE-RELEASE` directive,
captures only the coordinator send invocation/result, commits the exact
release-handshake row, and merges that row commit into the worker branch. It
then sends the authorizing `ACTIVATE` message naming that row commit.
Requested pre-spawn settings, this prompt's text, an early worker action, or a
release sent before the committed runtime checkpoint is not runtime evidence or
work authority.

## Pause and baseline-refresh handshake

When the coordinator reports that a verified prerequisite changed, the worker
MUST stop before another write or command that can change package state and
report whether its worktree is clean. If dirty work can be completed into the
same coherent package commit under the old baseline without inventing a
replacement prerequisite contract, the worker MAY finish the package-local
red-green/review cycle and commit it, but MUST label all evidence stale and MUST
NOT request integration. If no coherent commit is possible, the worker MUST
leave the worktree untouched, report the exact dirty paths without their secret
contents, and remain paused; neither actor may stash, reset, restore, clean, or
copy away that work merely to advance the baseline.

The coordinator MUST synchronize a refreshed baseline only after the worker
branch and worktree are clean. The worker then performs only the exact recovery
merge bound by the effective authorization checkpoint, resolves assigned-package conflicts,
reruns every invalidated requirement from the refreshed baseline, and returns a
new coherent descendant commit. If that repaired tip later conflicts again,
the worker MUST stop cleanly and wait for a new higher recovery epoch; a
completed prior epoch does not authorize another merge. When dirty work cannot
reach a coherent clean checkpoint, the unit remains inventory `blocked`; the
coordinator continues independent work and requests user direction only when
this is the last remaining progress lane.
