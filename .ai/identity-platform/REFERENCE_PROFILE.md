# Identity Platform Reference Deployment Profile

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Purpose and precedence

Package cores remain policy-configurable and MUST reject ambiguous zero values.
`identity/reference` MUST supply this exact coherent profile so independently
implemented modules compose predictably. A deployment MAY override values only
within package-declared safe bounds and MUST record security, migration and
compatibility effects. `END_STATE.md` and package security invariants take
precedence if a value here would otherwise weaken them.
Protocol behavior MUST follow `PROTOCOL_BASELINES.md`; security audit behavior
MUST follow `SECURITY_EVENTS.md`. A profile override MUST NOT advertise a
protocol or security property whose selected baseline no longer proves it.

## Identity and identifiers

- All internal IDs MUST be opaque 128-bit-or-stronger random identifiers using
  the repository identifier primitive and canonical text encoding.
- Email MUST be trimmed around the complete input, length-bounded, IDNA-
  canonicalized for the domain and case-folded for reference-profile lookup;
  provider-specific dot/plus rules MUST NOT be applied. Display input is kept
  separately only when policy permits.
- Usernames MUST use Unicode 15.1 NFC then case folding for canonical lookup and
  be 3 to 32 Unicode code points. The reference profile permits only `Latin`
  and `Common`, with at most one non-Common script per name; mixed scripts are
  denied. The versioned reserved set is `admin`, `administrator`, `api`, `auth`,
  `login`, `logout`, `me`, `oauth`, `oidc`, `root`, `scim`, `sso`, `support`,
  `system`, and `webhook`, matched after case folding. Display username is
  separate and MUST NOT determine uniqueness.
- Phone input MUST canonicalize to E.164 with pinned numbering metadata; no
  implicit default region is used unless deployment configuration supplies it.
- Unverified identifiers MUST NOT authorize linking, recovery, SSO routing or
  organization invitation acceptance.
- Typed additional fields MUST be deny-input/deny-output/deny-write and
  sensitive by default until every permission is declared explicitly.
- Every public host MUST map through static configuration to exactly one tenant
  or the named public realm before identifier lookup. The reference profile
  MUST NOT accept a caller-supplied tenant selector for signin and MUST NOT
  derive tenant from an untrusted forwarded host. Unknown hosts return HTTP 421;
  an unknown tenant/identifier within a valid host uses the same public status,
  body size class, rate policy, and constant-work verification as an unknown
  identifier.
- Verified email, username, and phone identifiers remain tombstoned for 30 days
  after removal or anonymization and MUST NOT be reassigned while a capability,
  invitation, provider link, or recovery reference can still name the old
  identifier version. Reuse requires new ownership verification and MUST NOT
  revive an old provider/account link.

## Password and verification

- Passwords MUST be 12 to 128 bytes in the reference profile, MUST NOT be
  trimmed or Unicode-normalized, and MUST use the existing password primitive's
  reviewed Argon2id recommended profile with automatic bounded rehash.
- Signup, password change, reset and administrator set MUST reject any HIBP
  breach count greater than zero. HIBP unavailable, throttled, malformed, or
  ambiguous fails closed for those operations in the reference profile and
  MUST NOT be treated as a clean password.
- Email verification capability lifetime is 24 hours. Resend cooldown starts at
  60 seconds, uses the risk limiter, and supersedes earlier reference-profile
  links after successful enqueue of the replacement intent.
- Password reset capability lifetime is 30 minutes, single-use, password-
  version bound and revokes all existing sessions after successful reset.
- Password change requires a session no older than 15 minutes and revokes all
  other sessions by default.
- A password marked compromised MUST be versioned as compromised, deny new
  password authentication, revoke sessions and trusted devices, notify through
  the durable delivery contract, and require the normal independently verified
  reset journey. API keys and provider grants remain separate credentials and
  are revoked only by their explicit compromise/cascade policy.

## Sessions, cookies, and CSRF

- The default profile uses an opaque 32-byte random session token, stores only
  a scoped digest, has 7-day absolute expiry, 24-hour idle expiry, 24-hour
  freshness and rotation on authentication, privilege change, or when the
  session age reaches 12 hours during an authorized refresh. Rotation has no
  bearer reuse grace in the reference profile.
- Every session-issuing flow resolves one `RememberPolicy` before its first
  continuation. Persistent uses the normal seven-day profile; explicit
  non-persistent uses a browser-session cookie and a 24-hour absolute server
  lifetime. Risk, MFA, OAuth/SSO state and callbacks preserve that value and
  cannot upgrade it.
