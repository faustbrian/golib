# Identity Platform Lifecycle Consumer Manifest

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Version and closure rule

Manifest version `identity-lifecycle-consumers-v1` is immutable for a running
cascade generation. Every `lifecycle.cascade.*` row in
`LIFECYCLE_CASCADES.md` MUST appear exactly once below. Consumers are exact
inventory units or registered existing primitives; a generic subsystem name is
not a consumer ID. The destructive owner snapshots these IDs and their public
contract fingerprints when it creates a cascade. Adding, removing, or moving a
consumer requires a new manifest version and an explicit migration decision.

## Exact consumer sets

| Cascade ID | Owning unit | Required consumer IDs |
| --- | --- | --- |
| `lifecycle.cascade.identity_suspend` | `identity` | `identity/session`, `identity/session/valkey`, `identity/impersonation`, `identity/apikey`, `identity/apikey/valkey`, `oauth-server`, `oauth-server/device`, `oauth-server/oidc`, `authorization`, `authorization/valkey`, `identity/risk`, `audit` |
| `lifecycle.cascade.identity_restore` | `identity` | `identity/session`, `identity/session/valkey`, `identity/impersonation`, `identity/apikey`, `identity/apikey/valkey`, `oauth-server`, `authorization`, `authorization/valkey`, `audit` |
| `lifecycle.cascade.anonymous_upgrade` | `identity/anonymous` | `identity/anonymous`, `identity/session`, `identity/session/valkey`, `identity`, `audit` |
| `lifecycle.cascade.password_reset` | `identity/password` | `identity/session`, `identity/session/valkey`, `identity/password`, `capability/postgres`, `identity/mfa`, `identity/oauth`, `oauth-server`, `audit` |
| `lifecycle.cascade.password_compromise` | `identity/password` | `identity/session`, `identity/session/valkey`, `identity/password`, `capability/postgres`, `identity/mfa`, `identity/oauth`, `oauth-server`, `identity/delivery`, `audit` |
| `lifecycle.cascade.factor_reset` | `identity/mfa` | `identity/mfa`, `webauthn`, `passkey`, `identity/session`, `identity/session/valkey`, `audit` |
| `lifecycle.cascade.identifier_change` | `identity` | `identity/password`, `identity/email`, `identity/phone`, `identity/magiclink`, `identity/otp`, `organization`, `identity/delivery`, `sso`, `identity/risk`, `audit` |
| `lifecycle.cascade.identifier_remove` | `identity` | `identity/password`, `identity/email`, `identity/phone`, `identity/magiclink`, `identity/otp`, `organization`, `identity/delivery`, `sso`, `identity/risk`, `audit` |
| `lifecycle.cascade.membership_role` | `organization` | `identity/session`, `identity/session/valkey`, `authorization`, `authorization/valkey`, `identity/apikey`, `identity/apikey/valkey`, `oauth-server`, `oauth-server/oidc`, `identity/impersonation`, `organization`, `audit` |
| `lifecycle.cascade.membership_remove` | `organization` | `identity/session`, `identity/session/valkey`, `authorization`, `authorization/valkey`, `identity/apikey`, `identity/apikey/valkey`, `oauth-server`, `oauth-server/oidc`, `identity/impersonation`, `organization`, `audit` |
| `lifecycle.cascade.team_change` | `organization` | `authorization`, `authorization/valkey`, `identity/session`, `identity/session/valkey`, `scim/organization`, `audit` |
| `lifecycle.cascade.team_delete` | `organization` | `authorization`, `authorization/valkey`, `identity/session`, `identity/session/valkey`, `scim/organization`, `audit` |
| `lifecycle.cascade.social_provider_disable` | `identity/oauth` | `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/proxy`, `identity`, `identity/session`, `identity/session/valkey`, `identity/delivery`, `audit` |
| `lifecycle.cascade.social_provider_unlink` | `identity/oauth` | `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/proxy`, `identity`, `identity/session`, `identity/session/valkey`, `identity/delivery`, `audit` |
| `lifecycle.cascade.enterprise_provider_disable` | `sso` | `sso`, `identity/session`, `identity/session/valkey`, `organization`, `scim/organization`, `authorization`, `authorization/valkey`, `audit` |
| `lifecycle.cascade.domain_revoke` | `sso/domain-verification` | `sso`, `organization`, `scim/organization`, `identity/session`, `identity/session/valkey`, `authorization`, `authorization/valkey`, `audit` |
| `lifecycle.cascade.identity_anonymize` | `identity` | `identity/session`, `identity/session/valkey`, `identity/password`, `identity/email`, `identity/phone`, `identity/otp`, `identity/mfa`, `webauthn`, `passkey`, `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/proxy`, `identity/apikey`, `identity/apikey/valkey`, `identity/impersonation`, `organization`, `sso`, `scim`, `scim/organization`, `oauth-server`, `oauth-server/oidc`, `identity/risk`, `identity/delivery`, `authorization`, `authorization/valkey`, `capability/postgres`, `audit` |
| `lifecycle.cascade.identity_delete` | `identity` | `identity/session`, `identity/session/valkey`, `identity/password`, `identity/email`, `identity/phone`, `identity/otp`, `identity/mfa`, `webauthn`, `passkey`, `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/proxy`, `identity/apikey`, `identity/apikey/valkey`, `identity/impersonation`, `organization`, `sso`, `scim`, `scim/organization`, `oauth-server`, `oauth-server/device`, `oauth-server/oidc`, `identity/risk`, `identity/delivery`, `authorization`, `authorization/valkey`, `capability/postgres`, `audit` |
| `lifecycle.cascade.organization_archive` | `organization` | `organization`, `identity/session`, `identity/session/valkey`, `identity/apikey`, `identity/apikey/valkey`, `identity/impersonation`, `sso`, `scim`, `scim/organization`, `oauth-server`, `oauth-server/oidc`, `identity/delivery`, `authorization`, `authorization/valkey`, `audit` |
| `lifecycle.cascade.organization_restore` | `organization` | `organization`, `identity/session`, `identity/session/valkey`, `identity/apikey`, `identity/apikey/valkey`, `identity/impersonation`, `sso`, `scim`, `scim/organization`, `oauth-server`, `oauth-server/oidc`, `identity/delivery`, `authorization`, `authorization/valkey`, `audit` |
| `lifecycle.cascade.organization_delete` | `organization` | `organization`, `identity/session`, `identity/session/valkey`, `identity/apikey`, `identity/apikey/valkey`, `identity/impersonation`, `sso`, `scim`, `scim/organization`, `oauth-server`, `oauth-server/oidc`, `identity/delivery`, `authorization`, `authorization/valkey`, `audit` |
| `lifecycle.cascade.global_compromise` | `identity` | `identity/session`, `identity/session/valkey`, `identity/password`, `identity/email`, `identity/magiclink`, `identity/otp`, `identity/phone`, `identity/anonymous`, `identity/mfa`, `webauthn`, `passkey`, `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/proxy`, `identity/apikey`, `identity/apikey/valkey`, `identity/impersonation`, `organization`, `sso`, `scim`, `oauth-server`, `oauth-server/device`, `oauth-server/oidc`, `authorization`, `authorization/valkey`, `identity/risk`, `identity/delivery`, `audit` |

