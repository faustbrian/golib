# Goal: pkg/identity/http

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/http`
- Canonical module: `pkg/identity/http`
- Canonical goal after scaffolding: `pkg/identity/http/.ai/GOAL.md`
- Requires: every preceding inventory unit except `identity/identitytest`
- Consumes existing primitives: `authentication`, `authorization`, `openapi`, `audit`, `rate-limit`, `telemetry`
- Unlocks after verification: `identity/identitytest`

The full explicit prerequisite list in `INVENTORY.md` is authoritative and
MUST be rendered into the worker assignment. This final composition unit MUST
not start while any feature, protocol, provider or selected persistence adapter
is unverified.

## Start gate

The worker MUST satisfy `../COMMON_REQUIREMENTS.md` and MUST start only after
the coordinator marks this unit `in-progress`, records this worker, and verifies
every explicit prerequisite in the inventory. The worker MUST reject a
summarized prerequisite list or any assignment based on a stale integration
commit.

## Objective

Build the standard-library `net/http` composition and reference server that
makes the complete identity platform usable without undocumented application
glue. It MUST expose every in-scope parity operation and execute every
applicable journey in `END_STATE.md` through public package contracts.

## Ownership boundary and public contract

This module owns HTTP route composition, handlers, request decoding and
limits, content negotiation, stable error envelopes, auth context bridging,
cookie policy, CSRF, CORS/trusted origins and proxies, redirect allowlists,
security/cache headers, hooks/middleware, idempotency surfaces, route and
operation-ID collision detection, OpenAPI 3.1.1 composition, and a complete
reference server assembly for PostgreSQL/Valkey/provider adapters. It does not
reimplement domain, protocol, persistence or provider behavior; render an
admin UI; introduce global registration; or hide configuration decisions.

The public API MUST define immutable route sets, explicit dependency bundle,
server lifecycle, base-path and origin policy, cookie/CSRF policy, request and
response limits, error mapper, middleware/hook phases, OpenAPI builder and
reference-server configuration. Missing dependencies, duplicate routes,
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
readiness limited to safe operational state. No in-scope parity row may be
omitted because an application could add a handler later.

## HTTP security and semantics

Every request MUST have bounded header, URL, body, multipart/form and decoded
field limits before allocation or cryptographic work. Accepted methods,
content types, status codes, cache behavior and stable error codes MUST be
specified. Cookie sessions MUST explicitly enforce `Secure`, `HttpOnly`,
`SameSite`, domain/path, prefix, partition/cross-site and rotation semantics.
State-changing cookie requests MUST have unbypassable CSRF binding. Bearer
endpoints MUST not rely on cookie CSRF assumptions.

Trusted proxy, forwarded-header, host, scheme, origin, CORS and redirect policy
MUST be fail-closed and resistant to spoofing, IDN confusion and open redirects.
Security headers MUST account for OAuth popups/One Tap without globally
weakening isolation. Middleware and hooks MUST have documented order,
cancellation and error behavior and MUST not run under package locks or open
transactions. Authentication MUST remain distinct from authorization on every
privileged route.

## OpenAPI and reference deployment

The generated OpenAPI 3.1.1 document MUST contain every registered operation,
request/response/error schema, security scheme, callback where representable,
examples and feature metadata; every operation MUST map to exactly one handler.
Schema validation and a generated-client smoke test are REQUIRED.

The reference server MUST start and stop cleanly with explicit contexts, apply
selected migrations, wire PostgreSQL and Valkey, provider adapters, delivery,
keys and policies, expose readiness without secrets, and document production
configuration/rotation. It MUST not contain hidden singleton state or
development bypasses. Examples MUST use the same public constructor and route
set proven by tests.

## Acceptance and blockers

Integration tests MUST execute all `END_STATE.md` journeys through real
`httptest`/network HTTP semantics and selected real PostgreSQL/Valkey profiles,
including cookies, redirects, concurrent requests, ambiguous commits,
provider failures and localized errors. Hostile tests MUST cover CSRF, CORS,
host/proxy spoofing, request smuggling assumptions, SSRF inputs, open redirects,
route/schema collisions, oversized/decompression inputs, enumeration,
cross-tenant access and secret redaction. Browser evidence is REQUIRED for
cookie, redirect, popup/One Tap and WebAuthn ceremony profiles where HTTP-only
proof is insufficient.

Exact coverage/mutation, race/stress/leak, decoder fuzz, resource benchmarks,
clean-consumer, API baseline, docs/examples/changelog and complete security and
supply-chain gates MUST pass. The unit MUST remain unverified if any in-scope
operation requires consumer-written glue, handler/OpenAPI coverage differs,
security defaults are unsafe, reference assembly is incomplete, or a required
journey/provider/infrastructure profile is unproved.
