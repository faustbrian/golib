# Response Lifecycle And Decoding

Callers own successful response bodies returned by `Client.Do` and must close
them. `Client.Close` is a final safety net for bodies still active at client
shutdown; it is not a replacement for closing each response promptly.

The ownership contract is consistent across every exit path:

| Path | Body owner and action |
| --- | --- |
| Final network, cache-hit, or synthetic response | Caller closes, decodes, transfers, or drains it. |
| Response middleware replacement | Pipeline closes the superseded body; caller owns the returned replacement. |
| Retry-discarded response | Retry middleware boundedly drains and closes it before another attempt. |
| Redirect-intermediate response | The standard `net/http` client closes it; caller owns only the final response. |
| `DecodeResponse` or `DecodeJSONResponse` | Helper consumes ownership and always closes it. |
| Rejected `ClassifyResponse` | Helper boundedly drains and closes it; accepted bodies stay caller-owned. |
| `CopyResponse` and file/resume helpers | Helper consumes ownership and always closes it; destinations remain caller-owned. |
| Request-pool executor | Pool never owns bodies; each executor must finish its direct-operation contract. |

When a caller does not need a returned body, `DrainResponse` boundedly consumes
it for possible connection reuse and always closes it:

```go
err := httpclient.DrainResponse(
	response,
	httpclient.DrainOptions{MaximumBytes: 64 << 10},
)
```

An exactly-at-limit body succeeds when EOF is reached. More data returns
`ResponseDrainLimitError`, matching `ErrResponseDrainLimit`, after a one-byte
probe. Read and close causes remain available through `errors.Is` and
`errors.As` without being rendered. Zero selects the finite 64 KiB default;
explicit limits cannot exceed 16 MiB.

`DecodeJSONResponse` transfers body ownership to a bounded decoder. It always
closes the body before returning, whether decoding succeeds or fails:

```go
type Widget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

widget, err := httpclient.DecodeJSONResponse[Widget](
	response,
	httpclient.DecodeOptions{
		MaximumBodyBytes: 1 << 20,
	},
)
```

The helper streams one JSON document through a finite reader. It does not
silently buffer the complete body. The default maximum is 8 MiB and explicit
bounds may not exceed 1 GiB. Exceeding the bound returns
`ResponseLimitError`, which matches `ErrResponseBodyLimit`.

Vendor-specific codecs use `DecodeResponse` with an explicit media-type
allowlist and a typed `DecodeFunc`:

```go
widget, err := httpclient.DecodeResponse(
	response,
	httpclient.DecodeOptions{
		MaximumBodyBytes:   1 << 20,
		ExpectedMediaTypes: []string{"application/vnd.example.widget"},
	},
	func(reader io.Reader) (Widget, error) {
		return decodeWidget(reader)
	},
)
```

The codec receives only the bounded stream and must consume one complete
representation. Any unread byte is trailing data unless `AllowTrailingData`
is explicit; opted-in trailing bytes are still drained through the same bound.
Unlike the generic helper, `DecodeJSONResponse` uses a JSON decoder to
distinguish legal trailing whitespace from a second or malformed document.
Both helpers consume ownership and always close the body.

When `ContentLength` is declared, reaching EOF with a different byte count
returns `ResponseLengthError`, matching `ErrResponseLength`. An explicit
`Content-Length: 0` is distinct from an omitted unknown length. Length errors
expose only numeric expected and actual counts, and their rendered text contains
neither value. The configured body limit still wins when the peer sends more
than the decoder is allowed to inspect.

## Media types and document boundaries

JSON decoding accepts `application/json` and structured `application/*+json`
by default, including valid parameters such as `charset`. Set
`ExpectedMediaTypes` to require another exact parsed media type. Generic
`DecodeResponse` requires an explicit nonempty allowlist so a custom codec
cannot silently inherit JSON media types.

Trailing JSON values or malformed trailing bytes are rejected. Set
`AllowTrailingData` only for a protocol that deliberately places data after
the first JSON value; the full stream remains bounded and is consumed before
returning.

## Empty responses

`HEAD`, 1xx, 204, 205, and 304 responses are semantically empty and return the
zero value without invoking decoding or declared payload-length validation. An
empty representation for another status returns `ErrEmptyResponseBody` unless
`AllowEmpty` is explicit.

Status classification is intentionally independent from body decoding. A
vendor client decides which statuses are successful before selecting its
success or error codec; the decoder does not assume every 2xx response has the
same schema or that every non-2xx response is JSON.

## Errors and sensitive data

`ResponseDecodeError` preserves codec and reader causes through `errors.Is` and
`errors.As` without rendering them, because decoder errors can contain response
data. `ResponseBodyError` applies the same rule to close failures.
`UnexpectedContentTypeError` exposes only parsed media-type values, never the
URL, query, headers, credentials, or response body.

## Status classification and vendor errors

Classify status before selecting a success or error decoder:

```go
err := httpclient.ClassifyResponse(response, httpclient.StatusOptions{
	MaximumExcerptBytes: 4 << 10,
	RedactExcerpt: func(content []byte) ([]byte, error) {
		return redactVendorPayload(content), nil
	},
	Retryable: func(status int, _ http.Header) bool {
		return status == http.StatusTooManyRequests || status >= 500
	},
	MapVendorError: func(snapshot httpclient.StatusSnapshot) (string, error) {
		return extractVendorCode(snapshot.Excerpt), nil
	},
})
```

The default accepts 2xx. An accepted response body remains untouched and
caller-owned for subsequent decoding or streaming. A rejected response is
boundedly drained and always closed.

Safe excerpts are opt-in: `MaximumExcerptBytes` cannot be nonzero without a
redactor, redactor expansion beyond the bound is rejected, and only the
redacted snapshot reaches vendor mapping. Draining remains independently
bounded for connection reuse. Reader, redactor, mapper, and close causes are
preserved without being rendered.

`HTTPStatusError` exposes cloned headers, status, bounded redacted excerpt,
configured vendor code, retryability, and the first allowlisted request ID.
The default request-ID headers are `X-Request-ID` and `X-Correlation-ID`;
applications can provide an explicit ordered list. Error text contains none of
those values.
