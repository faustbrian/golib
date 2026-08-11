# Cohesion Baseline Audit

This report is the reviewed phase-one baseline for the Golib cohesion goal. It
measures the repository before public API or module-path normalization. The
generated `modules.json`, `packages.json`, package catalog, API baselines, and
module documentation remain the detailed source evidence.

## Measured Scope

The baseline contains 134 Go modules and 684 Go packages. Of those, 107 are
independently releasable libraries or adapters. All releasable modules target
Go 1.26.5, are currently unpublished, and plan their first public release as
`v1.0.0`.

The repository already has strong shared engineering policy, isolated module
boundaries, independent release tags, exact quality gates, generated dependency
metadata, and explicit specification governance. The remaining cohesion work is
consumer-facing: taxonomy, construction and lifecycle language, adapter names,
entry-point documentation, supported compositions, and known-good version sets.

## Family Inventory

Every releasable module has one primary navigation family. Families do not add
import namespaces or dependency layers.

### Foundations

`clock`, `config`, `config/adapters/awssecretsmanager`, `correlation`,
`identifier`, `international`, `localized`, `tenancy`, and `validation` own
common immutable values, deterministic seams, configuration, identity
propagation, and input validation.

### Service Edge

`api-query`, `authentication`, `authentication/authotel`,
`authentication/jwt`, `authentication/oidc`, `authorization`, `capability`,
`http-middleware`, `password`, `router`, and `service` own request-edge policy,
identity, access decisions, routing, middleware, and process lifecycle.

### Protocols And Descriptions

`cloudevents`, `cloudevents/adapters/golib`, `http-signature`, `json-schema`,
`jsonapi`, `jsonrpc`, `openapi`, `openrpc`, `schema-registry`,
`schema-registry/providers/confluent`, `schema-registry/providers/glue`,
`webhook`, `wire`, `wsdl`, and `xsd` own standards, envelopes, schemas,
descriptions, signatures, and wire interoperability.

### Persistence And Durability

`audit`, `audit/postgres`, `cache`, `event-sourcing`,
`event-sourcing/adapters/gokafka`, `event-sourcing/adapters/outbox`,
`event-sourcing/adapters/queue`, `event-sourcing/adapters/gotelemetry`,
`event-sourcing/postgres`, `feature-flags`, `idempotency`, `lease`,
`migrations`, `outbox`, `outbox/adapters/gokafka`,
`outbox/adapters/queue`, `outbox/adapters/gotelemetry`, `postgres`, `queue`,
`queue-control-plane`, `queue/queueservice`, `scheduler`, `sequencer`,
`settings`, `state-machine`, and `workflow` own durable state, delivery,
coordination, recovery, and operational lifecycle.

### Resilience

`adaptive-throttle`, `bulkhead`, `circuit-breaker`, `concurrency-limit`,
`fault-injection`, `hedge`, `rate-limit`, `resilience`, `retry`, and `semaphore`
own explicit admission, isolation, retry, amplification, and failure policies.

### Observability

`log` and `telemetry` own structured logging extensions and OpenTelemetry
runtime lifecycle. Core domain modules continue to expose narrow observation
hooks instead of depending on these modules by default.

### Integration And Data Movement

`external-sort`, `filesystem`, `http-client`, `kafka`,
`kafka/adapters/gotelemetry`, `kafka/adapters/mskiam`, `kafka/kafkaservice`,
`search`, `search/adapters/opensearch`, `secret-envelope`,
`secret-store/adapters/awssecretsmanager`, and `tabular` own bounded external
I/O, data movement, provider boundaries, and derived search state.

### Domain Utilities

`barcode`, `calendar`, `ecma-regexp`, `geo`, `keyphrase`, `knapsack`,
`knapsack/objective/gomoney`, `math`, `measurement`,
`merkle-patricia-trie`, `merkle-tree`, `money`, `opening-hours`, `rule-engine`,
`rule-engine/adapters/math`, `rule-engine/adapters/measurement`,
`rule-engine/adapters/temporal`, `temporal`, and `verkle-tree` own
domain-specific immutable values, algorithms, parsers, proofs, and calculations.

### Tooling

`analysis`, `cli`, and `prompts` own static policy, command construction, and
interactive terminal behavior.

