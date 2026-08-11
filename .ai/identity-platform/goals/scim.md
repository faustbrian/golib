# Goal: pkg/scim

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `scim`
- Canonical module: `pkg/scim`
- Canonical goal after scaffolding: `pkg/scim/.ai/GOAL.md`
- Requires: `identity`, `organization`
- Consumes existing primitives: `authentication`, `authorization`, `identifier`, `audit`, `openapi`
- Unlocks after verification: `scim/postgres`, `scim/organization`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `scim` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/scim` module that owns SCIM 2.0 protocol server, ServiceProviderConfig, schemas, resource types, Users and Groups, filtering, sorting, pagination, PATCH, bulk policy, ETags, and error semantics. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns SCIM 2.0 protocol server, ServiceProviderConfig, schemas, resource types, Users and Groups, filtering, sorting, pagination, PATCH, bulk policy, ETags, and error semantics. It does not own organization-specific resource mapping, persistence, SSO, vendor connections, and admin UI. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define HTTP-independent request/response model, Resource, Schema, Attribute, Filter AST, Patch, ListResponse, Bulk policy, Version, Authenticator, Authorizer, Mapper, and Store contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST parse bounded filters and paths; apply PATCH atomically; enforce mutability and returned policy; paginate and sort deterministically; use ETags for concurrency; preserve SCIM error status/type/detail redaction; handle bulk dependency ordering and limits; test RFC examples and independent clients. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Protocol discovery MUST expose ServiceProviderConfig, Schemas and
  ResourceTypes that exactly match implemented Users, Groups, filters, sort,
  PATCH, bulk and authentication capabilities.
- User and Group operations MUST include create, get, list/filter/page/sort,
  replace, atomic PATCH and delete/deactivate with SCIM content types, canonical
  schemas and location/version metadata.
- Filter and path grammar MUST support the declared RFC 7644 operators,
  precedence, value types, multi-valued attributes and extension URNs. A
  capability omitted from the parser/planner MUST be omitted from discovery.
- PATCH MUST implement add/remove/replace for scalar, complex and multi-valued
  attributes with mutability, required, uniqueness and caseExact semantics and
  all-or-nothing resource versioning.
- Authentication MUST support scoped digested bearer tokens with reveal-once
  generation, rotation/revocation, expiry and organization/provider ownership.
- Provider connections MUST support create/list/get/delete, ownership policy,
  safe provider metadata, attribute mappings and hooks. A default connection
  MUST not bypass organization scope.
- Bulk, if advertised, MUST enforce operation/payload limits, failOnErrors,
  bulkId dependencies and per-operation results without cross-tenant references.
- Error responses MUST use stable SCIM status/scimType/detail with enumeration
  and provider diagnostics redacted. ETag/If-Match conflicts MUST not be
  reported as successful replacement.
- RFC examples plus at least one independent SCIM client/conformance suite MUST
  prove discovery and every advertised operation.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.

## Persistence, lifecycle, and compatibility

The core MUST remain adapter-neutral unless this goal is itself an adapter.
State ownership, consistency, retention, deletion, migration, key rotation,
clock skew, concurrent callers, shutdown, and recovery MUST be documented and
tested where applicable. Unsupported protocol or deployment profiles MUST be
stated rather than silently approximated.

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific behavior above, the module's exact coverage and mutation
gates, race/fuzz/interoperability gates that apply, clean-consumer proof,
manifests, public API baseline, security and supply-chain checks, documentation,
changelog, and changed reverse-dependant gates. The final evidence record MUST
name any non-applicable gate with a reviewed reason; absence of infrastructure
or provider access is a blocker, not a pass.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
