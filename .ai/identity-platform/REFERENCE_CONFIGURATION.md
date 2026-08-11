# Identity Platform Reference Configuration Manifest

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Manifest rules

This file is the canonical machine-value authority for the reference profile.
`REFERENCE_PROFILE.md` is explanatory only. A conflict between the files MUST
block assignment and MUST be corrected in `REFERENCE_PROFILE.md`; the validator
MUST prove every explanatory value has an equal canonical row here.
`identity/reference` MUST expose these fields through one typed immutable
schema. Every declarative table row is a normative **MUST** requirement.

Every supported field MUST have one atomic primitive row or one explicitly
closed structured-policy row here. A structured row's clauses in its reference
value and semantics are its named fields, fixed enum values, and invariants;
the generated type MUST expose them without a free-form string/map escape.
A field absent from this
manifest is unsupported and construction MUST reject it; there is no implicit
“required by package invariant” exception. Every optional feature MUST have an
explicit Boolean/mode enable field and each dependency MUST have an explicit
required-if predicate.

### Schema rules

The generated schema MUST materialize the following metadata for every row:

| Metadata | Exact derivation |
| --- | --- |
| row ID | `ref.` plus the exact path; unique and stable |
| primitive type/unit | the generator MUST assign exactly one of `bool`, `string:utf8`, `bytes:opaque`, `uint64:count`, `uint64:bytes`, `uint64:codepoints`, `decimal:fixed6`, `duration:nanoseconds`, `rate:uint64-per-duration`, `uri:absolute`, `url:absolute-https`, `path:absolute`, `list:cidr`, `list:origin`, `list:string`, `list:duration`, `enum:string`, `handle:<kind>`, or `struct:<row-id>` using the ordered grammar below; the first matching rule is authoritative and later overlapping rules are ignored |
| default | the exact reference value; `REQUIRED` rows have no default |
| minimum/maximum/enum | the exact values in the semantics column; an omitted bound for a numerical field or omitted closed set for an enum is a manifest error |
| required-if | `always` for `REQUIRED`, the exact predicate stated by the row, otherwise `false` |
| secret | `true` exactly for `secrets.*`, `providers.<id>.client_secret`, `captcha.<id>.secret`, `postgres.dsn`, `delivery.sender`, `saml.sp_signing_key`, and `saml.sp_decryption_key`; `false` for every other path; value-dependent classification is forbidden |
| reload class | `atomic` only when the row says reloadable; otherwise `startup-only` |
| stable validation code | `identity.reference.` + path with `_` preserved and `.` replaced by `/` + `/invalid` |
| fingerprint group | `identity-reference-v1/` plus the first path segment; secret values contribute a keyed digest, never plaintext |

`validate.rb` MUST materialize and validate this complete metadata object for
every row; checking only row presence or typeability is insufficient. Every
table row MUST compile to this full metadata shape; prose-only or compound
fields without a closed `struct:<row-id>` schema are invalid. Unknown/duplicate
fields, unknown struct members, and unknown enum values fail.
There is no inferred environment-variable name.

Type selection scans the complete reference-value and semantics cells using
this ordered grammar: a value containing `struct:ref.` is `struct:<row-id>`;
`true`/`false` is `bool`; any row containing the noun `handle` is
`handle:<last-path-segment>`; a path under `secrets.*` whose row does not contain
`handle`, a path ending `.client_secret`, a path matching
`captcha.<id>.secret`, and any secret key/token value declaring an exact byte
length is `bytes:opaque`; a configuration path ending
in `_bytes` or `.max_bytes` is `uint64:bytes`; a value with `/duration` is
`rate:uint64-per-duration`; a fractional decimal without a time/data unit is
`decimal:fixed6`; any value containing a numeric duration default is
`duration:nanoseconds`; remaining numerical codepoint/count values use the
corresponding named scalar; an absolute HTTPS literal is
`url:absolute-https`; an absolute slash-prefixed literal is `path:absolute`;
comma-separated homogeneous values use the corresponding list type; a `REQUIRED` value takes
the first exact type noun while scanning its reference value and then its
semantics (`URL`, `URI`, `string`, `list`, or `handle`) and is invalid if none
occurs; every remaining textual
value is `enum:string` whose closed set is the exact reference value plus only
additional values explicitly introduced by the word `enum` in its semantics. The
first matching rule wins; zero matches are impossible and multiple matches do
not create ambiguity.

`decimal:fixed6` stores an integer scaled by 1,000,000, accepts at most six
fractional decimal digits, rejects binary floating-point input, and compares
integers exactly. `rate:uint64-per-duration` stores an unsigned numerator and a
positive integer-nanosecond denominator; refill uses checked multiplication,
floor division, and an exact carried remainder and MUST never round upward.

Paths containing `<id>` are catalog templates. Their template row ID is the
literal `ref.` plus the unexpanded path (for example,
`ref.providers.<id>.enabled`). `identity/reference` MUST expand it for every ID
in the versioned provider/CAPTCHA catalog using instance ID
`ref.<catalog-version>.<expanded-path>` (for example,
`ref.provider-catalog-v1.providers.google.enabled`). Goals enumerate template
IDs; generated assignments enumerate the template ID and each selected instance
ID. IDs outside the pinned catalog, duplicate expansions, and an instance whose
template is absent MUST fail validation.

`CONFIGURATION_CATALOGS.json` is the sole machine-readable authority for both
catalogs. Its IDs are sorted and unique; each catalog pins the SHA-256 digest of
its LF-terminated ID list. The version rows here MUST exactly match that file.
Applicability validation MUST expand every selected template against the
corresponding catalog, construct every instance ID, reject collisions and
unknown catalog IDs, and prove every expansion retains its selected template.

The following rows use closed structured types; no additional member is
permitted:

