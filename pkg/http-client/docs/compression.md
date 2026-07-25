# Compression

The default transport disables `net/http` implicit compression. This keeps
compressed input measurable and makes content decoding an explicit policy.
Without compression middleware, a compressed response remains encoded for the
caller.

```go
compression, err := httpclient.NewCompressionMiddleware(
	httpclient.CompressionOptions{
		Name:                     "vendor-gzip",
		Layer:                    httpclient.MiddlewareClient,
		MaximumDecompressedBytes: 64 << 20,
		MaximumExpansionRatio:    100,
	},
)
```

Register the middleware in `Config.Middleware`. It runs for every physical
attempt, advertises `Accept-Encoding: gzip` when the request does not already
set it, and decodes only `gzip` or `identity`. Other response encodings fail
with `ErrUnsupportedContentEncoding` rather than passing unexpected bytes to a
decoder.

Decoded responses remove `Content-Encoding` and `Content-Length`, set
`ContentLength` to -1, and set `Uncompressed`. The body remains streaming and
caller-owned. Both the decoded output limit and compressed-to-decompressed
ratio are enforced while reading. Limit failures expose finite byte counts in
`DecompressionLimitError` without response content.

## Streaming request compression

Request gzip is opt-in:

```go
compression, err := httpclient.NewCompressionMiddleware(
	httpclient.CompressionOptions{
		Name:                "vendor-gzip",
		Layer:               httpclient.MiddlewareClient,
		CompressRequests:    true,
		MinimumRequestBytes: 4 << 10,
	},
)
```

Known bodies smaller than `MinimumRequestBytes` are unchanged. Unknown-length
bodies are compressed because their final size cannot be known without
buffering. A request with an existing `Content-Encoding` is also unchanged.

Compression streams through an owned pipe; it does not buffer the full request.
Closing the request body closes the source, unblocks the compressor, and waits
for that worker to exit. A replayable source retains `GetBody`, with a fresh
compressor for retries and redirects. A one-shot source remains one-shot, so
compression never makes a request retryable.

Compression runs at attempt transport priority `-100`. Authentication or
signing implemented at request stages observes the uncompressed representation.
Protocols that sign encoded bytes must supply an editor or signing policy that
constructs the encoded representation before signing instead of enabling this
transport compression policy.

`CompressionError` preserves request-source, gzip, checksum, and close causes
without rendering them. This prevents payload-bearing decoder errors from
appearing in ordinary error strings.
