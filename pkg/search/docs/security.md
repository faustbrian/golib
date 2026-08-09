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
