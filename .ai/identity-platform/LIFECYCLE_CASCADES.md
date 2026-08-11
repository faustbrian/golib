# Identity Platform Lifecycle Cascades

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

Every declarative table row is a normative **MUST** requirement unless its row
explicitly says otherwise.
`lifecycle.foundation` is the stable requirement ID for the envelope, privacy,
schema evolution, version monotonicity, scope selectors, cascade generation,
acknowledgements, holds/waivers, and consumer obligations. Selecting any
`lifecycle.*` row or lifecycle event MUST import and enumerate this ID.

## Envelope, privacy, and schema evolution

`identity` owns the lifecycle envelope and catalog. Domain modules own their
named events. Durable adapters MUST publish through `TRANSACTION_CONTRACT.md`.
The envelope MUST contain event ID, schema name and major/minor version,
aggregate kind and opaque ID/version, tenant opaque ID, command ID, PostgreSQL
UTC occurrence time, policy version, and a bounded redacted payload. Actor,
effective-subject, causation, correlation, and cascade ID/generation fields are
present exactly when their typed applicability predicates below hold.

The public envelope MUST be one closed immutable `LifecycleEnvelopeV1` value
with these exact field types: IDs are 16 random bytes encoded as 22-character
unpadded base64url; schema and aggregate kind are exact registered ASCII enums;
major/minor versions are unsigned 16-bit integers; aggregate, policy, and
cascade generations are unsigned 64-bit integers; occurrence time is a signed
64-bit Unix-microsecond PostgreSQL UTC value; absent optional actor, effective
subject, causation, correlation, and cascade references are omitted, never
empty. The payload is a catalog-selected typed union, not a free-form map. It
may contain only opaque IDs, registered enums, Booleans, bounded counts, and
versions; it is limited to 16 fields, 256 UTF-8 bytes per encoded field, and
4 KiB total. RFC 8949 deterministic CBOR with shortest integers, definite
lengths, sorted encoded keys, valid UTF-8, and no floats is the sole canonical
codec. Unknown union variants, duplicate fields, non-canonical encodings, and
unknown major versions enter reconciliation before any consumer mutation.

`LifecycleAcknowledgementV1` is likewise closed: cascade ID/generation,
consumer ID/contract version, event ID/schema major/minor, checkpoint and
mutation versions, status, limitation code, PostgreSQL UTC occurrence time,
and evidence key version/digest. Status is exactly `applied`,
`not-applicable`, `pending`, `limited`, or `outcome-unknown`, matching
`LIFECYCLE_CONSUMERS.md`. A rejected event, version gap, or unknown schema is
reported as `pending` or `outcome-unknown` with a registered redacted limitation
code and enters reconciliation. Only `applied` and a policy-permitted
`not-applicable` close a required local consumer; `limited` closes only an
eligible external consumer through the audited waiver contract. The evidence
digest is HMAC-SHA-256 over the ASCII domain `identity-lifecycle-ack-v1`, a zero
byte, and the canonical CBOR acknowledgement with the digest fields omitted.
Key removal is forbidden
until `lifecycle.ack_retention` expires.

Opaque IDs are pseudonymous personal data. Direct identifiers, credentials,
tokens, provider payloads, and free-form reasons MUST NOT appear. Envelope and
checkpoint retention MUST follow the audit/lifecycle manifest. Erasure MUST
crypto-shred or replace linkable IDs with non-reversible scoped tombstones where
law permits; an audit-retained pseudonym MUST NOT be presented as anonymous.

Major versions change meaning or required fields and unknown majors MUST be
rejected into reconciliation. Minor versions are additive only: fields MAY be
added as optional with safe defaults, existing meaning/type MUST NOT change,
and consumers MUST ignore unknown minor fields. Consumers MUST deduplicate event
IDs, checkpoint aggregate versions monotonically, and stop destructive work on
a gap, regression, or conflicting duplicate.

## Exact event catalog

