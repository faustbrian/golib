# Compatibility Boundaries

The RFC 9421 implementation and legacy or vendor signing protocols are separate
trust boundaries. The core parser never auto-detects Cavage drafts, AWS
Signature V4, OAuth 1.0, or vendor-specific formats. The `compatibility`
subpackage provides explicit callback seams; callers own each external
protocol implementation, policy, vectors, and lifecycle.

Outbound signing callbacks receive an isolated request view. They can emit
protocol-specific headers and trailers, but `Signature-Input`, `Signature`, and
`Accept-Signature` remain reserved for RFC 9421, and changes to the method, URL,
host, request target, body, or `GetBody` never reach the delegated request. A
Cavage implementation that uses the generic adapter therefore emits its
`Signature` authentication scheme through `Authorization`, not the reserved
RFC 9421 `Signature` field. A signing callback failure closes the caller body
before the sanitized failure is returned.

Inbound verification callbacks receive an isolated request view with no body
or `GetBody` access. Callback views also omit parsed form and multipart state,
TLS state, and redirect responses because `net/http` clones can retain indirect
aliases to multipart temporary files, certificates, response bodies, or the
original request. Their only verification result is the returned error; no
request mutation reaches rejection handling or later middleware. This applies
to every header and trailer, not just signature fields, because RFC 9421 can
cover any HTTP field. Discarding a legacy normalization of `Content-Digest`,
`Authorization`, or an application field prevents a later RFC verifier from
accepting a signature over a message different from the one received.

See [HTTP-SIG-DEC-018](specification-decisions.md#http-sig-dec-018-legacy-and-vendor-protocol-isolation)
for the selected behavior, alternatives, consequences, and executable evidence.
See [integration guidance](integration.md) for adapter ordering and
[security guidance](security.md) for trust-boundary requirements.