- Session bearer delivery uses only `struct:ref.session.bearer_issuance`.
  Authorization returns a 60-second reveal-once continuation, not a bearer;
  the issue route accepts that continuation only in its bounded JSON body,
  ignores cookies, rejects a request `Authorization` credential, and cannot
  change the bound audience, origin, lifetime or JSON/header transport. The
  issue transaction consumes the continuation with bearer creation and emits
  one `no-store` response; unknown or replayed outcomes return no credential.
- Standalone current-password verification requires the authenticated session,
  exact purpose and audience and the configured five-minute freshness window.
  It returns a short-lived proof only, creates or extends no session, and
  rejects replay or cross-purpose use without revealing account existence.
- Browser cookie name is `__Host-identity_session`; it MUST be `Secure`,
  `HttpOnly`, `Path=/`, have no `Domain`, and use `SameSite=Lax`. Cross-site or
  shared-parent-domain deployments require an explicit alternate profile and
  MUST NOT reuse this cookie name misleadingly. The cookie is limited to 3,800
  bytes.
  A request containing duplicate session-cookie names, conflicting Cookie
  headers, malformed encoding, or more than 32 cookies/16 KiB total cookie
  bytes MUST fail authentication. Logout deletes the cookie with the identical
  Secure, HttpOnly, SameSite, Path and Domain tuple used for issuance.
- POST, PUT, PATCH, and DELETE requests authenticated by cookies require a
  32-byte random synchronizer CSRF token. At issuance, the server computes and
  stores only `HMAC-SHA-256(csrf_key_version,
  "identity-csrf-v1\\x00" || tenant_id || session_id || session_version ||
  raw_token)` plus its key version. The active token state is exactly
  `(tenant, session, session version, key version, digest)`; it becomes invalid
  atomically on authentication, session rotation/revocation, account/tenant
  switch, or privilege change. Validation accepts exactly one token channel,
  `X-CSRF-Token` or the exact form field, recomputes the digest with the named
  current/retained key, and compares it in constant time. Duplicate channels,
  unknown key versions, stale session versions, and tokens in query parameters
  are forbidden. CSRF-key rotation issues new tokens with the newest key and
  retains an old key only for the configured session/CSRF overlap. The request Origin
  MUST exactly match a configured origin; a browser request with missing or
  `null` Origin is denied, except a same-origin form profile may use an exact
  HTTPS Referer fallback. The token rotates on authentication, session rotation,
  account/tenant switch, and privilege change.
- Every unauthenticated endpoint capable of issuing, replacing, or linking a
  session MUST use a five-minute, single-use pre-auth transaction bound to an
  `__Host-identity_flow` cookie with Secure, HttpOnly, SameSite=Lax, Path=/ and
  no Domain, plus exact origin, intended action, tenant, callback/redirect and
  server nonce. Password/OTP signin and OAuth initiation require the
  transaction; OAuth/OIDC callbacks additionally require its state/nonce
  binding. Passive GET, HEAD, prefetch and link-scanner requests MUST NOT
  consume a credential or create a session. Email
  verification, magic-link, password-reset and invitation URLs render a
  no-store/no-referrer confirmation and consume only through the bound POST.
- Apple and SAML cross-site HTTP-POST callbacks use a separate
  `__Secure-identity_frontchannel` correlation cookie with `Secure`,
  `HttpOnly`, `SameSite=None`, no `Domain`, an exact Apple or SAML callback
  `Path`, a five-minute maximum lifetime and one-time flow binding. It is
  issued only for the selected cross-site POST flow and deleted with the exact
  issuance tuple after reservation. It never authenticates a session. The
  normal `__Host-identity_session` and `__Host-identity_flow` cookies remain
  `SameSite=Lax`; this exception MUST NOT weaken or reuse either cookie.
- Bearer-authenticated requests MUST ignore ambient session cookies, reject
  multiple Authorization headers or credentials in query parameters, and use
  bearer-specific `Cache-Control: no-store` and CORS policy.
- Maximum simultaneous account sessions per browser container is 5. Adding a
  sixth denies by default; eviction requires an explicit user-selected session.
- Last-login-method storage is disabled until notice/consent policy is enabled;
  when enabled it retains only the bounded method ID for 90 days and supports
  immediate clear/delete.
- Stateless sessions use authenticated encryption, 15-minute token lifetime,
  user/global version counters and no promise of per-token revocation. The
  reference production profile remains opaque/durable; stateless is a separately
  exercised compatibility profile.
- Cookie cache maximum staleness is 5 minutes and MUST be invalidated by user,
  session, active organization, membership, role, factor, password, API-key
  owner authority, impersonation grant, ban or global version change.
