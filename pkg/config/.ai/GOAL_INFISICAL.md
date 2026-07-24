# Goal: Production-Grade Infisical Integration

## Objective

Deliver complete, secure, and operationally predictable Infisical support for
`config` without turning configuration core into a secret-management SDK or
requiring every Kubernetes application to communicate with Infisical directly.

The integration MUST support boot-time secret loading and explicit, safe secret
refresh for long-running applications. A refresh MUST produce a complete,
immutable, decoded, and validated configuration snapshot. It MUST NOT mutate an
existing snapshot, process environment, or arbitrary application fields.

This goal covers both platform-delivered Infisical secrets and the optional
native Go adapter. It is an execution specification, not permission to weaken
the source, validation, redaction, or dependency boundaries in `GOAL.md` and
`GOAL_HARDEN.md`.

## Product Position

Infisical is the system of record and rotation authority. `config` is a
read-only consumer and configuration assembler.

The integration owns:

- consuming Operator-managed environment variables and Kubernetes Secrets
- consuming CSI- or Agent-mounted secret files
- optional native retrieval through the official Infisical Go SDK
- refresh detection and complete candidate-snapshot reconstruction
- safe publication of validated snapshots
- bounded cache, retry, stale-value, readiness, and shutdown behavior
- source provenance, metrics, health state, and redacted diagnostics

The integration MUST NOT own:

- secret creation, editing, deletion, rotation policy, approval, or write-back
- Infisical project, identity, role, permission, or environment administration
- Kubernetes controller, CSI provider, Agent Injector, or operator behavior
- application-specific credential installation or resource reconstruction
- hidden mutation of global state, environment variables, or live config structs

## Delivery Modes

### Kubernetes Operator

The Operator is the default when secrets need to become Kubernetes Secret data
or environment variables.

- The Operator continuously synchronizes Infisical values into Kubernetes.
- Environment variables are immutable for the lifetime of a process.
- Environment-based consumers therefore require a workload restart to adopt a
  changed value.
- The deployment recipe MUST document Infisical's workload auto-reload
  annotation and the resulting rolling-restart behavior.
- `config` performs an ordinary boot load and has no Infisical SDK dependency
  in this mode.
- Readiness MUST remain false until all required values decode and validate.

This mode is preferred for boot-only settings and credentials whose client or
pool cannot rotate safely in-process.

### Kubernetes CSI

CSI-mounted files are the default when an application must observe static
secret changes without restarting its pod.

- The Secrets Store CSI driver's rotation support MUST be enabled explicitly.
- Rotation poll interval and expected propagation delay MUST be documented.
- Mounted files MUST be read through the ordinary filesystem source.
- A file-change watcher MAY trigger reload, but correctness MUST NOT rely only on
  filesystem events; periodic reconciliation is required.
- File replacement, truncation, rename, symlink, permission, and partial-write
  behavior MUST be handled without accepting mixed or incomplete values.
- The watcher MUST debounce bursts and reconstruct the entire configuration
  plan, not patch individual fields.
- CSI support MUST document that Infisical currently limits this integration to
  static secrets.

### Agent Injector

Agent-rendered shared-volume files MAY be consumed using the same file and
reconciliation behavior as CSI.

- Template output format and atomic replacement assumptions MUST be explicit.
- The application MUST NOT parse a partially rendered file.
- Agent authentication, renewal, and rendering remain platform concerns.

### Native Infisical Adapter

The native adapter exists for non-Kubernetes services, local or operational
tools, and applications with a demonstrated need for direct on-demand retrieval.
It is not the default Kubernetes path.

- It MUST use the official Infisical Go SDK.
- It MUST be read-only.
- It MUST be isolated from the core module dependency graph.
- Core users MUST NOT compile, link, or be forced to update the Infisical SDK.
- The preferred packaging is a separately versioned nested adapter module with a
  documented compatibility matrix against `config` core.
- The adapter MUST implement the generic source and refresh contracts rather
  than introduce an Infisical-specific application configuration model.

