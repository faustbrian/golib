# Security and hardening review

## Acceptance boundary

The RFC 9421 parsers accept only `Signature-Input`, `Signature`, and
`Accept-Signature` Structured Fields. They do not inspect `Authorization`,
translate `(request-target)`, or recognize AWS SigV4, OAuth 1.0, Cavage draft,
or vendor syntax. The `compatibility` package is a separate application seam
whose caller supplies the external implementation. Its safe returned errors do
not wrap callback errors; only the explicit diagnostic callback receives the
original failure.

All untrusted Structured Fields decoding crosses narrow panic-containment
wrappers around the external parser. A dependency panic is converted to the
same typed malformed-input category as an ordinary parse error; semantic
validation outside that dependency boundary is not recovered.

## Cryptography and timing

- RSA-PSS, RSA PKCS #1 v1.5, ECDSA, and Ed25519 use Go standard-library
  implementations. RSA-PSS and ECDSA use Go-managed cryptographically secure
  randomness and never consume caller-configured readers. The package performs
  no custom private-key arithmetic.
- ECDSA keys are re-encoded and parsed through the current safe standard-library
  APIs before use. Malformed caller-built keys fail without exposing deprecated
  affine-coordinate operations or panics.
- HMAC signatures and digest bytes use `subtle.ConstantTimeCompare` after an
  explicit public length check. Algorithm, label, coverage, timestamp, and key
  selection are public policy inputs and are rejected before cryptographic use.
- Error values disclose a coarse category only. They do not render key IDs,
  nonce values, signature bases, signatures, bodies, key material, or backend
  failures. Resolver and replay causes are reduced to cancellation, deadline,
  or a generic backend category.
- Go does not guarantee key zeroization. `HMACKey` copies input material to
  prevent aliasing, but applications must still keep keys out of logs, dumps,
  and long-lived process state.

## Concurrency, faults, and lifetime

The core starts no goroutines. `goleak.VerifyTestMain` covers both production
packages. `make stress` repeatedly races atomic nonce consumption and
cancellation; `make soak` repeats deterministic signing, verification, and
expiry transitions; `make fault` selects injected reader, resolver, replay,
callback, body-limit, trailer, and transport failures. `make
lifecycle` runs shared-verifier rotation, revocation, cache-refresh race,
resolver outage, replay unknown-commit, cancellation, and shutdown scenarios
under the race detector. `make race` retains the complete race-detector gate.
Parser limits, body limits, replay capacity and TTL, resolver and replay
deadlines, validity intervals, and cache freshness are all mandatory
configuration rather than hidden defaults.

`VerificationProfile` values are immutable after construction and can be
shared. Caller-owned clocks, resolvers, replay stores, and resolved key objects
must also be safe for concurrent use. Resolver caches must publish complete
immutable key snapshots, set a bounded `FreshUntil`, surface revocation rather
than serving a cached success, and return an error for an unknown refresh
outcome. Cancellation of the caller context is the package shutdown boundary;
resolver and replay implementations must stop promptly when that context is
done.

## Golib composition boundaries

The implementation depends on standard-library contracts so integration does
not require service location or hidden registration:

| Consumer | Explicit composition |
| --- | --- |
| `http-client` | Install the signing/digest `RoundTripper` at the per-attempt transport boundary, inside logical retry and redirect policy. Recreate streaming requests for every attempt. |
| `http-middleware` | Convert `RequestVerificationMiddleware` or a body-verification middleware to the identical `func(http.Handler) http.Handler` contract and give it a stable descriptor name. |
| `router` | Place the middleware in a `router.NamedMiddleware`; route policy chooses the profile and required coverage. |
| `service/serverhttp` | Convert to `serverhttp.Middleware` and order body limits, trusted proxy reconstruction, digest verification, signature verification, authentication, capability enforcement, and authorization explicitly. |
| `clock` | Assign the selected `clock.Clock.Now` method to profile and replay `Now` fields. No package clock is read implicitly. |
| secret stores | Implement only `SigningKeyProvider` or `KeyResolver`, perform the bounded store operation under the supplied context, and map versions to key validity, rotation, revocation, and freshness fields. |
| `audit`, correlation, tenancy, telemetry | Read correlation and tenant values from the supplied context inside application resolver, replay, error-mapping, and audit callbacks. Record only safe failure categories. |
| `capability` | Run capability verification after signature verification. A valid message signature is never converted into a grant or authorization result. |

These are direct, executable Go interface conversions. The core deliberately
does not import the consumer packages, own their lifecycle, or create a cyclic
dependency graph.

## Proxy and HTTP transformation review

Forwarded headers are never read. Applications behind a proxy must require and
supply `ExternalRequestContext` from a separately trusted boundary. Tests cover
origin, absolute, authority, and asterisk targets; escaped paths and queries;
default and non-default ports; contradictory external context; combined fields;
trailers; content coding boundaries; and equivalent HTTP/1.1, HTTP/2, and
HTTP/3 information visible through `net/http`. Transformations outside that
visible model must be included in deployment interoperability fixtures.
