# Security model

Tenant IDs are opaque routing data, not secrets and not authorization claims.
Their raw representation is nevertheless redacted from ordinary formatting and
structured `log/slog` values to reduce enumeration and high-cardinality
disclosure. Applications decide who may create tenant and system scopes.

The package protects only paths using its explicit seams. It cannot stop raw
SQL, provider clients, caches, telemetry APIs, or goroutines that bypass them.
HMAC namespace keys require an application-owned random key of at least 32
bytes. Rotate keys with an explicit versioned dual-read migration; losing the
key makes prior namespaces unreachable.

Primary threats are spoofed forwarding metadata, conflicting sources, confused
deputies, stale pooled sessions, ambiguous string keys, cross-tenant retries,
tenant enumeration, privileged RLS bypass, and retained scoped contexts. The
runtime rejects ambiguity and malformed scope, while application authentication,
authorization, broker policy, database roles, and audit retention remain outside
this package.