## Native Source Scope

The native source MUST support:

- explicit site URL for Infisical Cloud and self-hosted installations
- explicit organization, project, environment, and secret path
- retrieval of one secret, selected keys, or a bounded path listing
- optional bounded recursive retrieval
- explicit shared/personal type where supported and appropriate
- secret reference expansion with documented behavior and depth limits
- folder imports with deterministic precedence and collision handling
- exact key preservation by default
- explicit key-to-field mapping and optional caller-selected normalization
- duplicate and post-normalization collision rejection
- source sensitivity and provenance without exposing values
- context cancellation, deadlines, and caller-provided HTTP transport

Unbounded recursive loading, implicit project discovery, implicit environment
selection, and ambient fallback to a different scope are forbidden.

## Authentication

### Required Methods

The adapter MUST prioritize workload identity and short-lived credentials:

- Kubernetes machine identity authentication
- Universal Auth for non-Kubernetes machines where workload identity is not
  available
- an explicitly supplied access token for bounded operational tooling

Additional official machine-identity methods MAY be added only with complete
tests and documentation. Authentication methods MUST be additive and MUST NOT
change the source or refresh semantics.

### Authentication Rules

- Authentication selection MUST be explicit and mutually exclusive.
- Kubernetes service-account tokens MUST be read from a caller-selected path and
  re-read when the authentication flow requires renewal.
- Universal Auth client secrets and access tokens MUST use secret-safe types.
- Credentials MUST NOT appear in URLs, errors, logs, traces, metrics, snapshots,
  examples, fixtures, panic output, or process arguments.
- The SDK's automatic authentication-token refresh MUST be configurable and
  lifecycle-managed.
- Authentication-token refresh MUST NOT be described as application-secret
  refresh; they are separate mechanisms.
- Client contexts and SDK refresh goroutines MUST be stopped on shutdown.
- Custom certificate authorities, strict TLS verification, proxy policy, and
  transport timeouts MUST be configurable without insecure defaults.

## Boot-Time Loading

- A required native source MUST authenticate and fetch synchronously during
  application boot.
- The candidate data MUST pass mapping, layering, decoding, and validation before
  a snapshot is returned.
- Required authentication, network, scope, retrieval, decode, or validation
  failures MUST fail startup closed.
- Optional sources MAY be absent only according to explicit source policy;
  malformed or unauthorized sources MUST NOT be treated as absent.
- Startup retries MUST be bounded by context deadline and a configured attempt or
  elapsed-time limit.
- No partially loaded or previously cached snapshot may be presented as a fresh
  successful boot unless an explicit persisted-cache policy is introduced and
  enabled by the application.
- Persisted plaintext secret caches are out of scope and forbidden by default.

## Secret Refresh

### Refresh Modes

The integration MUST provide explicit modes:

- `Disabled`: load once at boot.
- `Reconcile`: periodically check and rebuild when source content changes.
- `OnDemand`: refresh only through an explicit application call.

No mode may be selected implicitly. Native automatic refresh MUST default to
disabled until the application declares how updated credentials are consumed.

### Reconciliation Semantics

- Poll intervals MUST have minimums, maximums, and randomized jitter.
- Only one refresh may run per source or configuration plan at a time.
- Concurrent triggers MUST coalesce rather than create an unbounded queue.
- A trigger MUST rebuild the complete layered plan, including non-Infisical
  sources, defaults, interpolation, decoding, and validation.
- Change detection SHOULD use stable versions or secret-safe fingerprints when
  available; raw values MUST never be emitted.
- An unchanged candidate MUST not notify consumers.
- A changed candidate MUST be published atomically as a new immutable snapshot.
- Snapshot generations MUST increase monotonically.
- Failed candidates MUST be discarded completely.
- The previous valid snapshot MUST remain readable after a runtime refresh
  failure, subject to the configured maximum-staleness policy.