## Privacy-export contributor set

The exact version-1 privacy-export contributor set is `identity`,
`identity/session`, `identity/password`, `identity/email`, `identity/phone`, `identity/otp`, `identity/mfa`,
`webauthn`, `passkey`, `identity/oauth`, `identity/apikey`,
`identity/anonymous`, `identity/impersonation`, `organization`, `sso`, `scim`,
`scim/organization`, `oauth-server`, `identity/risk`, `identity/delivery`,
`authorization`, and `audit`. A request MUST snapshot each ID and its contract
version. A contributor MAY return an explicit `not-applicable` fragment, but
silence, timeout, an unknown contract version, or an unproved checkpoint MUST
block publication. This set is independent of cascade generations and MUST NOT
be inferred from whichever packages happen to return data.

## Checkpoint persistence ownership

Owning and required-consumer IDs above are the only semantic acknowledgement
identities. A selected storage adapter MUST persist its semantic unit's
checkpoint with the same authoritative mutation, but MUST NOT emit a duplicate
semantic acknowledgement. The reference profile uses these exact persistence
bindings: `identity` uses `identity/postgres`; `identity/session` uses
`identity/session/postgres`; `identity/password` uses
`identity/password/postgres`; `identity/otp` uses `identity/otp/postgres`;
`identity/mfa` uses `identity/mfa/postgres`; `identity/phone` uses
`identity/postgres` for identifier-change/remove semantic checkpoints in the
same authoritative mutation and emits no duplicate semantic acknowledgement;
`webauthn` uses
`webauthn/postgres`; `passkey` uses `passkey/postgres`; `identity/oauth` uses
`identity/oauth/postgres`; `identity/apikey` uses `identity/apikey/postgres`;
`identity/anonymous` uses `identity/anonymous/postgres`;
`identity/impersonation` uses `identity/impersonation/postgres`; `organization`
uses `organization/postgres`; `sso` uses `sso/postgres`; `scim` and
`scim/organization` use `scim/postgres`; `oauth-server` uses
`oauth-server/postgres`; `identity/delivery` uses
`identity/delivery/postgres`; and `identity/risk` uses
`identity/risk/postgres` for its durable semantic checkpoint while
`identity/risk/valkey` persists a subordinate ephemeral-store checkpoint that
must be reconciled into the one `identity/risk` acknowledgement. The
`identity/reference` composition invokes global compromise but owns neither its
authority row nor an acknowledgement; `identity` owns the cascade and
`identity/postgres` persists its global generation and owner checkpoint.

