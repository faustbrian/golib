# Goal: Make Golib A Cohesive Library Ecosystem

## Mission

Turn `golib` into a deliberately cohesive set of independently adoptable Go
libraries. A user who learns one package SHOULD recognize the construction,
configuration, ownership, lifecycle, failure, observability, documentation,
and integration patterns used by the others, while each package retains the
API shape appropriate to its domain.

The desired result is a set of compatible Lego stones, not a framework. Golib
MUST provide one design language, predictable interoperability, and clear
adoption paths without introducing a service container, global application
object, mandatory bootstrap, umbrella runtime dependency, or hidden magic.

This goal owns ecosystem-level API and developer-experience cohesion. It MUST
execute before the repository documentation portal, final compatibility audit,
final hardening audit, operational assurance, and release.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174

## Product Position

Golib MUST be recognizable as one ecosystem through:

- a documented public design language;
- stable ownership and dependency-direction rules;
- predictable construction, configuration, validation, and lifecycle patterns;
- consistent failure, cancellation, resource, security, and observability
  semantics;
- target-oriented adapter naming and optional dependency boundaries;
- human package families and problem-oriented package selection;
- runnable, tested multi-package recipes;
- explicit tested compatibility sets for independently released modules;
- uniform package-level documentation entry points; and
- automation that detects drift without enforcing superficial sameness.

The useful Illuminate comparison is organizational and experiential: focused
components share terminology, expectations, integration conventions, and a
coherent documentation system. Golib MUST NOT reproduce Laravel's container,
facades, service providers, runtime discovery, mutable globals, framework
inheritance, model binding, or application magic.

## Non-Goals And Hard Prohibitions

This goal MUST NOT create:

- one umbrella module that imports the complete ecosystem;
- a mandatory `golib` runtime, application object, kernel, or bootstrap;
- a service container, locator, facade system, or global registry;
- a universal configuration object shared by unrelated packages;
- a generic `contracts` dumping ground for interfaces without one semantic
  owner;
- a universal error type that erases domain-specific failure information;
- a mandatory logger, telemetry backend, router, queue, database, or transport;
- one default resilience stack silently applied to all operations;
- synchronized package APIs where different domains require different models;
- synchronized releases merely because modules share a repository; or
- source-breaking normalization without a consumer, compatibility, and
  migration audit.

Uniform principles are REQUIRED. Identical APIs are not.

## Authoritative Inputs

Before implementation, inspect and reconcile at least:

- `AGENTS.md`, `README.md`, `docs/architecture.md`, and
  `docs/package-selection.md`;
- `.ai/GOAL_DOCUMENTATION.md`, `.ai/GOAL_COMPATIBILITY.md`,
  `.ai/GOAL_RELEASE.md`, and `.ai/GOAL_OPERATIONAL_ASSURANCE.md`;
- `modules.json`, `packages.json`, the complete owned dependency graph, and
  every releasable module's `go.mod`;
- every exported API, package name, constructor, configuration type, option,
  builder, runtime, error, lifecycle method, adapter, and test helper;
- every package README, documentation tree, example, badge, installation
  command, compatibility statement, and minimum-Go claim;
- the `service` platform contracts, decisions, examples, and Track/Postal/
  Location adoption fixtures;
- integration, interoperability, and benchmark harnesses that already encode
  cross-package behavior; and
- current consumers, public versions, tags, import paths, and external usage.

The audit MUST remeasure current state. Historical counts and this goal's
examples are starting hypotheses, not permanent facts.

## Required Baseline Audit

Produce a durable, reviewed cohesion report before changing APIs. For every
releasable module and nested adapter, record:

- package family and responsibility;
- import path and default Go package identifier;
- lifecycle and release status;
- what the module owns and explicitly does not own;
- construction style and rationale;
- configuration/default/validation style;
- context, deadline, cancellation, and shutdown contract;
- resource ownership and concurrency model;
- error categories, cause preservation, and retry classification;
- observability integration and sensitive-data boundaries;
- required and optional owned dependencies;
- companion packages and adapters;
- README/docs/examples status and root navigation;
- supported Go, platforms, protocols, specifications, and backends;
- current consumers and migration risk; and
- every divergence from the proposed ecosystem design language.

