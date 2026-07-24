# Keys, namespacing, versioning, and invalidation

A key space produces backend keys shaped like:

```text
<namespace>:<name>:v<version>:<base64url-sha256(logical-key-bytes)>
```

Raw logical key bytes never appear in the backend key, telemetry, or bundled
observers. Use separate namespaces for applications or environments and a
specific name for each semantic object type.

`KeyEncoder` must be deterministic and collision-free for the application's
logical key domain before hashing. For compound keys, encode lengths and fields
unambiguously; do not concatenate variable fields without separators or length
prefixes.

Increment the key-space version when changing key encoding, value meaning, or
invalidation scope. A version bump provides immediate logical invalidation
without a key scan. Old records then disappear through backend expiration.

Use `Delete` for targeted invalidation after a successful source mutation.
Delete every affected semantic key only after the source transaction commits.
Bulk invalidation returns per-key errors; decide explicitly whether to retry or
fail the mutation workflow.

Never use tenant IDs, email addresses, tokens, or other sensitive values as
namespace/name parts. Those prefix parts are visible. Put all sensitive or
high-cardinality material in the logical key so it is hashed.
