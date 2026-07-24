# Security deployment

## Trusted proxies

List only direct proxy address ranges you operate. Ensure each trusted proxy
removes client-supplied forwarding fields before appending its own. Choose
either RFC 7239 `Forwarded` or the explicitly configured `X-Forwarded-*` mode.
Malformed or oversized fields fail closed to direct connection data. Trusted
prefix lists are validated, canonicalized, and capped at 256 entries. Never use
effective host or scheme without an allowlist when building redirects.
RFC 7239 elements reject duplicate parameters and invalid parameter names.

## CORS

List exact origins whenever credentials are enabled. Wildcard method, header,
exposure, and origin configurations are rejected with credentials. CORS only
controls browser response visibility; it is not authentication, authorization,
or CSRF protection. Requested methods are validated as tokens before wildcard
matching or response-header reflection.

## HSTS and headers

HSTS requires `AcknowledgeHSTS`. Confirm HTTPS works for every covered host
before enabling a long max age or subdomains. CSP is opt-in because this package
cannot infer scripts, templates, nonces, or application assets.

## Compression

Set `Cache-Control: no-transform` for secrets reflected near attacker-controlled
input. Compression skips ranges, existing encodings, no-body statuses, HEAD,
and small responses. Eligible large responses continue as bounded-memory gzip
streams. Coding changes remove representation-specific length, digest, and
entity-tag fields.

## IDs, bodies, and timeouts

Inbound IDs are untrusted by default, accept printable ASCII only, and are
never authorization evidence. Body limits apply to encoded transport bytes
before decoding and must be installed before any reader consumes the body.
They can only count unread bytes visible at installation. Context deadlines do
not interrupt code that ignores context.
Buffered handler timeouts cap retained output and intentionally reject
streaming capabilities. `MaxConcurrent` also bounds handler executions that
remain after their timeout because they ignored cancellation.

Content negotiation rejects duplicate `Content-Type` fields and validates all
`Accept` field values before allowing a representation. A valid leading range
cannot hide a malformed or oversized tail.