Classify each divergence as:

1. justified domain-specific design;
2. intentional compatibility constraint;
3. temporary pre-v1 inconsistency;
4. stale documentation or generated metadata;
5. naming or package-layout debt;
6. unsafe or ambiguous public behavior; or
7. unresolved decision requiring maintainer approval.

The report MUST distinguish discoverability problems from API problems.
Documentation MUST NOT be used to excuse a needlessly inconsistent API, and an
API rewrite MUST NOT be used to solve a navigation-only problem.

## Consumer-Facing Design Language

Create `docs/design-language.md` as the canonical public contract. It MUST be
written for consumers, not only contributors, and MUST explain both the common
rules and when packages intentionally differ.

At minimum, it MUST cover:

- standard-library-first APIs and portability boundaries;
- package ownership and dependency direction;
- constructors, configuration, options, builders, and compile phases;
- defaults, zero values, validation, and immutable runtime state;
- context, cancellation, deadlines, retries, and unknown outcomes;
- resource ownership, lifecycle, drain, shutdown, and close;
- errors, classification, wrapping, disclosure, and observability;
- concurrency, goroutine, callback, channel, and synchronization ownership;
- security defaults, secret handling, hostile-input bounds, and opt-ins;
- adapters, optional dependencies, and package naming;
- test helpers, fakes, fixtures, clocks, identifiers, and deterministic seams;
- compatibility, deprecation, versioning, and migration; and
- how applications compose packages without a framework.

`AGENTS.md` remains engineering policy. The public design-language document
MUST explain the consumer consequences of that policy without copying a second
normative policy that can drift.

## Package Family Taxonomy

Create a reviewed package taxonomy and add machine-readable family metadata to
the repository catalog source. Every releasable module MUST belong to one
primary family and MAY list secondary capabilities.

The initial families SHOULD be:

| Family | Typical responsibility |
| --- | --- |
| Foundations | clocks, identifiers, correlation, validation, configuration, common immutable values |
| Service edge | service lifecycle, routing, HTTP middleware, authentication, authorization, API queries |
| Protocols and descriptions | JSON-RPC, JSON:API, JSON Schema, OpenAPI, OpenRPC, webhooks, CloudEvents, WSDL, XSD |
| Persistence and durability | PostgreSQL, migrations, cache, leases, idempotency, outbox, queues, scheduling, workflows, event sourcing |
| Resilience | retry, timeout, rate limiting, circuit breaking, bulkheads, concurrency limits, hedging, adaptive throttling, fault injection |
| Observability | structured logging, telemetry, correlation adapters, operational control planes |
| Integration and data movement | HTTP clients, Kafka, filesystems, wire formats, tabular data, search, schema registries |
| Domain utilities | time, money, measurement, geography, localization, rules, state machines, identifiers, packing, authenticated trees |
| Tooling | CLI, prompts, analysis, test support, repository commands |

The final taxonomy MUST resolve overlaps explicitly. A package family is a
navigation and ownership aid, not a new dependency layer or import namespace.

Generate two distinct views:

- a human consumer catalog containing only releasable libraries and adapters,
  grouped by problem and family; and
- an engineering inventory containing libraries, adapters, fixtures,
  interoperability harnesses, benchmarks, examples, and internal tools.

The consumer catalog MUST answer "what should I install and why?" The
engineering inventory MUST answer "what exists and how is it verified?"

## Construction Decision Table

Audit every exported constructor and document one decision table. Use the
following default model unless domain evidence justifies an exception:

| Shape | Use when | Required behavior |
| --- | --- | --- |
| Plain function | operation is stateless and has no retained ownership | explicit inputs, deterministic result, no hidden I/O |
| `New(Config)` | object has meaningful required configuration or invariants | validate completely, return `(T, error)`, retain no caller-mutable aliases |
| `New(Options)` | several named settings form one coherent configuration value | same validation and ownership rules as `Config` |
| Functional options | options are genuinely orthogonal, optional, and additive | typed option category, deterministic application, duplicate/conflict policy |
| Builder then `Compile`/`Build` | startup-time registration precedes an immutable concurrent runtime | single-owner mutation, complete compile-time validation, immutable result |
| `Open`/`Connect`/`Load`/`Init` | construction necessarily performs external I/O or acquires resources | `context.Context`, bounded I/O, explicit returned ownership and cleanup |
| `Must...` helper | tests, generated constants, or process startup where panic is explicitly intended | never the only production API; panic contract documented |

