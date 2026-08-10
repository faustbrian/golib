# JWT validation

`jwt` validates signed compact JWTs at the authentication boundary. It owns
JWT/JWS parsing, signature and claim policy, static JWK sets, and a bounded
remote JWKS cache. It does not extract HTTP credentials, discover OIDC
providers, issue tokens, manage sessions, or make authorization decisions.

## Quick start

Construct a `Validator` with one issuer, one audience, an explicit algorithm
allowlist, a deterministic `clock.Clock`, and exactly one key source:

```go
validator, err := jwt.New(jwt.Config{
	Issuer:     "https://issuer.example.com",
	Audience:   "orders",
	Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()},
	KeySet:     keys,
	Clock:      clock,
})
if err != nil {
	return err
}

principal, err := validator.ValidateBearer(ctx, compactToken)
```

The compiling package example shows a complete signed-token setup.

## Validation policy

Every validator requires `iss`, `aud`, `sub`, `iat`, and `exp`. The subject,
issuer, and audience must be non-empty; issuer and audience must match the
configured values. `Subjects` optionally restricts subjects to an exact
allowlist. `RequiredClaims` adds deployment-specific required claims to the
mandatory registered set. `exp`, `nbf`, and `iat` must be JSON numbers and are
checked against the configured clock with the configured non-negative `Skew`.

Tokens must use compact JWS serialization with exactly three non-empty,
unpadded base64url segments. The protected header must contain non-empty `alg`
and `kid` string values. The algorithm must be in the configured allowlist and
must match the selected JWK's declared algorithm and key type. `none`,
deprecated algorithms, unknown algorithms, duplicate JSON members, invalid
UTF-8, malformed JSON, duplicate key IDs, unknown critical headers, and JSON or
nested serialization are rejected. Token-provided key references (`jku`,
`jwk`, and `x5*`) are rejected. Unpaired UTF-16 escapes and excessively long
JSON numbers are rejected before claim decoding.

`MaxTokenBytes`, `MaxClaims`, `MaxClaimDepth`, and `MaxKeys` bound hostile
inputs. Zero values select conservative defaults. Negative values and values
above the authentication package's shared claim limits are invalid
configuration. `MaxClaims` must reserve capacity for all five mandatory claims
and every configured `RequiredClaims` entry.

## Algorithms and keys

The validator can allow the non-deprecated signature algorithms registered by
the pinned JWX version in these key families:

- `HS256`, `HS384`, and `HS512` with `oct` keys;
- `RS256`, `RS384`, and `RS512` with RSA keys;
- `PS256`, `PS384`, and `PS512` with RSA keys;
- `ES256`, `ES384`, and `ES512` with EC keys;
- `Ed25519` with OKP keys.

HMAC keys must contain at least 32, 48, or 64 bytes for HS256, HS384, or HS512.
RSA keys must be public and have a modulus from 2048 through 8192 bits. EC keys
must be public and use the algorithm's exact curve. `none`, deprecated generic
`EdDSA`, `ES256K`, and algorithms outside the listed families are rejected.

Only algorithms explicitly supplied in `Config.Algorithms` are accepted. JWKs
must have unique non-empty `kid` values and an `alg` in that allowlist. If
present, `use` must be `sig`, and `key_ops` must include `verify`. Keep HMAC
secrets separate from asymmetric public-key material; the key-type checks are
an additional defense against algorithm confusion, not a substitute for key
separation.

## Local and remote key providers

Use `Config.KeySet` for a local set or `Config.Provider` for a narrow dynamic
provider. Supplying both or neither is invalid. Sets are copied and validated
before use, so callers retain ownership and cannot mutate the validator's
static trust state.

`NewRemote` accepts HTTPS by default, performs an initial bounded fetch, and
then owns a JWX cache for that exact URL. The URL must not contain user info or
a fragment. All redirects and compressed responses are denied. Response
bodies, aggregate headers, key count, concurrent operations, and initialization
time are bounded. `Cache-Control: max-age` and `Expires` determine refresh time
within the configured minimum and maximum intervals; absent or unusable cache
headers fall back to the minimum. `no-cache`, `no-store`, and
`must-revalidate` force the configured minimum interval, and `Age` reduces a
`max-age` lifetime before bounds are applied. Each provider applies independent
bounded refresh jitter (10 percent by default) to avoid synchronized fleet
load.

