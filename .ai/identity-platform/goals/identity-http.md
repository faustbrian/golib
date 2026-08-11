# Goal: pkg/identity/http

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/http`
- Canonical module: `pkg/identity/http`
- Canonical goal after scaffolding: `pkg/identity/http/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/http:v1`; owned operation IDs: `contract:operation:identity.health:v1`, `contract:operation:identity.openapi.document:v1`
- Requires: every feature/protocol contract listed explicitly in `INVENTORY.md`, plus `primitive/authorization-identity-contracts`
- Consumes existing primitives: `authentication`, `authorization`, `openapi`, `audit`, `rate-limit`, `telemetry`
- Unlocks after verification: `identity/reference`

The full explicit prerequisite list in `INVENTORY.md` is authoritative and
MUST be rendered into the worker assignment. This transport unit MUST NOT
start while a listed feature or protocol contract is unverified. Concrete
PostgreSQL, Valkey, HIBP and CAPTCHA adapters belong to
`identity/reference` and MUST NOT become mandatory dependencies here.

## Start gate

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress`, records this worker, and verifies
every explicit prerequisite in the inventory. The worker MUST reject a
summarized prerequisite list or any assignment based on a stale integration
commit.

## Objective

Build the reusable standard-library `net/http` transport and route composition
that makes every feature contract usable without consumer-written handlers.
It MUST expose every in-scope parity operation through public package
contracts. Deployment assembly and real-adapter end-state proof belong to
`identity/reference`.

## Ownership boundary and public contract

This module owns HTTP route composition, handlers, request decoding and
limits, content negotiation, stable error envelopes, auth context bridging,
cookie policy, CSRF, CORS/trusted origins and proxies, redirect allowlists,
security/cache headers, hooks/middleware, idempotency surfaces, route and
operation-ID collision detection, and OpenAPI 3.1.1 composition. It does not
reimplement domain, protocol, persistence or provider behavior; render an
admin UI; import concrete storage/provider adapters; assemble a deployment;
introduce global registration; or hide configuration decisions.

The public API MUST define immutable route sets, explicit dependency bundle,
server lifecycle, base-path and origin policy, cookie/CSRF policy, request and
response limits, route-rate policy and store, error mapper, typed extension
module, migration contributor, middleware/hook phases and OpenAPI builder.
Missing dependencies, duplicate routes,
duplicate operation IDs, incompatible schemas, unsafe cookie/origin settings
and unsupported feature combinations MUST fail during construction.

## Complete endpoint surface

The canonical operation inventory is
`.ai/identity-platform/API_OPERATIONS.md`. This module MUST consume that file as
input to its endpoint acceptance manifest and MUST implement every `both` or
`protocol` row plus the route behavior of every `middleware` row; `direct`
rows MUST remain direct-only. A required HTTP row is implemented only when its exact
method/path, authentication and authorization modes, CSRF/origin policy,
request/parser limits, idempotency contract, success and error envelopes,
handler, rate policy and OpenAPI operation are present and behaviorally proved.
The worker MUST NOT substitute this prose summary, a package export inventory,
or generated OpenAPI for the canonical operation inventory.
`identity.platform.bootstrap-administrator` is a direct-only offline reference
composition operation. It MUST NOT have a handler, route, schema path or
OpenAPI operation, even while bootstrap is enabled for operator invocation.

Handlers and OpenAPI MUST cover identity/account lifecycle; email, password,
username and verification; sessions, multi-account switching and last login
method; magic links, OTP, phone and anonymous upgrade; MFA, WebAuthn and
passkeys; social OAuth, provider catalog, One Tap and proxy; API keys;
authorized administration, bans, session control and impersonation;
organizations, members, invitations, roles, permissions and teams; enterprise
OIDC/OAuth/SAML; SCIM 2.0; OAuth/OIDC provider, consent, client management and
device authorization; CAPTCHA/risk/HIBP interactions; localization; health and
readiness limited to safe operational state; and one-time session-transfer
generate/consume endpoints. No in-scope parity row may be
omitted because an application could add a handler later.