| Row ID/type | Exact members | Semantics |
| --- | --- | --- |
| `struct:ref.identity.delete.proof` | `password_user = fresh_session_and_current_password`; `passkey_user = fresh_uv_passkey`; `provider_user = emailed_capability`; `no_email_recovery = fresh_uv_passkey_or_administrator_recovery`; `ttl = 15m`; `admin_authorization = identity.delete.admin` | closed immutable policy |
| `struct:ref.platform.authority` | `role_store = authorization/postgres`; `assignment_store = authorization/postgres`; `identity_store = identity/postgres`; `bootstrap = one_time_out_of_band`; `role_operations = identity.platform.role.create,identity.platform.role.update,identity.platform.role.delete`; `permission_statement_operations = identity.platform.permission-statement.create,identity.platform.permission-statement.update,identity.platform.permission-statement.delete`; `assignment_operation = identity.admin.user-role-set`; `audit = audit/postgres`; `cache = authorization/valkey_positive_only` | closed immutable policy; every successful mutation increments the platform authority version and invalidates prior-version decisions |
| `struct:ref.audit.retention_policy` | `durable_store = audit/postgres`; `initialization = insert_once_from_bootstrap_defaults`; `runtime_authority = durable_policy_version`; `restart = load_durable_never_overwrite`; `update_operation = identity.audit-retention.policy.update`; `initialization_event = identity.audit_retention.change_policy`; `unset = unsupported`; `reset = unsupported`; `plan_binding = policy_version_and_hold_checkpoint` | sole closed authority policy; concurrent initialization is insert-once and every later mutation uses an expected durable version |
| `struct:ref.session.cookie.max_age` | `browser_session = omitted`; `persistent = remaining_absolute_lifetime`; `minimum = 15m`; `maximum = 7d`; `session_bound = true` | closed immutable policy; omission and duration are distinct typed variants |
| `struct:ref.organization.active_scope` | `owner = session`; `container = browser_container_id`; `membership_check = every_switch_and_use`; `user_global = false` | closed immutable policy |
| `struct:ref.webauthn.profile` | `standard = W3C-WebAuthn-Level-3-CR-2026-05-26`; `authenticator_protocol_claim = none`; `backup_flags = required`; `same_origin = crossOrigin_false_and_topOrigin_absent` | closed immutable policy |
| `struct:ref.api_key.encoding` | `prefix = idk_`; `secret_bytes = 32`; `alphabet = base64url`; `padding = false`; `configuration_id = separate_field` | closed immutable policy |
| `struct:ref.risk.authority` | `lockout = postgres`; `journal = postgres`; `velocity = valkey_ephemeral`; `valkey_unknown = unavailable` | closed immutable policy |
| `struct:ref.risk.precedence` | `order = deny,step_up,throttle,allow`; `unknown = step_up`; `protected_mutation_unknown = deny` | closed immutable policy |
| `struct:ref.delivery.queue` | `workflow = workflow/postgres`; `outbox = outbox/postgres`; `sender = bounded_callback` | closed immutable policy |
| `struct:ref.delivery.backoff` | `strategy = full_jitter`; `attempt_schedule_upper_bounds = 0s,1s,5s,30s,2m`; `attempts = 5` | closed immutable policy; attempt 1 is immediate and each later value is the delay ceiling before that attempt |
| `struct:ref.delivery.dead_letter` | `after_attempt = 5`; `permanent_rejection = immediate`; `delivery_claim = false` | closed immutable policy |
| `struct:ref.lifecycle.legal_hold` | `hard_delete = blocked`; `authority_revocation = required`; `anonymization = maximum_lawful`; `release_authorization = privacy-administrator`; `default_expiry = none` | closed immutable policy |
| `struct:ref.password.recovery_factor_policy` | `eligible = totp,recovery_code,uv_passkey`; `precedence = uv_passkey,totp,recovery_code`; `minimum = one_current_factor`; `no_factor_fallback = purpose_bound_emailed_reset_capability`; `no_verified_email = deny`; `factor_unavailable = deny`; `attempt_authority = postgres` | closed immutable policy |
| `struct:ref.phone.recovery` | `request_when_disabled = deny`; `complete_when_disabled = deny`; `proof = canonical_reset_capability_plus_purpose_bound_phone_otp_plus_eligible_independent_factor`; `risk_authority = identity/risk`; `risk_evidence = immutable_one_use`; `risk_binding = tenant,subject,operation,purpose,canonical_number,preauth_transaction,attempt_id,policy_version`; `risk_ttl = 2m`; `caller_signals = forbidden`; `sim_swap = negative_allow,positive_deny,unknown_deny,unavailable_deny`; `number_recycling = negative_allow,positive_deny,unknown_deny,unavailable_deny`; `carrier = negative_allow,positive_deny,unknown_deny,unavailable_deny`; `carrier_signal = required` | closed immutable risk/proof policy; `phone.recovery.enabled` is the sole enablement authority; enabling requires explicit operator acceptance of SIM-swap, number-recycling and carrier risks and never weakens the OTP or independent-factor requirements; absent required evidence is `unavailable`; stale, mismatched or replayed evidence denies |
| `struct:ref.risk.valkey_recovery` | `journal = postgres`; `maximum_window = configured_maximum_velocity_window`; `batch = reconciliation.batch`; `lease_owners = 1`; `healthy_condition = watermark_reaches_database_utc_minus_maximum_window`; `operator_bypass = false` | closed immutable policy |
| `struct:ref.captcha.evidence` | `attempt_bytes = 32`; `ttl = 2m`; `uses = 1`; `binding = provider,site,tenant,subject_or_anonymous_flow,action,request_fingerprint`; `retry = same_logical_request_only` | closed immutable policy |
| `struct:ref.delivery.provider_retry` | `maximum_attempts = 5`; `unknown = reconcile`; `resubmit = pinned_provider_idempotency_only`; `permanent_rejection = dead_letter`; `queue_acceptance = not_delivered` | closed immutable policy |
| `struct:ref.http.external_retry` | `maximum_attempts = 3`; `maximum_retries = 2`; `retryable = proven_idempotent_before_effect_or_pinned_idempotency`; `ambiguous_after_send = outcome_unknown`; `redirects = 0` | closed immutable policy |
| `struct:ref.postgres.command_retry` | `maximum_attempts = 3`; `retryable = serialization_failure,deadlock_detected`; `jitter = full`; `retry_upper_bounds = 10ms,100ms`; `ambiguous_bookkeeping = reconcile` | closed immutable policy |
| `struct:ref.oauth.rp_transaction` | `state_bytes = 32`; `nonce_bytes = 32`; `ttl = 10m`; `uses = 1`; `binding = issuer,provider,client_id,redirect_uri,response_mode,pkce,nonce,requested_scopes,tenant,operation,preauth_transaction,initiating_subject,popup_opener_origin,popup_channel_id,continuation_ref,remember_policy`; `storage = keyed_digest` | closed immutable policy; nullable bindings are encoded explicitly and cannot be added by a callback |
| `struct:ref.oauth_server.dynamic_registration` | `enabled = false`; `registration_protocol = RFC7591`; `profile = initial_access_token`; `owner = tenant_or_organization_or_platform`; `allowed_scopes = oauth_server.dynamic_registration.allowed_scopes`; `management = unselected`; `initial_access_token = required`; `registration_access_token = not_issued` | sole closed disabled-by-default RFC 7591 policy authority; enabling does not select RFC 7592, which requires a future separately selected management profile |
| `struct:ref.oauth_server.protected_resource` | `enabled = true`; `resource = oauth_server.protected_resource.resource`; `authorization_servers = oauth_server.issuer`; `bearer_methods = authorization_header`; `scopes_supported = oauth_server.protected_resource.supported_scopes`; `metadata_path = /.well-known/oauth-protected-resource` | closed RFC 9728 policy; advertised values and token validation must agree |
| `struct:ref.oauth_server.pairwise_subject` | `enabled = false`; `sector_identifier = registered_sector_host`; `derivation_key = versioned_secret_envelope_key`; `algorithm = HMAC-SHA-256`; `rotation = stable_overlap_migration` | closed OIDC pairwise-subject policy; raw internal identifiers and cross-sector correlation are forbidden |

Persisted or distributed issue, expiry, lease, heartbeat, retry, freshness, and
retention decisions MUST use authoritative PostgreSQL UTC time. A monotonic
process clock MUST be used only for process-local timeout and elapsed-duration
measurement and MUST NOT establish cross-process expiry.

Security, identity, persistence topology, and protocol fields are startup-only.
Rows explicitly marked `reloadable` MAY publish a complete immutable snapshot
only after full validation; failure leaves the prior snapshot active.

## Identity, organization, and administration

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `identity.email.max_bytes` | 254 | 3..254; IDNA result and original input both bounded |
| `identity.unicode_version` | Unicode 15.1.0 | canonicalization changes require collision preflight and migration |
| `identity.phone_metadata` | libphonenumber metadata 9.0.10 | version/checksum required; no silent metadata update |
| `identity.username.min_codepoints` | 3 codepoints | 1..32 |
| `identity.username.max_codepoints` | 32 codepoints | 3..64; MUST be at least minimum |
| `identity.username.scripts` | `Latin`, `Common`, one script per name | fixed reference set; mixed scripts denied; any change requires a new manifest value and collision preflight |
| `identity.username.reserved_set` | v1: `admin`, `administrator`, `api`, `auth`, `login`, `logout`, `me`, `oauth`, `oidc`, `root`, `scim`, `sso`, `support`, `system`, `webhook` | case-folded exact match; startup fails without version/checksum |
| `identity.identifier_reuse` | 30-day tombstone | 1 day..365 days; verified takeover review required to shorten |
| `identity.delete.mode` | anonymize then hard-delete after acknowledgements | hard delete never precedes lifecycle closure |
| `identity.delete.grace` | 24 hours | 0..30 days; zero still requires recent proof and acknowledgements |
| `identity.delete.proof` | closed policy `struct:ref.identity.delete.proof` | passkey-only users use a fresh UV-required passkey assertion; provider-only users use a 15-minute emailed capability; users lacking verified email use fresh UV passkey or audited administrator recovery; every proof is purpose/version bound |
| `identity.ban.expiry_action` | audited automatic restore requiring new login | old authority never reactivates |
| `platform.authority` | closed composition `struct:ref.platform.authority` | all six role/permission-statement operations plus user-role-set are the sole mutation surface; platform roles and subject assignments are durable versioned authorization state; identity metadata and deployment configuration are never authority |
| `platform.bootstrap.enabled` | `false` | explicit Boolean; may be true only for one bounded offline operator invocation and never registers an HTTP/OpenAPI surface |
| `platform.bootstrap.transport` | `offline-command` | fixed enum `offline-command`; HTTP, RPC and startup-listener transports are forbidden |
| `platform.bootstrap.capability_ttl` | 10 minutes | 1..15 minutes; one use, exact tenant/audience/authority-version bound and digest-stored |
| `organization.max_per_user` | 100 | 1..1,000 |
| `organization.max_members` | 100 | 1..100,000 |
| `organization.max_pending_invitations` | 100 | 1..10,000 |
| `organization.invitation_ttl` | 48 hours | 15 minutes..30 days |
| `organization.max_teams` | 100 | 0 disables teams; maximum 10,000 |
| `organization.team_change_batch` | 100 subjects | 1..1,000; larger changes split into versioned cascade batches and rely on organization epoch for immediate denial |
| `organization.max_roles` | 100 | 1..1,000 |
| `organization.active_scope` | closed policy `struct:ref.organization.active_scope` | never user-global; membership/version checked on every switch and use |
| `organization.delete.mode` | archive, then hard-delete after cascade | domain/slug reuse waits 30-day tombstone |
| `impersonation.ttl` | 15 minutes | 1..15 minutes |
| `impersonation.approvals` | one distinct authorized approver for protected targets; otherwise none | self-approval forbidden; approval expires in 15 minutes |

