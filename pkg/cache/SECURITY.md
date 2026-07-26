# Security policy

## Supported versions

Security fixes are provided for the latest released minor version. Before
`v1.0.0`, only the latest prerelease line is supported.

## Reporting

Do not open a public issue for a suspected vulnerability. Use GitHub's private
security advisory flow for `faustbrian/cache`. Include affected versions,
impact, reproduction steps, and any suggested mitigation. Expect an initial
acknowledgement within seven days.

## Security model

Cache data is reconstructible and is not a durability boundary. Applications
own transport encryption, authentication, authorization, network policy,
credentials, and native-client configuration. Treat backend bytes as untrusted.

The project enforces GO-SAFETY-1: production code contains no `unsafe`, cgo, or
`go:linkname`. Keys and values are excluded from bundled telemetry. Hashed keys
reduce accidental disclosure but are not an encryption mechanism.

Use a unique logical key per tenant and semantic value type. Namespace/name
prefixes are visible and must not contain tenant IDs, email addresses, tokens,
or credentials. Treat SHA-256 as collision-resistant identity, not secrecy.

Do not serve stale authorization, revocation, pricing, balance, or other
security-sensitive state. Same-process invalidation is ordered against active
loads. The Valkey-only `SetIfOwned` capability can protect reconstructible
cache refresh publication by atomically validating an active lease, but it
does not fence durable side effects or ordinary mutations. Applications that
need global business-data ordering must persist source versions or fencing
tokens at the authoritative resource.

The release matrix proves password authentication and certificate-verified TLS
for standalone Redis and Valkey. It does not prove cluster, Sentinel, failover,
replica, or cross-node TLS configuration. Never use `InsecureSkipVerify` in
production, and scope backend ACL users to only the commands and key patterns
the application needs.

The adapters issue `EVAL` (whose scripts use `STRLEN`, `EXISTS`, `GET`, `HGET`,
and `SET`), `SET`, and `DEL`. Scope the backend identity to those commands, the
cache's versioned key prefix, and only the guarded lease hashes needed by the
application. Native-client connection setup may require its own connection
commands; verify the effective ACL in the deployment smoke test.
