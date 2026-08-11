# Identity Platform Worker Prompt

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

The coordinator MUST render every angle-bracket marker before spawning a
worker. A worker MUST reject an assignment containing any unresolved marker.

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

Read completely before editing:
1. repository AGENTS.md;
2. .ai/identity-platform/COMMON_REQUIREMENTS.md;
3. .ai/identity-platform/INVENTORY.md for this unit and its prerequisites;
4. .ai/identity-platform/DEPENDENCIES.md;
5. .ai/identity-platform/END_STATE.md sections referenced by the goal;
6. .ai/identity-platform/REFERENCE_PROFILE.md sections owned by this unit;
7. .ai/identity-platform/BETTER_AUTH_PARITY.md rows owned by this unit;
8. .ai/identity-platform/API_OPERATIONS.md operations owned or consumed by this
   unit;
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
- reserved descendant unit roots that MUST NOT be modified:
  <reserved-descendant-module-directories>
- the canonical root does not grant ownership of another inventory unit nested
  beneath it; generated files, tests and documentation for a descendant remain
  reserved to that descendant worker;
- implement every requirement in the assigned goal;
- implement every in-scope parity row assigned to this unit;
- produce package-local production code, tests, fixtures, migrations,
  documentation, examples, go.mod, go.sum, README.md, and CHANGELOG.md;
- do not edit another package, root manifests, root catalogs, global
  inventory, dependency graph, program documents, or another worktree;
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
11. Do not push, rebase, or alter coordinator-owned state. Do not merge except
    when the coordinator sends an explicit recovery directive naming one exact
    resume/authorization commit after recording the corresponding
    `in-progress -> blocked -> in-progress` transitions and an `authorized`
    conflict-recovery row in `PREFLIGHT_EVIDENCE.md`. Before any package edit,
    verify that row matches this unit, generation, and worker checkpoint;
    verify its integration commit is the named resume commit's first parent;
    then merge the exact named resume/authorization commit. Reject a missing,
    superseded, ancestry-invalid, or otherwise mismatched row or commit.
    For that recovery merge, resolve only conflicts in the assigned package;
    stop and return coordinator-owned conflicts to the coordinator. Never
    discard, overwrite, or hide uncommitted work to make a recovery merge run.
12. Before returning, prove the commit changes only the assigned canonical
    package directory and that the worktree contains no uncommitted package
    work.

Return:
- unit and canonical module;
- branch, worktree, assignment generation, assignment-state commit, current
  worker-branch tip, and ordered worker-authored commit hashes;
- changed package-local paths;
- requirement-to-evidence mapping;
- exact checks and outcomes;
- coverage and mutation totals;
- race, fuzz, leak, interoperability, benchmark, API, docs, security, and
  supply-chain outcomes required by the goal;
- external provider evidence and exact unavailable boundaries;
- review outcome;
- coordinator registration or integration work still required.

Do not claim the package verified. Only the coordinator can set verified after
integration and final-input gates.
```

## Coordinator rendering rules

The coordinator MUST include only prerequisites whose integration-branch state
is `verified`. The goal path MUST refer to the planning goal before
integration and to the canonical package goal after it has moved. The worker
branch MUST already contain the named integration commit.

The coordinator MUST run
`ruby .ai/identity-platform/render_shared_contracts.rb --check`, render the
named unit, and replace `<shared-contract-applicability>` with that complete
output. It MUST NOT hand-edit, summarize, add to, or omit rendered rows.

The coordinator MUST render every other inventory module directory nested
beneath the assigned module as a reserved descendant, or `none` when there is
no such unit. Returned-path validation MUST use the assigned module root minus
those reserved roots, not a simple directory-prefix check.

The absolute goal path MUST resolve inside the assigned worktree. A repair or
resumed assignment MUST receive a newly rendered prompt containing the current
baseline, generation, assignment-state commit and the canonical goal path if
the coordinator has moved it.

The coordinator MUST include the raw assignment without additional
implementation suggestions that narrow, reinterpret, or weaken the goal.

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
merge authorized by the coordinator, resolves assigned-package conflicts,
reruns every invalidated requirement from the refreshed baseline, and returns a
new coherent commit. When dirty work cannot reach a coherent clean checkpoint,
the unit remains inventory `blocked`; the coordinator continues independent
work and requests user direction only when this is the last remaining progress
lane.
