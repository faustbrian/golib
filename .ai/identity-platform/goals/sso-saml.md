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
- Public contracts: unit ID `contract:unit:sso/saml:v1`; owned operation IDs: `contract:operation:identity.sso.saml-acs:v1`, `contract:operation:identity.sso.saml-idp-init:v1`, `contract:operation:identity.sso.saml-logout-start:v1`, `contract:operation:identity.sso.saml-metadata:v1`, `contract:operation:identity.sso.saml-slo:v1`, `contract:operation:identity.sso.saml-start:v1`
- Requires: `sso`, `primitive/capability-identity-contracts`
- Consumes existing primitives: `http-client`, `secret-envelope`, `capability`, `audit`
- Unlocks after verification: `identity/http`

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
The module MUST implement `sso.ProtocolAdapter` and translate a successful,
fully validated SAML assertion into `sso.ProtocolAssertion`. It MUST NOT apply
SSO routing, JIT, membership, role, directory-sync, token-vault, or session
policy.
After authoritative local-session selection, the public `LocalRevocation`
projection MUST contain the exact local session identifier, authoritative
revocation version, committed state and explicit reconciliation requirement.
Both logout result contracts and every post-selection typed error variant they
can return MUST carry that projection; remote support, provider failure,
timeout or unknown completion cannot erase it or replace an unknown local
outcome with committed success. A pre-selection invalid-request error MUST NOT
carry or fabricate a local session identifier or revocation version.

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
- The binding table is directional and closed: SP-to-IdP AuthnRequest and
  LogoutRequest use HTTP-Redirect; IdP-to-SP login Response, LogoutRequest and
  LogoutResponse use HTTP-POST; and an SP LogoutResponse answering an inbound
  HTTP-POST LogoutRequest returns by HTTP-POST to the verified IdP endpoint.
  Application routes that initiate those messages MUST NOT be described or
  advertised as SAML bindings.
- SP metadata MUST advertise the exact SP-initiated ACS as default and the
  separate IdP-initiated ACS only when enabled. Response Destination and
  SubjectConfirmation Recipient MUST match the selected configured URL
  byte-for-byte; only SP-initiated responses require the outstanding request ID
  in `InResponseTo`. RelayState is optional wherever SAML permits it. When
  present its wire encoding MUST be at most 80 bytes and MUST contain only an
  opaque handle to bounded internal transaction state, never raw application
  state. On an SP-initiated flow it MUST use the complete `tx.capability.*`
  issue/validate/reserve/apply/finalize/recover protocol. IdP-initiated login
  MUST NOT require RelayState or a pre-auth transaction and MUST treat any
  received RelayState as untrusted for authority.
  The IdP-initiated URL MUST be composed only from the validated external-base
  origin plus `/saml2/{canonical_provider_id}/idp-init`; metadata Location,
  Response Destination and Recipient MUST equal it byte-for-byte, and both the
  Response and SubjectConfirmation `InResponseTo` attributes MUST be absent.
- Apple and SAML cross-site HTTP-POST callbacks MUST use the separate
  `__Secure-identity_frontchannel` Secure/HttpOnly/SameSite=None, exact-path,
  five-minute, one-use correlation cookie. Normal session and flow cookies
  remain SameSite=Lax and MUST NOT be reused or weakened for this exception.
- Signed AuthnRequests MUST use the closed reference algorithm and explicit key
  rotation. The public constant `SignatureAlgorithmRSAPSSSHA256` MUST equal
  exactly `http://www.w3.org/2007/05/xmldsig-more#sha256-rsa-MGF1`, and
  MUST implement the fixed-parameter URI selected by RFC 6931 §2.3.10 rather
  than the parameterized `rsa-pss` URI from §2.3.9: SHA-256 message digest,
  MGF1 with SHA-256, 32-byte salt and trailer field `0xBC`. The
  `saml.redirect_signature_algorithm` MUST select only that constant in this
  profile. Every outbound HTTP-Redirect AuthnRequest and LogoutRequest MUST use
  that configured constant and sign the exact
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
- Logout message handling MUST be discriminated before subject/session
  validation. Only a signed LogoutRequest may supply NameID and optional
  SessionIndex as logout targets. A LogoutResponse MUST correlate through its
  signed `InResponseTo` to the reserved outstanding request and recover the
  provider, NameID, SessionIndex and local session exclusively from stored
  request context; response-supplied subject/session fields MUST NOT select
  authority.
- Authorized SAML logout MUST complete local session revocation independently
  of remote support or outcome. Every `identity.sso.saml-logout-start` and
  `identity.sso.saml-slo` success, local-only, provider-error, timeout,
  unavailable, or unknown-reconciliation result after authoritative session
  selection MUST expose an unconditional
  typed `LocalRevocation` projection containing the local session identifier,
  revocation version, committed state, and reconciliation requirement. A
  remote error or unknown outcome MUST NOT erase, defer, or ambiguously encode
  the authoritative local revocation; if the local revocation itself is
  unknown, no result may claim it committed and reconciliation is mandatory.
  Malformed or unsupported input rejected before authoritative session
  selection MUST return the distinct invalid-request error without a
  `LocalRevocation` projection and MUST NOT invent a session or version.
- Single Logout MUST implement SAML Core 2.0 §3.7 and SAML Profiles 2.0 §4.4
  under the exact directional binding table; unlisted SLO profiles and bindings
  are unsupported.
- Implementation and conformance tests MUST trace every selected behavior to
  the exact closed Core, Bindings, Profiles, Metadata, XML Signature and
  Security clause lists in `PROTOCOL_BASELINES.md`. The requirement-to-test
  matrix MUST name the individual clause locator for each test; a source-level
  or whole-section-family citation is insufficient. Unlisted clauses MUST NOT
  silently select another binding, profile, algorithm, or trust decision.
- Response validation MUST cover XML signature location/reference, issuer,
  audience, destination, recipient, subject confirmation, InResponseTo,
  NotBefore/NotOnOrAfter, session index and assertion/response replay.
- The Response ID and every consumed Assertion ID MUST be reserved atomically
  as one replay set before authority issuance. If any ID exists, none are newly
  reserved; an unknown commit outcome remains reserved until authoritative
  reconciliation.
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

This unit owns applicability for `ref.frontchannel_post.cookie`,
`ref.saml.relay_state`, `ref.saml.replay_set`,
`ref.struct:ref.frontchannel_post_cookie`, and
`ref.struct:ref.saml.replay_set`; workers MUST consume those exact policies and
MUST NOT invent adapter-local cookie, RelayState, or replay-set semantics.

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