- One-time session-transfer tokens expire after 3 minutes, store only a scoped
  digest, bind one exact target origin/audience, require a source session fresh
  within 15 minutes, and are consumed once. Target cookie issuance is disabled
  unless that exact origin is configured for it.
  Consumption MUST NOT disclose or reuse the source bearer. It mints an exact-
  origin child handle in the same session family that cannot outlive or be
  fresher/more privileged than the source and is independently revocable.
- Anonymous upgrade locks the anonymous transition and target identity, revokes
  the complete anonymous session family, rotates the current session, and
  issues the permanent session in the same command transaction. The old
  anonymous bearer has no grace window. A committed same-command replay returns
  the recorded permanent-session result; an unknown result issues no second
  session and remains in primary-authority reconciliation.

## One-time credentials and MFA

- Email/phone OTP is exactly 6 ASCII decimal digits from unbiased randomness,
  valid for 5 minutes, maximum 5 verification attempts, 60-second resend
  cooldown and 5
  sends per hour per scoped subject before risk policy tightens it. Whitespace,
  separators, Unicode digits, and variable length are rejected. Storage uses a
  domain-separated HMAC-SHA-256 digest keyed by an independent rotating secret
  and bound to tenant, purpose, subject, challenge and key version; raw or
  unkeyed digests are forbidden and missing key material fails closed.
- Magic links are valid for 15 minutes, single-use and signup-disabled unless
  the initiating profile explicitly permits signup.
- Organization invitations are valid for 48 hours, single-use, digest-stored,
  and bound to tenant, organization, invitation version, intended verified
  recipient, role and purpose. Resend invalidates the prior token only after the
  replacement delivery intent is durably enqueued; cancel/expiry/recipient
  change prevents acceptance atomically.
- TOTP uses RFC 6238 SHA-1, 6 digits, 30-second period and one-step past/future
  skew for broad authenticator compatibility; accepted time steps are replay-
  protected. Stronger algorithms MAY be configured only with client support.
- Recovery setup generates 10 independent 128-bit-or-stronger codes, displays
  them once, stores only digests and replaces the whole set on regeneration.
  No operation re-views plaintext; later reads expose only bounded status/count,
  and a fresh reauthenticated regeneration atomically invalidates every old
  code.
- TOTP enrollment secrets and provisioning URI/QR output are reveal-once,
  `Cache-Control: no-store`, absent from logs/referrers, and expire unconfirmed
  after 10 minutes. Labels use strict percent encoding and issuer label/query
  values MUST match.
- Trusted-device credentials are 32 random bytes, reveal once in a
  `__Host-identity_trusted_device` Secure/HttpOnly/SameSite=Lax/Path=/ cookie,
  store a domain-separated keyed digest, bind user/tenant/factor/session and
  version, and are issued only after successful MFA and session rotation. They
  rotate on the first successful use after reaching 24 hours of age and never
  more frequently. One transaction claims the old credential and issues the
  successor; concurrent uses of the old credential after that claim fail closed
  and MUST NOT mint additional successors. They fail closed on replay/ambiguity,
  undergo risk evaluation on every use, expire after 30 days, are individually
  revocable, and are invalidated by password reset, factor reset, user
  suspension or global compromise version change.
- MFA challenge permits 5 failed attempts total across all methods and expires
  after 10 minutes. Success atomically consumes the challenge once, binds the
  result to one tenant/session/action transaction, rotates the session, and
  records `amr`, `acr`, `auth_time`, factor version, and the resulting 15-minute
  freshness. Factor changes require that freshness. Phone possession alone
  MUST NOT reset a password or recover an account; it requires a separately
  verified factor or an existing fresh session. Phone changes have a 24-hour
  recovery-sensitive quarantine unless an independent factor approves them.
- Phone password recovery is disabled by default. Both request and completion
  deny unless `phone.recovery.enabled=true` is explicitly selected at startup.
  When enabled, the canonical reset capability, purpose-bound phone OTP, and an
  eligible independent factor remain mandatory. Only fresh one-use immutable
  `RiskEvidence` issued by `identity/risk` and bound to the exact recovery flow
  may carry carrier decisions; caller-supplied facts deny. Recovery denies whenever a SIM-swap,
  number-recycling, or carrier signal is positive, unknown, or unavailable.

## WebAuthn and passkeys

- RP ID and allowed HTTPS origins are explicit configuration; loopback HTTP is
  allowed only in the development profile. User verification is `preferred`
  only for adding a compatibility credential that cannot satisfy authentication,
  freshness or MFA by itself; it is `required` for passwordless, usernameless,
  recent-authentication and MFA ceremonies.