The audit MUST NOT rename `Config` to `Options`, introduce functional options,
or add builders merely to make names uniform. It MUST normalize equivalent
concepts that differ only because packages were designed independently.

## Configuration, Defaults, And Validation

- Configuration values MUST be explicit and caller-owned.
- Constructors MUST reject invalid configuration before background work or I/O
  begins, unless the constructor is explicitly an I/O operation.
- Safe non-zero defaults MUST be visible through a documented zero-value
  policy, `DefaultConfig`, named profiles, or constructors. Defaults MUST NOT be
  split invisibly across unrelated layers.
- Zero values MUST be documented as useful, invalid, or intentionally empty.
- Configuration structs MUST distinguish absent, zero, empty, disabled, and
  defaulted values where behavior differs.
- Functional options MUST have one duplicate/conflict rule and MUST reject
  incompatible combinations deterministically.
- Caller-provided maps, slices, byte buffers, functions, and mutable objects
  MUST have explicit copy or ownership semantics.
- Environment variables and filesystem discovery belong only to packages that
  explicitly own configuration loading; ordinary libraries MUST NOT read them.
- Validation errors MUST identify safe field paths or option names without
  exposing secret values.

Create a decision record for every justified exception.

## Public Function And Type Conventions

- Use standard Go names and types where they express the complete contract:
  `context.Context`, `error`, `time.Time`, `time.Duration`, `io.Reader`,
  `io.Writer`, `fs.FS`, `http.Handler`, `http.RoundTripper`, and `*slog.Logger`.
- Do not wrap standard-library types solely for branding or cross-repository
  ownership.
- Define interfaces at the consuming boundary and keep them minimal.
- Export interfaces only when consumers or adapters need substitution; do not
  create an interface for every concrete type.
- Prefer concrete return types from constructors so APIs can evolve without
  forcing interface changes.
- Make nil acceptance, zero values, mutability, concurrency safety, and
  ownership explicit for every public type.
- Avoid generic `Manager`, `Helper`, `Util`, `Provider`, or `Service` names when
  a precise responsibility exists.
- Generic APIs MUST improve type safety without obscuring ownership or failure
  behavior.
- Reflection, code generation, registration, and package initialization MUST
  remain explicit optional mechanisms with documented failure boundaries.

## Context, Cancellation, And Time

- Every operation that may block on I/O, waiting, external callbacks, resource
  admission, or long-running work MUST accept `context.Context` as its first
  parameter.
- Pure value operations SHOULD NOT accept context merely for uniformity.
- Public objects MUST NOT retain request contexts beyond the operation that
  owns them.
- A package MUST NOT detach from caller cancellation unless it exposes and
  documents a separately owned lifecycle operation.
- Deadlines, retries, hedges, queues, and worker attempts MUST share one
  observable total-work budget where composition requires it.
- A timeout MUST NOT claim to stop arbitrary code that ignores cancellation.
- Wall time and elapsed time MUST use the repository's explicit clock seams
  where deterministic behavior or testability requires them.
- Unknown external outcomes MUST remain distinguishable from known failure.

## Lifecycle And Resource Ownership

Freeze one vocabulary and semantic model:

- `Run(ctx)` performs one caller-bounded long-running execution and returns its
  terminal result.
- `Start(ctx)` transfers ownership only after successful startup and requires a
  separately documented stop/join operation.
- `Drain(ctx)` stops new intake and waits for accepted work without necessarily
  releasing infrastructure.
- `Shutdown(ctx)` is repeatable, concurrency-safe, bounded, and performs the
  complete ordered shutdown owned by that object.
- `Close()` is used for immediate synchronous release matching `io.Closer`;
  context-aware cleanup MUST use a separately named method.
