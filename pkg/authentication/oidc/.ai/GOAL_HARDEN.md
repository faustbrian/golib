# Goal Harden: OpenID Connect ID Token Validation

## Mission

Prove OIDC validation remains correct under malicious providers, malformed
tokens, key rotation, outages, concurrency, and fleet-scale refresh.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Derive discovery, metadata, ID-token, nonce, and error matrices from current
  OIDC specifications and every public option.
- Test issuer normalization attacks, cross-issuer key substitution, malicious
  discovery URLs, redirects, algorithm downgrade, audience/`azp` ambiguity,
  nonce replay, claim duplication, and clock edges.
- Fuzz provider metadata, JWKS, JWT headers/claims, URLs, Unicode, and numbers
  under strict body, depth, allocation, and execution limits.
- Inject DNS, TLS, partial response, oversized body, bad cache headers, timeout,
  cancellation, key rotation, stale cache, provider rollback, and outage.
- Stress synchronized refresh in one process and jittered refresh across many
  replicas; prove no unbounded goroutines, timers, bodies, or retained tokens.
- Race validation, nonce callbacks, refresh, and cancellation; contain callback
  panic and preserve stable redacted errors.
- Run standards conformance and selected real-provider interoperability with
  exact versioned evidence and no provider-specific weakening.
- Benchmark warm validation, discovery initialization, rotation misses,
  contention, and hostile-input rejection.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests and no unresolved conformance, security,
race, fuzz, or lifecycle finding.
