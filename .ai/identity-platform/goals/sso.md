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
- Public contracts: unit ID `contract:unit:sso:v1`; owned operation IDs: `contract:operation:identity.sso.break-glass.consume:v1`, `contract:operation:identity.sso.break-glass.issue:v1`, `contract:operation:identity.sso.directory-sync-apply:v1`, `contract:operation:identity.sso.directory-sync-cancel:v1`, `contract:operation:identity.sso.directory-sync-start:v1`, `contract:operation:identity.sso.directory-sync-status:v1`, `contract:operation:identity.sso.discover:v1`, `contract:operation:identity.sso.domain-challenge:v1`, `contract:operation:identity.sso.domain-verify:v1`, `contract:operation:identity.sso.enforcement.update:v1`, `contract:operation:identity.sso.provider.credentials-rotate:v1`, `contract:operation:identity.sso.provider.delete:v1`, `contract:operation:identity.sso.provider.disable:v1`, `contract:operation:identity.sso.provider.enable:v1`, `contract:operation:identity.sso.provider.get:v1`, `contract:operation:identity.sso.provider.list:v1`, `contract:operation:identity.sso.provider.register-oauth:v1`, `contract:operation:identity.sso.provider.register-oidc:v1`, `contract:operation:identity.sso.provider.register-saml:v1`, `contract:operation:identity.sso.provider.update:v1`, `contract:operation:identity.sso.signin-start:v1`
- Requires: `identity`, `identity/session`, `identity/risk`, `organization`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `authentication`, `authorization`, `capability`, `secret-envelope`, `audit`, `workflow`
- Unlocks after verification: `sso/domain-verification`, `sso/oidc`, `sso/oauth2`, `sso/saml`, `sso/postgres`, `scim/organization`, `identity/http`

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

The design MUST define Provider, Protocol, DomainRoute,
DiscoveryPolicy, AttributeMapping, JITPolicy, MembershipPolicy,
RepeatLoginPolicy, EnforcementPolicy, LoginTransaction, EnterpriseTokenVault,
ProvisioningUnitOfWork, ProvisioningCommand, ProvisioningResult,
ProtocolAdapter, ProtocolAssertion, ProtocolStateStore,
ProtocolReplayID, ProtocolMappingCheckpoint, DirectorySyncContributor,
DirectoryDeltaBatch, DirectoryApplyResult, Repository, and Hook contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.
`ProtocolAdapter` MUST expose exactly
`Start(context.Context, ProtocolStartCommand) (ProtocolStartResult, error)` and
`Complete(context.Context, ProtocolCallback) (ProtocolAssertion, error)`.
SSO MUST own every provider-registration request, result and protocol-neutral
registration value, including `SAMLMetadataDocument`, `SAMLConfiguration` and
`OIDCStaticMetadata`. The `identity.sso.provider.register-oauth`,
`identity.sso.provider.register-oidc` and
`identity.sso.provider.register-saml` contracts MUST NOT name their protocol
adapter packages as collaborators or use adapter-owned request/result types;
adapters validate or render wire material only after SSO selects the provider
snapshot.
Protocol packages MUST translate only validated protocol evidence into
`ProtocolAssertion`; SSO alone applies routing, JIT, membership, role,
token-vault, and session policy. `DirectorySyncContributor` MUST expose exactly
`ApplyDirectoryDelta(context.Context, DirectoryDeltaBatch) (DirectoryApplyResult, error)`.
The batch and result MUST bind provider, organization, generation, mapping
version, stable child command IDs, predecessor checkpoint, per-child outcome,
and unknown/reconciliation state. `scim/organization` implements that
consumer-owned interface; SSO remains sole owner of sync generations and cursors.

`ProvisioningUnitOfWork` is the sole callable boundary for the atomic JIT or
repeat-login transition. `Execute` MUST accept one complete
`ProvisioningCommand` binding the globally unique command identity, validated
protocol assertion, JIT/repeat-login/membership policy versions and resolved
`identity/session.RememberPolicy`; it MUST atomically apply identity linkage,
organization membership and role changes, enterprise-token linkage, session
issuance, required audit and outbox effects. `Recover` MUST return the exact
same `ProvisioningResult` by command identity without repeating protocol
exchange or any child effect. Unknown returns no session.

