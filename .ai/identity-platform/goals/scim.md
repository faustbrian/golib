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
- Public contracts: unit ID `contract:unit:scim:v1`; owned operation IDs: `contract:operation:identity.scim.bulk:v1`, `contract:operation:identity.scim.connection-create:v1`, `contract:operation:identity.scim.connection-delete:v1`, `contract:operation:identity.scim.connection-get:v1`, `contract:operation:identity.scim.connection-list:v1`, `contract:operation:identity.scim.connection-reconcile:v1`, `contract:operation:identity.scim.connection-rotate:v1`, `contract:operation:identity.scim.connection-token-revoke:v1`, `contract:operation:identity.scim.connection-update:v1`, `contract:operation:identity.scim.group-create:v1`, `contract:operation:identity.scim.group-delete:v1`, `contract:operation:identity.scim.group-get:v1`, `contract:operation:identity.scim.group-list:v1`, `contract:operation:identity.scim.group-patch:v1`, `contract:operation:identity.scim.group-replace:v1`, `contract:operation:identity.scim.group-search:v1`, `contract:operation:identity.scim.resource-type-get:v1`, `contract:operation:identity.scim.resource-types-list:v1`, `contract:operation:identity.scim.schema-get:v1`, `contract:operation:identity.scim.schemas-list:v1`, `contract:operation:identity.scim.search:v1`, `contract:operation:identity.scim.service-provider-config:v1`, `contract:operation:identity.scim.user-create:v1`, `contract:operation:identity.scim.user-delete:v1`, `contract:operation:identity.scim.user-get:v1`, `contract:operation:identity.scim.user-list:v1`, `contract:operation:identity.scim.user-patch:v1`, `contract:operation:identity.scim.user-replace:v1`, `contract:operation:identity.scim.user-search:v1`
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
  ServiceProviderConfig MUST publish `filter.maxResults` from effective
  `scim.page_max`, `bulk.maxOperations` from effective
  `scim.bulk.operations`, and `bulk.maxPayloadSize` from the effective decoded
  `scim.bulk.bytes` limit, and MUST NOT advertise a configured maximum that the
  runtime cannot enforce or substitute a different byte boundary.
  ServiceProviderConfig MUST use exactly the RFC 7643 Section 5 envelope and
  fields: the ServiceProviderConfig schema URN, optional configured
  `documentationUri`, the explicit supported objects for PATCH, Bulk, filter,
  change-password, sort and ETags, Bulk/filter limits, and the bounded runtime
  `authenticationSchemes` catalog. Every advertised Boolean, URI, limit and
  authentication scheme MUST come from the same effective runtime snapshot
  used by request admission; the reference profile MUST advertise change-
  password as unsupported and MUST advertise only authentication schemes it
  actually accepts. Each Schema resource MUST contain exactly the Schema core
  schema URN. Each ResourceType resource MUST contain exactly the ResourceType
  core schema URN, and each `schemaExtensions` element MUST be the exact RFC
  object with `schema` and explicit Boolean `required`; a bare URI list is
  forbidden.
  Schemas and ResourceTypes collection operations MUST return RFC ListResponse
  messages with the ListResponse schema URN, exact `totalResults`, `Resources`,
  `startIndex`, and `itemsPerPage`; bare arrays are forbidden.
- The decoder MUST reject duplicate JSON member names and any pair of member
  names equal under SCIM's case-insensitive attribute-name comparison in every
  core, extension, Bulk, PATCH, complex and multi-valued object before mapping,
  authorization, filtering, audit or persistence. First-wins, last-wins and
  case-dependent collision resolution are forbidden. Set-valued attributes
  MUST reject values that collide under their schema's canonical comparison.
- User and Group operations MUST include create, get, list/filter/page/sort,
  replace, atomic PATCH and delete/deactivate with SCIM content types, canonical
  schemas and location/version metadata.
