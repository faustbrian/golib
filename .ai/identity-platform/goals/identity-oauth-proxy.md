# Goal: pkg/identity/oauth/proxy

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/oauth/proxy`
- Canonical module: `pkg/identity/oauth/proxy`
- Canonical goal after scaffolding: `pkg/identity/oauth/proxy/.ai/GOAL.md`
- Requires: `identity/oauth`
- Consumes existing primitives: `capability`, `secret-envelope`, `http-client`, `audit`, `rate-limit`
- Unlocks after verification: `identity/http`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with `identity/oauth` verified.
Build a callback proxy for preview/development deployments whose callback URLs
cannot be registered directly, without making the proxy an identity authority.

## Ownership and public contract

The module owns environment binding, proxy state, registered preview-origin
allowlists, production callback receipt, provider exchange delegation,
encrypted/authenticated profile-result envelopes and redirects back to the
originating preview. It does not create or update production users, accounts,
sessions or cookies; persist provider tokens; act as a general redirector; or
proxy arbitrary HTTP traffic.

Public contracts MUST define proxy and preview endpoints, shared-secret/key
rotation, deployment/environment IDs, origin registration, one-time state
store, maximum envelope lifetime/size, callback result and typed stable errors.
The profile envelope MUST bind provider, client, preview origin, redirect,
state, timestamps, key ID and result status using authenticated encryption.

## Required behavior and security

Initiation MUST accept only configured HTTPS preview origins (with a narrowly
documented loopback-development exception), bind state and PKCE context, and
produce the registered production callback. Callback MUST authenticate and
atomically consume state, delegate exchange exactly once, redact provider
failures, encrypt the minimum profile result, and redirect only to the bound
preview origin. Preview consumption MUST authenticate, decrypt, validate all
bindings and consume the envelope once before invoking generic OAuth linking
and session issuance in the preview environment.

Production-side stores and hooks MUST prove that no user, account or session
write can occur. SSRF, open redirect, DNS/IDN confusion, wildcard origin,
state/envelope replay, environment substitution, key confusion, compression
bombs and oversized provider errors MUST fail closed. Ambiguous token exchange
MUST remain reconcilable and MUST NOT be blindly retried. Proxy secrets,
authorization codes, tokens, profiles and envelopes MUST never be logged.

## Acceptance and blockers

A two-origin integration profile MUST prove the complete flow and assert zero
production identity/session writes. Tests MUST cover tamper, replay,
expiration, key rotation, concurrent callbacks, provider denial, exchange
ambiguity, preview outage, cancellation, cleanup and redaction. Exact
coverage/mutation, parser fuzz, race/leak, resource benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates MUST pass.

The unit MUST remain unverified if production can write identity state, any
redirect is not exact-bound, envelopes are reusable or unauthenticated, shared
secrets lack rotation/context, or the two-origin no-write proof is absent.
