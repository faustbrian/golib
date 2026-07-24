# Goal: Typed Layered Configuration for Go

## Objective

Build a production-grade open-source configuration package that loads layered
JSON, YAML, TOML, dotenv, environment, and programmatic sources into typed Go
structs with deterministic precedence, strict validation, source provenance,
and secret-safe diagnostics.

The package should provide the practical discovery and loader ergonomics of
Cosmiconfig and Lilconfig while remaining appropriate for long-running Go
services, workers, commands, libraries, and Kubernetes deployments.

## Product Position

`config` owns configuration discovery, source loading, merging, decoding,
provenance, redaction, and validation orchestration. It MUST NOT become a global
service locator, cloud SDK catalog, secret-management platform, or application
framework.

Applications define their own root configuration struct. Specialized packages
such as `postgres`, `telemetry`, `filesystem`, `queue`, and
`authentication` define the configuration types they own. `config` loads
those types without duplicating or centralizing them.

## Core Loading Model

- Load into caller-owned typed structs.
- Preserve distinction between absent, explicitly empty, zero, null, and
  defaulted values where the destination type supports it.
- Build one immutable configuration snapshot per successful load.
- Return no partially decoded snapshot on failure.
- Support context-aware loading for sources that may perform I/O.
- Provide stable typed errors with field path, source, location where available,
  expected type, safe received description, and underlying cause.
- Aggregate independent decode and validation errors deterministically.

## Sources

### Built-In Sources

- Programmatic defaults.
- JSON files using strict standard-library semantics plus duplicate detection.
- YAML files through an audited maintained decoder with strict duplicate and
  unknown-field behavior.
- TOML files through an audited maintained decoder.
- Dotenv files with explicit quoting, escaping, comments, multiline, duplicate,
  and interpolation semantics.
- Process environment variables.
- Explicit programmatic overrides for tests, commands, and composition roots.
- Reader, byte-slice, map, and `fs.FS` sources for embedding and tests.

### Source Contract

- Sources have stable names, priority, sensitivity, and optional/required state.
- A source produces a typed intermediate tree plus provenance; it MUST NOT mutate
  the destination struct or process environment.
- Optional sources may be absent but MUST NOT hide malformed or unreadable files.
- Every source has explicit size, depth, key-count, and read-time limits.
- Remote sources are optional adapter packages and are never required by core.

## Discovery

- Explicit filename and directory search.
- Ordered search-place lists using application names and supported extensions.
- Optional upward search with an explicit stop directory.
- Optional platform user-config directory search.
- Search-all and search-first modes with deterministic results.
- Symlink policy, root containment, permission expectations, and discovered-path
  provenance.
- Production defaults MUST NOT silently traverse parent directories.
- No executable JavaScript-style configuration files or code evaluation.

## Layering And Precedence

The default precedence, from lowest to highest, is:

1. Struct/programmatic defaults.
2. Discovered base configuration file.
3. Environment/profile-specific discovered file.
4. Explicit files in caller-provided order.
5. Dotenv values.
6. Existing process environment variables.
7. Explicit programmatic overrides.

Callers may construct another order explicitly. The resolved plan MUST be
inspectable before loading and the final snapshot MUST expose safe provenance
without exposing secret values.

Merge semantics MUST be explicit for objects, maps, slices, scalars, nulls, and
deletions. Slices replace by default; implicit append and index-wise merging are
forbidden. Type changes across layers fail unless a documented decoder handles
the conversion.

## Environment Mapping

- Explicit `env` tags and configurable prefixes.
- Deterministic nested-field mapping and separator rules.
- Detection of collisions after normalization.
- Required, optional, default, secret, deprecated, and ignored field metadata.
- Typed decoding for booleans, integers, floats, durations, timestamps, URLs,
  byte sizes, string slices, maps, enums, and text-unmarshaling types.
- Existing process environment wins over dotenv by default.
- Loading MUST NOT attach values to or mutate the process environment.
- Environment-name case behavior is explicit across supported operating systems.

## Defaults, Interpolation, And Validation

- Programmatic options and struct tags may define defaults with one documented
  precedence model.
- Optional `${NAME}` interpolation is explicit, bounded, cycle-detected, and
  resolved against a caller-selected source view.
- Missing interpolation variables fail unless a documented default is supplied.
- Types may implement a small `Validate() error` contract.
- Callers may register typed post-decode validators.
- Validation runs only after a complete candidate snapshot is decoded.
- Validation errors preserve field paths and redact sensitive values.
- `config` MUST NOT impose a general validation framework on applications.

## Secret Handling

- Provide a small secret wrapper or metadata contract whose formatting,
  marshaling, error, and logging behavior is redacted by default.
- Secret values MUST never appear in provenance, diffs, diagnostics, telemetry,
  panic output, or validation summaries.
- Snapshot inspection reports source and presence, not secret content.
- File permission checks are available for secret-bearing files.
- The package loads secrets but is not their system of record.

## Infisical Boundary

### Production Kubernetes Default

Infisical integration SHOULD be handled by the Kubernetes/platform layer:

- Infisical Operator may synchronize values into Kubernetes Secrets and reload
  dependent workloads.
- Infisical CSI may mount static secrets directly as files without creating
  Kubernetes Secret objects.
- The Agent Injector may render secrets into a shared pod volume.
- `config` consumes the resulting environment variables or mounted files like
  any other source and remains unaware of Infisical credentials and lifecycle.

This keeps Infisical network access, machine identity, token refresh, secret
rotation, and outage behavior out of each application binary.

