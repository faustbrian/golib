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
- Requires: `sso`, `organization`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `http-client`, `capability`, `audit`, `rate-limit`, `telemetry`
- Unlocks after verification: `identity/http`, `identity/reference`

## Start gate and objective

The worker MUST read and satisfy
`.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin until the
coordinator has marked `sso/domain-verification` `in-progress`, recorded this
worker, and verified both prerequisites. Build the concrete DNS TXT and HTTPS
well-known proof engine that produces bounded classified evidence for SSO
orchestration. Only `organization` may move a domain claim to verified state.

## Ownership boundary

This module owns domain canonicalization for proof lookup, cryptographic
challenge generation, DNS/HTTPS proof retrieval, bounded validation, evidence
classification, re-verification and expiry signals. `organization` owns the
claim lifecycle and uniqueness; `sso` owns routing and enforcement. This module
MUST implement the proof-engine collaborator for `identity.sso.domain-challenge`
and `identity.sso.domain-verify`, while `sso` remains their sole callable API,
HTTP and OpenAPI owner. It MUST NOT own organizations, provider registration,
DNS records, certificates, generic web crawling, HTTP handlers or persistence
schemas or publish competing operation definitions.

## Required public contract

The public API MUST define immutable `Profile`, `VerificationRequest`,
`ProofMethod`, `DNSResolver`, bounded HTTPS fetcher, `ObservedDomainEvidence`,
`Verifier`, `Clock` and typed
result/failure contracts. It MUST define supported DNS record name/value and
HTTPS path/media/body formats exactly, along with challenge entropy, expiry,
retry, cache, quorum and clock behavior. Construction MUST reject an empty
method set, unsafe timeout/size limits, insecure HTTPS policy and ambiguous
domain configuration.
`Verifier` MUST expose exactly
`Verify(context.Context, VerificationRequest) (ObservedDomainEvidence, error)`.
`VerificationRequest` MUST bind tenant, organization, canonical domain, claim
ID/version, method, purpose, challenge digest, and expiry.
`ObservedDomainEvidence` is non-authoritative observed evidence only and MUST
bind that request identity, method, checked-at time, evidence expiry, and
stable classification without containing a writable organization proof or
route. Only `organization.DomainEvidenceTransition` may translate it into the
authoritative `organization.DomainProof`.

## Required behavior and security

- A challenge MUST contain at least 32 random bytes, bind tenant,
  organization, canonical registrable domain, claim/version, method, purpose
  and expiry, and expose only the exact proof value needed by the administrator.
  Signing, time checks, rotation and revocation MUST use `capability` rather
  than a module-specific bearer-token format.
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