| Row ID | Owner | Required event names |
| --- | --- | --- |
| `lifecycle.events.identity` | `identity` | `identity.created.v1`, `identity.updated.v1`, `identity.suspended.v1`, `identity.restored.v1`, `identity.anonymization_requested.v1`, `identity.anonymized.v1`, `identity.deletion_requested.v1`, `identity.deleted.v1`, `authority.global_compromised.v1` |
| `lifecycle.events.identifier` | `identity` identifier owner | `identifier.added.v1`, `identifier.verified.v1`, `identifier.changed.v1`, `identifier.removed.v1` |
| `lifecycle.events.anonymous` | `identity/anonymous` | `anonymous.created.v1`, `anonymous.upgraded.v1`, `anonymous.expired.v1`, `anonymous.deleted.v1` |
| `lifecycle.events.password` | `identity/password` | `password.created.v1`, `password.changed.v1`, `password.reset.v1`, `password.compromised.v1`, `password.removed.v1` |
| `lifecycle.events.session` | `identity/session` | `session.created.v1`, `session.rotated.v1`, `session.revoked.v1`, `session_family.revoked.v1`, `session.active_account_changed.v1` |
| `lifecycle.events.factor` | `identity/mfa` | `factor.enrolled.v1`, `factor.changed.v1`, `factor.removed.v1`, `factor.reset.v1`, `trusted_device.created.v1`, `trusted_device.revoked.v1` |
| `lifecycle.events.passkey` | `passkey` | `passkey.registered.v1`, `passkey.counter_advanced.v1`, `passkey.backup_state_changed.v1`, `passkey.revoked.v1`, `passkey.compromised.v1` |
| `lifecycle.events.social_provider` | `identity/oauth` | `social_provider.linked.v1`, `social_provider.unlinked.v1`, `social_provider.configuration_changed.v1`, `social_provider.disabled.v1`, `social_provider.token_revoked.v1` |
| `lifecycle.events.api_key` | `identity/apikey` | `api_key.created.v1`, `api_key.rotated.v1`, `api_key.permissions_changed.v1`, `api_key.revoked.v1` |
| `lifecycle.events.organization` | `organization` | `organization.created.v1`, `organization.updated.v1`, `organization.archived.v1`, `organization.restored.v1`, `organization.deletion_requested.v1`, `organization.deleted.v1`, `membership.created.v1`, `membership.role_changed.v1`, `membership.removed.v1`, `team.created.v1`, `team.membership_changed.v1`, `team.deleted.v1`, `domain_claim.created.v1`, `domain_claim.verified.v1`, `domain_claim.revoked.v1` |
| `lifecycle.events.enterprise_provider` | `sso` | `enterprise_provider.created.v1`, `enterprise_provider.configuration_changed.v1`, `enterprise_provider.mapping_changed.v1`, `enterprise_provider.enabled.v1`, `enterprise_provider.disabled.v1`, `enterprise_provider.deleted.v1` |
| `lifecycle.events.oauth_server` | `oauth-server` | `oauth_client.created.v1`, `oauth_client.configuration_changed.v1`, `oauth_client.secret_rotated.v1`, `oauth_client.revoked.v1`, `grant.created.v1`, `grant.scope_changed.v1`, `grant.revoked.v1`, `consent.created.v1`, `consent.revoked.v1`, `signing_key.added.v1`, `signing_key.retired.v1`, `signing_key.compromised.v1` |
| `lifecycle.events.risk` | `identity/risk` | `risk.policy_changed.v1`, `risk.evidence_expired.v1`, `risk.subject_anonymized.v1`, `risk.lockout_started.v1`, `risk.lockout_ended.v1` |
| `lifecycle.events.captcha` | `identity/risk/captcha` | `captcha.site_disabled.v1`, `captcha.configuration_changed.v1`, `captcha.secret_rotated.v1` |
| `lifecycle.events.hibp` | `identity/risk/hibp` | `hibp.configuration_changed.v1`, `hibp.cache_reset.v1` |
| `lifecycle.events.delivery` | `identity/delivery` | `delivery.template_changed.v1`, `delivery.sender_changed.v1`, `delivery.payload_erased.v1`, `delivery.effect_terminal.v1` |
| `lifecycle.events.risk_valkey` | `identity/risk/valkey` | `risk_valkey.epoch_unavailable.v1`, `risk_valkey.replay_completed.v1`, `risk_valkey.epoch_published.v1` |

