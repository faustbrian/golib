# Error Classification

Use `errors.Is` for stable categories and `errors.As` for structured metadata.
Rendered messages are intentionally sparse because URLs, decoder causes,
vendor messages, and transport failures can contain secrets.

The principal families are:

| Layer | Stable categories and typed values |
| --- | --- |
| Client and transport | `ErrClientClosed`, `ErrNilRequest`, `TransportError` |
| Middleware | `ErrInvalidMiddlewareResult`, `MiddlewareExecutionError`, `MiddlewarePanicError` |
| Admission and resilience | `RateLimitError`, `CircuitBreakerError`, `RetryExhaustedError` |
| HTTP status | `ErrHTTPStatus`, `HTTPStatusError` |
| Decode and lifecycle | `ResponseDecodeError`, `ErrResponseDecoderPanic`, `ResponseBodyError`, `ResponseLimitError`, `ResponseLengthError` |
| Streaming and files | `TransferError`, `ErrTransferProgressPanic`, `RangeError`, `CompressionError`, `FileTransferError` |
| Pagination and pools | `PaginationError`, `PoolError`, `PoolPanicError` |
| Testing fixtures | `FixtureError`, `FixtureReplayError` |

`HTTPStatusError` may expose cloned headers, status, a stable vendor code,
bounded redacted excerpt, allowlisted request ID, and retryability. It never
renders those fields. Status classification consumes and closes rejected
responses; successful responses remain caller-owned.

Joined errors preserve multiple lifecycle failures. Check the category rather
than comparing message text:

```go
var status *httpclient.HTTPStatusError
switch {
case errors.As(err, &status):
	// Use status.StatusCode, status.VendorCode, and status.Retryable.
case errors.Is(err, context.Canceled):
	// Caller cancellation, not a dependency failure.
case errors.Is(err, httpclient.ErrRetryExhausted):
	// Inspect RetryExhaustedError for bounded attempt metadata.
}
```

Vendor mappers receive only a bounded redacted snapshot. Keep vendor codes
low-cardinality and never promote raw messages to errors, logs, or metrics.