When API-key session authentication is enabled, HTTP MUST extract exactly the
configured single header, reject duplicate/ambiguous credentials and cookie
fallback, invoke `identity.apikey.session-authenticate` once, and attach its
request-scoped session-compatible principal only to the current request. Route
rate limiting, authorization and audit consume that result; no downstream
handler may reverify or redebit the key. Disabled configuration MUST register
no API-key session authenticator, and organization-owned keys MUST never enter
the user-session authentication path.

## Extension, custom-field, and popup contract

- Before hooks MUST run after bounded decoding and authentication context
  construction but before the documented domain transaction; after hooks MUST
  declare whether they observe attempted or committed results. Hook order,
  short-circuit response, error mapping, deadline and reentrancy MUST be fixed.
- Background work MUST use an owned queue/outbox with shutdown and evidence;
  request hooks MUST NOT start fire-and-forget goroutines.
- Typed additional identity and session fields MUST be included in request,
  response and OpenAPI schemas only when their field policy permits it.
  Sensitive/write-only fields MUST never appear in responses or examples.
- Custom session enrichment MUST preserve stable core session fields, expose a
  declared schema, honor cache/error policy and remain consistent across direct
  handler calls and network HTTP.
- OAuth popup completion MUST emit a minimal bounded HTML response with a
  nonce-bearing CSP, exact opener origin and one-time result; it MUST use an
  exact-origin `postMessage`, clear sensitive URL state, close only after
  delivery acknowledgment/timeout, and provide a normal redirect fallback.
  Popup result pages MUST NOT load third-party scripts.
- Browser-extension and cross-domain deployments MUST use an explicit trusted
  origin/base-URL profile. Extension origins, reverse-proxy host rewriting and
  shared-parent-domain cookies MUST NOT enable wildcard trust.

## Endpoint acceptance manifest

The module MUST maintain one machine-readable manifest that maps every
in-scope parity operation to HTTP method, path, authentication, authorization,
CSRF mode, request limit, idempotency, success/error types, handler owner and
OpenAPI operation ID. Construction and CI MUST fail when a parity operation is
missing, duplicated, undocumented, registered without authorization policy or
documented without a handler. The manifest is an API contract, not a
source-text substitute for behavioral tests.

Generation MUST preserve the canonical operation identifier from
`API_OPERATIONS.md`, reject unknown or stale rows, and emit a deterministic
coverage report containing every canonical row, including explicitly non-HTTP
rows. CI MUST compare both directions: every required HTTP row has one handler
and OpenAPI operation, and every registered handler/OpenAPI operation names one
canonical row or a validated typed-extension operation. Conditional features
MUST retain their rows and fail construction when enabled without the required
dependency; disabling a feature MUST produce the specified unavailable or
not-found behavior and MUST NOT silently remove unrelated routes.

Typed extension modules MUST contribute bounded route/operation schemas,
middleware and request/response hooks, authorization/CSRF/idempotency metadata,
per-route rate rules, reviewed trusted origins and migration contributors
without global registration. Construction MUST reject route, operation,
component, migration-owner and origin-policy conflicts before serving.

## HTTP security and semantics

Every request MUST have bounded header, URL, body, multipart/form and decoded
field limits before allocation or cryptographic work. Accepted methods,
content types, status codes, cache behavior and stable error codes MUST be
specified. Cookie sessions MUST explicitly enforce `Secure`, `HttpOnly`,
`SameSite`, domain/path, prefix, partition/cross-site and rotation semantics.
State-changing cookie requests MUST have unbypassable CSRF binding. Bearer
endpoints MUST NOT rely on cookie CSRF assumptions.
The reference profile MUST use `csrf.referer_fallback=deny`. A non-reference
profile MAY select `same-origin-https`, but only when Origin is absent and a
syntactically valid absolute HTTPS Referer has the exact trusted effective
origin. Missing, opaque, downgraded, cross-origin, userinfo-bearing or malformed
Referer values MUST deny.

