# Identity Platform Program

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Deliver a storage-neutral, independently releasable Go identity platform whose
composed backend capability is competitive with the pinned Better Auth
baseline in `BETTER_AUTH_PARITY.md`. `pkg/authentication` remains limited to
credential-to-principal validation. This program owns product workflows,
issuance, sessions, federation, provisioning, administration, HTTP composition,
and their adapters through explicit packages.

The goals remain in this planning tree until modules exist. On integration,
the coordinator MUST move each goal byte-for-byte unchanged to
the exact canonical goal path declared in that goal, update the inventory, and
register the module. `GOAL_MANIFEST.json` pins the exact semantic bytes,
planning location, and canonical destination for every unit. Every later
validator consumer MUST resolve the body through the current inventory link;
missing, rewritten, duplicated, mismatched, or orphan goal paths are invalid.
The move changes location, not ownership: the coordinator retains exclusive
write custody of the moved goal even when its canonical path is beneath a
worker-owned module root. The moved goal path is excluded from worker write
scope and included in returned-diff custody validation.

The canonical contract read order is `END_STATE.md`,
`END_STATE_ACCEPTANCE.json`, `ACCEPTANCE_ARTIFACTS.json`,
`REFERENCE_PROFILE.md`, `API_OPERATIONS.md`, `OPERATION_SEMANTICS.json`,
`PUBLIC_CONTRACTS.json`, `public_contracts.rb`, then the upstream disposition
and source documents. Public contracts precede implementation and package
goals. A worker and the coordinator MUST NOT infer, add, broaden, or substitute
a public API beyond the exact unit and operation contract IDs assigned to its
goal.

## Product-authority boundary

The coordinator is an executor and custodian, not the product authority. The
following are semantic changes: adding, removing, reclassifying, weakening, or
strengthening product scope; package ownership; a dependency for behavioral
rather than already-authorized mechanical reasons; a public unit or operation
contract; operation semantics; a parity disposition; a protocol revision or
profile; an end-state journey, claim, or artifact contract; a reference
configuration behavior; a security, transaction, or lifecycle requirement; or
any goal-body byte.

Before the first semantic byte changes, the user MUST explicitly authorize the
exact proposed old and new semantic digests by returning the canonical
standalone authorization statement defined in `PREFLIGHT_EVIDENCE.md`; the
coordinator then records its durable `user:<safe-approval-id>` row. The coordinator MUST
NOT author, approve, infer, or self-authorize that row. Chat reasoning by the
coordinator, a worker recommendation, validator failure, upstream drift, or a
desire to make implementation easier is not user authorization. The
coordinator MAY make byte-preserving moves and mechanically regenerate derived
artifacts from already-authorized unchanged sources; regeneration that changes
an independently authoritative semantic source is forbidden.

User authority is recorded only by the committed exact-byte user-message capture
event defined in `PREFLIGHT_EVIDENCE.md`, verified under the trust document
already pinned on the recorded base. A repository row, a reproducible message
digest, coordinator text, or an unsigned export is self-certification and MUST
NOT authorize a change. When verifiable platform evidence is unavailable, the
affected request remains `awaiting-user` and blocks only its derived closure
until all independent work is exhausted.

The authoritative semantic-source catalog is revision-aware. It includes every
root identity-platform semantic source plus exactly one goal path per
`GOAL_MANIFEST.json` unit: the planning path before its byte-preserving move or
the canonical `pkg/.../.ai/GOAL*.md` path after it. Each commit-edge audit uses
the union of parent/current resolved paths, rejects duplicate planning and
canonical copies, and treats a location change as a valid move only when bytes
and digest are identical. Canonical moved goals therefore remain eligible for
user-authorized semantic repair and remain coordinator custody.

When implementation exposes a missing or contradictory semantic decision, the
coordinator MUST preserve the affected lane, record the exact bounded decision
and impact, continue independent lanes, and request user authority only when
the decision gates progress. An unauthorized semantic change blocks assignment,
integration, verification, and final acceptance for its affected closure.

## Product boundary decisions

- There is no `identityadmin` or `identity/management` module and no admin UI.
  User status belongs to `identity`, session control to `identity/session`,
  organization administration to `organization`, authorization decisions to
  `authorization`, and privileged impersonation to `identity/impersonation`.
  `identity/http` exposes these operations as a coherent backend API.
- `sso` is the enterprise-federation boundary. `sso/oidc`, `sso/oauth2`, and
  `sso/saml` isolate protocol behavior without introducing a vague federation
  facade.
- `webauthn` owns protocol ceremonies and cryptographic verification;
  `passkey` owns identity-facing discoverable-credential lifecycle. WebAuthn
  also supports non-passkey security-key use, so these remain separate.
