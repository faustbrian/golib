# Goal: pkg/identity/identitytest

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/identitytest`
- Canonical module: `pkg/identity/identitytest`
- Canonical goal after scaffolding: `pkg/identity/identitytest/.ai/GOAL.md`
- Requires: `identity/reference`
- Consumes existing primitives: `postgres`, `migrations`, `identifier`
- Unlocks after verification: No program unit.

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with `identity/reference` verified.
Build a test-only consumer toolkit for deterministic identity-platform tests,
using public APIs and the complete HTTP surface without creating production
authentication bypasses.

## Ownership and public contract

The module owns test identity/account/organization/client factories, isolated
PostgreSQL/Valkey fixture lifecycle, migration/reset helpers, deterministic
clock and cryptographic-random sources explicitly safe only for tests,
delivery and OTP capture, session/cookie/API-key request helpers, provider and
protocol fixture drivers, failure injection, cleanup assertions and compact
end-state scenario helpers. It does not own production routes, production
configuration, alternate domain semantics, global process state or mocks that
replace meaningful package integration.

Every helper MUST accept `testing.TB` or an explicit lifecycle owner, register
cleanup immediately, be safe for parallel tests or reject parallel use
explicitly, and return public-domain objects/results. Factories MUST produce
valid defaults with explicit overrides and MUST not rely on package internals.
Generated credentials MUST be redacted and scoped to disposable resources.

## Required behavior and isolation

The toolkit MUST create isolated database schemas/namespaces, apply migrations,
start a complete reference handler, create verified/unverified/suspended users,
organizations and OAuth clients, capture and consume magic links/OTPs, perform
password/social/MFA/passkey/session/API-key HTTP operations, inject provider
success/failure/ambiguity, advance time deterministically, and assert audit and
cleanup outcomes. It MUST support table-driven and parallel scenarios without
identifier, cookie, cache, clock or provider-fixture collisions.

Privileged helpers MUST be unavailable to production construction and MUST not
register public routes, honor production credentials, disable authorization,
weaken CSRF/origin policy, or compile into the reference server through blank
imports. Test deterministic randomness MUST carry an unmistakable unsafe-for-
production contract. Cleanup MUST occur on success, failure, panic and
cancellation and MUST remove only task-owned resources.

## Acceptance and blockers

Clean external-consumer examples MUST prove minimal setup, parallel isolation,
OTP capture, authenticated HTTP calls, failure injection and cleanup. Tests
MUST prove no production route/config exposure, no shared-state leakage,
deterministic reproducibility, migration compatibility, cancellation and
partial-setup cleanup. The complete end-state suite MUST consume these helpers
without private imports or manual database mutation.

Exact coverage/mutation, race/stress/leak, fixture parser fuzz where applicable,
setup/cleanup benchmarks, clean-consumer, API/docs/examples/changelog and
supply-chain gates MUST pass. The unit MUST remain unverified if helpers bypass
observable contracts, can affect non-task-owned data, are unsafe in parallel,
leak secrets/resources, or can be enabled in production.
