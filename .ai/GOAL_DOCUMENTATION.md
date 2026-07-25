# Goal: Build The Go Libraries Documentation Portal

## Mission

Create a cohesive repository-wide documentation system for `golib` so a
new user can discover the ecosystem, select the right packages, understand how
they compose, evaluate tradeoffs, install compatible versions, and operate a
production service without reading source or searching every module manually.

This is an implementation goal, not a documentation inventory. Inspect the
live repository, verify every claim against current APIs and behavior, create
or update the actual root README and documentation, repair package links, add
runnable examples, validate all documentation, and finish with exact evidence.

## Documentation Principles

- The root repository MUST provide the canonical entry point.
- Documentation MUST be task-oriented before it is package-oriented.
- Every recommendation MUST explain context, alternatives, tradeoffs, and
  exceptions.
- Intentional product boundaries MUST be distinguished from missing features.
- Actual limitations MUST be stated plainly with impact and workaround.
- Implemented, experimental, planned, deprecated, and unavailable capabilities
  MUST never be presented as equivalent.
- Examples MUST use real current module paths and public APIs.
- Root guidance MUST link to package detail rather than duplicating content that
  will drift.
- Generated indexes MAY supplement documentation but MUST NOT replace reviewed
  explanatory guidance.
- A documentation website MAY be generated later, but canonical Markdown MUST
  remain complete and usable directly on GitHub and pkg.go.dev.

## Audiences

Write explicit paths for:

- a developer evaluating one package;
- a team building a new API or RPC service;
- a team building an ingester, processor, worker, or scheduler;
- an existing Laravel/PHP team migrating incrementally;
- an operator deploying to Kubernetes;
- an open-source contributor implementing or extending a package;
- a security or architecture reviewer evaluating guarantees and boundaries.

## Root README

Create a production-quality root `README.md` containing:

- one-paragraph ecosystem purpose and design philosophy;
- current maturity and stability statement;
- a problem-oriented package catalog with status and concise purpose;
- a five-minute path to a minimal service;
- links to selection guides, recommended stacks, recipes, operations,
  architecture, compatibility, security, benchmarks, and contribution docs;
- workspace and independent-module installation instructions;
- monorepo release and prefixed-tag explanation;
- supported Go version policy;
- honest workflow, documentation, security, and release badges;
- links to pkg.go.dev and package READMEs only when targets exist;
- a clear statement that consumers import only the modules they need.

The README MUST remain scannable. Detailed matrices and recipes belong in
`docs/` and must be linked from the README.

## Information Architecture

Create a navigable root `docs/` hierarchy with at least:

- `docs/index.md`: complete documentation map and audience entry points;
- `docs/packages.md`: authoritative human-readable package catalog;
- `docs/choosing-packages.md`: problem-to-package decision guide;
- `docs/recommended-stacks.md`: supported package combinations;
- `docs/api-protocols.md`: JSON-RPC, JSON:API, OpenAPI, OpenRPC, webhooks, and
  raw HTTP selection guidance;
- `docs/architecture.md`: dependency direction, package ownership, optional
  modules, and composition roots;
- `docs/integration-map.md`: package interoperability and adapter ownership;
- `docs/recipes/`: complete runnable multi-package scenarios;
- `docs/migration/`: Laravel/PHP and standalone Go migration guidance;
- `docs/operations/`: Kubernetes, configuration, secrets, telemetry, queues,
  migrations, shutdown, and failure handling;
- `docs/comparisons/`: fair alternatives and tradeoff analyses;
- `docs/status.md`: implemented, experimental, planned, and blocked work;
- `docs/versioning.md`: independent modules, compatibility, and release tags;
- `docs/glossary.md`: stable ecosystem terminology.

Every page MUST have a clear parent/index link and related-next-step links.
Avoid orphan pages and circular navigation that offers no entry point.

## Package Catalog

For every top-level package and nested releasable module, document:

- import path and current lifecycle status;
- one-sentence responsibility;
- what it deliberately does not own;
- primary use cases and when not to use it;
- required and optional dependencies;
- important companion packages;
- current stable version or unreleased status;
- minimum Go version;
- specification or backend support where relevant;
- links to README, API docs, adoption, security, compatibility, performance,
  changelog, and pkg.go.dev;
- implementation and hardening evidence status.

Generate machine-readable catalog data where practical and validate human
documentation against it. Human recommendations MUST remain reviewed prose.

## Choosing API Protocols

Provide a nuanced decision guide rather than a universal rule.

