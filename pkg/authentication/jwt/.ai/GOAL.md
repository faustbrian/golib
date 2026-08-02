# Goal: Strict JWT Validation

## Objective

Build `jwt` as the strict JWT/JWS/JWK validation component for
`authentication`. It MUST validate signed tokens against explicit policy and
remote or local key providers without owning HTTP authentication, OIDC flows,
token issuance, sessions, or authorization.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Enforce an explicit algorithm allowlist, key type/algorithm compatibility,
  signature verification, issuer, audience, subject policy, expiration,
  not-before, issued-at, leeway, required claims, and bounded token size.
- Reject `none`, algorithm confusion, duplicate or malformed claims, invalid
  numeric dates, ambiguous keys, unknown critical headers, and unsupported
  serialization forms unless deliberately specified.
- Provide deterministic clocks and narrow key-provider contracts.
- Support bounded HTTPS JWKS retrieval with body limits, refresh bounds,
  cache-control handling, key rotation, synchronized refresh, cancellation,
  and an explicit stale-key/outage policy.
- Redact token, signature, key, claim, endpoint-query, and remote error data.
- Define concurrency, cache lifetime, close behavior, and whether cancellation
  stops waiting or remote work.
- Preserve standards-backed errors through stable categories and
  `errors.Is`/`errors.As` without string matching.

## Interoperability And Documentation

Test current JOSE/JWT RFC vectors and major conforming libraries with identical
keys, algorithms, claims, and clocks. Document API, supported and rejected
algorithms, remote-key policy, examples, adoption, FAQ, security,
compatibility, and migration guidance.

## Completion Gates

CI MUST enforce formatting, vet, strict static analysis, race tests, fuzzing,
security and dependency checks, API compatibility, interoperability,
benchmarks, docs, exactly 100% statement coverage, and exactly 100% of viable
mutants killed by meaningful tests.
