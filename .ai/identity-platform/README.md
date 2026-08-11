# Identity Platform Execution Order

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## How to use this program

This directory is the staging area for the identity-platform goals:

1. Read `PROGRAM.md` for product and ownership decisions.
2. Read `COMMON_REQUIREMENTS.md` for requirements shared by every package.
3. Use `INVENTORY.md` as the authoritative status and dependency record.
4. Use `DEPENDENCIES.md` to inspect the complete dependency graph.
5. Execute only a goal whose inventory status is `ready`.

The waves below state the earliest valid execution order. Units within one
wave MAY run in parallel, but a wave is not a blanket authorization to start
all of its units. An agent MUST still wait until every unit named in its
`Requires` field is `verified`, the coordinator has changed its status to
`ready`, and one owner has claimed it.

## Wave 0: independent roots

These units have no dependency on another future identity-platform package and
MAY begin immediately when their existing primitive audits are current:

1. `identity`
2. `identity/delivery`
3. `webauthn`

They establish the user/account model, delivery contracts, and WebAuthn
protocol layer. Starting them first allows the remaining product work to
compose stable contracts instead of inventing local substitutes.

## Wave 1: identity foundations

These units MAY begin after `identity` is verified, with the additional
prerequisites shown:

1. `identity/postgres` after `identity`
2. `identity/session` after `identity`
3. `identity/risk` after `identity`
4. `identity/email` after `identity` and `identity/delivery`
5. `identity/apikey` after `identity`
6. `organization` after `identity`

This wave establishes durable identities, sessions, auth-specific risk
decisions, verified email lifecycle, managed API keys, and organization
contracts. Dependants MUST use these public contracts rather than creating
parallel identity or session models.

## Wave 2: complete identity journeys and product cores

Each unit below MAY begin as soon as all listed prerequisites are verified:

1. `identity/session/postgres` after `identity/session` and
   `identity/postgres`
2. `identity/session/valkey` after `identity/session`
3. `identity/risk/postgres` after `identity/risk` and `identity/postgres`
4. `identity/risk/valkey` after `identity/risk`
5. `identity/risk/captcha` after `identity/risk`
6. `identity/risk/hibp` after `identity/risk`
7. `identity/password` after `identity`, `identity/session`,
   `identity/risk`, and `identity/delivery`
8. `identity/magiclink` after `identity`, `identity/session`,
   `identity/email`, and `identity/risk`
9. `identity/otp` after `identity`, `identity/session`,
   `identity/risk`, and `identity/delivery`
10. `identity/anonymous` after `identity` and `identity/session`
11. `passkey` after `identity`, `identity/session`, `identity/risk`,
    and `webauthn`
12. `identity/oauth` after `identity`, `identity/session`, and
    `identity/risk`
13. `identity/impersonation` after `identity`, `identity/session`, and
    `identity/risk`
14. `organization/postgres` after `organization` and `identity/postgres`
15. `sso` after `identity`, `identity/session`, `identity/risk`, and
    `organization`
16. `scim` after `identity` and `organization`
17. `oauth-server` after `identity`, `identity/session`, and
    `identity/risk`

The units are peers within this wave; their numeric order is for readability,
not an additional dependency. For example, HIBP and CAPTCHA adapters do not
block password lifecycle work because `identity/password` consumes the
provider-neutral risk contract rather than importing provider implementations.

## Wave 3: protocol, provider, and persistence extensions

These leaf units MAY begin after their corresponding Wave 2 cores are verified:

1. `identity/risk/captcha/recaptcha` after `identity/risk/captcha`
2. `identity/risk/captcha/turnstile` after `identity/risk/captcha`
3. `identity/phone` after `identity`, `identity/otp`, and
   `identity/delivery`
4. `identity/mfa` after `identity`, `identity/session`, `identity/otp`,
   and `identity/risk`
5. `sso/oidc` after `sso`
6. `sso/oauth2` after `sso`
7. `sso/saml` after `sso`
8. `sso/postgres` after `sso` and `organization/postgres`
9. `scim/postgres` after `scim` and `organization/postgres`
10. `scim/organization` after `scim` and `organization`
11. `oauth-server/oidc` after `oauth-server`
12. `oauth-server/device` after `oauth-server`
13. `oauth-server/postgres` after `oauth-server`, `identity/postgres`,
    and `identity/session/postgres`

## Readiness rule

The order is dependency-driven, not calendar-driven. A later-wave unit MAY
start before every earlier-wave unit is complete when all of its own
prerequisites are already `verified`. Conversely, an earlier-wave unit MUST
NOT start while any of its prerequisites is merely `in-progress` or
`implemented-unverified`.

When a module is first scaffolded, its planning goal MUST move unchanged from
`goals/` to its canonical `pkg/<module>/.ai/GOAL.md` path and the inventory
link MUST change in the same coherent batch.
