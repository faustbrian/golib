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
| `identity/risk/captcha/hcaptcha` | `pkg/identity/risk/captcha/hcaptcha` | `identity/risk/captcha` | proposed | — | `goals/identity-risk-captcha-hcaptcha.md` |
| `identity/risk/captcha/captchafox` | `pkg/identity/risk/captcha/captchafox` | `identity/risk/captcha` | proposed | — | `goals/identity-risk-captcha-captchafox.md` |
| `identity/risk/hibp` | `pkg/identity/risk/hibp` | `identity/risk` | proposed | — | `goals/identity-risk-hibp.md` |
| `identity/password` | `pkg/identity/password` | `identity`<br>`identity/session`<br>`identity/risk`<br>`identity/delivery` | proposed | — | `goals/identity-password.md` |
| `identity/password/postgres` | `pkg/identity/password/postgres` | `identity/password`<br>`identity/postgres` | proposed | — | `goals/identity-password-postgres.md` |
| `identity/username` | `pkg/identity/username` | `identity`<br>`identity/password` | proposed | — | `goals/identity-username.md` |
| `identity/email` | `pkg/identity/email` | `identity`<br>`identity/delivery` | proposed | — | `goals/identity-email.md` |
| `identity/magiclink` | `pkg/identity/magiclink` | `identity`<br>`identity/session`<br>`identity/email`<br>`identity/risk` | proposed | — | `goals/identity-magiclink.md` |
| `identity/otp` | `pkg/identity/otp` | `identity`<br>`identity/session`<br>`identity/risk`<br>`identity/delivery` | proposed | — | `goals/identity-otp.md` |
| `identity/otp/postgres` | `pkg/identity/otp/postgres` | `identity/otp`<br>`identity/postgres` | proposed | — | `goals/identity-otp-postgres.md` |
| `identity/phone` | `pkg/identity/phone` | `identity`<br>`identity/otp`<br>`identity/delivery` | proposed | — | `goals/identity-phone.md` |
| `identity/anonymous` | `pkg/identity/anonymous` | `identity`<br>`identity/session` | proposed | — | `goals/identity-anonymous.md` |
| `identity/mfa` | `pkg/identity/mfa` | `identity`<br>`identity/session`<br>`identity/otp`<br>`identity/risk`<br>`webauthn` | proposed | — | `goals/identity-mfa.md` |
| `identity/mfa/postgres` | `pkg/identity/mfa/postgres` | `identity/mfa`<br>`identity/postgres`<br>`webauthn/postgres` | proposed | — | `goals/identity-mfa-postgres.md` |
| `webauthn` | `pkg/webauthn` | — | ready | — | `goals/webauthn.md` |
| `webauthn/postgres` | `pkg/webauthn/postgres` | `webauthn` | proposed | — | `goals/webauthn-postgres.md` |
| `passkey` | `pkg/passkey` | `identity`<br>`identity/session`<br>`identity/risk`<br>`webauthn` | proposed | — | `goals/passkey.md` |
| `passkey/postgres` | `pkg/passkey/postgres` | `passkey`<br>`identity/postgres`<br>`webauthn/postgres` | proposed | — | `goals/passkey-postgres.md` |
| `identity/oauth` | `pkg/identity/oauth` | `identity`<br>`identity/session`<br>`identity/risk` | proposed | — | `goals/identity-oauth.md` |
| `identity/oauth/postgres` | `pkg/identity/oauth/postgres` | `identity/oauth`<br>`identity/postgres` | proposed | — | `goals/identity-oauth-postgres.md` |
| `identity/oauth/providers` | `pkg/identity/oauth/providers` | `identity/oauth` | proposed | — | `goals/identity-oauth-providers.md` |
| `identity/oauth/onetap` | `pkg/identity/oauth/onetap` | `identity/oauth`<br>`identity/oauth/providers` | proposed | — | `goals/identity-oauth-onetap.md` |
| `identity/oauth/proxy` | `pkg/identity/oauth/proxy` | `identity/oauth` | proposed | — | `goals/identity-oauth-proxy.md` |
| `identity/apikey` | `pkg/identity/apikey` | `identity` | proposed | — | `goals/identity-apikey.md` |
| `identity/apikey/postgres` | `pkg/identity/apikey/postgres` | `identity/apikey`<br>`identity/postgres` | proposed | — | `goals/identity-apikey-postgres.md` |
| `identity/apikey/valkey` | `pkg/identity/apikey/valkey` | `identity/apikey`<br>`identity/apikey/postgres` | proposed | — | `goals/identity-apikey-valkey.md` |
| `identity/impersonation` | `pkg/identity/impersonation` | `identity`<br>`identity/session`<br>`identity/risk` | proposed | — | `goals/identity-impersonation.md` |
| `identity/impersonation/postgres` | `pkg/identity/impersonation/postgres` | `identity/impersonation`<br>`identity/postgres`<br>`identity/session/postgres` | proposed | — | `goals/identity-impersonation-postgres.md` |
| `organization` | `pkg/organization` | `identity`<br>`identity/delivery` | proposed | — | `goals/organization.md` |
| `organization/postgres` | `pkg/organization/postgres` | `organization`<br>`identity/postgres` | proposed | — | `goals/organization-postgres.md` |
| `sso` | `pkg/sso` | `identity`<br>`identity/session`<br>`identity/risk`<br>`organization` | proposed | — | `goals/sso.md` |
| `sso/oidc` | `pkg/sso/oidc` | `sso` | proposed | — | `goals/sso-oidc.md` |
| `sso/oauth2` | `pkg/sso/oauth2` | `sso` | proposed | — | `goals/sso-oauth2.md` |
| `sso/saml` | `pkg/sso/saml` | `sso` | proposed | — | `goals/sso-saml.md` |
| `sso/postgres` | `pkg/sso/postgres` | `sso`<br>`sso/oidc`<br>`sso/oauth2`<br>`sso/saml`<br>`organization/postgres` | proposed | — | `goals/sso-postgres.md` |
| `scim` | `pkg/scim` | `identity`<br>`organization` | proposed | — | `goals/scim.md` |
| `scim/postgres` | `pkg/scim/postgres` | `scim`<br>`organization/postgres` | proposed | — | `goals/scim-postgres.md` |
| `scim/organization` | `pkg/scim/organization` | `scim`<br>`organization` | proposed | — | `goals/scim-organization.md` |
| `oauth-server` | `pkg/oauth-server` | `identity`<br>`identity/session`<br>`identity/risk` | proposed | — | `goals/oauth-server.md` |
| `oauth-server/oidc` | `pkg/oauth-server/oidc` | `oauth-server` | proposed | — | `goals/oauth-server-oidc.md` |
| `oauth-server/device` | `pkg/oauth-server/device` | `oauth-server` | proposed | — | `goals/oauth-server-device.md` |
| `oauth-server/postgres` | `pkg/oauth-server/postgres` | `oauth-server`<br>`oauth-server/device`<br>`identity/postgres`<br>`identity/session/postgres` | proposed | — | `goals/oauth-server-postgres.md` |
| `identity/i18n` | `pkg/identity/i18n` | `identity` | proposed | — | `goals/identity-i18n.md` |
| `identity/http` | `pkg/identity/http` | `identity`<br>`identity/session`<br>`identity/delivery`<br>`identity/risk`<br>`identity/risk/captcha`<br>`identity/password`<br>`identity/username`<br>`identity/email`<br>`identity/magiclink`<br>`identity/otp`<br>`identity/phone`<br>`identity/anonymous`<br>`identity/mfa`<br>`webauthn`<br>`passkey`<br>`identity/oauth`<br>`identity/oauth/providers`<br>`identity/oauth/onetap`<br>`identity/oauth/proxy`<br>`identity/apikey`<br>`identity/impersonation`<br>`organization`<br>`sso`<br>`sso/oidc`<br>`sso/oauth2`<br>`sso/saml`<br>`scim`<br>`scim/organization`<br>`oauth-server`<br>`oauth-server/oidc`<br>`oauth-server/device`<br>`identity/i18n` | proposed | — | `goals/identity-http.md` |
| `identity/reference` | `pkg/identity/reference` | `identity/http`<br>`identity/postgres`<br>`identity/session/postgres`<br>`identity/session/valkey`<br>`identity/risk/postgres`<br>`identity/risk/valkey`<br>`identity/risk/captcha/recaptcha`<br>`identity/risk/captcha/turnstile`<br>`identity/risk/captcha/hcaptcha`<br>`identity/risk/captcha/captchafox`<br>`identity/risk/hibp`<br>`identity/password/postgres`<br>`identity/otp/postgres`<br>`identity/mfa/postgres`<br>`webauthn/postgres`<br>`passkey/postgres`<br>`identity/oauth/postgres`<br>`identity/apikey/postgres`<br>`identity/apikey/valkey`<br>`identity/impersonation/postgres`<br>`organization/postgres`<br>`sso/postgres`<br>`scim/postgres`<br>`oauth-server/postgres` | proposed | — | `goals/identity-reference.md` |
| `identity/identitytest` | `pkg/identity/identitytest` | `identity/reference` | proposed | — | `goals/identity-identitytest.md` |

## Coordinator checklist

Before changing a unit to `ready`, verify that every named requirement exists,
is `verified`, and has current evidence. Before changing a unit to
`in-progress`, record one owner. Before changing it to `verified`, inspect
the final package goal evidence and affected reverse-dependant results. Every
inventory unit MUST have exactly one goal, every goal MUST name exactly one
inventory unit, and the Requires graph MUST remain acyclic.