### JSON-RPC

Recommend JSON-RPC when the API is operation-oriented, command/query-shaped,
has a controlled client population, benefits from explicit method names or
batches, or is primarily service-to-service. Explain:

- why internal microservices are a common fit but not the only fit;
- notification, batch, idempotency, retry, transport, and error semantics;
- OpenRPC discovery and documentation integration;
- why JSON-RPC is not automatically appropriate for public resource browsing,
  generic HTTP tooling, caching, or third-party REST expectations.

### JSON:API

Recommend JSON:API when the API is resource-oriented and consumers benefit from
standardized relationships, compound documents, sparse fields, filtering,
sorting, pagination, errors, profiles, or extensions. Explain:

- why customer-facing and partner-facing resource APIs are a common fit;
- when internal resource APIs also benefit;
- query complexity, payload shape, caching, Atomic Operations, and extension
  tradeoffs;
- why JSON:API is awkward for action-heavy workflows that do not map cleanly
  to resources.

### Raw HTTP And OpenAPI

Explain when conventional HTTP endpoints with OpenAPI are preferable, including
ecosystem interoperability, generated clients, gateways, browser/tool support,
streaming, file transfer, and mixed media types. Clarify that OpenAPI describes
an HTTP API and is not itself a runtime serialization protocol.

### Webhooks And Event Delivery

Explain outbound asynchronous notifications, signatures, replay protection,
retries, idempotency, ordering, and when a queue or event bus is more suitable.

### Multiple Protocols

Document that one service MAY expose JSON-RPC for internal commands, JSON:API
or conventional OpenAPI-described HTTP for external resources, and webhooks for
outbound events. Show how to share application use cases without sharing
transport models or leaking protocol concerns into domain code.

Include a decision table and flowchart whose outcomes are supported by prose,
not simplistic labels such as “internal equals RPC” and “external equals REST.”

## Recommended Package Combinations

Document supported stacks with explicit dependency direction and middleware
order. Include at least:

- minimal HTTP service;
- internal JSON-RPC service;
- external JSON:API service;
- conventional OpenAPI-described HTTP service;
- mixed-protocol service;
- queue producer and worker;
- ingester and processor;
- scheduled singleton task;
- PostgreSQL transactional service with migrations and outbox;
- webhook sender and receiver;
- vendor API client with retries, rate limits, circuit breaker, cache, and
  telemetry;
- file ingestion from local, SFTP, object storage, and R2;
- configuration with Kubernetes/Infisical integration;
- authenticated and authorized service;
- observable Kubernetes deployment.

Each stack MUST identify required modules, optional modules, initialization
order, request/job lifecycle, shutdown order, ownership boundaries, failure
semantics, and links to complete recipes.

## Integration And Ownership Map

Create diagrams and tables showing which package owns each concern:

- lifecycle, routing, middleware, authentication, authorization;
- protocol parsing and description;
- PostgreSQL, migrations, cache, queue, leases, idempotency, outbox;
- logging, telemetry, configuration, scheduling;
- filesystems, tabular formats, wire formats, time, geography, localization;
- HTTP clients, retries, rate limiting, circuit breaking, and webhooks.

Document adapter direction and prevent circular “everything integrates with
everything” guidance. Make clear which package initializes infrastructure and
which package only accepts an interface or standard-library type.

## Comparisons And Tradeoffs

Create fair comparison pages for important adoption decisions. Cover at least:

- owned service/router/middleware versus plain `net/http`, Chi, Gin, Echo, and
  Fiber/fasthttp;
- `log`/slog versus Zap and Zerolog;
- owned JSON Schema versus santhosh-tekuri/jsonschema and other maintained
  peers;
- owned OpenAPI versus kin-openapi, libopenapi, and openapi tooling;
- owned JSON-RPC and JSON:API versus maintained alternatives;
- owned queue, cache, PostgreSQL, migration, and HTTP client layers versus
  direct clients and major alternatives.

Every comparison MUST distinguish:

- equivalent capability from adjacent capability;
- measured fact from inference;
- correctness and conformance from popularity;
- abstraction overhead from full-feature behavior;
- intentional non-goal from current limitation;
- missing feature from rejected hidden magic;
- benchmark result from production recommendation.

Do not claim “faster,” “safer,” “simpler,” “complete,” or “compatible” without
linked current evidence. Link raw benchmark methodology and conformance results.

## Caveats And Limitations

Maintain a visible ecosystem limitations register. For every material caveat,
record:

