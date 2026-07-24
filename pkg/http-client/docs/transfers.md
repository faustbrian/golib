# Streaming Transfers

`CopyResponse` streams a response body into a caller-provided writer without
buffering the complete representation:

```go
expected, err := hex.DecodeString(expectedSHA256)
if err != nil {
	return err
}

result, err := httpclient.CopyResponse(
	ctx,
	response,
	destination,
	httpclient.TransferOptions{
		MaximumBytes:    512 << 20,
		DigestAlgorithm: httpclient.DigestSHA256,
		ExpectedDigest:  expected,
	},
)
```

The helper takes ownership of `response.Body` and always closes it. The
destination remains caller-owned and is never closed. A successful result
reports transferred bytes, elapsed time, and an independent digest snapshot.

`MaximumBytes` is mandatory in effect: zero selects the finite 1 GiB default,
and the largest explicit bound is 1 TiB. `ExpectedBytes` can require a protocol
length; zero uses a nonnegative response `ContentLength`, while -1 explicitly
disables length validation. A known expected length larger than the maximum is
rejected before copying.

SHA-256 and SHA-512 are the supported digest algorithms. Expected digests must
have the exact algorithm length and are compared in constant time. Digest
values never appear in error strings.

## Progress and cancellation

Progress observers receive immutable snapshots outside internal locks. The
first snapshot reports zero bytes and the final snapshot has `Complete` set.
Intermediate callbacks require both `ProgressBytes` and `ProgressInterval` to
be reached, preventing callback storms for either very fast or very small
reads. Defaults are 64 KiB and 100 ms. A fake `TransferClock` makes callback
timing deterministic in tests.

The operation checks context before every source read. Cancellation cannot
interrupt an arbitrary writer blocked inside its own `Write`; callers needing
that behavior must provide a context-aware destination. Observer failures stop
the transfer immediately.

## Errors and partial destinations

Limit, expected-length, and digest failures use `TransferLimitError`,
`TransferLengthError`, and `DigestMismatchError`. Reader, writer, observer,
cancellation, and close failures use `TransferError`, whose text does not
render the underlying error because it can contain destination paths or body
data.

A failed copy may leave partial bytes in the caller’s writer. Use
`CopyResponseToFile` when a destination path must become visible only after
complete validation:

```go
result, err := httpclient.CopyResponseToFile(
	ctx,
	response,
	destination,
	httpclient.FileTransferOptions{
		Mode: 0o640,
		Transfer: httpclient.TransferOptions{
			MaximumBytes:    512 << 20,
			DigestAlgorithm: httpclient.DigestSHA256,
			ExpectedDigest:  expected,
		},
	},
)
```

The helper creates a restricted same-directory temporary file, streams and
validates into it, syncs and closes it, atomically renames it over the
destination, then syncs the directory. A failure before rename removes the
temporary file and leaves any existing destination unchanged. Filesystem error
text never includes the destination or temporary path.

## Range and resume protocol

`WithRange` clones a `GET` or `HEAD` request and applies a bounded byte range:

```go
ranged, err := httpclient.WithRange(request, httpclient.RangeOptions{
	Offset: partialBytes,
	Validator: httpclient.RangeValidator{
		ETag: `"strong-representation-tag"`,
	},
})
```

Only strong ETags or one `Last-Modified` date can be used as `If-Range`.
Request bodies, unsafe methods, negative offsets, overflowing lengths, weak
ETags, and ambiguous validators are rejected.

After the exchange, `ValidateRangeResponse` parses `Content-Range`, checks the
offset, requested length, response content length, and representation
validator, then returns one explicit disposition:

- `RangeContinue` permits appending a validated 206 body;
- `RangeRestart` permits replacing partial data after an allowed 200 fallback;
- `RangeComplete` accepts a 416 only when its complete length equals the local
  partial offset.

The validator never reads or closes the response body. This preserves one
clear ownership transition: validation chooses the transfer path, and the
selected copy helper then consumes and closes the body.

`ResumeDownloadToFile` combines those primitives into a persistent partial-file
state machine:

```go
result, err := httpclient.ResumeDownloadToFile(
	ctx,
	client,
	request,
	destination,
	httpclient.ResumeFileOptions{
		Validator: httpclient.RangeValidator{ETag: strongETag},
		Transfer: httpclient.TransferOptions{
			MaximumBytes:    512 << 20,
			ExpectedBytes:   expectedSize,
			DigestAlgorithm: httpclient.DigestSHA256,
			ExpectedDigest:  expectedDigest,
		},
	},
)
```

The default partial path is `destination + ".partial"` and must remain in the
destination directory. A nonempty partial requires a strong ETag or
`Last-Modified` validator. A validated 206 appends; a 200 truncates and restarts
unless `DisableRestart` is set; a matching 416 validates the existing partial.

An append failure or whole-file validation failure truncates back to the prior
safe offset. Successful continuation re-reads the complete partial under the
same length and digest bounds before syncing and atomically publishing it.
Offset-aware progress reports the existing byte count and the final validated
whole-file digest. Restart responses use normal transfer progress from zero.
