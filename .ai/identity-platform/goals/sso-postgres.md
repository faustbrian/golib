# Goal: pkg/sso/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `sso/postgres`
- Canonical module: `pkg/sso/postgres`
- Canonical goal after scaffolding: `pkg/sso/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:sso/postgres:v1`; owned operation IDs: none
- Requires: `sso`, `identity/postgres`, `organization/postgres`, `primitive/capability-identity-contracts`, `primitive/capability-postgres-identity-contracts`
- Consumes existing primitives: `postgres`, `migrations`, `capability/postgres`, `secret-envelope`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `sso/postgres` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/sso/postgres` module that owns durable SSO providers, domains, mappings, encrypted configuration, login transactions, enforcement state, and audit linkage. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns durable SSO providers, domains, mappings, encrypted
configuration, OIDC/OAuth token-vault state, SAML request/assertion replay
state, login transactions, enforcement state, and audit linkage. It MUST
implement only the protocol-neutral `sso.Repository`,
`sso.EnterpriseTokenVault`, `sso.ProtocolStateStore` and
`sso.ProvisioningUnitOfWork` persistence contracts. It MUST NOT import
`sso/oidc`, `sso/oauth2` or `sso/saml`, implement protocol-package-owned
persistence interfaces, or make an optional protocol a persistence
prerequisite. It
does not own protocol parsing, SSO policy, and provider network calls. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define schema, configuration envelope context, provider
versioning, domain-proof references, domain uniqueness, login transaction
consumption, enterprise token-vault storage, mapping/sync persistence,
migrations, cleanup, and reconciliation contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

The adapter MUST publish an open versioned `identitypostgres.Contributor` for
composition-root registration. It maps the complete
`sso.ProvisioningCommand` through generation-bound `identitypostgres.Work` and
returns the exact `sso.ProvisioningResult`; it never begins, commits, rolls
back or retains a transaction. The constructor MUST NOT accept a coordinator,
Work, carrier, enlister or contributor registry. Protocol-neutral repository,
vault and replay/checkpoint contracts may share the same store, but no optional
wire-protocol module is imported below this boundary.
The public `Store` MUST implement the exact core `sso.Repository` method set:
`Provider(context.Context, sso.ProviderID) (sso.Provider, error)`,
`Route(context.Context, sso.DomainRouteRequest) (sso.DomainRoute, error)`,
`ConsumeLogin(context.Context, sso.ConsumeLoginRequest) (sso.LoginTransaction, sso.Completion, error)`, and
`RecoverLogin(context.Context, sso.CommandID) (sso.SigninCompletionResult, error)`.
It MUST NOT substitute adapter-local request, result, or completion types or
add methods to that implemented-interface declaration.

## Required behavior

The implementation and tests MUST race provider and domain changes; consume login transactions once; rotate encrypted configuration; preserve old/new binary compatibility; recover ambiguous commits; retain audit linkage; enforce organization isolation and indexed routing. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- The schema MUST persist provider ownership/configuration, protocol type,
  verified domains, encrypted client secrets/private keys, certificates and
  rollover, organization links, mappings/policy versions, login transactions,
  replay IDs, JIT links and reconciliation state.
- Recoverable credentials/keys MUST use `secret-envelope` with provider/
  tenant/organization/protocol context and rotation. Metadata/list operations
  MUST never return secret material.
- Provider enable/disable, credential rotation and enforcement update MUST be
  separate versioned commands. Their readiness checkpoints, overlap/drain
  policy, unknown outcomes and reconciliation records MUST commit atomically
  with the provider and organization authority versions and outbox events.
- OIDC logout state MUST bind provider, issuer, local session/version and the
  allowlisted post-logout target, be consumed exactly once by completion, and
  retain an unknown provider outcome only until authoritative reconciliation;
  local session revocation remains committed on every provider outcome.
- Enterprise provider tokens MUST be envelope-encrypted with tenant/
  organization/provider/subject/purpose/version context. The vault MUST
  serialize refresh per grant, atomically rotate token generations, detect
  stale/reused generations, retain only declared history and make
  revoke/delete outcomes reconcilable without exposing token material.
- Provider ID, issuer/entity ID, domain and organization-link uniqueness MUST
  have database-enforced scope and deterministic conflict/takeover behavior.
- Routes MUST reference the organization-owned domain proof identity/version
  rather than duplicate proof truth. Expiry, revocation or transfer MUST
  atomically disable or version-invalidate affected routes and new login
  transactions, including under ambiguous proof-update outcomes.
- Login state/relay state, authorization codes where stored, SAML request/
  assertion IDs and provisioning commands MUST be digest-indexed, expiring and
  atomically single-use.
- OAuth/OIDC RP transactions MUST retain a raw PKCE verifier only as
  envelope-AEAD ciphertext under `struct:ref.oauth.rp_transaction`; its keyed
  commitment alone is indexed. The exact AAD, transaction lifetime, reserved
  decrypt authority, terminal erasure, and ambiguous exchange recovery are
  mandatory and plaintext persistence is forbidden.
- JIT identity/membership/provider-link updates and outbox state MUST be atomic
  through the sole public `identity/postgres` coordinator and open versioned
  contributors; the adapter MUST NOT duplicate identity rows or coordinator
  SQL. `sso.ProvisioningUnitOfWork` maps the storage-neutral command to those
  contributors at composition. External or separately owned
  store ambiguity MUST enter reconciliation.
- Repeat-login sync MUST persist provider/profile and mapping-policy versions,
  last applied assertion/claim version, authoritative-field decisions and
  reconciliation state. Concurrent/replayed logins MUST converge without
  restoring removed roles or overwriting application-owned fields.
- Disable/delete/credential rotation/domain revocation MUST race safely with
  login and immediately prevent new transactions at a documented isolation
  point without corrupting in-flight evidence.
- Stable indexed pagination and production-shaped query plans are REQUIRED for
  providers, domains, organization links and reconciliation work.
- Migration/restore evidence MUST cover active sessions/transactions, secret
  and certificate rotation, populated mappings, mixed binaries, replay-state
  retention and interrupted JIT provisioning.

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
