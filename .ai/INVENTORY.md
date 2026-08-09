# Goal Execution Inventory

## Purpose

This file is the source of truth for goals that still require execution,
re-execution, hardening, or recurring maintenance. Generated manifests may
inventory goal files and nearby implementation artifacts, but they MUST NOT
infer execution or verification from the presence of a README, changelog,
source file, test, benchmark, or historical commit.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Status Model

| Status | Meaning |
| --- | --- |
| `pending` | The goal has not been completed against its current requirements. |
| `pending-reexecution` | An implementation or earlier campaign exists, but the current goal is new, changed, or applies to changed downstream scope. |
| `in-progress` | An execution campaign is active, but its complete acceptance contract has not passed. |
| `implemented-unverified` | Required implementation is present, but final goal-specific evidence is incomplete, unavailable, stale, or failing. |
| `verified` | Every current requirement and mandatory gate has current, scoped evidence. |
| `blocked` | Progress cannot continue without a named external dependency, decision, credential, service, or environment. |
| `recurring` | The goal defines continuing maintenance rather than a one-time campaign. |
| `superseded` | A named replacement goal owns the remaining requirements. |

Uncommitted work, elapsed time, implementation-file presence, test-file
presence, coverage without meaningful assertions, and a green unrelated CI run
MUST NOT advance a status.

## Execution Rules

- Execute entries in ascending order unless a documented dependency change
  justifies a different order.
- For a paired package entry, execute `GOAL.md` first and
  `GOAL_HARDEN.md` immediately afterward before advancing to dependents.
- For supplemental pairs, execute the base supplement before its hardening
  supplement.
- Record material evidence and status changes immediately when received. Do
  not defer inventory updates until the end of a long campaign.
- Verification state MUST be tied to the applicable requirement and content
  identity, not solely to a branch name, commit hash, tag, or mutable Git
  history. Rebase, force-push, history repair, or repository relocation MUST
  NOT invalidate unchanged evidence.
- A code or requirement change invalidates only evidence affected by that
  change. It MUST NOT restart unrelated package campaigns.
- Git commits and CI URLs MAY be recorded as navigation aids, but they MUST NOT
  be the only proof.
- Evidence MUST identify the goal requirement, affected package/content,
  command or proving artifact, result, relevant environment, and observation
  time.
- External-service, interoperability, performance, race, fuzz, security, and
  mutation claims MUST retain the environment and configuration needed to
  understand their scope.
- Exactly 100% statement coverage and exactly 100% of viable mutants killed by
  meaningful tests remain mandatory wherever required by the referenced goal.
- `blocked`, skipped, flaky, unavailable, and partially verified boundaries
  MUST remain explicit.

## Pending Execution Order

The pair notation `path/{A,B}` means both named files in the listed order.

