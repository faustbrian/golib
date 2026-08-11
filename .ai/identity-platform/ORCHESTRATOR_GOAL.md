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
6. `BETTER_AUTH_PARITY.md`;
7. `DEPENDENCIES.md`;
8. `INVENTORY.md`;
9. `WORKER_PROMPT.md`;
10. the exact goal assigned to that worker.

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
`ready` units while retaining one slot for itself. It MUST NOT spawn a worker
for a `proposed`, `blocked`, `in-progress`, or
`implemented-unverified` unit.

## Branch and worktree topology

The coordinator MUST use the repository branch and worktree skills.

1. Create `feature/identity-platform` from `main` in an isolated integration
   worktree.
2. Create every worker branch from `main`, as repository policy requires.
3. Before a dependent worker starts, merge the current local integration branch
   into its worker branch so all verified prerequisites are present.
4. Never rebase a worker or integration branch.
5. Never reuse a worktree for two concurrent workers.
6. Never run package commands outside the assigned worker worktree.
7. Never switch a worktree containing uncommitted changes.

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

When a package worker returns a verified package-local commit, the coordinator
MUST:

1. independently inspect the complete package diff and evidence;
2. integrate the commit into `feature/identity-platform`;
3. move the planning goal to the unit's declared canonical goal path;
4. update the inventory goal link and status;
5. register the module and packages in every root manifest;
6. regenerate only required catalogs through repository tooling;
7. run structural inventory and dependency validation;
8. run the package gate and changed reverse-dependant gates;
9. mark the unit `verified` only after every final-input gate passes;
10. commit the coherent integration state.

## Scheduling algorithm

The coordinator MUST repeat this loop until completion:

1. Re-read inventory state from the integration branch.
2. For each `proposed` unit, check whether every `Requires` unit is
   `verified`.
3. Mark every newly eligible unit `ready`.
4. Fill available worker slots with distinct `ready` units.
5. Before spawning, atomically mark the unit `in-progress` and record the
   worker task, branch, and worktree.
6. Supervise active workers without duplicating their work.
7. On worker return, classify the result:
   - package-local requirements complete and focused gates pass:
     integration review;
   - implementation complete but evidence missing:
     `implemented-unverified`;
   - correct work cannot continue:
     retain `blocked` only under repository blocked-status rules;
   - confirmed defect:
     return the exact finding to the same worker.
8. Integrate successful units one at a time.
9. Recompute newly eligible units immediately after each verified integration.
10. Continue unrelated ready work when one provider or environment is blocked.

Initial scheduling MUST start only the currently ready roots:
`identity`, `identity/delivery`, and `webauthn`.

## Worker acceptance

A worker return MUST include:

- inventory unit and canonical module;
- branch, worktree, and commit hash;
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
`END_STATE.md` reference journeys against the final integrated branch. This
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