- Challenges and opaque user handles are 32 random bytes; user handles are
  unique within the RP and at most 64 bytes. Credential IDs are unique across
  the complete RP namespace and MUST NOT be disambiguated by tenant.
- ES256 and EdDSA are enabled. The named RS256 compatibility profile is also
  enabled in the reference configuration and MUST be tested independently.
  Unsupported/weak algorithms MUST be rejected rather than downgraded.
- Attestation defaults to `none`; enterprise/direct attestation requires a
  named trust/metadata policy. Registration/assertion challenges expire after
  5 minutes and are single-use.
- Collected client data is capped at 16 KiB, attestation objects at 64 KiB,
  authenticator data at 4 KiB, and credential IDs at 1 KiB before allocation or
  parsing. CBOR is capped at depth 16 and 256 items. The selected typed
  extensions are `credProps` and `credProtect`; `appid` and `uvm` are disabled.
- Discoverable credentials are required for passkey-first registration and
  usernameless signin. Removing the final configured recovery path is denied.
- Backup eligibility is immutable; `BS=1` with `BE=0` is rejected. Backup state
  is persisted on every assertion. For a backup-eligible credential, zero,
  equal or decreased counters produce risk evidence and persistence retains the
  larger stored value. For a non-backup credential, zero/zero is accepted as
  unsupported counting and a positive increase advances; once the stored value
  is positive, an equal, decreased or zero received value denies authentication
  and requires recovery or an independently verified factor.

## OAuth, OIDC, and federation

- Authorization code flows MUST use PKCE S256. Plain PKCE and implicit/password
  grants are disabled. State and nonce are 32 random bytes, single-use and valid
  for 10 minutes. Callback and post-logout redirects use byte-for-byte exact
  allowlists. Redirects containing fragments, userinfo, wildcards, backslashes,
  or normalization-dependent matches are rejected; invalid redirects never
  receive an OAuth error redirect.
- Social and enterprise relying-party transactions retain the raw PKCE verifier
  only as envelope-AEAD ciphertext for the one bound token exchange. Exact AAD
  binds tenant, provider/issuer, client, transaction/state digest, redirect,
  response mode, operation and configuration version. A separate keyed
  commitment is the only lookup/replay value. Decrypt requires the reserved
  callback capability; terminal outcomes erase ciphertext, while an ambiguous
  exchange retains it only for authoritative recovery without resubmission.
- Provider tokens and client secrets MUST use envelope encryption with provider,
  client, tenant and account context. Refresh is single-flight with no blind
  retry after an ambiguous exchange.
- Native provider-token signin is the closed `native-token-modes-v1` matrix:
  Apple accepts `id_token`; Facebook accepts `opaque_access_token`; Google and
  LINE accept either; every other `provider-catalog-v1` ID accepts neither.
  An unsupported provider/mode pair is rejected before token parsing or
  provider I/O. ID tokens use the catalog's offline issuer/signature/audience/
  authorized-party/nonce checks; opaque tokens use only the catalog's bounded
  introspection or UserInfo proof with platform-client and subject binding.
- Apple redirect signin alone fixes `response_type=code id_token` and
  `response_mode=form_post`. Its callback requires the bound code, ID token and
  state success tuple, validates nonce and `c_hash`, exchanges the code, and
  requires the same subject across both proofs. This does not enable hybrid or
  `form_post` behavior for another social provider, enterprise OIDC or the
  reference authorization server.
- Every one-time capability uses the closed HMAC-SHA-256 signing/verification
  and independent HMAC-SHA-256 replay-digest key sets in
  `struct:ref.capability.crypto`. New issuance uses newest active versions;
  retained versions remain verification/lookup authority until no bearer,
  unresolved reservation or tombstone depends on them. Missing required key
  material fails readiness.
- Social signup is enabled only for providers whose profile supplies a stable
  subject and acceptable identifier proof. Implicit account linking requires a
  provider-verified email from a provider explicitly allowlisted as authoritative
  for that domain, an already verified matching local identifier, and a named
  cross-provider collision policy. It is disabled by default. User-initiated
  linking requires a non-impersonated session fresh within 15 minutes and binds
  the target identity/session version in state. Forced linking is off.
- OAuth popup messages use the exact initiating origin, a one-time channel and
  no wildcard target. Preview OAuth proxy is disabled outside explicit preview
  profiles and performs no production identity/session writes.
