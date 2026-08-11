# Goal: pkg/sso/saml

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `sso/saml`
- Canonical module: `pkg/sso/saml`
- Canonical goal after scaffolding: `pkg/sso/saml/.ai/GOAL.md`
- Requires: `sso`
- Consumes existing primitives: `http-client`, `secret-envelope`, `capability`, `audit`
- Unlocks after verification: `sso/postgres`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `sso/saml` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/sso/saml` module that owns SAML 2.0 service-provider behavior for enterprise SSO. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns SAML 2.0 service-provider behavior for enterprise SSO. It does not own SSO routing/JIT policy, identity-provider UI, SCIM, and persistence. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define metadata import/export, AuthnRequest, Response, Assertion, bindings, signature policy, encryption policy, NameID, RelayState, replay store, clock policy, and logout support declaration. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST validate XML safely; prevent wrapping and signature substitution; require audience, destination, recipient, InResponseTo, time, issuer, and subject confirmation; bind RelayState; consume assertion IDs once; rotate certificates; test pinned official and independent IdP fixtures; state supported bindings and profiles. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Registration MUST support bounded IdP metadata import and explicit fields,
  SP entity ID/ACS metadata export, certificate rotation and exact IdP/SP
  identity. Metadata URL retrieval MUST satisfy SSRF/TLS/size policy.
- SP-initiated login MUST send AuthnRequest only with HTTP-Redirect and receive
  Response only with HTTP-POST at the exact configured SP ACS. IdP-initiated
  HTTP-POST Response MUST use a distinct configured route and metadata ACS;
  the two routes MUST NOT alias. IdP-initiated login MUST be disabled by default; an enabled
  provider MUST have an explicit unsolicited-response allowlist, current
  organization domain proof, fixed destination, short acceptance window and
  replay policy, and MUST NOT pretend to validate a nonexistent request ID.
- SP metadata MUST advertise the exact SP-initiated ACS as default and the
  separate IdP-initiated ACS only when enabled. Response Destination and
  SubjectConfirmation Recipient MUST match the selected configured URL
  byte-for-byte; only SP-initiated responses require the outstanding request ID
  in `InResponseTo`. RelayState for either route MUST use the complete
  `tx.capability.*` issue/validate/reserve/apply/finalize/recover protocol.
- Signed AuthnRequests MUST be configurable with supported algorithms and key
  rotation. Every outbound HTTP-Redirect AuthnRequest and LogoutRequest MUST
  use the configured `saml.redirect_signature_algorithm`, sign the exact
  `SAMLRequest=value&RelayState=value&SigAlg=value` query sequence when
  RelayState is present or `SAMLRequest=value&SigAlg=value` when absent, and
  append `Signature` afterward. Missing, duplicated, unsupported, or mismatched
  `SigAlg` MUST be rejected. Inbound responses and assertions MUST satisfy an explicit pinned
  signature profile that identifies which object(s) must be signed and rejects
  unsigned content by default. Any compatibility exception MUST be provider-
  specific, must still authenticate the exact consumed assertion, and MUST NOT
  weaken the reference profile's requirement for signed responses/assertions.
- LogoutRequest initiation uses HTTP-Redirect; inbound LogoutRequest and
  LogoutResponse use the one HTTP-POST SP SLO route. A signed LogoutResponse to
  an inbound request uses HTTP-POST to the verified IdP response endpoint, and
  SP metadata MUST NOT advertise an unregistered Redirect SLO endpoint.
- Response validation MUST cover XML signature location/reference, issuer,
  audience, destination, recipient, subject confirmation, InResponseTo,
  NotBefore/NotOnOrAfter, session index and assertion/response replay.
- XML processing MUST disable DTDs, external entities, XInclude and network/
  filesystem resolution; reject duplicate IDs and ambiguous references; bind
  signature verification to the exact assertion subsequently consumed; and
  perform schema/profile validation without reserializing or reparsing trusted
  subtrees. Wrapping, namespace confusion, transform abuse and multiple-
  assertion selection MUST have deterministic hostile fixtures.
- Algorithm allowlists MUST reject SHA-1 and other weak profiles by default;
  XML depth, nodes, attributes, text, decoded size, signatures, assertions and
  certificates MUST be bounded before expensive work.
- Attribute/NameID mapping MUST identify the stable subject and distinguish
  verified from asserted email/roles. Unknown attributes MUST NOT create
  privileges.
- Every successful response, including repeat login for an existing NameID,
  MUST return assertion provenance and explicit absent/null/authoritative
  attribute semantics to the SSO repeat-login sync policy; the SAML adapter
  MUST NOT independently retain stale role or membership authority.
- Official SAML fixtures and independent IdP proof MUST include signed request,
  SP initiated, permitted IdP initiated, encrypted assertion if claimed,
  rollover, replay and clock-boundary cases.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.

## Persistence, lifecycle, and compatibility

The core MUST remain adapter-neutral unless this goal is itself an adapter.
State ownership, consistency, retention, deletion, migration, key rotation,
clock skew, concurrent callers, shutdown, and recovery MUST be documented and
tested where applicable. Unsupported protocol or deployment profiles MUST be
stated rather than silently approximated.

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific behavior above, the module's exact coverage and mutation
gates, race/fuzz/interoperability gates that apply, clean-consumer proof,
manifests, public API baseline, security and supply-chain checks, documentation,
changelog, and changed reverse-dependant gates. The final evidence record MUST
name any non-applicable gate with a reviewed reason; absence of infrastructure
or provider access is a blocker, not a pass.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
