# Adoption Examples

The repository includes compiling examples for two materially different vendor
styles:

- `examples/githubrest` wraps a resource-oriented REST endpoint. It composes
  same-origin bearer authentication, safe read retries, vendor media types,
  independent status classification, and bounded typed JSON decoding.
- `examples/ethereumjsonrpc` sends a replayable JSON-RPC envelope over POST and
  distinguishes HTTP status from a protocol-level error returned in a
  successful HTTP response.

Both examples are executable output tests backed by deterministic local TLS
servers, so the CI gate validates their request shape, authentication or
protocol envelope, response decoding, and printed result without public
network access. They preserve `http.Request` and `http.Response`, keep DTOs and
domain errors outside core, share a closeable policy client, and make body
ownership explicit. Production vendor packages should additionally escape
dynamic path segments, define redacted typed vendor errors, configure egress
allowlists, and add strict fixture plus `httptest.Server` contract coverage.

For generated clients, prefer a doer seam that the vendor wrapper can route
through `Client.Do`, ensure only one retry layer is active, and keep generated
DTOs behind typed public methods. Injecting `Client.HTTPClient()` uses only its
standard transport, timeout, and jar; it bypasses all middleware policy.