A successful refresh atomically replaces the cached set. A failed refresh
returns `ErrAuthenticationUnavailable` and retains the last successful set.
That fail-stale policy preserves validation for already-known keys during an
issuer outage; it never accepts an unknown key. Applications that require
fail-closed freshness must stop using or close the provider after their own
freshness deadline.

`Remote` is safe for concurrent validation and refresh. Automatic and explicit
refreshes use the same hardened client and never overlap remote work;
overlapping explicit refreshes share one in-process result. Returned sets are
deep copies. Canceling a refresh waiter stops that caller waiting. The remote
request admitted by the cache continues under the provider lifecycle until it
finishes or `Close` cancels the provider. Canceling the context passed to a
successful `NewRemote` does not close the provider. `Close` rejects new work,
cancels in-flight provider operations, waits for them to leave the provider,
and shuts down cache-owned goroutines. A canceled close stops that caller
waiting and reports the context error; if shutdown did not finish, a later
`Close` may retry. The caller that creates a `Remote` owns it and must close it.

## Results and errors

`ValidateBearer` returns an immutable authentication principal. Registered
claims and configured scope/tenant claims populate typed principal fields;
remaining private claims are defensively copied into `Principal.Claims`.
`Authenticate` additionally accepts only `authentication.BearerCredential`.

Failures use stable authentication categories:

- malformed or oversized credentials: `ErrCredentialsInvalid`;
- failed signature, claims, or trust policy: `ErrCredentialsRejected`;
- provider, cancellation, or lifecycle failures:
  `ErrAuthenticationUnavailable`.

Standards-backed parse and claim failures retain the safe JWX sentinels such as
`jwt.ParseError()`, `jwt.TokenExpiredError()`, and
`jwt.MissingRequiredClaimError()` in the error chain. Key-source failures use
the redacted `ErrKeyProviderUnavailable` sentinel. Raw provider, transport,
endpoint-query, key, token, signature, and claim values are not retained.

Use `errors.Is` and `errors.As`; do not match strings. Public error text is the
stable category and does not include token, signature, key, claim, endpoint, or
remote error text. Only safe standard categories and redacted provider causes
remain available through the standard error chain.

## Adoption and migration

Keep transport extraction in `authentication/authhttp` or another adapter and
pass only the bearer credential to this package. Keep OIDC discovery and ID
token policy in `authentication/oidc`. Migrate permissive JWT parsers by first
inventorying issuers, audiences, algorithms, key IDs, clock skew, and required
claims; then configure the narrowest observed policy and reject legacy tokens
that do not satisfy it.

The module is pre-v1. Public API compatibility is checked against
`api/baseline.txt`, but minor releases may still require an intentional
migration. The package follows the pinned JWX algorithm registry; review that
dependency and this algorithm list when upgrading it.

The RFC-derived acceptance, remote-boundary, and error matrix is recorded in
[`docs/hardening.md`](docs/hardening.md).
The complete exported surface and defaults are recorded in
[`docs/api.md`](docs/api.md).

## Security notes and tradeoffs

- Treat tokens, JWKs, provider URLs, and wrapped causes as secrets in logs and
  telemetry.
- Prefer asymmetric verification for independently operated issuers. HMAC
  requires every verifier to possess signing-capable secret material.
- Keep leeway as small as operational clock accuracy permits.
- A larger token, claim, depth, key, body, or refresh bound increases resource
  exposure.
- Remote caching improves availability and rotation behavior but introduces a
  deliberate fail-stale window after the last successful refresh.

## FAQ

**Does this package issue tokens or perform authorization?** No. It only
validates authentication evidence and constructs a principal.

**Does an unknown `kid` trigger an unbounded fetch?** No. Validation uses the
current bounded provider snapshot. Refresh is controlled by the cache schedule
or an explicit bounded `Refresh` call.

**Can HTTP be enabled for JWKS?** `WithInsecureHTTP` exists for isolated tests
and trusted development networks. Production endpoints should use HTTPS.

**Can callers mutate returned key sets?** A provider owns its returned set;
the validator copies and validates it for each validation attempt. `Remote`
retains ownership of its cached set.

**What happens during a JWKS outage?** Refresh reports unavailable, while the
last successful key set remains usable according to the documented fail-stale
policy.
