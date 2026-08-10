# Privacy and redaction

Redaction runs before persistence. Recorder surfaces redaction failure through
an opaque safe diagnostic; it preserves cancellation and deadline sentinels but
does not retain arbitrary redactor errors that may contain rejected data.
Standard rules default-deny attributes and changes unless allowlisted and can
remove safe description, network origin, and user agent.

A custom redactor may transform only those privacy fields, may not inject new
attribute or change keys, and must preserve record identity, actor, tenant,
subject, action, outcome, time, correlation, policy, and explicit change-state
semantics. Redaction that would alter an already sealed canonical record is
rejected; redact before integrity sealing. Persistence adapters reject records
that have not crossed an explicit redaction boundary.

Authorization headers, cookies, passwords, API keys, credentials, secrets,
tokens, and unrestricted request or response bodies are rejected by field-name
policy.
Authentication method is a bounded credential-free label containing only
letters, digits, `.`, `_`, or `-`; pass the mechanism name, never an
authorization header or credential value.
All durable text rejects NUL because PostgreSQL cannot represent that code
point in JSON text; other valid UTF-8 remains supported within field limits.
Callers must still minimize values, pseudonymize identifiers where lawful,
define retention and erasure exceptions, apply legal holds, restrict privileged
fields, and prevent canonical records from reaching logs, traces, metrics,
support bundles, or error strings.

Actor, subject, tenant, correlation, and record IDs are never observation
fields and must not be metric labels. Trace identifiers may be record context
only when policy permits; the audit record remains distinct from the trace.
