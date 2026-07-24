# Goal: Harden the Go Libraries Monorepo

## Mission

Perform a final, evidence-driven hardening audit of the complete
`golib` ecosystem after the canonical module migration is implemented.
Do not treat green aggregate tests, file presence, existing badges, or prior
package hardening claims as release proof.

This goal verifies the monorepo as a system while preserving each module's
independent product and release contract.

## Required Baseline

Before changing behavior:

1. Inventory every package, module, nested adapter module, fixture module,
   command, generated artifact, goal, workflow, release tag, and dependency.
2. Rebuild the owned-module dependency graph and reverse-dependency graph.
3. Run every documented local root gate and every package-specific gate.
4. Record failures, flakes, skips, warnings, suppressions, and environmental
   blockers by module.
5. Map every requirement in the root and package `.ai` goals to current
   implementation, tests, documentation, and CI evidence.

Do not modify production behavior until a failing regression or an explicit
goal violation demonstrates the gap.

## Hardening Scope

Audit and prove:

- module paths match repository directories;
- no old standalone imports, pseudo-versions, absolute local paths, or
  permanent development replacements remain;
- the root workspace is complete and deterministic;
- every releasable module works independently with `GOWORK=off`;
- fixture and compatibility modules are never published accidentally;
- dependency direction is acyclic and foundational packages remain small;
- optional adapters do not force unrelated dependencies on core consumers;
- every package satisfies its implementation, hardening, supplemental, and
  specification goals;
- every public API has meaningful tests and accurate documentation;
- meaningful 100% production coverage requirements are satisfied without
  coverage-only assertions;
- parsers, protocols, schedulers, queues, caches, resolvers, clients, and
  persistence code are bounded under hostile input;
- all concurrent behavior is race-free, cancellation-aware, leak-free, and
  explicit about ownership;
- errors remain typed, inspectable, wrapped correctly, and safely redacted;
- formal specification claims have normative traceability and conformance
  fixtures;
- benchmarks cover representative and adversarial behavior;
- CI and local tooling execute equivalent rules;
- release dry-runs produce valid module-prefixed tags in dependency order;
- clean external consumers resolve every proposed release without a workspace
  or local replacement.

## Cross-Package Audits

Exercise integrations that unit tests inside one module cannot prove:

- authentication with authorization and service middleware;
- queue with scheduler, lease, idempotency, telemetry, outbox, webhook, and
  queue control plane;
- PostgreSQL with migrations, outbox, lease, idempotency, and rate limiting;
- HTTP client with circuit breaker, authentication, cache, telemetry, and
  middleware;
- config with wire formats, secrets, filesystem, and dynamic refresh;
- calendar, clock, temporal, opening hours, and scheduler across DST and
  timezone transitions;
- JSON Schema with OpenRPC, API query, configuration, and optional service
  integration once implemented;
- JSON:API, JSON-RPC, OpenRPC, and wire packages at real transport boundaries.

Add compatibility harnesses only where they prove real consumer contracts.
Do not create broad framework glue merely to increase integration coverage.

## Verification Standard

Run the root equivalents of:

- formatting and module metadata validation;
- all isolated unit and integration tests;
- workspace integration tests;
- race tests;
- fuzz smoke tests and package-specific extended fuzzing;
- meaningful coverage and mutation checks;
- static analysis, vulnerability, security, license, and secret checks;
- documentation, examples, API compatibility, and conformance checks;
- representative and adversarial benchmarks;
- release dry-runs and clean-consumer resolution tests.

All results MUST be attributable to modules. A single aggregate percentage or
workflow status is insufficient.

## Required Deliverables

1. A traceability matrix for every root and package goal.
2. A hardening report with severity, evidence, impact, and disposition.
3. Focused regressions and fixes for every accepted finding.
4. Updated package and root documentation and changelogs.
5. A release-readiness report listing exact commands and results.

## Completion Criteria

This goal is complete only when every high- and medium-severity finding is
fixed or rejected with evidence, all required local gates pass without
unexplained skips, clean consumers resolve proposed modules, no goal remains
unaccounted for, and the final report does not overstate readiness.