An owner MUST NOT emit a generic `*.changed` event outside these exact names.
New transitions require an explicit catalog row and consumer-impact review.

## Authority-version dimensions and owners

All dimensions are monotonically increasing unsigned 64-bit values. Overflow is
terminal and MUST fail closed; restore, import, and cache rebuild MUST NOT reset
or decrement them.

| Row ID | Dimension | Sole durable owner | Scope key | Bumped by |
| --- | --- | --- | --- | --- |
| `lifecycle.dimension.global` | `global` | vocabulary/interface in `identity`; durable implementation in `identity/postgres`; `identity/reference` invokes it | deployment authority ID | global logout or platform compromise |
| `lifecycle.dimension.tenant` | `tenant` | `identity/postgres` | tenant ID | tenant disable/restore or isolation-policy change |
| `lifecycle.dimension.user` | `user` | `identity/postgres` | tenant + user ID | suspend, restore, anonymize, delete, user compromise |
| `lifecycle.dimension.privacy` | `privacy` | `identity/postgres` | tenant + user ID | privacy-export generation/cancellation, anonymization, or deletion |
| `lifecycle.dimension.identifier` | `identifier` | `identity/postgres` | tenant + user ID | identifier add/verify/change/remove or reuse-policy transition |
| `lifecycle.dimension.credential` | `credential` | `identity/postgres` | tenant + user ID | password or primary credential create/change/reset/remove/compromise |
| `lifecycle.dimension.session_family` | `session_family` | `identity/session/postgres` | tenant + session-family ID | family rotation, revoke-all, fixation, compromise |
| `lifecycle.dimension.session` | `session` | `identity/session/postgres` | tenant + session ID | rotation, explicit revocation, active-account change; natural expiry checks `expires_at` and MUST NOT bump this version |
| `lifecycle.dimension.authorization` | `authorization` | existing `authorization/postgres` | tenant + policy namespace + subject ID | role, statement, policy, or entitlement change |
| `lifecycle.dimension.organization` | `organization` | `organization/postgres` | tenant + organization ID | archive/restore/delete, membership/role/team/domain-policy change |
| `lifecycle.dimension.factor` | `factor` | `identity/mfa/postgres` | tenant + user ID | factor enroll/change/remove/reset or trusted-device change |
| `lifecycle.dimension.passkey` | `passkey` | `passkey/postgres` | tenant + user ID | passkey register/counter/backup-state/revoke/compromise change |
| `lifecycle.dimension.social_connection` | `social_connection` | `identity/oauth/postgres` | tenant + provider connection ID | connection configuration/key/disable change |
| `lifecycle.dimension.social_link` | `social_link` | `identity/postgres` | tenant + user ID + provider connection ID | account link/token/unlink change; `identity/oauth/postgres` enlists this owner for token-coupled mutations |
| `lifecycle.dimension.enterprise_provider` | `enterprise_provider` | `sso/postgres` | tenant + enterprise connection ID | configuration/mapping/key/domain/enable/disable/delete change |
| `lifecycle.dimension.api_key` | `api_key` | `identity/apikey/postgres` | tenant + API-key configuration ID | permission/quota/configuration/rotate/revoke change |
| `lifecycle.dimension.oauth_client` | `oauth_client` | `oauth-server/postgres` | tenant + client ID | redirect/auth-method/scope/secret/policy/revoke change |
| `lifecycle.dimension.grant` | `grant` | `oauth-server/postgres` | tenant + subject + client + grant-family ID | consent/scope/refresh-family/revocation change |
| `lifecycle.dimension.signing_compromise` | `signing_key_compromise_epoch` | `oauth-server/postgres` | issuer ID | key compromise or forced retirement before all bound tokens expire |
| `lifecycle.dimension.risk_policy` | `risk_policy` | `identity/risk/postgres` | tenant + risk-policy ID | policy, digest-key, lockout, override, or evidence-authority change |
| `lifecycle.dimension.delivery_policy` | `delivery_policy` | `identity/delivery/postgres` | tenant + delivery-profile ID | sender, template, locale, encryption-key, or retention-policy change |

