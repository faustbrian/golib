# Goal: pkg/identity

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity`
- Canonical module: `pkg/identity`
- Canonical goal after scaffolding: `pkg/identity/.ai/GOAL.md`
- Requires: None; this root execution unit may be claimed when its existing primitive audit is current.
- Consumes existing primitives: `authentication`, `authorization`, `identifier`, `tenancy`, `audit`
- Unlocks after verification: `identity/postgres`, `identity/session`, `identity/risk`, `identity/password`, `identity/username`, `identity/email`, `identity/magiclink`, `identity/otp`, `identity/phone`, `identity/anonymous`, `identity/mfa`, `passkey`, `identity/oauth`, `identity/apikey`, `identity/impersonation`, `organization`, `sso`, `scim`, `scim/organization`, `oauth-server`, `identity/i18n`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity` module that owns users, accounts, login identifiers, credential references, verification state, account status and domain events. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns users, accounts, login identifiers, credential references, verification state, account status and domain events. It does not own sessions, protocol ceremonies, delivery, provider APIs, organization membership, and administrative UI. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define User, Account, Identifier, CredentialRef, Verification, StatusPolicy, AttributeSchema, AttributeValue, FieldPolicy, Repository, UnitOfWork, Hook, and Event contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST create and retrieve users; attach, canonicalize, verify, make primary, and remove identifiers; link and unlink accounts without orphaning access; suspend, restore, anonymize, and delete according to policy; reject duplicate and cross-tenant identities under races. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

The public identity contract MUST own the global-compromise authority
transition, lifecycle event, cascade generation, and semantic acknowledgement.
Composition packages may invoke that operation and selected storage adapters
may persist it, but neither becomes a parallel semantic owner.

## Package-specific acceptance checklist

- User lifecycle MUST include create, get, list/search with stable cursor,
  update, suspend/ban with reason and optional expiry, restore, anonymize and
  delete. Status checks MUST be enforced by downstream authentication flows.
- Account lifecycle MUST list, link and unlink credential/provider accounts,
  expose only safe metadata, prevent orphaning the final usable access method,
  and distinguish implicit, explicit and administrator-authorized linking.
- Identifier lifecycle MUST cover email, phone, username and provider subject;
  canonical value, display value, verification, primary selection, uniqueness,
  replacement, cooldown/reuse and deletion MUST be separate decisions.
- Typed additional fields MUST declare data type, cardinality, default,
  validation, normalization, input permission, output permission, write
  authorization, sensitivity, indexing and migration version. Unknown fields,
  incompatible versions and unauthorized writes MUST fail deterministically.
- Hooks MUST have ordered before/after phases, cancellation, bounded execution,
  reentrancy policy and explicit transaction visibility. An observer failure
  MUST NOT silently roll back an already committed identity mutation.
- Repository queries MUST define tenant scope, status filters, pagination,
  ordering, consistency and redaction. Search MUST be bounded and MUST NOT
  become an enumeration bypass.
- Events MUST carry stable IDs, aggregate version, actor, tenant, causation and
  correlation without embedding secrets or unbounded attribute payloads.
- Administrator create/update/delete and credential-reset consumers MUST be
  supported through explicit authorization inputs; this module MUST NOT infer
  administrator status from an authenticated principal.
- Self-service profile update, privacy export and destructive lifecycle
  commands MUST have distinct authorization, field-redaction, stable snapshot,
  retention/legal-hold and idempotency contracts, exposed through the operation
  shapes in `.ai/identity-platform/API_OPERATIONS.md`. A delete result MUST
  become `completed` only after the owned identity state and every required
  cascade checkpoint in `.ai/identity-platform/LIFECYCLE_CASCADES.md` is
  confirmed; accepted or queued work MUST remain `pending`, and blocked or
  outcome-unknown work MUST remain distinguishable.
- Self-service deletion MUST consume the closed proof policy from
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`: current password plus a
  fresh session, fresh UV passkey, or a purpose/subject/session/version-bound
  emailed capability according to account type. Proof verification, cascade
  initiation, session revocation and terminal result MUST use the transaction
  and unknown-outcome contracts; an old session or caller assertion that it is
  fresh is insufficient.
- Provider links are identity-owned account records keyed by tenant, provider,
  issuer and provider subject. Provider modules may prove a subject and retain
  provider credentials, but MUST NOT create an independent link authority;
  link, relink and unlink MUST enforce explicit collision and final-access
  policy through the identity command.
- Identity status and identifier changes MUST emit the identity-owned authority
  versions/events, while credential and factor owners emit their corresponding
  dimensions and session modules own token discovery and revocation execution.
  The authority, acknowledgement and fail-closed boundaries MUST follow
  `.ai/identity-platform/LIFECYCLE_CASCADES.md`; their audit outcomes MUST use
  `.ai/identity-platform/SECURITY_EVENTS.md`.
- `UnitOfWork` MUST expose the participant semantics required by
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`; it MUST NOT imply that an
  arbitrary caller transaction, provider callback or post-commit hook is
  atomically enrolled without the documented coordinator contract.

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