Core `sso` MUST remain storage-neutral. No public core constructor, method,
command, result, callback or field may mention `identitypostgres.Work`,
`identitypostgres.Contributor`, `identitypostgres.Coordinator`, `pgx.Tx`, a
carrier or an enlister. PostgreSQL adapters expose open versioned contributors
and map the consumer-owned provisioning command at the composition boundary;
only the `identity/postgres` coordinator owns transaction lifetime.

`ProtocolStateStore` is the protocol-neutral persistence boundary consumed by
every protocol adapter. It owns digest-indexed replay consumption and validated
mapping checkpoints using only SSO-owned types. `sso/oidc`, `sso/oauth2` and
`sso/saml` MUST NOT publish persistence interfaces or require their
implementation package to be imported by `sso/postgres`.

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
- Provider delete MUST atomically establish local provider denial and return
  the exact `lifecycle.cascade.enterprise_provider_delete` ID, generation,
  status and redacted limitations. `deleted` is impossible until every
  transaction/routing/token-vault, session/cache, organization/domain, SCIM,
  authorization-cache and audit consumer closes; pending, outcome-unknown or
  limited provider cleanup MUST remain visible and cannot restore authority.
- The callable lifecycle MUST expose `identity.sso.provider.enable`,
  `identity.sso.provider.disable`, `identity.sso.provider.credentials-rotate`
  and `identity.sso.enforcement.update` as distinct operations with the exact
  access, CSRF, rate, idempotency and outcome contracts in
  `API_OPERATIONS.md`; generic provider update/delete MUST NOT substitute for
  those transitions.
- Generic `identity.sso.provider.update` MUST be limited to non-secret provider
  metadata, display, discovery, mapping, JIT, membership and synchronization
  policy fields. It MUST reject credentials, client secrets, signing or
  decryption keys, certificates, secret handles and key/certificate activation
  or retirement. Credential and certificate/key rotation MUST occur only
  through `identity.sso.provider.credentials-rotate` with its separate
  authorization, reveal/redaction, overlap, audit and recovery contract.
- Routing MUST support explicit provider ID, verified-domain discovery,
  configured default and deterministic multiple-provider conflict. Unverified
  domains, arbitrary email suffixes and hostile discovery metadata MUST NOT
  select a provider.
- Domain routing and organization linking MUST consume the current uniquely
  owned `organization.DomainProof` contract through
  `organization.DomainProofReader`, including proof version and
  expiry/revocation state. SSO MUST NOT maintain an independent truth for
  domain ownership; proof expiry, revocation or transfer MUST invalidate routes
  and block new transactions at a documented boundary.
- `sso` owns the callable `identity.sso.domain-challenge` and
  `identity.sso.domain-verify` orchestration and their HTTP/OpenAPI contracts.
  It MUST translate its challenge into the verifier's exact
  `DomainProofRequest`, invoke `DomainProofEngine.Observe`, receive only
  `DomainProofObservation`, and commit that
  evidence through `organization.DomainEvidenceTransition`; subsequent routing
  MUST retrieve only `organization.DomainProof` through
  `organization.DomainProofReader`. It MUST delegate bounded proof retrieval
  and classification to `sso/domain-verification` and commit claim state only
  through `organization`;
  neither collaborator may publish a competing operation definition.
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
  `sso` is the sole semantic owner of sync generations, provider cursors,
  checkpoints, cancellation, reconciliation and canonical directory-sync
  events. Apply MUST enlist the injected `DirectorySyncContributor`
  before the first write; unknown child outcomes block cursor advancement until
  recovered.
- `EnterpriseTokenVault` MUST own provider access/refresh token storage,
  lookup, rotation, serialized refresh, revocation, retention and deletion.
  Recoverable tokens MUST use `secret-envelope` with tenant/organization/
  provider/subject/purpose/version context; metadata, errors, hooks and audit
  MUST remain token-free. Protocol adapters MUST pass tokens only through this
  contract and MUST NOT persist them independently.
- SSO enforcement MUST define enrollment, grace, bypass/recovery administrators,
  provider outage, disabled provider and separate break-glass issuance/use audit
  behavior. `identity.sso.break-glass.issue` MUST only issue and audit the
  reveal-once capability; `identity.sso.break-glass.consume` MUST atomically
  consume it, establish bounded recovery authority, and emit the distinct use
  event. It MUST NOT
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

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
