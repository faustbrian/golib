# Identity Platform Reference Deployment Profile

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Purpose and precedence

Package cores remain policy-configurable and MUST reject ambiguous zero values.
`identity/reference` MUST supply this exact coherent profile so independently
implemented modules compose predictably. A deployment MAY override values only
within package-declared safe bounds and MUST record security, migration and
compatibility effects. `END_STATE.md` and package security invariants take
precedence if a value here would otherwise weaken them.

## Identity and identifiers

- All internal IDs MUST be opaque 128-bit-or-stronger random identifiers using
  the repository identifier primitive and canonical text encoding.
- Email MUST be trimmed around the complete input, length-bounded, IDNA-
  canonicalized for the domain and case-folded for reference-profile lookup;
  provider-specific dot/plus rules MUST NOT be applied. Display input is kept
  separately only when policy permits.
- Usernames MUST use Unicode NFC then case folding for canonical lookup, 3 to
  32 Unicode code points, a configured allowed-script policy and a reserved-name
  set. Display username is separate and MUST NOT determine uniqueness.
- Phone input MUST canonicalize to E.164 with pinned numbering metadata; no
  implicit default region is used unless deployment configuration supplies it.
- Unverified identifiers MUST NOT authorize linking, recovery, SSO routing or
  organization invitation acceptance.
- Typed additional fields MUST be deny-input/deny-output/deny-write and
  sensitive by default until every permission is declared explicitly.

## Password and verification

- Passwords MUST be 12 to 128 bytes in the reference profile, MUST NOT be
  trimmed or Unicode-normalized, and MUST use the existing password primitive's
  reviewed Argon2id recommended profile with automatic bounded rehash.
- Signup, password change, reset and administrator set MUST reject any HIBP
  breach count greater than zero when HIBP is available. HIBP unavailable or
  ambiguous MUST require an explicit risk step-up/fail policy; it MUST NOT be
  treated as a clean password.
- Email verification capability lifetime is 24 hours. Resend cooldown starts at
  60 seconds, uses the risk limiter, and supersedes earlier reference-profile
  links after successful enqueue of the replacement intent.
- Password reset capability lifetime is 30 minutes, single-use, password-
  version bound and revokes all existing sessions after successful reset.
- Password change requires a session no older than 15 minutes and revokes all
  other sessions by default.

## Sessions, cookies, and CSRF

- The default profile uses an opaque 32-byte random session token, stores only
  a scoped digest, has 7-day absolute expiry, 24-hour idle expiry, 24-hour
  freshness and rotation on authentication, privilege change and configured
  refresh. Rotation has no bearer reuse grace in the reference profile.
- Browser cookie name is `__Host-identity_session`; it MUST be `Secure`,
  `HttpOnly`, `Path=/`, have no `Domain`, and use `SameSite=Lax`. Cross-site or
  shared-parent-domain deployments require an explicit alternate profile and
  MUST NOT reuse this cookie name misleadingly.
- State-changing cookie-authenticated requests require a 32-byte CSRF token
  bound to session and trusted origin. Bearer-authenticated requests MUST ignore
  ambient session cookies and use bearer-specific cache/CORS policy.
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
  session, role, factor, password, ban or global version change.
- One-time session-transfer tokens expire after 3 minutes, store only a scoped
  digest, bind one exact target origin/audience, require a source session fresh
  within 15 minutes, and are consumed once. Target cookie issuance is disabled
  unless that exact origin is configured for it.

## One-time credentials and MFA

- Email/phone OTP is 6 decimal digits from unbiased randomness, valid for 5
  minutes, maximum 5 verification attempts, 60-second resend cooldown and 5
  sends per hour per scoped subject before risk policy tightens it.
- Magic links are valid for 15 minutes, single-use and signup-disabled unless
  the initiating profile explicitly permits signup.
- TOTP uses RFC 6238 SHA-1, 6 digits, 30-second period and one-step past/future
  skew for broad authenticator compatibility; accepted time steps are replay-
  protected. Stronger algorithms MAY be configured only with client support.
- Recovery setup generates 10 independent 128-bit-or-stronger codes, displays
  them once, stores only digests and replaces the whole set on regeneration.
- Trusted devices expire after 30 days, are individually revocable and are
  invalidated by password reset, factor reset, user suspension or global
  compromise version change.
- MFA challenge permits 5 failed attempts total across all methods and expires
  after 10 minutes. Factor changes require 15-minute session freshness.

## WebAuthn and passkeys

- RP ID and allowed HTTPS origins are explicit configuration; loopback HTTP is
  allowed only in the development profile. User verification is `preferred`
  for general registration and `required` for passkey-first and MFA step-up.
- ES256 and EdDSA are preferred; RS256 MAY remain enabled for compatibility.
  Unsupported/weak algorithms MUST be rejected rather than downgraded.
