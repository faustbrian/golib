# Integration and middleware ordering

## Client requests

For buffered body signatures, compose transports from outermost to innermost:

1. application retry and redirect policy creates a fresh request;
2. `BufferedContentDigestRoundTripper` consumes and closes the body, enforces
   its limit, refreshes trailers populated by the body at EOF, and creates a
   replayable clone;
3. `SigningRoundTripper` signs the already-present digest and representation
   metadata required by the profile;
4. telemetry records only safe categories and correlation identifiers;
5. the network transport sends the request.

Do not put signing outside digest generation: the resulting signature would
not cover the generated digest. Do not reuse a request passed to a
`RoundTripper`; `net/http` transfers body ownership to the transport.

For streaming, `TrailerSigningRoundTripper` is placed immediately above the
network transport. It declares trailers before delegation, hashes returned body
bytes, refreshes values for the application trailer names declared before
delegation, and signs only at EOF. Adding, removing, or case-aliasing trailer
names during the body read fails closed. Supported HTTP/1 requests use exact
chunked framing even for an empty body; HTTP/2 leaves transfer coding to the
protocol. `CONNECT` and profiles covering connection, content-length,
keep-alive, proxy-connection, TE, trailer, transfer-encoding, or upgrade are
rejected because those fields can change across transports. A wrapped transport
that returns a response before EOF finalization is also rejected and the
response body is closed.

The streaming adapter intentionally has no `GetBody`. A retry, redirect, hedge,
or failover must create a new request and body and rerun the adapter. An error
after earlier reads can mean partial bytes reached the peer; non-idempotent
application retries therefore require an idempotency protocol.

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
earlier. Buffered request verification refreshes and preserves application
trailers populated while the original body reaches EOF before delegating.

## Responses

Direct response signing and verification must identify the transport model in
`MessageContext.ResponseTransport`. Use `ResponseTransportWrite` before
emission through `Response.Write`, and `ResponseTransportReceived` for a
response parsed by `net/http` or returned by a `RoundTripper`. The unspecified
zero value resolves transport-managed fields only when the received and write
representations are identical; ambiguity returns `ErrSignatureBase`. The
provided response middleware and round trippers set this mode themselves.

`ResponseSigningMiddleware` buffers the complete response before emitting
headers. The configured maximum is a hard failure boundary; no buffered partial
response is sent. The adapter intentionally does not expose `Flusher`,
`Hijacker`, or full-duplex behavior. Informational responses other than a final
response are suppressed while the final response is buffered. A final 101 or a
successful 2xx `CONNECT` is rejected before emission because subsequent bytes
belong to an upgraded protocol or tunnel and cannot be modeled as signed HTTP
content. Ordinary
headers already present on the outer writer are inherited by the handler; the
handler's `Set`, `Add`, and `Del` operations define the exact signed and emitted
field set. The adapter owns response framing and rejects handler-managed
`Transfer-Encoding`, `Trailer`, and `http.TrailerPrefix+Name` fields rather than
signing a model that `net/http` could rewrite on emission. Its required,
concurrency-safe `ReportError` callback receives the redacted `ErrBodyRead`
category exactly once if signed response emission returns an error, a short
write, or an invalid count after headers commit; the middleware does not try to
rewrite the committed status or body.

`TrailerResponseSigningMiddleware` adds `Content-Digest`, `Signature-Input`,
and `Signature` to the final trailer declaration at header commit, preserves
valid application-declared trailers in canonical sorted order, hashes actual
emitted content, and finalizes the trailers after the handler returns. Valid
trailers added after commit with `http.TrailerPrefix+Name` are authenticated
only when the signing profile explicitly covers the exact `name;tr` component;
unlisted late trailers can still be emitted but are not signed. Profiles that
cover protocol-dependent connection or framing fields, including the `trailer`
declaration, are rejected at construction. Malformed, forbidden, or
case-colliding late trailer names are rejected.

The middleware exposes the underlying writer through `Unwrap` for
`http.ResponseController`. HTTP/1.0, `HEAD`, bodyless final statuses, and
protocol switching are rejected because they cannot carry this trailer
contract accurately. A handler attempt to return a successful 2xx `CONNECT`
is replaced with an unsigned 501 response before tunnel mode begins; non-2xx
`CONNECT` responses use ordinary signed HTTP handling. Other failures discovered
before commit produce a cleared unsigned 500 response. Flush and write failures
are reported as redacted body integration failures.
The response is already in flight when a late body-limit, key, randomness, or
signing failure occurs, so the required `ReportError` callback must record the
failure. Every late failure clears signer-owned and handler-injected protected
trailer values while leaving the message incomplete. A recipient must wait for
EOF and reject absent or invalid trailers;
it must never use the response merely because initial headers or body bytes
arrived.

For `ResponseSigningMiddleware`, `HEAD` and status codes that do not permit
content use the actual empty message content for digest calculation and
emission. A handler can still write a `HEAD` representation to establish its
metadata, matching `net/http`, but those bytes are never emitted or included in
`Content-Digest`. A 205 handler body write returns `http.ErrBodyNotAllowed`; the
middleware signs and emits the empty 205 response required by RFC 9110 Section
15.3.6. The streaming trailer middleware instead rejects `HEAD`, 204, 205, and
304 because those responses cannot complete its mandatory trailer contract.

`VerifyingRoundTripper` verifies response signatures against the exact related
request. A signature that does not cover `Content-Digest` leaves the body
unread. When the selected signature covers the complete digest field, configure
`ContentDigestAlgorithms` and `MaxBufferedBytes`; the adapter then consumes and
closes the coded body, rejects an already transparently decompressed response,
verifies every configured authenticated digest, and returns a replayable
in-memory body. Selection and external-context callbacks receive isolated
snapshots and cannot replace the body, digest, trailers, or related request
being verified. The snapshots intentionally omit mutable TLS connection state;
callbacks that need TLS policy input must receive an application-owned immutable
projection through their closure. Digest-covered 101 and successful `CONNECT`
responses are rejected and closed before any protocol bytes are read. A selected
signature that does not cover `Content-Digest` leaves the opaque body untouched.
On failure the adapter closes the body and returns no response.
Verification metadata is attached to a cloned `Response.Request` context.

`BufferedTrailerVerifyingRoundTripper` consumes and closes the bounded coded
response body so trailers are available, rejects transparent decompression,
verifies the content digest and its authenticated trailer coverage, and returns
a replayable in-memory body only after the signature succeeds. Its callbacks
also receive isolated snapshots. It rejects 101 and successful `CONNECT`
responses before reading the upgraded connection or tunnel. On any failure it
returns no response.

## Proxies and caches

Forwarded fields are untrusted input. Construct `ExternalRequestContext` only
from a trusted listener or proxy contract, and require it in profiles deployed
behind origin-rewriting intermediaries. Preserve the raw request target when
available. If `Accept-Signature` affects a response, make it uncacheable or add
`Accept-Signature` to `Vary` as required by the application profile.
