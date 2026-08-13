# Key Lifecycle Evidence

Observed at `2026-08-13T09:24:31Z` on `darwin/arm64` with Go `1.26.5`.

## Executed Proof

A bounded key-lifecycle campaign ran selected public package tests under the Go
race detector with the repository workspace and cold task-owned Go caches:

- bearer and API-key authenticators atomically replaced bounded active key
  sets while concurrent authentication and rotation remained race-free;
- remote JWT verification accepted a rotated JWK and retained bounded behavior
  when the issuer became unavailable;
- OIDC discovery and verification refreshed on rotation misses, rejected a
  retired-key rollback, kept a still-fresh known key during a rotation-probe
  outage, failed closed after cache expiry, and synchronized concurrent
  authentication with rotation;
- capability verification accepted an intentional old/new overlap, rejected
  the old key after removal, rejected explicit revocation, preserved the new
  key during compromise response, checked every revocation boundary, and
  failed closed when the revocation store was unavailable or canceled;
- HTTP-signature verification rejected a retired key after rotation, accepted
  the replacement, and rejected explicit revocation; and
- API cursor decoding accepted old and new keys during overlap, rejected the
  retired key after removal, and remained race-free during rotation.

All fifteen selected tests passed under `-race`. The task-owned Go caches were
removed after the campaign.

## Claim Boundary

This proves consistent local key overlap, refresh, retirement, revocation,
rollback rejection, outage behavior, and concurrent rotation across the
selected packages. It does not prove live identity-provider, secret-store, KMS,
or credential rotation; production key generation, custody, distribution, and
destruction; compromised-host response; token population drain; ECS rolling
deployment; operator authorization; or incident timing. The associated
operational-assurance scenarios remain pending.