- Notification delivery MUST be ordered, bounded, cancellation-aware, and safe
  from a slow or panicking subscriber.
- Refresh MUST stop promptly when its context is canceled.

### Stale-Value Policy

Applications MUST choose and document one runtime policy:

- keep serving with the last valid snapshot indefinitely while reporting
  degraded health
- keep serving only until a maximum snapshot age, then fail readiness
- terminate after a bounded stale period so Kubernetes can replace the workload

The library MUST NOT terminate the process itself. It reports state and lets the
application or service runtime apply policy.

A stale snapshot MUST never be labeled current. Metrics and health state MUST
include last successful refresh time, age, generation, and redacted failure
category.

## Consumer Rotation Contract

Publishing a new configuration snapshot does not guarantee that existing
clients have adopted its credentials. The API and documentation MUST make this
distinction unavoidable.

- Consumers explicitly subscribe to snapshot generations or obtain credentials
  through a rotation-aware provider.
- Static strings captured during construction MUST be documented as boot-only.
- Consumers MUST acknowledge successful or failed application of a generation
  when orchestration depends on it.
- Consumer failure MUST NOT roll back the globally published immutable snapshot
  silently.
- Partial consumer adoption MUST be observable.
- Applications define whether readiness depends on all critical consumers
  adopting the latest generation.

Required adoption recipes include:

- HTTP bearer, API-key, and OAuth clients using an atomic credential provider
- PostgreSQL creating and validating a replacement pool before atomically
  switching and draining the previous pool
- Redis and Valkey creating and validating replacement clients before switching
- object-storage, SFTP, and vendor clients replacing sessions safely
- queue workers pausing acquisition where necessary while replacing clients

Rotation documentation MUST account for credential overlap. Infisical rotation
should keep the previous credential valid long enough for propagation,
validation, client replacement, and connection draining.

## Caching, Retry, And Outages

- SDK response caching MUST be disabled by default or configured with an explicit
  TTL lower than the required refresh objective.
- Cache TTL, refresh interval, and maximum staleness MUST be independent settings.
- Retry only errors classified as transient.
- Authentication and authorization failures, invalid scope, missing required
  keys, collisions, decode failures, and validation failures MUST NOT be retried
  indefinitely.
- Retries MUST use capped exponential backoff with jitter and honor server retry
  guidance where safe.
- Every network attempt MUST have a deadline.
- Retry budgets MUST prevent startup storms and synchronized pod polling.
- Rate limiting and circuit-breaking behavior MUST be bounded and observable.
- Runtime outages retain only the last complete validated in-memory snapshot
  according to stale policy.
- Recovery MUST reconcile immediately and then return to the normal interval.

## Dynamic Secrets

Dynamic-secret leases are a separate lifecycle from static-secret refresh.

- Operator-managed dynamic secrets SHOULD remain a Kubernetes/platform concern.
- Native dynamic-secret support MUST NOT be added as if it were ordinary static
  polling.
- If native dynamic secrets are later implemented, they require a dedicated
  lease abstraction covering issuance, TTL, renewal, overlap, revocation,
  shutdown, crash recovery, and consumer acknowledgement.
- A dynamic lease MUST never be persisted or silently reused beyond its TTL.
- Native dynamic-secret support is not complete until PostgreSQL and other
  relevant client-rotation integration tests prove uninterrupted replacement.

## Security Requirements

- Secret values are sensitive from receipt through decode and consumer handoff.
- All adapter errors MUST use typed, redacted categories.
- Secret values, identity credentials, access tokens, request bodies, and
  sensitive headers MUST be removed from SDK and HTTP logging.
- OpenTelemetry spans MUST contain only bounded identifiers and outcome data.
- Metrics MUST not use secret keys, paths, project IDs, environment names, or
  error messages as unbounded labels.
- Source provenance may report configured aliases and presence, never values.
- Secret-safe equality or fingerprints MUST use keyed hashing or another design
  that does not expose low-entropy values through ordinary digest comparison.
