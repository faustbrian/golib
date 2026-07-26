# Quickstart

## Server

Register transport-independent handlers, build a dispatcher, then choose an
adapter. A handler receives the request context and raw params so it can choose
positional, named, custom, or typed decoding.

```go
registry := jsonrpc.NewRegistry()
if err := registry.Register("greet", func(
    ctx context.Context,
    raw json.RawMessage,
) (any, error) {
    params, rpcErr := jsonrpc.DecodeParams[struct {
        Name string `json:"name"`
    }](raw)
    if rpcErr != nil || params.Name == "" {
        return nil, jsonrpc.InvalidParams()
    }
    return map[string]string{"message": "Hello " + params.Name}, nil
}); err != nil {
    log.Fatal(err)
}

dispatcher := jsonrpc.NewDispatcher(registry)
http.Handle("/rpc", jsonrpc.NewHTTPHandler(dispatcher))
log.Fatal(http.ListenAndServe(":8080", nil))
```

Wrap `NewHTTPHandler` in ordinary `net/http` middleware for HTTP authentication,
CORS, or rate limiting. Use dispatcher middleware for RPC-aware logging,
tracing, authorization, or metrics.

## Client

```go
transport, err := jsonrpc.NewHTTPTransport(
    "http://localhost:8080/rpc",
    jsonrpc.WithHTTPHeader("Authorization", "Bearer "+token),
)
if err != nil {
    log.Fatal(err)
}
client := jsonrpc.NewClient(transport)

type greeting struct {
    Message string `json:"message"`
}
result, err := jsonrpc.Call[greeting](ctx, client, "greet", map[string]string{
    "name": "Ada",
})
```

Use `client.Call` instead when the result type is chosen dynamically or the
result can be discarded.

## Notification

Notifications deliberately have no ID and receive no response:

```go
err := client.Notify(ctx, "events.record", map[string]any{
    "name": "shipment.created",
})
```

Successful transport only proves that the bytes were sent and the HTTP server
accepted them. A notification cannot report handler success or failure. Use a
request when the caller requires an application acknowledgement.

## Batch

```go
var first, second int
calls := []*jsonrpc.BatchCall{
    {Method: "math.add", Params: []int{1, 2}, Result: &first},
    {Method: "events.record", Params: []string{"viewed"}, Notification: true},
    {Method: "math.add", Params: []int{3, 4}, Result: &second},
}
if err := client.Batch(ctx, calls...); err != nil {
    log.Fatal(err) // transport or invalid batch response
}
for _, call := range calls {
    if call.Error != nil {
        log.Printf("RPC error: %v", call.Error)
    }
}
```

Batch responses may arrive in any order. The client correlates them by ID.
Per-call RPC failures populate `BatchCall.Error`; structural, transport, or
decode failures are returned from `Batch`.

## Custom transport

Implement one method to use a message bus, test double, WebSocket, or another
request/response mechanism:

```go
type Transport interface {
    RoundTrip(context.Context, []byte) ([]byte, error)
}
```

For notifications, return `nil, nil`. The transport must preserve the complete
JSON-RPC payload, including batch arrays.
