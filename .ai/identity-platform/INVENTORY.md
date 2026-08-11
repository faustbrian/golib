# Identity Platform Inventory

This is the authoritative execution inventory. A coordinator MUST preserve the
status rules in `DEPENDENCIES.md`. An agent MUST NOT claim a `proposed` unit.

| Unit | Canonical module | Requires verified | Status | Owner/blocker | Goal |
| --- | --- | --- | --- | --- | --- |
| `identity` | `pkg/identity` | — | ready | — | `goals/identity.md` |
| `identity/postgres` | `pkg/identity/postgres` | `identity` | proposed | — | `goals/identity-postgres.md` |
| `identity/session` | `pkg/identity/session` | `identity` | proposed | — | `goals/identity-session.md` |
| `identity/session/postgres` | `pkg/identity/session/postgres` | `identity/session`<br>`identity/postgres` | proposed | — | `goals/identity-session-postgres.md` |
| `identity/session/valkey` | `pkg/identity/session/valkey` | `identity/session` | proposed | — | `goals/identity-session-valkey.md` |
| `identity/delivery` | `pkg/identity/delivery` | — | ready | — | `goals/identity-delivery.md` |
| `identity/risk` | `pkg/identity/risk` | `identity` | proposed | — | `goals/identity-risk.md` |
| `identity/risk/postgres` | `pkg/identity/risk/postgres` | `identity/risk`<br>`identity/postgres` | proposed | — | `goals/identity-risk-postgres.md` |
| `identity/risk/valkey` | `pkg/identity/risk/valkey` | `identity/risk` | proposed | — | `goals/identity-risk-valkey.md` |
| `identity/risk/captcha` | `pkg/identity/risk/captcha` | `identity/risk` | proposed | — | `goals/identity-risk-captcha.md` |
| `identity/risk/captcha/recaptcha` | `pkg/identity/risk/captcha/recaptcha` | `identity/risk/captcha` | proposed | — | `goals/identity-risk-captcha-recaptcha.md` |
| `identity/risk/captcha/turnstile` | `pkg/identity/risk/captcha/turnstile` | `identity/risk/captcha` | proposed | — | `goals/identity-risk-captcha-turnstile.md` |
| `identity/risk/hibp` | `pkg/identity/risk/hibp` | `identity/risk` | proposed | — | `goals/identity-risk-hibp.md` |
| `identity/password` | `pkg/identity/password` | `identity`<br>`identity/session`<br>`identity/risk`<br>`identity/delivery` | proposed | — | `goals/identity-password.md` |
| `identity/email` | `pkg/identity/email` | `identity`<br>`identity/delivery` | proposed | — | `goals/identity-email.md` |
| `identity/magiclink` | `pkg/identity/magiclink` | `identity`<br>`identity/session`<br>`identity/email`<br>`identity/risk` | proposed | — | `goals/identity-magiclink.md` |
| `identity/otp` | `pkg/identity/otp` | `identity`<br>`identity/session`<br>`identity/risk`<br>`identity/delivery` | proposed | — | `goals/identity-otp.md` |
| `identity/phone` | `pkg/identity/phone` | `identity`<br>`identity/otp`<br>`identity/delivery` | proposed | — | `goals/identity-phone.md` |
| `identity/anonymous` | `pkg/identity/anonymous` | `identity`<br>`identity/session` | proposed | — | `goals/identity-anonymous.md` |
| `identity/mfa` | `pkg/identity/mfa` | `identity`<br>`identity/session`<br>`identity/otp`<br>`identity/risk` | proposed | — | `goals/identity-mfa.md` |
| `webauthn` | `pkg/webauthn` | — | ready | — | `goals/webauthn.md` |
| `passkey` | `pkg/passkey` | `identity`<br>`identity/session`<br>`identity/risk`<br>`webauthn` | proposed | — | `goals/passkey.md` |
| `identity/oauth` | `pkg/identity/oauth` | `identity`<br>`identity/session`<br>`identity/risk` | proposed | — | `goals/identity-oauth.md` |
| `identity/apikey` | `pkg/identity/apikey` | `identity` | proposed | — | `goals/identity-apikey.md` |
| `identity/impersonation` | `pkg/identity/impersonation` | `identity`<br>`identity/session`<br>`identity/risk` | proposed | — | `goals/identity-impersonation.md` |
| `organization` | `pkg/organization` | `identity` | proposed | — | `goals/organization.md` |
| `organization/postgres` | `pkg/organization/postgres` | `organization`<br>`identity/postgres` | proposed | — | `goals/organization-postgres.md` |
| `sso` | `pkg/sso` | `identity`<br>`identity/session`<br>`identity/risk`<br>`organization` | proposed | — | `goals/sso.md` |
| `sso/oidc` | `pkg/sso/oidc` | `sso` | proposed | — | `goals/sso-oidc.md` |
| `sso/oauth2` | `pkg/sso/oauth2` | `sso` | proposed | — | `goals/sso-oauth2.md` |
| `sso/saml` | `pkg/sso/saml` | `sso` | proposed | — | `goals/sso-saml.md` |
| `sso/postgres` | `pkg/sso/postgres` | `sso`<br>`organization/postgres` | proposed | — | `goals/sso-postgres.md` |
| `scim` | `pkg/scim` | `identity`<br>`organization` | proposed | — | `goals/scim.md` |
| `scim/postgres` | `pkg/scim/postgres` | `scim`<br>`organization/postgres` | proposed | — | `goals/scim-postgres.md` |
| `scim/organization` | `pkg/scim/organization` | `scim`<br>`organization` | proposed | — | `goals/scim-organization.md` |
| `oauth-server` | `pkg/oauth-server` | `identity`<br>`identity/session`<br>`identity/risk` | proposed | — | `goals/oauth-server.md` |
| `oauth-server/oidc` | `pkg/oauth-server/oidc` | `oauth-server` | proposed | — | `goals/oauth-server-oidc.md` |
| `oauth-server/device` | `pkg/oauth-server/device` | `oauth-server` | proposed | — | `goals/oauth-server-device.md` |
| `oauth-server/postgres` | `pkg/oauth-server/postgres` | `oauth-server`<br>`identity/postgres`<br>`identity/session/postgres` | proposed | — | `goals/oauth-server-postgres.md` |

## Coordinator checklist

Before changing a unit to `ready`, verify that every named requirement exists,
is `verified`, and has current evidence. Before changing a unit to
`in-progress`, record one owner. Before changing it to `verified`, inspect
the final package goal evidence and affected reverse-dependant results. Every
inventory unit MUST have exactly one goal, every goal MUST name exactly one
inventory unit, and the Requires graph MUST remain acyclic.
