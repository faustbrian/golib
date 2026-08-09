# Security policy

## Supported versions

Before v1, pin an exact tenancy module version and review every upgrade. After
v1, only versions explicitly listed in repository release notes are supported.

## Reporting

Report suspected vulnerabilities through GitHub's private security-advisory
workflow. Do not include tenant IDs, customer data, namespace HMAC keys,
credentials, database URLs, or unredacted request and event metadata. Include
the affected version, trust topology, enforcement seams, and a sanitized
reproduction.

## Boundary

This module transports and asserts explicit tenant routing identity. It does
not authenticate callers, authorize tenant membership, grant database roles,
or protect consumers that bypass its context, propagation, namespace, and
persistence seams. Applications own authorization, credential management,
broker and proxy policy, PostgreSQL roles, audit retention, and incident
response.
