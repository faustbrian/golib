# Goal: OpenID Connect ID Token Validation

## Objective

Build `oidc` as a standards-conformant OpenID Connect discovery and ID-token
validation module. It MUST compose strict JWT verification with OIDC issuer,
audience, authorized-party, nonce, and provider metadata rules without owning
OAuth authorization flows, sessions, HTTP middleware, or authorization.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Follow OpenID Connect Core and Discovery for exact issuer matching,
  discovery location, metadata validation, signing algorithms, JWKS retrieval,
  audiences, `azp`, temporal claims, subject, nonce, and optional token hash
  validation when exposed.
- Require HTTPS by default; make loopback/development exceptions explicit.
- Bound discovery and JWKS redirects, bodies, decompression, refresh, caching,
  initialization, and request duration.
- Synchronize metadata/key refresh, handle rotation and provider outage with a
  documented fail-closed/stale-cache policy, and avoid fleet refresh storms.
- Keep nonce replay storage caller-owned through a narrow context-aware contract.
- Never log or expose tokens, claims, nonce values, keys, credentials, or
  arbitrary provider response text.
- Define immutable configuration, concurrent use, cancellation, and resource
  lifetime precisely.

## Interoperability And Documentation

Use OIDC conformance vectors and representative standards-compliant providers.
Document supported profiles and deliberate exclusions, API, setup, nonce
ownership, cache/rotation behavior, security, examples, adoption, FAQ,
compatibility, and migration.

## Completion Gates

CI MUST enforce strict static checks, race and fuzz testing, security and
dependency scanning, API compatibility, conformance, interoperability,
benchmarks, docs, exactly 100% statement coverage, and exactly 100% of viable
mutants killed by meaningful tests.
