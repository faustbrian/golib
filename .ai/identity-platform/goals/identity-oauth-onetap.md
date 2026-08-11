# Goal: pkg/identity/oauth/onetap

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/oauth/onetap`
- Canonical module: `pkg/identity/oauth/onetap`
- Canonical goal after scaffolding: `pkg/identity/oauth/onetap/.ai/GOAL.md`
- Requires: `identity/oauth`, `identity/oauth/providers`
- Consumes existing primitives: `authentication/oidc`, `authentication/jwt`, `capability`, `audit`, `rate-limit`
- Unlocks after verification: `identity/http`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
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
Initiation MUST create a purpose-bound, one-use pre-auth transaction that binds
tenant, provider, client, authorized origin, nonce, redirect/mode, initiating
subject when present, risk-policy version and the caller's exact
`identity/session.RememberPolicy`. Callback validation is read-only; credential
completion MUST reserve, apply and finalize that transaction with the generic
OAuth identity result and preserve RememberPolicy unchanged through MFA and
session issuance. Unknown completion MUST recover before retry.

The module MUST expose a transport-neutral browser seam that produces a typed,
bounded Google Identity Services initialization/prompt/button configuration and
accepts a typed credential callback command. The seam MUST identify required
script origin, CSP and Cross-Origin-Opener-Policy constraints, callback versus
redirect mode, authorized parent origin and cancellation/dismissal signals;
it MUST NOT render HTML, execute JavaScript, trust browser-supplied client
configuration or require consumers to parse an untyped map.

## Required behavior and security

The module MUST validate Google signature/JWK, issuer, audience/authorized
party, expiry/not-before/issued-at, nonce, subject and hosted domain where
configured. It MUST consume nonce/state atomically, deny replay and origin or
redirect substitution, distinguish prompt dismissal from authentication
failure, and pass email verification only according to Google's pinned claim
semantics. Callback and redirect modes MUST produce equivalent identity
decisions. Existing-account linking MUST require the generic OAuth package's
verified evidence and collision policy.

Every browser credential callback MUST validate Google One Tap CSRF binding:
the bounded `g_csrf_token` cookie and request-body value MUST both be present,
canonicalized, compared without data-dependent early acceptance and bound to
the same initiating origin and One Tap transaction before credential parsing.
Cookie absence, duplicate cookie or body values, cross-site substitution,
expired initiation and retry after consumption MUST fail closed. Redirect mode
MUST additionally bind the exact configured redirect and MUST NOT use the
credential or redirect value as CSRF state.

Inputs, JWTs, headers and redirect values MUST be bounded before decoding or
cryptographic work. Tokens, nonce, PII and raw claims MUST NOT escape. Tests
MUST cover JWK rotation, cached-key failure, `azp`, multiple audiences, clock
skew, replay races, FedCM-related callback variations, origin canonicalization,
open redirects, cancellation and account collision.

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Acceptance and blockers

Pinned Google fixtures and documented current browser/sandbox interoperability
for prompt, button and redirect behavior are REQUIRED. Exact coverage/mutation,
JWT/parser fuzz, race, bounded benchmark, clean-consumer, API/docs/changelog and
supply-chain gates MUST pass. This unit owns provider/browser interoperability
through its public contract; `identity/http` and `identity/reference` own the
later full HTTP signup/signin/link journeys.

The callback operation MUST follow [`API_OPERATIONS.md`](../API_OPERATIONS.md),
the atomic nonce/state and identity-result handoff MUST follow
[`TRANSACTION_CONTRACT.md`](../TRANSACTION_CONTRACT.md), Google token validation
MUST follow [`PROTOCOL_BASELINES.md`](../PROTOCOL_BASELINES.md), and browser and
origin defaults MUST be explicit in
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
Success and denial MUST emit the One Tap records defined by
[`SECURITY_EVENTS.md`](../SECURITY_EVENTS.md).

The unit MUST remain unverified if it trusts email or hosted domain without
pinned semantics, omits nonce/origin/audience validation, conflates dismissal
with success, requires unrecorded frontend behavior, or lacks current Google
interoperability evidence.
