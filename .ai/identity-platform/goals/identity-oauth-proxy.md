# Goal: pkg/identity/oauth/proxy

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/oauth/proxy`
- Canonical module: `pkg/identity/oauth/proxy`
- Canonical goal after scaffolding: `pkg/identity/oauth/proxy/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/oauth/proxy:v1`; owned operation IDs: `contract:operation:identity.oauth.proxy-forward:v1`
- Requires: `identity/oauth`, `primitive/capability-identity-contracts`
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
State and envelope bindings MUST also include tenant, initiating command ID and
request fingerprint, generic OAuth transaction, claims-mapping profile/version,
claims provenance, intended preview client/recipient and the caller's unchanged
`identity/session.RememberPolicy`. Validation grants no authority; production
callback and preview consumption MUST each reserve, apply and finalize their
own one-use artifact, and unknown outcomes MUST recover before retry.

## Required behavior and security

Initiation MUST accept only configured HTTPS preview origins (with a narrowly
documented loopback-development exception), bind state and PKCE context, and
produce the registered production callback. Callback MUST authenticate and
atomically consume state, delegate exchange exactly once, redact provider
failures, encrypt the minimum profile result, and redirect only to the bound
preview origin. Preview consumption MUST authenticate, decrypt, validate all
bindings and consume the envelope once before invoking generic OAuth linking
and session issuance in the preview environment.

The production exchange boundary MUST define ownership of authorization codes,
access tokens, refresh tokens and provider response bodies on every success,
denial, timeout, cancellation and ambiguous outcome. Provider tokens MUST
never enter the preview envelope or preview logs. The proxy MUST retain them
only in bounded memory long enough to derive the minimum verified profile,
close every response, zero or discard recoverable buffers where practicable,
and invoke configured revocation when the provider supports it. A refresh token
MUST NOT be persisted, forwarded or silently abandoned as an accepted success;
if revocation or disposal outcome is unknown, the transaction MUST be recorded
as reconciliation-required without exposing the credential.

Production-side stores and hooks MUST prove that no user, account or session
write can occur. SSRF, open redirect, DNS/IDN confusion, wildcard origin,
state/envelope replay, environment substitution, key confusion, compression
bombs and oversized provider errors MUST fail closed. Ambiguous token exchange
MUST remain reconcilable and MUST NOT be blindly retried. Proxy secrets,
authorization codes, tokens, profiles and envelopes MUST never be logged.

Initiation, callback and preview-consumption authority MUST match
[`API_OPERATIONS.md`](../API_OPERATIONS.md). State, exchange and envelope
consumption MUST match [`TRANSACTION_CONTRACT.md`](../TRANSACTION_CONTRACT.md),
token and envelope expiry/revocation MUST match
[`LIFECYCLE_CASCADES.md`](../LIFECYCLE_CASCADES.md), and preview origins, keys,
lifetimes and size limits MUST be explicit in
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
Pending and reserved proxy transactions and envelopes MUST consume and fail
closed on `lifecycle.cascade.global_compromise`,
`lifecycle.cascade.identity_anonymize`, `lifecycle.cascade.identity_delete`,
`lifecycle.cascade.social_provider_disable` and
`lifecycle.cascade.social_provider_unlink`; this module acknowledges those
versions but does not own the underlying identity or provider-link authority.
Every forwarded or denied proxy transaction MUST emit exactly
`identity.oauth.use_proxy`. Generic OAuth start/callback/link/account records
remain owned by `identity/oauth`, and session records remain owned by
`identity/session`.

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

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
