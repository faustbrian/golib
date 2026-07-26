# Goal: `queue`

## Objective

Build an open source Go queue package that becomes one consolidated,
production-grade unit rather than a thin wrapper over a scattered family of
queue backends and repos.

The first version is expected to start as a 1:1 fork strategy around the
`golang-queue` ecosystem, but with the backend implementations merged directly
into this repository and module so consumers install one owned package instead
of stitching together multiple external dependencies.

## Source Baseline

The intended consolidation baseline is:

- `https://github.com/golang-queue/queue`
- `https://github.com/golang-queue/nsq`
- `https://github.com/golang-queue/redisdb-stream`
- `https://github.com/golang-queue/nats`
- `https://github.com/golang-queue/redisdb`
- `https://github.com/golang-queue/rabbitmq`

The package goal is not to depend on these as separate install-time modules.
The goal is to absorb the relevant implementations into a single owned package
and release process.

## Why This Exists

Queueing is operationally critical for the service family being migrated to
Go. The organization wants:

- one audited dependency unit
- one release process
- one compatibility contract
- one observability model
- reduced supply-chain sprawl

The current concern is that the `golang-queue` family is too scattered to
trust as-is for long-term critical infrastructure.

## Product Position

`queue` should be:

- open source
- production-grade
- transport and backend aware
- explicit about delivery semantics
- strongly observable
- suitable for high-throughput worker systems

It should not be a toy abstraction that hides the hard operational parts.

## First-Version Product Strategy

### Fork Strategy

The first version should preserve the upstream `golang-queue` behavior closely
enough that migration and verification remain tractable.

That means:

- keep the upstream conceptual model recognizable
- port backend implementations into this repository directly
- minimize gratuitous redesign during the initial consolidation
- document every intentional divergence from upstream

This is a first-version consolidation, not an excuse for immediate framework
reinvention.

### Backend Strategy

- Redis and Valkey Streams are first-class durable backends
- Redis support uses `github.com/redis/go-redis/v9`
- Valkey support uses `github.com/valkey-io/valkey-go`
- Valkey support is additive and MUST NOT replace or masquerade as Redis support
- additional backends are additive, not mandatory
- backend support should not force every consumer to pull a web of external
  queue subpackages

The initial backend family to consolidate is:

- Redis
- Redis Streams
- Valkey Streams
- NATS
- NSQ
- RabbitMQ

Support should be explicit per backend rather than vaguely “pluggable”.

## Scope

### In Scope

- consolidated queue abstraction and worker model
- backend implementations merged into one repository
- retries
- backoff
- worker lifecycle handling
- graceful shutdown
- error handling hooks
- delivery acknowledgement semantics
- observability hooks
- stable worker-status, management, control-command, failed-job, dead-letter,
  and capability contracts for `queue-control-plane`
- backend-specific configuration surfaces
- compatibility notes for each backend

### Potentially In Scope After Consolidation Stabilizes

- middleware or interceptor chains
- `idempotency` middleware integration
- scheduling and delayed delivery helpers
- metrics exporters for popular observability stacks

Dead-lettering for durable backends is mandatory first-version data-plane
scope. The package owns terminal classification, safe settlement, durable
records, and bounded management operations; it is not an optional helper.

### Out of Scope For The First Version

- speculative support for every queue backend in existence
- a workflow engine
- job orchestration DSLs
- application-specific business semantics
- pretending that all backends have identical guarantees
- administrative API, CLI, web UI, historical dashboard, fleet inventory, or
  Kubernetes scaling integration; these belong to `queue-control-plane`

## Non-Goals

- Do not hide backend differences that materially affect correctness.
- Do not over-abstract until the consolidated fork is stable.
- Do not introduce unnecessary framework dependencies.
- Do not let “generic queue package” turn into “do everything distributed
  systems package”.

## Core Requirements

### 1. Consolidated Ownership

Consumers should install one package family that is owned and released as one
unit. They should not need to compose separate upstream backend repos just to
get normal functionality.

### 2. Redis And Valkey Excellence

Redis and Valkey MUST have native, independently tested adapters. Shared worker
and delivery semantics should live in package-owned internal code rather than
in either client library.

That means:

- Redis behavior must be especially well-tested
- Valkey 9 behavior through `valkey-go` must be especially well-tested
- Redis observability must be first-class
- Valkey observability must be first-class
- Redis shutdown and retry semantics must be clear
- public APIs must not expose `go-redis` or `valkey-go` types

### 3. Honest Backend Semantics

The package must not pretend all backends have identical guarantees.

Differences in:

- acknowledgement
- ordering
- redelivery
- visibility timeout
- consumer group behavior
- delayed delivery

must be documented explicitly.

### 4. Operational Observability

The package must make it easy to observe:

- queue depth where available
- job age where available
- retry counts
- handler failures
- acknowledgement failures
- worker throughput
- shutdown behavior

### 5. Predictable Worker Lifecycle

The package must give clear control over:

- startup
- polling or subscription lifecycle
- cancellation
- shutdown
- in-flight job handling
- lease or ack completion behavior

