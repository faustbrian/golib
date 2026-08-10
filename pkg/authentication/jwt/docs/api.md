# API reference

## Validator

`New(Config)` creates an immutable validator. Configuration requires one
issuer, one audience, a non-empty explicit algorithm allowlist, a non-nil
deterministic clock, and exactly one of `KeySet` or `Provider`.

`Config.Subjects` is an optional exact allowlist. A nil or empty list accepts
every non-empty subject. `Config.RequiredClaims` adds unique non-empty custom
claim names; the mandatory `iss`, `aud`, `sub`, `iat`, and `exp` names must not
be repeated. Both slices are defensively copied.

`MaxTokenBytes`, `MaxClaims`, `MaxClaimDepth`, and `MaxKeys` default to 16 KiB,
the authentication package claim limits, and 64 keys respectively. `Skew`
defaults to zero and cannot be negative. `MaxClaims` must accommodate the five
mandatory claims plus every custom required claim. `ScopeClaim` and
`TenantClaim` default to `scope` and `tenant` and must be distinct.

`ValidateBearer(context.Context, string)` validates one compact JWT and returns
an immutable `authentication.Principal`. `Authenticate` provides the same
behavior through the `authentication.Authenticator` contract and accepts only
`authentication.BearerCredential`.

`KeyProvider` exposes one context-aware `KeySet` operation. `KeyProviderFunc`
adapts a function to that contract. Provider sets are copied and revalidated
for each validation attempt.

## Remote provider

`NewRemote` performs a bounded initial fetch, starts a provider-owned cache,
and returns a `KeyProvider`. The caller must call `Close`.

- `WithHTTPClient` supplies the transport policy. The client is shallow-copied
  and then hardened with no redirects, no compression, exact-URL access, and
  configured response limits.
- `WithInsecureHTTP` permits plain HTTP for isolated development and tests.
- `WithRefreshBounds` sets positive minimum and maximum cache intervals.
- `WithRefreshJitter` sets a fraction in `[0, 1)`; the default is 10 percent.
- `WithMaxJWKBodyBytes` defaults to 1 MiB.
- `WithMaxJWKHeaderBytes` defaults to 32 KiB aggregate header bytes.
- `WithMaxJWKKeys` defaults to 64 keys.
- `WithInitializationTimeout` defaults to 10 seconds.

`KeySet` returns a deep copy of the current cached set. `Refresh` requests a
synchronous refresh; overlapping explicit refreshes share a result and all
automatic and explicit HTTP work is serialized. Caller cancellation stops
waiting. `Close` rejects new operations, cancels admitted operations, joins
them, and shuts down cache work. A canceled close may be retried.

## Errors

All failures support `errors.Is` and `errors.As` through
`authentication.Failure`. Credential shape, rejection, and provider failures
retain the authentication package categories. Safe JWX parse and validation
sentinels remain available for standards-backed decisions.
`ErrKeyProviderUnavailable` replaces raw provider and remote causes so the
error chain does not retain token, key, endpoint-query, or remote response
data.