- RFC 7644 POST search MUST be independently exposed at `.search`,
  `Users/.search`, and `Groups/.search`. Each operation MUST accept the exact
  SearchRequest message schema and return the same bounded RFC ListResponse,
  filter, sort, projection, authorization and pagination semantics as its GET
  list scope; all three routes and OpenAPI operations are mandatory.
- Writable User and Group input contracts MUST contain only client-writable
  schemas, externalId and attributes. Create, replace and Bulk resource inputs
  MUST NOT require or accept server-generated `id` or `meta`; response Resource
  contracts remain distinct and MUST include authoritative `id` and `meta`.
- List behavior MUST follow the SCIM baseline exactly: pagination is 1-based;
  omitted or below-one `startIndex` is 1; omitted `count` is
  `scim.page_default`; negative count is zero; count is capped at
  `scim.page_max`; and `count=0` returns no resources with exact `totalResults`,
  `startIndex` and `itemsPerPage=0`. `itemsPerPage` MUST equal the actual number
  returned. Absent `sortBy` uses server-generated `id` ascending and absent
  `sortOrder` means ascending. Each list's Resources and `totalResults` MUST
  come from one transaction snapshot, and sorting MUST use schema comparison
  semantics plus the stable server-generated `id` tie-break.
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
  applicable limit before mutation, before allocating filter node 257, and
  before descending past filter depth 16. The raw route body, decoded body,
  decoded BulkRequest, each decoded Bulk child, and streamed response MUST use
  their distinct manifest bounds; unsupported Content-Encoding MUST fail before
  decoding. `scim.path_bytes` applies to every filter attribute path, PATCH
  path, `sortBy`, `attributes`, and `excludedAttributes` path.
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
- Provider connections MUST additionally expose distinct
  `identity.scim.connection-update`,
  `identity.scim.connection-token-revoke` and
  `identity.scim.connection-reconcile` operations with exactly the access,
  CSRF, rate, idempotency and bounded result semantics in
  `API_OPERATIONS.md`; rotate and delete MUST NOT substitute for token revoke.
- Connection deletion MUST define disable-first behavior and disposition of
  tokens, mappings, external-ID links, pending bulk work and provisioned users/
  groups. It MUST NOT silently delete personal identities or organization
  memberships owned by another module.
- `externalId` MUST retain RFC 7643 `uniqueness: none`. Lookup MUST remain
  partitioned by tenant, organization, provider connection and resource type
  and honor schema `caseExact`, but equal values MAY identify multiple
  resources and MUST NOT create a server uniqueness conflict. Mapping MUST use
  authoritative SCIM `id` or return an explicit mapping conflict rather than
  taking over an account from `externalId` alone.
