# Standards scope and divergences

The package implements narrow server behavior; it does not claim full
compliance with an entire specification.

- Go 1.26.5 `net/http` handler, body, timeout, trailer, optional writer, and
  `ResponseController` contracts, including informational responses and valid
  status-code bounds.
- RFC 9110 field lists, qvalues, media types, content coding selection,
  status/method body exclusions, singular `Content-Type`, whole-list `Accept`
  validation, `Vary`, and `Retry-After` syntax used here.
- RFC 9111 `no-store`, `no-transform`, and cache-key variation behavior.
- RFC 7239 bounded `Forwarded` list, quoted value, node, host, and proto parsing
  at an explicit trusted-peer boundary. Duplicate or malformed parameters,
  obfuscated nodes, and `unknown` nodes intentionally fail closed rather than
  become effective request data.
- The Fetch Living Standard CORS response and preflight headers, serialized
  origins, credentials/wildcard restrictions, and cache variation. Private
  Network Access is opt-in and documented as an extension, not blanket Fetch
  compliance. HTTP method tokens retain their case-sensitive semantics.
- RFC 6797 HSTS directive grammar with an additional conservative ten-year
  construction bound.

W3C Trace Context parsing and SDK/exporter lifecycle remain owned by
`telemetry`; this package's adapters compose owning middleware and do not
create spans or exporters.

Primary references: [Go net/http](https://pkg.go.dev/net/http),
[RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html),
[RFC 9111](https://www.rfc-editor.org/rfc/rfc9111.html),
[RFC 7239](https://www.rfc-editor.org/rfc/rfc7239.html),
[Fetch](https://fetch.spec.whatwg.org/#http-cors-protocol), and
[Trace Context](https://www.w3.org/TR/trace-context/).

Buffered timeout intentionally diverges from upgrade-capable writers: it
forwards informational responses but cannot honor `101`, hijacking, full
duplex, or streaming. Such endpoints must not install buffered timeout.

Compression intentionally removes identity-representation entity tags,
lengths, and digest headers or trailers when content coding changes. Custom
trailers retain standard trailer timing and are tested through real HTTP.

The body-limit contract intentionally counts encoded bytes visible when the
middleware is entered. `net/http` exposes no prior-read counter, so installing
the limit after any body-reading layer cannot enforce a whole-request budget
and is an invalid chain order.
