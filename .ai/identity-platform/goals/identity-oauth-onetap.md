# Goal: pkg/identity/oauth/onetap

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/oauth/onetap`
- Canonical module: `pkg/identity/oauth/onetap`
- Canonical goal after scaffolding: `pkg/identity/oauth/onetap/.ai/GOAL.md`
- Requires: `identity/oauth`, `identity/oauth/providers`
- Consumes existing primitives: `authentication/oidc`, `authentication/jwt`, `capability`, `audit`, `rate-limit`
- Unlocks after verification: `identity/http`

## Start gate and objective

The worker MUST satisfy `../COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress` with both prerequisites verified.
Build Google Identity Services One Tap server-side orchestration for prompt and
button modes, using the pinned Google provider profile and the existing OAuth
link/session policies.

## Ownership and public contract

The module owns One Tap request context, nonce/state generation and
consumption, credential callback validation, authorized-origin and redirect
policy, prompt/button mode options, login-hint and context policy, dismissal
classification, and conversion of a valid Google credential into the generic
OAuth identity result. It does not serve Google's JavaScript, render UI,
weaken Google token validation, own generic account linking, or invent a
session outside `identity/oauth` policy.

Public contracts MUST expose bounded configuration, nonce/state store,
credential command, redirect result, typed prompt status and stable redacted
errors. Client ID, issuer, audience, authorized origins, hosted-domain policy,
clock skew, nonce lifetime, redirect allowlist and account-linking policy MUST
be explicit. Cross-Origin-Opener-Policy and browser integration requirements
MUST be documented for consumers without embedding framework code.

## Required behavior and security

The module MUST validate Google signature/JWK, issuer, audience/authorized
party, expiry/not-before/issued-at, nonce, subject and hosted domain where
configured. It MUST consume nonce/state atomically, deny replay and origin or
redirect substitution, distinguish prompt dismissal from authentication
failure, and pass email verification only according to Google's pinned claim
semantics. Callback and redirect modes MUST produce equivalent identity
decisions. Existing-account linking MUST require the generic OAuth package's
verified evidence and collision policy.

Inputs, JWTs, headers and redirect values MUST be bounded before decoding or
cryptographic work. Tokens, nonce, PII and raw claims MUST not escape. Tests
MUST cover JWK rotation, cached-key failure, `azp`, multiple audiences, clock
skew, replay races, FedCM-related callback variations, origin canonicalization,
open redirects, cancellation and account collision.

## Acceptance and blockers

Pinned Google fixtures and documented current browser/sandbox interoperability
for prompt, button and redirect behavior are REQUIRED. Exact coverage/mutation,
JWT/parser fuzz, race, bounded benchmark, clean-consumer, API/docs/changelog and
supply-chain gates MUST pass, followed by full HTTP signup/signin/link journeys.

The unit MUST remain unverified if it trusts email or hosted domain without
pinned semantics, omits nonce/origin/audience validation, conflates dismissal
with success, requires unrecorded frontend behavior, or lacks current Google
interoperability evidence.