| Order | Phase | Status | Execution unit | Depends on |
| ---: | --- | --- | --- | --- |
| 1 | Decisions | `pending-reexecution` | `.ai/GOAL_SPECIFICATION_DECISIONS.md` | Current specifications and package inventory |
| 2 | Architecture | `pending` | `.ai/GOAL_RESILIENCE.md` | 1 |
| 3 | Composition | `pending` | `pkg/resilience/.ai/{GOAL.md,GOAL_HARDEN.md}` | 2 |
| 4 | Primitive | `pending-reexecution` | `pkg/semaphore/.ai/{GOAL.md,GOAL_HARDEN.md}` | 3 |
| 5 | Primitive | `pending-reexecution` | `pkg/bulkhead/.ai/{GOAL.md,GOAL_HARDEN.md}` | 4 |
| 6 | Primitive | `pending-reexecution` | `pkg/concurrency-limit/.ai/{GOAL.md,GOAL_HARDEN.md}` | 3-5 |
| 7 | Primitive | `pending-reexecution` | `pkg/rate-limit/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3 |
| 8 | Primitive | `pending-reexecution` | `pkg/retry/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3, 7 |
| 9 | Primitive | `pending-reexecution` | `pkg/circuit-breaker/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3 |
| 10 | Primitive | `pending-reexecution` | `pkg/adaptive-throttle/.ai/{GOAL.md,GOAL_HARDEN.md}` | 6, 7, 9 |
| 11 | Primitive | `pending-reexecution` | `pkg/hedge/.ai/{GOAL.md,GOAL_HARDEN.md}` | 3, 6, 8 |
| 12 | Integration | `pending-reexecution` | `pkg/cache/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3, 8 |
| 13 | Integration | `pending-reexecution` | `pkg/http-client/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3, 6-11 |
| 14 | Verification | `pending-reexecution` | `pkg/fault-injection/.ai/{GOAL.md,GOAL_HARDEN.md}` | 3-13 |
| 15 | Fleet | `pending-reexecution` | `pkg/feature-flags/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3, 7-9, 12 |
| 16 | Fleet | `pending-reexecution` | `pkg/settings/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3, 7-9, 12 |
| 17 | Fleet | `pending-reexecution` | `pkg/sequencer/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3-9 |
| 18 | Fleet | `pending-reexecution` | `pkg/service/.ai/{GOAL_RESILIENCE.md,GOAL_RESILIENCE_HARDEN.md}` | 3-17 |
| 19 | Security | `pending-reexecution` | `pkg/secret-envelope/.ai/{GOAL.md,GOAL_HARDEN.md}` | 1 |
| 20 | Security | `pending-reexecution` | `pkg/secret-store/adapters/awssecretsmanager/.ai/{GOAL.md,GOAL_HARDEN.md}` | 19 |
| 21 | Authentication | `pending-reexecution` | `pkg/authentication/jwt/.ai/{GOAL.md,GOAL_HARDEN.md}` | 1, 13 |
| 22 | Authentication | `pending-reexecution` | `pkg/authentication/oidc/.ai/{GOAL.md,GOAL_HARDEN.md}` | 21 |
| 23 | Authentication | `pending-reexecution` | `pkg/authentication/authotel/.ai/{GOAL.md,GOAL_HARDEN.md}` | 21, 22 |
| 24 | Kafka | `pending-reexecution` | `pkg/kafka/adapters/mskiam/.ai/{GOAL.md,GOAL_HARDEN.md}` | 13, 19 |
| 25 | Kafka | `pending-reexecution` | `pkg/kafka/adapters/gotelemetry/.ai/{GOAL.md,GOAL_HARDEN.md}` | 18 |
| 26 | Kafka | `pending-reexecution` | `pkg/kafka/kafkaservice/.ai/{GOAL.md,GOAL_HARDEN.md}` | 18, 24, 25 |
| 27 | Queue | `pending-reexecution` | `pkg/queue/queueservice/.ai/{GOAL.md,GOAL_HARDEN.md}` | 18 |
| 28 | Event sourcing | `pending-reexecution` | `pkg/event-sourcing/postgres/.ai/{GOAL.md,GOAL_HARDEN.md}` | 1 |
| 29 | Event sourcing | `pending-reexecution` | `pkg/event-sourcing/adapters/gooutbox/.ai/{GOAL.md,GOAL_HARDEN.md}` | 28 |
| 30 | Outbox | `pending-reexecution` | `pkg/outbox/adapters/gokafka/.ai/{GOAL.md,GOAL_HARDEN.md}` | 24-26, 29 |
| 31 | Outbox | `pending-reexecution` | `pkg/outbox/adapters/goqueue/.ai/{GOAL.md,GOAL_HARDEN.md}` | 27, 29 |
| 32 | Outbox | `pending-reexecution` | `pkg/outbox/adapters/gotelemetry/.ai/GOAL_HARDEN.md` | 18, 29-31 |
| 33 | Event sourcing | `pending-reexecution` | `pkg/event-sourcing/adapters/gokafka/.ai/{GOAL.md,GOAL_HARDEN.md}` | 24-26, 28 |
| 34 | Event sourcing | `pending-reexecution` | `pkg/event-sourcing/adapters/goqueue/.ai/{GOAL.md,GOAL_HARDEN.md}` | 27, 28 |
| 35 | Event sourcing | `pending-reexecution` | `pkg/event-sourcing/adapters/gotelemetry/.ai/{GOAL.md,GOAL_HARDEN.md}` | 18, 28, 33, 34 |
| 37 | Leaf adapter | `pending-reexecution` | `pkg/rule-engine/adapters/gomath/.ai/{GOAL.md,GOAL_HARDEN.md}` | 1 |
| 39 | Leaf adapter | `pending-reexecution` | `pkg/rule-engine/adapters/gotemporal/.ai/{GOAL.md,GOAL_HARDEN.md}` | 1 |
| 40 | Hardening | `pending` | `pkg/external-sort/.ai/GOAL_HARDEN.md` | Existing external-sort implementation |
| 41 | Protocol | `pending` | `pkg/http-signature/.ai/{GOAL.md,GOAL_HARDEN.md}` | 1, 13, 19 |
| 42 | Security | `pending` | `pkg/capability/.ai/{GOAL.md,GOAL_HARDEN.md}` | 19, 21-23, 41 |
| 43 | Isolation | `pending` | `pkg/tenancy/.ai/{GOAL.md,GOAL_HARDEN.md}` | 18, 21-23 |
| 44 | Audit | `pending` | `pkg/audit/.ai/{GOAL.md,GOAL_HARDEN.md}` | 18, 29-32, 43 |
| 45 | Event contracts | `pending` | `pkg/schema-registry/.ai/{GOAL.md,GOAL_HARDEN.md}` | 24-26 |
| 46 | Event interoperability | `pending` | `pkg/cloudevents/.ai/{GOAL.md,GOAL_HARDEN.md}` | 24-35, 43, 45 |
| 47 | Durable orchestration | `pending` | `pkg/workflow/.ai/{GOAL.md,GOAL_HARDEN.md}` | 3-18, 27-35, 43, 44, 46 |
| 48 | Search | `pending` | `pkg/search/.ai/{GOAL.md,GOAL_HARDEN.md}` | 3-18, 29-32, 43 |
| 49 | Search adapter | `pending` | `pkg/search/adapters/opensearch/.ai/{GOAL.md,GOAL_HARDEN.md}` | 48 |
| 50 | Ecosystem cohesion | `pending` | `.ai/GOAL_COHESION.md` | 3-49 |
| 51 | Resilience audit | `pending` | `.ai/GOAL_RESILIENCE_HARDEN.md` | 3-50 |
| 52 | Repository audit | `pending-reexecution` | `.ai/GOAL_COMPATIBILITY.md` | 3-51 |
| 53 | Repository audit | `pending-reexecution` | `.ai/GOAL_SECURITY.md` | 3-52 |
| 54 | Repository audit | `pending-reexecution` | `.ai/GOAL_SUPPLY_CHAIN.md` | 3-53 |
| 55 | Repository audit | `pending-reexecution` | `.ai/GOAL_BENCHMARKS.md` | 3-54 |
| 56 | Repository audit | `pending-reexecution` | `.ai/GOAL_PERFORMANCE.md` | 55 |
| 57 | Repository audit | `pending-reexecution` | `.ai/GOAL_CODE_DOCUMENTATION.md` | 3-56 |
| 58 | Repository audit | `pending-reexecution` | `.ai/GOAL_DOCUMENTATION.md` | 50, 57 |
| 59 | Repository audit | `pending-reexecution` | `.ai/GOAL_POLISH.md` | 3-58 |
| 60 | Repository audit | `pending-reexecution` | `.ai/GOAL_MONOREPO_REMEDIATION.md` | 3-59 |
| 61 | Repository audit | `pending-reexecution` | `.ai/GOAL_HARDEN.md` | 3-60 |
| 62 | Repository audit | `pending-reexecution` | `.ai/GOAL.md` | 3-61 |
| 63 | Operational assurance | `pending` | `.ai/GOAL_OPERATIONAL_ASSURANCE.md` | 3-62 |
| 64 | Release | `pending` | `.ai/GOAL_RELEASE.md` | 3-63 and explicit release authority |