## Construction Audit

The exported API scan shows five meaningful construction groups.

| Style | Modules | Decision |
| --- | --- | --- |
| Value and parser constructors | `barcode`, `calendar`, `cloudevents`, `correlation`, `ecma-regexp`, `geo`, `identifier`, `international`, `json-schema`, `jsonapi`, `keyphrase`, `localized`, `math`, `measurement`, `money`, `opening-hours`, `temporal`, `validation`, `wire`, `wsdl`, `xsd` | Keep domain-specific `New`, parse, compile, and value functions. Do not add lifecycle or functional options for symmetry. |
| Immutable compile or build | `api-query`, `cli`, `ecma-regexp`, `json-schema`, `jsonrpc`, `knapsack`, `migrations`, `openapi`, `openrpc`, `router`, `rule-engine`, `schema-registry`, `state-machine`, `verkle-tree`, `workflow` | Mutable registration may precede an explicit immutable compile/build boundary. Document duplicate and conflict behavior. |
| Process-local runtime policy | `adaptive-throttle`, `bulkhead`, `circuit-breaker`, `concurrency-limit`, `fault-injection`, `hedge`, `http-middleware`, `rate-limit`, `resilience`, `retry`, `semaphore` | Validate fully before use; expose bounds, concurrency safety, and drain or shutdown only when resources are retained. |
| Resource-owning or durable runtime | `audit`, `cache`, `event-sourcing`, `feature-flags`, `filesystem`, `http-client`, `idempotency`, `kafka`, `lease`, `log`, `outbox`, `postgres`, `queue`, `queue-control-plane`, `scheduler`, `search`, `sequencer`, `service`, `settings`, `telemetry` | I/O and ownership must be visible through `Open`, `Load`, `Run`, or an explicitly documented constructor. Cleanup and unknown outcomes are part of the API. |
| Focused adapter | Every independently releasable adapter and provider module | Keep construction thin, perform no import-time registration, and leave the target resource lifecycle with the caller unless the adapter explicitly opens it. |

`authentication`, `authorization`, `capability`, `password`, and `tenancy`
combine immutable policy with request-edge runtimes. Their documentation must
distinguish compiled policy from retained caches, remote key sources, or durable
stores rather than forcing the complete module into one construction label.

## Lifecycle Audit

The shared vocabulary is now frozen in the design language. Current modules
fall into these ownership classes:

- Value-only modules own no background lifecycle and return immutable values or
  compiled plans.
- Process-local policies may own bounded state but no external resource. They
  expose shutdown only when waiters, observers, timers, or workers require it.
- Backend adapters own a connection only when they open it. Adapters receiving a
  caller-owned client, pool, transaction, producer, or store do not close it.
- Service, queue, scheduler, sequencer, workflow, telemetry, and control-plane
  runtimes own explicit run, drain, and shutdown ordering.
- PostgreSQL transaction helpers preserve caller ownership and never commit an
  outer transaction implicitly.

The API inventory found mixed uses of `Run`, `Start`, `Open`, and `New`, but no
evidence supports mechanically renaming all constructors. Remediation must
target only operations whose name conceals I/O, ownership transfer, or retained
background work.

## Error Audit

Packages consistently use ordinary Go errors, but equivalent outcome categories
are not yet documented in one place. The design language freezes the common
classification rules without introducing a shared error package.

The audit requires focused follow-up where an abstraction can currently leak a
backend error, where retries may collapse unknown outcomes into failures, or
where aggregate errors can retain unbounded values. Domain-owned sentinels and
structured errors remain preferred over a universal taxonomy type.

## Adapter Naming Findings

The following unpublished paths were reviewed against the target-oriented
adapter scheme. Unresolved items require atomic pre-v1 remediation after active
specialist work has finished:

