# Security audit findings

The audit reviewed protocol parsing, secret handling, concurrency, remote key
lifecycles, dependency direction, interoperability, and resource bounds. Each
finding below includes its reproducer and final disposition.

| ID | Severity | Evidence and reproduction | Disposition |
| --- | --- | --- | --- |
| AUTH-001 | High | Concurrent unknown OIDC key IDs could queue behind an issuer fetch without a waiter bound; reproduced in `oidc/remote_concurrency_test.go`. | Fixed with one refresh owner, bounded waiters, cancellation-aware waits, cooldown, and min/max freshness. |
| AUTH-002 | High | `jwt.Remote.Close` could race an admitted refresh and deadlock shutdown; reproduced by deadline and state tests in `jwt/remote_test.go` and `jwt/remote_internal_test.go`. | Fixed by owning, cancelling, and draining admitted operations before upstream shutdown. |
| AUTH-003 | High | A JWK selected by ID could carry metadata or key material inconsistent with the configured algorithm; reproduced in JWT and OIDC validator tests. | Fixed with key-family, curve, validation, `use`, and `key_ops` enforcement. |
| AUTH-004 | Medium | OIDC expiry used upstream timing while other dates used local policy, producing inconsistent skew; reproduced by numeric-date tests. | Fixed by enforcing `exp`, `nbf`, `iat`, and `auth_time` once through the configured clock and skew. |
| AUTH-005 | Medium | Basic user/password inputs containing control bytes were accepted; reproduced by extractor and static-authenticator tests. | Fixed according to RFC 7617 before storage or authentication. |
| AUTH-006 | Medium | Challenge parameters lacked explicit count/size bounds and accepted control bytes; reproduced in `authhttp/challenge_test.go`. | Fixed with exported limits and strict quoted-text validation. |
| AUTH-007 | Medium | An OIDC issuer URL could include a query component; reproduced in OIDC configuration tests. | Fixed by rejecting issuer queries and fragments per OIDC Discovery. |
| AUTH-008 | Medium | Explicit query credential sources can expose secrets to infrastructure before extraction. The library has no URL-writing API, so no source-level output leak was reproduced. | Residual deployment risk documented; query constructors retained for compatibility and deprecated for new designs. |
| AUTH-009 | Low | Hostile header and challenge fields were not directly fuzzed. | Fixed with bounded fuzz targets for complete header sets and challenge formatting. |

## Rejected boundary findings

- Authorization is intentionally absent: the authenticated principal is an
  input to a separate policy layer, not evidence that an action is allowed.
- The root module permits only the dependency-free `clock` capability
  module. Optional modules do not import `service`, `http-client`, or
  `authorization`; the executable boundary check prevents this repository
  from closing a dependency cycle.
- OIDC owns no background goroutine. JWT remote resources expose an explicit,
  idempotent, deadline-aware `Close` and reject new work after closing begins.

No identified source-level security blocker remains. Release readiness still
depends on the local gates listed in [test-matrices.md](test-matrices.md).