- OAuth-server authorization codes live 5 minutes; access tokens 15 minutes;
  refresh tokens 30 days with rotation and family reuse revocation. Device
  codes are 32 random bytes, unpadded-base64url encoded, digest-stored and live
  10 minutes with a 5-second initial polling interval; every RFC 8628
  `slow_down` increases that device code's interval by exactly 5 seconds.
  Device flow is explicitly enabled in the reference profile; its
  `verification_uri` is the external URL for the registered
  `identity.oauth-server.device-inspect` operation, and a complete URI larger
  than 512 bytes is never emitted.
- Every social/generic provider profile MUST prove S256 support. A provider that
  cannot complete S256 is disabled in the reference profile and MUST NOT
  downgrade to state-only protection.
- Public clients use `token_endpoint_auth_method=none` and PKCE; confidential
  clients use `client_secret_basic` with constant-time digested-secret
  verification. Multiple credential channels are rejected. Dynamic registration
  is disabled until startup configuration selects the typed initial-access-token
  profile, one immutable owner and its versioned secret-provider token; startup
  must establish the owner/audience/version-scoped digest and expiry in
  `oauth-server/postgres` before registering the route or becoming ready. The
  token is consumed atomically with one client creation. Disabling the profile
  removes the route without deleting registered clients. Requested
  scopes contained in `oauth_server.dynamic_registration.allowed_scopes`.
  The authorization-server catalog is the exact `oauth_server.scopes` set, and
  RFC 9728 metadata advertises only the exact
  `oauth_server.protected_resource.supported_scopes` subset. Its `resource` and
  every protected-resource token audience are byte-for-byte equal to the
  canonical `oauth_server.protected_resource.resource` origin. Enabling dynamic
  registration selects RFC 7591 only. RFC 7592 management remains an unselected
  future profile, so the reference profile issues no registration management
  URI or registration access token.
- OIDC signs with ES256 by default, rotates keys every 30 days, retains old
  public keys through maximum token lifetime plus clock skew, and uses pairwise
  subjects only when an exact sector identifier and versioned secret
  HMAC-SHA-256 derivation key are configured. Derivation-key rotation requires
  an explicit stable-subject overlap/migration.
- The authenticated-session JWT exchange is a required private extension in the
  reference profile, not RFC 8693. Its grant identifier is
  `urn:golib:params:oauth:grant-type:session-jwt`; it accepts no caller-selected
  subject, actor, or arbitrary audience and is omitted from standard OAuth grant
  metadata except in a namespaced extension object. RFC 8693 token exchange is
  explicitly unsupported and unadvertised. Issued JWTs bind global, tenant,
  user, authorization, applicable organization/factor, OAuth-client, grant,
  signing-key-compromise epoch and `kid`, plus source session and session-family
  dimensions and never outlive the source session.
- Enterprise SSO domain routing requires DNS TXT proof at
  `_identity-verification.<registrable-domain>` containing a 32-byte random,
  tenant/organization/domain-bound token. IDNA2008 canonicalization and the
  pinned Public Suffix List determine the registrable domain; public suffix and
  wildcard claims are rejected. Proof expires after 24 hours and routing stops
  until a fresh proof succeeds; conflict or revocation stops routing
  immediately. DNSSEC status is recorded when available
  but is not required. JIT role mapping is deny-by-default. IdP-initiated SAML
  is the explicit Boolean `false`; setting it to `true` requires the configured
  unsolicited-response and login-CSRF/confirmation policy. SP-initiated login uses the exact configured HTTP-POST ACS and
  requires Destination/Recipient/InResponseTo; RelayState is optional and,
  when present, uses one-time binding.
  IdP-initiated login, if enabled, uses a distinct configured HTTP-POST URL,
  exact Destination/Recipient without a fabricated request ID or required
  RelayState. An optional RelayState is untrusted for authority. Outbound
  HTTP-Redirect requests use the exact configured
  `saml.redirect_signature_algorithm` and sign the encoded SAMLRequest, optional
  RelayState, and SigAlg fields in protocol order before appending Signature.
  SAML requires signed responses/assertions, timestamps, audience
  and replay protection; SHA-1 algorithms are disabled. SP-initiated logout and inbound SLO are selected;
  they require signed messages, exact endpoint/issuer/session-index binding,
  replay protection, and local-session revocation even when the IdP outcome is
  unknown.
- Existing-provider SSO login applies the current versioned mapping policy to
  claim provenance before session issuance. Missing/null claims, authoritative
  removals, local-owned fields, downgrades, replay and unknown outcomes are
  explicit; no stale mapping may restore privilege or overwrite local data.