- Temporary byte buffers and copies MUST be minimized, while documentation MUST
  not claim physical zeroization guarantees Go cannot provide.
- Core dumps, panic reporting, debug endpoints, heap profiles, and support
  bundles MUST be covered by the operational threat model.
- Dependency versions and checksums MUST be pinned and reviewed.
- Adapter releases MUST include vulnerability and provenance checks.

## Observability And Health

The adapter MUST expose interfaces compatible with `log/slog` and OpenTelemetry
without requiring either in core.

Required signals include:

- boot-load duration and outcome
- refresh attempts, successes, failures, and unchanged results
- source age and last successful refresh timestamp
- current and candidate generation numbers
- retry count and bounded failure category
- authentication renewal outcome without token details
- subscriber adoption success, failure, and lag where enabled
- degraded and stale health state

Logs MUST be event-oriented, rate-limited for repeated failures, and redacted.
The package MUST provide liveness, readiness, and diagnostic state as data; it
MUST NOT register HTTP endpoints or impose a service framework.

## API And Package Shape

The detailed API is finalized through tests and examples, but MUST preserve
these conceptual boundaries:

- `infisical.Source`: one bounded read-only source configuration
- `infisical.Auth`: mutually exclusive authentication configuration
- `infisical.Client`: narrow SDK wrapper suitable for deterministic tests
- `infisical.RefreshPolicy`: disabled, reconcile, or on-demand behavior
- `infisical.Status`: redacted lifecycle and health state
- `infisical.Trigger`: explicit on-demand reconciliation
- generic `config` snapshot publication and subscription contracts
- test doubles and assertions in an adapter-specific test package

Interfaces SHOULD be defined by consumers at the narrowest useful seam. The
package MUST NOT mirror the entire Infisical SDK or expose SDK response types in
public configuration contracts.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Coverage MUST prove
behavior and failure semantics rather than execute lines without assertions.

Required automated testing includes:

- authentication success, expiry, renewal, cancellation, and rejection matrices
- Cloud and self-hosted URL, TLS, CA, proxy, timeout, and transport tests
- project, organization, environment, path, import, reference, recursion, and
  key-mapping conformance tests
- startup fail-closed and optional-source behavior
- refresh state-machine and generation truth tables
- unchanged, changed, malformed, partial, missing, revoked, and colliding values
- retry classification, backoff, jitter, rate-limit, outage, and recovery tests
- cache TTL and maximum-staleness boundary tests with a fake clock
- slow, failed, panicking, canceled, and concurrent subscriber tests
- race-detector tests for load, refresh, publication, status, and shutdown
- goroutine, timer, connection, body, and file-descriptor leak tests
- secret-leak canaries across errors, logs, traces, metrics, snapshots, test
  output, panic recovery, and fuzz failures
- hostile-response, malformed-field, mapping, reference, and state-machine fuzzing
- Operator environment and restart recipe validation
- CSI rotation, atomic file replacement, reconciliation, and failure tests
- compatibility tests against pinned supported Infisical versions
- benchmarks for boot loading, large secret sets, refresh, unchanged detection,
  snapshot rebuild, and concurrent readers

Unit tests MUST use a narrow fake client and deterministic clock. Integration
tests MUST run against a pinned ephemeral Infisical deployment or an explicitly
versioned protocol-compatible fixture. Live Cloud credentials MUST never be
required for pull-request CI.

## GitHub Actions And Supply Chain

GitHub Actions MUST run:

- formatting, vetting, linting, and API compatibility checks
- meaningful exact coverage enforcement
- unit, integration, race, and fuzz smoke tests
- supported Go and Infisical compatibility matrices
- Kubernetes manifest and example validation
- vulnerability, license, checksum, and dependency-review checks
- secret scanning and generated-artifact verification
- documentation link, snippet, and runnable-example checks
- benchmark comparison with documented regression thresholds
- release provenance, signed tags or artifacts, and changelog validation