## Authentication and sessions

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `password.min_bytes` | 12 bytes | 8..128; no trim/normalization |
| `password.max_bytes` | 128 bytes | 64..1,024; MUST be at least minimum |
| `password.hibp.outage` | deny mutation with retryable unavailable | every password create/change/reset is denied while unavailable; no reference-profile exception |
| `session.remember_default` | `true` | one shared default for every session-issuing flow; explicit false emits a session cookie with no `Max-Age` and caps server state at 24 hours, while true uses normal 7-day persistent behavior |
| `password.primary_proof_ttl` | 5 minutes | 1..10 minutes; purpose/audience bound, single transition to MFA |
| `password.recovery_factor_policy` | closed policy `struct:ref.password.recovery_factor_policy` | one current eligible factor; unavailable factor authority denies; emailed reset capability is the no-enrolled-factor fallback |
| `password.reset_ttl` | 30 minutes | 5 minutes..1 hour; single-use and password-version bound |
| `email.verification_ttl` | 24 hours | 15 minutes..7 days; single-use and identifier-version bound |
| `email.resend_interval` | 60 seconds | 30 seconds..10 minutes; predecessor remains valid until replacement enqueue commits |
| `session.token_bytes` | 32 | 32..64 random bytes |
| `session.absolute_ttl` | 7 days | 15 minutes..30 days |
| `session.idle_ttl` | 24 hours | 5 minutes..absolute TTL |
| `session.refresh_after` | 12 hours | 1 minute..idle TTL; rotate only after authoritative validation |
| `session.fresh_for` | 24 hours | 15 minutes..24 hours; sensitive operations use 15 minutes |
| `session.max_per_container` | 5 sessions | 1..20; deny the next session; no automatic eviction |
| `session.cookie.max_bytes` | 3,800 | 512..3,800 |
| `session.cookie_cache.ttl` | 5 minutes | 0 disables; maximum 5 minutes |
| `session.valkey.role` | positive cache only; PostgreSQL authority | no negative authorization; loss falls back only after authoritative read |
| `session.stateless.ttl` | 15 minutes | 1..15 minutes; incompatible immediate-revocation features rejected |
| `session.transfer.ttl` | 3 minutes | 30 seconds..3 minutes |
| `session.rotation_overlap` | 0 seconds | fixed at zero for bearer credentials in the reference profile |
| `authentication.preauth_ttl` | 5 minutes | 1..10 minutes; single-use and origin/action/tenant bound |
| `session.last_login_method_retention` | 90 days | 1..365 days; feature remains disabled until notice/consent policy is selected |
| `csrf.token_bytes` | 32 | exactly 32..64; header/body token plus session/origin binding |
| `otp.code_digits` | 6 digits | 6..10 digits |
| `otp.code_alphabet` | decimal `0` through `9` | fixed reference alphabet |
| `otp.digest` | HMAC-SHA-256 | keyed versioned secret; unkeyed digest forbidden |
| `otp.ttl` | 5 minutes | 30 seconds..10 minutes |
| `otp.attempts` | 5 total per challenge across methods | 1..10 |
| `otp.send_limit` | 5 sends/hour | 1..10 sends/hour per tenant + purpose + subject + destination |
| `otp.resend_interval` | 60 seconds | 30 seconds..10 minutes; replacement after committed enqueue |
| `otp.replacement` | atomic successor enqueue plus predecessor challenge supersession | `superseded` belongs to the OTP challenge record, not its delivery effect; one PostgreSQL transaction locks challenge family, inserts successor/encrypted delivery effect, and supersedes predecessor challenge; predecessor remains valid until that commit and is invalid immediately after it; concurrent verify/resend serialize on the family lock; ambiguous commit uses command query and never accepts both |
| `magic_link.ttl` | 15 minutes | 1 minute..1 hour; signup disabled |
| `mfa.challenge_ttl` | 10 minutes | 1..15 minutes |
| `mfa.attempts` | 5 across all methods | 1..10, one PostgreSQL authority |
| `mfa.totp.algorithm` | `SHA-1` | enum `SHA-1`; compatibility profile pinned to RFC 6238 |
| `mfa.totp.digits` | 6 digits | fixed at 6 digits |
| `mfa.totp.period` | 30 seconds | fixed at 30 seconds |
| `mfa.totp.skew_steps` | 1 step before or after current | fixed symmetric one-step window; every accepted step is replay-protected |
| `mfa.enrollment_ttl` | 10 minutes | 1..15 minutes; unconfirmed secret is erased at expiry |
| `mfa.recovery_codes` | 10 codes | 1..20; whole set rotates atomically |
| `mfa.recovery_code_bytes` | 16 bytes | 16..32 random bytes before canonical encoding |
| `phone.change_quarantine` | 24 hours | 0..7 days; zero requires independent-factor approval |
| `phone.recovery.enabled` | `false` | explicit Boolean; request and completion are denied while false; changing to true is startup-only and requires the closed risk policy |
| `phone.recovery.policy` | closed policy `struct:ref.phone.recovery` | canonical capability, purpose-bound phone OTP, eligible independent factor and authoritative fresh one-use `identity/risk` evidence are all required; positive/unknown/unavailable signals and stale/mismatched/replayed/caller-supplied evidence deny; risk step-up cannot replace the eligible independent factor |
| `trusted_device.ttl` | 30 days | 1..90 days |
| `trusted_device.rotate_after` | 24 hours | 1 hour..7 days; concurrent renewal yields one successor and immediately invalidates the predecessor |
| `trusted_device.rotation_overlap` | 0 seconds | fixed at zero in the reference profile; an old credential MUST NOT remain usable after successful rotation |

### Freshness operation matrix

The following operations MUST require a primary authentication proof issued no
more than 15 minutes earlier and bound to the exact tenant, actor, subject,
operation, and current authority versions: password change; factor
enroll/change/remove/reset; email or phone add/change/remove; account anonymize
or delete; API-key create/rotate/revoke; OAuth client-secret create/rotate;
provider link/unlink; organization ownership transfer; impersonation start or
approval; administrator credential reset; session transfer; and session-to-JWT
exchange. Normal session use retains 24-hour freshness metadata and does not
satisfy these operations after 15 minutes. A package MUST NOT add or remove a
freshness exception locally. Forgotten-password reset instead requires the
unexpired 15-minute single-use reset capability, current bound authority
versions, and `password.recovery_factor_policy`; it MUST NOT require the
forgotten secret.

## WebAuthn and passkeys

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `webauthn.profile` | closed policy `struct:ref.webauthn.profile` | server owns WebAuthn RP behavior, not CTAP; exact spec/errata/checksum recorded |
| `webauthn.challenge_ttl` | 5 minutes | 30 seconds..10 minutes |
| `webauthn.challenge_bytes` | 32 | 32..64 |
| `webauthn.algorithms` | ES256, EdDSA; RS256 compatibility enabled | no SHA-1; additions require fixtures |
| `webauthn.attestation` | `none` | direct/enterprise disabled without named MDS/trust profile |
| `webauthn.formats` | `none`, `packed`, `fido-u2f` parser support | only `none` accepted by default; other acceptance requires trust profile |
| `webauthn.extensions` | typed `credProps` and `credProtect`; `appid` and `uvm` disabled | unknown outputs are discarded unless a separately approved typed profile explicitly retains bounded non-authoritative evidence |
| `webauthn.user_handle_bytes` | 32 bytes | 32..64 bytes; unique within RP |
| `webauthn.user_handle_max_bytes` | 64 bytes | fixed protocol ceiling before allocation |
| `webauthn.credential_id_max_bytes` | 1 KiB | 16 bytes..1 KiB before allocation or lookup |
| `webauthn.client_data_json_max_bytes` | 16 KiB | 1 byte..16 KiB before JSON parsing |
| `webauthn.attestation_object_max_bytes` | 64 KiB | 1 byte..64 KiB before CBOR parsing |
| `webauthn.authenticator_data_max_bytes` | 4 KiB | 37 bytes..4 KiB before extension parsing |
| `webauthn.cbor_depth` | 16 levels | 1..16 before allocation |
| `webauthn.cbor_items` | 256 items | 1..256 before allocation |
| `passkey.tenant_rp_scope` | one RP ID maps to one tenant | shared-RP multi-tenant usernameless flow is rejected |
| `passkey.user_verification` | required for signup/signin/step-up | preferred only for non-authoritative registration compatibility |