- Bulk, if advertised, MUST enforce maximum operations, payload and per-
  operation resource limits before execution; validate unique `bulkId` values;
  resolve bounded prior, forward, and circular in-request `bulkId` dependencies
  in the same connection as required by RFC 7644 Section 3.7. The resolver MUST
  build and durably persist the complete bounded graph and deterministic SCC
  plan before mutation, preallocate stable final resource IDs for create
  members, execute the SCC condensation graph topologically with original-index
  tie breaking, commit acyclic children independently, and commit each circular
  SCC as one bounded transaction with deferred within-SCC reference checks.
  Failure MUST roll back the complete SCC and produce deterministic per-member
  dependency failures without claiming whole-request atomicity; it MUST reject
  unknown, cross-tenant, and cross-request references;
  honor
  `failOnErrors`; and return ordered per-operation status/location/version/error
  without claiming whole-request atomicity. It MUST consume the SCIM Bulk
  contract in `.ai/identity-platform/TRANSACTION_CONTRACT.md`: persist one
  parent, every ordered independently identified child, and the graph/SCC plan
  at admission; commit each acyclic child independently and each cyclic SCC
  under its bounded atomic rule; durably mark all remaining admitted children
  that have not entered `running` skipped only after a positive `failOnErrors` threshold reaches that
  many durable failed child results, treat zero or omission as no cutoff, block
  unknown dependencies for reconciliation, and replay processed child results
  in request order from durable checkpoints. A skipped child is unprocessed and
  MUST be omitted from BulkResponse `Operations`; it has no wire status,
  location, version, or Error body, while its durable state and
  `identity.scim.bulk_skip_child` audit event remain mandatory. The SCIM reason
  extension `identity.scim.fail_on_errors_cutoff` is reserved exclusively for
  that audit event and MUST NOT appear as an unregistered SCIM `scimType`.
  The implementation MUST consume `struct:ref.scim.bulk_execution` and expose
  `skipped` only as the persisted unprocessed child state; status projections,
  recovery and replay MUST NOT convert it into a failed child or synthesize a
  wire status.
  Matching retry means the same connection scope and exact canonical Bulk
  fingerprint, including operation order, `failOnErrors`, targets, bodies and
  preconditions; it MUST recover the same durable parent and replay the same
  processed-child semantic results. Any mismatch MUST conflict before mutation
  and MUST NOT expose or reuse the prior result. A singleton acyclic SCC MUST
  commit its one child in one independent transaction. A cyclic SCC MUST stage
  all members, defer only within-SCC reference checks, and commit every member
  in one bounded transaction or roll the complete SCC back. The persisted child
  state machine MUST contain only admitted, running, succeeded, failed,
  dependency-blocked and skipped; only succeeded and failed may carry a result,
  dependency-blocked remains reconcilable, and skipped remains unprocessed.
  Every returned BulkResponse operation MUST include its method and MUST echo
  `bulkId` exactly when the request operation supplied it; POST operations MUST
  supply unique request-local bulk IDs. The request `failOnErrors` value MUST be
  zero/omitted or 1..the exact configured maximum 100 and MUST be rejected
  before admission when larger.
- Every SCIM mutation MUST consume the HTTP/SCIM idempotency-admission contract
  in `.ai/identity-platform/TRANSACTION_CONTRACT.md`. Server-owned command
  identity and a canonical request fingerprint are mandatory; `Idempotency-Key`
  is an optional extension rather than a protocol prerequisite. When supplied,
  a matching scoped key and fingerprint MUST recover the same command and
  semantic result, a mismatch MUST conflict without mutation, and an in-progress
  or unknown mapping MUST remain blocked. DELETE with the same scoped key and
  fingerprint MUST replay the original successful response without evaluating
  `If-Match` against its tombstone; the same key with a changed body or
  precondition MUST conflict. For DELETE, the canonical connection, target and
  precondition fingerprint MUST recover the same server-owned command and
  result even when the optional header is absent. A different target or
  precondition fingerprint follows current-state behavior. The original result remains replayable for
  `scim.idempotency_retention` even if `scim.delete_tombstone_retention` expires
  first.
- Error responses MUST use stable SCIM status/detail with enumeration and
  provider diagnostics redacted. Every error body MUST contain exactly the SCIM
  Error schema URN and MUST encode the actual HTTP status as a three-digit JSON
  string; numeric, missing, additional-schema or mismatched values are
  forbidden. `scimType` is optional and MUST be present only
  when the RFC registers one for that exact condition. Opaque resource versions MUST produce
  stable ETags; GET/list/write responses MUST expose the version required for
  the next mutation. Replace and PATCH MUST require and atomically evaluate
  `If-Match` in the reference profile; missing or stale preconditions MUST fail
  without mutation and MUST NOT be reported as successful replacement.
- These contracts MUST satisfy SCIM journey 11 in
  `.ai/identity-platform/END_STATE.md`, the token and `If-Match` policy in
  `.ai/identity-platform/REFERENCE_PROFILE.md`, and deletion/redaction
  boundaries in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.
- RFC examples plus at least one independent SCIM client/conformance suite MUST
  prove every advertised ServiceProviderConfig, Schemas, ResourceTypes, User,
  Group, GET-list, POST-search, PATCH, delete and Bulk operation and every exact
  capability, limit, error, ETag, pagination, filter and sort claim.

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
