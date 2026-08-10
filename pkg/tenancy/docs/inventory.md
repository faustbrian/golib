# Tenancy boundary inventory

This is the exhaustive owned-surface inventory used by the security review.
Application and provider code remains in scope only when it uses one of these
seams or one of the declared executable consumer fixtures.

## Identity and scope representations

| Representation | Owner and invariant |
| --- | --- |
| Raw tenant string or bytes | Exists only at trusted wire and persistence boundaries; `ParseTenantID`, `UnmarshalText`, and `UnmarshalJSON` validate without trimming, folding, or normalization. |
| `TenantID` | Immutable, case-sensitive, bounded opaque identity; zero is invalid and diagnostic formatting is redacted. |
| `Metadata` | Immutable bounded routing metadata; copied on construction and return and prohibited from carrying authorization or secrets. |
| `Scope` and `ScopeKind` | Exactly one tenant, explicit system work, or deliberate unscoped work; zero and mixed states are invalid. |
| `AdministrativeReason` | Bounded accountable actor, purpose, and optional reference; redacted by default. |
| `SystemCapability` | Explicit construction token recording system intent; it grants no authorization. |
| `Carrier` and `MapCarrier` | Bounded multi-value wire representation used before trust-gated extraction. |
| HTTP header | Configurable field, default `X-Tenant-ID`; values remain untrusted until the immediate-peer callback accepts the request. |
| JSON-RPC metadata | Bounded raw JSON object; duplicate keys are detected before map decoding. |
| PostgreSQL predicate argument | Raw tenant value owned by `QueryPredicate.Arguments` for an explicit equality predicate. |
| PostgreSQL transaction setting | Transaction-local raw tenant value in `app.tenant_id` or the configured setting; read back before work and after the callback. |
| Opaque namespace | `tn2_` plus lowercase hexadecimal HMAC-SHA-256 over version, scope, tenant, domain, boundary, and logical key. |
| Provider metadata | Application-owned queue, event, cache, search, workflow, audit, and telemetry representations covered only by the declared composition fixtures. |

## Context, propagation, and enforcement

| Surface | Owned operations |
| --- | --- |
| Context installation | `WithScope`; equal scope is idempotent and any different pre-existing scope is rejected. |
| Context extraction | `ScopeFromContext`, `RequireScope`, `RequireTenant`, `RequireSystem`, and `RequireUnscoped`. |
| Enforcement | `AssertTenant`, `AssertScope`, `TenantAsserter`, `TenantAssertFunc`, and `RunScoped`. |
| Generic propagation | `PropagationCodec.Extract`, `Inject`, `Accept`, and `InjectFromContext`; `Integration.Send` and `Receive` bind the same policy to a semantic boundary. |
| HTTP | `Adapter.Extract`, `Accept`, `Inject`, and `Wrap`; the trust function authenticates the immediate hop and `ErrorHandler` owns rejection responses. |
| JSON-RPC | `Codec.Extract`, `Accept`, and `Inject`; the trust function authenticates the immediate hop. |
| Background work | `Group.Submit`, `Close`, and `Shutdown`; the group owns concurrency, cancellation, task lifetime, and error delivery. |
| Administration | `IterateTenants`, `TenantPager`, `AdministrativeAudit`, `ResumeToken`, and bounded iteration options. |

Every context-producing path derives from its caller or group parent. No owned
path replaces a deadline or cancellation chain with `context.Background`.

## Namespace and integration encoders

`NamespaceEncoder.Encode` is the only owned opaque namespace primitive.
`Integration.Key` adds a length-delimited semantic boundary and accepts tenant
scope only. The integration inventory is queue, outbox, Kafka, CloudEvents,
audit, correlation, idempotency, cache, rate limit, search, scheduler,
workflow, event sourcing, and telemetry. The namespace-domain inventory is
cache, idempotency, rate limit, search, queue, scheduler, event, workflow, and
telemetry. Several semantic boundaries intentionally share a domain but remain
separated by the embedded boundary value.

## Persistence and provider seams