## OAuth, OIDC, SSO, and API keys

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `oauth.rp.state_bytes` | 32 bytes | 32..64 bytes; server-side opaque record |
| `oauth.rp.state_ttl` | 10 minutes | 1..15 minutes; single-use |
| `oauth.rp.nonce_bytes` | 32 bytes | 32..64 bytes |
| `oauth.rp.nonce_ttl` | 10 minutes | 1..15 minutes; single-use |
| `oauth.rp.transaction` | closed policy `struct:ref.oauth.rp_transaction` | every callback validates the complete binding before code exchange |
| `oauth.clock_skew` | 60 seconds | 0..5 minutes |
| `oauth.provider_timeout` | 10 seconds | 1..30 seconds; no blind ambiguous retry |
| `oauth_server.access_format` | signed JWT ES256 | opaque profile also tested; selected runtime default is JWT |
| `oauth_server.access_ttl` | 15 minutes | 1..60 minutes |
| `oauth_server.code_ttl` | 5 minutes | 30 seconds..10 minutes |
| `oauth_server.refresh_ttl` | 30 days | 1..90 days; rotate every use |
| `oauth_server.client_secret_bytes` | 32 | 32..64; reveal once, digest at rest |
| `oauth_server.client_secret_overlap` | 0 | 0..24 hours; explicit audited override |
| `oauth_server.client_auth` | public `none` plus PKCE; confidential `client_secret_basic` | `client_secret_post` disabled; private-key JWT requires a separately evidenced profile |
| `oauth_server.session_jwt_exchange` | enabled | 5-minute JWT, configured audience allowlist, never beyond source session |
| `oauth_server.session_jwt_ttl` | 5 minutes | 1..5 minutes; never beyond source-session expiry |
| `oauth_server.audiences` | `oauth_server.protected_resource.resource`, `oauth_server.issuer` | exact non-empty access-token audience set; the resource audience is byte-for-byte equal to RFC 9728 `resource`; wildcards and relative values forbidden |
| `oauth_server.scopes` | `email,identity:read,identity:write,offline_access,openid,profile` | canonical sorted unique authorization-server catalog; discovery, registration, consent, grants and tokens MUST use only this closed set |
| `oauth_server.protected_resource` | closed policy `struct:ref.oauth_server.protected_resource` | RFC 9728 metadata is enabled and MUST match resource/audience validation |
| `oauth_server.protected_resource.resource` | REQUIRED URL | canonical absolute HTTPS origin; no path, query, fragment, userinfo, wildcard, or loopback; MUST equal the origin of `http.external_base_url`; RFC 9728 metadata and access-token audience validation use this exact byte string |
| `oauth_server.protected_resource.supported_scopes` | `identity:read,identity:write` | exact sorted unique subset of `oauth_server.scopes`; RFC 9728 `scopes_supported` and access-token enforcement MUST match |
| `oauth_server.dynamic_registration` | closed policy `struct:ref.oauth_server.dynamic_registration` | enables RFC 7591 registration only; disabled unless all owner, token, and metadata fields validate; RFC 7592 management remains unselected |
| `oauth_server.dynamic_registration.allowed_scopes` | `email,identity:read,identity:write,offline_access,openid,profile` | closed registration subset of `oauth_server.scopes`; unknown, duplicate, and out-of-catalog requested scopes are rejected |
| `oauth_server.dynamic_registration.allowed_grant_types` | authorization_code, refresh_token | closed non-empty subset of implemented grants; client_credentials requires explicit owner policy |
| `oauth_server.dynamic_registration.allowed_auth_methods` | `none`, `client_secret_basic` | closed subset of implemented methods; public/confidential metadata must be coherent |
| `oauth_server.dynamic_registration.metadata_bytes` | 32 KiB | 1 KiB..64 KiB before JSON allocation; duplicate keys rejected |
| `oauth_server.dynamic_registration.initial_access_token_ttl` | 15 minutes | 1..60 minutes; single-use and owner/audience bound |
| `oauth_server.signing_rotation` | 30 days | 1..90 days; old public key retained token TTL + 60 seconds |
| `oauth_server.oidc.response_mode` | `query` | enum `query`; form_post, fragment, and JWT-secured modes unsupported |
| `oauth_server.oidc.rp_initiated_logout` | `true` | exact registered post-logout redirect and validated ID-token hint/session binding required |
| `oauth_server.oidc.frontchannel_logout` | `false` | fixed false in reference profile |
| `oauth_server.oidc.backchannel_logout` | `false` | fixed false in reference profile |
| `oauth_server.oidc.id_token_signing_alg` | `ES256` | enum `ES256`; metadata and protected header MUST agree |
| `oauth_server.oidc.pairwise_subject` | closed policy `struct:ref.oauth_server.pairwise_subject` | disabled by default; enabling requires sector registration and a versioned secret derivation key |
| `oauth.rp.id_token_algs` | `ES256`, `RS256` | fixed allowlist; symmetric algorithms and `none` forbidden |
| `oauth.pkce.generated_verifier_bytes` | 64 bytes | 32..96 random bytes; unpadded base64url result MUST be 43..128 unreserved ASCII characters |
| `oauth.json_depth` | 16 levels | 1..16 for discovery, JWKS, token, ID-token payload, and UserInfo JSON |
| `oauth.json_keys` | 256 keys | 1..256 per object and 1,000 total elements |
| `oauth.json_string_bytes` | 16 KiB | 1 byte..16 KiB per string; complete response also obeys provider-response bound |
| `oauth.discovery_cache_ttl` | 1 hour | 5 minutes..24 hours; stale metadata is never used past issuer/key validity |
| `oauth.jwks_unknown_kid_refreshes` | 1 refresh | fixed at one single-flight refresh per validation; no fallback key selection |
| `oauth_server.device_ttl` | 10 minutes | 1..15 minutes |
| `oauth_server.device_poll` | 5 seconds | 5..60 seconds |
| `oauth_server.device_slow_down_increment` | 5 seconds | fixed at exactly 5 seconds per RFC 8628 `slow_down`; cumulative poll interval capped at 60 seconds |
| `oauth_server.device_code_bytes` | 32 bytes | fixed 256-bit random value before base64url encoding; reveal once and store only a domain-separated keyed digest |
| `oauth_server.device_code_encoded_length` | 43 characters | fixed unpadded base64url length for 32 bytes; non-canonical encodings rejected |
| `oauth_server.device_code_digest_key` | REQUIRED when device flow enabled | versioned secret-envelope key handle; raw key and device code forbidden from logs/config reports |
| `oauth_server.device_user_code_length` | 8 characters | 8..12 characters; display grouping 4-4 |
| `oauth_server.device_user_code_alphabet` | `BCDFGHJKLMNPQRSTVWXZ23456789` | fixed reference alphabet |
| `oauth_server.device_lookup_limit` | 32 failures/hour per trusted IP | 1..100; collision regenerates before reveal |
| `sso.domain_proof.method` | DNS TXT | enum DNS TXT; HTTP proof unsupported in reference profile |
| `sso.domain_proof.token_bytes` | 32 bytes | 32..64 bytes |
| `sso.domain_proof.ttl` | 24 hours | 15 minutes..48 hours |
| `sso.jit_repeat_sync` | disabled | enabling requires versioned source-of-truth mapping and rollback policy |
| `saml.clock_skew` | 2 minutes | 0..5 minutes |
| `saml.replay_ttl` | assertion expiry plus 5 minutes, minimum 10 minutes | 10 minutes..24 hours |
| `saml.signatures` | both response and assertion required | unsigned material is rejected in the reference profile |
| `saml.xml_bytes` | 1 MiB | 16 KiB..2 MiB |
| `saml.decoded_bytes` | 2 MiB | 16 KiB..4 MiB |
| `saml.nodes` | 10,000 nodes | 1..10,000; increase forbidden without new security proof |
| `saml.depth` | 64 levels | 1..64; increase forbidden without new security proof |
| `saml.signatures_max` | 4 signatures | 1..4; increase forbidden without new security proof |
| `saml.attributes_per_element` | 128 attributes | 1..128 before allocation or schema interpretation |
| `saml.assertions_per_response` | 1 assertion | fixed at one consumed assertion; duplicates or additional assertions are rejected |
| `saml.certificates_per_metadata_entity` | 5 certificates | 1..5 active or rollover certificates |
| `saml.text_bytes` | 1 MiB | 1 KiB..1 MiB aggregate XML text before concatenation |
| `saml.namespaces` | 256 declarations | 1..256 total |
| `saml.attributes_total` | 2,048 attributes | 1..2,048 total and subject to per-element bound |
| `saml.id_uri_bytes` | 2 KiB | 1 byte..2 KiB for any ID, URI, issuer, audience, destination, or recipient value |
| `saml.attribute_value_bytes` | 16 KiB | 0..16 KiB per mapped value; mapping output remains separately bounded |
| `saml.base64_bytes` | 4 MiB | 1 byte..4 MiB before certificate or signature decoding |
| `saml.certificate_bytes` | 64 KiB | 1 byte..64 KiB DER per certificate |
| `saml.redirect_deflate_bytes` | 64 KiB | 1 byte..64 KiB compressed input; decoded XML bound still applies |
| `saml.redirect_signature_algorithm` | `http://www.w3.org/2007/05/xmldsig-more#sha256-rsa-MGF1` | closed outbound HTTP-Redirect `SigAlg`; RSA key MUST be at least 2048 bits; missing, duplicated, unsupported, or mismatched values are rejected |
| `saml.inflate_ratio` | 20 | 1..20 decoded-to-compressed ratio before further allocation |
| `saml.idp_initiated` | `false` | explicit Boolean; `true` requires an unsolicited-response allowlist, current organization domain proof, fixed destination, short acceptance window, replay policy, and login-CSRF/explicit-confirmation profile |
| `webauthn.development_localhost_http` | `false` | production fixed false; development may enable only exact `http://localhost` with an explicit port and MUST reject IP literals, subdomains and non-localhost HTTP origins |
| `webauthn.public_suffix_snapshot` | `public-suffix-list-e1b8015c` | commit `e1b8015c3b2f0f4f8c18659c2480fc1a22c07b20` and SHA-256 `fe6adc7fb8014f57d28d69b18d0aa3e581efb432544922e12131a5d4a87bd954` MUST equal `PROTOCOL_CONFORMANCE_MANIFEST.json` |
| `api_key.encoding` | closed policy `struct:ref.api_key.encoding` | prefix, alphabet, padding, secret length, and configuration-ID separation are fixed |
| `api_key.digest` | HMAC-SHA-256, versioned 32-byte pepper | pepper is secret-envelope/key-provider owned; rotation supports old verify/new issue |
| `api_key.expiry` | 90 days | 1 day..365 days; unlimited administrator-only |
| `api_key.quota.capacity` | 1,000 tokens | 1..1,000,000; bucket scoped by tenant + configuration + key ID |
| `api_key.quota.refill` | 1,000 tokens/hour continuously | PostgreSQL UTC computes fractional accrual; cap at capacity |
| `api_key.quota.debit` | one token per authenticated request | PostgreSQL atomic lock/update; ambiguous commit follows command-result query protocol |
| `api_key.rotation_overlap` | 0 | 0..24 hours explicit override |
| `api_key.valkey_cache_ttl` | 60 seconds | 0 disables; maximum 60 seconds; positive metadata only and never authority |