- `Wait(ctx)` waits for an already-owned operation and MUST NOT acquire hidden
  ownership.

Not every object requires every method. Packages MUST use only the methods
whose semantics they own. The cohesion audit MUST identify conflicting current
meanings and either normalize them before v1 or document a justified domain
exception.

Every resource-owning API MUST define:

- acquisition and ownership-transfer point;
- cleanup responsibility and order;
- cancellation and deadline behavior;
- repeat and concurrent shutdown behavior;
- partial-start rollback;
- goroutine, timer, connection, file, response, row, transaction, and buffer
  lifetime; and
- behavior after terminal shutdown.

## Error And Outcome Language

Golib MUST use ordinary Go errors, not a universal ecosystem exception base.
The shared conventions MUST require:

- `errors.Is` for stable categories and `errors.As` for structured details;
- preservation of the original cause where disclosure is safe;
- deterministic configuration and validation diagnostics;
- bounded, secret-safe error strings;
- separate machine-readable categories from human messages;
- explicit retryable, permanent, local-rejection, cancellation, deadline,
  conflict, unavailable, partial, and unknown-outcome distinctions where the
  domain supports them;
- no string matching as a public classification contract;
- no automatic retry implication merely because an error is transient;
- no backend-specific errors crossing an abstraction boundary without an
  explicit adapter contract; and
- aggregate/partial errors that preserve item identity and every relevant
  outcome without unbounded retention.

The audit MUST map equivalent categories across packages and adapters. Reuse a
semantic owner where one already exists; do not create a generic error package
merely to share names.

## Concurrency And Callback Language

- Public documentation MUST state whether values are immutable, single-owner,
  or safe for concurrent use.
- Every goroutine, channel, timer, callback, observer, iterator, and worker MUST
  have one visible owner and bounded lifetime.
- Callbacks MUST document synchronization, re-entry, panic, blocking,
  cancellation, and retention rules.
- Locks MUST remain private implementation details unless their effect is part
  of observable ordering or blocking behavior.
- Packages MUST expose bounds and backpressure rather than silently creating
  unbounded goroutines, queues, buffers, histories, or cardinality.
- Test helpers MUST make concurrency, fake time, shutdown, and leak behavior
  deterministic without changing production global state.

## Security And Observability Language

- Secure behavior MUST be the default when a generally useful safe default
  exists. Risky proxy trust, credential placement, debug disclosure, weak
  algorithms, unbounded input, and permissive fallback require explicit opt-in.
- Secrets, credentials, payloads, tenant identifiers, and high-cardinality
  values MUST have one documented treatment across errors, logs, traces,
  metrics, fixtures, and snapshots.
- Core packages SHOULD expose bounded observations or hooks rather than import
  telemetry backends.
- Logging integrations SHOULD use `*slog.Logger`; telemetry integrations SHOULD
  use standard OpenTelemetry APIs or the explicit `telemetry` module boundary.
- Packages MUST document who creates, owns, flushes, and shuts down logging and
  telemetry resources.
- Metric and trace naming SHOULD use a documented Golib namespace and stable
  attribute vocabulary without forcing applications to initialize globals.

## Adapter And Module Naming

Define and enforce one target-oriented naming scheme before stable release.

Default rules:

- An adapter beneath `pkg/<owner>/adapters/<target>` is owned by `<owner>` and
  adapts its contract to `<target>`.
- Use target names such as `kafka`, `queue`, `outbox`, `otel`, `postgres`,
  `valkey`, `http`, `service`, or `money`.
- Do not prefix adapters with `go`; the complete ecosystem is Go.
- Do not use `golib` as a package-name prefix merely to signal repository
  ownership.
- Use `otel` when the adapter directly targets OpenTelemetry and `telemetry`
  only when it targets the Golib `telemetry` module.
- Service lifecycle adapters SHOULD live under `adapters/service` or another
  single selected pattern, not alternate between `kafkaservice`,
  `queueservice`, and unrelated naming styles.
- AWS integrations MUST identify the actual service or protocol, such as
  `awssecretsmanager` or `mskiam`, rather than a generic `aws` package.