- There is no generic one-time-token package. `capability` supplies signed
  payloads, issuer/audience/resource/action binding, time checks, rotation,
  revocation, `MaxUses=1`, atomic consumption, replay storage, and stable
  failures. Each workflow owns lookup, delivery, callback, transaction, and
  state-transition semantics.
- CAPTCHA has a provider-neutral contract and adapters for Google reCAPTCHA,
  Cloudflare Turnstile, hCaptcha, and CaptchaFox.
- HIBP Pwned Passwords is in scope through its k-anonymity range protocol. It
  produces risk evidence and never owns password policy or sends complete
  passwords or hashes.
- Social OAuth has one built-in provider-profile catalog. Provider-specific
  SDK wrappers remain out of scope.
- The public transport is standard-library `net/http`; packages below it
  remain transport-neutral. `identity/http` depends on feature/protocol
  contracts, while `identity/reference` is the only mandatory-all-adapters
  composition. Concrete PostgreSQL, Valkey and provider adapters MUST NOT
  become mandatory dependencies of the reusable HTTP module.

## Exact out-of-scope and divergence categories

Billing and payment plugins, SIWE, MCP authentication, agent authentication,
JavaScript framework clients, and database engines beyond the selected
PostgreSQL and Valkey profiles are product exclusions. Lead tracking, CLI
scaffolding, and community catalogs are non-capabilities; personal SCIM is an
unselected deployment profile; database-less OAuth state/provider-token
cookies are a security divergence. None authorizes adding those integrations.
Product exclusion is not permission to leave an in-scope capability partial. Upstream changes after
the pinned revision become a separately approved parity audit.

## Existing primitives

`authentication`, `authentication/jwt`, `authentication/oidc`,
`authorization`, `password`, `capability`, `tenancy`, `audit`, `rate-limit`,
`secret-envelope`, `identifier`, `postgres`, `migrations`, `outbox`,
`workflow`, `webhook`, `http-client`, `openapi`, and `telemetry` remain
lower-level primitives. JWT/JWK and OIDC remain validation-only. Authorization
server issuance, grants, discovery, consent, and JWKS publication belong to
`oauth-server` and `oauth-server/oidc`.

Five dependency-free schedulable prerequisite units extend the exact pinned
public contracts needed by the identity platform: authentication,
authorization, capability (including its PostgreSQL adapter contract),
identifier, and password. They are not additional identity-platform product
units: the product scope remains 61 units while the complete execution DAG has
67 schedulable units.

## Program completion contract

Completion requires all 61 identity-platform inventory units and all six
primitive-extension prerequisites to be `verified` (67 schedulable units
total), every in-scope
row in `BETTER_AUTH_PARITY.md` to have executable proof, and every composed
journey and cross-cutting property in `END_STATE.md` to pass against final
inputs. No row may remain partial, depend on undocumented application glue, or
be represented only by a primitive that lacks the required workflow.

The coordinator artifacts close the implementation choices that package goals
consume. `END_STATE_ACCEPTANCE.json` closes all 19 journeys, cross-cutting claims and acceptance-artifact producers; `API_OPERATIONS.md` owns the complete transport operation catalog and `OPERATION_SEMANTICS.json` pins every operation semantic field;
`UPSTREAM_DISPOSITIONS.md` owns the disposition of every pinned upstream
surface; `UPSTREAM_SURFACE.json` pins the machine-verifiable source objects and
every exact source-item -> disposition-row -> capability -> operation-ID edge,
with operation owners resolving to registered goals; `UPSTREAM_LEAVES.json`
pins the exact closed leaf inventory consumed by that mapping; independent inventory
digests or owner-has-some-operation checks are insufficient;
`PROTOCOL_BASELINES.md` pins supported protocol revisions and profiles;
`PROTOCOL_CONFORMANCE_MANIFEST.json` binds every selected source and
conformance tool to its immutable revision, retrieved digest, license and
consumers; `SECURITY_EVENTS.md` owns the interoperable audit taxonomy;
`TRANSACTION_CONTRACT.md` owns cross-module atomicity, idempotency,
compensation, and ambiguous-outcome rules; `LIFECYCLE_CASCADES.md` owns
destructive and privilege-changing cascades; `LIFECYCLE_CONSUMERS.md` owns the
versioned exact consumer set for every cascade; `REFERENCE_CONFIGURATION.md`
owns exact deployable defaults; `CONFIGURATION_CATALOGS.json` owns the
versioned provider/CAPTCHA instance IDs and checksums used for template
expansion; `PARITY_DISPOSITIONS.json` pins exclusions and ownership reclassifications; `VERIFICATION_APPLICABILITY.json` closes every unit's seven verification selectors; and `PREFLIGHT_EVIDENCE.md` records the
coordinator's versioned, attributable preflight result. Every artifact MUST be
complete and structurally valid before the first worker assignment. Package
goals MUST consume these decisions and MUST NOT reopen them independently.

