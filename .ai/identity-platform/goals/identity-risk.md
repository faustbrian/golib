# Goal: pkg/identity/risk

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `identity/risk`
- Canonical module: `pkg/identity/risk`
- Canonical goal after scaffolding: `pkg/identity/risk/.ai/GOAL.md`
- Requires: `identity`
- Consumes existing primitives: `rate-limit`, `audit`, `telemetry`, `identifier`
- Unlocks after verification: `identity/risk/postgres`, `identity/risk/valkey`, `identity/risk/captcha`, `identity/risk/hibp`, `identity/password`, `identity/magiclink`, `identity/otp`, `identity/mfa`, `passkey`, `identity/oauth`, `identity/impersonation`, `sso`, `oauth-server`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/risk` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/risk` module that owns auth-specific risk signals, policy evaluation, step-up and deny decisions, abuse counters, and privacy-bounded evidence. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns auth-specific risk signals, policy evaluation, step-up and deny decisions, abuse counters, and privacy-bounded evidence. It does not own general authorization, CAPTCHA provider protocols, fraud vendor integrations, and a universal machine-learning engine. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Action, Subject, Context, Signal, Assessment, Decision, Policy, Counter, Evidence, ChallengeRequirement, and Observer contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST combine deterministic signals; distinguish allow, deny, throttle, and step-up; fail closed or degrade only per explicit action policy; bound cardinality; expire evidence; prevent cross-tenant signal contamination. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- The action catalog MUST at least cover signup, password/username signin,
  password reset/change, email/phone verification and change, OTP/magic-link
  request/consume, OAuth start/callback/link, MFA/passkey enrollment/challenge,
  API-key management, administration, impersonation, organization invitation,
  SSO/SCIM and OAuth-server authorization/token/device endpoints.
- Signals MUST distinguish trusted server facts from spoofable request hints.
  The public contract MUST accept transport-neutral verified network facts;
  it MUST NOT import or depend on `identity/http`. The later HTTP composition
  supplies facts derived only from its trusted-proxy policy. Risk processing
  MUST canonicalize IPv4/IPv6, support explicit IPv6 subnet aggregation and
  avoid storing raw addresses when a scoped digest suffices.
- Policies MUST define windows, thresholds, subject dimensions, allow/deny/
  throttle/step-up result, retry-after, evidence lifetime, fail behavior and
  override authorization. Unknown signal/provider outcomes MUST not become a
  clean assessment.
- Counters and evidence MUST have controlled cardinality, per-tenant namespace,
  bounded fan-out and deterministic clock/window boundaries. Attackers MUST
  not create unlimited keys through arbitrary identifiers, headers or actions.
- Lockout MUST resist denial-of-service against known accounts, preserve a
  safe recovery route, and never expose account existence through response,
  timing or retry metadata.
- CAPTCHA and HIBP are signals, not decisions. Their provider-specific evidence
  and unavailable/ambiguous outcomes MUST survive normalization for the action
  policy and audit trail.
- Trusted administrative overrides MUST be narrow, expiring and audited; a
  generic context flag MUST not bypass risk evaluation.

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
