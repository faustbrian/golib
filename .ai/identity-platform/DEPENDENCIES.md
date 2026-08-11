# Identity Platform Dependencies

## Status model

`proposed` means scoped but not authorized. `ready` means all start gates
are satisfied and one agent may claim the unit. `in-progress` has one owner.
`implemented-unverified` has implementation but incomplete release evidence.
Only `verified` satisfies a dependant. `blocked` records an explicit blocker.

```text
proposed -> ready -> in-progress -> implemented-unverified -> verified
                  \-> blocked -> in-progress
```

A unit MUST NOT become `ready` unless all `Requires` units are `verified`.
It MUST return to `implemented-unverified` when changed complete inputs
invalidate its evidence.

## Dependency DAG

```mermaid
flowchart TD
  session[identity/session]
  delivery[identity/delivery]
  risk[identity/risk]
  captcha[identity/risk/captcha]
  recaptcha[identity/risk/captcha/recaptcha]
  turnstile[identity/risk/captcha/turnstile]
  hcaptcha[identity/risk/captcha/hcaptcha]
  captchafox[identity/risk/captcha/captchafox]
  hibp[identity/risk/hibp]
  email[identity/email]
  password[identity/password]
  username[identity/username]
  magiclink[identity/magiclink]
  otp[identity/otp]
  phone[identity/phone]
  anonymous[identity/anonymous]
  mfa[identity/mfa]
  oauth[identity/oauth]
  oauth_providers[identity/oauth/providers]
  oauth_onetap[identity/oauth/onetap]
  oauth_proxy[identity/oauth/proxy]
  apikey[identity/apikey]
  impersonation[identity/impersonation]
  identity --> identity_pg[identity/postgres]
  identity --> session
  session --> session_pg[identity/session/postgres]
  identity_pg --> session_pg
  session --> session_valkey[identity/session/valkey]
  identity --> risk
  identity_pg --> risk_pg[identity/risk/postgres]
  risk --> risk_pg[identity/risk/postgres]
  risk --> risk_valkey[identity/risk/valkey]
  risk --> captcha
  captcha --> recaptcha
  captcha --> turnstile
  captcha --> hcaptcha
  captcha --> captchafox
  risk --> hibp
  identity --> email
  delivery --> email
  identity --> password
  session --> password
  risk --> password
  delivery --> password
  identity --> username
  password --> username
  email --> magiclink
  identity --> magiclink
  session --> magiclink
  risk --> magiclink
  identity --> otp
  delivery --> otp
  session --> otp
  risk --> otp
  otp --> phone
  identity --> phone
  delivery --> phone
  identity --> anonymous
  session --> anonymous
  otp --> mfa
  identity --> mfa
  session --> mfa
  risk --> mfa
  webauthn --> passkey
  identity --> passkey
  session --> passkey
  risk --> passkey
  identity --> oauth
  session --> oauth
  risk --> oauth
  oauth --> oauth_providers
  oauth --> oauth_onetap
  oauth_providers --> oauth_onetap
  oauth --> oauth_proxy
  identity --> apikey
  identity --> impersonation
  session --> impersonation
  risk --> impersonation
  identity --> organization
  organization --> organization_pg[organization/postgres]
  identity_pg --> organization_pg
  organization --> sso
  identity --> sso
  session --> sso
  risk --> sso
  sso --> sso_oidc[sso/oidc]
  sso --> sso_oauth2[sso/oauth2]
  sso --> sso_saml[sso/saml]
  sso --> sso_pg[sso/postgres]
  organization_pg --> sso_pg
  organization --> scim
  identity --> scim
  scim --> scim_pg[scim/postgres]
  organization_pg --> scim_pg
  scim --> scim_org[scim/organization]
  organization --> scim_org
  identity --> oauth_server[oauth-server]
  session --> oauth_server
  risk --> oauth_server
  oauth_server --> oauth_server_oidc[oauth-server/oidc]
  oauth_server --> oauth_server_device[oauth-server/device]
  oauth_server --> oauth_server_pg[oauth-server/postgres]
  identity_pg --> oauth_server_pg
  session_pg --> oauth_server_pg
  identity --> i18n[identity/i18n]
  identity --> identity_http[identity/http]
  identity_pg --> identity_http
  session --> identity_http
  session_pg --> identity_http
  session_valkey --> identity_http
  delivery --> identity_http
  risk --> identity_http
  risk_pg --> identity_http
  risk_valkey --> identity_http
  captcha --> identity_http
  recaptcha --> identity_http
  turnstile --> identity_http
  hcaptcha --> identity_http
  captchafox --> identity_http
  hibp --> identity_http
  password --> identity_http
  username --> identity_http
  email --> identity_http
  magiclink --> identity_http
  otp --> identity_http
  phone --> identity_http
  anonymous --> identity_http
  mfa --> identity_http
  webauthn --> identity_http
  passkey --> identity_http
  oauth --> identity_http
  oauth_providers --> identity_http
  oauth_onetap --> identity_http
  oauth_proxy --> identity_http
  apikey --> identity_http
  impersonation --> identity_http
  organization --> identity_http
  organization_pg --> identity_http
  sso --> identity_http
  sso_oidc --> identity_http
  sso_oauth2 --> identity_http
  sso_saml --> identity_http
  sso_pg --> identity_http
  scim --> identity_http
  scim_pg --> identity_http
  scim_org --> identity_http
  oauth_server --> identity_http
  oauth_server_oidc --> identity_http
  oauth_server_device --> identity_http
  oauth_server_pg --> identity_http
  i18n --> identity_http
  identity_http --> identitytest[identity/identitytest]
```

The `Requires` field in `INVENTORY.md` is authoritative. The diagram is
explanatory and MUST change in the same commit when an edge changes.