- Enterprise providers are created disabled. Enable, disable, credential
  rotation and enforcement update are distinct versioned commands; generic
  update/delete cannot substitute for them. Enable requires current verified
  domain, credential and lockout-safety checkpoints. Disable denies new starts
  immediately, credential rotation has no default overlap, and every ambiguous
  provider-side result remains reconciliation-required. Break-glass issuance
  reveals one 15-minute organization/policy-bound capability; only its separate
  one-use consumption establishes bounded recovery authority.
- Enterprise domain challenge and verification are callable SSO operations.
  `sso/domain-verification` owns only bounded proof retrieval/classification,
  and `organization` remains the sole durable claim/uniqueness authority; no
  verifier or routing cache may become a second ownership source.

## API keys, SCIM, and administration

- API keys contain at least 32 random bytes, reveal once, store only digest and
  bounded prefix, default to 90-day expiry, and carry explicit permissions.
  Unlimited lifetime/quota requires an explicit administrator policy.
- API-key effective authority is the intersection of the immutable key grant,
  current active user status, current organization/membership/role authority,
  and current authorization policy. Ban, deletion, organization archive,
  membership removal, and role reduction increment an authority version and
  invalidate positive caches before the change is acknowledged. Verification
  fails closed when current authority cannot be established.
- PostgreSQL is authoritative for API keys. Valkey secondary storage has a
  maximum 60-second positive cache, no positive fallback after revocation
  invalidation failure, and no negative-cache authorization decision.
- SCIM bearer tokens contain at least 32 random bytes, reveal once, store only a
  scoped digest, expire after 90 days by default and are organization/provider
  owned. Every SCIM PUT, PATCH and DELETE requires `If-Match`; missing returns
  HTTP 428 and mismatch returns HTTP 412 without mutation. ETags are strong
  quoted encodings of the authoritative resource version.
- Personal or user-owned SCIM connections are rejected. Connections, bearer
  tokens, mappings and external IDs are organization/provider scoped; legacy
  personal records require an explicit migrate-to-organization, disable or
  delete disposition before readiness.
- Connection update, individual bearer-token revoke and bounded reconciliation
  are distinct administrator operations. Update is versioned, revocation never
  reveals bearer material, and reconciliation reports attributable bounded
  drift/conflict counts and preserves an unknown-outcome cursor without
  overwriting concurrent local authority.
- SCIM list count defaults to 100 and is capped at 1,000; filters are capped at
  depth 16 and 256 AST nodes; PATCH is capped at 100 operations. Bulk is capped
  at 1,000 operations and 1 MiB decoded body, with `failOnErrors` capped at 100.
  SCIM JSON rejects exact duplicate members and case-insensitive attribute-name
  collisions. Below-one `startIndex` is 1, negative `count` is zero, absent
  sort uses server `id` ascending, and `itemsPerPage` is the actual returned
  count from the same snapshot as `totalResults`.
- SCIM Bulk persists every ordered child before execution. After a positive
  `failOnErrors` threshold is reached by durable failed results, each remaining
  not-started child becomes durably `skipped`; zero or omitted threshold means
  no cutoff. A skipped child is unprocessed, emits
  `identity.scim.bulk_skip_child`, and is omitted from BulkResponse
  `Operations` without a wire status, location, version, Error or private
  `scimType`. Replay does not convert it into a failed child.
- Active organization is session/container state, never a user-global row. A
  switch requires current membership and increments the session version;
  membership removal, organization archive, or role invalidation clears it.
- Impersonation lasts at most 15 minutes, requires a session fresh within 15
  minutes with MFA and a non-empty reason, cannot nest or use a stateless/offline-
  revocation session, and is disabled for protected service/super-administrator
  accounts by default. Effective authority is the intersection of current actor
  authority, current target authority, grant scope, and current policy.
  Credential/factor/API-key/recovery changes, ownership transfer, impersonation
  administration and privilege-policy changes are denied while impersonating.
- Bans are immediate for new authentication and session refresh. Existing
  sessions are revoked by default. Every privileged administration operation
  requires an explicit authorization statement and immutable audit.
- Platform-administrator roles and subject assignments are versioned rows in
  `authorization/postgres`, composed with `identity/postgres` identities and
  `audit/postgres`/outbox effects. Bootstrap creates the first role and
  assignment once; later changes use authorized assignment operations, advance
  authority versions and invalidate positive caches before acknowledgement.
- Production audit uses `audit/postgres`, the event profile in
  `SECURITY_EVENTS.md`, and atomic or durable-outbox delivery. Missing mandatory
  audit readiness blocks startup; logs, metrics and traces are not substitutes.
