# Security policy

## Supported versions

Only the latest released minor version receives security fixes. Before v1,
users must pin an exact version and review every upgrade. After v1, fixes are
backported only when the maintainer explicitly announces a supported branch.

## Reporting

Do not open a public issue for a suspected vulnerability. Use GitHub's private
security-advisory workflow for this repository. Include the affected version,
threat model, reproduction, and whether secrets or untrusted endpoints are
involved. Expect acknowledgement within seven calendar days.

## Boundary

The library protects only the contracts documented in `docs/security.md`.
Operators own secret generation and storage, TLS trust, tenant scoping,
durable replay storage, endpoint allowlisting, application authorization, and
post-verification payload validation. No exactly-once guarantee is made.
