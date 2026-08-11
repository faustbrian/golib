# Goal: pkg/scim

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

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
  PATCH, Bulk and authentication capabilities. Discovery collections and
  documents MUST be bounded by `scim.resource_depth`,
  `scim.resource_attributes` and `scim.string_bytes`;
  ServiceProviderConfig MUST publish the effective manifest values for
  `scim.page_max`, `scim.bulk.operations` and `scim.bulk.bytes`, including the
  decoded Bulk byte limit, and MUST NOT advertise a configured maximum that the
  runtime cannot enforce.
- User and Group operations MUST include create, get, list/filter/page/sort,
  replace, atomic PATCH and delete/deactivate with SCIM content types, canonical
  schemas and location/version metadata.
- List behavior MUST follow the SCIM baseline exactly: pagination is 1-based;
  omitted `startIndex` is 1; omitted `count` is `scim.page_default`; count is
  capped at `scim.page_max`; and `count=0` returns no resources with exact
  `totalResults`, `startIndex` and `itemsPerPage=0`. Each list observes one
  transaction snapshot, and sorting MUST use schema comparison semantics plus
  a stable server-generated `id` tie-break.
- Each SCIM connection MUST be owned by one tenant organization and provider,
  and every token, resource, external ID, bulk reference and audit event MUST
  inherit that scope. Personal or unowned connections MUST be rejected; any
  legacy personal connection requires an explicit migrate-to-organization,
  disable or delete disposition before this unit can be verified.
- Filter and path grammar MUST support the declared RFC 7644 operators,
  precedence, value types, multi-valued attributes and extension URNs. A
  schema-valid advertised filter or sort MUST use an indexed plan or a bounded
  fallback; only invalid grammar or paths are unsupported. Filter comparison
  and sort ordering MUST honor schema type and `caseExact`; sorting a
  multi-valued attribute MUST use the RFC-selected primary/value semantics and
  then the stable server `id` tie-break. A resource-budget failure MUST return a
  stable SCIM error, never partial results or an incorrect `totalResults`.
  Parser and resource admission MUST consume the effective manifest values for
  `scim.filter_bytes`, `scim.filter_tokens`, `scim.filter.depth`,
  `scim.filter.nodes`, `scim.path_bytes`, `scim.resource_depth`,
  `scim.resource_attributes`, `scim.string_bytes`, `scim.group_members`,
  `scim.patch.operations`, `scim.bulk.operations`, `scim.bulk.bytes`,
  `scim.bulk.fail_on_errors`, `scim.bulk.operation_bytes`,
  `scim.bulk.response_bytes` and `scim.error_detail_bytes`, enforcing each
  applicable limit before allocation or mutation.
- PATCH MUST implement add/remove/replace for scalar, complex and multi-valued
  attributes with mutability, required, uniqueness and caseExact semantics and
  all-or-nothing resource versioning.
- The public protocol contract MUST expose a typed write-only credential
  decision seam. The reference profile MUST reject every `password` write as
  unsupported at that seam before invoking any Mapper or Store. A future
  approved password-write profile MUST route accepted values only through the
  public `identity/password` contract. `password` and every attribute with
  `returned: never` MUST be omitted from responses, events, audit, errors and
  persisted SCIM projections; reads, comparison and filter/sort use MUST fail
  without revealing whether a value exists.
- Authentication MUST support scoped digested bearer tokens with reveal-once
  generation, rotation/revocation, expiry and organization/provider ownership.
- Provider connections MUST support create/list/get/delete, ownership policy,
  safe provider metadata, attribute mappings and hooks. A default connection
  MUST NOT bypass organization scope.
- Connection deletion MUST define disable-first behavior and disposition of
  tokens, mappings, external-ID links, pending bulk work and provisioned users/
  groups. It MUST NOT silently delete personal identities or organization
  memberships owned by another module.
- `externalId` uniqueness and lookup MUST use the exact tuple `(tenant,
  organization, provider connection, resource type, externalId)` and the
  resource schema's exact `caseExact` comparison. The same upstream value MAY
  exist outside that tuple; collation or generic identifier canonicalization
  MUST NOT widen, fold or otherwise alter it.
- Bulk, if advertised, MUST enforce maximum operations, payload and per-
  operation resource limits before execution; validate unique `bulkId` values;
  resolve only prior in-request dependencies in the same connection; reject
  cycles/forward, cross-tenant and cross-request references; honor
  `failOnErrors`; and return ordered per-operation status/location/version/error
  without claiming whole-request atomicity. It MUST consume the SCIM Bulk
  contract in `.ai/identity-platform/TRANSACTION_CONTRACT.md`: persist one
  parent and every ordered independently identified child at admission, commit
  each executing child independently, durably mark all remaining not-started
  children skipped only after a positive `failOnErrors` threshold reaches that
  many durable failed child results, treat zero or omission as no cutoff, block
  unknown dependencies for reconciliation, and replay every declared child in
  order from its durable checkpoint.
- Every SCIM mutation MUST consume the HTTP/SCIM idempotency-admission contract
  in `.ai/identity-platform/TRANSACTION_CONTRACT.md`. A matching scoped key and
  fingerprint MUST recover the same command and semantic result, a mismatch
  MUST conflict without mutation, and an in-progress or unknown mapping MUST
  remain blocked. DELETE MUST retain and replay its original result for the
  `scim.delete_tombstone_retention` and `scim.idempotency_retention` contracts;
  a different key follows normal precondition/not-found behavior.
- Error responses MUST use stable SCIM status/scimType/detail with enumeration
  and provider diagnostics redacted. Opaque resource versions MUST produce
  stable ETags; GET/list/write responses MUST expose the version required for
  the next mutation. Replace and PATCH MUST require and atomically evaluate
  `If-Match` in the reference profile; missing or stale preconditions MUST fail
  without mutation and MUST NOT be reported as successful replacement.
- These contracts MUST satisfy SCIM journey 11 in
  `.ai/identity-platform/END_STATE.md`, the token and `If-Match` policy in
  `.ai/identity-platform/REFERENCE_PROFILE.md`, and deletion/redaction
  boundaries in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.
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