- Directory name, module path, package declaration, default import identifier,
  documentation title, and catalog label MUST be recorded and intentionally
  related.

The initial rename audit MUST include at least:

- `adapters/gokafka` -> `adapters/kafka`;
- `adapters/goqueue` -> `adapters/queue`;
- `adapters/gooutbox` -> `adapters/outbox`;
- `adapters/gotelemetry` -> either `adapters/otel` or
  `adapters/telemetry`, according to the actual dependency;
- `adapters/gomath` -> `adapters/math`;
- `adapters/gomeasurement` -> `adapters/measurement`;
- `adapters/gotemporal` -> `adapters/temporal`;
- `kafkaservice` and `queueservice` -> the selected service-adapter layout; and
- `authotel` -> the selected authentication OpenTelemetry adapter layout.

These are proposed outcomes, not authority to break released consumers. Before
each rename, inspect public tags and actual consumers. Unreleased and unused
paths SHOULD be renamed directly. Released or used paths require the repository
deprecation and migration policy.

## Package Documentation Contract

Define one discoverability contract without forcing identical prose. Every
implemented releasable module MUST provide a README containing or linking to:

1. purpose and one-sentence ownership boundary;
2. lifecycle/maturity and supported Go version;
3. installation command using the canonical module path;
4. a five-minute executable quick start;
5. package/subpackage map;
6. when to use and when not to use it;
7. construction, configuration, defaults, and validation overview;
8. errors, cancellation, resource ownership, concurrency, and shutdown;
9. important integrations, adapters, and companion packages;
10. security and sensitive-data notes;
11. compatibility, migration, performance, and operational caveats;
12. API reference, examples, testing helpers, FAQ, troubleshooting, changelog,
    license, support, and security-reporting links; and
13. a link back to the root ecosystem index and relevant package family.

Planned modules MUST have visibly planned goal documentation and MUST NOT offer
installation instructions or imply released behavior. Nested releasable
adapters require their own README even when the parent package documents them.

Create a reusable README template and structural metadata. Automation MUST
check presence and link validity, but MUST NOT require empty or irrelevant
sections solely to satisfy a template.

## Supported Compositions And Recipes

Define an explicit set of supported compositions before the documentation
portal writes recipes. At minimum include:

- minimal HTTP service;
- internal JSON-RPC service;
- external JSON:API or conventional OpenAPI-described service;
- authenticated and authorized service;
- queue producer and worker;
- ingester and processor;
- PostgreSQL transactional service with migrations, idempotency, and outbox;
- scheduled singleton work;
- Kafka event flow with schema and CloudEvents where selected;
- vendor HTTP client with retry, rate limit, breaker, bulkhead, cache, and
  telemetry;
- filesystem and tabular ingestion;
- durable workflow with compensation;
- searchable Track/Location-style projection; and
- Track, Postal, and Location service-role composition.

For each composition, freeze:

- required and optional modules;
- dependency direction and adapter owner;
- construction and initialization order;
- middleware/policy order;
- request, job, event, or workflow lifecycle;
- transaction, acknowledgement, retry, and unknown-outcome behavior;
- drain and shutdown order;
- configuration and secret ownership;
- logging, tracing, metrics, and correlation ownership;
- failure and recovery semantics;
- compatible version set; and
- executable reference evidence.

Reference compositions MUST use public APIs only and MUST remain
non-releasable examples or interoperability harnesses. They MUST NOT become a
mandatory framework bootstrap.

## Tested Compatibility Sets

Independent SemVer remains the module release model. Add an ecosystem
compatibility product that records combinations proven together without
creating a dependency bundle.

Create a canonical machine-readable `compatibility-sets.json` and generated
`docs/compatibility-sets.md`, or document and implement a better equivalently
explicit naming decision. Each set MUST include:

- stable set identifier and publication status;
- exact Go version and supported OS/architecture matrix;
- exact versions of every included Golib module;
- recipe and interoperability scenarios covered;
- external service and protocol versions;
- evidence/content fingerprints and observation time;
- known exclusions, caveats, and accepted risks;
- upgrade and rollback notes; and
- source and release-manifest references.

