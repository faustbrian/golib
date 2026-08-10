# tenancy

`tenancy` is a small explicit foundation for tenant isolation. It transports
validated tenant identity and makes tenant-bound, system-wide, and deliberately
unscoped work distinct in Go APIs. It does not authenticate callers, decide
membership, or authorize access.

## Core model

Tenant IDs are case-sensitive opaque ASCII values. They are preserved exactly,
bounded to 128 bytes, and never inferred from arbitrary request data. Their
`String` representation is redacted; use `Value` or serialization methods only
at trusted transport and persistence boundaries.

```go
id, err := tenancy.ParseTenantID("customer-42")
if err != nil {
    return err
}
scope, err := tenancy.NewTenantScope(id, tenancy.Metadata{})
if err != nil {
    return err
}
ctx, err = tenancy.WithScope(ctx, scope)
```

`WithScope` preserves the parent context and rejects any attempt to replace an
existing distinct scope. `RequireTenant`, `AssertTenant`, and `AssertScope`
provide fail-closed application and persistence seams.

System-wide work requires a deliberately constructed `SystemCapability` with
an actor and purpose. This records intent; it does not grant permission. The
application remains responsible for authorizing capability construction.

## Opaque namespaces

`NamespaceEncoder` uses HMAC-SHA-256 over versioned, length-delimited scope,
domain, and logical-key input. Its `tn2_` lowercase hexadecimal output is safe
for first-party provider resource names, including OpenSearch indexes. It
prevents ambiguous concatenation and keeps raw tenant IDs and logical keys out
of cache, search, queue, scheduler, idempotency, event, workflow, rate-limit,
and telemetry namespaces. Callers own and rotate the encoder key. Version 1
names require the bounded migration described in `docs/migration.md`.

## Security boundary

Tenant identity is routing and isolation data, never authorization evidence.
This module cannot guarantee isolation when a consumer bypasses its enforcement
seams. Transport trust, PostgreSQL patterns, administrative iteration, async
integration, analyzers, migration guidance, and the complete threat model are
documented with the corresponding adapters.

## Propagation and trust

`PropagationCodec` is the transport-neutral contract used by queue, outbox,
Kafka, CloudEvents, audit, correlation, idempotency, cache, rate-limit, search,
scheduler, workflow, event-sourcing, and telemetry integrations. Extraction
parses metadata but accepts it only when the caller supplies an explicit trust
decision. Missing, repeated, conflicting, untrusted, malformed, oversized, and
pre-existing values fail with distinct errors.

The `http` adapter requires a trust function for the authenticated immediate
peer. Direct backend requests and forwarded headers are untrusted unless that
function proves the boundary. It scans header names case-insensitively so map
entries with different casing cannot conceal duplicates. The `jsonrpc` adapter
parses a bounded raw metadata object before map decoding, so duplicate JSON keys
cannot be silently collapsed.

`Integration` names every first-party boundary and provides `Send`, `Receive`,
and opaque `Key` operations. Queue retries and redeliveries carry tenant scope
explicitly; extraction never promotes system or unscoped work into a tenant.

## Background and administrative work

`Group` owns every goroutine it starts. Submission is bounded and cancellable;
`Close` drains work and `Shutdown` cancels it. Each task receives only its
submitted immutable scope while preserving the submission context's values,
deadline, and cancellation. Group-parent cancellation also cancels every task.
`Submit` rejects a conflicting scope synchronously before acquiring capacity or
starting a goroutine. `GroupOptions.HandleError` may be called concurrently by
independently completing tasks.

`IterateTenants` requires a system scope with an administrative actor, purpose,
and optional reference, plus a mandatory audit callback. It reads bounded pages
from a consumer-owned `TenantPager`, returns exact page-and-offset resume tokens,
and derives every operation from the original unscoped base context. Tenant
state therefore cannot survive into the next iteration. The capability records
intent but applications must still authorize the operation.

## PostgreSQL

The `postgres` adapter keeps query and session enforcement explicit:

- `Predicate` returns a quoted tenant equality clause and its owned argument;
  callers still place the clause in every applicable query.
- `Manager.WithTenant` leases one `database/sql` connection, clears any stale
  session value, begins a transaction, installs the tenant with transaction-local
  `set_config`, verifies it by reading it back, and resets the same leased
  connection before it can return to the pool.
- `Manager.WithSystem` accepts only explicit system scope and installs an empty
  tenant setting. It does not bypass RLS or grant database privileges.
- `NewRLSPlan` returns quoted `ENABLE`, `FORCE`, paired permissive and
  restrictive `CREATE POLICY`, and rollback statements. Applications apply
  these statements through their migration
  owner and should run application traffic through a non-owner role because
  table owners and privileged roles can otherwise bypass RLS.

The supplied RLS expression fails closed when the custom setting is absent or
has been reset. Connection reset failures cause the physical connection to be
discarded. Operations must use only the transaction passed to their callback;
opening another connection or issuing tenant queries outside it bypasses this
enforcement seam.

## Testing

`tenancytest` provides test-only tenant and system scope constructors, context
installation, and tenant assertions. The package includes hostile-input fuzz
targets, randomized cross-tenant namespace models, concurrent isolation stress,
and allocation-reporting benchmarks for context propagation, namespace
encoding, and tenant assertions. These tests prove the owned enforcement seams;
they cannot prove isolation in application paths that bypass those seams.

Detailed adoption and security guidance is in [`docs/`](docs/): trust and
service propagation, integrations, PostgreSQL/RLS, administration, migration,
the exhaustive boundary inventory, static-analysis boundaries, hardening
evidence, security caveats, and FAQ.
The checked-in `analysis.yml` and `make analyzers` fixture provide executable
negative proof for declared direct-provider, context-replacement, and telemetry
cardinality bypasses.
The current threat-to-test mapping and residual trust boundaries are recorded
in [`docs/security-review.md`](docs/security-review.md).
