# Security Policy

## Supported versions

No version has been published yet. Until v1, security fixes land on the default
branch and remain part of the unreleased candidate. After v1 is published, the
latest v1 release receives fixes through supported patch releases.

## Reporting

Report vulnerabilities privately through GitHub's security advisory workflow.
Do not open a public issue containing credentials, payloads, exploit details,
or tenant data.

## Operational responsibilities

- Restrict database roles to the required schema and statements.
- Treat payloads and metadata as sensitive; do not include them in logs,
  metrics, traces, or support tickets.
- Authorize replay and retention outside the library and audit every operator.
- Use TLS and authenticated connections to PostgreSQL and publishers.
- Keep consumers idempotent and bound their own retries.
- Require injected publishers, health checks, heartbeats, replay authorizers,
  and archive hooks to honor context cancellation and enforce finite I/O
  deadlines.

Lease tokens prevent stale transitions but do not authorize callers. Replay
request fields provide audit context but do not replace application access
control.

Go cannot safely terminate a callback that ignores its context. A stuck
publisher can delay shutdown, and a stuck archive hook can retain transaction
locks. Treat callback liveness as application security and availability policy.
