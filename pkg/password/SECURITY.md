# Security policy

## Supported versions

Before v1, fixes are applied to the current unreleased line. After v1, the
latest major version receives security fixes unless a longer window is stated.

| Version | Supported |
| --- | --- |
| Unreleased | Yes |

## Reporting a vulnerability

Do not open a public issue. Use private vulnerability reporting for the
repository. Include the affected version, synthetic reproduction, realistic
impact, suspected password/hash exposure, and embargo constraints. Do not send
real credentials or production hashes.

## Security boundary

The package protects parsing, resource admission, primitive invocation,
classified outcomes, safe diagnostic formatting, and explicit upgrade data. It
does not protect a compromised process, malicious collaborators, application
logging of raw inputs, user enumeration at the endpoint, insecure transport,
weak application password policy, database compromise, or authorization after
authentication.

Only maintained `golang.org/x/crypto/argon2` and
`golang.org/x/crypto/bcrypt` primitives are used. There is no unsafe, cgo,
assembly, reversible storage, custom password primitive, or default pre-hash.

See the [threat model](docs/threat-model.md),
[secret-handling guide](docs/secret-handling.md), and
[security review packet](docs/security-review.md).
