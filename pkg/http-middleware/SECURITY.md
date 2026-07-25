# Security policy

## Supported versions

Security fixes are provided for the latest tagged major version. Before v1,
only the latest release is supported.

## Reporting

Use GitHub private vulnerability reporting for this repository. Do not open a
public issue containing an exploit, credential, private address, or production
payload. Include the affected version, middleware order, minimal reproduction,
and whether headers or a response were already committed.

## Deployment boundary

The package assumes no production access and performs no network calls. It does
not replace a hardened `http.Server`, ingress request limits, TLS, proxy header
sanitization, authentication, authorization, CSRF protection, or application
validation. Forwarded data is ignored unless the direct peer matches an
explicit trusted network.

See [docs/security.md](docs/security.md) and
[docs/threat-model.md](docs/threat-model.md).