## SCIM, risk, delivery, and HTTP

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `scim.page_default` | 100 resources | 1..1,000; MUST NOT exceed page maximum |
| `scim.page_max` | 1,000 resources | 1..1,000 |
| `scim.bulk.enabled` | true | required reference capability |
| `scim.bulk.operations` | 1,000 operations | 1..1,000 |
| `scim.bulk.bytes` | 1 MiB | 16 KiB..2 MiB |
| `scim.bulk.fail_on_errors` | 100 failed operations | 1..100 and MUST NOT exceed `scim.bulk.operations` |
| `scim.patch.operations` | 100 operations | 1..100 per request |
| `scim.filter.depth` | 16 nested expressions | 1..16 before evaluation |
| `scim.filter.nodes` | 256 parsed nodes | 1..256 before allocation or evaluation |
| `scim.if_match` | required for replace, PATCH, and delete | missing is precondition-required; stale is precondition-failed |
| `scim.token_bytes` | 32 bytes | 32..64 bytes |
| `scim.token_ttl` | 90 days | 1..365 days |
| `scim.token_overlap` | 0 | 0..1 hour explicit override |
| `scim.external_id_scope` | tenant + organization + provider connection + resource type | never tenant-wide by itself |
| `scim.filter_bytes` | 8 KiB | 1 byte..8 KiB before tokenization |
| `scim.filter_tokens` | 512 tokens | 1..512; reject before allocating token 513 |
| `scim.path_bytes` | 1 KiB | 1 byte..1 KiB per attribute or PATCH path |
| `scim.resource_depth` | 16 levels | 1..16 before allocation |
| `scim.resource_attributes` | 1,000 attributes | 1..1,000 total parsed attributes |
| `scim.string_bytes` | 64 KiB | 0..64 KiB per string value |
| `scim.group_members` | 10,000 members | 0..10,000 per group; larger changes use bounded pages |
| `scim.error_detail_bytes` | 512 bytes | 0..512 enumeration-safe UTF-8 bytes |
| `scim.bulk.operation_bytes` | 256 KiB | 1 KiB..1 MiB and MUST NOT exceed bulk total bytes |
| `scim.bulk.response_bytes` | 2 MiB | 16 KiB..4 MiB; response construction is streamed |
| `scim.idempotency_retention` | 30 days | 24 hours..90 days after terminal child/parent result; unknown mappings have no time-based release |
| `scim.delete_tombstone_retention` | 30 days | 24 hours..90 days; same key/fingerprint replays original DELETE result while a different key sees normal precondition/not-found behavior |
| `risk.authority` | closed policy `struct:ref.risk.authority` | PostgreSQL is authority; Valkey is ephemeral only |
| `risk.precedence` | closed policy `struct:ref.risk.precedence` | all signals recorded; no allow short-circuit over unknown/stronger result |
| `risk.valkey.boot_epoch` | required random 128-bit epoch plus PostgreSQL-recorded health marker | restart, flush, missing marker, or epoch mismatch is unavailable, never zero counters |
| `risk.valkey.recovery` | closed policy `struct:ref.risk.valkey_recovery` | replay the PostgreSQL journal before atomically publishing a healthy epoch; state remains unavailable until the exact watermark condition holds |
| `captcha.ambiguity` | deny signup/signin/reset/credential change; step-up for low-risk read flows only when an independent configured factor exists, otherwise deny | adapters return evidence only; unavailable/unknown never becomes allow |
| `captcha.timeout` | 5 seconds | 1..15 seconds |
| `captcha.score_owner` | risk engine | enum `risk engine`; adapters return evidence and MUST NOT decide |
| `captcha.score_threshold` | 0.5 | 0.0..1.0; protected actions deny below threshold when selected provider supplies a score |
| `captcha.evidence` | closed policy `struct:ref.captcha.evidence` | adapters return evidence only; the risk engine owns consumption and decision |
| `hibp.cache.enabled` | `true` | explicit Boolean; memory only |
| `hibp.cache.entries` | 10,000 prefixes | 100..100,000 |
| `hibp.cache.ttl` | 24 hours | 1 hour..7 days |
| `hibp.cache.eviction` | `LRU` | enum `LRU`; failure is unavailable, never clean |
| `hibp.cache.singleflight` | `true` | fixed true per prefix |
| `hibp.add_padding` | `true` | fixed true; zero-count suffixes are padding/no-match only |
| `hibp.response_bytes` | 1 MiB | 1 KiB..2 MiB before parsing |
| `hibp.max_breach_count` | 1,000,000,000 matches | 1..1,000,000,000; a larger public count is malformed/unavailable evidence and MUST NOT wrap, saturate, truncate, or become no-match |
| `hibp.outbound_concurrency` | 8 requests | 1..64 concurrent outbound range requests per process; same-prefix requests still coalesce and queued callers obey their own cancellation/deadline |
| `hibp.timeout` | 10 seconds | 1..30 seconds total request deadline |
| `delivery.queue` | closed policy `struct:ref.delivery.queue` | PostgreSQL durable authority; application supplies only bounded Sender callbacks |
| `delivery.attempts` | 5 attempts | 1..10 |
| `delivery.sender_timeout` | 10 seconds | 1..30 seconds per provider callback; cancellation REQUIRED |
| `delivery.sender_concurrency` | 8 per sender instance | 1..64; independent from worker claim concurrency |
| `delivery.backoff` | closed policy `struct:ref.delivery.backoff` | exact five-entry duration list matching attempts |
| `delivery.dead_letter` | closed policy `struct:ref.delivery.dead_letter` | queue acceptance never means delivered |
| `delivery.provider_retry` | closed policy `struct:ref.delivery.provider_retry` | timeout, disconnect, or ambiguous response is outcome-unknown and is never treated as rejection |
| `delivery.dedupe_ttl` | workflow token lifetime plus 24 hours | minimum 1 hour; maximum 31 days |
| `i18n.locales` | `en`, `fi` | non-empty unique BCP 47 tags; both catalogs REQUIRED |
| `i18n.default` | `en` | MUST be in locales; fallback remains `en` |
| `http.body_bytes` | 1 MiB | 1 KiB..1 MiB; endpoint MUST NOT raise without reviewed manifest row |
| `http.header_bytes` | 64 KiB | 1 KiB..64 KiB |
| `http.url_bytes` | 8 KiB | 1 KiB..8 KiB |
| `http.json_depth` | 32 levels | 1..32 |
| `http.json_elements` | 1,000 elements | 1..1,000 |
| `http.read_header_timeout` | 5 seconds | 1..30 seconds |
| `http.read_timeout` | 15 seconds | 1..60 seconds |
| `http.write_timeout` | 30 seconds | 1..60 seconds |
| `http.idle_timeout` | 60 seconds | 5..120 seconds |
| `http.shutdown_drain` | 30 seconds | 1..60 seconds |
| `http.rate.default` | 100/minute | 1..10,000/minute |
| `http.rate.signin` | 10/minute | 1..100/minute per trusted IP + scoped subject |
| `http.rate.signup` | 5/hour | 1..100/hour per trusted IP + tenant |
| `http.rate.reset_link_send` | 5/hour | 1..100/hour per trusted IP + scoped subject + destination |
| `http.rate.otp_send` | 5/hour | 1..100/hour per trusted IP + scoped subject + destination |
| `http.rate.otp_verify` | 10/10 minutes | 1..100/10 minutes per trusted IP + challenge |
| `http.rate.mfa_verify` | 10/10 minutes | 1..100/10 minutes per trusted IP + challenge |
| `http.rate.admin` | 30/minute | 1..300/minute per actor + trusted IP; stricter package limits win |
| `http.rate.algorithm` | PostgreSQL atomic token-bucket counters | each period is continuously refilled by database time with the declared capacity as maximum burst; a request atomically debits every applicable bucket or none; checked fixed-point remainder prevents upward rounding; unknown commit is queried by stable debit command ID |
| `http.rate.authority` | PostgreSQL | Valkey MAY cache positive metadata only; PostgreSQL unavailable fails closed for sensitive/admin operations and returns unavailable for default operations |
| `http.client_ip.normalization` | IPv4 mapped addresses normalize to IPv4; IPv6 uses canonical RFC 5952 address | no subnet aggregation; invalid/non-unicast client address is unavailable and sensitive operations fail closed |
| `http.provider_response_bytes` | 1 MiB | 16 KiB..2 MiB; SAML uses its own bound |

