# Security policy

## Supported versions

Until v1 is tagged, security fixes apply to `main`. After v1, the latest minor
release receives fixes; severe issues may receive a patch on the prior minor at
maintainer discretion.

## Reporting

Report vulnerabilities privately through GitHub Security Advisories for this
repository. Do not open a public issue containing exploit details, secrets, or
customer data. Include affected versions, impact, reproduction, and a suggested
fix if available. Maintainers aim to acknowledge reports within five business
days and will coordinate disclosure after a fix is available.

## Security guarantees

Diagnostics never retain or render protected operation results/errors.
Allocation-sized settings have hard bounds. Core contains no production
`unsafe`, cgo, `go:linkname`, finalizers, network control plane, or third-party
dependency. Required security checks are documented in
[docs/verification.md](docs/verification.md).
