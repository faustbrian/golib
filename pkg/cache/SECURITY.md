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
loads, but there is no cross-process generation fence. Applications that need
global ordering must include source versions or use versioned keys.

The release matrix proves password authentication and certificate-verified TLS
for standalone Redis and Valkey. It does not prove cluster, Sentinel, failover,
replica, or cross-node TLS configuration. Never use `InsecureSkipVerify` in
production, and scope backend ACL users to only the commands and key patterns
the application needs.

The adapters issue `EVAL` (whose script uses `STRLEN`, `EXISTS`, and `GET`),
`SET`, and `DEL`. Scope the backend identity to those commands and the cache's
versioned key prefix. Native-client connection setup may require its own
connection commands; verify the effective ACL in the deployment smoke test.
