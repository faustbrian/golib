# Security policy

## Supported versions

Security fixes are made on the latest supported major version. Before the
first tagged release, the current `main` branch is the supported line.

## Reporting

Report suspected vulnerabilities privately to the repository owner. Include a
minimal reproducer, affected version, expected invariant, and observed impact.
Do not attach customer records, account identifiers, or production monetary
payloads.

## Threat model

Untrusted inputs may attempt excessive digits, scales, ratios, allocation
counts, JSON nesting, locale expansion, or diagnostic amplification. Public
parsers and adapters apply fixed bounds before or during expensive work.

The package performs no network access, loads no ambient currency rates, uses
no unsafe code, and logs no monetary source records. Callers remain responsible
for database authorization, transport authentication, rate provenance, and
business-level amount limits narrower than the package maximums.

See [docs/security.md](docs/security.md) for the full boundary inventory.
