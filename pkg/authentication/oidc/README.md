# OIDC ID-token validation

`oidc` discovers an OpenID Provider and validates signed OpenID Connect ID
tokens. It owns the authentication trust boundary only: applications retain
OAuth authorization flows, redirects, sessions, cookies, middleware, nonce
storage, and authorization policy.

The module implements the OpenID Connect Core 1.0 ID-token validation rules and
OpenID Connect Discovery 1.0 provider metadata rules. It supports the
authorization-code, implicit, and hybrid ID-token profiles with asymmetric
`RS*`, `PS*`, `ES*`, and `EdDSA` signatures. The provider MUST advertise every
configured algorithm and MUST advertise `RS256`, as required by Discovery.
Symmetric client-secret signatures, encrypted ID tokens, distributed claims,
OAuth flow execution, UserInfo, logout, dynamic registration, and access-token
validation are deliberately excluded.

## Install

```sh
go get github.com/faustbrian/golib/pkg/authentication/oidc
```

## Setup

```go
validator, err := oidc.New(ctx, oidc.Config{
	Issuer:     "https://accounts.example.com/tenant",
	ClientID:   "web-client",
	Algorithms: []string{"RS256"},
	Clock:      clock.System{},
	NonceValidator: oidc.NonceValidatorFunc(
		func(ctx context.Context, nonce string) error {
			return nonceStore.Consume(ctx, nonce)
		},
	),
})
if err != nil {
	return err
}

principal, err := validator.ValidateIDToken(ctx, rawIDToken, oidc.TokenBinding{
	AccessToken:       accessToken,
	AuthorizationCode: authorizationCode,
})
```

`New` reads
`<issuer>/.well-known/openid-configuration`, validates the returned issuer and
metadata, and eagerly fetches the initial JWKS. An issuer with a path follows
the Discovery append rule; the configured and returned issuer strings and the
token `iss` claim must match exactly.

`ValidateBearer` validates an ID token without front-channel hash inputs.
`ValidateIDToken` additionally validates `at_hash` and `c_hash` for each
non-empty `TokenBinding` field. Hash binding is supported for the `*256`,
`*384`, and `*512` families; an `EdDSA` token with a requested binding is
rejected because that algorithm name defines no OIDC half-hash selection.
`Authenticate` adapts the same validation to the parent authentication
package's bearer-credential contract.

## Configuration

Configuration is copied into a `Validator`; later changes to slices or to the
`Config` value do not change it. Caller-supplied collaborators (`Clock`,
`NonceValidator`, and an `HTTPClient` transport) must themselves be safe for
concurrent use and must not be mutated after construction.

Important options and defaults:

| Option | Meaning | Default |
| --- | --- | --- |
| `Issuer` | Exact provider issuer and discovery base | required |
| `ClientID` | Required audience and `azp` value | required |
| `TrustedAudiences` | Additional audience values allowed beside the client ID | none |
| `Algorithms` | Explicit asymmetric ID-token algorithm allowlist | required |
| `ClockSkew` | Symmetric temporal tolerance | 5 minutes |
| `MaxTokenBytes` | Compact-token size limit | 16 KiB |
| `MaxClaims`, `MaxClaimDepth` | JSON member and nesting limits | authentication package limits |
| `MaxHTTPBodyBytes` | Decompressed discovery/JWKS body limit | 1 MiB |
| `DiscoveryTimeout` | Initialization discovery/JWKS deadline | 10 seconds |
| `MaxKeys` | JWKS key-count limit | 64 |
| `MinRefreshInterval`, `MaxRefreshInterval` | Refresh and cache bounds | 1 minute / 1 hour |
| `MaxRefreshWaiters` | Concurrent callers admitted to a refresh | 64 |

Configuration also enforces hard ceilings of 1 MiB per token, 16 MiB per HTTP
body, 4,096 keys, 4,096 refresh waiters, five minutes for initialization, and
24 hours for refresh intervals. A supplied HTTP client may shorten the request
timeout but cannot extend the module's 30-second per-request ceiling.

HTTPS is required for the issuer and provider endpoints. `InsecureHTTP` is an
explicit whole-provider exception intended only for loopback development or a
similarly isolated test provider; it is not a production downgrade switch.

