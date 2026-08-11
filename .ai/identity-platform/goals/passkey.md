# Goal: pkg/passkey

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `passkey`
- Canonical module: `pkg/passkey`
- Canonical goal after scaffolding: `pkg/passkey/.ai/GOAL.md`
- Requires: `identity`, `identity/session`, `identity/risk`, `webauthn`
- Consumes existing primitives: `audit`, `identifier`
- Unlocks after verification: `passkey/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `passkey` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/passkey` module that owns identity-facing passkey enrollment, discoverable credentials, passwordless signup/signin, synced and backup state, naming, listing, deletion, and passkey-first policy. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns identity-facing passkey enrollment, discoverable credentials, passwordless signup/signin, synced and backup state, naming, listing, deletion, and passkey-first policy. It does not own WebAuthn wire verification, non-discoverable security-key policy, browser UI, and session persistence. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Service, RegistrationFlow, AuthenticationFlow, CredentialProfile, UserHandlePolicy, BackupState, DeviceLabel, SessionIssuer, Store, and AccountRecoveryPolicy contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST create stable opaque user handles; support discoverable authentication; bind WebAuthn results to identities; expose backup eligibility/state; enroll and delete with freshness; prevent deletion of the last allowed recovery path; handle synced credentials and counter policy without false takeover. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Flows MUST cover authenticated add, passkey-first pre-auth registration when
  enabled, identifier-based signin, discoverable/usernameless signin and
  conditional UI option generation without claiming browser UI ownership.
- Registration policy MUST bind opaque stable user handle, RP, authenticated or
  pre-auth subject, attestation/authenticator selection, resident-key and user-
  verification requirements and exclude existing credentials.
- Pre-auth registration MUST use a short-lived risk-assessed transaction and
  MUST NOT create a durable user/session until verified WebAuthn registration
  and identity creation commit coherently.
- Credential management MUST list safe metadata, add, rename from caller or
  authenticator hints, update labels and delete with recent authentication and
  last-recovery-method policy.
- Passkey policy MAY consume only extension outputs that `webauthn` returns in
  its cryptographically verified result under the selected typed profile.
  Unknown or unselected extension outputs are unavailable to this module and
  MUST NOT be retained, reconstructed, or used for identity, authorization,
  discoverability, naming, backup, or risk decisions.
- Discoverable assertion MUST resolve user handle and credential together and
  must not reveal whether either exists before cryptographic verification.
- Synced passkey backup eligibility/state and signature-counter behavior MUST
  follow an explicit profile that avoids false account takeover while still
  surfacing cloned non-sync authenticators.
- Official fixtures plus real browser/platform and roaming authenticator
  profiles MUST prove passkey-first, usernameless, conditional UI where claimed,
  rename/delete and recovery behavior.
- Passkey flows MUST consume a cryptographically verified, versioned WebAuthn
  result satisfying the exact RP/origin/UV/backup/counter rules in
  `.ai/identity-platform/PROTOCOL_BASELINES.md`. Passkey-first signup and step-
  up MUST require UV; conditional UI, discoverability or backup eligibility
  MUST NOT weaken tenant, RP, identity-status or risk checks.
- Tenant and RP selection MUST be trusted configuration established before
  ceremony creation. Opaque user handles and credential resolution MUST bind
  tenant, RP and identity together; neither browser-supplied RP fields nor a
  globally unique credential-ID assumption may cross that boundary.
- Pre-auth signup, authenticated enrollment, assertion/session issuance and
  deletion MUST use `.ai/identity-platform/TRANSACTION_CONTRACT.md` so ceremony
  consumption, WebAuthn credential state, identity/passkey mapping and session
  effects finalize together or have a recoverable outcome. Authentication MUST
  not be exposed before finalization. Recent-auth deletion MUST use an explicit
  reauthentication proof, not a boolean freshness assertion.
- Authentication and signup inputs MUST carry the session-owned persistent or
  non-persistent remember policy through every pre-auth/risk continuation into
  the SessionIssuer. No continuation, default or conditional-UI path may
  upgrade a non-persistent choice to a persistent credential or longer server
  lifetime.

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
