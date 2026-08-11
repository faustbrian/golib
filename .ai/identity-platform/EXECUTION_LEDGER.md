# Identity Platform Execution Ledger

This coordinator-owned ledger records recoverable orchestration identity.
`INVENTORY.md` remains authoritative for dependency, status and current
owner/blocker. Every inventory status or owner/blocker change MUST update the
corresponding ledger row in the same coordinator commit. Ledger-only metadata
such as a newly known prior commit hash or evidence pointer MAY be finalized in
a later ledger-only commit because the hash of a commit cannot be embedded in
that same commit. Secret values and provider credentials MUST NOT appear here.

`Generation` is incremented whenever an abandoned assignment is safely returned
to `ready`. `Assignment commit` is `pending` only in the assignment-state
commit; the immediately following ledger-only finalization MUST replace it with
that prior commit's hash before the worker starts. `Integration checkpoint` is
the already-known non-fast-forward worker merge commit, never the hash of the
status commit that records it. `External evidence` records only `not-needed`, `available`,
`unavailable:{safe-profile-id}`, or an attributable evidence-record path. A dash
means never assigned.

After `initial`, `Last transition` MUST use
`v<positive-integer> <status> owner=<owner-or-dash> at=<RFC3339>` and mirror
the inventory status and owner/blocker exactly. Every later ledger update MUST
increment that version. The validator enforces this mirror and per-state field
shape; Git history remains the transition journal.

Worker task and owner IDs MUST be whitespace-free safe identifiers. Worker
branches MUST be conventional and worktrees absolute. Integrated gate
fingerprints MUST use `sha256:<64-lowercase-hex>`. A verified row MUST record
`not-needed`, `available`, or an attributable `.ai/...` evidence path; it MUST
NOT retain missing or unavailable evidence.

| Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Worker commit | Integration checkpoint | Gate fingerprint | External evidence | Last transition |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `identity` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/session` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/session/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/session/valkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/delivery` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/valkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/recaptcha` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/turnstile` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/hcaptcha` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/captcha/captchafox` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/risk/hibp` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/password` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/password/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/username` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/email` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/magiclink` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/otp` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/otp/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/phone` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/anonymous` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/mfa` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/mfa/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `webauthn` | 0 | — | — | — | — | — | — | — | — | initial |
| `webauthn/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `passkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `passkey/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/providers` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/onetap` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/oauth/proxy` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/apikey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/apikey/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/apikey/valkey` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/impersonation` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/impersonation/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `organization` | 0 | — | — | — | — | — | — | — | — | initial |
| `organization/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/oidc` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/oauth2` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/saml` | 0 | — | — | — | — | — | — | — | — | initial |
| `sso/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `scim` | 0 | — | — | — | — | — | — | — | — | initial |
| `scim/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `scim/organization` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server/oidc` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server/device` | 0 | — | — | — | — | — | — | — | — | initial |
| `oauth-server/postgres` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/i18n` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/http` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/reference` | 0 | — | — | — | — | — | — | — | — | initial |
| `identity/identitytest` | 0 | — | — | — | — | — | — | — | — | initial |
