# Security Policy

## Supported Versions

Before `v1.0.0`, security fixes are applied to the latest revision of
`main`. After the first stable release, supported release lines and
end-of-support dates will be documented here.

## Reporting A Vulnerability

Use GitHub private vulnerability reporting for this repository. Include a
minimal reproducer, expected and observed behavior, affected versions, impact,
and any suggested mitigation. Do not include secrets or production data.

## Response Process

Maintainers will acknowledge the report, reproduce and assess it privately,
coordinate a fix and advisory, and credit the reporter when requested. Public
disclosure should wait until a fix or agreed mitigation is available.

## Package Security Boundary

Requests, responses, parameters, IDs, batches, transports, and handler error data are untrusted protocol inputs. Body and batch limits are part of the maintained security boundary.

## Application Responsibilities

Applications remain responsible for transport limits, authentication,
authorization, rate limiting, deadlines, secret handling, deployment policy,
and business-level validation. Package safeguards do not replace those
controls.

See [docs/security.md](docs/security.md) and
[docs/hardening.md](docs/hardening.md) for adoption guidance and maintained
evidence.