- affected package and versions;
- user-visible impact;
- whether it is intentional boundary, temporary limitation, unsupported
  platform/specification/backend, or known defect;
- rationale and alternatives;
- workaround or migration path;
- tracking issue or revisit condition.

Do not bury limitations only in package FAQs. Root selection guides MUST surface
limitations that could change an architectural decision.

## Runnable Recipes

Recipes MUST be complete enough to execute and MUST include:

- module imports and compatible versions;
- configuration structs and environment expectations;
- construction and dependency wiring;
- contexts, cancellation, cleanup, and shutdown;
- errors, retries, timeouts, and limits;
- tests demonstrating the documented behavior;
- Kubernetes notes where relevant;
- links to package-level detail.

Compile and test every Go snippet. Prefer executable examples or dedicated
example modules over copied snippets. Never use pseudocode without labeling it.

## Migration Documentation

Provide concept maps and staged migration guidance from Laravel/PHP, including:

- Horizon to queue and queue-control-plane responsibilities;
- middleware and router composition without service-container magic;
- Eloquent/query behavior to pgx/sqlc and explicit persistence seams;
- Laravel cache, scheduler, filesystem, HTTP client, logging, validation,
  authentication, authorization, dates, and configuration equivalents;
- migrations without replaying existing Laravel migration history;
- incremental coexistence, database compatibility, payload compatibility, and
  rollback boundaries.

State where no direct equivalent exists because the Go design intentionally
avoids framework behavior.

## Operations Documentation

Document production composition for Kubernetes and local development:

- process model, probes, graceful shutdown, and resource requests;
- Valkey, PostgreSQL, queues, dead letters, retries, outbox, and idempotency;
- logs, metrics, traces, correlation, sampling, and cardinality;
- configuration, secret injection, refresh, and Infisical ownership;
- migrations as controlled jobs;
- rate limits, circuit breakers, backpressure, and overload behavior;
- capacity planning and benchmark interpretation;
- upgrade, rollback, incident, and compatibility procedures.

## Package Documentation Consistency

Audit every package README and docs tree. Standardize discoverability without
forcing identical prose. Every implemented public package MUST expose:

- purpose, status, install, quick start, package map, and API overview;
- when to use and when not to use;
- integrations and ownership boundaries;
- security, compatibility, performance, migration, troubleshooting, FAQ,
  examples, cookbook, and changelog links where applicable;
- links back to the root ecosystem documentation;
- accurate badges and current module paths.

Planned packages MUST have visibly planned documentation and MUST NOT present
installation or release instructions as if implementation exists.

## Validation And Automation

Provide locally runnable and CI-enforced documentation checks for:

- Markdown formatting and style;
- internal and external link integrity with deterministic exceptions;
- anchors, navigation, and orphan pages;
- valid module paths, package names, versions, and pkg.go.dev links;
- executable Go examples and snippets;
- generated catalog drift;
- duplicate or contradictory recommendations;
- stale status, compatibility, benchmark, specification, and release claims;
- required pages and package-to-root backlinks;
- spelling and terminology allowlists;
- accidental secrets, private URLs, local paths, and production identifiers.

Checks MUST be runnable through documented root `make` targets and represented
accurately in CI and README badges.

## Execution Plan

1. Inventory current root and package documentation, links, claims, examples,
   statuses, and gaps.
2. Define information architecture, terminology, catalog schema, and ownership.
3. Build root README, documentation index, package catalog, and status pages.
4. Build package-selection, API-protocol, recommended-stack, architecture, and
   integration guidance.
5. Build runnable recipes, migration guidance, operations guidance,
   comparisons, caveats, and limitations.
6. Repair package READMEs and backlinks without duplicating canonical detail.
7. Implement documentation validation, example compilation, link checks, and
   CI/local commands.
8. Perform an adoption walkthrough as each target audience and close every
   navigation or comprehension gap.

## Acceptance Criteria

- A new user can choose and install the correct package from the root README.
- Protocol guidance explains JSON-RPC, JSON:API, OpenAPI/raw HTTP, and webhooks
  without simplistic internal/external rules.
- Recommended stacks show correct initialization, ownership, ordering, and
  shutdown across real package combinations.
- Every package and detailed page is reachable from a clear entry point and
  links back appropriately.
- Intentional boundaries, current limitations, tradeoffs, and alternatives are
  explicit and evidence-backed.
- All examples compile and all documentation checks pass locally and in CI.
- No stale module path, misleading badge, unsupported claim, orphan page, or
  undocumented material caveat remains.
