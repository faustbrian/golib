# ResponseWriter compatibility

| Middleware | Flusher | Hijacker | Pusher | ReaderFrom | ResponseController |
|---|---|---|---|---|---|
| chain, request ID, proxy, content, admission | unchanged | unchanged | unchanged | unchanged | unchanged |
| recovery, observe | exact underlying set | exact | exact | exact | forwarded |
| CORS, secure headers, no-store | exact underlying set | exact | exact | exact | forwarded |
| body limit | exact underlying set | exact | exact | exact | forwarded |
| buffered deadline timeout | unavailable | unavailable | unavailable | unavailable | unavailable |
| buffered compression | unavailable | unavailable | unavailable | unavailable | unavailable |

“Exact” means an optional interface is exposed only when the underlying writer
implements it. `http.ResponseController` reaches supported operations through
the wrapper's `Unwrap` behavior. Buffered policies deliberately withhold
streaming and connection takeover because replay cannot honor those contracts.

HTTP/1.1 flush, trailers, and hijacking and HTTP/2 flush and trailers are tested
on real listeners. HTTP/2 does not support hijacking. Push remains dependent on
the Go transport and may return `http.ErrNotSupported`.

Tracking, header-policy, compression, and buffered-timeout writers preserve
valid 1xx responses without treating them as final commitment. Compression
commits `101 Switching Protocols` immediately as identity; buffered timeout is
not compatible with upgrades because it intentionally exposes no takeover
capability. Buffering writers panic for status codes outside net/http's
100-through-999 range.

Nested transparent wrappers are tested through `ResponseController` for read
and write deadlines and full duplex. Compression and timeout deliberately stop
controller traversal because buffered replay cannot honor those operations.
Compression snapshots ordinary headers at logical commitment, keeps declared
trailers out of initial headers, and copies eligible trailer values only after
the body completes.