### Optional Native Adapter

An optional `infisical` adapter MAY be implemented after the core reaches a
stable release for non-Kubernetes applications, local tools, or genuine
on-demand secret retrieval.

If implemented, it MUST:

- live in a separately imported package so core users do not pull the SDK
- use the official Infisical Go SDK behind the generic source contract
- be read-only and MUST NOT create, update, delete, or push secrets
- support explicit project, environment, path, imports, and organization scope
- support Kubernetes identity and selected machine-identity authentication
- never attach fetched secrets to process environment
- define startup, cache, stale-value, retry, token-refresh, outage, and shutdown
  semantics precisely
- default to fail-closed for required secrets
- redact all secret values, tokens, identity credentials, and sensitive paths
- have independent compatibility and integration matrices

The native adapter MUST NOT be required or preferred for the existing
Kubernetes deployments.

## Common Configuration Types

Core MAY provide format-independent scalar helpers such as:

- `Secret`
- `Duration`
- `ByteSize`
- validated URL and network endpoint values
- optional values preserving presence
- generic string-list and string-map decoders

Core MUST NOT define AWS, GCP, Azure, PostgreSQL, Redis, Valkey, SFTP, OAuth,
OTLP, or vendor credential structs. Their owning integration packages define
those contracts and avoid central configuration-version coupling.

## Integration Boundaries

- `service` uses `config` for startup configuration and no longer owns
  environment parsing.
- `wire` may provide bounded JSON/YAML/TOML decoding internals where its API
  preserves the strict configuration semantics required here.
- `log` and `telemetry` integrations expose source and validation events
  without values or high-cardinality keys.
- Package dependencies MUST remain acyclic and optional format/remote adapters
  MUST not inflate core dependencies.

## Non-Goals

- No global singleton, package-level mutable registry, or implicit `init` load.
- No service locator or dependency injection container.
- No automatic hot reload in v1.
- No executable configuration files or arbitrary expression language.
- No remote configuration server in core.
- No secret creation, rotation, write-back, or secret-manager administration.
- No universal cloud/database credential catalog.
- No implicit filesystem traversal or implicit `.env` loading in production.
- No mutation of process environment.

## Package Shape

- Root package: loader, plan, snapshot, source, provenance, options, errors.
- `decode`: typed decoding, tags, scalar hooks, and strict field mapping.
- `merge`: deterministic typed-tree merge behavior.
- `discover`: explicit Cosmiconfig-style file discovery.
- `json`, `yaml`, `toml`, `dotenv`, `environment`, and `filesystem` sources.
- `validation`: small validation orchestration and error aggregation.
- `configtest`: deterministic sources, environments, filesystems, and assertions.
- `infisical`: optional post-core native source adapter.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
precedence, presence, merge, decoding, discovery, redaction, validation,
provenance, cancellation, and failure behavior rather than merely execute lines.

Required verification includes:

- exhaustive precedence and merge truth tables
- format conformance and cross-format equivalence fixtures
- duplicate, unknown, null, empty, default, optional, deprecated, and collision
  tests
- hostile file, environment, interpolation, tag, and destination-type fuzzing
- symlink, traversal, permission, partial-read, cancellation, and filesystem race
  tests
- no-secret-leak assertions across every error and diagnostic path
- deterministic parallel-load and race-detector suites
- source-plan, decode, merge, validation, and large-config benchmarks
- optional Infisical adapter integration and failure-injection tests

## Documentation Deliverables

- Complete API reference and five-minute typed configuration quickstart.
- JSON, YAML, TOML, dotenv, environment, defaults, discovery, precedence,
  merging, interpolation, validation, secrets, and provenance guides.
- Kubernetes ConfigMap, Secret, Infisical Operator, CSI, and mounted-file recipes.
- Migration guide from Laravel configuration and direct `os.Getenv` usage.
- Package-author guide for defining reusable component configuration structs.
- Security, operations, troubleshooting, compatibility, performance, FAQ,
  contribution, examples, and maintained `CHANGELOG.md` documentation.
- Every user-facing source and composition scenario MUST have runnable examples.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, unit and integration tests,
exact meaningful coverage, race tests, fuzz smoke tests, vulnerability scanning,
cross-platform path/environment matrices, format interoperability, benchmarks,
docs, examples, API compatibility, and release automation. Optional Infisical
workflows MUST be isolated and MUST NOT expose credentials to untrusted builds.

## Execution Plan

1. Specify source, plan, snapshot, precedence, merge, presence, and error models.
2. Implement typed decoding, defaults, environment, provenance, and validation.
3. Implement strict JSON, YAML, TOML, dotenv, filesystem, and discovery sources.
4. Complete secret redaction, hostile-input, race, cross-platform, and benchmark
   hardening.
5. Integrate `service`, package-owned config types, and Kubernetes recipes.
6. Evaluate the optional Infisical adapter against demonstrated non-Kubernetes
   or on-demand requirements after core stabilization.
7. Publish complete adoption, migration, operations, and API documentation.

## Acceptance Criteria

- Caller-owned structs load deterministically from every supported source.
- Precedence, merge, presence, provenance, redaction, and validation semantics
  are explicit and mechanically verified.
- Production Kubernetes services consume Infisical-delivered environment or file
  sources without an Infisical SDK dependency.
- Optional remote adapters cannot inflate or weaken core behavior.
- Meaningful 100% coverage and every GitHub Actions gate pass.
- Documentation enables adoption without source inspection.
- `CHANGELOG.md` records every user-visible and compatibility change.
