# Goal: pkg/identity/risk

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk`
- Canonical module: `pkg/identity/risk`
- Canonical goal after scaffolding: `pkg/identity/risk/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/risk:v1`; owned operation IDs: `contract:operation:identity.risk.evaluate:v1`
- Requires: `identity`
- Consumes existing primitives: `rate-limit`, `audit`, `telemetry`, `identifier`
- Unlocks after verification: `identity/risk/postgres`, `identity/risk/valkey`, `identity/risk/captcha`, `identity/risk/hibp`, `identity/password`, `identity/magiclink`, `identity/otp`, `identity/phone`, `identity/anonymous`, `identity/mfa`, `passkey`, `identity/oauth`, `identity/impersonation`, `sso`, `oauth-server`, `identity/http`

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

The design MUST define Action, Subject, Context, Signal, Assessment, Decision, Policy, Counter, Evidence, RiskEvidence, CaptchaEvidence, CaptchaEvidenceContributor, ChallengeRequirement, and Observer contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

CAPTCHA provider adapters MUST return normalized bounded verification facts
only. This core owns the issuance decision and MUST issue `CaptchaEvidence`
through an injected durable contributor; it MUST NOT construct an evidence
reference from provider output or import a concrete persistence adapter.
`identity/risk` MUST derive the replay fingerprint itself from the raw provider
token and trusted profile scope using the exact configured keyed construction;
it MUST NOT accept a caller- or adapter-supplied fingerprint.

The implementation and tests MUST combine deterministic signals; distinguish allow, deny, throttle, and step-up; fail closed or degrade only per explicit action policy; bound cardinality; expire evidence; prevent cross-tenant signal contamination. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

`Decision.Action` is the exact closed enum `allow`, `deny`, `throttle`, and
`step_up`; API/acceptance text may render `step_up` as a challenge but MUST NOT
introduce `challenge` as a fifth internal value. `revoke` is not a risk action.
When policy requires session revocation after a `deny`, a separately authorized
response coordinator invokes `identity.session.revoke-all` and records that
effect alongside the unchanged `deny` decision; risk evaluation itself neither
owns nor reports `revoke`.

## Package-specific acceptance checklist

- The action catalog MUST equal the closed `risk.operation_matrix` in
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`. Every evaluation MUST
  receive exactly one canonical listed action; aliases, category-only values,
  unlisted actions and caller-selected fail behavior MUST deny before signal
  access. Construction MUST validate every canonical public-operation ID
  against `struct:ref.risk.operation_matrix` completeness metadata and its
  closed default; no operation may be silently omitted. A package MUST NOT add
  a local action or exception.
- Signals MUST distinguish trusted server facts from spoofable request hints.
  The public contract MUST accept transport-neutral verified network facts;
  it MUST NOT import or depend on `identity/http`. The later HTTP composition
  supplies facts derived only from its trusted-proxy policy. The selected
  reference profile MUST canonicalize IPv4/IPv6 and use the full canonical RFC
  5952 IPv6 address without subnet aggregation. Explicit IPv6 subnet
  aggregation MAY exist only in a future, separately selected profile. Risk
  processing MUST avoid storing raw addresses when a scoped digest suffices.
