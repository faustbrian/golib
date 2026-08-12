# Goal: pkg/sso/domain-verification

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `sso/domain-verification`
- Canonical module: `pkg/sso/domain-verification`
- Canonical goal after scaffolding: `pkg/sso/domain-verification/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:sso/domain-verification:v1`; owned operation IDs: none
- Requires: `sso`, `organization`
- Consumes existing primitives: `http-client`, `audit`, `rate-limit`, `telemetry`
- Unlocks after verification: `identity/http`, `identity/reference`

## Start gate and objective

The worker MUST read and satisfy
`.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin until the
coordinator has marked `sso/domain-verification` `in-progress`, recorded this
worker, and verified both prerequisites. Build the concrete DNS TXT and HTTPS
well-known implementation of the consumer-owned `sso.DomainProofEngine`
interface. It produces bounded classified observations for SSO orchestration;
only `organization` may move a domain claim to verified state.

## Ownership boundary

This module owns DNS/HTTPS proof retrieval, network-safe canonicalization,
bounded validation, evidence classification, and observation-expiry signals.
`organization` owns the claim lifecycle and uniqueness. `sso` owns the
challenge, capability binding, consumer request/result types, orchestration,
routing, enforcement, and the callable `identity.sso.domain-challenge` and
`identity.sso.domain-verify` HTTP/OpenAPI operations. This module imports `sso`
and implements its narrow consumer-owned interface; `sso` MUST NOT import this
implementation module. It MUST NOT own organizations, challenge/bearer formats,
provider registration, DNS records, certificates, generic web crawling, HTTP
handlers, persistence schemas, or competing operation definitions.

## Required public contract

The `sso` package MUST first define immutable `DomainProofRequest`,
`DomainProofObservation`, `DomainProofMethod`, and the consumer-owned interface
`DomainProofEngine` with exactly
`Observe(context.Context, DomainProofRequest) (DomainProofObservation, error)`.
Those values bind tenant, organization, canonical domain, claim ID/version,
method, purpose, challenge digest, expiry, and observation time/classification;
they contain no writable organization proof or routing decision. This module's
public API MUST define only concrete immutable `Profile`, `DNSResolver`, bounded
HTTPS fetcher, `Engine`, `Clock`, construction, and typed implementation
failures. A compile-time assertion MUST prove `*Engine` implements
`sso.DomainProofEngine`; no adapter or callback registration is permitted.

The profile MUST define supported DNS record name/value and
HTTPS path/media/body formats exactly, along with accepted challenge-digest
shape, observation expiry,
retry, cache, quorum and clock behavior. Construction MUST reject an empty
method set, unsafe timeout/size limits, insecure HTTPS policy and ambiguous
domain configuration.
`Engine` MUST expose no additional behavior-bearing verification method. Its
`sso.DomainProofObservation` is non-authoritative observed evidence only. SSO
MUST pass it to `organization.DomainEvidenceTransition`, which alone may
translate it into authoritative `organization.DomainProof`.

## Required behavior and security

- The engine MUST accept only an `sso.DomainProofRequest` whose challenge has
  already been issued and bound by SSO. It MUST compare the proof value but MUST
  NOT generate, sign, rotate, revoke, persist, or expose challenge authority.
- Domain input MUST be length-bounded, IDNA-canonicalized and rejected for IP
  literals, public suffixes, invalid labels, wildcard ambiguity, trailing-dot
  confusion or a parent/child scope the selected policy does not allow.
- DNS verification MUST query the exact configured TXT owner, bound response
  records/bytes/CNAME depth and distinguish NXDOMAIN, NODATA, SERVFAIL,
  timeout, truncation, malformed data and challenge mismatch. Resolver cache
  data MUST retain TTL and source classification and MUST NOT fabricate DNSSEC
  validation.
- HTTPS verification MUST use the exact canonical host and well-known path,
  require verified TLS, reject redirects by default, bound addresses,
  connections, headers and body before allocation, and resist proxy, DNS
  rebinding, private/link-local target and alternate-port SSRF.
- Proof success MUST compare the full tenant/organization/domain/claim/version
  binding in constant work where secret comparison applies. A stale,
  superseded or revoked challenge MUST never verify a newer claim.
- Verification MUST return attributable observed evidence and checked time to
  the organization transition without retaining raw DNS/HTTP bodies or
  sensitive network diagnostics. Only the organization repository may commit
  verified, expired, revoked or conflict state.
- Re-verification policy MUST define evidence lifetime, DNS/HTTPS disappearance,
  ownership transfer, parent/child conflicts and the point at which SSO routing
  stops. Cached prior success MUST NOT outlive the committed claim policy.
- Attempts MUST apply per-tenant, organization, domain and network rate limits
  with bounded concurrency. Cancellation, throttling and ambiguous network
  outcomes MUST remain unavailable evidence and MUST NOT be converted to
  success or automatic ownership loss.
- Audit/telemetry MUST use bounded safe dimensions and record method and result
  class without challenge values, complete domains when policy treats them as
  sensitive, DNS answers, HTTP bodies or infrastructure addresses.
- Concurrent claims for the same domain MUST be resolved by the organization
  repository's uniqueness/version contract. This verifier MUST NOT choose a
  winner based on request order or network timing.

## Required evidence

Tests MUST cover IDNA and public-suffix boundaries, parent/child conflicts,
DNS TXT/CNAME/TTL/error cases, HTTPS TLS/redirect/rebinding/private-address
attacks, stale and superseded challenges, replay, rate limits, cancellation and
re-verification expiry. Fuzzing is REQUIRED for domain, DNS record and HTTPS
body parsing. Interoperability MUST use independent task-owned authoritative
DNS and HTTPS implementations; documented live proof is REQUIRED only for a
deployment profile that claims live external verification.

Exact coverage and mutation, race/stress/leak, resolver/fetch resource
benchmarks, clean-consumer, API/docs/examples/changelog, security and
supply-chain gates are REQUIRED. The unit MUST remain unverified if an
unverified or stale domain can route SSO, network ambiguity becomes success,
SSRF remains possible, another organization's challenge can verify, or domain
ownership is duplicated outside `organization`.