Normal additive signing-key rotation MUST NOT bump
`signing_key_compromise_epoch`. JWTs bind issuer, `kid`, and compromise epoch;
old uncompromised keys remain valid through their declared verification window.

## Artifact-to-version applicability

Version snapshot and mutation MUST share one transaction when stored in the same
authority. “Online” means a primary-authority read or an explicitly listed
positive cache whose maximum staleness is enforced; invalidation messages only
accelerate expiry.

| Row ID | Artifact/decision | Bound dimensions and scope keys | Lookup owner | Acceptance policy |
| --- | --- | --- | --- | --- |
| `lifecycle.artifact.session` | durable browser session | global, tenant, user, identifier, credential, session_family, session, authorization, organization when active, factor when MFA/trust asserted, passkey when a passkey authenticated or stepped up the session | owners above | online at creation, refresh, active-account switch, sensitive operation, and at least every 5 minutes during use; also enforce absolute/idle expiry |
| `lifecycle.artifact.cookie_cache` | cookie session cache | exact durable-session snapshot | `identity/session/valkey` projection | positive cache at most 5 minutes; miss/epoch loss reads all authorities; never negative authorization |
| `lifecycle.artifact.stateless_session` | stateless session compatibility token | global, tenant, user, identifier, credential, session_family, session, authorization, organization/factor claims used, passkey when passkey-authenticated or stepped up | token issuer plus owners above | no online lookup; at most 15 minutes; construction MUST reject bans, impersonation, immediate revoke, active-account switching, or other stronger guarantees |
| `lifecycle.artifact.api_key` | API-key principal | global, tenant, user for user-owned key, authorization, organization when scoped, api_key | `identity/apikey/postgres` plus dimension owners | online on every authentication and quota debit |
| `lifecycle.artifact.api_key_cache` | API-key positive metadata cache | exact authoritative API-key principal snapshot and authority versions | `identity/apikey/valkey` projection | positive cache at most 60 seconds; miss, epoch loss, or invalidation failure reads every authority and never permits fallback authorization |
| `lifecycle.artifact.impersonation` | impersonation session | global, tenant, target user, actor user, authorization for both, organization when scoped, session_family, session | `identity/impersonation/postgres` plus owners | online on every use; no offline/cached acceptance |
| `lifecycle.artifact.access_jwt` | OAuth access JWT | global, tenant, user, authorization, organization/factor claims used, oauth_client, grant, signing_key_compromise_epoch and `kid` | resource-server validator plus owners | signature/expiry every use; dimension snapshot cache at most 5 minutes; profiles requiring immediate revoke MUST select opaque tokens |
| `lifecycle.artifact.session_exchange_jwt` | private authenticated-session exchange JWT | global, tenant, user, authorization, organization/factor claims used, oauth_client, grant, signing_key_compromise_epoch and `kid`, source session_family and session | resource-server validator plus `oauth-server/oidc`, `identity/session`, and owners | signature/expiry every use; never outlives source session; every bound dimension is compared under the access-JWT policy |
| `lifecycle.artifact.opaque_access` | opaque OAuth access token | global, tenant, user, authorization, organization/factor claims used, oauth_client, grant | `oauth-server/postgres` | online introspection every use; positive result cache at most 5 minutes only when immediate revoke is not promised |
| `lifecycle.artifact.refresh` | OAuth refresh token | global, tenant, user, credential, session_family when session-derived, oauth_client, grant | `oauth-server/postgres` plus owners | primary-authority comparison and atomic rotation every use |
| `lifecycle.artifact.authorization_cache` | authorization cache entry | global, tenant, user/service subject, authorization, organization when referenced | `authorization/valkey` positive projection of `authorization/postgres` | at most 5 minutes; protected mutation requires online comparison |
| `lifecycle.artifact.session_enrichment` | session enrichment/active organization | tenant, user, authorization, organization | `identity/session/postgres` plus owners | online on switch and protected organization operation; positive cache at most 5 minutes |
| `lifecycle.artifact.trusted_device` | trusted-device assertion | global, tenant, user, credential, factor, session_family | `identity/mfa/postgres` plus owners | online whenever used to suppress step-up; expiry always checked |
| `lifecycle.artifact.passkey` | WebAuthn/passkey authentication state | global, tenant, user, credential, passkey; factor only when the passkey is enrolled as an MFA factor | `passkey/postgres`; additionally `identity/mfa/postgres` only for MFA enrollment | online counter/status/version comparison before authority issuance |
| `lifecycle.artifact.privacy_export` | privacy-export artifact and download capability | tenant, user, privacy, export ID, contributor contract versions and checkpoint vector | `identity/postgres` plus the exact contributors in `LIFECYCLE_CONSUMERS.md` and `capability/postgres` | fragment reads at the captured version vector; primary-authority identity/privacy/capability comparison on finalization and every download |
| `lifecycle.artifact.risk_velocity` | Valkey velocity/attempt counter | tenant, risk_policy, digest-key version, Valkey boot epoch | `identity/risk/valkey` projection of `identity/risk/postgres` journal | unavailable after restart, flush, marker loss, or epoch mismatch until the single reconciler reaches the PostgreSQL replay watermark and atomically publishes the new healthy epoch |
| `lifecycle.artifact.delivery_effect` | queued encrypted provider effect | tenant, delivery_policy, command ID, effect ID, template and envelope-key versions | `identity/delivery/postgres` | online lease/generation comparison for every attempt; ciphertext is retained only in `planned`, `submitted`, `retry-wait`, or `outcome-unknown` before expiry and erases atomically at `confirmed`, `rejected`, `expired`, `cancelled`, `exhausted`, or `superseded` while keyed replay authority remains |