Before public module releases, compatibility sets MAY use explicit unreleased
content identities and MUST be labeled non-installable. Public compatibility
sets MUST use exact published semantic versions and clean external-consumer
verification.

Consumers MUST remain free to select other compatible versions. A compatibility
set is a known-good recommendation, not a hidden lockstep release train or a Go
module that imports every package.

## Catalog And Manifest Metadata

Extend the catalog source so generated documentation can expose, at minimum:

- family;
- public package identifier;
- responsibility and non-goals;
- lifecycle and maturity;
- construction and lifecycle style;
- required and optional owned dependencies;
- adapters and companion modules;
- supported Go/platform/backend/specification data;
- README, API, adoption, security, compatibility, performance, examples, FAQ,
  changelog, and pkg.go.dev links;
- known-good compatibility sets; and
- implementation/hardening/release status without conflating them.

Generated metadata MUST be deterministic and validated against the tree.
Human recommendations MUST remain reviewed prose and MUST NOT be generated
from the first paragraph of arbitrary READMEs.

## Automation And Enforcement

Add a root `make cohesion` gate, or an equivalently clear canonical command,
that is fully runnable locally and in the single root CI workflow. It MUST
validate objective invariants including:

- every releasable module has required catalog metadata and documentation;
- package family, module path, package identifier, and adapter naming are valid;
- README Go versions match `go.mod` and repository support policy;
- installation commands and module paths are current;
- package-local workflow badges and stale standalone-repository links are
  absent;
- root backlinks, documentation indexes, internal links, anchors, and examples
  are valid;
- compatibility-set module versions exist and their scenarios have attributable
  evidence;
- generated consumer and engineering catalogs are current;
- supported composition examples compile through clean module resolution;
- no contradictory lifecycle, ownership, error, retry, or shutdown claims are
  published; and
- all exceptions are explicit, reviewed, narrow, and expiring where temporary.

Extend `pkg/analysis` only for semantic source rules with acceptable precision,
such as context position, forbidden globals, constructor ownership, interface
placement, or lifecycle misuse. Documentation and manifest policy belongs in
repository tooling, not source analysis.

The gate MUST NOT enforce superficial preferences such as requiring every
package to expose `New`, every configuration type to share one name, or every
README to contain identical headings when the semantic requirement is met.

## Required Remediation

After the baseline and decisions are approved:

1. normalize pre-v1 naming and package layout where evidence supports it;
2. normalize equivalent public API concepts while preserving justified
   domain-specific shapes;
3. repair package names, docs, examples, imports, manifests, API baselines,
   dependency edges, and changelogs atomically for every rename;
4. repair stale minimum-Go claims against current `go.mod` files;
5. replace invalid package-local workflow badges with accurate root-workflow
   status or remove them;
6. add missing README entry points for every releasable nested module;
7. make existing service/adoption examples discoverable as ecosystem recipes;
8. separate consumer package selection from the exhaustive engineering
   inventory;
9. add tested compatibility sets and their release integration; and
10. update the documentation, compatibility, release, polish, operational
    assurance, and maintenance goals to consume the resulting contracts where
    necessary.

Do not perform broad mechanical rewrites. Each API or path change MUST have a
named cohesion requirement, consumer inventory, migration decision, affected
dependency closure, focused verification, and changelog impact.

## Execution Phases

### Phase 1: Inventory And Decisions

1. Reproduce the baseline across every releasable module.
2. Inventory current consumers, releases, package names, APIs, and docs.
3. Define package families and common terminology.
4. Draft the construction, lifecycle, error, adapter, and documentation
   decision matrices.
5. Record every justified exception and unresolved decision.

No public rename or API normalization begins before this phase is reviewed.

### Phase 2: Design-Language Foundation

1. Publish `docs/design-language.md`.
2. Add catalog metadata for families, ownership, companions, and API styles.
3. Implement consumer and engineering catalog separation.
4. Implement the initial cohesion validation command.
5. Freeze adapter and lifecycle naming decisions.

### Phase 3: Pre-v1 Remediation

1. Rename inconsistent adapters according to consumer/release evidence.
2. Normalize equivalent constructors, configs, options, errors, and lifecycle
   methods where justified.
