# Security policy

Security reports should be submitted privately through GitHub's security
advisory feature for `faustbrian/clock`. Do not include secrets, production
timestamps, callback payloads, or customer data in a public issue.

Version 1.x receives security fixes while it is the current major release. The
maintainers will acknowledge a report, assess affected versions, coordinate a
fix and advisory, and credit the reporter when requested.

The package has no production network, filesystem, cgo, unsafe, or runtime
patching surface. Resource exhaustion, callback isolation, and process-global
clock mutation are part of the threat model in
[docs/security-model.md](docs/security-model.md).