`validate.rb` proves only the current files' structural invariants. A static
file validator cannot prove Git ancestry, commit reachability, prior ledger
versions, exact generation increments, or whether a same-status row update
changed only permitted fields. Before every coordinator state commit, the
transition-check procedure in `ORCHESTRATOR_GOAL.md` MUST compare the proposed
inventory and ledger to their exact Git parent and prove those historical
properties. Passing `validate.rb` MUST NOT be reported as ancestry or
transition-history proof.

The configurable package contracts MUST compose under the topology in
`REFERENCE_PROFILE.md` and the exact defaults and bounds in
`REFERENCE_CONFIGURATION.md`. A package worker MUST NOT choose an incompatible
local default merely because its isolated tests pass.

The complete reference server MUST use standard `net/http`, PostgreSQL, and
Valkey and MUST exercise provider integrations where the goals require them.
Unavailable provider or infrastructure evidence is an explicit blocker for
the affected claim, not a pass.

Executable evidence has two distinct owners. The implementation or package
worker MAY produce raw observations, but only the coordinator-owned runner MAY
execute an authoritative gate, capture its process boundary, and close an
acceptance record. For every acceptance artifact, a separate artifact-specific
verifier that is not authored or executed by the producing package worker MUST
recompute the artifact contract from the runner's immutable capture. The
producer's exit zero, self-report, transcript, digest, or package-local test is
never independent verification of its own claim.

Protocol and security claims require evidence independent of the
implementation under test. Where `PROTOCOL_CONFORMANCE_MANIFEST.json` selects
an external suite or independent implementation, that selected verifier is
REQUIRED. Otherwise the coordinator MUST run a separately owned
artifact-specific verifier against raw wire, state-transition, or security-
event observations. Mocks, producer-authored expected values, and a second
invocation of the same implementation are not independent evidence.

### Exhaustive success predicate

There is exactly one successful completion predicate. `PROGRAM-COMPLETE` is
true if and only if all of the following are true at one exact clean committed
integration revision:

1. all 67 inventory units occur exactly once and have status `verified`;
2. every verified row's current complete-input manifest and root reproduce at
   that revision, with attributable unit-gate and external-evidence bindings;
3. every in-scope Better Auth parity row has its exact verified owner and
   executable passing proof, and every exclusion or divergence still matches
   its user-authorized pinned disposition;
4. every `END_STATE_ACCEPTANCE.json` journey and cross-cutting claim passes and
   every `ACCEPTANCE_ARTIFACTS.json` artifact has one current attributable
   binding against the same final-input revision;
5. every public contract, operation semantic, protocol-conformance,
   configuration, security-event, transaction, lifecycle, lifecycle-consumer,
   migration, interoperability, and clean-consumer obligation selected for the
   program is proven without undocumented application glue;
6. every repository-required affected release gate and final complete-diff
   review passes with no unresolved finding, stale result, warning
   substitution, or unavailable required result;
7. the final integration tree contains no unauthorized semantic change, every
   authorization and transition history validates, and every committed result
   descends through the required assignment and integration topology;
8. all task-owned disposable resources are reconciled and removed under the
   integration-worktree handoff rule; the coordinator separately records its
   assertion that it invoked no push, the bounded local Git state it observed,
   and the explicit limitation that available tooling cannot independently
   prove complete command history, remote non-delivery, or actions by other
   actors; that assertion is not evidence for this or any stronger verified
   completion claim; and
9. the durable final state can render an exact one-row status disposition for
   each of the 67 units and every parity, journey, gate, provider, deployment,
   cleanup, and authorization boundary required by the final-report schema.

The successful final report is a rendering of this predicate, not another
predicate. Every reported `PROGRAM-COMPLETE` clause MUST bind the exact current
evidence record, receipt, commit, and digest that proves it; any absent,
duplicate, stale, unavailable, non-passing, or non-reproducible binding makes
that clause false and therefore makes `PROGRAM-COMPLETE` false.

No subset, percentage, wave completion, package-local pass, structural
validation pass, or absence of a known finding makes `PROGRAM-COMPLETE` true.
The coordinator MUST repeat scheduling, repair, invalidation, final-input
verification, and review until this predicate is true or the sole blocked-stop
predicate in `ORCHESTRATOR_GOAL.md` is satisfied.

## Delivery policy

`ORCHESTRATOR_GOAL.md` is the sole program-level execution goal. The
coordinator assigns exactly one inventory unit per worker using
`WORKER_PROMPT.md`. A worker MUST NOT start before every `Requires` unit is
integrated and verified. The coordinator alone owns shared state, manifests,
integration, reverse-dependant verification, parity closure, and end-state
proof.
