# Typed Vendor Integration Patterns

Vendor packages own DTOs, endpoint names, codecs, and business errors. This
module supplies ordinary `http.Client` policy, request decorators, bounded
response handling, and test transports. Generated models never enter the core
API.

## JSON and JSON-RPC

For REST JSON, construct a replayable `NewBytesBody`, build a `RequestSpec`,
call `Client.Do`, classify status, and use `DecodeJSONResponse[T]`. For
JSON-RPC, the vendor package owns the request ID, method, params, result, and
protocol-error envelope. A successful HTTP status can still contain a JSON-RPC
error, so inspect it after bounded decoding.

Never enable unsafe retries merely because a payload is JSON. The endpoint
contract, HTTP method, body replayability, and idempotency policy must all
agree.

## XML and SOAP

Core intentionally has no XML or SOAP codec. Pass a caller-selected decoder to
`DecodeResponse[T]` with an explicit media-type allowlist. A SOAP vendor
package owns envelope namespaces, actions, fault mapping, and XML hardening.
SOAP faults commonly arrive with HTTP 500, so choose whether to decode a
bounded fault before or instead of generic status classification. Redact any
fault excerpt before storing it in an `HTTPStatusError`.

Streaming XML decoders must consume exactly one representation. The bounded
reader and trailing-data policy remain enforced by `DecodeResponse`.

## Generated OpenAPI clients

Prefer a generator option that accepts `*http.Client` or `http.RoundTripper`.
`client.HTTPClient()` exposes the underlying standard client, but calls made
through it bypass operation identity and every middleware policy. It is only a
valid generated-client integration when standard transport, timeout, and jar
behavior are sufficient. It does not retain retry, authentication, cache,
telemetry, compression, or target-URL egress guarantees.

When those policies are required, prefer generated code that accepts a doer
implemented by the vendor wrapper and route each operation through `Client.Do`.
Keep `client.Close` under that wrapper's ownership. Generated request editors
can be adapted as operation or attempt middleware, but signing and credentials
belong at the attempt boundary so retries and redirects receive fresh policy.

If a generator accepts only a transport, pass a middleware-composed transport
whose ownership remains explicit. Do not copy generated request or response
types into this module and do not layer the generator's retry loop over this
module's retry middleware.

## Generated WSDL clients

The same bypass distinction applies to WSDL. Leave SOAP types and fault
semantics in the vendor package and use bounded streaming decoding. Validate
whether the generator buffers bodies, follows redirects, or retries before
enabling overlapping policy.

See [adoption examples](adoption-examples.md) for materially different REST and
JSON-RPC wrappers.
