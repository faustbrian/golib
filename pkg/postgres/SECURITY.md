# Security policy

## Supported versions

| Release line | Status | End of support |
| --- | --- | --- |
| `main` (pre-v1) | Supported development line | First `v1.0.0` release |

No release has been published. Security fixes are applied to `main` until the
first `v1.0.0` tag. An advisory will identify affected revisions and the
supported release lines after publication.

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
