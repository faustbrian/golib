# Goal Harden: Strict JWT Validation

## Mission

Audit JWT validation as a hostile-input and remote-key security boundary.
Passing happy-path tokens is not evidence of safety.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Build an algorithm/key/claim/error matrix from current RFCs and implementation.
- Run official and adversarial JOSE vectors covering truncation, duplicate
  members, invalid UTF-8, huge numbers, malformed base64url, key confusion,
  `kid` collisions, critical headers, clock boundaries, and nested payloads.
- Fuzz every parser and remote response boundary with strict byte, depth,
  header, redirect, decompression, and time limits.
- Inject DNS, TLS, redirect, timeout, cancellation, partial-body, oversized
  body, cache poisoning, key rotation, stale key, and outage failures.
- Prove synchronized refresh prevents a thundering herd inside one process and
  fleet jitter avoids synchronized provider load across Kubernetes replicas.
- Race validation, refresh, cancellation, and close; audit timer, response-body,
  goroutine, key-set, and caller-buffer ownership.
- Differentially test accepted and rejected tokens against selected maintained
  libraries without relaxing stricter explicit policy.
- Benchmark local and cached-remote validation, rotation misses, hostile input,
  memory retention, and contention.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests, with no unresolved security,
interoperability, race, fuzz, or resource-lifecycle finding.
