# Goal: pkg/identity/delivery

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `identity/delivery`
- Canonical module: `pkg/identity/delivery`
- Canonical goal after scaffolding: `pkg/identity/delivery/.ai/GOAL.md`
- Requires: None; this root execution unit may be claimed when its existing primitive audit is current.
- Consumes existing primitives: `workflow`, `outbox`, `audit`, `telemetry`
- Unlocks after verification: `identity/password`, `identity/email`, `identity/otp`, `identity/phone`, `organization`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/delivery` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/delivery` module that owns provider-neutral identity message intents, templates, locale selection, enqueueing, deduplication, and delivery-result contracts. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns provider-neutral identity message intents, templates, locale selection, enqueueing, deduplication, and delivery-result contracts. It does not own SMTP, SMS, push, vendor SDKs, marketing campaigns, and UI rendering. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Message, Channel, Recipient, Template, Renderer, Sender, Queue, Attempt, DeliveryResult, locale fallback, and redaction contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST render deterministic locale variants; reject unsafe template input; deduplicate enqueue; classify transient and permanent delivery failure; cancel bounded sends; avoid claiming delivery from enqueue success. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Message intents MUST cover verification, email/phone change, password reset,
  magic link, OTP, invitation, MFA OTP/recovery notification and security/admin
  notification with stable purpose and template version.
- Templates MUST declare channel, locale, subject/body/content type, required
  typed variables, sensitive variables, link/origin policy and size limits.
  Missing or extra variables and unsafe URL/HTML contexts MUST fail before
  enqueue.
- Rendering MUST have deterministic locale fallback, context-aware escaping,
  plain-text behavior and no secret-bearing diagnostic output. Template updates
  MUST not change already enqueued message semantics without versioning.
- Enqueue MUST return a stable intent/attempt ID, deduplicate by bounded
  idempotency scope and distinguish queued from delivered. Enumeration-safe
  workflows MUST be able to enqueue a no-op-equivalent result without exposing
  recipient existence.
- Sender adapters MUST define timeout, retry, provider idempotency, throttling,
  accepted/delivered/bounced classification and unknown outcome. Retries MUST
  not duplicate one-time credentials beyond documented workflow policy.
- Recipient addresses and rendered bodies are sensitive; retention, audit,
  tracing and delivery-provider deletion boundaries MUST be explicit.
- Test capture MUST use the public Sender/Queue contracts and be impossible to
  select accidentally in production configuration.

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
