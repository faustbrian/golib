# Security

Authorize tenant, logical index, operation, field projection, filters, raw
extensions, and lifecycle resources before network access. Never derive a
physical index name directly from untrusted input. The OpenSearch adapter uses
an explicit resolver and a separate lifecycle authorizer.

Use TLS, least-privilege credentials, credential rotation, bounded trusted node
discovery, and explicit proxy policy. Treat queries, sources, highlights,
suggestions, diagnostics, cursors, task IDs, and backend error bodies as
sensitive. Errors and telemetry must expose classifications rather than raw
secret-bearing payloads.

Signed cursors provide integrity and query binding, not confidentiality. Keep
cursor payloads free of secrets and rotate keys with an explicit compatibility
window. Do not expose unrestricted backend DSL to untrusted callers.

Keep JSON depth and node limits appropriate to the application schema. Source,
settings, and mapping byte limits alone do not prevent deeply nested documents
or mapping explosion. The core rejects duplicate object keys before canonical
encoding or backend access. Physical index definition names reject whitespace,
control characters (including NUL), reserved punctuation, unsafe prefixes, and
uppercase letters while retaining valid lowercase Unicode names.
