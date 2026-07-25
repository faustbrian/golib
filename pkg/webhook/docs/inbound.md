# Inbound verification and raw bodies

Verification must be the first component that consumes the request body.
`CaptureBody` rejects invalid limits, rejects a known oversized
`Content-Length` before reading, reads at most `limit+1`, closes the original,
and restores a new reader containing the exact bytes. It does not decompress,
decode, normalize, or trust forwarded headers.

Configure strict `HeaderLimits`, a trusted clock, tolerance, and verification
keys. `VerifyRequest` parses headers before reading the body. Authentication
occurs before event-ID extraction or replay storage. Middleware returns only a
fixed safe status and message; detailed categories are available through
typed errors and secret-safe observations.

`Content-Type` and `Idempotency-Key` are fixed signed fields. Duplicate,
oversized, non-UTF-8, or line-breaking values are rejected before body reads.

Empty and chunked bodies are supported within the bound. A body consumed by
earlier middleware cannot be recovered; place verification first. HTTP
content-coding is signed as received. Trailer values are not part of `v1`.
Decode and validate application payloads only from
`VerifiedBodyFromContext(ctx)`.

Rotation uses overlapping validity windows. Emit with the new and old active
keys, deploy recipient verification for both, then revoke the old key after
the maximum delivery and replay window. Key IDs are public routing labels,
not secrets.
