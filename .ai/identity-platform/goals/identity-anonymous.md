# Goal: pkg/identity/anonymous

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/anonymous`
- Canonical module: `pkg/identity/anonymous`
- Canonical goal after scaffolding: `pkg/identity/anonymous/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/anonymous:v1`; owned operation IDs: `contract:operation:identity.anonymous.delete:v1`, `contract:operation:identity.anonymous.signin:v1`, `contract:operation:identity.anonymous.upgrade:v1`
- Requires: `identity`, `identity/session`, `identity/risk`, `primitive/authentication-identity-contracts`
- Consumes existing primitives: `audit`, `identifier`
- Unlocks after verification: `identity/anonymous/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/anonymous` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/anonymous` module that owns anonymous identities and atomic upgrade into durable registered identities. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns anonymous identities and atomic upgrade into durable registered identities. It does not own browser fingerprinting, advertising identity, guest UI, and general session storage. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Issuer, AnonymousSubject, UpgradePolicy, MergePlan,
Conflict, SessionTransition, Store, UnitOfWork, UpgradeRecord, CleanupPolicy,
and expiry contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST issue bounded anonymous subjects; isolate tenants; upgrade exactly once; merge allowed data without credential takeover; handle existing-account conflicts explicitly; expire and anonymize abandoned identities. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Anonymous creation MUST generate a non-addressable opaque identity with
  configured display-name and placeholder-email policy from
  `anonymous.display_name`, `anonymous.placeholder_email_domain` and
  `anonymous.ttl`, minimal privileges,
  bounded expiry and an explicit anonymous principal/session marker. Creation
  and upgrade MUST obtain an action-specific `identity/risk` decision before
  committing identity or session state.
- Placeholder identifiers MUST be reserved to the anonymous namespace and
  MUST never be delivered to or treated as verified real contact information.
- Link/upgrade MUST require verified target credentials, consume the anonymous
  transition once, preserve the permanent account as authority and apply a
  typed merge plan for every transferable field/resource.
- Upgrade MUST use an injected public capability participant for its
  purpose-bound one-use anonymous transition. Validate is read-only;
  Reserve/Apply/Finalize run with the owning upgrade command, and Recover
  resolves an unknown outcome. The core MUST NOT open a private capability
  store or import a concrete capability persistence adapter.
- A successful upgrade MUST atomically revoke the initiating anonymous session
  and rotate to one new permanent session bound to the target identity and the
  caller's unchanged remember policy. The new session MUST NOT become usable
  before the upgrade commits; the anonymous session MUST NOT remain usable
  after commit; an unknown outcome MUST reconcile both session records and the
  upgrade command before retry.
- Conflict policy MUST cover an already signed-in permanent user, identifier or
  provider-account collision, concurrent upgrades, banned target and unknown
  commit. It MUST never force-link credentials based only on anonymous state.
- `onLinkAccount`-style hooks MUST observe a committed mapping or a documented
  pre-commit plan and be idempotent; hook failure MUST NOT duplicate transfer.
- An authenticated anonymous subject MUST be able to delete its own anonymous
  identity through `identity.anonymous.delete` without presenting permanent-
  account credentials. The operation MUST bind the exact anonymous session,
  subject, tenant and version, revoke its session, dispose of anonymous-owned
  merge/expiry state atomically or enter reconciliation, and MUST NOT delete a
  linked permanent identity or bypass retention/legal-hold policy.
- Delete-on-link, retain/anonymize and abandoned cleanup MUST be explicit,
  bounded and compatible with audit/legal-hold policy.
- The core MUST persist only through its public Store/UnitOfWork contracts.
  Anonymous transition identity, merge journal, expiry and cleanup state belong
  to the anonymous adapter; ordinary user/account records remain owned by
  `identity`, and bearer/session records remain owned by `identity/session`.

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