### 6. Control-Plane Contract

`queue` owns the data-plane side of control-plane interoperability:

- versioned worker identity, heartbeat, status, capability, and event models
- durable pause/resume/drain command observation and enforcement
- failed-job and dead-letter management operations backed by native semantics
- command IDs, idempotent application, acknowledgement, and unknown outcomes
- rolling-version capability negotiation

`queue` MUST remain fully operational without a running control plane. It
MUST NOT own the administrative API, UI, audit store, fleet history, or
Kubernetes orchestration.

### 7. Conservative First-Version Change Model

The first version should prioritize:

- consolidation
- ownership
- verification

over aggressive redesign.

If the upstream model is ugly in places, document it and stabilize first.
Refine later once the consolidated unit is proven.

## First-Version Deliverables

### Package Surface

- queue interface and worker primitives
- merged backend implementations
- configuration model
- observability hooks
- error and retry model
- shutdown and lifecycle controls
- versioned management and control-plane protocol contracts

### Verification

- backend-specific test coverage
- parity checks against upstream behavior where practical
- integration tests for Redis and Valkey Streams scenarios
- benchmarks for enqueue, consume, ack, retry, and shutdown paths

## Documentation Deliverables

- README
- architecture overview
- full public API reference
- backend support matrix
- delivery semantics matrix
- migration notes from upstream `golang-queue`
- `queue-control-plane` protocol and integration guide
- `idempotency` consumer integration guide
- compatibility policy
- adoption guide
- backend setup guides
- end-to-end examples
- scenario cookbook
- FAQ
- troubleshooting guide
- versioning and release guide
- contribution guide
- maintained `CHANGELOG.md` with every user-visible change

The documentation must be good enough that a new user can:

- understand backend semantics before adoption
- set up the Redis or Valkey Streams path quickly
- understand the tradeoffs of each backend
- adopt the package without reading the whole implementation
- find examples for common worker and retry scenarios quickly

## Testing Standard

Meaningful 100% coverage for production package code is required.

That requirement does not mean “touch every line somehow.” It means tests must
exercise and prove the behavior of:

- happy paths
- edge cases
- backend-specific branches
- retry and redelivery flows
- error paths
- shutdown behavior
- acknowledgement and lease semantics

Coverage games are not acceptable. Hitting lines without proving behavior does
not satisfy this goal.

Testing must include:

- unit tests for the core queue abstractions
- backend-specific integration tests
- retry and backoff behavior tests
- shutdown and cancellation tests
- failure and redelivery tests
- regression tests for every discovered backend edge case
- benchmarks for the Redis backend at minimum

## Versioning And Compatibility

- semantic versioning
- explicit release notes for behavior changes
- documented compatibility constraints per backend
- no silent semantic changes to ack or retry behavior

## Repository Automation And Quality Gates

The repository must include GitHub Actions workflows for:

- test execution
- formatting checks
- linting
- static analysis
- integration test execution strategy
- benchmark execution strategy
- documentation validation where practical
- dependency and security scanning
- tagged release automation

At minimum, pull requests must have automated checks that prove:

- the code builds
- tests pass
- meaningful `100%` production-code coverage is maintained
- formatting is enforced
- lint and static-analysis gates are green
- backend-critical examples do not silently rot

Release workflows must be explicit and reproducible.

## Open Source Standard

This package should be publishable as serious infrastructure:

- no Shipit-specific names in public APIs
- no hidden project assumptions
- no vague promises about backend parity
- no unclear ownership of merged upstream code

## Execution Plan

### Phase 1: Consolidation Definition

- map the upstream package surfaces
- define the merged repository layout
- define what remains 1:1 versus what is intentionally changed
- define the Redis and Valkey native-client support standard

### Phase 2: Initial Fork And Merge

- import upstream code
- merge backend packages into one repository
- normalize build and module layout
- preserve behavior as closely as practical

### Phase 3: Verification And Hardening

- add parity and regression coverage
- add integration tests
- add observability hooks
- tighten public API where needed

### Phase 4: Open Source Readiness

- document upstream provenance
- document backend semantics clearly
- finalize technical API documentation
- finalize adoption documentation
- finalize FAQ and troubleshooting content
- finish GitHub Actions and release automation
- publish roadmap
- release first public version

## Acceptance Criteria

This goal is achieved when:

- the repository acts as one consolidated queue package unit
- the upstream backend family is no longer required as separate consumer
  dependencies
- Redis and Valkey production usage is credible and independently tested
- backend semantics are documented honestly
- meaningful 100% production-code coverage is documented and enforced
- GitHub Actions quality gates and release automation are in place
- user-facing docs are complete enough for direct adoption
- the package is suitable for open source release and real service adoption

## Hard Warnings

- Do not turn the first version into a broad redesign project.
- Do not erase backend-specific semantics behind false abstraction.
- Do not add more backends until the consolidated first set is stable.
- Do not let supply-chain consolidation become a pretext for vague behavior
  changes.
