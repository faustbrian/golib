# Identity Platform Worker Prompt

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
8. the exact assigned goal.

Verified prerequisites present in this branch:
<verified-prerequisite-list>

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
   Prove the integration baseline is an ancestor of HEAD; the goal path and
   every read/command path are inside the assigned worktree; and inventory plus
   ledger match this unit, branch, worktree, generation, assignment commit,
   owner and `in-progress` status. Stop on any mismatch.
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
10. Use commit-with-intent to create one coherent package commit.
11. Do not push, merge, rebase, or alter coordinator-owned state.
12. Before returning, prove the commit changes only the assigned canonical
    package directory and that the worktree contains no uncommitted package
    work.

Return:
- unit and canonical module;
- branch, worktree, assignment generation, assignment-state commit and worker
  commit hash;
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
