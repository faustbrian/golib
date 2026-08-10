# Security policy

Report suspected vulnerabilities privately through the repository security
contact rather than a public issue. Include the affected workflow or PostgreSQL
API, impact, reproduction, and whether durable history or credentials may have
been exposed. Do not include live secrets or customer payloads.

Supported versions follow released module tags. Pre-release revisions receive
fixes on a best-effort basis.

The package assumes trusted workflow definitions, activities, compensation
handlers, and operator authorization. It does not sandbox code, provide tenant
authorization, encrypt payloads, manage database credentials, or make external
effects exactly once. Applications must enforce those controls and must redact
driver, activity, signal, history-export, and hook data before logging or
telemetry emission.

Security-sensitive invariants include bounded inputs and results, immutable
definition fingerprints, commit-before-progression, stable idempotency keys,
unknown-outcome reconciliation, lease fencing, audited operator commands, and
truthful compensation state. A report that can violate one of these invariants
is considered a security or integrity issue even without code execution.