3. Repair stale Go versions, badges, links, module paths, and package docs.
4. Add missing nested-module READMEs and ecosystem backlinks.
5. Update API baselines, dependency manifests, changelogs, and migration notes.

### Phase 4: Composition And Compatibility

1. Define supported package stacks and exact ownership/order contracts.
2. Promote existing integration evidence into public executable recipes.
3. Add missing multi-package reference compositions.
4. Create compatibility-set schema, tooling, generated docs, and clean-consumer
   verification.
5. Prove Track, Postal, and Location adoption paths against the same public
   design language.

### Phase 5: Documentation Handoff

1. Update `.ai/GOAL_DOCUMENTATION.md` inputs with the frozen design language,
   taxonomy, compositions, compatibility sets, and remediation results.
2. Ensure documentation work does not reopen settled API decisions silently.
3. Hand off a complete residual-gap and intentional-exception register.

### Phase 6: Final Verification

1. Run the cohesion gate and every affected module/reverse-dependant gate.
2. Run clean external-consumer examples for each supported composition.
3. Validate generated manifests and catalogs.
4. Audit the final APIs and documentation as each target consumer.
5. Record every remaining exception, caveat, and release blocker.

## Target Consumer Walkthroughs

Completion requires documented walkthroughs for:

- a user adopting one standalone value or protocol package;
- a developer choosing among overlapping packages;
- a team creating a new HTTP or JSON-RPC service;
- a team creating a worker, ingester, processor, scheduler, or workflow;
- a Laravel/PHP team mapping familiar concerns without framework magic;
- an operator deploying PostgreSQL, Valkey, Kafka, OpenSearch, and telemetry;
- an open-source contributor adding a package or adapter; and
- a maintainer releasing an independently versioned compatible package set.

Each walkthrough MUST identify every point where the user must guess a package,
name, constructor, default, ownership rule, lifecycle method, error category,
integration order, compatible version, or next documentation page. Every
unjustified guess is an unresolved cohesion defect.

## Verification Requirements

- Run manifest and cohesion validation against the complete final tree.
- Compile and execute every supported composition example with clean module
  resolution and no workspace replacement.
- Run API compatibility and focused behavioral gates for every normalized API
  or renamed module.
- Run reverse-dependant selection for every changed public contract.
- Verify all documentation links, examples, package names, module paths,
  supported Go versions, badges, and compatibility sets.
- Verify package catalogs contain no fixtures or harnesses in the consumer view
  and omit no releasable module or adapter.
- Verify every catalog and compatibility artifact is deterministic and current.
- Verify no framework runtime, global registry, umbrella dependency, hidden
  initialization, or dependency cycle was introduced.

Expensive test, race, fuzz, mutation, benchmark, conformance, security, and
external-service evidence MAY be reused only when the complete applicable input
fingerprint is unchanged. Documentation-only evidence MUST NOT be presented as
proof of runtime compatibility.

## Acceptance Criteria

This goal is complete only when:

- Golib has one reviewed, consumer-facing design language;
- every releasable module is classified by family, responsibility, non-goals,
  construction style, lifecycle style, companions, and maturity;
- equivalent API concepts follow the same convention or have a documented
  justified exception;
- adapter and service-integration names follow one target-oriented scheme;
- every releasable module and nested adapter has a compliant entry-point
  README and root ecosystem navigation;
- stale Go versions, package-local workflow badges, standalone paths, and
  misleading status claims are eliminated;
- supported multi-package stacks have executable recipes and explicit
  initialization, ownership, failure, and shutdown contracts;
- independently released modules have published known-good compatibility sets;
- local and CI cohesion validation detects objective drift;
- Track, Postal, and Location composition uses the same public conventions;
- the documentation goal can execute without inventing or silently changing
  API design decisions;
- no unjustified consumer guess identified by the walkthroughs remains; and
- cohesion was achieved without creating a framework, umbrella dependency,
  service container, global runtime, or hidden magic.

Completion MUST include an explicit residual-exception register. A package MAY
differ because its domain requires it; it MUST NOT differ merely because it was
implemented by a different agent or at a different time.