## PostgreSQL, Valkey, workers, and operations

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `postgres.pool_open` | 20 connections per application instance | 1..200 |
| `postgres.pool_idle` | 10 connections per application instance | 0..pool_open |
| `postgres.connect_timeout` | 5 seconds | 1..30 seconds |
| `postgres.statement_timeout` | 10 seconds | 100ms..60s; migrations have explicit per-step timeout |
| `postgres.lock_timeout` | 2 seconds | 100ms..10s |
| `postgres.tx_timeout` | 15 seconds | 1..60 seconds |
| `postgres.command_retries` | 2 retries | 0..2, producing at most 3 total attempts; only classified deadlock/serialization |
| `postgres.command_retry` | closed policy `struct:ref.postgres.command_retry` | same command ID and reservation generation; stop on cancellation, lease loss, ambiguous bookkeeping, or externally visible effect |
| `valkey.operation_timeout` | 2 seconds | 100ms..10s |
| `valkey.command_retries` | 0 retries | 0..2; reference profile fails the operation to its owning fallback/reconciliation policy without hidden client retries |
| `valkey.pool_size` | 20 per instance | 1..200 |
| `valkey.eviction` | `noeviction` | readiness fails otherwise for selected ephemeral distributed counters; Valkey is never authority |
| `worker.concurrency` | 8 per worker kind | 1..64 |
| `worker.claim_ttl` | 60 seconds | 10 seconds..10 minutes |
| `worker.heartbeat` | 20 seconds | 1 second..less than half claim TTL |
| `cleanup.batch` | 500 records | 1..10,000 |
| `cleanup.interval` | 5 minutes | 1 minute..24 hours |
| `reconciliation.batch` | 100 records | 1..1,000 |
| `reconciliation.interval` | 1 minute | 10 seconds..1 hour |
| `migration.startup_mode` | check-only | `plan` and `apply` are explicit operator actions; auto-apply disabled |
| `migration.lock_timeout` | 30 seconds | 1s..5m; one PostgreSQL advisory-lock owner |
| `migration.drift` | fail readiness | dirty, missing, reordered, or checksum mismatch never auto-repaired |
| `audit.sink` | `audit/postgres`, synchronous record/outbox enlistment | process memory and telemetry export are not authoritative audit storage |
| `audit.retention.policy` | closed authority `struct:ref.audit.retention_policy` | `audit/postgres` is authoritative after insert-once initialization; restart and configuration reload never overwrite its durable version |
| `audit.retention.standard` | 365 days | 90 days..7 years; startup bootstrap default only, used to initialize version 1 when durable policy is absent; ordinary authentication and lifecycle audit records |
| `audit.retention.protected` | 400 days | 365 days..7 years; startup bootstrap default only, used to initialize version 1 when durable policy is absent; privileged, federation, provisioning, key-management, and integrity records |
| `health.path` | `/healthz` | absolute path; fixed reference path |
| `health.success_status` | 200 | exact 200; process-only; no dependency or secret detail |
| `readiness.path` | `/readyz` | absolute path; fixed reference path |
| `readiness.success_status` | 200 | exact success status |
| `readiness.failure_status` | 503 | exact failure status; 2-second probe deadline; no cache |
| `openapi.path` | `/openapi.json` | OpenAPI 3.1.1, immutable ETag, no credentials |
| `telemetry.metric_label_limit` | 32 values per controlled dimension | 1..32 values per controlled dimension; no subject, tenant, token, provider body, URL, email, or raw error labels |
| `telemetry.flush_timeout` | 5 seconds | 0 disables flush; maximum 30 seconds |

## Feature selection and external HTTP authority

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `runtime.mode` | `production` | enum `production`, `test`; test mode MUST use explicit capture providers and MUST NOT weaken protocol validation |
| `valkey.enabled` | `true` | explicit Boolean; PostgreSQL remains authority |
| `valkey.authentication.enabled` | `true` | explicit Boolean; credential handle REQUIRED when true |
| `captcha.enabled` | `false` | explicit Boolean; when true, provider MUST be non-`none` |
| `captcha.catalog_version` | `captcha-catalog-v1` | fixed version/checksum required; expansion IDs use this value |
| `captcha.provider` | `none` | enum `none`, `recaptcha`, `turnstile`, `hcaptcha`, `captchafox` |
| `captcha.<id>.site_key` | REQUIRED when that CAPTCHA provider is selected | string, 1..512 bytes; public browser configuration, but redacted from unrelated diagnostics |
| `hibp.enabled` | `true` | explicit Boolean; disabling requires a non-reference profile and MUST NOT be inferred from missing endpoint |
| `delivery.enabled` | `true` | explicit Boolean; production sender then REQUIRED |
| `sso.enabled` | `false` | explicit Boolean; selected enterprise connections require their protocol fields |
| `saml.enabled` | `false` | explicit Boolean; may be true only when SSO enabled and every required SAML field is present |
| `http.external_base_url` | REQUIRED | one absolute HTTPS URL with origin and optional path prefix; query, fragment, userinfo, wildcard, and loopback forbidden in production |
| `http.trusted_proxy_cidrs` | empty list | exact CIDR list; empty means no forwarded headers trusted; reloadable |
| `http.forwarded_precedence` | RFC 7239 `Forwarded` only | when socket peer is untrusted, ignore all forwarding headers and use peer IP; when trusted, ignore `X-Forwarded-For` and process `Forwarded` right-to-left through trusted hops; malformed, duplicate-conflicting, obfuscated, or exhausted chains are unavailable and sensitive operations fail closed |
| `http.forwarded_max_hops` | 8 hops | 1..16 RFC 7239 elements; reject before processing element 17 |
| `http.trusted_origins` | exact origin of `http.external_base_url` | 1..32 absolute HTTPS origins; no wildcard, path, query, fragment, or userinfo; reloadable |
| `http.cors.enabled` | `false` | same-origin reference default; enabling requires a non-empty exact trusted-origin set |
| `http.cors.origins` | exact `http.trusted_origins` set | 1..32 origins; wildcard forbidden with credentials; reloadable |
| `http.cors.methods` | `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS` | fixed unique uppercase allowlist |
| `http.cors.headers` | `Authorization`, `Content-Type`, `Idempotency-Key`, `If-Match`, `X-CSRF-Token` | fixed case-insensitive allowlist |
| `http.cors.expose_headers` | `ETag`, `Location`, `Retry-After` | fixed case-insensitive allowlist |
| `http.cors.credentials` | `true` | requires exact non-wildcard origins |
| `http.cors.max_age` | 10 minutes | 0..24 hours |
| `http.external_total_timeout` | 10 seconds | 1..30 seconds |
| `http.external_connect_timeout` | 3 seconds | 100 milliseconds..10 seconds; MUST NOT exceed total timeout |
| `http.external_tls_handshake_timeout` | 5 seconds | 100 milliseconds..10 seconds; MUST NOT exceed total timeout |
| `http.external_retry` | closed policy `struct:ref.http.external_retry` | a timeout/disconnect after a possibly transmitted mutation is not retryable without a pinned idempotency contract |
| `http.idempotency_key_bytes` | 32 bytes | exactly 32 random bytes encoded as 43-character unpadded base64url in `Idempotency-Key` |
| `http.idempotency_retention` | 24 hours | 1 hour..7 days after terminal result; unresolved/unknown mapping has no time-based release |
| `tenant.routing_mode` | `host` | enum `host`; header/path routing is unsupported in reference profile |
| `tenant.host_suffix` | REQUIRED string | lower-case IDNA ASCII DNS suffix; 1..253 bytes; exact-label suffix match only |

