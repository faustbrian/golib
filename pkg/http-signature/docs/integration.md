# Integration and middleware ordering

## Client requests

For buffered body signatures, compose transports from outermost to innermost:

1. application retry and redirect policy creates a fresh request;
2. `BufferedContentDigestRoundTripper` consumes and closes the body, enforces
   its limit, and creates a replayable clone;
3. `SigningRoundTripper` signs the already-present digest and representation
   metadata required by the profile;
4. telemetry records only safe categories and correlation identifiers;
5. the network transport sends the request.

Do not put signing outside digest generation: the resulting signature would
not cover the generated digest. Do not reuse a request passed to a
`RoundTripper`; `net/http` transfers body ownership to the transport.

For streaming, `TrailerSigningRoundTripper` is placed immediately above the
network transport. It declares trailers before delegation, hashes returned body
bytes, and signs only at EOF. It intentionally has no `GetBody`. A retry,
redirect, hedge, or failover must create a new request and body and rerun the
adapter. An error after earlier reads can mean partial bytes reached the peer;
non-idempotent application retries therefore require an idempotency protocol.

## Server requests

Recommended order:

1. connection and header limits;
2. trusted external-origin reconstruction from deployment configuration;
3. bounded body/digest verification;
4. HTTP signature verification and atomic replay consumption;
5. authentication mapping;
6. authorization or capability enforcement;
7. application handler;
8. response digest and signing;
9. telemetry and audit redaction.

If the signature covers `Content-Digest`, digest verification must complete
before application content processing. Decompression ordering is a protocol
decision: RFC 9530 `Content-Digest` covers the coded HTTP content. Verify it
before decompression when `Content-Encoding` is present. `Repr-Digest` covers
selected representation data and cannot be inferred generically for partial or
status representations.

`RequestVerificationMiddleware` does not read the body. Pair it with a digest
middleware when the profile covers content. `BufferedTrailerVerificationMiddleware`
performs both operations after EOF because trailer values are unavailable
earlier.

## Responses

`ResponseSigningMiddleware` buffers the complete response before emitting
headers. The configured maximum is a hard failure boundary; no buffered partial
response is sent. The adapter intentionally does not expose `Flusher`,
`Hijacker`, or full-duplex behavior. Informational responses other than a final
101 are suppressed while the final response is buffered and signed.

`TrailerResponseSigningMiddleware` declares `Content-Digest`,
`Signature-Input`, and `Signature` trailers before the handler runs, hashes
actual emitted content, and finalizes the trailers after the handler returns.
It exposes the underlying writer through `Unwrap` for `http.ResponseController`.
Protocol switching is rejected because a 101 response cannot carry this
trailer contract.
The response is already in flight when a late body-limit, key, randomness, or
signing failure occurs, so the required `ReportError` callback must record the
failure and the connection path must preserve trailer loss as an incomplete
message. A recipient must wait for EOF and reject absent or invalid trailers;
it must never use the response merely because initial headers or body bytes
arrived.

For `HEAD` and status codes that do not permit content, digest calculation and
emission use the actual empty message content. A handler can still write a
`HEAD` representation to establish its metadata, matching `net/http`, but those
bytes are never emitted or included in `Content-Digest`.

`VerifyingRoundTripper` verifies response signatures against the exact related
request and leaves the body unread. On failure it closes the body. Verification
metadata is attached to a cloned `Response.Request` context.

`BufferedTrailerVerifyingRoundTripper` instead consumes and closes the bounded
response body so trailers are available, verifies the content digest before the
signature, and returns a replayable in-memory body only after both succeed. On
any failure it returns no response.

## Proxies and caches

Forwarded fields are untrusted input. Construct `ExternalRequestContext` only
from a trusted listener or proxy contract, and require it in profiles deployed
behind origin-rewriting intermediaries. Preserve the raw request target when
available. If `Accept-Signature` affects a response, make it uncacheable or add
`Accept-Signature` to `Vary` as required by the application profile.
