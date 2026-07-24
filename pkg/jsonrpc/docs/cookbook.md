# Scenario cookbook

## Named parameters

```go
type createParams struct {
    Reference string `json:"reference"`
}

params, rpcErr := jsonrpc.DecodeParams[createParams](raw)
if rpcErr != nil || params.Reference == "" {
    return nil, jsonrpc.InvalidParams()
}
```

`DecodeParams` rejects unknown fields. Use `json.Unmarshal` directly when an API
has a deliberate forward-compatible or partially dynamic params object.

## Optional parameters

The protocol permits params to be omitted, but `DecodeParams` treats missing
params as invalid. A handler with optional params should detect an empty raw
message first and install its defaults explicitly.

## Custom application errors

```go
return nil, jsonrpc.NewError(-30001, "Resource not ready").WithData(map[string]any{
    "retryable": true,
})
```

Use application codes outside JSON-RPC's reserved `-32768` through `-32000`
range. Error messages and data are public wire content; keep private causes in
`WithCause` instead.

Do not reuse and mutate one shared `*Error` with per-request data. Construct a
new value for request-specific data to avoid races and accidental disclosure.

## Mapping internal errors

Return ordinary Go errors from application layers. Configure one
`WithErrorMapper` to translate known categories and map every unknown error to
`InternalError`. `WithCause` keeps the original error available to local
logging and `errors.Is` without putting it on the wire.

## Notification audit

A notification has no response, including when the method is missing or the
handler fails. If audit confirmation matters, record it inside the handler or
use a normal request. Never make an HTTP `204` mean that application execution
succeeded; it only means no JSON-RPC response exists.

## Batch with partial errors

After `Client.Batch` returns `nil`, inspect every non-notification
`BatchCall.Error`. A returned error means the entire response was unusable or
the transport failed. A populated per-call error is a valid correlated RPC
failure. Results may arrive in any order.

## Custom transport

```go
type queueTransport struct { /* ... */ }

func (transport *queueTransport) RoundTrip(
    ctx context.Context,
    payload []byte,
) ([]byte, error) {
    // Preserve payload as one complete JSON-RPC message.
    // Return nil for notification-only messages.
}
```

Do not split or reorder members inside a batch at the transport layer. If the
underlying transport streams frames, reassemble one complete JSON value before
giving it to the client or dispatcher.

## Custom request IDs

Implement `IDGenerator` when IDs must carry a trace-friendly string or another
process-safe scheme. Generators used concurrently must be safe for concurrent
calls. Do not use fractional numeric IDs: the JSON-RPC specification discourages
them because binary representations can make correlation unreliable.

## HTTP authentication

Use `WithHTTPHeader` for a fixed header. For rotating credentials, install a
custom `http.RoundTripper` on a client supplied through `WithHTTPClient`; it can
derive a fresh credential per request without rebuilding the JSON-RPC client.

## Cancellation and deadlines

The HTTP transport attaches the supplied context to the request. The HTTP
handler passes `request.Context()` into dispatch. Handlers and middleware must
honor cancellation in their own I/O; the package cannot interrupt code that
ignores context.
