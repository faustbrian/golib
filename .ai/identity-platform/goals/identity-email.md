# Goal: pkg/identity/email

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `identity/email`
- Canonical module: `pkg/identity/email`
- Canonical goal after scaffolding: `pkg/identity/email/.ai/GOAL.md`
- Requires: `identity`, `identity/delivery`
- Consumes existing primitives: `capability`, `workflow`, `audit`, `identifier`
- Unlocks after verification: `identity/magiclink`, `identity/http`

## Start gate

The worker MUST read and satisfy `../COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/email` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/email` module that owns email address verification, primary-email change, re-verification, and verified-address lifecycle. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns email address verification, primary-email change, re-verification, and verified-address lifecycle. It does not own mail transport, password reset, magic-link signin, and email-client UI. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define AddressPolicy, VerificationProfile, ChangeProfile, Service, Delivery, CallbackResult, and capability payload contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST send enumeration-safe verification; consume once; bind action and subject; race address claims; confirm old/new address policy; revoke superseded links; avoid losing the last reachable verified identifier. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `../COMMON_REQUIREMENTS.md`.

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
