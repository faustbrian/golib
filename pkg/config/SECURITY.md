# Security policy

## Supported versions

Security fixes are applied to the latest released minor line. Before the first
stable release, only the latest release is supported.

## Reporting a vulnerability

Use GitHub private vulnerability reporting for this repository. Do not open a
public issue containing secrets, credentials, exploit details, or vulnerable
deployment information. Include the affected version, configuration source,
minimal reproduction, impact, and any known mitigations.

## Scope

Secret disclosure, path traversal, symlink/root escape, parser resource
exhaustion, partial snapshot publication, unsafe optional-source suppression,
and validation or interpolation bypasses are security relevant. The package
does not claim physical memory zeroization: Go strings and garbage collection
cannot provide that guarantee.

See the full [security model](docs/security.md).