| Current path | Intended target | Classification |
| --- | --- | --- |
| `event-sourcing/adapters/gokafka` | `event-sourcing/adapters/kafka` | Naming debt |
| `event-sourcing/adapters/gooutbox` | `event-sourcing/adapters/outbox` | Resolved before v1; no published tag depended on the old path. |
| `event-sourcing/adapters/goqueue` | `event-sourcing/adapters/queue` | Resolved before v1; no published tag depended on the old path. |
| `event-sourcing/adapters/gotelemetry` | `event-sourcing/adapters/otel` or `telemetry` after dependency audit | Unresolved target |
| `kafka/adapters/gotelemetry` | `kafka/adapters/otel` | Naming debt; specialist-owned scope |
| `kafka/kafkaservice` | `kafka/adapters/service` | Layout debt; specialist-owned scope |
| `outbox/adapters/gokafka` | `outbox/adapters/kafka` | Naming debt |
| `outbox/adapters/goqueue` | `outbox/adapters/queue` | Resolved before v1; no published tag depended on the old path. |
| `outbox/adapters/gotelemetry` | `outbox/adapters/otel` | Naming debt |
| `queue/queueservice` | `queue/adapters/service` | Layout debt |
| `rule-engine/adapters/gomath` | `rule-engine/adapters/math` | Resolved before v1; no tags or Track, Postal, Location, Mono, or API consumers used the old path. |
| `rule-engine/adapters/gomeasurement` | `rule-engine/adapters/measurement` | Resolved before v1; no tags or Track, Postal, Location, Mono, or API consumers used the old path. |
| `rule-engine/adapters/gotemporal` | `rule-engine/adapters/temporal` | Resolved before v1; no tags or Track, Postal, Location, Mono, or API consumers used the old path. |
| `authentication/authotel` | `authentication/adapters/otel` | Layout debt |

`cloudevents/adapters/golib` is not target-oriented. It spans several Golib
contracts and must either be decomposed by actual target or retain a documented
ecosystem-bridge exception. `schema-registry/providers/*` is intentionally a
provider family rather than a generic adapter label because provider identity
is part of the public schema-registry contract.

## Package Identifier Findings

Module directories and default package identifiers need not be textually
identical when Go naming or domain terminology makes a different identifier
clear. The following differences are currently intentional candidates rather
than automatic defects:

- `adaptive-throttle` imports as `throttle`;
- `circuit-breaker` imports as `breaker`;
- `ecma-regexp` imports as `ecmascript`;
- `math` imports as `gomath` to avoid collision with the standard library;
- `merkle-patricia-trie` imports as `mpt`;
- `queue-control-plane` imports as `controlplane`.

Each must be recorded in catalog metadata so consumers do not discover the
identifier by trial. Modules that are intentionally collections of subpackages,
including `analysis` and `barcode`, must list their public entry points instead
of inventing an empty root package.

## Documentation Findings

All releasable modules have a README and changelog, but entry-point structure is
not yet uniform enough for ecosystem navigation. The next phase must validate
purpose, ownership boundary, Go version, installation, executable quick start,
subpackage map, construction, errors, concurrency, security, compatibility,
performance, troubleshooting, and root backlinks without requiring irrelevant
empty headings.

At baseline, the generated package catalog mixed consumer modules with
engineering harness detail. The catalog now has separate consumer and
engineering views backed by exact family metadata. Package-local workflow
badges and old standalone repository links remain objective drift candidates
for automated checks.

## Decisions

1. Golib remains a set of independently versioned modules, not an umbrella
   framework or lockstep release train.
2. Package families are navigation metadata, not import namespaces.
3. Standard-library types remain the cross-module integration language.
4. Construction and lifecycle semantics are normalized by meaning, not by
   forcing identical method names.
5. Optional integrations use target-oriented nested modules.
6. Known-good compatibility sets supplement independent SemVer; they do not
   replace it.
7. Existing verified runtime evidence remains valid across documentation and
   history-only changes when its complete input fingerprint is unchanged.

## Residual Decisions

- Choose `otel` versus `telemetry` for each current `gotelemetry` adapter by
  inspecting whether it targets OpenTelemetry directly or the Golib telemetry
  module.
- Decide whether `cloudevents/adapters/golib` is decomposed or retained as a
  narrow documented bridge.
- Decide whether service lifecycle adapters use `adapters/service` uniformly
  when their current module path is unpublished but active specialist work is
  in progress.
- Define the first non-installable compatibility set and its content identity
  after the catalog metadata schema exists.

No public rename begins until its consumer inventory, dependency closure,
replacement path, API baseline, changelog impact, and verification scope are
recorded.
