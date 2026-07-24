# Signature scheme v1

`v1` supports `sha256` and `sha512`. It signs a labeled, newline-delimited
sequence of: version, algorithm, Unix timestamp seconds, nonce, key ID,
exact case-sensitive method, escaped path, canonical query, lowercase host, fixed
`Content-Type` and `Idempotency-Key` values, SHA-256 body digest, and canonical
metadata. Variable byte fields are unpadded base64url,
so embedded delimiters cannot alter the grammar. Query keys use Go URL parsing
and lexical ordering; duplicate values retain wire order because Go's
`Query().Get` observes the first value.
Metadata keys are sorted; key and value bytes are independently base64url
encoded without padding.

The HTTP header is a structured, single-value-per-line field:

```text
Webhook-Signature: v1;algorithm=sha256;keyid=<base64url>;timestamp=<unix>;nonce=<base64url>;signature=<base64url>
```

Unknown versions or algorithms, duplicate parameters, padding, whitespace,
invalid encodings, timestamp disagreement, and duplicate complete signature
values are rejected. Comparisons use `hmac.Equal`. The timestamp is covered by
the MAC and checked against the verifier-owned clock and inclusive tolerance.
Caller timestamps are compared at the protocol's Unix-second precision.
The nonce is also covered by the MAC, bounded to 128 UTF-8 bytes, and generated
once per signing operation. `SignerConfig.NonceGenerator` supports
deterministic tests; the default uses `crypto/rand`.

Timestamps are non-negative Unix seconds. Signing keys are selected at the
normalized signed timestamp, not at a separate wall-clock read, so historical
fixtures and delayed signing cannot disagree with verifier rotation windows.
Configurations whose `NotAfter` precedes `NotBefore` are rejected.

The two fixed headers are each limited to one UTF-8 value of at most 256 bytes
and are validated before the body is read. This prevents content decoding or
delivery deduplication semantics from being changed without invalidating the
signature. Trace propagation headers are intentionally excluded because the
HTTP telemetry transport injects them after signing.

The exact normative vectors are `testdata/vectors/v1.json`. They were produced
independently by Python's standard `hmac`, `hashlib`, and URL primitives and
are checked by `scripts/check_interoperability.py`. Any canonicalization or
header change requires a major version and new vector version.