## Nonce ownership

Nonce generation and replay storage remain caller-owned. Supplying a
`NonceValidator` means every accepted token is passed to that callback after
signature and token-hash validation. The callback receives the validation
context and the raw nonce, and should atomically consume a single-use value.
An empty, unknown, expired, replayed, or otherwise invalid nonce should return
an error. The validator does not retain the nonce. Callback errors and panics
are converted to a credential rejection and their text is not exposed.

## Validation policy

Validation requires:

- a permitted signature algorithm and a matching public JWK;
- RSA public keys between 2,048 and 8,192 bits, or the exact curve/key shape
  required by the configured ECDSA or EdDSA algorithm;
- exact issuer equality;
- a non-empty subject;
- the client ID in `aud`, no duplicate audience, and no audience outside
  `ClientID` plus `TrustedAudiences`;
- `azp` equal to the client ID whenever present, and present for multiple
  audiences;
- required `iat` and `exp`, with optional `nbf` and `auth_time` checked using
  the configured clock and skew;
- the caller-owned nonce check when configured; and
- `at_hash` and `c_hash` when their corresponding binding values are supplied.

Registered protocol claims, configured scope claims, and configured tenant
claims are not copied into the principal's arbitrary claim map. Scope and
tenant values are exposed through the principal's typed accessors.

## Discovery, cache, and rotation

Initialization is synchronous and fails closed unless both valid provider
metadata and a valid non-empty JWKS are available. Redirects are disabled.
Response bodies are bounded after transport decompression, HTTP requests have
a client timeout, and the initialization context has its own deadline.

Metadata and keys share one synchronized refresh. Once the cached freshness
deadline is reached, one admitted caller re-discovers metadata and conditionally
fetches the current JWKS; other admitted callers wait with their own contexts.
Provider cache headers are clamped to the configured refresh bounds. Positive
freshness windows are refreshed early with per-instance jitter to spread fleet
load. After the minimum refresh interval, an unknown key ID triggers one
synchronized rotation probe even while known keys remain fresh. A rotated
`jwks_uri` clears validators associated with the former URL.

Known keys remain usable only while the cache is fresh. A provider outage after
expiry fails closed, including for a formerly known key. The failed refresh is
cached until the minimum refresh interval, preventing a local retry storm; no
provider response text is surfaced. The module starts no goroutines and owns no
closeable lifetime beyond each synchronous call.

## Concurrency, cancellation, and errors

`Validator` is immutable after construction and safe for concurrent use.
Construction and every validation operation accept `context.Context`.
Cancellation interrupts discovery, JWKS retrieval, refresh ownership, refresh
waiting, and nonce validation where the collaborator honors the context.

Errors use the parent authentication package's stable classifications:
malformed or missing credentials are invalid, cryptographic or claim failures
are rejected, and provider/refresh/cancellation failures are unavailable.
Errors never include tokens, claims, nonce values, keys, credentials, or
arbitrary provider bodies. Applications must preserve that property in their
own logs and callbacks.

## Adoption and compatibility

The module is pre-v1. Pin a reviewed version, configure an explicit algorithm
set, enable nonce consumption for browser flows, and exercise provider metadata
and key rotation before rollout. `NewWithKeySet` supports callers that already
own standards-compliant key retrieval; those callers also own all key-cache,
rotation, outage, and transport policy.

When migrating from a permissive verifier, review exact issuer spelling,
additional audiences, `azp`, required `iat`, provider-advertised algorithms,
HTTPS endpoints, and nonce behavior. Tokens previously accepted through issuer
aliases, untrusted extra audiences, stale keys, or omitted nonce checks may be
rejected intentionally.

## FAQ

**Does this start an authorization request or exchange a code?** No. Supply the
resulting ID token and, when available, its access token and authorization code.

**Does this validate an access token?** No. An access token is accepted only as
an opaque input for `at_hash` binding.

**Can stale keys be allowed during an outage?** Only until their cache freshness
deadline. There is no serve-stale override.

**Can HTTP be enabled for one production endpoint?** No. `InsecureHTTP` applies
to the issuer and discovered endpoints and is intended for isolated development.

**Who deletes replayed nonces?** The caller's `NonceValidator`, atomically with
successful validation.
