# Privacy and redaction

Redaction runs before persistence. Recorder surfaces redaction failure through
an opaque safe diagnostic; it preserves cancellation and deadline sentinels but
does not retain arbitrary redactor errors that may contain rejected data.
Standard rules default-deny attributes and changes unless allowlisted and can
remove safe description, network origin, and user agent.

Authorization headers, cookies, passwords, credentials, secrets, tokens, and
unrestricted request or response bodies are rejected by field-name policy.
Callers must still minimize values, pseudonymize identifiers where lawful,
define retention and erasure exceptions, apply legal holds, restrict privileged
fields, and prevent canonical records from reaching logs, traces, metrics,
support bundles, or error strings.

Actor, subject, tenant, correlation, and record IDs are never observation
fields and must not be metric labels. Trace identifiers may be record context
only when policy permits; the audit record remains distinct from the trace.
