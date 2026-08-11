# Goal: pkg/identity/http

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/http`
- Canonical module: `pkg/identity/http`
- Canonical goal after scaffolding: `pkg/identity/http/.ai/GOAL.md`
- Requires: every feature/protocol contract listed explicitly in `INVENTORY.md`
- Consumes existing primitives: `authentication`, `authorization`, `openapi`, `audit`, `rate-limit`, `telemetry`
- Unlocks after verification: `identity/reference`

The full explicit prerequisite list in `INVENTORY.md` is authoritative and
MUST be rendered into the worker assignment. This transport unit MUST not
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
  shared-parent-domain cookies MUST not enable wildcard trust.

## Endpoint acceptance manifest

The module MUST maintain one machine-readable manifest that maps every
in-scope parity operation to HTTP method, path, authentication, authorization,
CSRF mode, request limit, idempotency, success/error types, handler owner and
OpenAPI operation ID. Construction and CI MUST fail when a parity operation is
missing, duplicated, undocumented, registered without authorization policy or
documented without a handler. The manifest is an API contract, not a
source-text substitute for behavioral tests.

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
endpoints MUST not rely on cookie CSRF assumptions.

Client-initiated routes MUST apply the configured default or stricter
endpoint/extension rate rule through the existing `rate-limit` primitive.
Client identity MUST come only from the trusted-network-facts resolver,
normalize IPv4-mapped IPv6 and aggregate IPv6 by configured prefix. Denial MUST
return stable retry metadata. Direct in-process API calls MUST have an explicit
rate policy rather than bypassing controls accidentally.

Trusted proxy, forwarded-header, host, scheme, origin, CORS and redirect policy
MUST be fail-closed and resistant to spoofing, IDN confusion and open redirects.
Security headers MUST account for OAuth popups/One Tap without globally
weakening isolation. Middleware and hooks MUST have documented order,
cancellation and error behavior and MUST not run under package locks or open
transactions. Authentication MUST remain distinct from authorization on every
privileged route.

## OpenAPI and lifecycle

The generated OpenAPI 3.1.1 document MUST contain every registered operation,
request/response/error schema, security scheme, callback where representable,
examples and feature metadata; every operation MUST map to exactly one handler.
Schema validation and a generated-client smoke test are REQUIRED.

The composed handler/server MUST start and stop cleanly with explicit contexts,
expose safe health/readiness hooks supplied by composition, and contain no
hidden singleton state or development bypasses. Examples MUST inject public
feature contracts through the same constructor and route set proven by tests.

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