## Cookie, issuer, relying-party, and provider settings

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `session.cookie.name` | `__Host-identity_session` | 1..128 ASCII cookie-token bytes; `__Host-` invariant enforced |
| `session.cookie.path` | `/` | fixed for `__Host-`; no narrowing |
| `session.cookie.domain` | empty | MUST remain empty for `__Host-` |
| `session.cookie.secure` | `true` | fixed true |
| `session.cookie.http_only` | `true` | fixed true |
| `session.cookie.same_site` | `Lax` | enum `Lax`, `Strict`; `None` unsupported in reference profile |
| `session.cookie.partitioned` | `false` | fixed false; enabling requires a separate compatibility/security profile |
| `session.cookie.priority` | `High` | enum `High` |
| `session.cookie.max_age` | closed policy `struct:ref.session.cookie.max_age` | browser-session omission and persistent remaining-lifetime duration are explicit variants; no inferred scalar/default is permitted |
| `http.cookies.max_count` | 32 cookies per request | 1..32; duplicate session-cookie names are rejected before selection |
| `http.cookies.max_header_bytes` | 16 KiB aggregate Cookie headers | 1 KiB..16 KiB before parsing or allocation |
| `oauth_server.issuer` | REQUIRED | absolute HTTPS URL, no query/fragment/userinfo; exact issuer comparison |
| `oauth_server.issuer_path_policy` | `origin-only` | issuer path MUST be empty or `/`; path-bearing issuers are rejected so every configured well-known route is unambiguous |
| `oauth_server.authorization_endpoint` | `/oauth2/authorize` | absolute path beneath issuer; fixed reference route |
| `oauth_server.token_endpoint` | `/oauth2/token` | absolute path beneath issuer; fixed reference route |
| `oauth_server.jwks_endpoint` | `/.well-known/jwks.json` | absolute path beneath issuer; fixed reference route |
| `oauth_server.redirect_uris` | REQUIRED registered exact URI set | 1..100 per client; HTTPS except loopback native clients; no fragment/wildcard |
| `webauthn.rp_id` | REQUIRED string | valid DNS domain suffix of external host; no scheme, port, wildcard, or public suffix |
| `webauthn.rp_name` | REQUIRED string | 1..64 Unicode codepoints; display only, never authority |
| `webauthn.origins` | exact `http.trusted_origins` set | 1..32 exact HTTPS origins; no wildcard |
| `oauth.rp.redirect_uris` | REQUIRED when any social provider is enabled | exact per-provider HTTPS callback URIs beneath external base URL; 1..32 |
| `oauth.rp.audiences` | exact configured provider client IDs | non-empty per enabled provider; exact comparison |
| `providers.catalog_version` | `provider-catalog-v1` | fixed version/checksum required; expansion IDs use this value |
| `providers.<id>.enabled` | `false` | explicit Boolean per catalog provider |
| `providers.<id>.protocol` | `oidc` | enum `oidc`, `oauth2`; catalog entry pins the value |
| `providers.<id>.discovery_enabled` | `true` for OIDC, `false` for OAuth 2.0 | explicit Boolean; OAuth 2.0 MUST NOT infer OIDC discovery |
| `providers.<id>.issuer` | REQUIRED URL when enabled and protocol is OIDC | absolute HTTPS issuer; exact comparison; discovery remains within pinned issuer |
| `providers.<id>.authorization_endpoint` | REQUIRED when enabled and either protocol is OAuth 2.0 or discovery is disabled | absolute HTTPS URL; host allowlisted |
| `providers.<id>.token_endpoint` | REQUIRED when enabled and either protocol is OAuth 2.0 or discovery is disabled | absolute HTTPS URL; host allowlisted |
| `providers.<id>.jwks_uri` | REQUIRED when enabled, protocol is OIDC, and discovery is disabled | absolute HTTPS URL; host allowlisted |
| `providers.<id>.profile_endpoint` | REQUIRED when enabled and protocol is OAuth 2.0 | absolute HTTPS URL; host allowlisted; bounded JSON response |
| `providers.<id>.subject_pointer` | REQUIRED string when enabled and protocol is OAuth 2.0 | RFC 6901 JSON Pointer to immutable provider subject; mutable email/name forbidden |
| `providers.<id>.client_id` | REQUIRED string when provider enabled | 1..512 bytes; identifier but redacted from public diagnostics |
| `providers.<id>.confidential` | `true` | explicit Boolean; when true client secret REQUIRED |
| `captcha.recaptcha.endpoint` | `https://www.google.com/recaptcha/api/siteverify` | fixed HTTPS URL in the reference profile |
| `captcha.recaptcha.api` | `classic_siteverify` | enum `classic_siteverify`; Enterprise Assessment is unsupported |
| `captcha.recaptcha.version` | `v3` | enum `v3`; reference deployment selects score-bearing v3 rather than v2 checkbox/invisible |
| `captcha.recaptcha.tier` | `standard` | enum `standard`; Enterprise is unsupported |
| `captcha.recaptcha.allowed_hostnames` | REQUIRED list when reCAPTCHA is selected | list of 1..32 canonical IDNA ASCII DNS hostnames; no wildcard, port, path, or suffix matching |
| `captcha.recaptcha.allowed_origins` | empty list | list of origins is fixed empty because classic Siteverify returns hostname, not origin |
| `captcha.recaptcha.expected_action` | REQUIRED string when reCAPTCHA v3 is selected | string, 1..64 visible ASCII bytes; exact case-sensitive comparison |
| `captcha.recaptcha.remote_ip_disclosure` | `false` | explicit Boolean; `remoteip` MUST be omitted |
| `captcha.turnstile.endpoint` | `https://challenges.cloudflare.com/turnstile/v0/siteverify` | fixed HTTPS URL |
| `captcha.turnstile.api` | `siteverify` | enum `siteverify` |
| `captcha.turnstile.version` | `v0` | enum `v0` |
| `captcha.turnstile.tier` | `free` | enum `free`; Enterprise-only behavior is unsupported |
| `captcha.turnstile.allowed_hostnames` | REQUIRED list when Turnstile is selected | list of 1..32 canonical IDNA ASCII DNS hostnames; no wildcard, port, path, or suffix matching |
| `captcha.turnstile.allowed_origins` | empty list | list of origins is fixed empty because Siteverify returns hostname, not origin |
| `captcha.turnstile.expected_action` | REQUIRED string when Turnstile is selected | string, 1..32 alphanumeric, underscore, or hyphen bytes; exact comparison |
| `captcha.turnstile.remote_ip_disclosure` | `false` | explicit Boolean; `remoteip` MUST be omitted |
| `captcha.hcaptcha.endpoint` | `https://api.hcaptcha.com/siteverify` | fixed HTTPS URL |
| `captcha.hcaptcha.api` | `siteverify` | enum `siteverify` |
| `captcha.hcaptcha.version` | `2026-08-11` | pinned documented contract revision; changing it requires new interoperability evidence |
| `captcha.hcaptcha.tier` | `publisher_pro` | enum `publisher_pro`; Enterprise score/action fields are unsupported |
| `captcha.hcaptcha.allowed_hostnames` | REQUIRED list when hCaptcha is selected | list of 1..32 canonical IDNA ASCII DNS hostnames; no wildcard, port, path, or suffix matching |
| `captcha.hcaptcha.allowed_origins` | empty list | list of origins is fixed empty because Siteverify returns hostname, not origin |
| `captcha.hcaptcha.expected_action` | `unsupported` | enum `unsupported`; action binding MUST be rejected for the selected tier |
| `captcha.hcaptcha.remote_ip_disclosure` | `false` | explicit Boolean; `remoteip` MUST be omitted |
| `captcha.captchafox.endpoint` | `https://api.captchafox.com/siteverify` | fixed HTTPS URL |
| `captcha.captchafox.api` | `siteverify` | enum `siteverify` |
| `captcha.captchafox.version` | `2026-08-11` | pinned documented contract revision; changing it requires new interoperability evidence |
| `captcha.captchafox.tier` | `free` | enum `free`; Enterprise insights are unsupported |
| `captcha.captchafox.allowed_hostnames` | REQUIRED list when CaptchaFox is selected | list of 1..32 canonical IDNA ASCII DNS hostnames; no wildcard, port, path, or suffix matching |
| `captcha.captchafox.allowed_origins` | empty list | list of origins is fixed empty because Siteverify returns hostname, not origin |
| `captcha.captchafox.expected_action` | `unsupported` | enum `unsupported`; generic action binding MUST be rejected |
| `captcha.captchafox.remote_ip_disclosure` | `false` | explicit Boolean; `remoteIp` MUST be omitted |
| `hibp.endpoint` | `https://api.pwnedpasswords.com/range/` | fixed HTTPS prefix; query uses only five-character SHA-1 prefix |
| `hibp.user_agent` | REQUIRED string | exact visible-ASCII format `golib-identity-reference/<module-version> (+<security-contact-URL>)`; 1..256 bytes; no secret or user data |

