# Goal: Explicit Tenant Isolation

## Objective

Build `tenancy` as a small, explicit Go foundation for propagating tenant
identity and enforcing tenant isolation across HTTP, RPC, jobs, PostgreSQL,
cache keys, events, telemetry, and background execution.

The package MUST make tenant scope visible in APIs. It MUST NOT use ambient
global state, goroutine-local tricks, reflection-driven model scoping, hidden
query rewriting, or Laravel-style magic.

## Tenant Model

Provide validated typed tenant identifiers, optional tenant metadata, and
explicit representations for tenant-bound, system-wide, and intentionally
unscoped operations. Empty or malformed identity MUST fail closed where tenant
scope is required. System scope MUST require an explicit capability rather than
an empty tenant ID.

Specify normalization, comparison, serialization, maximum size, redaction, and
ownership. Tenant IDs MUST remain opaque and MUST NOT encode authorization.

## Propagation

- Provide context helpers that reject conflicting tenant identities and retain
  cancellation/deadlines.
- Provide HTTP and JSON-RPC extraction/injection adapters with configurable,
  validated trust boundaries.
- Provide queue, outbox, Kafka, CloudEvents, audit, correlation, idempotency,
  cache, workflow, and event-sourcing integration contracts.
- Missing, duplicate, conflicting, malformed, spoofed, or oversized tenant
  metadata MUST have deterministic errors.
- Forwarded tenant headers MUST never be trusted merely because they exist.

## Enforcement

Define small consumer-facing contracts for asserting scope at application and
persistence boundaries. PostgreSQL support MUST cover explicit predicates,
transaction-local tenant settings where used, Row-Level Security integration,
connection-pool reset safety, and tests proving no cross-tenant leakage.

Cache, idempotency, rate-limit, search, queue, scheduler, and telemetry helpers
MUST compose tenant identity into namespace keys without collision or exposing
secret tenant data. Raw string concatenation is insufficient unless encoding
is unambiguous and tested.

The package MUST NOT decide user permissions, tenant membership, subscription
state, or authorization policy. Those belong to applications and
`authorization`.

## Administrative Operations

Cross-tenant maintenance, migrations, imports, support access, and analytics
MUST use explicit scopes, reason metadata, and audit integration. APIs MUST
make accidental promotion from one tenant to all tenants difficult. Iterating
tenants MUST be bounded, cancellable, resumable, and must not retain tenant
state across iterations.

## Tooling

Provide test helpers and, where technically credible, analyzers that detect:

- tenant-required operations invoked without explicit scope;
- unsafe cache or idempotency keys lacking tenant namespace;
- context replacement that drops tenant identity;
- persistence adapters that reuse tenant-scoped sessions unsafely;
- telemetry using raw tenant IDs as high-cardinality labels.

Analyzers MUST document false-positive boundaries and MUST not replace runtime
enforcement.

## Verification

Tests MUST prove propagation, conflicts, spoofing resistance, explicit system
scope, nested operations, pool reuse, transaction rollback, RLS behavior,
cache/search/event key separation, asynchronous execution, cancellation,
shutdown, and hostile identifiers. Include concurrency stress and property
tests that no operation for tenant A can observe or mutate tenant B. Exact 100%
statement coverage and 100% viable mutation kills are REQUIRED.

## Documentation And Delivery

Document trust models, extraction policies, service-to-service propagation,
PostgreSQL and RLS patterns, administrative access, integrations, migration
from ad hoc tenant fields, security caveats, FAQ, and complete examples. Add
repository manifests, CI, benchmarks, changelog, and clean-consumer proof.

## Non-Goals

- authentication, authorization, billing, organizations, or user membership;
- automatic tenant discovery from arbitrary request data;
- hidden ORM/query scoping;
- guaranteeing isolation when consumers bypass all enforcement seams.
