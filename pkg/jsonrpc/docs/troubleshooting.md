# Troubleshooting

Start by separating HTTP transport failures from JSON-RPC error objects. HTTP
statuses other than 200 or 204 indicate the adapter or upstream rejected the
exchange. A 200 response may contain either `result` or `error`.

## Parse error (-32700)

The payload is not valid JSON. Check truncation, framing, character encoding,
and whether a stream combined multiple JSON values. Log payload size and a
safe correlation ID; avoid logging confidential raw bodies.
JSON text must be valid UTF-8. Malformed bytes are rejected rather than
silently replaced with the Unicode replacement character.

## Invalid Request (-32600)

The JSON value is valid but not a valid request object. Common causes are a
missing or non-`"2.0"` `jsonrpc`, a non-string method, scalar/null
params, an invalid ID type, an empty batch, or a scalar batch member. Invalid
requests respond with a null ID because the supplied ID cannot be trusted.
Duplicate envelope members are also rejected because different JSON parsers
may select different values. Remove the duplicate rather than relying on
last-member-wins behavior.
Reserved member names are case-sensitive: use `jsonrpc`, `method`, `params`,
and `id` exactly. Case variants such as `Method` are not aliases.

## Method not found (-32601)

Confirm exact, case-sensitive registration and request names. The `rpc.` prefix
is reserved and cannot be registered. Registration rejects duplicates, so
check and handle the error returned by `Registry.Register` during startup.

## Invalid params (-32602)

Confirm whether the method expects positional arrays or named objects. When
using `DecodeParams`, unknown object fields are rejected. Missing params are
also rejected by that helper, as are duplicate top-level named parameters;
handlers with optional params must handle an empty raw message before decoding.

## Internal error (-32603)

The handler panicked, returned an ordinary unmapped error, produced an
unencodable result, or the configured mapper returned nil. Inspect server logs
and tracing using the local error cause. Do not replace the public message with
raw internal error text.

## Client says mismatched ID

The response ID is not exactly the request ID. String `"1"` and number `1` are
different. Check proxies, custom transports, shared buffers, and servers that
generate new IDs instead of echoing the request member.

## Missing or duplicate batch response

Every non-notification batch member must have exactly one response. The order
may differ. A missing member often means a server treated a null ID as a
notification; a duplicate often means the transport retried or combined
frames incorrectly.

`ErrDuplicateRequestID` occurs before transport I/O when a custom ID generator
returns the same canonical ID for multiple non-notification calls in one
batch. Fix the generator; duplicate IDs cannot be correlated safely.

## Unexpected response to a notification

The server or custom transport returned bytes for a notification-only message.
For HTTP, the supplied adapter returns 204 with no body. Custom transports must
return `nil, nil` when no JSON-RPC response exists.

## Unsupported media type (415)

Set `Content-Type: application/json`. The HTTP adapter also accepts
`application/json-rpc` and `application/*+json`, including media-type
parameters. An absent content type is rejected.

## Request or response too large

The HTTP server and client default to four MiB. Change them with
`WithMaxRequestBytes` and `WithMaxResponseBytes` only after validating memory,
latency, and upstream proxy limits. For very large datasets, prefer a domain
pagination or streaming design rather than oversized RPC envelopes.

The transport-neutral client separately limits reply parsing to four MiB and
returns `ErrClientResponseTooLarge`. Change this with
`WithMaxClientResponseBytes`. Custom transports must still bound reads before
allocating and returning the response bytes.

Direct `Dispatcher` use has its own four-MiB payload limit and a default limit
of 1,024 batch members. Configure these with `WithMaxDispatchBytes` and
`WithMaxBatchItems`. A violation returns the implementation-defined `-32000`
`Request limit exceeded` response with a null ID before any member executes.
If `WithMaxRequestBytes` raises the HTTP limit, raise the dispatcher byte limit
separately when the larger payload is intentional.

## Context cancellation is ignored

The package propagates context, but handler code and downstream clients must
observe it. Ensure database, HTTP, and queue calls use the supplied handler
context instead of `context.Background()`.

Do not pass a nil context. `HTTPTransport.RoundTrip` rejects it before network
I/O; use `context.Background()` or `context.TODO()` when no narrower lifetime is
available.

## HTTP redirect is returned as a status error

The built-in transport does not follow redirects because configured headers
may contain credentials that must not cross origins. Use the final JSON-RPC
endpoint directly. If redirects are an intentional part of a trusted topology,
pass an `http.Client` with an explicit `CheckRedirect` policy through
`WithHTTPClient`.
