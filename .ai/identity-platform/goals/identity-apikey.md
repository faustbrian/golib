# Goal: pkg/identity/apikey

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/apikey`
- Canonical module: `pkg/identity/apikey`
- Canonical goal after scaffolding: `pkg/identity/apikey/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/apikey:v1`; owned operation IDs: `contract:operation:identity.apikey.create:v1`, `contract:operation:identity.apikey.delete:v1`, `contract:operation:identity.apikey.delete-expired:v1`, `contract:operation:identity.apikey.get:v1`, `contract:operation:identity.apikey.list:v1`, `contract:operation:identity.apikey.rotate:v1`, `contract:operation:identity.apikey.session-authenticate:v1`, `contract:operation:identity.apikey.update:v1`, `contract:operation:identity.apikey.verify:v1`
- Requires: `identity`, `organization`, `primitive/authorization-identity-contracts`
- Consumes existing primitives: `authentication`, `authorization`, `secret-envelope`, `audit`, `rate-limit`
- Unlocks after verification: `identity/apikey/postgres`, `identity/apikey/valkey`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/apikey` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/apikey` module that owns user-
and organization-owned API-key issuance, display-once secrets, digested lookup,
naming, scopes, expiry, rotation, revocation, and usage metadata. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns user- and organization-owned API-key issuance, display-once
secrets, digested lookup, naming, scopes, expiry, rotation, revocation, and
usage metadata. It does not own generic API-key request validation, OAuth
clients, service-to-service PKI, and UI. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Service, KeyID, OwnerKind, Owner, SecretGenerator,
Record, ScopePolicy, Store, AtomicVerifier, VerificationAttemptID,
VerificationAttemptState, AtomicVerificationRequest,
AtomicVerificationResult, VerificationReconcileQuery,
ErrDebitOutcomeUnknown, IssuanceResult, AuthenticatorAdapter, Rotation,
Revocation, UsageObserver, PermissionReductionDisposition,
PermissionReductionDecision, PermissionReductionReconcileQuery, and
PermissionReductionResult contracts.
`PermissionReductionDisposition` MUST be the closed enum
`PermissionReductionUnspecified`, `PermissionReductionNarrow`, and
`PermissionReductionRevoke`; Unspecified is invalid. `VerificationAttemptID` MUST be a
validated non-zero `identity.CommandID`. `VerificationAttemptState` MUST be the
closed enum `VerificationAttemptNotApplicable`,
`VerificationAttemptNotDebited`, `VerificationAttemptDebited`, and
`VerificationAttemptPossibleDebit`. `AtomicVerifier` MUST expose exactly
`VerifyAndConsume(context.Context, AtomicVerificationRequest) (AtomicVerificationResult, error)`
and `ReconcileVerifyAndConsume(context.Context, VerificationReconcileQuery) (AtomicVerificationResult, error)`;
no method may imply that reconciliation performs the attempt again.
`OwnerKind` MUST be the closed enum
`OwnerKindUnspecified`, `OwnerUser`, and `OwnerOrganization`. `Owner` MUST bind
tenant, kind, exactly one typed user or organization ID, and the positive
authoritative owner version; its fields MUST be constructed and validated as
one tagged value. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST issue unbiased secrets once; store only digest and prefix metadata; authenticate in constant-work shape; scope principals; race rotate and revoke; expire by authoritative clock; list without secrets; update last-used with bounded write amplification; prevent prefix collisions and tenant leakage. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Operations MUST include create, verify, get metadata, list with stable
  pagination, update name/metadata/permissions, rotate, revoke/delete, and
  bounded deletion of expired keys. Secrets MUST be returned only at creation
  or rotation.
- Keys MUST support explicit user or organization ownership, tenant binding,
  named configurations, permissions/scopes and explicit authorization for
  every management operation. An unspecified owner kind, both IDs, neither ID,
  the wrong ID arm, a zero owner version, or cross-tenant owner binding MUST be
  rejected before lookup or mutation. User-owned management MUST authorize the
  exact current user or an explicit administrator permission;
  organization-owned management MUST authorize the exact current organization
  membership and required organization permission at the owner version.
  Ownership kind and ID changes are forbidden; rotation preserves the owner
  and creates a traceable successor.
- Management and verification MUST compute effective current authority from
  the key record plus the authoritative current owner, organization membership,
  tenant status, policy, permission ceiling and revocation state. A permission
  snapshot embedded in a key MUST only narrow current authority; it MUST NOT
  preserve access removed after issuance, resurrect a disabled owner or grant
  permissions newly added to a role beyond the immutable key grant. Dynamic
  inheritance MAY only narrow that grant. The authority version and decision
  inputs MUST be observable in a redacted typed result suitable for cache
  invalidation and audit.
- Multiple configuration profiles MUST define prefix, length, entropy,
  expiration, storage, rate/quota and verification policies. The configuration
  ID MUST be stored and used for verification rather than inferred from
  attacker-controlled key text.
- Verification MUST resolve the stored configuration ID against an effective
  configuration revision with explicit active, verify-only, deprecated and
  disabled states. Issuance MUST use the current configured revision;
  verification of older revisions MAY continue only under a bounded migration
  policy. Missing or disabled revisions, weakened digest parameters and
  incompatible policy changes MUST fail closed rather than falling back to a
  similarly prefixed profile.
- Database, secondary-storage with explicit fallback, and custom store
  profiles MUST declare consistency, revocation propagation and unknown-outcome
  behavior. Fallback MUST NOT resurrect revoked or expired keys.
- Fixed-window or token-bucket limits, remaining allowance, refill interval,
  refill amount and expiry MUST have atomic semantics under concurrency.
  Verification MUST return bounded safe quota metadata where configured.
- `AtomicVerifier.VerifyAndConsume` MUST bind one non-zero
  `VerificationAttemptID` to the canonical tenant, configuration, candidate,
  presented-digest commitment, required-permission set and debit amount. Its
  closed outcome MUST distinguish `not-applicable`, `not-debited`, `debited`,
  and `possible-debit`. A successful unlimited/no-quota profile is valid only
  with `not-applicable`; quota-enforced success is valid only with `debited`; an
  ordinary denial or pre-submission failure is valid only with `not-debited`;
  and a transport or commit ambiguity after submission of a positive debit MUST
  return `possible-debit` with `ErrDebitOutcomeUnknown` carrying the same
  attempt ID. The result MUST carry that attempt ID, state and an optional
  `VerificationResult`, which is present only for known successful
  `not-applicable` or `debited` outcomes. It MUST NOT collapse a possible debit
  into unavailable, invalid credential, success or a safe retry.
- A matching replay of `VerifyAndConsume` MUST return the recorded terminal
  result without comparing the secret or debiting again; reuse of an attempt ID
  with a different canonical fingerprint MUST conflict before verification or
  quota mutation. `AtomicVerifier.ReconcileVerifyAndConsume` MUST query the
  exact attempt without performing verification, refill or debit and return
  `not-applicable`, `not-debited`, `debited`, or still `possible-debit`.
  `VerificationReconcileQuery` MUST bind the attempt ID, tenant and canonical
  request fingerprint without carrying the raw key or presented digest. Callers
  MUST retain the request-scoped principal/result only for a known successful
  `not-applicable` or `debited` outcome and MUST never invoke another authority,
  fallback, or new attempt to guess an unknown outcome. Attempt records and
  reconciliation retention MUST cover the maximum client retry/idempotency
  horizon.
- Custom generators/verifiers MUST meet minimum entropy, redaction and
  constant-work contracts; unsafe configurations MUST fail construction.
- Metadata MUST be schema-validated, size/depth bounded, authorization-filtered
  on read/write and excluded from unbounded telemetry labels.
- API-key-derived request authentication is selected only by the exact
  `api_key.session_authentication.enabled` and
  `api_key.session_authentication.header` configuration. When disabled, the
  configured header has no authentication meaning. When enabled, exactly one
  case-insensitive header occurrence is accepted; duplicate, multiple-name,
  cookie-fallback and ambiguous credentials fail before lookup.
- `identity.apikey.session-authenticate` MUST perform exactly one authoritative
  verification and quota debit per admitted request and return a request-scoped
  session-compatible principal only for a user-owned key. The result MUST bind
  tenant, owner, immutable key grant, current owner authority, permissions,
  expiry, key/owner/revocation versions and the verification decision so every
  downstream authorization check reuses it without a second debit. It MUST NOT
  create a durable session or browser cookie; organization-owned keys cannot
  impersonate a user; revocation, expiry, owner suspension or permission
  removal denies the next request and positive caches are version-bounded. A
  `possible-debit` outcome MUST return no principal and require reconciliation
  by the same attempt ID; HTTP or middleware retry MUST NOT debit again.
- User deletion MUST immediately deny verification and durably revoke every
  key owned by that user as the cascade's terminal local outcome. User ban or
  suspension MUST immediately deny verification while retaining the key in a
  distinct suspended-owner state that cannot be used or managed except for
  revocation; unban MAY restore only a still-unexpired, otherwise-unrevoked key
  after an authoritative owner-version recheck and MUST NOT restore a deleted
  user's key. A user authority-version or permission change MUST immediately
  remove authority no longer present and MUST NOT expand the immutable key
  grant; policy MUST choose either narrowing the effective grant or durably
  revoking the key, and store that configured outcome in the cascade result.
  Organization archive or deletion MUST immediately deny verification and
  durably revoke every organization-owned key as the cascade's terminal local
  outcome. A membership or role change MUST NOT revoke an organization-owned
  key merely because one human member lost access; it MUST invalidate cached
  management authority for that actor and deny that actor's next management
  operation. Only an organization authority-version or permission-ceiling
  change that reduces the key's effective grant MUST immediately deny the
  removed permissions; policy MUST choose either narrowing the immutable key
  grant or durably revoking the key, and that configured choice MUST be stored
  in the attributable cascade result. These transitions MUST be idempotent,
  version-fenced, non-enumerating, and observable in audit/reconciliation
  results; disabling one owner MUST NOT affect another owner's keys.
- Every `Configuration` MUST contain one immutable
  `PermissionReductionDisposition`. The reference configuration MUST select
  `PermissionReductionNarrow`; a deployment MAY select
  `PermissionReductionRevoke` only as an explicit configuration revision.
  `ScopePolicy.DecidePermissionReduction` MUST bind the key ID, exact owner and
  positive owner-authority version, expected key version, prior immutable
  grant, effective current grant, disposition and positive policy version.
  Narrow MUST persist a bytewise sorted duplicate-free strict subset as the
  new immutable grant, advance the key version and API-key authority dimension,
  and emit `api_key.permissions_changed.v1`. Revoke MUST persist deny-all,
  transition the key to revoked, advance the same versions, and emit
  `api_key.revoked.v1`. `Store.ApplyPermissionReduction` MUST version-fence and
  idempotently persist the decision and attributable `PermissionReductionResult`;
  a stale version conflicts before mutation and an ambiguous commit remains
  unknown and reconcilable by the original command. `Store` MUST expose the
  exact methods
  `ApplyPermissionReduction(context.Context, PermissionReductionDecision) (PermissionReductionResult, error)`
  and
  `ReconcilePermissionReduction(context.Context, PermissionReductionReconcileQuery) (PermissionReductionResult, error)`.
  The reconciliation query MUST contain the exact original `CommandID` and
  canonical fingerprint. A result MUST reproduce the decision and outcome,
  but its authoritative post-transition `Record` MUST be present only when a
  committed outcome is proven and MUST be absent for not-committed or unknown
  outcomes. Verification MUST deny
  removed permissions before persistence completes and MUST NOT use an old
  positive cache to bridge the cascade.
- Reveal-once results MUST distinguish committed-and-revealed,
  committed-but-delivery-unknown, not-committed and commit-unknown outcomes.
  Plaintext MUST NOT be durably recoverable or returned by get/list after the
  issuance call completes. Idempotency MUST NOT mint multiple usable keys for
  one request. When reveal delivery is lost or commit is unknown, recovery MUST
  use a typed status inquiry followed by explicit revoke-and-reissue or
  rotation; it MUST NOT re-reveal, reconstruct or log the original secret.

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

Operation authority MUST match [`API_OPERATIONS.md`](../API_OPERATIONS.md),
issuance/rotation/reveal ambiguity MUST match
[`TRANSACTION_CONTRACT.md`](../TRANSACTION_CONTRACT.md), and revocation,
owner disablement and derived-session invalidation MUST match
[`LIFECYCLE_CASCADES.md`](../LIFECYCLE_CASCADES.md). Configuration revisions,
entropy, lifetimes, quotas and migration states MUST be represented by
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
Creation, update, rotation, revocation and authentication denial MUST emit the
bounded records defined by [`SECURITY_EVENTS.md`](../SECURITY_EVENTS.md).

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