Login, signup, recovery, verification and account-link callbacks MUST apply the
exact initiating origin, return target, state/nonce/PKCE and one-time
continuation binding declared by their canonical row. Login CSRF MUST be
prevented even before a user session exists: OAuth/OIDC/SAML, magic-link, OTP,
One Tap, passkey/WebAuthn and native provider-signin starts and completions MUST
bind the initiating browser transaction and reject unsolicited, swapped,
replayed or cross-origin completions. Native provider signin MUST use the same
bounded provider contract and account-collision/linking policy as redirect
signin; accepting a provider token directly MUST NOT skip issuer, audience,
nonce, authorized-party, proof-of-possession or replay validation required by
that provider profile.

Cookie emission and deletion MUST use one reviewed serializer that enforces the
selected prefix, host/domain/path, `Secure`, `HttpOnly`, `SameSite`, partitioned
and cross-site invariants on every success, rotation, transfer, logout and error
path. Cookie-authenticated unsafe methods MUST require a session-bound token
and exact trusted `Origin`, with a narrowly documented same-origin Referer
fallback only when permitted; missing, opaque and `null` origins fail closed.
Bearer authentication MUST be acquired only from the single strict
`Authorization: Bearer` header grammar, reject duplicates, comma folding,
empty/oversized/control-bearing credentials and proxy-injected authorization,
ignore ambient cookies, and never accept bearer material from query strings,
forms or redirects unless a protocol row explicitly owns a different
single-use credential.

Request parsing MUST reject ambiguous framing, conflicting length/transfer
metadata, duplicate security-sensitive headers or JSON members, trailing JSON,
invalid UTF-8, unsupported content encoding/media type/charset, excessive
nesting/collections/parts/fields and decompressed-size expansion before domain
or cryptographic work. JSON, form, multipart, SCIM, SAML, WebAuthn and callback
parsers MUST each have explicit raw and decoded limits and deterministic error
mapping; multipart temporary resources MUST be task-owned and removed on every
path.

Every mutating row MUST declare whether it is naturally idempotent,
key-idempotent, single-use or non-replayable. Key-idempotent requests MUST bind
a bounded key to tenant, authenticated principal, operation and canonical
request digest; concurrent duplicates return the same committed result,
mismatched reuse is rejected, unknown outcomes remain unknown, and expiry does
not permit an unsafe replay. One-time credentials and protocol state MUST use
their owning capability semantics rather than generic response caching.

Client-initiated routes MUST apply the configured default or stricter
endpoint/extension rate rule through the existing `rate-limit` primitive.
Client identity MUST come only from the trusted-network-facts resolver,
normalize IPv4-mapped IPv6, and use the full canonical RFC 5952 IPv6 address
without subnet aggregation in the selected reference profile. Aggregation MAY
exist only in a future, separately selected profile. Denial MUST return stable
retry metadata. Direct in-process API calls MUST have an explicit rate policy
rather than bypassing controls accidentally.

Trusted proxy, forwarded-header, host, scheme, origin, CORS and redirect policy
MUST be fail-closed and resistant to spoofing, IDN confusion and open redirects.
Tenant resolution MUST consume only `tenant.host_mapping`: one exact canonical
effective host maps to either one tenant or the public realm, never both.
Unknown, overlapping, wildcard, trailing-dot or request-supplied tenant/realm
selection MUST be rejected before tenant-scoped state access.
Security headers MUST account for OAuth popups/One Tap without globally
weakening isolation. Middleware and hooks MUST have documented order,
cancellation and error behavior and MUST NOT run under package locks or open
transactions. Authentication MUST remain distinct from authorization on every
privileged route.

CORS MUST be route- and credential-mode-specific: exact normalized origins,
methods and headers only; no reflected or wildcard credentialed origins;
preflight must not authenticate or mutate; denials expose no sensitive detail;
and `Vary` and cache behavior must prevent cross-origin reuse. The trusted-
network resolver MUST accept forwarded host/scheme/client facts only from
configured proxy hops, parse one documented forwarding syntax, reject malformed
or ambiguous chains, and never combine trusted and untrusted values. External
base URL, issuer, callback and redirect construction MUST use that resolved
value, not arbitrary request headers.

