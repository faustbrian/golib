# JSON-RPC middleware

`idempotencyrpc` durably elects one handler for a method-scoped key and replays
both successful results and JSON-RPC protocol errors.

```go
middleware, err := idempotencyrpc.New(idempotencyrpc.Options{
	Service:          service,
	Lease:            30 * time.Second,
	MaxResponseBytes: 64 * 1024,
	TransitionTimeout: 5 * time.Second,
	Key: func(ctx context.Context, request idempotencyrpc.Request) (idempotency.Key, error) {
		return idempotency.NewKey(
			"public-rpc",
			tenantFromContext(ctx),
			request.Method,
			callerFromContext(ctx),
			deliveryKeyFromContext(ctx),
		)
	},
	Fingerprint: func(request idempotencyrpc.Request) (idempotency.Fingerprint, error) {
		return canonical.JSONFingerprint(
			"rpc-params-v1",
			request.Params,
			canonical.Limits{
				MaxInputBytes: 64 * 1024,
				MaxOutputBytes: 64 * 1024,
				MaxDepth: 32,
			},
		)
	},
})
if err != nil {
	return err
}
```

The key operation must exactly equal the JSON-RPC method. The delivery key must
be stable across retries and scoped by tenant and caller; the JSON-RPC request
ID alone is not necessarily a safe business idempotency key.

`Call` returns `conflict` or `in_progress` without invoking the handler. A
replayed call has `Replayed` set and preserves either the JSON result or the
protocol error code, message, and data. Storage errors are returned as Go errors
and fail closed; they are not converted into JSON-RPC application errors.

Responses contain exactly one valid JSON result or protocol error. Invalid or
oversized handler output is recorded as terminal JSON-RPC error `-32603`, which
prevents a retry from silently rerunning the handler. Panics release ownership
with a context detached from handler cancellation and bounded by
`TransitionTimeout`, then propagate to the transport's recovery layer. Process
death cannot run this cleanup; recovery still waits for lease expiry.

As with every adapter, lease takeover does not prove old work stopped. Apply the
ownership from `idempotency.OwnershipFromContext(ctx)` in the transaction or
conditional write that commits the business side effect.