| Seam | Isolation owner |
| --- | --- |
| `postgres.Predicate` | Caller must place the returned clause and argument in every applicable query. |
| `postgres.NewRLSPlan` | Migration owner must apply forced RLS plus both permissive-grant and restrictive policies as one unit. |
| `postgres.Manager.WithTenant` | Leases one physical connection, clears stale state, begins a transaction, installs and verifies local scope, re-verifies after the callback, rolls back on failure, resets before pool return, and discards on reset failure. |
| `postgres.Manager.WithSystem` | Installs an empty tenant setting for explicit system scope; it grants no RLS bypass. |
| Cache consumer adapter | Requires tenant context and uses the cache integration namespace before the first-party memory backend. |
| Search consumer adapter | Requires tenant context and rejects request/document mismatch before the first-party contract provider or live OpenSearch client. |
| Queue and event consumer adapter | Validates tenant metadata on emission and replay and rejects retained/wire conflicts through the CloudEvents Golib adapter. |
| Workflow consumer | Persists validated tenant identity on work and rejects malformed retry metadata. |
| Audit consumer | Stores separately tenant-attributed records and queries an exact tenant scope. |
| Telemetry consumer | Drops tenant identity from metric attributes and applies a hard stream-cardinality limit. |
| Administrative fan-out consumer | Uses bounded concurrency and an fsync-before-rename per-tenant journal for operation identity, attribution, attempts, partial failure, and resume. |

## Analyzers and fixtures

`analysis.yml` blocks direct construction of the declared cache, audit, queue,
workflow/PostgreSQL, and OTLP providers; replacement contexts; and `TenantID`
metric labels. The negative consumer fixture must emit the exact expected
diagnostics. Only the exact reviewed adapter fixture is exempt. The clean
consumer, live PostgreSQL, and live OpenSearch lanes are executable composition
evidence; analyzer success is never substituted for runtime proof.

## Errors and diagnostics

Core errors are `ErrInvalidTenantID`, `ErrInvalidMetadata`,
`ErrCapabilityRequired`, `ErrInvalidAdministrativeReason`,
`ErrInvalidContext`, `ErrScopeRequired`, `ErrTenantScopeRequired`,
`ErrSystemScopeRequired`, `ErrConflictingScope`, `ErrTenantMismatch`,
`ErrInvalidPropagation`, every `ErrTenantMetadata*` classification,
`ErrInvalidNamespaceKey`, `ErrInvalidNamespaceInput`,
`ErrInvalidIntegration`, `ErrInvalidOperation`, `ErrInvalidGroup`,
`ErrGroupClosed`, and `ErrInvalidIteration`.

HTTP adds `ErrInvalidOptions` and `ErrInvalidRequest`. JSON-RPC adds
`ErrInvalidOptions`, `ErrInvalidContext`, `ErrInvalidMetadata`, and
`ErrOversizedMetadata` in its package. PostgreSQL adds
`ErrInvalidIdentifier`, `ErrInvalidParameter`, `ErrInvalidRLSOptions`,
`ErrInvalidConfig`, `ErrInvalidOperation`, `ErrScopeVerification`, and
`ErrSessionReset`. Callers classify with `errors.Is`; errors do not include raw
tenant values. `TenantID`, `Metadata`, `AdministrativeReason`,
`SystemCapability`, and `Scope` redact `String` and `GoString` output.

## Administrative and trust escape hatches

The following are deliberate privileged seams and must remain visible in
application review:

- `TenantID.Value`, text/JSON marshaling, and PostgreSQL arguments disclose the
  raw ID at an explicit trusted boundary.
- `MustTenantID` panics and is limited to static configuration and tests.
- application authorization controls creation of `SystemCapability` and use of
  system or unscoped scope;
- HTTP and JSON-RPC trust callbacks decide immediate-peer authenticity;
- custom `Carrier` implementations own value bounds and replacement behavior;
- PostgreSQL callbacks receive `*sql.Tx`, so application interfaces and
  restricted roles must prevent setting manipulation or alternate connections;
- system database roles, direct provider clients, unscoped legacy keys,
  reflection, dynamic SQL, generated calls, and undeclared wrappers bypass the
  owned seams and are not made safe by this package;
- analyzer `allowed_packages` entries are exact reviewed exceptions and must
  not be broadened; and
- local-file administrative fan-out is single-host only; multi-host execution
  requires an application-owned shared durable ledger.
