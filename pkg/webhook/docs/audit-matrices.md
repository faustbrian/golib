# Audit matrices

## Signature and interoperability

| Scheme | Canonical inputs | Rotation | Independent evidence | Negative evidence |
| --- | --- | --- | --- | --- |
| Generic v1 HMAC-SHA-256 | algorithm, timestamp, nonce, key ID, exact method, escaped path, sorted query keys with bound duplicate-value order, lowercase host, content type, idempotency key, SHA-256 body digest, metadata | all active bounded keys, newest first | Python standard-library vector `hmac-sha256-python` | component mutation, wrong secret, malformed header, fuzz corpus |
| Generic v1 HMAC-SHA-512 | same v1 canonical grammar | same | Python standard-library vector `hmac-sha512-python` | component mutation, wrong algorithm, malformed header, fuzz corpus |
| Vendor presets | none claimed | not applicable | none | absence is enforced by the provider matrix |

The normative local specification is [signatures.md](signatures.md). Primitive
behavior follows Go's [`crypto/hmac`](https://pkg.go.dev/crypto/hmac),
[`crypto/sha256`](https://pkg.go.dev/crypto/sha256), and
[`crypto/sha512`](https://pkg.go.dev/crypto/sha512) contracts. The independent
generator is `scripts/check_interoperability.py`; it does not import this Go
module.

## HTTP and body boundary

| Input | Expected disposition | Executable evidence |
| --- | --- | --- |
| exact, empty, chunked, compressed, or one-byte partial reads | authenticate the exact current body bytes without transformation | `body_test.go`, `http_test.go` |
| declared or streamed oversized body | reject before unbounded allocation | `TestCaptureBodyRejectsDeclaredOversizeBeforeReading`, `TestCaptureBodyBoundsUnknownLengthBeforeAllocation` |
| prior middleware consumption | authenticate only remaining bytes; deployment must put verification first | `TestCaptureBodyAfterPriorReadAuthenticatesOnlyRemainingBytes` |
| malformed, duplicate, combined, padded, or oversized signature fields | deterministic rejection | `http_test.go`, `core_errors_test.go`, header fuzzer |
| timestamp at inclusive skew boundary | accept; reject outside boundary | `TestVerifierTimestampToleranceBoundaries` |

## Replay and delivery

| Boundary | Guarantee | Executable evidence |
| --- | --- | --- |
| concurrent identical replay checks | exactly one atomic acceptance | 64-way `TestVerifyAndRecordAtomicallyRejectsReplay` under `-race` |
| replay backend unavailable | fail closed | replay and idempotency adapter fault tests |
| ambiguous outbound receipt | retry only with explicit idempotency key | delivery retry tests |
| queue/outbox relay | exactly one HTTP attempt per durable claim | adapter tests and `ExampleAdapter_Enqueue` |
| client timeout/cancellation | bounded exhausted or canceled classification | `TestDeliverClassifiesHTTPClientTimeoutAsExhaustedTransport` and cancellation tests |

## SSRF

| Vector | Default result | Evidence |
| --- | --- | --- |
| HTTP, userinfo, fragment, noncanonical or non-ASCII host | denied | `TestSSRFPolicyRejectsUnsafeURLsAndAddresses` |
| private, loopback, link-local, multicast, unspecified, mapped IPv4, metadata, documentation, benchmark ranges | denied | controlled literal and resolver tests |
| mixed public/private or oversized DNS answer | whole answer denied | `TestSSRFPolicyRejectsMixedAndOversizedDNSAnswers` |
| DNS answer changes between validation and dial | re-resolved and denied | `TestSecureHTTPClientRevalidatesDNSAtDialTime` |
| redirect to another target | returned without following | `TestSecureHTTPClientRejectsRedirectWithoutContactingTarget` |
| environment proxy | disabled on secure transport | secure transport configuration test |
| explicit private allow prefix | allowed only when configured; explicit deny wins | prefix policy tests |

URL parsing and resolution behavior follows Go's
[`net/url`](https://pkg.go.dev/net/url), [`net/netip`](https://pkg.go.dev/net/netip),
and [`net/http`](https://pkg.go.dev/net/http) contracts. HTTP retry-date parsing
follows [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html).
