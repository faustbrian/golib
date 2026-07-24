# Security policy

## Supported versions

| Release line | Status | End of support |
| --- | --- | --- |
| 1.0.x | Supported | Not scheduled |
| < 1.0.0 | Unsupported | 2026-07-16 |

Security fixes are applied to the latest patch release in each supported line.
An advisory will identify affected versions and any change to this support
window. The `main` branch is development state, not a supported release.

## Reporting a vulnerability

Use GitHub private vulnerability reporting for this repository. Include a
minimal reproducer, affected version, impact, and mitigation. Do not include
production DSNs, credentials, query arguments, customer data, or certificates.

## Security boundary

DSNs, certificates, hooks, SQL, arguments, PostgreSQL errors, and telemetry
exporters cross trust boundaries. The package prevents its own validation and
startup errors from echoing DSNs, never emits SQL or arguments through its
observation API, and treats server `Detail` and `Hint` fields as sensitive.

Applications remain responsible for secret storage, certificate issuance,
network policy, PostgreSQL roles, row-level security, statement policy,
migrations, backups, authentication, authorization, and workload deadlines.

See [docs/security.md](docs/security.md) and
[docs/hardening.md](docs/hardening.md) for the threat model and evidence.
