# Security Model

The secure baseline is finite work and explicit trust. Clients have bounded
timeouts and header sizes; body, cache, pool, pagination, retry, fixture,
transfer, and decompression paths require finite limits. Cancellation stops
waits and body processing.

Credentials are attempt-local, HTTPS-only, and same-origin by default. HTTP is
available only through the explicit local-test `AllowInsecure` option.
Cross-origin redirects strip authorization, proxy authorization, cookies,
trace context, baggage, and configured sensitive headers. Query credentials
are explicit and discouraged. Errors, logs, metrics, and persisted fixtures
exclude live secret material by default.

Use `EgressPolicy` for untrusted or dynamic destinations. The policy validates
scheme, authority, port, origin, address class, CIDRs, redirects, proxies, and
every resolved DNS answer at connection time. Metadata, private, loopback,
link-local, multicast, Unix-socket, userinfo, and wildcard behavior require
explicit treatment. Use `TLSPolicy` for custom roots, server identity, client
identity, or additive SPKI pinning without disabling certificate validation.

Do not log request or response bodies. Redact bounded error excerpts before
vendor mapping. Scope caches, cookies, OAuth tokens, coalescing, limiters,
breakers, transports, and metrics by the identity dimensions required by the
application. Never use tenant, credential, cursor, raw path, query, or vendor
message values as metric labels.

`make safety` verifies module checksums, runs `govulncheck`, and enforces
`GO-SAFETY-1`: no production `unsafe`, cgo, or `go:linkname`. Report suspected
vulnerabilities privately according to the repository `SECURITY.md`.

The maintained threat model, policy matrix, findings, evidence, and release
verdict are in [production hardening](hardening.md).