Passkey revocation MUST bump `passkey` for the one tenant + user and emit
`passkey.revoked.v1`; every session authenticated or stepped up by that passkey
binds the passkey version and is denied on its next required comparison. When
the revoked passkey is stored as a primary authentication credential, the same
transaction MUST also bump `credential`. Passkey compromise MUST bump both
`passkey` and `credential`, emit `passkey.compromised.v1`, deny all prior user
sessions through the credential version, and cascade cleanup to pending
ceremonies, trusted devices, recovery policy, and audit. Neither transition may
claim authenticator-local credential removal.

## Privacy-export snapshot and deletion races

A privacy-export request MUST lock the identity aggregate and, in the same
identity command transaction, reject an identity already pending
anonymization/deletion, allocate an export ID and privacy epoch, record the
exact contributor IDs and contract versions from `LIFECYCLE_CONSUMERS.md`, and
capture one immutable cross-module watermark. The watermark is a version
vector containing the identity aggregate version, lifecycle event sequence,
and each contributor's authoritative checkpoint; a wall-clock timestamp alone
is not a watermark. A restartable worker MUST NOT retain or depend on a
long-lived exported PostgreSQL snapshot. Every contributor MUST instead read
from an append-only/versioned projection capable of reproducing its exact
recorded checkpoint, or atomically stage an immutable bounded fragment during
the request transaction. A contributor that cannot prove the requested cut
MUST leave the export pending or fail it; it MUST NOT mix a current read with
older contributor data or silently omit the contributor.

Every fragment MUST bind export ID, tenant, subject, privacy epoch, contributor
ID/contract version, requested checkpoint, observed checkpoint, schema version,
and a digest of its canonical redacted bytes. The coordinator MUST publish an
artifact only after every required fragment matches the captured watermark and
the final identity lock proves the same active identity version and privacy
epoch. Partial artifacts MUST remain encrypted, non-downloadable, and bounded;
failure or cancellation MUST erase them and revoke any issued download
capability.

An anonymization or deletion request MUST take the same identity lock and
increment the privacy epoch as the bounded atomic denial boundary. It MUST NOT
atomically fan out over every export or capability row. Export cancellation,
capability revocation marking, artifact erasure, and contributor cleanup run as
bounded generation-tagged cascade batches; every affected export and capability
is denied immediately because its captured privacy epoch is older. An export finalizer that loses this race
MUST NOT publish and MUST schedule partial-artifact erasure. If publication won
first, the destructive cascade MUST revoke download authority and erase the
artifact before terminal anonymized/deleted status. A download MUST compare the
current identity state, user and privacy epochs, export state, and capability
reservation on the primary in one transaction; a deletion/anonymization commit
therefore denies every later download even while artifact cleanup remains
pending. Legal hold MAY retain only the declared encrypted artifact/evidence
and MUST NOT preserve download authority.

