# Request Construction And Serialization

`RequestSpec` is an immutable reusable description that produces ordinary
`*http.Request` values. It does not wrap or replace the standard request after
construction.

## URL resolution

`NewRequestSpec` accepts an absolute `http` or `https` base URL and one relative
reference. User information is forbidden. Absolute and network-path references
are rejected even when they appear to select the same origin, so an endpoint
definition cannot silently replace the configured authority.

Resolution follows RFC 3986 through `url.URL.ResolveReference`. A trailing slash
therefore matters:

| Base | Reference | Result path |
| --- | --- | --- |
| `https://api.example/v1/` | `widgets` | `/v1/widgets` |
| `https://api.example/v1` | `widgets` | `/widgets` |
| `https://api.example/v1/` | `../health` | `/health` |

Escaped path data is preserved. For example, `widgets/a%2Fb` has the decoded
path `/v1/widgets/a/b` and escaped path `/v1/widgets/a%2Fb`. Query parameters
are canonicalized at build time, and fragments remain available through the
standard URL even though HTTP transports do not send fragments.

## Metadata precedence

Headers and query parameters use one explicit low-to-high precedence order:

1. `LayerClient`
2. `LayerEndpoint`
3. `LayerRequest`
4. `LayerAuthentication`
5. `LayerSigning`
6. `LayerOneShot`

`WithHeader` and `WithQuery` replace the same name from lower layers.
`AddHeader` appends separate field values at one layer; it never comma-folds
them. `WithoutHeader` and `WithoutQuery` are tombstones that remove inherited
values. A later, higher layer can add the name again.

Trailers use the same precedence through `WithTrailer`, `AddTrailer`, and
`WithoutTrailer`, but remain separate from initial headers. Trailer names and
values are snapshotted and every build receives an independent trailer map.
Framing, routing, credential, cookie, and representation metadata such as
`Content-Length`, `Transfer-Encoding`, `Host`, `Authorization`, `Cookie`, and
`Content-Type` is prohibited in trailers.

Header names must be valid HTTP tokens. Header values reject control bytes
other than horizontal tab, which prevents line injection before a request
reaches `net/http`.

## Query values

Parameter names are sorted by their Unicode byte representation. Repeated value
order is preserved. Names and values are percent-encoded structurally, with
spaces represented as `%20`, so custom encoders cannot inject separators.

| API | Example output |
| --- | --- |
| `RepeatedQuery("a", "b")` | `name=a&name=b` |
| `QueryValues(QueryRepeated, "a", "b")` | `name=a&name=b` |
| `QueryValues(QueryCommaDelimited, "a", "b")` | `name=a%2Cb` |
| `QueryValues(QuerySpaceDelimited, "a", "b")` | `name=a%20b` |
| `QueryValues(QueryPipeDelimited, "a", "b")` | `name=a%7Cb` |
| `DeepObjectQuery(map[string]string{"role": "admin"})` | `name%5Brole%5D=admin` |
| `NullQuery()` | `name` |
| `RepeatedQuery("")` | `name=` |
| `RepeatedQuery("0")` | `name=0` |
| `RepeatedQuery()` | omitted |
| `WithoutQuery(...)` | inherited value removed |

`CustomQuery` accepts a `QueryEncoder` that returns structured `QueryPart`
values. The package escapes every part, preserves the returned part order, and
returns `QueryEncodingError` when the encoder fails. The encoder owns the
semantic ordering of its parts and must be deterministic and safe for
concurrent use.

Query and body error messages omit values and underlying causes that may contain
payload data. Causes remain available through `errors.Is`, `errors.As`, and
`errors.Unwrap`.

## Request bodies

`NewBytesBody` snapshots caller bytes immediately. Every build and every
`GetBody` call receives a new reader over that snapshot.

`NewFormBody` snapshots `url.Values` as
`application/x-www-form-urlencoded`. Keys are ordered canonically, repeated
values retain their slice order, spaces use `+`, structural separators are
percent-encoded, empty strings remain `name=`, and nil or empty value slices
are omitted. A nil map produces an explicit replayable zero-length form body.

`NewReplayableBody` accepts a `BodyOpener`. The opener must return a fresh,
independent reader for the initial request and every replay. The declared
content length uses standard `http.Request` semantics: `-1` is unknown, and
zero or greater is explicit.

`NewStreamingBody` is intentionally one-shot. Ownership remains with the caller
until the first build attempt opens the body. A successful build transfers the
reader to the standard request, whose body must be closed. If later request
construction fails, the package closes the reader and it remains consumed.
Further builds return `ErrBodyConsumed`, and `GetBody` remains nil, preventing
automatic retry or redirect replay.

`NewMultipartBody` composes those same body policies into deterministic
`multipart/form-data` output. The caller supplies a stable boundary, explicit
part names, optional filenames and headers, and a finite total-byte limit:

```go
metadata, err := httpclient.NewBytesBody(
	"application/json",
	[]byte(`{"name":"widget"}`),
)
if err != nil {
	return err
}

upload, err := httpclient.NewReplayableBody(
	"application/octet-stream",
	fileSize,
	func() (io.ReadCloser, error) { return os.Open(fileName) },
)
if err != nil {
	return err
}

body, err := httpclient.NewMultipartBody(httpclient.MultipartOptions{
	Boundary:     "integration-widget-upload-v1",
	MaximumBytes: 32 << 20,
	Parts: []httpclient.MultipartPart{
		{Name: "metadata", Body: metadata},
		{Name: "attachment", FileName: "widget.bin", Body: upload},
	},
})
```

The multipart body is replayable only when every part is replayable. Its
content length is exact only when every part declares an exact length;
otherwise it is `-1` and the streaming writer enforces `MaximumBytes` while
encoding. A known body that already exceeds the limit is rejected during
construction. Part readers are opened before streaming starts, owned by the
multipart body after opening, closed exactly once, and joined with the encoder
worker when the returned reader closes. A declared part that produces fewer or
more bytes fails with `ErrMultipartPartLength`; a total overflow fails with
`ErrMultipartLimit`.

Part metadata rejects control characters and bounded-size violations. The
multipart encoder owns `Content-Disposition`, `Content-Type`, `Content-Length`,
and `Transfer-Encoding`; callers cannot override those fields through part
headers. `MultipartError` retains dependency causes for `errors.Is` and
`errors.As` without rendering payload-bearing cause text.

A body content type is a low-precedence default. Any layered `Content-Type`
header replaces it, and `WithoutHeader` can suppress it. When request
construction fails after opening a body, the package closes the opened reader.

Custom `RequestBody` implementations must be concurrency-safe when they report
themselves replayable. Typed-nil implementations are rejected rather than
invoked.

Trailers require a body. Because the standard HTTP/1.1 transport sends trailers
only with trailer-capable framing, a trailer-bearing built request reports
`ContentLength == -1` even when its body has a known length. A replayable body
still receives `GetBody`; the static trailer snapshot is cloned with each
request and retry.

## Aliasing and ownership

Every wither copies mutable header, query, deep-object, and URL state. Every
build creates a separate URL and header map. Mutating one built request cannot
change its specification, a derived specification, or another built request.

The caller owns every successfully returned request body and response body.
Closing the top-level `Client` also closes response bodies still registered with
that client; request bodies continue to follow the standard transport contract.