The server and every provider/store call MUST derive bounded contexts. Public
configuration MUST require positive `http.read_header_timeout`,
`http.read_timeout`, `http.write_timeout`, `http.idle_timeout`,
`http.handler_timeout`, `http.shutdown_drain` and
`http.external_total_timeout` values within reviewed bounds. Provider
connection and TLS deadlines MUST additionally use
`http.external_connect_timeout` and `http.external_tls_handshake_timeout`.
Timeouts and cancellation MUST map to stable errors without continuing
detached work, leaking a body/resource, committing an unreported transition or
revealing whether an identifier exists.
`http.handler_timeout` MUST bound the complete handler, including domain calls,
hooks, stores, providers and response streaming; it MUST NOT extend an inbound
deadline or exceed `http.write_timeout`. Cancellation alone MUST NOT be treated
as proof that an authoritative mutation rolled back.

## OpenAPI and lifecycle

The generated OpenAPI 3.1.1 document MUST contain every registered operation,
request/response/error schema, security scheme, callback where representable,
examples and feature metadata; every operation MUST map to exactly one handler.
Schema validation and a generated-client smoke test are REQUIRED.

The exact validated document and canonical-operation coverage report MUST be
exportable through a deterministic side-effect-free public API and an explicit
HTTP endpoint that is disabled by default or protected by deployment policy.
Export MUST NOT include runtime secrets, internal hosts or provider
credentials. Schema, handler and manifest input must share one immutable
snapshot so concurrent extension composition cannot produce mismatched output.

The composed handler/server MUST start and stop cleanly with explicit contexts,
expose safe health/readiness hooks supplied by composition, and contain no
hidden singleton state or development bypasses. Examples MUST inject public
feature contracts through the same constructor and route set proven by tests.
The transport MUST expose separate liveness and readiness probe handlers with
fixed bounded responses, methods, cache denial and content types. Liveness is
process-only; readiness delegates to the composed dependency/migration/key
snapshot, returns unavailable before startup and during drain, and exposes no
tenant, dependency address, migration detail, secret or provider response.
Their only routes are `GET /healthz` and `GET /readyz`; neither route may be
generated beneath `/v1` or registered under an alias.

Bearer issuance MUST implement `struct:ref.session.bearer_issuance` as two
distinct boundaries. The cookie-authenticated authorize route returns only a
reveal-once continuation under CSRF protection. The issue route accepts exactly
that continuation in bounded JSON, ignores ambient cookies, rejects request
`Authorization`, validates the exact bound Origin, and emits the bearer once in
the selected no-store JSON or exact CORS-exposed response headers. A transport
override, URL/query/cookie delivery, unknown outcome or replay emits no bearer.

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Acceptance and blockers

Contract fixtures and test implementations MUST exercise every handler through
real `httptest`/network HTTP semantics, including cookies, redirects,
concurrent requests, ambiguous domain outcomes, provider failures and
localized errors. `identity/reference` MUST rerun the complete `END_STATE.md`
journeys with selected real PostgreSQL/Valkey adapters. Hostile tests MUST cover CSRF, CORS,
host/proxy spoofing, request smuggling assumptions, SSRF inputs, open redirects,
route/schema collisions, oversized/decompression inputs, enumeration,
cross-tenant access and secret redaction. Browser evidence is REQUIRED for
cookie, redirect, popup/One Tap and WebAuthn ceremony profiles where HTTP-only
proof is insufficient.

Exact coverage/mutation, race/stress/leak, decoder fuzz, resource benchmarks,
clean-consumer, API baseline, docs/examples/changelog and complete security and
supply-chain gates MUST pass. The unit MUST remain unverified if any in-scope
operation requires consumer-written glue, handler/OpenAPI coverage differs,
security defaults are unsafe, feature composition is incomplete, or a required
HTTP/browser journey is unproved.