- Privacy export uses `identity-portable-json-v1`: one uncompressed UTF-8 JSON
  document with a schema/version manifest, RFC 3339 UTC timestamps, opaque IDs,
  stable section ordering and fragment/whole-artifact SHA-256 digests. Its exact
  portable sections are identity, accounts, identifiers, session metadata,
  devices, organizations, memberships and consents; credential verifiers,
  recovery material, bearers, provider tokens/raw payloads, risk signals and
  internal audit payloads are excluded. The asynchronous states are exactly
  `queued`, `running`, `ready`, `failed`, `cancelled`, and `expired`.
  Privacy export uses the contributor set and immutable version-vector
  watermark in `LIFECYCLE_CONSUMERS.md`. PostgreSQL contributors MUST read from
  an append-only/versioned projection at the recorded checkpoint or use an
  immutable bounded fragment staged during the request transaction. Other
  authorities MUST reproduce their recorded checkpoint from a versioned
  journal. Export artifacts remain
  envelope-encrypted at rest and non-downloadable until every fragment and final identity/privacy
  epoch check succeeds. Anonymization/deletion atomically cancels unpublished
  exports, revokes issued or reserved download capabilities, and denies every
  later download before artifact erasure completes. An authorized download
  decrypts only into the bounded HTTPS `application/json` response stream with
  `no-store`; legal holds and provider-held data appear only as manifest
  limitations and never extend a download capability.

## Risk, delivery, and localization

- Risk limits use trusted-proxy-derived IP only and canonical full IPv4/IPv6
  addresses, domain-separated HMAC-SHA-256 identifier/device/IP
  pseudonyms with rotating key IDs, and controlled action IDs. Raw or unkeyed
  low-entropy digests are forbidden. PostgreSQL owns durable lockouts,
  overrides, evidence journal and reconciliation; Valkey owns ephemeral velocity
  counters only. A Valkey outage cannot clear a PostgreSQL lockout.
- Risk decisions combine all required signals using precedence `deny` >
  `step-up` > `throttle` > `allow`. Counters mutate before returning the
  decision. The operation matrix is authoritative: HIBP unavailability for
  password create/change/reset and CAPTCHA ambiguity for protected signup,
  signin, reset, or credential change produce `deny`; CAPTCHA ambiguity for a
  low-risk read produces `step-up` only when an independent configured factor
  exists and otherwise produces `deny`. No unavailable/unknown required signal
  becomes allow or clean evidence. Administrative overrides are
  evaluated last only to narrow an expiring named action and cannot override a
  hard tenant/status/replay denial.
- Core client HTTP rate limiting is enabled in every deployed profile with a
  60-second window and 100-request default maximum. Sensitive route manifests
  MUST declare stricter rules. IPv4-mapped IPv6 is normalized to IPv4; IPv6
  uses its canonical RFC 5952 address without subnet aggregation. PostgreSQL
  owns atomic multi-instance counters; Valkey may cache positive metadata only.
  Failure follows the exact route policy and returns stable `Retry-After` when
  denied.
- CAPTCHA is required only after the risk policy requests it; adapters MUST
  enforce site/hostname/action binding. Protected signup/signin/reset actions
  fail closed on CAPTCHA provider ambiguity. Adapters return protocol evidence;
  only risk policy applies score thresholds. A 32-byte server-generated attempt
  ID binds provider/site, tenant, subject or anonymous flow, exact action and
  request fingerprint. Successful evidence is valid for two minutes and is
  reserved through the typed `CaptchaEvidenceContributor`, then finalized in
  the same PostgreSQL commit as the exact protected action, session/authority
  transition, audit/outbox records, and command result. A precheck or separate
  consumption transaction grants no authority. Retry is allowed only for the
  same command and request fingerprint; an ambiguous commit leaves evidence
  reserved for primary-authority reconciliation and MUST NOT rerun the
  provider. Production uses fixed provider HTTPS endpoints, no redirects
  or endpoint overrides, and rejects userinfo, query credentials, private/link-
  local/reserved destinations and DNS rebinding.
  `identity/risk` derives the sole replay fingerprint from the raw provider
  token plus tenant/provider/site/profile/configuration scope under the
  versioned CAPTCHA replay key. PostgreSQL enforces one issuance winner and
  retains a keyed tombstone after payload erasure; adapters and callers cannot
  choose replay identity.
- The reference CAPTCHA provider profiles are exactly those in
  `REFERENCE_CONFIGURATION.md`: each fixes its Siteverify API/version/tier,
  requires a site key and canonical hostname allowlist, fixes origins to empty,
  declares action support explicitly, and disables remote-IP disclosure.
