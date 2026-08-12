# Goal: pkg/oauth-server/device

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `oauth-server/device`
- Canonical module: `pkg/oauth-server/device`
- Canonical goal after scaffolding: `pkg/oauth-server/device/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:oauth-server/device:v1`; owned operation IDs: `contract:operation:identity.oauth-server.device-approve:v1`, `contract:operation:identity.oauth-server.device-authorize:v1`, `contract:operation:identity.oauth-server.device-deny:v1`, `contract:operation:identity.oauth-server.device-inspect:v1`, `contract:operation:identity.oauth-server.device-token:v1`
- Requires: `oauth-server`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `capability`, `rate-limit`, `audit`
- Unlocks after verification: `oauth-server/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `oauth-server/device` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/oauth-server/device` module that owns OAuth 2.0 device authorization grant, verification codes, polling policy, user approval/denial, expiry, and token exchange. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns OAuth 2.0 device authorization grant, verification codes, polling policy, user approval/denial, expiry, and token exchange. It does not own MCP auth, agent auth, device UI, and general OAuth token machinery. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define DeviceRequest, DeviceCode, UserCode, Verification, PollPolicy, Approval, Denial, Store, TokenExchange, and metadata contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

The public contract MUST separate device authorization, user-code inspection,
authenticated approval/denial and device polling. Each operation MUST expose
the actor and authorization required by
[`API_OPERATIONS.md`](../API_OPERATIONS.md), and MUST return typed protocol
outcomes without revealing whether an arbitrary user code exists.
Construction MUST consume the public `oauthserver.Service` interface and its
oauth-server-owned projections directly. This child MUST NOT declare an opaque
`CoreAuthority` value that no external composition root can construct.

## Required behavior

The implementation and tests MUST generate non-confusable user codes and high-entropy device codes; store digests; bind client and scope; throttle polling with slow_down; make approval/denial atomic; expire and consume once; avoid user-code enumeration; test RFC error transitions and independent clients. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Device authorization MUST validate client, grant permission and requested
  scopes before issuing codes and MUST publish verification URI and complete
  URI exactly according to the configured public base URL. It MUST allocate and
  persist a server-owned command identity; a proprietary `Idempotency-Key` MAY
  be accepted as an extension but MUST NOT be required.
- Device authorization MUST bind issuer, client, requested resource/audience,
  scopes, tenant and the exact verification URI to one transaction. It MUST
  issue `verification_uri_complete` only when the complete URI remains bounded
  and does not leak a bearer-equivalent device code. Discovery MUST advertise
  the endpoint only when the entire device profile is enabled.
- Device construction MUST consume `oauth_server.device.enabled`,
  `oauth_server.device.verification_uri` and
  `oauth_server.device.verification_uri_complete_bytes`. Disabled composition
  MUST advertise and register no device endpoint. The configured verification
  URI is the sole public URI authority; a request, proxy header or client MUST
  NOT replace its origin or path.
- Device codes MUST contain exactly 32 random bytes, use canonical
  43-character unpadded base64url, and have digest-at-rest lookup through the
  versioned domain-separated key configured by the reference profile; user
  codes MUST be non-confusable, bounded, rate-limited and collision-safe.
- Subject and consent bindings at issuance are nullable. Optional pre-binding
  MUST require explicit authenticated policy and MUST NOT let a device choose
  an arbitrary subject. Inspection MUST validate subject and consent versions
  only when those bindings are present; absence is not a stale-binding error.
- Polling MUST return authorization_pending, slow_down, access_denied,
  expired_token and success with exact interval escalation and no timing-based
  user-code enumeration.
- Polling MUST authenticate the client according to its registered public or
  confidential method, bind the device code to that client and token endpoint,
  enforce one authoritative next-poll time atomically, and increase the
  interval by exactly five seconds after every RFC 8628 `slow_down`. The
  increment MUST NOT be configurable. Concurrent polls
  MUST yield at most one success; cancellation, denial, expiry or consumption
  MUST permanently win over later approval or exchange.
- Verification UI contracts MUST support inspect, approve and deny with recent
  authentication, client/scope display and CSRF protection. Approval MUST bind
  the reviewing subject and consent version atomically when either was absent,
  or revalidate the exact pre-bound values when present.
- Inspection MUST return only bounded client display metadata and requested
  scopes/resources after canonical user-code lookup and rate limiting. The UI
  MUST always display the canonical user code and require the user to explicitly
  compare and confirm it before approval, including when navigation began from
  `verification_uri_complete`; the embedded code MUST NOT silently approve.
  Approval MUST revalidate client status, scope/resource policy, current user
  authority and recent authentication; it MUST NOT accept a subject supplied by
  the device. Denial MUST be idempotent without disclosing whether a different
  subject previously acted.
- Issue, inspect, approve, deny and poll MUST map the code pair onto capability
  Issue, Validate, Reserve, Apply, Finalize and Recover roles. This package owns
  permanent winning device state; core owns client, consent, subject and token
  authority. Every inspect, approve and poll MUST revalidate those versions;
  client deletion, consent revocation, subject disablement/deletion or global
  compromise MUST permanently prevent later approval or token success.
- Custom code generators and client validators MUST meet entropy,
  canonicalization, authorization and redaction contracts or fail
  construction.
- RFC 8628 fixtures plus an independent CLI/client MUST prove issuance,
  polling, slow-down, approval, denial, expiry, replay and cancellation.

Issuance, polling, approval, denial and consumption MUST implement
[`TRANSACTION_CONTRACT.md`](../TRANSACTION_CONTRACT.md). Protocol metadata,
errors and polling behavior MUST conform to
[`PROTOCOL_BASELINES.md`](../PROTOCOL_BASELINES.md); expiry, client deletion,
subject disablement and consent revocation MUST follow
[`LIFECYCLE_CASCADES.md`](../LIFECYCLE_CASCADES.md); and code entropy,
lifetimes, interval and verification base URL MUST be explicit in
[`REFERENCE_CONFIGURATION.md`](../REFERENCE_CONFIGURATION.md).
Authorization issuance MUST emit exactly
`identity.oauth_server.authorize_device`; approval MUST emit exactly
`identity.oauth_server.approve_device`; denial and security-relevant polling
MUST emit exactly `identity.oauth_server.deny_device` and
`identity.oauth_server.poll_device` respectively. Inspection is read-only and
emits none.

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
