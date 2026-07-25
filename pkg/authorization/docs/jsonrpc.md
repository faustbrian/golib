# JSON-RPC integration

The `authrpc` package provides native middleware for
`github.com/faustbrian/golib/pkg/jsonrpc`. Applications map each method's context and
raw parameters to a typed authorization request.

```go
middleware, err := authrpc.NewMiddleware(
    engine,
    func(ctx context.Context, params json.RawMessage) (authorization.Request, error) {
        principal, ok := principalFromContext(ctx)
        if !ok {
            return authorization.Request{}, errUnauthenticated
        }
        return authorization.Request{
            Subject:  principal,
            Action:   "invoice.read",
            Resource: authorization.Resource{Type: "invoice"},
        }, nil
    },
)
```

Only an explicit allow invokes the method handler. Deny and not-applicable
return the bounded server error code `authrpc.CodeForbidden`; mapper,
evaluation, and invalid-outcome failures return JSON-RPC internal errors. Local
causes are retained by `jsonrpc` but are not serialized.

Applications can customize the denial and internal error mapping with
`WithDeniedError` and `WithErrorMapper`. Returning nil from either custom
mapper is contained as an internal error. Allowed handlers can inspect the
decision with `authrpc.DecisionFromContext`.
