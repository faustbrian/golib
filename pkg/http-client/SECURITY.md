# Security Policy

## Supported versions

Until v1, only the latest commit on `main` is supported. After v1, the latest
minor release of the current major version receives security fixes. Older
release lines may receive fixes at maintainer discretion.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub private
vulnerability reporting for this repository. Include affected versions,
impact, reproduction steps, and any suggested mitigation. Avoid sending live
credentials, customer data, or production traffic captures.

The maintainer will acknowledge a report within seven days, coordinate
validation and remediation privately, and credit the reporter unless they
prefer anonymity. Disclosure timing is coordinated after a fix is available.

## Security contract

The module follows `GO-SAFETY-1`: production code contains no `unsafe`, cgo,
or `go:linkname`. Default clients use finite timeouts. Credentials are
same-origin by default, error and telemetry surfaces exclude payload secrets,
and persisted HTTP fixtures require sanitization. Run `make safety` for module
integrity, vulnerability scanning, and the source audit.
