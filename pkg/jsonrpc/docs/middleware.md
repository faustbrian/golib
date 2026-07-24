# Middleware, authentication, and observability

Dispatcher middleware wraps method execution without changing the transport.
It is appropriate for RPC-aware authentication, authorization, logging,
tracing, metrics, deadlines, and correlation. Ordinary `net/http` middleware
remains the right place for TLS identity, HTTP headers, CORS, or IP controls.

Use `Hooks` when telemetry must include parse errors, invalid requests,
method-not-found responses, notifications, and panic recovery in addition to
handler execution:

```go
hooks := jsonrpc.Hooks{
    OnRequest: func(ctx context.Context, request *jsonrpc.Request) context.Context {
        // request is nil when no valid request envelope could be recovered.
        return startProtocolSpan(ctx, request)
    },
    OnResponse: func(ctx context.Context, request *jsonrpc.Request, response *jsonrpc.Response) {
        // Notifications provide an internal outcome that is never sent.
        finishProtocolSpan(ctx, request, response)
    },
}
dispatcher := jsonrpc.NewDispatcher(registry, jsonrpc.WithHooks(hooks))
```

Hooks are protected from panics. A context returned by `OnRequest` flows into
middleware and the handler. Recovered handler panics attach a local cause and
stack to the internal error, visible to `OnResponse` but never serialized.

## Ordering

Middleware is listed outermost first:

```go
dispatcher := jsonrpc.NewDispatcher(
    registry,
    jsonrpc.WithMiddleware(recoverMetadata, authenticate, observe),
)
```

The call order is `recoverMetadata -> authenticate -> observe -> handler`, then
the reverse order on return. The dispatcher itself contains panics after all
middleware and converts them to internal errors.

## Request metadata and correlation

The validated request is available through context before the chain runs:

```go
func correlate(next jsonrpc.Handler) jsonrpc.Handler {
    return func(ctx context.Context, params json.RawMessage) (any, error) {
        request, ok := jsonrpc.RequestFromContext(ctx)
        if !ok {
            return nil, jsonrpc.InternalError()
        }
        correlationID := fmt.Sprintf("%s:%d", request.Method, request.ID.Kind())
        ctx = context.WithValue(ctx, correlationKey{}, correlationID)
        return next(ctx, params)
    }
}
```

Use your own formatter for IDs; the example is intentionally schematic. Do not
put secrets or full params into correlation IDs.

## Authentication and authorization

HTTP bearer tokens can be verified before the RPC handler. If authorization
depends on the RPC method, place the authenticated principal in context in HTTP
middleware and enforce policy in dispatcher middleware:

```go
var unauthorized = jsonrpc.NewError(-32001, "Unauthorized")

func authorize(next jsonrpc.Handler) jsonrpc.Handler {
    return func(ctx context.Context, params json.RawMessage) (any, error) {
        request, _ := jsonrpc.RequestFromContext(ctx)
        principal, ok := principalFromContext(ctx)
        if !ok || !principal.CanCall(request.Method) {
            return nil, unauthorized
        }
        return next(ctx, params)
    }
}
```

Choose application error codes from the reserved server-error range `-32099`
through `-32000`, or a documented application range. Never include token or
policy internals in error data.

## Observability

A vendor-neutral middleware can emit logs, spans, and metrics around the same
execution boundary:

```go
func observe(next jsonrpc.Handler) jsonrpc.Handler {
    return func(ctx context.Context, params json.RawMessage) (result any, err error) {
        request, _ := jsonrpc.RequestFromContext(ctx)
        started := time.Now()
        ctx, span := tracer.Start(ctx, "rpc."+request.Method)
        defer func() {
            span.End()
            metrics.Observe(request.Method, err, time.Since(started))
        }()
        return next(ctx, params)
    }
}
```

Keep metric labels bounded: method and normalized outcome are safe; request ID,
user ID, raw error text, and params are usually not. Log causes on the server
from errors returned by application code, but send only mapped public errors.

## Error mapping

`WithErrorMapper` is the final boundary for ordinary Go errors:

```go
jsonrpc.WithErrorMapper(func(err error) *jsonrpc.Error {
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        return jsonrpc.NewError(-32002, "Deadline exceeded").WithCause(err)
    default:
        return jsonrpc.InternalError().WithCause(err)
    }
})
```

Handlers can return `*jsonrpc.Error` directly for expected public failures.
Causes never serialize; `Data` does, so populate it only with deliberate public
information.
