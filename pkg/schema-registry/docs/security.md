# Security

Threats include SSRF, redirect credential forwarding, schema bombs, reference
cycles and explosion, oversized responses and payloads, cache poisoning,
compatibility downgrade, concurrent-registration ambiguity, destructive version
deletion, and sensitive schema leakage.

Controls include explicit HTTPS endpoints, injected transports/SDK clients,
endpoint-scoped credentials, total deadlines, bounded retries/concurrency/body
sizes, compile and graph limits, local-only canonicalizers, selector/result
identity validation, negative-cache expiry, explicit stale policy, immutable
bundle verification, and exact fingerprint confirmation before deletion.
Successful remote responses are accepted only when schema content, provider ID,
lifecycle, selector identity, version representation, and advertised provider
capabilities agree; malformed successes are never cached.

Treat subjects, schema names, definitions, diagnostics, and payloads as
potentially sensitive. Observability should use low-cardinality provider,
operation, outcome, lifecycle, and cache-state fields. Do not allow callers to
construct endpoints from message data. Do not follow registry-supplied URLs or
load `$ref`/imports from the network during decode.
