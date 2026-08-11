# Goal: pkg/sso

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `sso`
- Canonical module: `pkg/sso`
- Canonical goal after scaffolding: `pkg/sso/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/risk`, `organization`
- Consumes existing primitives: `authentication`, `authorization`, `capability`, `capability/postgres`, `secret-envelope`, `audit`, `workflow`
- Unlocks after verification: `sso/domain-verification`, `sso/oidc`, `sso/oauth2`, `sso/saml`, `sso/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `sso` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/sso` module that owns enterprise SSO provider registration, verified-domain routing, discovery policy, attribute mapping, JIT provisioning, organization membership provisioning, enforcement, and lifecycle. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns enterprise SSO provider registration, verified-domain routing, discovery policy, attribute mapping, JIT provisioning, organization membership provisioning, enforcement, and lifecycle. It does not own protocol wire handling, consumer social login, SCIM, generic identity UI, and persistence adapters. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Provider, Protocol, DomainRoute, DomainProof,
DiscoveryPolicy, AttributeMapping, JITPolicy, MembershipPolicy,
RepeatLoginPolicy, EnforcementPolicy, LoginTransaction, EnterpriseTokenVault,
SessionIssuer, Repository, and Hook contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST register and enable providers safely; verify domains before routing; resolve deterministic routing conflicts; map claims without privilege escalation; JIT provision atomically; enforce SSO without locking out recovery admins; disable and rotate providers; bind login transactions and organization scope. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Provider lifecycle MUST include register, get/list, update, enable/disable,
  rotate credentials/certificates, delete and organization link/unlink with
  explicit ownership and authorization. Provider IDs, domains and issuer/entity
  IDs MUST have collision rules.
- Routing MUST support explicit provider ID, verified-domain discovery,
  configured default and deterministic multiple-provider conflict. Unverified
  domains, arbitrary email suffixes and hostile discovery metadata MUST NOT
  select a provider.
- Domain routing and organization linking MUST consume the current uniquely
  owned `organization` DomainProof contract, including proof version and
  expiry/revocation state. SSO MUST NOT maintain an independent truth for
  domain ownership; proof expiry, revocation or transfer MUST invalidate routes
  and block new transactions at a documented boundary.
- Login transactions MUST bind protocol, provider, organization, tenant,
  redirect, state/relay state, initiator and expiry and MUST be atomically
  single-use across shared callback URLs.
  OAuth/OIDC state and SAML RelayState MUST use every `tx.capability.*` role
  selected for this goal; validation alone grants no authority, and durable
  reserve/apply/finalize/recover semantics require `capability/postgres`.
- JIT user provisioning MUST define required stable subject, verified email
  policy, collision/linking behavior, default attributes and rollback/unknown
  outcome. Organization provisioning MUST be idempotent and custom-role mapping
  MUST never create privilege outside configured statements.
- Repeat login for an existing provider subject MUST run a versioned sync
  policy for mapped identity attributes, membership and roles. It MUST define
  authoritative versus application-owned fields, missing/null claims,
  downgrades/removals, conflict handling and transactional/compensating
  behavior; stale or unknown claims MUST NOT preserve or create privilege by
  accident.
- SSO initiation MUST accept `identity/session`'s `RememberPolicy`; OIDC,
  OAuth2 and SAML state or relay state MUST bind it and callbacks MUST preserve
  it unchanged through MFA, JIT and synchronization until session issuance.
- Directory synchronization MUST expose start, bounded apply, status, cancel
  and reconciliation contracts for provider-sourced user, group and role
  deltas. Mapping versions, checkpoints, source-of-truth fields,
  deprovisioning, local-override conflicts and outcome-unknown recovery MUST be
  explicit; directory input MUST NOT bypass SCIM or organization authority.
- `EnterpriseTokenVault` MUST own provider access/refresh token storage,
  lookup, rotation, serialized refresh, revocation, retention and deletion.
  Recoverable tokens MUST use `secret-envelope` with tenant/organization/
  provider/subject/purpose/version context; metadata, errors, hooks and audit
  MUST remain token-free. Protocol adapters MUST pass tokens only through this
  contract and MUST NOT persist them independently.
- SSO enforcement MUST define enrollment, grace, bypass/recovery administrators,
  provider outage, disabled provider and break-glass audit behavior. It MUST NOT
  permanently lock all administrators out.
- Self-service provider administration MUST expose only organization-owned
  providers and safe metadata; secrets, certificates/private keys and raw IdP
  errors MUST remain redacted.
- Hooks MUST cover provider and provisioning lifecycle with explicit
  transaction/compensation behavior and MUST NOT reinterpret protocol proof.
- The composed behavior MUST satisfy enterprise-federation journey 10 in
  `.ai/identity-platform/END_STATE.md` and the domain routing, deny-by-default
  JIT mapping and token handling rules in
  `.ai/identity-platform/REFERENCE_PROFILE.md`.

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