- HIBP uses only `https://api.pwnedpasswords.com/range/{prefix}`, sends
  `Add-Padding: true` and User-Agent
  supplied by the required non-secret `hibp.user_agent` field in the form
  `golib-identity-reference/<module-version> (+<security-contact-URL>)`,
  follows no redirects, and caches only fully validated prefix sets for 24 hours
  with at most 10,000 entries and single-flight fill. Each response has a
  10-second total deadline, a 1 MiB reference limit, and a public maximum
  breach count of 1,000,000,000. At most 8 outbound HIBP range requests run
  concurrently per process; same-prefix requests still coalesce. Zero-count entries are
  padding/no match; identical duplicates collapse, conflicting duplicates or
  count overflow reject the response.
- Delivery templates are versioned, locale-aware and context-escaped. Queue
  acceptance is not delivery. Reference tests use capture senders; production
  configuration MUST supply a real bounded Sender implementation through the
  documented Better-Auth-equivalent application seam. A delivery effect has at
  most five provider attempts: attempt one is immediate and later attempts use
  full-jitter delay ceilings of 1 second, 5 seconds, 30 seconds, and 2 minutes.
  An ambiguous provider outcome
  is reconciled and is resubmitted only under a pinned provider-idempotency
  contract; permanent rejection enters the dead-letter state immediately.
- Locale precedence is explicit request, authenticated user/session, signed
  cookie, bounded `Accept-Language`, then `en`. Unsupported locales fall back to
  `en`; machine error codes and original error identity never change.

## HTTP limits and lifecycle

- Default request body limit is 1 MiB, header bytes 64 KiB, URL 8 KiB, JSON
  nesting 32 and collection elements 1,000 unless a stricter endpoint limit is
  declared. SAML is capped at 1 MiB encoded request, 2 MiB decoded XML, depth
  64, 10,000 elements, 128 attributes per element, 4 signatures, 1 assertion and
  5 certificates. SAML clock skew is 2 minutes and replay state is retained
  through assertion expiry plus 5 minutes, never less than 10 minutes.
- Server read-header timeout is 5 seconds, complete read timeout 15 seconds,
  write timeout 30 seconds and idle timeout 60 seconds. External provider calls
  have a 10-second total deadline, 3-second connect timeout, 5-second TLS
  handshake timeout, zero redirects unless a selected protocol explicitly
  requires an exact allowlisted redirect, and at most 3 total attempts (2
  retries) only when the operation is proven idempotent before any effect or a
  pinned provider-idempotency contract makes resubmission safe. A timeout or
  disconnect after a possibly transmitted mutation is outcome-unknown and is
  not retried blindly.
- Trusted proxies, hosts, origins and externally visible base URL are explicit
  allowlists. The reference proxy profile trusts only configured CIDRs and
  parses one RFC 7239 `Forwarded` field right-to-left across at most 8 elements.
  Multiple fields, more than 8 elements, duplicate/conflicting
  `for`/`host`/`proto`, obfuscated identifiers, invalid quoting, and simultaneous
  `X-Forwarded-*` input are rejected. Forwarded headers from untrusted peers are
  ignored, not merged.
- CORS is disabled by default. An enabled browser profile uses exact origins,
  explicit methods/headers, credentials only for named origins, a 10-minute
  preflight maximum age, and `Vary: Origin, Access-Control-Request-Method,
  Access-Control-Request-Headers`; wildcard origins and reflected request
  origins are forbidden.
- HTTP idempotency uses the existing PostgreSQL adapter and a 32-byte random
  caller key carried only in `Idempotency-Key`. Its keyed lookup digest is
  scoped only by tenant, actor, method, and canonical route ID; request body and
  other behavior-bearing fields MUST NOT alter that lookup key. The separately
  stored canonical request fingerprint includes the request-body SHA-256,
  content type, preconditions, and every behavior-bearing input. Entries expire
  after 24 hours; in-flight duplicates receive HTTP 409 with bounded retry
  metadata. Only successful deterministic responses are replayed. Denials,
  provider failures and unknown commits are never cached as success; an unknown
  commit remains blocked until reconciliation resolves the command ID.
- Health is process-only; readiness requires mandatory key material,
  PostgreSQL/migrations and selected authoritative stores. Optional provider
  degradation is reported safely without secret/configuration values.
- Shutdown stops admission, allows a bounded 30-second drain, then cancels
  owned work and closes resources exactly once.

## Change control

Every override MUST appear in generated configuration documentation and the
reference application's safe diagnostic summary without secret values. A
change to token format, canonicalization, identifier scope, cryptography,
session/cookie semantics, expiry, migration, provider trust or authorization
MUST have compatibility/migration notes and invalidate the affected evidence
fingerprints.
