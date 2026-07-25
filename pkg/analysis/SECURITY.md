# Security policy

## Supported code

The project is pre-v1. Security fixes target the latest released version and
the current `main` branch. Older snapshots may not receive patches. Consumers
should pin an exact release artifact and checksum, then update deliberately.

## Report a vulnerability

Do not include exploit details, repository secrets, private source, or target
diagnostic output in a public issue. Use the repository's private security
reporting facility when available. If private reporting is unavailable, ask a
maintainer for a private contact channel without disclosing the vulnerability.

Include the affected version, rule or command, minimal secret-free
reproduction, impact, and any known mitigation. Reports about analyzer escape,
target-code execution, path disclosure, configuration parsing, report leakage,
artifact integrity, or dependency compromise are in scope.

## Security boundaries

The maintained [security and threat model](docs/security.md) documents trust
boundaries, abuse cases, controls, residual risks, report handling, and release
verification.

`analysis` parses configuration and Go source but does not execute target
code or configuration programs. It does not load untrusted analyzer plugins.
Reports use repository-relative paths and omit source snippets. Configuration,
diagnostics, suppressions, and SARIF artifacts may still reveal package names
or policy decisions and should receive the repository's normal access controls.

The suite complements gosec, govulncheck, CodeQL, dependency review, race
testing, and fuzzing. It is not a sandbox, compiler, malware scanner, or proof
of memory safety.

Release artifacts should be built with `make reproducible`, checksummed, and
signed by the publishing environment when signing is available. Consumers
should verify the published checksum before execution.