## Recurring And Previously Executed Goals

| Goal | Status | Treatment |
| --- | --- | --- |
| `.ai/GOAL_MAINTENANCE.md` | `recurring` | Execute its cadence continuously and before each supported-Go, dependency, security, specification, or release transition. |
| `pkg/outbox/adapters/gotelemetry/.ai/GOAL.md` | `verified` | The base goal formerly included in pending order 32 has current scoped implementation and mandatory gate evidence; its separate `GOAL_HARDEN.md` remains pending. |
| `pkg/knapsack/objective/gomoney/.ai/{GOAL.md,GOAL_HARDEN.md}` | `verified` | Former pending order 36. Current scoped evidence verifies exact-money behavior, hardening requirements, and every mandatory module gate; requeue only when that evidence becomes stale or requirements change. |
| `pkg/rule-engine/adapters/gomeasurement/.ai/{GOAL.md,GOAL_HARDEN.md}` | `verified` | Former pending order 38. Current scoped evidence verifies exact quantity encoding and comparison behavior, hardening requirements, and every mandatory module gate; requeue only when that evidence becomes stale or requirements change. |
| `.ai/GOAL_QUEUE_WORKER_BALANCING.md` | `implemented-unverified` | A subsequent implementation campaign exists; include it in the final repository and release audit rather than restarting it solely because this inventory was added. |
| `pkg/merkle-tree/.ai/{GOAL.md,GOAL_HARDEN.md}` | `implemented-unverified` | Subsequent implementation and conformance work exists; refresh only affected evidence and include it in final repository gates. |
| `pkg/merkle-patricia-trie/.ai/{GOAL.md,GOAL_HARDEN.md}` | `implemented-unverified` | Subsequent implementation, interoperability, persistence, and hardening work exists; refresh only affected evidence. |
| `pkg/verkle-tree/.ai/{GOAL.md,GOAL_HARDEN.md}` | `in-progress` | Current uncommitted work affects this package; its owner must update status and evidence when the active campaign reaches a stable boundary. |

