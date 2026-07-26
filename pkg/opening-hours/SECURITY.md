# Security policy

## Supported versions

Until v1, only the latest commit on `main` receives security fixes. After v1,
the latest minor release is supported.

## Reporting

Report vulnerabilities through GitHub private vulnerability reporting. Do not
open a public issue containing exploit details, customer schedules, credentials,
or database contents.

Include the affected version, safe reproduction, impact, and suggested
mitigation. Maintainers will acknowledge a report within five business days.

The package has no network transport, exporter, background worker, global
registry, unsafe code, or cgo. See the detailed [security model](docs/security.md).