- Policies MUST define windows, thresholds, subject dimensions, allow/deny/
  throttle/step-up result, retry-after, evidence lifetime, fail behavior and
  override authorization by consuming the operation-specific risk matrix in
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`; implementations MUST NOT
  duplicate its numeric defaults or silently substitute another profile.
- Evaluation MUST use the exact precedence `deny > step-up > throttle > allow`.
  It MUST evaluate every mandatory signal for the selected operation, but MAY
  skip an expensive signal after an irreversible deny. A skipped signal MUST
  be recorded as skipped, not unavailable or clean, and MUST NOT weaken the
  deny decision.
- Provider unavailable/unknown results MUST use the selected operation's exact
  matrix outcome in `REFERENCE_CONFIGURATION.md`. In the reference profile,
  HIBP unavailability for password create/change/reset and CAPTCHA ambiguity
  for protected signup/signin/reset/credential change are deny; CAPTCHA
  ambiguity for a low-risk read is step-up only when an independent configured
  factor exists and otherwise deny. No unavailable result becomes allow or a
  clean assessment.
- Counter mutation MUST occur exactly at the phase declared by the selected
  operation profile. `not-committed` MAY be retried within its bounded replay
  contract; `committed` MUST consume the returned post-mutation snapshot;
  `unknown` MUST become an unavailable signal and MUST NOT be retried unless an
  operation-scoped idempotency key proves the retry cannot double-mutate.
- Counters and evidence MUST have controlled cardinality, per-tenant namespace,
  bounded fan-out and deterministic clock/window boundaries. Attackers MUST
  not create unlimited keys through arbitrary identifiers, headers or actions.
  Construction MUST enforce `risk.velocity.max_dimensions`,
  `risk.velocity.max_counters`, `risk.velocity.max_window`,
  `risk.velocity.max_limit`, `risk.velocity.key_bytes` and
  `risk.velocity.retry_after_max`; each operation profile's lower exact bounds
  also apply and a caller cannot add a dimension or counter.
- Stored keys MUST use a versioned keyed digest with domain separation over the
  canonical tenant, operation, dimension kind and dimension value. Unkeyed
  hashes, cross-tenant digest reuse and raw subject/network/provider values in
  keys MUST be rejected. Rotation MUST preserve active-window and durable-
  lockout continuity without creating a bypass.
- Lockout MUST resist denial-of-service against known accounts, preserve a
  safe recovery route, and never expose account existence through response,
  timing or retry metadata.
- Each configured state class MUST have exactly one mutation authority.
  PostgreSQL MUST own durable lockout state and the evidence/decision journal;
  Valkey MUST own only short-lived attempt, velocity, concurrency and challenge
  windows. Core construction MUST reject overlapping ownership. Valkey loss,
  eviction or reset MUST NOT clear, shorten or supersede a durable lockout.
- CAPTCHA and HIBP are signals, not decisions. Their provider-specific evidence
  and unavailable/ambiguous outcomes MUST survive normalization for the action
  policy and audit trail.
- For phone password recovery, `identity/risk` MUST be the sole authority that
  issues immutable `RiskEvidence` after evaluating SIM-swap, number-recycling
  and carrier signals. Each item MUST be fresh with the selected `risk_ttl`
  (two minutes in the reference profile), MUST bind tenant, subject, recovery
  operation, recovery purpose, canonical number, pre-auth transaction, attempt
  ID and risk-policy version, and MUST be atomically consumed at most once by
  `identity/risk`. Positive, unknown or unavailable carrier-risk decisions MUST
  deny issuance. Stale, mismatched or replayed evidence MUST deny before
  credential mutation; callers MUST NOT mint evidence or supply raw carrier
  facts or decisions as equivalent input.
- `identity.risk.evaluate` is the canonical RiskEvidence issuance operation.
  It MUST accept a trusted issuance phase and authoritative server-resolved
  facts plus the complete binding. The phase catalog MUST be exactly `none`,
  `phone-reset-initiation`, and `phone-reset-completion`; `none` is non-issuing.
  Phase `phone-reset-initiation` MUST map only
  to purpose `phone-password-reset-initiate`; phase `phone-reset-completion`
  MUST map only to purpose `phone-password-reset-complete`. Purpose MUST be
  derived exclusively from the issuing phase. Unknown/unsupported phases, a
  purpose with `none`, and every caller-supplied purpose MUST deny before
  provider evaluation or state access. Callers MUST NOT
  fabricate or override phase, purpose, facts, provider evidence, decisions, or
  evidence results.
- Evidence-producing evaluation MUST use a command ID/fingerprint and enlist
  an injected durable RiskEvidence contributor through the shared unit of work; the reference composition supplies `identity/risk/postgres`. Denied MUST return no
  reference; Failed MUST prove no issued row; Unknown MUST return no reference
  and recover the same command before any retry; same-command and
  same-fingerprint replay MUST return the exact recorded result without
  evaluating providers or issuing another artifact.
- The result MUST expose only an opaque RiskEvidence reference and safe purpose,
  issued-at, expires-at, and one-use metadata; raw signals, provider evidence,
  decision internals, embedded evidence payloads, keyed digests, signatures, and journal
  identifiers MUST NOT cross the public contract.
- Issuance MUST persist the one-use record through an injected durable contributor; the reference composition supplies `identity/risk/postgres`; an
  immutable bearer without that durable `issued` row is not valid RiskEvidence.
  Core validation MAY perform a read-only precheck, but only the enlisted
  durable reserve/apply/finalize protocol grants authority to the recovery
  command.
- Phone reset initiation and completion MUST receive separate artifacts with
  purposes `phone-password-reset-initiate` and
  `phone-password-reset-complete`; their references, keyed digests,
  reservations, and terminal records MUST remain distinct. Evidence issued for
  one phase MUST NOT validate, reserve, replay, or substitute for the other.
- Trusted administrative overrides MUST be narrow, expiring and audited; a
  generic context flag MUST NOT bypass risk evaluation.
- Every assessment and mutation MUST bind a trusted operation identifier,
  tenant, action, purpose, subject dimensions, policy version and replay or
  idempotency identifier as applicable. Caller-supplied labels MUST NOT be
  treated as trusted operation identity, and replay under another binding MUST
  fail before state mutation or provider evidence reuse.
- Risk decisions and state changes MUST emit the applicable canonical events
  from `.ai/identity-platform/SECURITY_EVENTS.md`; retention, subject erasure,
  tenant deletion, key rotation and disablement MUST follow
  `.ai/identity-platform/LIFECYCLE_CASCADES.md`.

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