## Destructive-state and invalidation matrix

An “exact dimensions” cell identifies bounded scope selectors, not an unbounded
row-by-row fan-out. A user trigger bumps the one user-scoped dimension named;
all child artifacts MUST bind it. An organization trigger bumps the one
organization-scoped dimension named; subject authorization counters are bumped
only for the directly changed subject in membership/role operations. A global
trigger bumps `global` once. Per-session-family, per-user-under-organization,
per-client, and per-grant fan-out MUST run as a versioned cascade for cleanup
only and MUST NOT be the mechanism that establishes denial. Each cascade
snapshot MUST record the exact finite scope keys it will reconcile; batches are
bounded by `reconciliation.batch` and cannot claim atomic global fan-out.

The owner transaction commits the authority-denying domain state and version
bump immediately. The cascade itself then has status `pending` until every
required consumer closes; public mutation results MUST distinguish that pending
cleanup from terminal completion. Thus suspension/archive/compromise denial is
effective at the requesting commit even when the aggregate's lifecycle result
is still pending, while restore never reactivates pre-transition artifacts.
Deletion/anonymization keeps its explicitly named pending/held domain state
until closure. A deadline breach leaves the cascade pending and fail-closed; it
does not roll back the authority version or fabricate completion.

| Row ID | Trigger | Exact dimensions bumped atomically | Exact lifecycle event | Mandatory consumers/cascade | Terminal state | External limitation behavior |
| --- | --- | --- | --- | --- | --- | --- |
| `lifecycle.cascade.identity_suspend` | identity suspend/ban | user | `identity.suspended.v1` | session, impersonation, API key, OAuth grants, authorization cache, risk, audit | `suspended` after local acknowledgements; new auth denied immediately | external revocations remain named pending/limited effects; local authority stays denied |
| `lifecycle.cascade.identity_restore` | identity restore/unban | user | `identity.restored.v1` | session, impersonation, API key, OAuth grants, authorization cache, audit | `active`; prior authority never reactivates and new authentication is required | no waiver may restore old external tokens |
| `lifecycle.cascade.anonymous_upgrade` | anonymous identity upgrade/link | user for the permanent target; session_family and session for the anonymous session lineage | `anonymous.upgraded.v1` and `session.rotated.v1` | anonymous store, identity, identity/session, session cache, audit | upgraded exactly once; the old anonymous bearer and every sibling in its family are revoked before the permanent session is issued | unsupported application-data merge blocks the upgrade before mutation; post-commit cleanup remains pending without restoring the old bearer |
| `lifecycle.cascade.password_reset` | password reset | credential for the one tenant + user | `password.reset.v1` | all session families as cleanup, reset capabilities, trusted devices, MFA proofs, OAuth session-derived grants, audit | credential active with all prior bound authority denied by credential version | provider revocation limitations are recorded and do not restore local use |
| `lifecycle.cascade.password_compromise` | password compromise | credential for the one tenant + user | `password.compromised.v1` | all session families as cleanup, reset capabilities, trusted devices, MFA proofs, OAuth session-derived grants, audit | credential disabled until reset; all prior bound authority denied by credential version | provider revocation limitations are recorded and do not restore local use |
| `lifecycle.cascade.factor_reset` | self-service or approved administrative factor reset | factor for the one tenant + user | `factor.reset.v1` | trusted devices, MFA/passkey challenges, recovery material, sessions as cleanup, audit; administrative recovery delivery may start only after this generation commits | factor reset with old secrets crypto-erased; authority denied by factor version; no administrator receives replacement factor material | unsupported authenticator-side erasure is recorded as external limitation and cannot authorize recovery |
| `lifecycle.cascade.identifier_change` | identifier change | identifier | `identifier.changed.v1` | recovery, link, invitation, delivery, SSO routing, risk, audit | new identifier state plus old-value reuse tombstone | delivery/provider cleanup limitation cannot permit old identifier authentication |
| `lifecycle.cascade.identifier_remove` | identifier removal | identifier | `identifier.removed.v1` | recovery, link, invitation, delivery, SSO routing, risk, audit | identifier removed plus reuse tombstone | delivery/provider cleanup limitation cannot permit old identifier authentication |
| `lifecycle.cascade.membership_role` | membership role change | organization for the one tenant + organization; authorization for the one changed subject + policy namespace | `membership.role_changed.v1` | session enrichment, authorization cache, API-key organization grants, OAuth grants, active selection, organization-scoped impersonation grants/sessions, audit | new role authoritative | external downstream lag is exposed and local access fails closed |
| `lifecycle.cascade.membership_remove` | membership removal | organization for the one tenant + organization; authorization for the one removed subject + policy namespace | `membership.removed.v1` | session enrichment, authorization cache, API-key organization grants, OAuth grants, active selection, organization-scoped impersonation grants/sessions, audit | membership removed | unsupported external deprovisioning is a named limitation |
| `lifecycle.cascade.team_change` | team membership change | organization for the one tenant + organization; authorization for at most `organization.team_change_batch` directly changed subjects in the command | `team.membership_changed.v1` | authorization cache, session enrichment, SCIM, audit | new team graph authoritative | external directory lag does not preserve local access |
| `lifecycle.cascade.team_delete` | team delete | organization for the one tenant + organization; no per-member authorization fan-out establishes denial | `team.deleted.v1` | authorization cache/session enrichment cleanup, SCIM, audit | team deleted; organization epoch denies stale team authority | external directory lag does not preserve local access |
| `lifecycle.cascade.social_provider_disable` | social provider disable | social_connection for the one tenant + connection | `social_provider.disabled.v1` | every account link and provider-only session under the connection as cleanup, callbacks, token refresh/storage, audit | provider disabled locally for all users by connection epoch | token deletion/revocation remains pending, confirmed, rejected, unsupported, or outcome-unknown |
| `lifecycle.cascade.social_provider_unlink` | social provider unlink | social_link for the one tenant + user + connection; credential for that tenant + user only when the link was an authentication credential | `social_provider.unlinked.v1` | callbacks, token refresh/storage, account linking, sessions issued solely from that link, audit | one user link removed locally | token deletion/revocation remains pending, confirmed, rejected, unsupported, or outcome-unknown |
| `lifecycle.cascade.enterprise_provider_disable` | enterprise provider disable | enterprise_provider for the one tenant + connection; organization for its owning organization | `enterprise_provider.disabled.v1` | SSO routing/callbacks, JIT, SCIM mapping, sessions/enrichment and authorization cleanup, audit | provider disabled locally by enterprise-provider/organization epochs | external provider status never inferred from local completion |
| `lifecycle.cascade.domain_revoke` | enterprise domain claim revoke | enterprise_provider for the one tenant + connection; organization for its owning organization | `domain_claim.revoked.v1` | SSO routing, JIT, SCIM mapping, sessions/enrichment and authorization cleanup, audit | domain routing disabled locally | external provider status never inferred from local completion |
| `lifecycle.cascade.identity_anonymize` | identity anonymize | user, privacy, identifier, credential, factor, and passkey for the one tenant + user; authorization for exact namespace `identity-self` and that subject; every other authorization namespace and social_link scope is cleanup only | `identity.anonymization_requested.v1`, then `identity.anonymized.v1` | every identity lifecycle consumer captured by cascade manifest | `anonymized` after all non-waived acknowledgements; no authority | legal hold retains only required pseudonymous evidence; external limitations remain visible |
| `lifecycle.cascade.identity_delete` | identity delete | user, privacy, identifier, credential, factor, and passkey for the one tenant + user; authorization for exact namespace `identity-self` and that subject; every other authorization namespace and social_link scope is cleanup only | `identity.deletion_requested.v1`, then `identity.deleted.v1` | every identity lifecycle consumer captured by cascade manifest, including `capability/postgres`, `scim`, and `scim/organization` | `deleted` only after hard-delete closure; otherwise `deletion-pending` or `anonymized-held` | unsupported external deletion yields `completed-local-with-external-limitation`, never fully deleted |
| `lifecycle.cascade.organization_archive` | organization archive | organization for the one tenant + organization; provider and subject counters are cleanup only | `organization.archived.v1` | invitations, membership/team writes, active selection, SSO, SCIM core/organization mapping, API keys, grants, organization-scoped impersonation, authorization cleanup, audit | `archived`, reversible; names/domains not reusable | external write disablement remains visible until reconciled |
| `lifecycle.cascade.organization_restore` | organization restore | organization for the one tenant + organization; provider and subject counters are cleanup only | `organization.restored.v1` | invitations, membership/team writes, active selection, SSO, SCIM core/organization mapping, API keys, grants, organization-scoped impersonation cleanup, authorization cleanup, audit | `active`; old sessions/selections and impersonation authority require revalidation | old external authority is not presumed restored |
| `lifecycle.cascade.organization_delete` | organization delete | organization for the one tenant + organization; provider and subject counters are cleanup only | `organization.deletion_requested.v1`, then `organization.deleted.v1` | every organization lifecycle consumer captured by manifest, including SCIM core/organization mapping, organization-scoped impersonation, and authorization cleanup | `deleted` only after closure; otherwise `deletion-pending` or `archived-held` | unsupported external deletion yields completed-local limitation, never full deletion |
| `lifecycle.cascade.global_compromise` | global compromise | global once; signing_key_compromise_epoch once per affected issuer only when an issuer key is compromised/forcibly retired | `authority.global_compromised.v1`; also `signing_key.compromised.v1` exactly when signing compromise epoch bumps | all authority issuers/validators, sessions, API keys, grants, caches, providers, audit as cleanup | compromised global generation permanently denied | unknown external outcomes remain quarantined and visible |