Integration workflows MUST use least-privilege ephemeral identities and MUST NOT
run privileged secret-bearing jobs for untrusted pull requests.

## Documentation Deliverables

Documentation MUST allow adoption and operation without reading implementation
source. It MUST include:

- five-minute Operator, CSI, and native-adapter quickstarts
- decision guide for Operator environment, Operator file/Secret, CSI, Agent, and
  native SDK delivery
- complete public API reference and configuration option reference
- Kubernetes identity and Universal Auth setup guides
- Infisical Cloud and self-hosted setup
- boot-only and live-refresh application examples
- HTTP, PostgreSQL, Redis, Valkey, queue, object-storage, and SFTP rotation recipes
- rotation overlap and zero-downtime runbooks
- outage, stale-value, readiness, and recovery runbooks
- secret revocation and emergency response guidance
- observability dashboards, alerts, and redacted troubleshooting examples
- threat model and security guidance
- compatibility and upgrade policy
- migration guide from environment-only boot loading to CSI or native refresh
- local-development and test guidance without production credentials
- FAQ covering token refresh versus secret refresh, environment immutability,
  cache behavior, restarts, stale values, and consumer adoption
- maintained `CHANGELOG.md` entries for every user-visible or compatibility change

All snippets and examples MUST compile or be validated automatically.

## Execution Plan

1. Freeze the platform-versus-native decision matrix and threat model.
2. Specify source, authentication, status, refresh, generation, subscription,
   staleness, and consumer-adoption contracts.
3. Build the isolated read-only SDK adapter with boot-time loading.
4. Implement deterministic refresh and complete snapshot reconstruction.
5. Add CSI and Agent mounted-file reconciliation without Infisical coupling in
   core filesystem loading.
6. Implement status, health, metrics, tracing, logging, and adoption reporting.
7. Prove HTTP, PostgreSQL, Valkey, Redis, queue, and storage credential rotation.
8. Complete hostile-input, outage, race, fuzz, leak, and compatibility hardening.
9. Publish adoption, API, operations, security, troubleshooting, and migration
   documentation.
10. Release only after all acceptance criteria and hardening gates pass.

## Release Blockers

- Infisical SDK dependency entering `config` core.
- Secret write, mutation, deletion, or administration capability.
- Partial, mutable, unvalidated, or non-atomic snapshot publication.
- Automatic refresh enabled without explicit consumer rotation behavior.
- Secret disclosure through any diagnostic, telemetry, test, or error surface.
- Unbounded retries, polling, recursion, imports, references, caches, queues, or
  subscriber execution.
- Confusing SDK authentication-token renewal with application-secret refresh.
- Environment-variable documentation claiming in-process rotation.
- Silent stale-value use or missing last-success and age reporting.
- Data race, panic, goroutine leak, timer leak, connection leak, or ignored
  cancellation.
- Missing meaningful 100% coverage, compatibility evidence, documentation, or a
  required GitHub Actions gate.

## Acceptance Criteria

- Kubernetes applications can choose Operator restart semantics or CSI file
  refresh with an explicit, documented operational model.
- Non-Kubernetes applications can load required secrets at boot through the
  isolated native adapter and fail closed predictably.
- Applications can opt into bounded reconciliation and receive only complete,
  immutable, validated configuration generations.
- Existing valid snapshots remain available according to explicit stale policy
  when Infisical or candidate validation fails.
- Critical consumers can prove adoption of rotated HTTP, PostgreSQL, Valkey,
  Redis, queue, and storage credentials without hidden global mutation.
- Authentication renewal, secret refresh, dynamic leases, and consumer adoption
  are modeled as separate lifecycles.
- Every secret-bearing diagnostic and telemetry path passes leak-canary tests.
- Meaningful 100% coverage and all GitHub Actions gates pass.
- Documentation covers the complete user-facing API, adoption paths, examples,
  FAQ, operations, security, migration, and troubleshooting scenarios.
- `CHANGELOG.md` is complete and the supported Infisical compatibility matrix is
  published.
