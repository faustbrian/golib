# HTTP middleware

`idempotencyhttp` requires `Idempotency-Key`, elects one durable handler owner,
and stores a bounded, versioned response. A completed response is replayed only
when the application-supplied fingerprint matches.

```go
middleware, err := idempotencyhttp.New(idempotencyhttp.Options{
	Service:          service,
	Lease:            30 * time.Second,
	MaxResponseBytes: 64 * 1024,
	TransitionTimeout: 5 * time.Second,
	ReplayHeaders:    []string{"Content-Type", "Location"},
	Key: func(request *http.Request, value string) (idempotency.Key, error) {
		return idempotency.NewKey(
			"public-api",
			request.Header.Get("X-Tenant-ID"),
			"POST /widgets",
			request.Header.Get("X-Caller-ID"),
			value,
		)
	},
	Fingerprint: func(request *http.Request) (idempotency.Fingerprint, error) {
		body, err := io.ReadAll(io.LimitReader(request.Body, 64*1024+1))
		if err != nil {
			return idempotency.Fingerprint{}, err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		return canonical.JSONFingerprint(
			"widget-json-v1",
			body,
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

handler := middleware.Handler(createWidgetHandler)
```

The fingerprint callback bounds the read and restores the request body for the
downstream handler. Use operation-specific canonicalization rules; transport
headers alone rarely identify the business request safely.

## Outcomes

- `acquired` and `stale_owner_takeover` execute the handler.
- `replayed` returns the stored status, selected headers, and body with
  `Idempotency-Replayed: true`.
- `in_progress` and `conflict` return HTTP 409 without executing the handler.
- storage failures return HTTP 503 and fail closed.
- missing keys and key or fingerprint validation failures return HTTP 400.

Every response includes `Idempotency-Outcome`. Do not treat a lease as proof
that an older side effect stopped. An executing handler reads its ownership with
`idempotency.OwnershipFromContext(request.Context())`. Apply the returned
`FencingToken` in the application write when stale work is possible.

## Buffering and failure boundaries

The wrapper buffers the response and therefore does not expose streaming,
flushing, hijacking, or full-duplex interfaces. Use it only for bounded request
and response operations.

If a handler crosses `MaxResponseBytes`, its `Write` returns
`ErrResponseTooLarge`. The middleware records a terminal HTTP 500 response so a
retry cannot silently execute the handler again. If completion or terminal
failure persistence is unavailable, the middleware returns HTTP 503 without
emitting the buffered application response. The storage result is then unknown;
operators must inspect the durable record before manual replay.

A panic is propagated after a best-effort release using a context detached
from request cancellation and bounded by `TransitionTimeout`. This prevents a
canceled handler context from leaving an avoidable active lease. Process death
cannot run this cleanup; recovery still waits for lease expiry.

Only headers listed in `ReplayHeaders` are emitted and persisted. Never include
hop-by-hop, connection-specific, unbounded, or secret-bearing headers.