## Enterprise SAML connection fields

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `saml.sp_entity_id` | REQUIRED when `saml.enabled=true` | absolute HTTPS URI, 1..1,024 bytes; exact comparison |
| `saml.sp_acs_url` | REQUIRED per enabled provider when `saml.enabled=true` | exact absolute HTTPS URL produced by substituting that provider's canonical ID into `/saml2/{provider_id}/acs` beneath external base URL; HTTP-POST only; metadata Location, Response Destination and SubjectConfirmation Recipient MUST match byte-for-byte |
| `saml.sp_idp_initiated_url` | REQUIRED per enabled provider when `saml.idp_initiated=true` | distinct exact absolute HTTPS URL produced by substituting that provider's canonical ID into `/saml2/{provider_id}/idp-init`; HTTP-POST only; metadata Location, Response Destination and SubjectConfirmation Recipient MUST match byte-for-byte |
| `saml.sp_slo_url` | REQUIRED when SLO enabled | exact absolute HTTPS URL beneath external base URL; HTTP-POST only for inbound LogoutRequest/LogoutResponse; metadata MUST NOT advertise Redirect for this SP endpoint |
| `saml.idp_entity_id` | REQUIRED when `saml.enabled=true` | absolute URI, 1..1,024 bytes; exact comparison |
| `saml.idp_metadata` | REQUIRED versioned metadata-document handle when `saml.enabled=true` | bounded verified XML handle; source URL fetch occurs outside request path and is pinned by digest/version |
| `saml.idp_sso_url` | REQUIRED when metadata does not supply selected binding | absolute HTTPS URL; host must match metadata allowlist |
| `saml.idp_slo_url` | REQUIRED when SLO enabled and metadata does not supply it | absolute HTTPS URL; host must match metadata allowlist |
| `saml.idp_certificates` | derived verified certificate set from `saml.idp_metadata` | exactly the 1..5 active/rollover signing certificates in the pinned metadata version; no separate trust input; metadata refresh atomically replaces the set after overlap validation |
| `saml.sp_signing_key` | REQUIRED signing-key-provider handle when `saml.enabled=true` | private key is secret; staged rotation; public certificate published only through metadata |
| `saml.sp_decryption_key` | REQUIRED decryption-key-provider handle when encrypted assertions are enabled | private key is secret; old decrypt-only key retained for bounded overlap |
| `saml.encrypted_assertions.enabled` | `false` | explicit Boolean; when true `saml.sp_decryption_key` is REQUIRED and decrypt-then-validate processing applies |
| `saml.nameid_formats` | emailAddress, persistent | closed ordered allowlist; transient unsupported for durable linking |
| `saml.attribute_mapping` | REQUIRED versioned mapping handle when `saml.enabled=true` | closed typed source-to-identity mapping; direct free-form expressions forbidden |
| `saml.slo.enabled` | `true` when `saml.enabled=true` | explicit Boolean; requires SLO endpoints and session-index persistence |
| `saml.bindings` | outbound AuthnRequest/LogoutRequest = HTTP-Redirect; inbound login Response/LogoutRequest/LogoutResponse = HTTP-POST; outbound LogoutResponse to an inbound LogoutRequest = HTTP-POST | closed directional binding profile; the outbound LogoutResponse uses the request's verified return endpoint; HTTP-POST AuthnRequest, HTTP-Redirect Response, artifact, SOAP and ECP are unsupported |

## PostgreSQL and Valkey transport security

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `postgres.tls.mode` | `verify-full` | enum `verify-full`; disabling is unsupported in production reference profile |
| `postgres.tls.server_name` | REQUIRED string | 1..253-byte DNS name matching certificate and DSN host |
| `postgres.tls.root_ca` | REQUIRED trust-store handle | typed handle; startup-only; no inline PEM in diagnostics |
| `valkey.tls.enabled` | `true` | REQUIRED true when Valkey enabled in production |
| `valkey.tls.server_name` | REQUIRED string when Valkey enabled | 1..253-byte DNS name matching certificate and endpoint host |
| `valkey.tls.root_ca` | REQUIRED when Valkey enabled | typed trust-store handle; startup-only |

## Recovery, reconciliation, and retention

| Path | Reference value | Safe bounds / semantics |
| --- | --- | --- |
| `command.pending_lease` | 60 seconds | 10 seconds..10 minutes |
| `command.heartbeat` | 20 seconds | 1 second..less than half pending lease |
| `command.recovery_deadline` | 24 hours | 1 hour..7 days; breach alerts and remains fail-closed |
| `command.result_retention` | 30 days | 7..90 days after terminal state |
| `capability.reservation_retention` | capability expiry plus 24 hours for proven non-unknown reservations | minimum recovery deadline; maximum 31 days after authoritative proof |
| `capability.unknown_retention` | until authoritative reconciliation or audited operator resolution, with no time-based deletion | after 31 days payload is crypto-shredded and the digest/state moves to restricted archive; reservation authority remains and reuse stays denied |
| `capability.terminal_retention` | 30 days | 7..90 days after finalized/released/expired/revoked; revoked record remains through later of original capability expiry and retention deadline, then payload/linkage is crypto-shredded; minimal tenant/purpose/keyed-digest/key-version/expiry/revoked tombstone has no time-based deletion and keeps replay denied until proved key-version retirement makes every bearer cryptographically invalid before lookup |
| `outbox.effect_pending_retention` | until terminal outcome with no time-based deletion | reconciler and alert remain active; payload bearer ciphertext still erases at bearer expiry |
| `outbox.effect_terminal_retention` | 30 days | 7..90 days; safe receipt metadata only |
| `lifecycle.cascade_deadline` | 7 days | 1 hour..30 days; breach alerts and blocks destructive completion |
| `lifecycle.retry_retention` | until acknowledgement or approved terminal limitation | no time-based silent drop |
| `lifecycle.event_retention` | 1 year | 90 days..7 years, subject to shorter privacy requirement only after checkpoint proof |
| `lifecycle.checkpoint_retention` | consumer lifetime plus 90 days | MUST outlive every retained event needed for gap detection |
| `lifecycle.ack_retention` | 7 years | 1..7 years; pseudonymous and crypto-shredded when lawful retention ends |
| `lifecycle.identifier_tombstone` | 30 days | 1..365 days; keyed non-reversible scoped digest |
| `lifecycle.deletion_tombstone` | 7 years | 1..7 years; opaque proof-of-deletion only, no direct identifier |
| `lifecycle.legal_hold` | closed policy `struct:ref.lifecycle.legal_hold` | no expiry default; release requires named privacy-administrator authorization and audit |

## Required secret and connection fields

| Path | Requirement | Secret / lifecycle |
| --- | --- | --- |
| `postgres.dsn` | REQUIRED URI; no default | secret; startup-only; diagnostics expose only safe driver/host class |
| `valkey.endpoint` | REQUIRED URI when `valkey.enabled=true` | non-secret `rediss://host:port` with no userinfo/query/fragment; embedded credentials forbidden; startup-only |
| `secrets.valkey_credential` | REQUIRED credential-provider handle when Valkey authentication is enabled | secret; startup-only; rotated by validated connection handoff |
| `secrets.command_fingerprint_key` | REQUIRED 32-byte-or-stronger versioned key | secret; retained until no pending/unknown command references the version and then through command-result retention plus recovery window; retirement MUST NOT make a command unverifiable or reusable |
| `secrets.envelope_provider` | REQUIRED authenticated key-provider handle | secret; staged rotate/reload supported, removal only after rewrap proof |
| `secrets.session_digest_key` | REQUIRED 32-byte-or-stronger versioned key | secret; new issue uses newest, old verify during declared rotation window |
| `secrets.otp_digest_key` | REQUIRED 32-byte-or-stronger versioned HMAC key | secret; newest signs new codes, retained prior versions verify only through maximum OTP TTL plus clock skew |
| `secrets.api_key_digest_key` | REQUIRED 32-byte-or-stronger versioned key | secret; same rotation rule, never emitted |
| `secrets.csrf_key` | REQUIRED 32-byte-or-stronger versioned key | secret; rotation retains prior key for maximum session/CSRF overlap |
| `secrets.oauth_signing_provider` | REQUIRED ES256 key-provider handle | secret/private material; staged rotation; public JWKS only |
| `providers.<id>.client_secret` | REQUIRED when selected provider is confidential | secret; provider-specific staged rotation; no generic fallback |
| `captcha.<id>.secret` | REQUIRED when `captcha.enabled=true` and provider equals `<id>` | secret; atomic reload permitted after validation |
| `delivery.sender` | REQUIRED sender-provider handle in production; capture-sender handle REQUIRED in tests and forbidden in production | secret-bearing handle; bounded timeout and concurrency |

Secret reload MUST construct and validate a complete new immutable snapshot,
atomically publish it, retain the old version for the exact verification/decrypt
overlap, and zero/drop unreferenced in-memory material. A missing current or
required previous key makes readiness fail; it MUST NOT cause new random key
generation at startup.

## Validation and proof

Construction MUST reject every out-of-range, contradictory, missing-required,
unknown, duplicate, or secret-in-plain-diagnostic value before side effects.
The reference package MUST generate a canonical redacted manifest and a hash of
the complete effective configuration. Tests MUST cover every default, lower and
upper bound, cross-field invariant, reload/restart classification, redaction,
and evidence-fingerprint change. A test that merely asserts constants in source
is insufficient; the values MUST be exercised through public construction and
the affected runtime contract.
