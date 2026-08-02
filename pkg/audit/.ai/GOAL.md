# Goal: Durable Audit Records

## Objective

Build `audit` as an infrastructure-neutral Go library for recording immutable,
security-relevant and business-relevant actions with enough context to answer
who did what, to which resource, when, from where, and with what outcome.

Audit records MUST remain distinct from application logs, traces, metrics,
event-sourcing history, and domain events. The package MUST NOT imply that an
event store is automatically a compliant audit trail.

## Product Boundary

The core MUST provide explicit contracts for records, actors, subjects,
actions, outcomes, changes, sinks, queries, redaction, integrity, and retention.
It MUST NOT own authentication, authorization decisions, business policies,
transport middleware, tenancy discovery, or application-specific event names.

Optional storage and transport integrations MUST be adapters. PostgreSQL MUST
be the first durable adapter. A bounded in-memory adapter MUST support tests.

## Record Contract

Define an immutable-by-contract record containing at least:

- globally unique record ID and occurrence/recording times;
- action, outcome, reason code, and safe description;
- actor kind, stable actor ID, authentication method, and delegated actor;
- subject/resource type and stable identifier;
- tenant, correlation, causation, request, trace, and idempotency identifiers;
- source service, version, environment, network origin, and user agent where
  policy permits;
- structured before/after changes or an explicit no-change state;
- policy/version metadata and extensible attributes with reserved namespaces;
- integrity sequence, previous digest, and digest where enabled.

Specify absent, empty, zero, unknown, anonymous, system, and deleted-identity
semantics. Maps and byte slices MUST have explicit ownership and size limits.
Record identity MUST not depend on map iteration, process-local counters, or
database-generated ordering alone.

## Recording And Delivery

- Every sink operation MUST accept `context.Context` and define cancellation,
  timeout, retry, duplicate, ordering, and partial-failure semantics.
- Single and bounded batch append MUST be supported.
- Callers MUST choose fail-closed, fail-open-with-alert, or durable-buffer
  policy explicitly; the core MUST not silently discard records.
- Duplicate submissions MUST be idempotent by record ID.
- Unknown append outcomes MUST be distinguishable from confirmed rejection.
- Integration with `outbox` MAY provide atomic business-write plus audit-write
  delivery without making `audit` depend on a concrete outbox implementation.
- Backpressure and maximum record, batch, and buffered-byte limits MUST be
  explicit.

## PostgreSQL Adapter

Provide a separately releasable PostgreSQL adapter using the repository's
PostgreSQL contracts where dependency direction permits. It MUST define:

- append-only schema and least-privilege roles;
- idempotent insertion and deterministic ordering;
- tenant-, actor-, subject-, action-, correlation-, and time-based indexes;
- bounded cursor pagination and stable export ordering;
- partitioning and archival guidance for high-volume installations;
- migration, rolling-upgrade, backup, restore, and reconciliation procedures;
- retention and legal-hold behavior without mutable historical rewriting.

Ordinary application credentials SHOULD NOT have update or delete authority.

## Integrity And Privacy

- Provide deterministic canonical encoding for optional hash chaining or
  Merkle anchoring without claiming non-repudiation by itself.
- Define chain partitioning, key rotation, verification, checkpointing,
  truncation detection, repair boundaries, and export verification.
- Redaction MUST occur before persistence and before diagnostics.
- Secrets, credentials, raw authorization headers, and unrestricted request or
  response bodies MUST be rejected or removed by policy.
- Data minimization, retention, erasure exceptions, pseudonymization, legal
  hold, and privileged-field access MUST be documented as caller policies.
- Cryptographic operations MUST use standard-library primitives and explicit
  key-provider contracts; this package MUST NOT invent cryptography.

## Query And Export

Queries MUST be bounded, deterministic, authorization-neutral, and explicit
about tenant scope. Provide cursor pagination, time ranges, stable filters,
streaming export, cancellation, and integrity verification. The package MUST
not decide whether a caller is allowed to read an audit record.

## Observability

Expose safe metrics and tracing hooks for accepted, rejected, buffered,
duplicated, failed, delayed, exported, and integrity-invalid records. Actor,
subject, tenant, and record IDs MUST NOT become unbounded metric labels.

## Verification

Meaningful tests MUST prove record validation, defensive ownership,
canonicalization, duplicate handling, unknown outcomes, redaction, chain
verification, query bounds, pagination, cancellation, transaction behavior,
retention boundaries, backup/restore, and adapter interoperability. Fuzz hostile
records and canonical encodings. Run race, leak, stress, fault-injection, and
PostgreSQL integration tests. Exact 100% statement coverage and 100% of viable
mutants killed are REQUIRED.

## Documentation And Delivery

Document threat model, compliance boundaries, adoption patterns, failure-mode
choices, schemas, retention, export, integrity verification, incident use,
migrations, FAQ, and complete API examples. Add repository manifests, CI gates,
benchmarks, changelog, license notices, and a clean external-consumer test.

## Non-Goals

- replacing logs, traces, event sourcing, SIEM, or authorization;
- defining business audit vocabularies for applications;
- claiming legal or regulatory compliance from library use alone;
- silently capturing arbitrary application state.
