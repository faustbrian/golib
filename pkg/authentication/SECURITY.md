# Security policy

## Supported versions

Before v1, security fixes are applied to the latest minor release. After v1,
the latest major release receives fixes unless a longer window is announced.

| Version | Supported |
| --- | --- |
| Unreleased | Yes |

## Reporting a vulnerability

Do not open a public issue. Use GitHub private vulnerability reporting for
`faustbrian/authentication`. Include the affected version, reproduction,
realistic impact, possible credential exposure, and any embargo constraints.
Expect acknowledgement within five business days.

## Threat model

The library treats credentials, tokens, headers, issuer responses, JWK sets,
claims, callbacks, clocks, and instrumentation as potentially malformed or
unavailable. It protects against:

- credential disclosure through default formatting, HTTP errors, logs, spans,
  and metrics;
- timing leaks from direct static-secret equality checks;
- credential smuggling through duplicate headers, query parameters, cookies,
  or multiple enabled sources;
- JWT algorithm confusion, missing or ambiguous key IDs, invalid JWK metadata,
  unsupported critical headers, issuer/audience mismatch, and invalid time
  claims;
- OIDC discovery redirects, oversized discovery or JWK responses, ambiguous
  keys, unknown-key refresh, and issuer outage for already cached keys;
- unbounded tokens, claims, claim depth, claim collections, JWK counts, HTTP
  bodies, refresh intervals, and initialization waits;
- caller mutation of authenticated principal data;
- instrumentation failure changing authentication behavior.

The library does not protect against compromised process memory, stolen source
configuration, a malicious validation callback that returns a false identity,
authorization mistakes after authentication, replay when the application does
not validate nonce or credential lifetime, or transport security disabled by
the caller. Plain HTTP options are for isolated development only.

The detailed [threat model](docs/security/threat-model.md),
[audit findings](docs/security/findings.md),
[test matrices](docs/security/test-matrices.md), and
[authoritative vectors](docs/security/vectors.md) are maintained with the
source. The [adoption checklist](docs/adoption.md) maps these controls to a
deployment.

## Secret handling

Never log raw credentials, token claims, authorization headers, query strings,
or cookies. Keep callback error messages secret-free even though public
`Failure.Error` does not render wrapped causes. Prefer fixed diagnostic
messages and bounded identifiers. Rotate API keys by overlapping old and new
entries briefly, then remove the old entry atomically.

Query credential extraction is retained only as an explicit compatibility
option and is deprecated for new designs. A query secret may already have
crossed browser-history, proxy, and access-log boundaries before extraction;
this package cannot undo that disclosure.

## Dependency and release policy

The default module has no external runtime dependencies. Optional modules pin
their protocol and telemetry dependencies. CI runs vulnerability and dependency
review, exact coverage, race, fuzz smoke, API compatibility, and reproducible
archive checks. Release tags are annotated and never force-updated.

OIDC remote refresh is synchronous, single-flight, capacity-bounded,
cancellation-aware, freshness-bounded, and subject to a failure cooldown. JWT
remote resources own admitted work and cancel and drain it during bounded
shutdown.