- Attestation defaults to `none`; enterprise/direct attestation requires a
  named trust/metadata policy. Registration/assertion challenges expire after
  5 minutes and are single-use.
- Discoverable credentials are required for passkey-first registration and
  usernameless signin. Removing the final configured recovery path is denied.

## OAuth, OIDC, and federation

- Authorization code flows MUST use PKCE S256. Plain PKCE and implicit/password
  grants are disabled. State and nonce are 32 random bytes, single-use and valid
  for 10 minutes. Callback and post-logout redirects use exact allowlists.
- Provider tokens and client secrets MUST use envelope encryption with provider,
  client, tenant and account context. Refresh is single-flight with no blind
  retry after an ambiguous exchange.
- Social signup is enabled only for providers whose profile supplies a stable
  subject and acceptable identifier proof. Implicit account linking requires a
  provider-verified email plus explicit collision policy; forced linking is off.
- OAuth popup messages use the exact initiating origin, a one-time channel and
  no wildcard target. Preview OAuth proxy is disabled outside explicit preview
  profiles and performs no production identity/session writes.
- OAuth-server authorization codes live 5 minutes; access tokens 15 minutes;
  refresh tokens 30 days with rotation and family reuse revocation; device
  codes 10 minutes with a 5-second initial polling interval.
- Public clients require PKCE; confidential clients use reviewed client-secret
  authentication. Dynamic registration is disabled until an administrator
  enables a bounded registration policy.
- OIDC signs with ES256 by default, rotates keys every 30 days, retains old
  public keys through maximum token lifetime plus clock skew, and uses pairwise
  subjects only when a sector policy is configured.
- Enterprise SSO domain routing requires current domain proof. JIT role mapping
  is deny-by-default. SAML requires signed responses/assertions, timestamps,
  audience/destination/recipient/InResponseTo and replay protection; SHA-1
  algorithms are disabled.

## API keys, SCIM, and administration

- API keys contain at least 32 random bytes, reveal once, store only digest and
  bounded prefix, default to 90-day expiry, and carry explicit permissions.
  Unlimited lifetime/quota requires an explicit administrator policy.
- PostgreSQL is authoritative for API keys. Valkey secondary storage has a
  maximum 60-second positive cache, no positive fallback after revocation
  invalidation failure, and no negative-cache authorization decision.
- SCIM bearer tokens contain at least 32 random bytes, reveal once, store only a
  scoped digest, expire after 90 days by default and are organization/provider
  owned. SCIM requires `If-Match` for conflicting replace/PATCH in the reference
  profile.
- Impersonation lasts at most 15 minutes, requires a session fresh within 15
  minutes and a non-empty reason, cannot nest, and is disabled for protected
  service/super-administrator accounts by default.
- Bans are immediate for new authentication and session refresh. Existing
  sessions are revoked by default. Every privileged administration operation
  requires an explicit authorization statement and immutable audit.

## Risk, delivery, and localization

- Risk limits use trusted-proxy-derived IP only, canonical IPv4 and IPv6 /64
  subnet profiles, scoped identifier/device digests and controlled action IDs.
  Unknown external provider state follows the action's explicit policy.
- Core client HTTP rate limiting is enabled in every deployed profile with a
  60-second window and 100-request default maximum. Sensitive route manifests
  MUST declare stricter rules. IPv4-mapped IPv6 is normalized to IPv4; IPv6 is
  grouped by /64 unless explicitly overridden. PostgreSQL or Valkey owns
  atomic multi-instance counters; failure follows an explicit route policy and
  returns stable `Retry-After` when denied.
- CAPTCHA is required only after the risk policy requests it; adapters MUST
  enforce site/hostname/action binding. Protected signup/signin/reset actions
  fail closed or step up on CAPTCHA provider ambiguity according to configured
  availability policy; no adapter itself chooses allow.
- Delivery templates are versioned, locale-aware and context-escaped. Queue
  acceptance is not delivery. Reference tests use capture senders; production
  configuration MUST supply a real bounded Sender implementation through the
  documented Better-Auth-equivalent application seam.
- Locale precedence is explicit request, authenticated user/session, signed
  cookie, bounded `Accept-Language`, then `en`. Unsupported locales fall back to
  `en`; machine error codes and original error identity never change.

## HTTP limits and lifecycle

- Default request body limit is 1 MiB, header bytes 64 KiB, URL 8 KiB, JSON
  nesting 32 and collection elements 1,000 unless a stricter endpoint limit is
  declared. SCIM bulk and SAML have separate explicit lower/upper profiles.
- Server read-header, read, write and idle timeouts MUST be configured; no
  external operation derives an unbounded context.
- Trusted proxies, hosts, origins and externally visible base URL are explicit
  allowlists. Forwarded headers from untrusted peers are ignored, not merged.
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
