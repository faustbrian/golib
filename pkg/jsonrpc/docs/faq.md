# FAQ

## Is this tied to HTTP?

No. `Dispatcher` and `Client` depend only on bytes, context, and the `Transport`
interface. The package includes HTTP adapters because they are common and easy
to compose with `net/http`.

## Why did my notification return no error response?

A valid notification has no ID and the server must not respond, even when the
method does not exist or execution fails. Use logs and metrics for server-side
visibility, or send a normal request when the caller needs acknowledgement.

## Are batch handlers concurrent?

No. The dispatcher executes members sequentially in input order. JSON-RPC does
not require a processing order, and callers must correlate batch responses by
ID rather than position. A future optional concurrent helper must preserve all
protocol semantics and be explicit about application concurrency.

## Can I use string IDs?

Yes. Use `StringID` or a custom `IDGenerator`. Number, string, and null IDs are
not interchangeable. Fractional numeric IDs are discouraged by the JSON-RPC
specification and should be avoided.

## Why are scalar params rejected?

JSON-RPC 2.0 says params, when present, must be a structured value: an array for
positional arguments or an object for named arguments. Strings, numbers,
booleans, and null are therefore invalid params shapes.

## Does the package validate my domain fields?

No. It validates envelopes and offers typed JSON decoding. Required fields,
ranges, formats, and business invariants belong in handlers or an application
validator. Return `InvalidParams` for invalid method arguments.

## Can I expose internal errors in Data?

You can, but should not. `Data` is serialized. Prefer `WithCause` for logs and
traces, then send a stable public message and deliberately safe data.

## Does the client retry?

No. Automatic retries can duplicate non-idempotent methods and are outside the
core protocol. Add retries around the transport only with application-specific
idempotency and backoff decisions.

## Is WebSocket supported?

The core works with any custom transport, but the first version does not ship a
WebSocket helper. A future adapter can be added without changing envelope or
dispatch semantics.

## Why does the HTTP server return 200 for RPC errors?

The HTTP exchange succeeded and carries a JSON-RPC response whose `error`
member describes the RPC failure. Transport problems such as unsupported media
type or excessive body size use HTTP error statuses before dispatch.

## Is explicit null ID a notification?

No. An ID member with `null` is still present and receives a response with a
null ID. Omitting the ID member creates a notification. The specification
discourages null request IDs because null is also used when an invalid request
ID cannot be established.