## Cascade generation, acknowledgements, holds, and waivers

At the requesting commit, the owner MUST persist a random cascade ID, generation
1, lifecycle-manifest version, exact required consumer IDs and contract versions,
trigger event/version, deadline, and current legal-hold state. Later manifest
changes MUST NOT silently add or remove consumers from that generation; an
audited migration creates a new generation with an explicit delta.

Each acknowledgement MUST bind cascade ID/generation, consumer ID and contract
version, consumed event ID/schema version, consumer checkpoint and authoritative
mutation version, status, limitation code, PostgreSQL UTC time, and evidence
digest. It MUST commit with the consumer mutation. Duplicate delivery is
idempotent; `pending`, `outcome-unknown`, and unwaived `limited` results remain incomplete. The owning
domain reconciler retries according to `REFERENCE_CONFIGURATION.md`, reports
deadline breach, and alone transitions the aggregate to terminal state.

A legal hold MUST block hard deletion but MUST permit authority revocation and
the maximum lawful anonymization, yielding `anonymized-held` or `archived-held`.
Required local-authority consumers cannot be waived. An unsupported external
delete MAY be waived only by a caller with the named `privacy-administrator`
authorization statement after the local adapter proves the provider lacks that
operation; the result is `completed-local-with-external-limitation`. Rejection,
outage, timeout, or outcome-unknown is not “unsupported” and cannot be waived.
Every hold and waiver MUST be purpose-bound, audited, expiring when applicable,
and included in public redacted status without private details.

## Consumer obligations and proof

The coordinator MUST maintain a versioned lifecycle-consumer manifest before a
destructive owner is assigned. Identity cascades MUST enumerate session,
password, OTP, MFA, WebAuthn, passkey, social OAuth, API key, impersonation,
organization, SSO, SCIM, OAuth server, risk, delivery, audit, and every selected
cache. Organization cascades MUST enumerate membership/team/role, session, API
key, SSO, SCIM, OAuth server, delivery, authorization, audit, and every cache.
Unknown or missing consumers block completion.

Every durable consumer MUST expose typed idempotent apply, acknowledge, status,
and reconcile behavior. Integrated proof MUST cover every matrix and artifact
row; event duplicates, reordering, gaps and unknown versions; consumer-set
snapshot stability; crash/restart; deadline; cross-tenant collision; cache loss;
restore without counter rollback; holds and waivers; provider failure and
unsupported operations; concurrent authentication versus every destructive
trigger; normal signing rotation versus compromise; and inability of old cached
or stateless authority to reactivate.
