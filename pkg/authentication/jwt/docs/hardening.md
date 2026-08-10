# JWT hardening matrix

This matrix records the validation policy owned by this module. It is based on
[RFC 7515](https://www.rfc-editor.org/rfc/rfc7515),
[RFC 7517](https://www.rfc-editor.org/rfc/rfc7517),
[RFC 7518](https://www.rfc-editor.org/rfc/rfc7518),
[RFC 7519](https://www.rfc-editor.org/rfc/rfc7519),
[RFC 8259](https://www.rfc-editor.org/rfc/rfc8259), and the JWT best current
practices in [RFC 8725](https://www.rfc-editor.org/rfc/rfc8725). The current
[IANA JOSE registry](https://www.iana.org/assignments/jose/jose.xhtml) remains
the algorithm-status authority. RFC 7520 vectors are pinned to
`ietf-jose/cookbook` commit `13692b68bfc18b99557a5b1ed311fd5077bfff04`.

## Algorithm and key matrix

| Algorithms | Required JWK | Additional policy |
| --- | --- | --- |
| `HS256`, `HS384`, `HS512` | `oct` | Explicit `alg`; at least 32, 48, or 64 key bytes respectively. |
| `RS256`, `RS384`, `RS512` | RSA public key | Explicit `alg`; modulus from 2048 through 8192 bits. |
| `PS256`, `PS384`, `PS512` | RSA public key | Explicit `alg`; modulus from 2048 through 8192 bits. |
| `ES256`, `ES384`, `ES512` | EC public key | Curve must be P-256, P-384, or P-521 respectively. |
| `Ed25519` | Ed25519 OKP public key | Generic deprecated `EdDSA` is rejected. |

Every key needs a unique non-empty `kid` and an algorithm in the validator's
explicit allowlist. `use`, when present, is `sig`; `key_ops`, when present,
contains `verify`. Asymmetric private keys, `none`, `ES256K`, unknown or
deprecated algorithms, algorithm/key-type confusion, weak HMAC keys, wrong EC
curves, and RSA moduli outside the work bound are invalid configuration. The
validator does not infer an algorithm from a key.

## Header, serialization, and claim matrix

| Boundary | Accepted | Rejected |
| --- | --- | --- |
| Serialization | Three non-empty compact JWS segments with strict unpadded base64url. | JWE, JSON serialization, nested compact payloads, truncation, padding, non-zero pad bits, or trailing data. |
| Protected header | Unique `alg` and `kid` strings selecting configured trust. | Duplicate members, `crit`, `jku`, `jwk`, `x5u`, `x5c`, `x5t`, `x5t#S256`, or a disallowed algorithm/key. |
| JSON | UTF-8 object with unique members, bounded depth, members, collections, numbers, and total token bytes. | Invalid UTF-8, unpaired UTF-16 escapes, duplicate members, excessive depth or collections, numbers over 128 encoded bytes, or a non-object payload. |
| Identity | Non-empty exact issuer, configured audience, subject allowed by the optional exact allowlist, issued-at time, expiration, and configured custom required claims. | Missing or malformed required claims, subject-policy failure, issuer/audience mismatch, future `iat`, expired token, or premature `nbf`. |
| Time boundary | `nbf <= now + skew`; `iat <= now + skew`; `now < exp + skew`. | Equality at `exp` without skew and values outside configured non-negative skew. |
| Private claims | Bounded JSON values copied into an immutable principal; configured scope and tenant shapes are strings or string arrays. | Unsupported values, empty tenant entries, or principal bounds exceeded. |

Token-provided key URLs and embedded keys are never dereferenced or used. A
deployment can additionally separate token kinds with distinct issuers,
audiences, algorithms, keys, or a higher-level type policy.

## Remote-key and error matrix

| Condition | Result and ownership |
| --- | --- |
| HTTPS fetch | One configured URL; redirects and compressed responses are rejected. Plain HTTP requires the test/development option. |
| Response limits | 1 MiB body, 32 KiB aggregate headers, 64 keys, strict JWK JSON, and a bounded initialization timeout by default. |
| DNS, TLS, timeout, cancellation, partial body, invalid JSON, key collision, or oversized response | `ErrAuthenticationUnavailable`; response bodies are closed and public error text is redacted. |
| Concurrent refresh | Automatic and explicit work uses one hardened serialized transport; overlapping explicit callers share one result. At most 128 provider operations are admitted. |
| Fleet refresh | Each provider instance applies independent 10 percent bounded jitter to cache refresh timing; zero jitter can be configured explicitly. |
| Successful rotation | The complete validated set atomically replaces the cached set. Returned sets are deep copies. |
| Refresh outage | `Refresh` reports unavailable while the last successful cached set remains usable for known keys. |
| Close | New work is rejected, admitted operations are canceled and joined, and cache goroutines are shut down. A canceled close can be retried. |

The configured URL is trusted application configuration, not token input.
Private-address or DNS-rebinding policy belongs in a supplied `http.Client`
transport because some deployments intentionally use private issuers.

Credential-shape failures use `ErrCredentialsInvalid`; signature, claim, or
trust-policy failures use `ErrCredentialsRejected`; provider, cancellation,
capacity, and lifecycle failures use `ErrAuthenticationUnavailable`.
Configuration and key-policy failures use `ErrInvalidConfiguration`.

## Executable evidence map

- RFC 7515 Appendix A.2, RFC 7520, the complete supported algorithm/key matrix,
  and bidirectional golang-jwt interoperability across HMAC, RSA, PSS, and ECDSA:
  `interoperability_test.go`.
- Duplicate members, malformed base64url, invalid Unicode, huge numbers,
  truncation, critical/key-reference headers, clock edges, and nested payloads:
  `validator_test.go` and `hardening_internal_test.go`.
- DNS, TLS, redirect, timeout, cancellation, partial/oversized/compressed body,
  collision, rotation, stale cache, outage, refresh herd, fleet jitter, close,
  and caller ownership: `remote_test.go` and `remote_internal_test.go`.
- Parser and remote-response fuzz boundaries: `fuzz_test.go`.
- Differential acceptance and deliberately stricter rejection against
  golang-jwt v5: `interoperability_test.go`.
- Local, cached-remote, rotation-miss, hostile-input, allocation, and
  contention benchmarks: `validator_benchmark_test.go`.
