# Goal: pkg/identity/session

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/session`
- Canonical module: `pkg/identity/session`
- Canonical goal after scaffolding: `pkg/identity/session/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/session:v1`; owned operation IDs: `contract:operation:identity.admin.session-list:v1`, `contract:operation:identity.admin.session-revoke:v1`, `contract:operation:identity.admin.session-revoke-all:v1`, `contract:operation:identity.session.bearer-authorize:v1`, `contract:operation:identity.session.bearer-issue:v1`, `contract:operation:identity.session.get:v1`, `contract:operation:identity.session.last-method-check:v1`, `contract:operation:identity.session.last-method-clear:v1`, `contract:operation:identity.session.last-method-get:v1`, `contract:operation:identity.session.last-method-record:v1`, `contract:operation:identity.session.list:v1`, `contract:operation:identity.session.refresh:v1`, `contract:operation:identity.session.revoke-all:v1`, `contract:operation:identity.session.revoke-one:v1`, `contract:operation:identity.session.revoke-other:v1`, `contract:operation:identity.session.select-active:v1`, `contract:operation:identity.session.signout:v1`, `contract:operation:identity.session.transfer-consume:v1`, `contract:operation:identity.session.transfer-generate:v1`, `contract:operation:identity.session.update:v1`
- Requires: `identity`, `primitive/authorization-identity-contracts`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `authentication`, `authorization`, `identifier`, `capability`, `capability/postgres`, `audit`, `secret-envelope`
- Unlocks after verification: `identity/session/postgres`, `identity/session/valkey`, `identity/password`, `identity/magiclink`, `identity/otp`, `identity/anonymous`, `identity/mfa`, `passkey`, `identity/oauth`, `identity/impersonation`, `organization`, `sso`, `oauth-server`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/session` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/session` module that owns session issuance, opaque cookies, persistence interfaces, freshness, rotation, revocation, devices, multi-account client sessions, last-login-method state, and administrative session control. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns session issuance, opaque cookies, persistence interfaces, freshness, rotation, revocation, devices, multiple account sessions in one client, active-session selection, maximum-session enforcement, last-login-method state, and administrative session control. It does not own credential verification, user repositories, UI, and OAuth authorization-server grants. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Session, Token, SessionIssuer, RememberPolicy, Store, CookiePolicy,
FreshnessPolicy, StorageProfile, StatelessCodec, SessionVersion, CookieCache,
SessionEnricher, EnrichmentSchema, Device, RotationResult, PrincipalResolver,
Revoker, SessionSet, ActiveSessionSelector, MaximumSessionsPolicy,
ActiveOrganizationSelection, OrganizationSelector, LastLoginMethodStore, and
SessionTransfer contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

`RememberPolicy` MUST be the single shared session-lifetime choice used by
every session-issuing flow, including password, username, magic-link, phone,
OTP, anonymous, passkey, OAuth, enterprise SSO and MFA. It MUST represent
at least an explicit persistent-session choice and an explicit browser-session
choice; an omitted choice MUST resolve through one documented configuration
default before the first authentication continuation is issued. `SessionIssuer`
MUST accept the resolved `RememberPolicy` with the final authenticated proof
and MUST return the session plus transport metadata needed to apply the
matching cookie policy. Downstream signin packages MUST use these public
contracts rather than define package-local remember flags or session-issuance
interfaces.

`ActiveOrganizationSelection` and `OrganizationSelector` MUST store only the
opaque selected organization ID, tenant and authority/version bindings on the
exact session/browser-container state. They MUST support set/get/clear and
version invalidation without importing `organization` or treating selection as
authorization; `organization` owns membership/lifecycle validation on every
switch and use.

`SessionIssuer` MUST support a caller-supplied command identity and a
transaction-aware issuance operation so a continuation owner can finalize its
one-time proof and persist the resulting session in one authoritative commit.
It MUST return `NotCommitted`, `Committed`, `Unknown`, `Conflict`, or
`InProgress` consistently with `TRANSACTION_CONTRACT.md`; after `Unknown`, the
caller MUST be able to recover the safe issuance result by command identity
without minting a second session or changing `RememberPolicy`.

The public contract MUST define typed bearer-authorization input/result and
bearer-issuance continuation/result values matching
`struct:ref.session.bearer_issuance`. Authorization MUST bind tenant, subject,
source session/family and versions, authorization decision/version, audience,
origin, lifetime, transport and command ID into a 60-second one-use
continuation without returning a bearer. Issuance MUST reserve, apply and
finalize that continuation with bearer creation in one authoritative command;
invalid, denied, expired, unknown or replayed outcomes return no credential.

## Required behavior

The implementation and tests MUST issue only after authenticated proof; rotate without reuse windows; reject fixation and replay; enforce absolute and idle expiry; revoke one or all sessions; list and label devices; preserve public logout idempotency; hold multiple account sessions in one browser without cross-account cookie confusion; atomically select and switch the active account; enforce a configured maximum with deterministic eviction or denial; and preserve list/revoke semantics across active and inactive sessions. It MUST track, get, check and clear the last successful login method with explicit database, signed-cookie, cross-domain, consent, retention and deletion behavior. Failed attempts MUST NOT replace the last successful method, and privacy-disabled tracking MUST leave no residual state. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Stateful opaque, secondary-storage, bounded cookie-cache and fully stateless
  profiles MUST be explicit configurations with identical principal, expiry,
  freshness, update, list/revocation capability declarations and documented
  feature differences. The implementation MUST NOT silently emulate a missing
  revocation guarantee.
- Stateless sessions MUST be authenticated and, when containing confidential
  fields, encrypted; bind issuer, audience, tenant, subject, session family and
  version; support signing/encryption key rotation; and define global/user
  version invalidation, compromise response and maximum cookie size.
- Cookie cache MUST define signed versus encrypted content, maximum age,
  refresh strategy, authoritative-store fallback, stale-data bound, version
  invalidation and behavior when the backing store is unavailable.
- Session operations MUST include get, list, update allowed metadata, revoke
  one, revoke other, revoke all, rotate, refresh/defer refresh, require
  freshness, and optional revoke-on-password-change behavior.
- Every password, username, phone or OTP signin that can require MFA MUST place
  the already-resolved `RememberPolicy` in the partial-auth continuation and
  pass that unchanged to `SessionIssuer` after successful completion. MFA and
  other continuation stages MUST NOT apply a new default, upgrade a browser
  session to a persistent session, extend the selected lifetime, or issue a
  full session before the continuation's authentication policy is complete.
  Direct signins and continued signins MUST exercise the same `SessionIssuer`
  contract and produce equivalent persistence semantics for the same policy.
- A bearer transport profile MUST authenticate the same opaque session token
  from an `Authorization` scheme without cookie fallback, preserve all expiry,
  freshness, version and revocation checks, and expose transport metadata so
  HTTP can apply the correct CSRF/cache policy. It MUST NOT turn a session into
  a generally reusable OAuth access token.
- Multi-account session sets MUST separate browser container identity from
  account sessions, prevent active-index substitution, define eviction at the
  configured maximum and preserve independent logout/revocation semantics.
- Last-login method MUST record only successful configured methods, support
  custom provider IDs, default resolution, cookie and database profiles,
  cross-domain policy, opt-out and deletion, and MUST NOT become authentication
  or account-enumeration evidence.
- Session transfer MUST require a fresh authenticated non-impersonated source
  session; issue a capability bound to session ID/version, tenant, exact target
  audience/origin and expiry; store no raw token; and return the same bounded
  session rather than minting a stronger independent family. Validation MUST
  be read-only. The target transition MUST reserve the capability, recheck
  source expiry/revocation/freshness, apply the transfer and finalize the
  capability in the same shared unit of work, with recovery by command ID and
  capability digest as specified by `TRANSACTION_CONTRACT.md`. Cookie issuance
  versus response-only use MUST be an explicit target policy.
- Session transfer MUST use `capability` for signed payload framing, issuer,
  audience, resource, action, expiry, key rotation, revocation and `MaxUses=1`
  reservation/finalization. Standalone consumption before or after the session
  transition is forbidden. This module MUST own only the session-specific
  authorization and state transition; it MUST NOT define a competing replay,
  signature or capability-consumption store.
- Session enrichment MUST declare output schema and source fields, run outside
  store locks, honor cancellation/deadline, bound latency/cardinality, define
  error fallback, and invalidate cache when any complete input changes.
  Enriched fields MUST NOT alter the authenticated principal or authorization
  facts unless a separate authorization decision validates them.
- Device labels, IP-derived metadata and user-agent data MUST be bounded,
  privacy-classified, spoof-aware and non-authoritative. Trusted-device state
  remains owned by MFA.

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
