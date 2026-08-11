# Goal: pkg/identity/username

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/username`
- Canonical module: `pkg/identity/username`
- Canonical goal after scaffolding: `pkg/identity/username/.ai/GOAL.md`
- Requires: `identity`, `identity/password`
- Consumes existing primitives: `identifier`, `authorization`, `audit`, `rate-limit`
- Unlocks after verification: `identity/http`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with both prerequisites verified.
Build username signup, password signin, availability and rename workflows on
the identity and password contracts, with normalized uniqueness independent of
the user-visible display username.

## Ownership and public contract

This module owns username syntax policy, canonicalization, reserved-name
policy, canonical and display forms, availability semantics, username-based
password authentication orchestration and rename workflow. `identity` remains
owner of identifier persistence and account state; `identity/password` remains
owner of password proof and session consequences. This module does not own
profiles, handles for organizations, email aliases, UI, or a persistence
schema that duplicates identity identifiers.

The public contract MUST define `Policy`, canonicalizer, validator,
`AvailabilityService`, signup/signin input, rename command, authorization and
audit hooks, typed stable errors and enumeration-safe public outcomes. Minimum
and maximum length MUST be measured in explicitly documented units before and
after normalization. Allowed scripts, case folding, Unicode normalization,
confusables, mixed-script policy, reserved words and custom validators MUST be
explicit and deterministic. Display username MUST never be used as the unique
lookup key.

## Required behavior

Signup MUST validate/canonicalize once and atomically reserve the username with
identity creation. Signin MUST use the same canonicalization and the password
workflow's enumeration, rate, risk, audit and session policies. Availability
MUST be rate-limited, non-authoritative, and never promise a later reservation.
Rename MUST authorize a fresh session where policy requires, atomically swap
identifiers, preserve or release the prior name according to documented
cooldown policy, invalidate caches, and produce stable domain events.

Concurrent signup/rename, canonical collisions, case variants, Unicode
equivalents, reserved-name races, validator cancellation and ambiguous commits
MUST be tested. Public signin timing and errors MUST NOT reveal whether a
username exists or is suspended. Names and user IDs MUST NOT become unbounded
metric labels; audit must follow PII policy.

## Acceptance and blockers

Tests MUST prove signup, signin, update, availability, custom validation,
length boundaries, canonical/display separation, collision races, rollback,
cross-tenant policy and password/session interactions. Property/fuzz tests are
REQUIRED for normalization and validation. The unit MUST prove the repository
contract has one atomic reservation outcome under concurrent callers. Real
PostgreSQL uniqueness and composed HTTP email-or-username journeys belong to
`identity/reference` after the relevant adapters and transport are verified.
Exact coverage/mutation, race, benchmark, clean-consumer, API/docs/changelog and
supply-chain gates MUST pass.

The unit MUST remain unverified if canonicalization differs by operation,
availability is treated as reservation, uniqueness is only process-local,
display names become lookup keys, or enumeration/collision behavior is not
proved under concurrency.