The positive-cache projections `identity/session/valkey` and
`identity/apikey/valkey` are intentionally separate required consumers in each
applicable row because invalidation is their observable behavior; each persists
its own generation-bound cache checkpoint. The selected existing primitive
`authorization/valkey` is likewise a separate required consumer in every row
that requires `authorization`; it persists its generation-bound invalidation
checkpoint independently of the `authorization` semantic mutation. `identity/risk/valkey` is an
ephemeral mutation authority rather than a positive-cache projection, so it
does not duplicate the `identity/risk` semantic acknowledgement.

`capability/postgres` is the exact lifecycle consumer for issued or reserved
password-reset capabilities. Its password-reset and password-compromise
acknowledgements MUST atomically transition every other `issued` capability in
that tenant, purpose, and subject scope to `revoked`. A `reserved` capability
MUST atomically set `revocation_pending=true`; its stale authority-version
binding blocks apply, and the cascade remains pending until reconciliation
proves whether its owning command committed and transitions it legally to
`finalized` or `revoked`. Finalized, released, expired, and revoked records
remain terminal and are never reactivated.
For identity anonymization and deletion it MUST apply the same rules to every
issued or reserved capability scoped to that tenant and subject, including
privacy-export download capabilities, before the cascade can close. Its
acknowledgement MUST bind the exact cascade ID, privacy epoch, and generation;
a checkpoint from an older generation cannot close the destructive transition.

## Consumer contract

Each listed consumer MUST expose a versioned idempotent apply/status/reconcile
contract, bind acknowledgements to cascade ID and generation, reject unknown
event or manifest versions, persist its checkpoint with its mutation, and
report `applied`, `not-applicable`, `pending`, `limited`, or `outcome-unknown`.
Only `applied` and a policy-permitted `not-applicable` close a required local
consumer. External `limited` outcomes follow the waiver rules in
`LIFECYCLE_CASCADES.md`; they never restore local authority.

## Validation and change control

The coordinator validator MUST prove a bijection between cascade IDs here and
the destructive-state matrix, resolve every owner and consumer, and reject
duplicates. Before assigning an owning or consuming unit, the coordinator MUST
render the exact rows that name it. Final end-state proof MUST exercise every
consumer in every row, including duplicate, reordered, crashed, delayed,
limited, and outcome-unknown delivery.