### Exact-money knapsack objective evidence

| Field | Record |
| --- | --- |
| Goal | `pkg/knapsack/objective/gomoney/.ai/{GOAL.md,GOAL_HARDEN.md}` |
| Scope | Exact-money cost configuration, totals, comparisons, errors, documentation, and module quality gates. |
| Status | `pending-reexecution` to `verified` |
| Evidence | `./scripts/run-modules.sh check --modules pkg/knapsack/objective/gomoney` against the completed implementation. |
| Result | Passed every mandatory module gate, including 95/95 statements, 38/38 viable mutants, race, fuzz, API, docs, and benchmarks. |
| Environment | Go 1.26.5 on darwin/arm64 with a task-owned disposable `GOCACHE`; no external services. |
| Observed | 2026-08-09T02:56:01Z |
| Gaps | The repository-wide wrapper remains independently blocked by missing `pkg/audit/LICENSE`; the scoped module contract is complete. |
| Navigation | Implementation commit `91a2fd2124b24c8d03510fe86081e7a72c971102`. |

### Exact measurement rule operator evidence

| Field | Record |
| --- | --- |
| Goal | `pkg/rule-engine/adapters/gomeasurement/.ai/{GOAL.md,GOAL_HARDEN.md}` |
| Scope | Versioned exact-quantity encoding, compatible-dimension comparison, stable error causes, limits, documentation, and module quality gates. |
| Status | `pending-reexecution` to `verified` |
| Evidence | `./scripts/run-modules.sh check --jobs 1 --modules pkg/rule-engine/adapters/gomeasurement` against the completed implementation. |
| Result | Passed every mandatory module gate, including 51/51 statements, 24/24 viable mutants, race, fuzz, API, docs, and benchmarks. |
| Environment | Go 1.26.5 on darwin/arm64 with a task-owned disposable `GOCACHE`; no external services. |
| Observed | 2026-08-09T02:59:40Z |
| Gaps | None within the scoped module contract. |

All other historical package goals remain outside the pending queue unless a
requirement change, implementation change, failed gate, stale external claim,
or explicit audit finding moves them to `pending-reexecution` or
`implemented-unverified`.

## Evidence Record Template

When an entry changes status, add or link a durable record containing:

| Field | Required content |
| --- | --- |
| Goal | Exact goal path and requirement identity |
| Scope | Package and affected behavior/content identity |
| Status | Previous and new inventory status |
| Evidence | Exact command, artifact, scenario, or external result |
| Result | Passed, failed, blocked, skipped, flaky, or unavailable |
| Environment | Relevant Go, OS, architecture, service, dependency, and configuration versions |
| Observed | UTC timestamp when the result became available |
| Gaps | Every remaining unverified or excluded boundary |
| Navigation | Optional commit, CI run, issue, or pull-request references |

Inventory updates MUST be committed with the implementation or evidence batch
that justified the transition. A later unrelated commit MUST NOT force
re-execution or erase previously valid scoped evidence.
