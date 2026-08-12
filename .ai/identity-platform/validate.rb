#!/usr/bin/env ruby
# frozen_string_literal: true

require "json"
require "digest"
require "open3"
require "rbconfig"
require "set"
require "time"
require "tmpdir"
require_relative "shared_contract_applicability"
require_relative "acceptance/schema_validation"
require_relative "acceptance/model"

ROOT = File.expand_path(__dir__)
REPOSITORY_ROOT = File.expand_path("../..", ROOT)
PRIMARY_WORKTREE_ROOT = begin
  common_dir, status = Open3.capture2(
    "git", "-C", REPOSITORY_ROOT, "rev-parse", "--path-format=absolute", "--git-common-dir"
  )
  status.success? ? File.dirname(File.realpath(common_dir.strip)) : REPOSITORY_ROOT
rescue Errno::ENOENT
  REPOSITORY_ROOT
end
EXPECTED_IDENTITY_UNITS = 61
EXPECTED_PRIMITIVE_EXTENSION_AUTHORITIES = 6
EXPECTED_PRIMITIVE_EXTENSION_UNITS = 6
EXPECTED_SCHEDULABLE_UNITS = EXPECTED_IDENTITY_UNITS + EXPECTED_PRIMITIVE_EXTENSION_UNITS
BASELINE = "b8077b74ef9a80a7757220b72834349bd8de05c0"
NORMATIVE_KEYWORD_PATTERN = /\b(?:MUST(?: NOT)?|REQUIRED|SHALL(?: NOT)?|SHOULD(?: NOT)?|RECOMMENDED|NOT RECOMMENDED|MAY|OPTIONAL)\b/
BCP14_NOTICE = <<~NOTICE.chomp.freeze
  The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
  "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
  "OPTIONAL" in this document are to be interpreted as described in BCP 14
  [RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
  shown here.
NOTICE
EXPECTED_UPSTREAM_CLOSURE_SHA256 = "88f43accab0304344209b4af592b16c785de42880d766a362e0d9e72538b7a37"
EXPECTED_UPSTREAM_LEAVES_SHA256 = "f0d20d17408ee143edf8614713641d2beb99159065c697b16ac505401111e2a9"
EXPECTED_PROGRAM_SEMANTIC_ROOT = "65cc7e397731cb49bb17763bda29f3db542cc03a7eda90f126a8f67db70b6058"
REQUIRED_ENVIRONMENT_INPUT_IDS = %w[
  environment:behavior-variables environment:external-profiles environment:go-toolchain
  environment:os-architecture environment:service-images
].freeze
NON_BEHAVIORAL_IDENTITY_PLATFORM_INPUTS = [
  ".ai/identity-platform/EXECUTION_LEDGER.md",
  ".ai/identity-platform/INVENTORY.md",
  ".ai/identity-platform/PREFLIGHT_EVIDENCE.md",
  ".ai/identity-platform/evidence/"
].freeze
EXPECTED_UPSTREAM_SOURCES = {
  "docs/content/docs/concepts" => ["tree", "9b98359e8415bc9a2f71617639cf2d61f15a0679", "Core documentation and route surface", "recursive_blobs"],
  "docs/content/docs/authentication" => ["tree", "b5132040e1543221912db02871153ed6ab57fc4c", "Core documentation and route surface", "recursive_blobs"],
  "docs/content/docs/plugins" => ["tree", "7f95f309b87e8737d2677df693a6763bfbf91907", "Official plugin documentation tree", "recursive_blobs"],
  "packages/better-auth/src/api/routes" => ["tree", "474bc1eab9bda2fa20ba6d1605a75a786c710447", "Core documentation and route surface", "recursive_blobs"],
  "packages/better-auth/src/plugins" => ["tree", "4e750e9a538dc8b75fe96dbfd7ba411a1deb3d1e", "Source-exported and internal plugin surface", "recursive_blobs"],
  "packages/better-auth/src/types/plugins.ts" => ["blob", "c3b7e77fdd268de9707e0b84feae92cc41b84dfb", "Source-exported and internal plugin surface", "exact_blob"],
  "packages/better-auth/src/utils/hide-metadata.ts" => ["blob", "a7ac60912fb14db1265e122b7e7daf0a59669bef", "Source-exported and internal plugin surface", "exact_blob"],
  "packages/core/src/social-providers" => ["tree", "39f9b83ca3681164e9eb8f8ef77f2ea5d5938e4c", "Provider catalog disposition", "recursive_blobs"],
  "packages" => ["tree", "2cc84b5f623da92e892bd3288243a8c3ec4a5110", "Official top-level packages", "immediate_children"]
}.freeze
ALLOWED_WORKER_PLACEHOLDERS = Set[
  "unit", "canonical-module", "absolute-worktree-path", "worker-branch",
  "integration-commit", "absolute-goal-path", "verified-prerequisite-list",
  "canonical-module-directory", "reserved-descendant-module-directories",
  "assignment-generation", "assignment-commit", "shared-contract-applicability"
].freeze
ALLOWED_STATUSES = Set[
  "proposed", "ready", "in-progress", "implemented-unverified", "verified",
  "blocked"
].freeze
EXISTING_OWNERS = Set[
  "audit", "authentication", "authentication/jwt", "authorization", "capability",
  "telemetry"
].freeze
HTTP_FEATURES = Set[
  "primitive/authorization-identity-contracts",
  "identity", "identity/session", "identity/delivery", "identity/risk",
  "identity/risk/captcha", "identity/password", "identity/username",
  "identity/email", "identity/magiclink", "identity/otp", "identity/phone",
  "identity/anonymous", "identity/mfa", "webauthn", "passkey",
  "identity/oauth", "identity/oauth/providers", "identity/oauth/onetap",
  "identity/oauth/proxy", "identity/apikey", "identity/impersonation",
  "organization", "sso", "sso/oidc", "sso/oauth2", "sso/saml",
  "sso/domain-verification", "scim",
  "scim/organization", "oauth-server", "oauth-server/oidc",
  "oauth-server/device", "identity/i18n"
].freeze
REFERENCE_ADAPTERS = Set[
  "identity/postgres", "identity/session/postgres", "identity/session/valkey",
  "identity/delivery/postgres", "identity/anonymous/postgres",
  "identity/risk/postgres", "identity/risk/valkey",
  "identity/risk/captcha/recaptcha", "identity/risk/captcha/turnstile",
  "identity/risk/captcha/hcaptcha", "identity/risk/captcha/captchafox",
  "identity/risk/hibp", "identity/password/postgres",
  "identity/otp/postgres", "identity/mfa/postgres", "passkey/postgres",
  "webauthn/postgres",
  "identity/oauth/postgres", "identity/apikey/postgres",
  "identity/apikey/valkey", "identity/impersonation/postgres",
  "organization/postgres", "sso/postgres", "scim/postgres",
  "oauth-server/postgres"
].freeze
COORDINATOR_ARTIFACTS = %w[
  END_STATE_ACCEPTANCE.json ACCEPTANCE_ARTIFACTS.json OPERATION_SEMANTICS.json PUBLIC_CONTRACTS.json PARITY_DISPOSITIONS.json
  VERIFICATION_APPLICABILITY.json
  API_OPERATIONS.md UPSTREAM_DISPOSITIONS.md UPSTREAM_SURFACE.json UPSTREAM_LEAVES.json PROTOCOL_BASELINES.md
  PROTOCOL_CONFORMANCE_MANIFEST.json CONFIGURATION_CATALOGS.json
  SECURITY_EVENTS.md TRANSACTION_CONTRACT.md LIFECYCLE_CASCADES.md
  LIFECYCLE_CONSUMERS.md REFERENCE_CONFIGURATION.md PREFLIGHT_EVIDENCE.md
].freeze
REQUIRED_ARTIFACT_SECTIONS = {
  "END_STATE_ACCEPTANCE.json" => [],
  "ACCEPTANCE_ARTIFACTS.json" => [],
  "OPERATION_SEMANTICS.json" => [],
  "PUBLIC_CONTRACTS.json" => [],
  "PARITY_DISPOSITIONS.json" => [],
  "VERIFICATION_APPLICABILITY.json" => [],
  "API_OPERATIONS.md" => [
    "Purpose and authority", "Contract notation",
    "Normative route and OpenAPI mapping",
    "Normative HTTP rate-policy catalog",
    "Core identity, password, and account operations",
    "Sessions, passwordless, phone, and MFA",
    "Passkeys, social OAuth, API keys, and administration",
    "Organizations, federation, and SCIM",
    "OAuth/OIDC authorization server and device operations",
    "Cross-cutting direct APIs and middleware", "Closure requirements"
  ],
  "UPSTREAM_DISPOSITIONS.md" => [
    "Baseline and closure rule", "Core documentation and route surface",
    "Official plugin documentation tree",
    "Source-exported and internal plugin surface", "Official top-level packages",
    "Provider catalog disposition", "Explicit divergences and exclusions", "Maintenance"
  ],
  "UPSTREAM_SURFACE.json" => [],
  "UPSTREAM_LEAVES.json" => [],
  "CONFIGURATION_CATALOGS.json" => [],
  "PROTOCOL_BASELINES.md" => [
    "Purpose and change control", "OAuth authorization and protected resources",
    "OpenID Connect", "SAML 2.0", "SCIM 2.0",
    "WebAuthn, passkeys, and authenticator data", "Mandatory conformance evidence"
  ],
  "PROTOCOL_CONFORMANCE_MANIFEST.json" => [],
  "SECURITY_EVENTS.md" => [
    "Ownership and purpose", "Mapping to `audit` canonical schema version 1",
    "Stable event taxonomy", "Actor, subject, and authorization invariants",
    "Atomicity, delivery, and reconciliation", "Privacy, integrity, and operations"
  ],
  "TRANSACTION_CONTRACT.md" => [
    "Scope and owners", "Command identity and fingerprint",
    "Durable command ledger and reservation state machine",
    "PostgreSQL unit of work", "Outbox, encrypted delivery, and provider effects",
    "One-time capability protocol", "Required proof"
  ],
  "LIFECYCLE_CASCADES.md" => [
    "Envelope, privacy, and schema evolution", "Exact event catalog",
    "Authority-version dimensions and owners", "Artifact-to-version applicability",
    "Destructive-state and invalidation matrix",
    "Cascade generation, acknowledgements, holds, and waivers",
    "Consumer obligations and proof"
  ],
  "LIFECYCLE_CONSUMERS.md" => [
    "Version and closure rule", "Exact consumer sets",
    "Checkpoint persistence ownership", "Consumer contract",
    "Validation and change control"
  ],
  "REFERENCE_CONFIGURATION.md" => [
    "Manifest rules", "Identity, organization, and administration",
    "Authentication and sessions", "WebAuthn and passkeys",
    "OAuth, OIDC, SSO, and API keys", "SCIM, risk, delivery, and HTTP",
    "PostgreSQL, Valkey, workers, and operations",
    "Required secret and connection fields", "Validation and proof"
  ],
  "PREFLIGHT_EVIDENCE.md" => [
    "Execution identity", "Worker assignment authorizations", "Worker runtime attestations",
    "Tool and environment lanes", "External evidence lanes",
    "Existing primitive contracts", "Task-owned resource registry",
    "Acceptance evidence bindings", "Goal digest revisions", "Conflict-recovery baselines",
    "Integrated-repair authorizations"
  ]
}.freeze
READING_ORDER = [
  "repository `AGENTS.md`", "`README.md`", "`PROGRAM.md`",
  "`COMMON_REQUIREMENTS.md`", "`END_STATE.md`", "`END_STATE_ACCEPTANCE.json`", "`ACCEPTANCE_ARTIFACTS.json`", "`REFERENCE_PROFILE.md`",
  "`BETTER_AUTH_PARITY.md`", "`PARITY_DISPOSITIONS.json`", "`API_OPERATIONS.md`", "`OPERATION_SEMANTICS.json`",
  "`PUBLIC_CONTRACTS.json`", "`public_contracts.rb`",
  "`UPSTREAM_DISPOSITIONS.md`", "`UPSTREAM_SURFACE.json`",
  "`PROTOCOL_BASELINES.md`", "`PROTOCOL_CONFORMANCE_MANIFEST.json`",
  "`SECURITY_EVENTS.md`", "`TRANSACTION_CONTRACT.md`",
  "`LIFECYCLE_CASCADES.md`", "`LIFECYCLE_CONSUMERS.md`",
  "`REFERENCE_CONFIGURATION.md`", "`CONFIGURATION_CATALOGS.json`", "`VERIFICATION_APPLICABILITY.json`",
  "`PREFLIGHT_EVIDENCE.md`", "`DEPENDENCIES.md`", "`INVENTORY.md`",
  "`EXECUTION_LEDGER.md`", "`WORKER_PROMPT.md`", "`GOAL_MANIFEST.json`",
  "the exact goal assigned to that worker."
].freeze

REQUIRED_PARITY_CAPABILITIES = Set[
  "Stateful, cached, and stateless sessions", "Custom session enrichment",
  "Typed additional fields", "OAuth popup", "Access-control composition",
  "Hooks and request extension", "Session bearer authentication",
  "JWT issuance and JWKS", "Schema and migration operations",
  "Dynamic base URL and trusted origins", "Safe instrumentation",
  "Pwned Passwords", "CAPTCHA", "Test utilities",
  "One-time session transfer", "Core HTTP rate limiting",
  "Typed extension modules", "Operational tooling",
  "Delivery callbacks and templates"
].freeze
PINNED_PLUGIN_EXPORTS = Set[
  "access", "admin", "anonymous", "bearer", "captcha", "custom-session",
  "device-authorization", "email-otp", "generic-oauth", "haveibeenpwned",
  "jwt", "last-login-method", "magic-link", "mcp", "multi-session",
  "oauth-popup", "oauth-proxy", "oidc-provider", "one-tap",
  "one-time-token", "open-api", "organization", "phone-number", "siwe",
  "test-utils", "two-factor", "username", "types/plugins and hide-metadata"
].freeze
REQUIRED_PROVIDER_NAMES = Set[
  "Apple", "Atlassian", "Amazon Cognito", "Discord", "Dropbox", "Facebook",
  "Figma", "GitHub", "GitLab", "Google", "Hugging Face", "Kakao", "Kick",
  "LINE", "Linear", "LinkedIn", "Microsoft", "Naver", "Notion", "Paybin",
  "PayPal", "Polar", "Railway", "Reddit", "Roblox", "Salesforce", "Slack",
  "Spotify", "TikTok", "Twitch", "Twitter/X", "Vercel", "VK", "WeChat",
  "Zoom", "Auth0", "Gumroad", "HubSpot", "Keycloak",
  "Microsoft Entra ID", "Okta", "Patreon", "Yandex"
].freeze
SCIM_VERIFIED_ERRATA = {
  "rfc-7643" => %w[5368 5606 5607 5990 5991 6004 6727 7522 8361 8415 8417 8435 8450 8471 8472 8475],
  "rfc-7644" => %w[6893 7898 7916 8096 8365]
}.freeze
OAUTH_SERVER_SCOPE_CATALOG = %w[email identity:read identity:write offline_access openid profile].freeze
OAUTH_PROTECTED_RESOURCE_SCOPES = %w[identity:read identity:write].freeze
OAUTH_DYNAMIC_REGISTRATION_SCOPES = OAUTH_SERVER_SCOPE_CATALOG
SAML_REDIRECT_SIGNATURE_ALGORITHM = "http://www.w3.org/2007/05/xmldsig-more#sha256-rsa-MGF1"
NORMATIVE_RFC_FILES = %w[
  PROGRAM.md COMMON_REQUIREMENTS.md END_STATE.md REFERENCE_PROFILE.md
  API_OPERATIONS.md PROTOCOL_BASELINES.md SECURITY_EVENTS.md
  TRANSACTION_CONTRACT.md LIFECYCLE_CASCADES.md LIFECYCLE_CONSUMERS.md
  REFERENCE_CONFIGURATION.md
].freeze
EXPECTED_CONFORMANCE_TOOLS = [
  {
    "id" => "openid-conformance-suite",
    "url" => "https://gitlab.com/openid/conformance-suite/-/archive/3f2bc78770e9ebdbb8165b6be86ae85b99bb2fc8/conformance-suite-3f2bc78770e9ebdbb8165b6be86ae85b99bb2fc8.tar.gz",
    "revision" => "3f2bc78770e9ebdbb8165b6be86ae85b99bb2fc8",
    "sha256" => "db61b212c6067a4f5a4258f8375840c7076bacca88d25adb102232a599d6074f",
    "license" => "Apache-2.0",
    "consumers" => %w[identity/oauth oauth-server oauth-server/oidc sso/oidc]
  },
  {
    "id" => "web-platform-tests",
    "url" => "https://codeload.github.com/web-platform-tests/wpt/tar.gz/9bc6e2404bff5349e48d7962b0a495582bc5ade8",
    "revision" => "9bc6e2404bff5349e48d7962b0a495582bc5ade8",
    "sha256" => "559c6d1edbff75dfbacb10a3466a36da8897dc3f586da1866a7b718f928a837a",
    "license" => "W3C 3-clause BSD",
    "consumers" => %w[passkey webauthn]
  },
  {
    "id" => "unboundid-scim2-sdk",
    "url" => "https://codeload.github.com/pingidentity/scim2/tar.gz/badd1eb5e8ee7ace3712a92f9d83891884f93189",
    "revision" => "badd1eb5e8ee7ace3712a92f9d83891884f93189",
    "sha256" => "116ba12596da10932da9c8cc6eb9fff8a646365e45925fbb73aeda5c967a6539",
    "license" => "Apache-2.0",
    "consumers" => %w[scim scim/organization scim/postgres]
  },
  {
    "id" => "simplesamlphp",
    "url" => "https://codeload.github.com/simplesamlphp/simplesamlphp/tar.gz/e049be1819327c76403fc0d6fa648d6dcfbc8516",
    "revision" => "e049be1819327c76403fc0d6fa648d6dcfbc8516",
    "sha256" => "ff7569639b54308fe44bb98f0f0b5fc84e540e32c2b3b4a586b3ba57c6914477",
    "license" => "LGPL-2.1-or-later",
    "consumers" => %w[sso/saml]
  }
].freeze
EXPECTED_PROTOCOL_SOURCE_IDENTITY_SHA256 = "e55db6f638681a26c2d8228876724a22176ff9a813aeeb33fdd8fb9495c8148b"
EXPECTED_PROTOCOL_SOURCE_CONSUMERS = {
  "oauth-form-post-response-mode-1.0-final" => %w[identity/oauth identity/oauth/providers oauth-server/oidc sso/oidc],
  "oauth-jarm-1.0-final" => %w[identity/oauth oauth-server/oidc sso/oidc],
  "oidc-frontchannel-logout-1.0-final" => %w[identity/oauth oauth-server/oidc sso/oidc],
  "oidc-backchannel-logout-1.0-final" => %w[identity/oauth oauth-server/oidc sso/oidc],
  "oauth-2.1-draft-15" => %w[identity/oauth identity/oauth/providers oauth-server sso/oauth2 sso/oidc],
  "rfc-2119" => :all_units,
  "rfc-3339" => %w[identity/reference],
  "rfc-4226" => %w[identity/mfa],
  "rfc-5280" => %w[webauthn],
  "rfc-5952" => %w[identity/http identity/reference identity/risk identity/risk/postgres identity/risk/valkey],
  "rfc-6238" => %w[identity/mfa identity/reference],
  "rfc-6585" => %w[scim],
  "rfc-6749" => %w[identity/oauth identity/oauth/providers oauth-server sso/oauth2 sso/oidc],
  "rfc-6750" => %w[identity/http identity/oauth identity/oauth/providers oauth-server oauth-server/oidc sso/oauth2 sso/oidc],
  "rfc-6901" => %w[identity/oauth/providers identity/reference],
  "rfc-7009" => %w[identity/oauth identity/oauth/providers oauth-server sso/oauth2 sso/oidc],
  "rfc-7239" => %w[identity/http identity/reference],
  "rfc-7515" => %w[identity/oauth identity/oauth/onetap oauth-server oauth-server/oidc sso/oidc],
  "rfc-7517" => %w[identity/oauth identity/oauth/onetap oauth-server oauth-server/oidc sso/oidc],
  "rfc-7518" => %w[identity/oauth identity/oauth/onetap oauth-server oauth-server/oidc sso/oidc],
  "rfc-7519" => %w[identity/oauth identity/oauth/onetap oauth-server oauth-server/oidc sso/oidc],
  "rfc-7591" => %w[oauth-server],
  "rfc-7592" => %w[oauth-server],
  "rfc-7636" => %w[identity/oauth identity/oauth/providers oauth-server sso/oauth2 sso/oidc],
  "rfc-7642" => %w[scim],
  "rfc-7643" => %w[scim scim/postgres],
  "rfc-7644" => %w[scim scim/postgres],
  "rfc-7662" => %w[identity/oauth identity/oauth/providers oauth-server],
  "rfc-8174" => :all_units,
  "rfc-8252" => %w[oauth-server],
  "rfc-8414" => %w[identity/oauth/providers oauth-server],
  "rfc-8628" => %w[oauth-server/device],
  "rfc-8693" => %w[oauth-server oauth-server/oidc],
  "rfc-8707" => %w[oauth-server],
  "rfc-8949" => %w[identity identity/anonymous identity/anonymous/postgres identity/apikey identity/apikey/postgres identity/apikey/valkey identity/delivery identity/delivery/postgres identity/email identity/impersonation identity/impersonation/postgres identity/magiclink identity/mfa identity/mfa/postgres identity/oauth identity/oauth/postgres identity/otp identity/otp/postgres identity/password identity/password/postgres identity/phone identity/postgres identity/reference identity/risk identity/risk/captcha identity/risk/hibp identity/risk/postgres identity/risk/valkey identity/session identity/session/postgres identity/session/valkey oauth-server oauth-server/device oauth-server/oidc oauth-server/postgres organization organization/postgres passkey passkey/postgres scim scim/organization scim/postgres sso sso/domain-verification sso/oauth2 sso/oidc sso/postgres sso/saml webauthn webauthn/postgres],
  "rfc-9052" => %w[webauthn],
  "rfc-9053" => %w[webauthn],
  "rfc-9207" => %w[identity/oauth identity/oauth/providers oauth-server sso/oauth2 sso/oidc],
  "rfc-9700" => %w[identity/oauth identity/oauth/providers oauth-server sso/oauth2 sso/oidc],
  "rfc-9728" => %w[identity/http oauth-server],
  "oidc-core-1.0-errata-2" => %w[identity/oauth identity/oauth/onetap identity/oauth/providers oauth-server/oidc sso/oidc],
  "oidc-discovery-1.0-errata-2" => %w[identity/oauth identity/oauth/providers oauth-server/oidc sso/oidc],
  "oidc-multiple-response-types-1.0" => %w[identity/oauth identity/oauth/providers oauth-server/oidc sso/oidc],
  "oidc-rp-initiated-logout-1.0" => %w[identity/oauth identity/oauth/providers oauth-server/oidc sso/oidc],
  "saml-core-2.0-os" => %w[sso/saml],
  "saml-bindings-2.0-os" => %w[sso/saml],
  "saml-profiles-2.0-os" => %w[sso/saml],
  "saml-metadata-2.0-os" => %w[sso/saml],
  "saml-security-2.0-os" => %w[sso/saml],
  "saml-errata-05" => %w[sso/saml],
  "xml-signature-1.1-20130411" => %w[sso/saml],
  "exclusive-xml-c14n-20020718" => %w[sso/saml],
  "webauthn-level-3-cr-20260526" => %w[passkey passkey/postgres webauthn webauthn/postgres],
  "fido-metadata-service-3.1.1-20260105" => %w[webauthn],
  "fido-metadata-statement-3.1.1-20260105" => %w[webauthn],
  "public-suffix-list-e1b8015c" => %w[sso/domain-verification webauthn],
  "google-authenticator-key-uri-8ba6e793" => %w[identity/mfa]
}.freeze
ADMINISTRATION_JOURNEY_TRANSITIONS = {
  "identity.platform.role.create" => [
    "absent -> role version 1 with validated statement bindings",
    "duplicate ID or unknown statement -> conflict; state unchanged"
  ],
  "identity.platform.role.update" => [
    "role version N -> N+1 with replacement statement bindings",
    "stale expected role version -> conflict; state unchanged"
  ],
  "identity.platform.role.delete" => [
    "role version N -> absent after assignment disposition",
    "stale expected role version or in-use without explicit bounded assignment removal/reassignment -> conflict; state unchanged"
  ],
  "identity.platform.permission-statement.create" => [
    "absent -> permission statement version 1",
    "duplicate ID or invalid typed statement -> conflict; state unchanged"
  ],
  "identity.platform.permission-statement.update" => [
    "permission statement version N -> N+1 with affected roles identified",
    "stale expected statement version -> conflict; state unchanged"
  ],
  "identity.platform.permission-statement.delete" => [
    "permission statement version N -> absent after role disposition",
    "stale expected statement version or in-use without explicit bounded role detachment/replacement -> conflict; state unchanged"
  ]
}.freeze
ADMINISTRATION_AUTHORITY_TRANSITION = "authority version A -> A+1; prior-version decisions rejected; cached decisions invalidated; audit/outbox commit atomically"
AUDIT_RETENTION_JOURNEY_TRANSITIONS = {
  "identity.audit-retention.policy.update" => [
    "retention policy version N -> N+1 at the declared effective boundary",
    "stale expected policy version -> conflict; state unchanged",
    "identity.audit_retention.change_policy"
  ],
  "identity.audit-retention.legal-hold.create" => [
    "absent -> active legal hold version 1",
    "duplicate hold ID or invalid scope -> conflict; state unchanged",
    "identity.audit_retention.create_legal_hold"
  ],
  "identity.audit-retention.legal-hold.update" => [
    "active legal hold version N -> N+1",
    "stale expected hold version -> conflict; state unchanged",
    "identity.audit_retention.update_legal_hold"
  ],
  "identity.audit-retention.legal-hold.release" => [
    "active legal hold version N -> released version N+1",
    "stale expected hold version or already released by another command -> conflict; state unchanged",
    "identity.audit_retention.release_legal_hold"
  ],
  "identity.audit-retention.records.delete" => [
    "eligible planned records -> deleted with protected receipt and checkpoint",
    "stale plan/policy/hold checkpoint, newly held record or ineligible record -> abort batch; records unchanged",
    "identity.audit_retention.delete_records"
  ]
}.freeze
PHONE_RECOVERY_JOURNEY_TRANSITIONS = {
  "identity.phone.password-reset-request" => [
    "enabled recovery + pre-auth transaction + canonical number + recovery purpose + fresh one-use RiskEvidence -> purpose-bound phone OTP challenge and canonical reset capability; no session or remember choice",
    "disabled recovery, missing/expired pre-auth transaction, raw caller carrier facts, or positive/unknown/unavailable risk decision -> enumeration-safe denial; no capability or OTP issued",
    "identity.risk decides and issues exact-bound evidence -> identity.phone atomically reserves, applies and finalizes initiation-only RiskEvidence with OTP challenge and reset capability issuance; completion requires a separate completion-only artifact"
  ],
  "identity.phone.password-reset-complete" => [
    "matching reset capability + reserved purpose-bound phone OTP + eligible independent factor + fresh one-use RiskEvidence -> credential reset and session revocation; no session or remember choice",
    "stale/mismatched/replayed/in-progress RiskEvidence, wrong exact binding, missing/invalid OTP, or missing independent factor -> denial; credentials and sessions unchanged",
    "identity.phone uses one coordinator unit of work to reserve identity.risk/postgres evidence, OTP and capability, then atomically finalize them with the password mutation, session invalidation and command result; unknown remains reserved for authoritative recovery"
  ]
}.freeze
AUDIT_RETENTION_API_CONTRACTS = {
  "identity.audit-retention.policy.update" => ["both", "fresh privacy admin / CSRF", "keyed", "identity.audit_retention.change_policy"],
  "identity.audit-retention.legal-hold.create" => ["both", "fresh privacy admin / CSRF", "keyed", "identity.audit_retention.create_legal_hold"],
  "identity.audit-retention.legal-hold.update" => ["both", "fresh privacy admin / CSRF", "keyed", "identity.audit_retention.update_legal_hold"],
  "identity.audit-retention.legal-hold.release" => ["both", "fresh privacy admin / CSRF", "repeat", "identity.audit_retention.release_legal_hold"],
  "identity.audit-retention.records.delete" => ["direct", "offline retention operator plus immutable plan confirmation / internal", "keyed", "identity.audit_retention.delete_records"]
}.freeze
AUDIT_RETENTION_AUTHORITY_REQUIREMENTS = {
  "configuration" => [
    "`struct:ref.audit.retention_policy`",
    "`initialization = insert_once_from_bootstrap_defaults`",
    "`runtime_authority = durable_policy_version`",
    "`restart = load_durable_never_overwrite`",
    "`update_operation = identity.audit-retention.policy.update`",
    "`initialization_event = identity.audit_retention.change_policy`",
    "`unset = unsupported`", "`reset = unsupported`",
    "`plan_binding = policy_version_and_hold_checkpoint`",
    "startup bootstrap default only"
  ],
  "security_events" => [
    "bootstrap defaults only", "durable runtime retention policy is the sole runtime authority",
    "initialized exactly once", "restart MUST NOT overwrite",
    "unset and reset are unsupported", "version-1 initialization emits exactly",
    "identity.audit_retention.change_policy"
  ],
  "reference_goal" => [
    "durable runtime retention policy", "initialized exactly once from the startup bootstrap defaults",
    "restart MUST load the durable version and MUST NOT overwrite it",
    "unset/reset are unsupported", "version-1 initialization MUST emit",
    "identity.audit-retention.policy.update"
  ],
  "api_operations" => [
    "mutates the durable `audit/postgres` runtime policy",
    "unset/reset values are rejected"
  ],
  "end_state" => [
    "initializes durable policy version 1 exactly once from startup bootstrap defaults",
    "restart loads that durable version without overwrite",
    "configuration drift does not reset runtime policy", "bootstrap initialization event"
  ]
}.freeze

def fail_check(message)
  warn "identity-platform validation: #{message}"
  exit 1
end

def canonical_json_value(value)
  case value
  when Hash
    value.keys.sort_by(&:b).to_h { |key| [key, canonical_json_value(value.fetch(key))] }
  when Array
    value.map { |item| canonical_json_value(item) }
  else
    value
  end
end

def semantic_row_digest(row)
  payload = row.reject { |key, _value| key == "semantic_digest" }
  Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(payload)))
end

def end_state_acceptance_section(row, end_state)
  if row.key?("number")
    number = row.fetch("number")
    end_state[/^#{number}\. \*\*.*?(?=^#{number + 1}\. \*\*|^## HTTP and browser-facing contract)/m].to_s
  else
    headings = {
      "cross.http-browser.v1" => "HTTP and browser-facing contract",
      "cross.persistence-recovery.v1" => "Persistence and failure recovery",
      "cross.security-privacy.v1" => "Security and privacy properties",
      "cross.operability-provider-proof.v1" => "Operability and provider proof",
      "cross.final-acceptance.v1" => "Final acceptance gates"
    }
    heading = headings.fetch(row.fetch("id"))
    end_state[/^## #{Regexp.escape(heading)}\n.*?(?=^## |\z)/m].to_s
  end
end

def end_state_semantic_digest(row, end_state)
  section = end_state_acceptance_section(row, end_state)
  return nil if section.empty?
  payload = JSON.generate(canonical_json_value(row.reject { |key, _| key == "semantic_digest" }))
  Digest::SHA256.hexdigest("#{payload}\n#{section}")
end

END_STATE_ACCEPTANCE_IDENTITIES = {
  "journeys" => [
    "journey.identity-lifecycle.v1", "journey.email-password-username.v1", "journey.sessions.v1",
    "journey.passwordless.v1", "journey.mfa-webauthn.v1", "journey.social-oauth.v1",
    "journey.api-keys.v1", "journey.administration.v1", "journey.organizations.v1",
    "journey.enterprise-federation.v1", "journey.scim.v1", "journey.oauth-oidc-provider.v1",
    "journey.abuse-controls.v1", "journey.localization.v1", "journey.developer-use.v1",
    "journey.extension-access-policy.v1", "journey.typed-modules-operations.v1", "journey.privacy-export.v1",
    "journey.audit-investigation.v1"
  ],
  "cross_cutting" => %w[
    cross.http-browser.v1 cross.persistence-recovery.v1 cross.security-privacy.v1
    cross.operability-provider-proof.v1 cross.final-acceptance.v1
  ]
}.freeze

PUBLIC_CONTRACT_OPERATION_CLOSURE_RULE =
  "The validator MUST require every operation declared by API_OPERATIONS.md to have an authoritative OPERATION_SEMANTICS.json row and MUST reject fallback transport, authorization, rate-policy, idempotency, or route derivation."

def end_state_journey_count_errors(document, count)
  words = {19 => "nineteen"}
  expected = "all #{words.fetch(count, count.to_s)} journeys"
  document.include?(expected) ? [] : ["end-state journey count prose drifted"]
end

def program_journey_count_errors(document, count)
  expected = "closes all #{count} journeys"
  document.include?(expected) ? [] : ["program journey count prose drifted"]
end

def public_contract_operation_closure_rule_errors(document)
  rules = document.fetch("validation_rules", [])
  matches = rules.grep(/authoritative OPERATION_SEMANTICS\.json row/)
  errors = []
  errors << "public-contract operation closure rule drifted" unless matches == [PUBLIC_CONTRACT_OPERATION_CLOSURE_RULE]
  errors << "public-contract operation closure rule hard-codes a stale count" if rules.any? { |rule| rule.match?(/\ball \d+ operations\b/) }
  errors
end

def end_state_acceptance_identity_errors(document)
  END_STATE_ACCEPTANCE_IDENTITIES.filter_map do |collection, expected_ids|
    rows = document[collection]
    actual = rows.is_a?(Array) ? rows.map { |row| row["id"] } : nil
    "end-state #{collection} semantic identity drifted" unless actual == expected_ids
  end
end

REQUIRED_ACCEPTANCE_OPERATION_EVIDENCE = {
  "operational-api-redaction-report" => %w[identity.audit.export identity.audit.get identity.audit.list identity.audit.search],
  "reference-http-password-journey" => %w[identity.email.address-get identity.email.address-list identity.email.address-remove identity.phone.password-signin],
  "mfa-recovery-report" => %w[identity.admin.mfa-recovery-issue identity.admin.mfa-reset],
  "api-key-http-lifecycle-report" => %w[identity.apikey.session-authenticate]
}.freeze

def required_acceptance_evidence_errors(document)
  errors = []
  audit_journey = document.fetch("journeys", []).find { |row| row["id"] == "journey.audit-investigation.v1" }
  errors << "audit-investigation journey artifact drifted" unless audit_journey&.fetch("number") == 19 && audit_journey.fetch("artifacts") == ["operational-api-redaction-report"]
  catalog = document.fetch("artifact_catalog", [])
  REQUIRED_ACCEPTANCE_OPERATION_EVIDENCE.each do |artifact_id, required_operations|
    row = catalog.find { |artifact| artifact["id"] == artifact_id }
    errors << "#{artifact_id} omits required operation evidence" unless row && (required_operations - row.fetch("operation_claims", [])).empty?
  end
  errors
end

def authoritative_artifact_semantic_digest(row, artifact_documents)
  artifact_path = row.fetch("artifact").split("#", 2).first
  artifact = artifact_documents[artifact_path]
  return nil unless artifact

  payload = JSON.generate(canonical_json_value(row.reject { |key, _| key == "semantic_digest" }))
  Digest::SHA256.hexdigest("#{payload}\n#{artifact}")
end

def program_semantic_root(goal_manifest:, acceptance:, acceptance_catalog:, acceptance_model:, acceptance_check:,
                          acceptance_schema_validation:, acceptance_schemas:, operations:, public_contracts:,
                          public_contracts_helper:, shared_applicability:, shared_applicability_helper:, parity:,
                          verification:, configuration:, protocol:)
  payload = {
    "goal_digests" => goal_manifest.fetch("goals").map { |row| {"unit" => row.fetch("unit"), "sha256" => row.fetch("sha256")} },
    "acceptance" => acceptance, "operations" => operations, "parity" => parity,
    "acceptance_contracts" => {
      "catalog_sha256" => Digest::SHA256.hexdigest(acceptance_catalog),
      "model_sha256" => Digest::SHA256.hexdigest(acceptance_model),
      "check_sha256" => Digest::SHA256.hexdigest(acceptance_check),
      "schema_validation_sha256" => Digest::SHA256.hexdigest(acceptance_schema_validation),
      "schemas" => acceptance_schemas.transform_values { |source| Digest::SHA256.hexdigest(source) }
    },
    "verification" => verification, "configuration" => configuration,
    "protocol" => protocol,
    "public_contracts_sha256" => Digest::SHA256.hexdigest(public_contracts),
    "public_contracts_helper_sha256" => Digest::SHA256.hexdigest(public_contracts_helper),
    "shared_applicability" => shared_applicability,
    "shared_applicability_helper_sha256" => Digest::SHA256.hexdigest(shared_applicability_helper)
  }
  Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(payload)))
end

def public_contract_source_digest_errors(document, root:)
  errors = []
  source_digests = document["source_digests"]
  return ["public contracts source digests are absent"] unless source_digests.is_a?(Hash)

  paths = {
    "API_OPERATIONS.md" => "API_OPERATIONS.md",
    "GOAL_MANIFEST.json" => "GOAL_MANIFEST.json",
    "OPERATION_SEMANTICS.json" => "OPERATION_SEMANTICS.json",
    "PARITY_DISPOSITIONS.json" => "PARITY_DISPOSITIONS.json",
    "public_contracts.rb" => "public_contracts.rb",
    "public_contracts_meta.json" => "fragments/public_contracts_meta.json"
  }
  paths.each do |key, relative|
    absolute = File.join(root, relative)
    expected = File.file?(absolute) ? "sha256:#{Digest::SHA256.file(absolute).hexdigest}" : nil
    errors << "public contracts source digest drifted for #{key}" unless source_digests[key] == expected
  end
  fragment_digests = source_digests["fragments"]
  unless fragment_digests.is_a?(Hash)
    errors << "public contracts fragment source digests are absent"
    return errors
  end
  expected_fragments = Dir[File.join(root, "fragments", "public_contracts_*.json")]
    .reject { |path| File.basename(path) == "public_contracts_meta.json" }
    .sort_by { |path| File.basename(path).b }
    .to_h { |path| [File.basename(path), "sha256:#{Digest::SHA256.file(path).hexdigest}"] }
  errors << "public contracts fragment source digest closure drifted" unless fragment_digests == expected_fragments
  errors
end

def public_contract_goal_binding_errors(document, program_rows, goal_bodies)
  errors = []
  units = document.fetch("units", [])
  operations = document.fetch("operations", [])
  types = document.fetch("types", [])
  unit_ids = units.map { |row| row["contract_id"] }
  operation_ids = operations.map { |row| row["contract_id"] }
  unit_names = units.map { |row| row["unit"] }
  operation_names = operations.map { |row| row["id"] }
  expected_unit_names = program_rows.reject { |row| row[:unit].start_with?("primitive/") }.map { |row| row[:unit] }
  unit_ids_valid = units.all? { |row| row["contract_id"] == "contract:unit:#{row['unit']}:v1" }
  operation_ids_valid = operations.all? { |row| row["contract_id"] == "contract:operation:#{row['id']}:v1" }
  errors << "public unit contract IDs are not exact and unique or unit rows are not canonical" unless unit_ids_valid && unit_ids.uniq.length == unit_ids.length && unit_names == expected_unit_names
  errors << "public operation contract IDs are not exact and unique or operation rows are not canonical" unless operation_ids_valid && operation_ids.uniq.length == operation_ids.length && operation_names == operation_names.sort_by(&:b)

  types_by_id = types.to_h { |row| [row["id"], row] }
  operations.each do |operation|
    method = operation.fetch("method", {})
    interface = types_by_id[method["interface"]]
    unless interface && interface["category"] == "interface"
      errors << "public operation interface is unresolved for #{operation['id']}"
      next
    end
    exact_method = interface.dig("definition", "methods").to_a.find do |candidate|
      candidate["name"] == method["name"] && candidate["signature"] == method["signature"] &&
        candidate["operation_semantics"] == operation["semantics"]
    end
    errors << "public operation method is absent from its interface for #{operation['id']}" unless exact_method
    [operation["request_type"], operation["result_type"], *operation.fetch("error_types", [])].each do |type_id|
      errors << "public operation schema is unresolved for #{operation['id']}: #{type_id}" unless types_by_id.key?(type_id)
    end
  end

  units_by_name = units.to_h { |row| [row["unit"], row] }
  extension_authorities = document.dig("manifest_schema", "required_primitive_extensions").to_a.group_by { |entry| entry.fetch("extension_unit") }
  bound_operation_ids = []
  program_rows.each do |row|
    unit_contract = units_by_name[row[:unit]]
    owned_authorities = extension_authorities.fetch(row[:unit], []).map { |entry| entry.fetch("unit") }
    expected = []
    expected << unit_contract["contract_id"] if unit_contract
    expected.concat(operations.select { |operation| operation["owner"] == row[:unit] || owned_authorities.include?(operation["owner"]) }.map { |operation| operation["contract_id"] })
    actual = metadata_values(goal_bodies.fetch(row[:unit], ""), "Public contracts")
    errors << "public contract goal IDs drifted for #{row[:unit]}" unless actual == expected
    bound_operation_ids.concat(expected.grep(/\Acontract:operation:/))
  end
  errors << "public operation goal bindings are incomplete or duplicated" unless bound_operation_ids.sort_by(&:b) == operation_ids.sort_by(&:b)
  errors
end

def public_contract_symbol_occurs?(value, symbol)
  case value
  when Hash
    value.any? { |key, nested| public_contract_symbol_occurs?(key, symbol) || public_contract_symbol_occurs?(nested, symbol) }
  when Array
    value.any? { |nested| public_contract_symbol_occurs?(nested, symbol) }
  when String
    value.match?(/(?<![A-Za-z0-9_])#{Regexp.escape(symbol)}(?![A-Za-z0-9_])/)
  else
    false
  end
end

def derived_primitive_consumers(public_contracts, requirement)
  symbols = requirement.fetch("required_symbols")
  patterns = symbols.map { |symbol| /(?<![A-Za-z0-9_])#{Regexp.escape(symbol)}(?![A-Za-z0-9_])/ }
  occurs = ->(value) do
    text = JSON.generate(value)
    patterns.any? { |pattern| text.match?(pattern) }
  end
  public_units = public_contracts.fetch("units").map { |row| row.fetch("unit") }.to_set
  consumers = Set.new
  symbol_contract_ids = Set.new
  public_contracts.fetch("units").each do |unit|
    consumers << unit.fetch("unit") if occurs.call(unit)
  end
  public_contracts.fetch("types").each do |contract|
    owner = contract["owner"]
    if occurs.call(contract)
      consumers << owner if public_units.include?(owner)
      symbol_contract_ids << contract.fetch("id")
    end
  end
  public_contracts.fetch("operations").each do |operation|
    referenced_contract_ids = [operation["request_type"], operation["result_type"], *operation.fetch("error_types", [])]
    next unless occurs.call(operation) || referenced_contract_ids.any? { |id| symbol_contract_ids.include?(id) }

    operation.fetch("declared_owners").each do |owner|
      consumers << owner if public_units.include?(owner)
    end
  end
  consumers.to_a.sort_by(&:b)
end

def evidence_revision_reuse_errors(tested_revision:, gate_execution_revision:, revalidation_revision:, input_manifest:, input_root:, module_roots: [], repository_root: nil)
  errors = []
  revision_pattern = /\A[0-9a-f]{40}\z/
  errors << "tested revision is invalid" unless tested_revision.to_s.match?(revision_pattern)
  errors << "gate execution revision is invalid" unless gate_execution_revision.to_s.match?(revision_pattern)
  manifest_root = input_manifest.is_a?(Array) ? "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(input_manifest)))}" : nil
  errors << "input root drifted" unless manifest_root && input_root == manifest_root

  if tested_revision == gate_execution_revision
    errors << "revalidation revision is present without reuse" unless revalidation_revision.nil?
  else
    errors << "revalidation revision is missing or invalid" unless revalidation_revision.to_s.match?(revision_pattern)
    errors << "revalidation revision does not identify the reused gate revision" unless revalidation_revision == gate_execution_revision
  end

  if repository_root && tested_revision.to_s.match?(revision_pattern) && gate_execution_revision.to_s.match?(revision_pattern)
    behavior_input_manifest_errors(input_manifest, revision: tested_revision, module_roots: module_roots, repository: repository_root).each do |error|
      errors << "tested revision #{error}"
    end
    if gate_execution_revision != tested_revision
      behavior_input_manifest_errors(input_manifest, revision: gate_execution_revision, module_roots: module_roots, repository: repository_root).each do |error|
        errors << "revalidated gate revision #{error}"
      end
    end
  end
  errors
end

def acceptance_evidence_errors(document, declaration:, revision:, input_fingerprint:, evidence_commit: nil, module_roots: [], repository_root: nil, record_path: nil, artifact_payloads: nil, live_capture: nil, final_execution: false)
  errors = []
  expected_keys = %w[schema_version artifact_id result tested_revision gate_execution_revision revalidation_revision input_manifest input_root tool_environment observations artifact_hashes recorded_at]
  errors << "schema fields drifted" unless document.keys == expected_keys
  errors << "schema version drifted" unless document["schema_version"] == 2
  errors << "artifact ID drifted" unless document["artifact_id"] == declaration["id"]
  claims = (declaration.fetch("claims") + declaration.fetch("operation_claims", [])).sort_by(&:b)
  observation_ids = declaration.fetch("observation_ids")
  errors << "pass result payload drifted" unless document["result"] == {"status" => "pass", "gate" => declaration["gate"]}
  errors << "tested revision is stale" unless document["tested_revision"] == revision && revision&.match?(/\A[0-9a-f]{40}\z/)
  evidence_revision_reuse_errors(
    tested_revision: document["tested_revision"], gate_execution_revision: document["gate_execution_revision"],
    revalidation_revision: document["revalidation_revision"], input_manifest: document["input_manifest"],
    input_root: document["input_root"], module_roots: module_roots, repository_root: repository_root
  ).each do |error|
    errors << error
  end
  errors << "input root is stale" unless document["input_root"] == input_fingerprint
  tool_environment = document["tool_environment"]
  errors << "tool/environment identity is missing" unless tool_environment.is_a?(Hash) && tool_environment.keys == %w[tool environment] && tool_environment.values.all? { |value| value.is_a?(String) && !value.empty? }
  artifacts_valid = document["artifact_hashes"].is_a?(Array) && document["artifact_hashes"].any? && document["artifact_hashes"].all? do |artifact|
    next false unless artifact.is_a?(Hash) && artifact.keys == %w[path sha256] && artifact["path"].match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z}) && !artifact["path"].include?("..")
    next true unless repository_root
    absolute = File.expand_path(artifact["path"], repository_root)
    committed = git_blob_bytes(evidence_commit, artifact["path"], repository: repository_root)
    absolute.start_with?(repository_root + File::SEPARATOR) && File.file?(absolute) && committed &&
      File.binread(absolute) == committed && artifact["sha256"] == "sha256:#{Digest::SHA256.hexdigest(committed)}"
  end
  errors << "artifact hashes are absent or invalid" unless artifacts_valid
  artifact_digests = document["artifact_hashes"].is_a?(Array) ? document["artifact_hashes"].filter_map { |artifact| artifact["sha256"] if artifact.is_a?(Hash) } : []
  observations = document["observations"]
  observation_fields = %w[observation_id claim_id contract_reference scenario preconditions stimulus expected_outcome actual_outcome result artifact_sha256]
  observations_valid = observations.is_a?(Array) && observations.length == claims.length && observations.each_with_index.all? do |observation, index|
    claim = claims[index]
    observation.is_a?(Hash) && observation.keys == observation_fields &&
      observation["observation_id"] == observation_ids.fetch(index) && observation["claim_id"] == claim &&
      observation["contract_reference"] == declaration.fetch("schema") &&
      %w[scenario preconditions stimulus expected_outcome actual_outcome].all? { |field| observation[field].is_a?(String) && !observation[field].empty? } &&
      observation["result"] == "pass" && observation["actual_outcome"] == observation["expected_outcome"] &&
      artifact_digests.include?(observation["artifact_sha256"])
  end
  errors << "artifact-specific behavioral observations drifted" unless observations_valid
  if repository_root || artifact_payloads
    acceptance_catalog = JSON.parse(File.read(File.join(ROOT, "ACCEPTANCE_ARTIFACTS.json")))
    artifact_contract = acceptance_catalog.fetch("artifacts").find { |entry| entry.fetch("artifact_id") == declaration.fetch("id") }
    if artifact_contract
      exact_path = artifact_contract.fetch("artifact_evidence_output_path")
      bound = document.fetch("artifact_hashes", []).find { |entry| entry.is_a?(Hash) && entry["path"] == exact_path }
      bytes = artifact_payloads&.fetch(exact_path, nil)
      bytes ||= git_blob_bytes(evidence_commit, exact_path, repository: repository_root) if repository_root
      if !bound || !bytes
        errors << "artifact-specific payload binding is absent"
      else
        digest = "sha256:#{Digest::SHA256.hexdigest(bytes)}"
        errors << "artifact-specific payload hash drifted" unless bound["sha256"] == digest
        begin
          payload = JSON.parse(bytes)
          schema_path = File.join(REPOSITORY_ROOT, artifact_contract.fetch("schema_path"))
          acceptance_schema = JSON.parse(File.read(schema_path))
          record_schema_errors = AcceptanceSchemaValidation.errors(document, acceptance_schema)
          errors << "acceptance record schema drifted: #{record_schema_errors.first}" unless record_schema_errors.empty?
          evidence_schema = acceptance_schema.dig("$defs", "artifact_evidence")
          payload_errors = AcceptanceSchemaValidation.errors(payload, evidence_schema)
          errors << "artifact-specific payload schema drifted: #{payload_errors.first}" unless payload_errors.empty?
          execution = payload["execution"]
          if execution.is_a?(Hash)
            errors << "artifact execution revision is not record-bound" unless execution["tested_revision"] == document["tested_revision"]
            errors << "artifact execution input root is not record-bound" unless execution["input_root"] == document["input_root"]
            errors << "artifact execution tool is not record-bound" unless execution["tool"] == document.dig("tool_environment", "tool")
            errors << "artifact execution environment is not record-bound" unless execution["environment"] == document.dig("tool_environment", "environment")
            errors << "artifact execution output path is not record-bound" unless execution["output_artifact_path"] == exact_path
            receipt_path = execution["execution_receipt_path"]
            receipt_bound = document.fetch("artifact_hashes", []).find { |entry| entry.is_a?(Hash) && entry["path"] == receipt_path }
            receipt_bytes = artifact_payloads&.fetch(receipt_path, nil)
            receipt_bytes ||= git_blob_bytes(evidence_commit, receipt_path, repository: repository_root) if repository_root && receipt_path.is_a?(String)
            errors << "separate execution receipt binding is absent" unless receipt_bound && receipt_bytes
            if receipt_bound && receipt_bytes
              receipt_digest = "sha256:#{Digest::SHA256.hexdigest(receipt_bytes)}"
              errors << "separate execution receipt hash drifted" unless receipt_bound["sha256"] == receipt_digest
              errors.concat(AcceptanceSchemaValidation.execution_receipt_errors(execution, receipt_bytes))
            end
            errors.concat(AcceptanceExecutionRunner.live_capture_errors(execution, live_capture, artifact_bytes: bytes)) if final_execution
          else
            errors << "artifact execution proof is absent"
          end
        rescue JSON::ParserError => e
          errors << "artifact-specific payload is invalid JSON: #{e.message}"
        end
      end
    else
      errors << "artifact-specific acceptance contract is absent"
    end
  end
  errors << "recorded_at is invalid" unless rfc3339?(document["recorded_at"].to_s)
  if repository_root && evidence_commit&.match?(/\A[0-9a-f]{40}\z/)
    errors << "evidence commit does not exist" unless git_commit_exists?(evidence_commit)
    errors << "evidence commit is not integrated" unless git_ancestor?(evidence_commit, "HEAD")
    gate_revision = document["gate_execution_revision"]
    errors << "gate execution revision does not exist" unless git_commit_exists?(gate_revision)
    errors << "evidence commit does not descend from gate execution revision" unless git_commit_exists?(gate_revision) && git_ancestor?(gate_revision, evidence_commit)
  elsif repository_root
    errors << "evidence commit binding is invalid"
  end
  if repository_root && record_path
    committed_record = git_blob_bytes("HEAD", record_path, repository: repository_root)
    absolute_record = File.expand_path(record_path, repository_root)
    errors << "evidence record is not committed exactly" unless committed_record && File.file?(absolute_record) && File.binread(absolute_record) == committed_record
  end
  errors
end

def local_gate_evidence_errors(gate, unit:, revision:, fingerprint:, module_roots:, repository_root:, record_path: nil, evidence_commit: nil)
  errors = []
  keys = %w[schema_version schema unit tested_revision gate_execution_revision revalidation_revision input_manifest input_root evidence_record outcome commands artifacts tool_identity environment_identity record_digest]
  errors << "schema drifted" unless gate.keys == keys && gate["schema_version"] == 2 && gate["schema"] == "identity-platform.local-gate.v2"
  errors << "unit drifted" unless gate["unit"] == unit
  errors << "gate execution revision drifted" unless gate["gate_execution_revision"] == revision
  evidence_revision_reuse_errors(
    tested_revision: gate["tested_revision"], gate_execution_revision: gate["gate_execution_revision"],
    revalidation_revision: gate["revalidation_revision"], input_manifest: gate["input_manifest"],
    input_root: gate["input_root"], module_roots: module_roots, repository_root: repository_root
  ).each { |error| errors << error }
  errors << "fingerprint drifted" unless gate["input_root"] == fingerprint
  evidence_record = gate["evidence_record"]
  record_path_value = evidence_record["path"] if evidence_record.is_a?(Hash) && evidence_record.keys == ["path"]
  errors << "evidence record binding drifted" unless record_path_value&.match?(%r{\A\.ai/identity-platform/evidence/gates/[a-zA-Z0-9._/-]+\.json\z}) && !record_path_value.include?("..")
  errors << "evidence record path drifted" if record_path && record_path_value != record_path
  errors << "outcome did not pass" unless gate["outcome"] == "pass"
  errors << "commands missing" unless gate["commands"].is_a?(Array) && gate["commands"].any? && gate["commands"].all? { |command| command.is_a?(String) && !command.empty? }
  artifact_revision = evidence_commit || gate["tested_revision"]
  valid_artifacts = gate["artifacts"].is_a?(Array) && gate["artifacts"].any? && gate["artifacts"].all? do |artifact|
    next false unless artifact.is_a?(Hash) && artifact.keys == %w[path sha256]
    relative = artifact["path"]
    next false unless relative.is_a?(String) && relative.match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z}) && !relative.include?("..") &&
      artifact["sha256"].is_a?(String) && artifact["sha256"].match?(/\Asha256:[0-9a-f]{64}\z/)
    next true unless repository_root
    absolute = File.expand_path(relative, repository_root)
    committed = git_blob_bytes(artifact_revision, relative, repository: repository_root)
    absolute.start_with?(repository_root + File::SEPARATOR) && File.file?(absolute) && committed &&
      File.binread(absolute) == committed && artifact["sha256"] == "sha256:#{Digest::SHA256.hexdigest(committed)}"
  end
  errors << "artifact digests invalid" unless valid_artifacts
  errors << "tool/environment identity missing" unless [gate["tool_identity"], gate["environment_identity"]].all? { |identity| identity.is_a?(String) && !identity.empty? }
  if record_path
    committed_record = git_blob_bytes(evidence_commit, record_path, repository: repository_root)
    absolute_record = File.expand_path(record_path, repository_root)
    expected_record = JSON.pretty_generate(gate) + "\n"
    errors << "gate evidence commit is invalid" unless evidence_commit&.match?(/\A[0-9a-f]{40}\z/) && git_commit_exists?(evidence_commit)
    errors << "gate evidence commit is not integrated" unless evidence_commit&.match?(/\A[0-9a-f]{40}\z/) && git_ancestor?(evidence_commit, "HEAD")
    errors << "gate evidence commit excludes gate execution revision" unless evidence_commit&.match?(/\A[0-9a-f]{40}\z/) &&
      git_commit_exists?(revision) && git_ancestor?(revision, evidence_commit)
    errors << "gate record is not committed exactly" unless committed_record && File.file?(absolute_record) &&
      File.binread(absolute_record) == committed_record && committed_record == expected_record
  end
  digest = Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(gate.reject { |key, _| key == "record_digest" })))
  errors << "record digest drifted" unless gate["record_digest"] == "sha256:#{digest}"
  errors
end

def local_gate_binding_errors(binding, ledger_entry:, repository_root:)
  errors = []
  expected_path = ".ai/identity-platform/evidence/gates/#{ledger_entry[:unit].tr('/', '-')}.json"
  errors << "binding identity drifted" unless binding.values_at(:unit, :generation, :gate_revision) ==
    [ledger_entry[:unit], ledger_entry[:generation], ledger_entry[:gate_revision]]
  errors << "binding path drifted" unless binding[:path] == expected_path
  errors << "binding commit is unreachable" unless binding[:commit].to_s.match?(/\A[0-9a-f]{40}\z/) &&
    git_commit_exists?(binding[:commit]) && git_ancestor?(binding[:commit], "HEAD")
  errors << "gate execution revision is unreachable" unless git_commit_exists?(ledger_entry[:gate_revision])
  errors << "binding commit excludes gate execution revision" unless git_commit_exists?(ledger_entry[:gate_revision]) &&
    git_commit_exists?(binding[:commit]) && git_ancestor?(ledger_entry[:gate_revision], binding[:commit])
  committed = git_blob_bytes(binding[:commit], binding[:path], repository: repository_root)
  absolute = File.expand_path(binding[:path], repository_root)
  exact = committed && absolute.start_with?(repository_root + File::SEPARATOR) && File.file?(absolute) &&
    File.binread(absolute) == committed && binding[:digest] == "sha256:#{Digest::SHA256.hexdigest(committed)}"
  errors << "binding does not prove exact local gate bytes" unless exact
  errors << "binding timestamp is invalid" unless rfc3339?(binding[:bound_at].to_s)
  errors
end

def recovery_lifecycle_errors(history)
  statuses = history.map { |row| row.fetch("status") }
  errors = []
  active_epoch = false
  previous = nil
  statuses.each_with_index do |status, index|
    case status
    when "authorized"
      errors << "duplicates authorization in one epoch" if active_epoch
      errors << "later authorization does not follow a terminal epoch" if index.positive? && !%w[superseded completed].include?(previous)
      active_epoch = true
    when "superseded"
      errors << "superseded lacks active authorization epoch" unless active_epoch
      active_epoch = false
    when "completed"
      errors << "completed lacks active authorization epoch" unless active_epoch
      active_epoch = false
    else
      errors << "has invalid status #{status}"
    end
    previous = status
  end
  errors << "lacks initial authorization" unless statuses.first == "authorized"
  errors
end

def recovery_epoch_identity_errors(history)
  errors = []
  authorization = nil
  history.each do |row|
    status = row.fetch("status")
    if status == "authorized"
      authorization = row
    elsif %w[superseded completed].include?(status)
      errors << "terminal row drifted from its authorization epoch" unless authorization && row.fetch("identity") == authorization.fetch("identity")
      authorization = nil if status == "superseded" || status == "completed"
    end
  end
  errors
end

def recovery_transition_errors(previous_rows, current_rows)
  errors = []
  errors << "recovery history is not append-only" unless current_rows.first(previous_rows.length) == previous_rows
  current_rows.drop(previous_rows.length).each do |row|
    next if row[5] == "authorized"

    identity = row.values_at(7, 0, 1, 2, 3)
    preceding_authorization = previous_rows.any? do |candidate|
      candidate[5] == "authorized" && candidate.values_at(7, 0, 1, 2, 3) == identity
    end
    errors << "recovery terminal lacks a preceding committed exact authorization" unless preceding_authorization
  end
  errors
end

def reserved_nested_roots(assigned_root, inventory_roots, modules_document, packages_document)
  registered = inventory_roots + modules_document.fetch("modules", []).filter_map { |row| row["directory"] } +
    packages_document.fetch("packages", []).filter_map { |row| row["module_directory"] }
  registered.select { |root| root != assigned_root && root.start_with?("#{assigned_root}/") }.sort_by(&:b).uniq
end

def current_verified_gate_root_errors(recorded_manifest, recorded_root, current_manifest)
  errors = []
  errors << "recorded gate root drifted" unless recorded_root ==
    "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(recorded_manifest)))}"
  current_root = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(current_manifest)))}"
  errors << "verified gate root is stale at current integration HEAD; demote the unit and its changed-fingerprint verified reverse dependants to implemented-unverified in one transition before revalidation" unless
    recorded_manifest == current_manifest && recorded_root == current_root
  errors
end

def worker_runtime_attestation_errors(attestation, unit:, generation:, task:)
  errors = []
  errors << "runtime attestation identity drifted" unless attestation.values_at(:unit, :generation, :task) == [unit, generation, task]
  errors << "runtime attestation agent ID is not immutable" unless attestation[:agent_id].to_s.match?(/\A[a-zA-Z0-9._:\/-]+\z/) && attestation[:agent_id] != "—"
  errors << "runtime attestation model drifted" unless attestation[:model] == "gpt-5.6-sol"
  errors << "runtime attestation reasoning drifted" unless attestation[:reasoning] == "medium"
  errors << "runtime attestation fork policy drifted" unless attestation[:fork_turns] == "none"
  errors << "runtime attestation subagent policy drifted" unless attestation[:subagents] == "false"
  errors << "runtime attestation is not platform-reported" unless attestation[:source] == "platform-spawn-result"
  errors << "runtime attestation timestamp is invalid" unless attestation[:recorded_at].to_s.match?(/\A\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\z/)
  errors
end

reserved_root_fixture = reserved_nested_roots(
  "pkg/identity",
  ["pkg/identity", "pkg/identity/session"],
  {"modules" => [{"directory" => "pkg/identity/private-module"}]},
  {"packages" => [{"module_directory" => "pkg/identity/plugin", "directory" => "package"}]}
)
unless reserved_root_fixture == %w[pkg/identity/plugin pkg/identity/private-module pkg/identity/session]
  fail_check("root-manifest reserved nested-root fixture was not enforced")
end

current_root_fixture = [{"path_or_environment_id" => "pkg/identity/file.go", "kind" => "blob", "content_identity" => "git-sha1:#{'0' * 40}", "owner" => "pkg/identity", "reason" => "behavior-affecting repository input"}]
current_root_fingerprint = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(current_root_fixture)))}"
unless current_verified_gate_root_errors(current_root_fixture, current_root_fingerprint, current_root_fixture).empty?
  fail_check("current verified gate-root fixture was rejected")
end
stale_root_fixture = Marshal.load(Marshal.dump(current_root_fixture))
stale_root_fixture[0]["content_identity"] = "git-sha1:#{'1' * 40}"
if current_verified_gate_root_errors(current_root_fixture, current_root_fingerprint, stale_root_fixture).empty?
  fail_check("stale verified gate-root negative fixture was accepted")
end

runtime_fixture = {
  unit: "identity", generation: "1", task: "identity-worker", agent_id: "agent-immutable-1",
  model: "gpt-5.6-sol", reasoning: "medium", fork_turns: "none", subagents: "false",
  source: "platform-spawn-result", recorded_at: "2026-08-12T00:00:00Z"
}
unless worker_runtime_attestation_errors(runtime_fixture, unit: "identity", generation: "1", task: "identity-worker").empty?
  fail_check("worker runtime-attestation fixture was rejected")
end
forged_runtime_fixture = runtime_fixture.merge(agent_id: "—")
if worker_runtime_attestation_errors(forged_runtime_fixture, unit: "identity", generation: "1", task: "identity-worker").empty?
  fail_check("worker runtime-attestation immutable-agent negative fixture was accepted")
end

def goal_revision_lifecycle_errors(history)
  errors = []
  history.group_by { |row| row.fetch(:revision_id) }.each_value do |rows|
    statuses = rows.map { |row| row.fetch(:status) }
    errors << "goal revision lacks preceding authorization" unless statuses.first == "authorized"
    errors << "goal revision lifecycle drifted" unless statuses.length.between?(1, 2) &&
      (statuses == ["authorized"] || [%w[authorized applied], %w[authorized superseded]].include?(statuses))
    identity = rows.first.values_at(:unit, :previous_digest, :current_digest)
    errors << "goal revision terminal identity drifted" unless rows.all? { |row| row.values_at(:unit, :previous_digest, :current_digest) == identity }
    errors << "goal revision authorization drifted" unless rows.all? { |row| row[:authorized_by] == "coordinator" }
    errors << "goal revision does not change a digest" unless identity[1] != identity[2]
  end
  history.select { |row| row[:status] == "authorized" }.group_by { |row| row[:unit] }.each do |unit, rows|
    expected = (1..rows.length).to_a
    actual = rows.map { |row| row[:revision_id][/:g(\d+)\z/, 1]&.to_i }
    errors << "goal revision sequence drifted for #{unit}" unless actual == expected
  end
  errors
end

def goal_digest_change_errors(previous_manifest, current_manifest, previous_revisions, current_revisions)
  errors = []
  previous_by_unit = previous_manifest.fetch("goals").to_h { |row| [row.fetch("unit"), row.fetch("sha256")] }
  current_by_unit = current_manifest.fetch("goals").to_h { |row| [row.fetch("unit"), row.fetch("sha256")] }
  changed = current_by_unit.keys.select { |unit| previous_by_unit[unit] != current_by_unit[unit] }
  new_rows = current_revisions.drop(previous_revisions.length)
  new_rows.reject { |row| row[:status] == "authorized" }.each do |row|
    preceding_authorization = previous_revisions.any? do |candidate|
      candidate[:status] == "authorized" &&
        candidate.values_at(:revision_id, :unit, :previous_digest, :current_digest) ==
          row.values_at(:revision_id, :unit, :previous_digest, :current_digest)
    end
    errors << "goal revision terminal lacks a preceding committed exact authorization" unless preceding_authorization
  end
  changed.each do |unit|
    previous_digest = "sha256:#{previous_by_unit[unit]}"
    current_digest = "sha256:#{current_by_unit[unit]}"
    authorization = previous_revisions.find do |row|
      row[:unit] == unit && row[:previous_digest] == previous_digest &&
        row[:current_digest] == current_digest && row[:status] == "authorized"
    end
    applied = new_rows.find do |row|
      authorization && row[:revision_id] == authorization[:revision_id] && row[:unit] == unit &&
        row[:previous_digest] == previous_digest && row[:current_digest] == current_digest && row[:status] == "applied"
    end
    errors << "goal digest change for #{unit} lacks prior authorized revision lifecycle" unless authorization && applied
  end
  new_rows.select { |row| row[:status] == "applied" }.each do |row|
    errors << "goal revision applied without a matching digest change" unless changed.include?(row[:unit])
  end
  errors
end

def parity_closure_errors(document, configuration)
  errors = []
  native = {"artifact" => "CONFIGURATION_CATALOGS.json#native_token_modes", "closed_default" => configuration.dig("native_token_modes", "default")}
  captcha = {"artifact" => "CONFIGURATION_CATALOGS.json#captcha.owners", "required_count" => configuration.dig("captcha", "owners").length}
  errors << "parity native-token closure drifted" unless document["provider_native_token_modes"] == native
  errors << "parity CAPTCHA-owner closure drifted" unless document["captcha_owners"] == captcha
  errors
end

def username_verification_errors(selectors, goal_body: nil)
  errors = %w[fuzz benchmark].filter_map do |selector|
    "identity/username required #{selector} verification applicability drifted" unless selectors.dig(selector, "status") == "required"
  end
  if goal_body && !(goal_body.include?("Property/fuzz tests are\nREQUIRED") && goal_body.include?("race, benchmark"))
    errors << "identity/username goal no longer requires property/fuzz and benchmark evidence"
  end
  errors
end

GOAL_VERIFICATION_REQUIREMENT_PATTERNS = {
  "benchmark" => /(?:\bbenchmarks?\b[^.]{0,180}\b(?:MUST|REQUIRED)\b|\b(?:MUST|REQUIRED)\b[^.]{0,180}\bbenchmarks?\b)/,
  "leak" => /(?:\b(?:leak tests?|leak gates?|race\/stress\/leak)\b[^.]{0,180}\b(?:MUST|REQUIRED)\b|\b(?:MUST|REQUIRED)\b[^.]{0,180}\b(?:leak tests?|leak gates?|race\/stress\/leak)\b)/
}.freeze

def goal_verification_selector_errors(unit:, selectors:, goal_body:)
  normalized = goal_body.split.join(" ")
  GOAL_VERIFICATION_REQUIREMENT_PATTERNS.filter_map do |selector, pattern|
    next unless normalized.match?(pattern)
    "#{unit} goal requires #{selector} verification" unless selectors.dig(selector, "status") == "required"
  end
end

def eligible_frontier_rows(rows, start_gate_blocked_units = Set.new)
  rows.select do |row|
    row[:status] == "proposed" && !start_gate_blocked_units.include?(row[:unit]) &&
      row[:requires].all? { |required| rows.find { |candidate| candidate[:unit] == required }[:status] == "verified" }
  end
end

def semantic_manifest_errors(document, kind:, collections:, known_owners:, digest_resolver: method(:semantic_row_digest), schema_version: 1)
  errors = []
  errors << "#{kind} schema version drifted" unless document["schema_version"] == schema_version
  errors << "#{kind} digest algorithm drifted" unless document["digest_algorithm"] == "sha256"
  expected_rule = "sha256 of RFC 8785-style canonical row JSON excluding semantic_digest, LF, then exact authoritative artifact bytes selected by source_anchor or artifact"
  errors << "#{kind} digest input drifted" unless document["digest_input"] == expected_rule
  ids = []
  collections.each do |name|
    rows = document[name]
    unless rows.is_a?(Array) && !rows.empty?
      errors << "#{kind} #{name} must be nonempty"
      next
    end
    rows.each do |row|
      unless row.is_a?(Hash) && row["id"].is_a?(String) && row["id"].match?(/\A[a-z0-9][a-z0-9.-]+\.v\d+\z/)
        errors << "#{kind} #{name} has invalid stable ID"
        next
      end
      ids << row.fetch("id")
      errors << "#{kind} #{row['id']} has unknown owner" unless known_owners.include?(row["owner"])
      artifacts = row["artifacts"] || [row["artifact"]].compact
      errors << "#{kind} #{row['id']} lacks acceptance/source artifacts" unless artifacts.is_a?(Array) && artifacts.any? && artifacts.all? { |item| item.is_a?(String) && !item.empty? }
      errors << "#{kind} #{row['id']} semantic digest drifted" unless row["semantic_digest"] == digest_resolver.call(row)
    end
  end
  errors << "#{kind} stable IDs are duplicated" unless ids.uniq == ids
  errors
end

def operation_semantics_errors(document, contracts)
  errors = []
  errors << "operation semantics schema drifted" unless document.keys == %w[schema_version authority closed_fields operations]
  errors << "operation semantics version drifted" unless document["schema_version"] == 2
  errors << "operation semantics authority drifted" unless document["authority"] == "API_OPERATIONS.md#complete-operation-catalog"
  required_fields = %w[owners exposure access authorization csrf_origin risk_class rate_policy idempotency event_semantics http_method http_path openapi_operation_id]
  errors << "operation semantics closed fields drifted" unless document["closed_fields"] == required_fields
  rows = document["operations"]
  unless rows.is_a?(Array)
    return errors << "operation semantics operations must be an array"
  end
  ids = rows.map { |row| row["id"] }
  errors << "operation semantics IDs are not sorted and unique" unless ids == ids.sort_by(&:b).uniq
  errors << "operation semantics operation closure drifted" unless ids == contracts.keys.sort_by(&:b)
  rows.each do |row|
    expected = contracts[row["id"]]
    unless expected
      errors << "operation semantics has unknown operation #{row['id']}"
      next
    end
    explicit_authorization = row["authorization"]
    errors << "operation authorization is not explicit for #{row['id']}" unless explicit_authorization.is_a?(String) && !explicit_authorization.empty? && explicit_authorization != row["access"]
    errors << "operation authorization incorrectly absent for #{row['id']}" if explicit_authorization == "none" && row["access"] != "public"
    expected_row = expected.merge("authorization" => explicit_authorization)
    unless expected_row == row
      drifted_fields = (expected_row.keys | row.keys).select { |field| expected_row[field] != row[field] }
      errors << "operation semantics drifted for #{row['id']}: #{drifted_fields.join(', ')}"
    end
  end
  errors
end

def normative_notice_error(path, body)
  return unless body.match?(NORMATIVE_KEYWORD_PATTERN)
  return if body.scan(BCP14_NOTICE).length == 1

  "#{path} must contain exactly one RFC 8174 section 2 BCP 14 notice"
end

def validate_normative_markdown_notices!
  Dir[File.join(ROOT, "**", "*.md")].sort.each do |path|
    error = normative_notice_error(path.delete_prefix("#{ROOT}/"), File.read(path))
    fail_check(error) if error
  end

  negative_fixtures = {
    "missing RFC 8174 reference" => BCP14_NOTICE.sub("[RFC2119] [RFC8174]", "[RFC2119]"),
    "duplicate notice" => "#{BCP14_NOTICE}\n\n#{BCP14_NOTICE}"
  }
  negative_fixtures.each do |label, invalid_notice|
    fixture = "# Invalid normative document\n\n#{invalid_notice}\n\nAn implementation MUST fail closed.\n"
    unless normative_notice_error("negative-fixture.md", fixture)
      fail_check("normative-notice negative fixture #{label} was not rejected")
    end
  end
end

def validate_administration_journey!(document)
  section = document[/^8\. \*\*Administration:\*\*(.*?)(?=^9\. \*\*)/m, 1].to_s
  fail_check("end-state administration journey is missing") if section.empty?
  normalized_section = section.gsub(/\s+/, " ")
  required_composition = [
    "reference `net/http` handlers", "public service contracts", "without an admin UI",
    "captured before each successful mutation", "denied after the authority-version increment"
  ]
  missing_composition = required_composition.reject { |required| normalized_section.include?(required) }
  unless missing_composition.empty?
    fail_check("end-state administration journey lacks composed proof: #{missing_composition.join(', ')}")
  end

  rows = section.lines.filter_map do |line|
    stripped = line.strip
    next unless stripped.start_with?("| `identity.platform.")

    cells = stripped.split("|").map(&:strip)
    fail_check("end-state administration journey row has wrong column count: #{line.chomp}") unless cells.length == 5
    operation_id = cells[1][/\A`([^`]+)`\z/, 1]
    [operation_id, cells[2], cells[3], cells[4]]
  end
  operation_ids = rows.map(&:first)
  expected_ids = ADMINISTRATION_JOURNEY_TRANSITIONS.keys
  fail_check("end-state administration operations drifted") unless operation_ids == expected_ids
  fail_check("end-state administration operations contain duplicates") unless operation_ids.uniq == operation_ids
  rows.each do |operation_id, success, rejection, authority|
    expected_success, expected_rejection = ADMINISTRATION_JOURNEY_TRANSITIONS.fetch(operation_id)
    fail_check("end-state administration success transition drifted for #{operation_id}") unless success == expected_success
    fail_check("end-state administration rejection transition drifted for #{operation_id}") unless rejection == expected_rejection
    fail_check("end-state administration authority transition drifted for #{operation_id}") unless authority == ADMINISTRATION_AUTHORITY_TRANSITION
  end

  audit_composition = [
    "four authenticated reference `net/http` operations", "direct-only record-deletion public service contract",
    "without an admin UI", "direct deletion operation MUST remain absent from HTTP and OpenAPI",
    "active hold prevents deletion", "newly confirmed plan"
  ]
  missing_audit_composition = audit_composition.reject { |required| normalized_section.include?(required) }
  unless missing_audit_composition.empty?
    fail_check("end-state audit-retention journey lacks composed proof: #{missing_audit_composition.join(', ')}")
  end
  audit_rows = section.lines.filter_map do |line|
    stripped = line.strip
    next unless stripped.start_with?("| `identity.audit-retention.")

    cells = stripped.split("|").map(&:strip)
    fail_check("end-state audit-retention journey row has wrong column count: #{line.chomp}") unless cells.length == 5
    operation_id = cells[1][/\A`([^`]+)`\z/, 1]
    event_id = cells[4][/\A`([^`]+)`\z/, 1]
    [operation_id, cells[2], cells[3], event_id]
  end
  audit_operation_ids = audit_rows.map(&:first)
  expected_audit_ids = AUDIT_RETENTION_JOURNEY_TRANSITIONS.keys
  fail_check("end-state audit-retention operations drifted") unless audit_operation_ids == expected_audit_ids
  fail_check("end-state audit-retention operations contain duplicates") unless audit_operation_ids.uniq == audit_operation_ids
  audit_rows.each do |operation_id, success, rejection, event_id|
    expected_success, expected_rejection, expected_event = AUDIT_RETENTION_JOURNEY_TRANSITIONS.fetch(operation_id)
    fail_check("end-state audit-retention success transition drifted for #{operation_id}") unless success == expected_success
    fail_check("end-state audit-retention rejection transition drifted for #{operation_id}") unless rejection == expected_rejection
    fail_check("end-state audit-retention event drifted for #{operation_id}") unless event_id == expected_event
  end
  rows + audit_rows
end

def phone_recovery_journey_errors(document)
  errors = []
  section = document[/^4\. \*\*Passwordless:\*\*(.*?)(?=^5\. \*\*)/m, 1].to_s
  return ["end-state phone recovery journey is missing"] if section.empty?

  normalized = section.gsub(/\s+/, " ")
  required_composition = [
    "reference `net/http` handlers and public service contracts",
    "`identity/risk` -> `identity/phone` seam",
    "fresh one-use immutable `RiskEvidence`",
    "tenant, subject, recovery operation, recovery purpose, canonical number, pre-auth transaction, attempt ID and risk-policy version",
    "Both operations issue no session and carry no remember choice"
  ]
  missing = required_composition.reject { |required| normalized.include?(required) }
  errors << "end-state phone recovery journey lacks composed proof: #{missing.join(', ')}" unless missing.empty?

  rows = section.lines.filter_map do |line|
    stripped = line.strip
    next unless stripped.start_with?("| `identity.phone.password-reset-")

    cells = stripped.split("|").map(&:strip)
    unless cells.length == 5
      errors << "end-state phone recovery journey row has wrong column count: #{line.chomp}"
      next
    end
    operation_id = cells[1][/\A`([^`]+)`\z/, 1]
    [operation_id, cells[2], cells[3], cells[4]]
  end
  operation_ids = rows.map(&:first)
  expected_ids = PHONE_RECOVERY_JOURNEY_TRANSITIONS.keys
  errors << "end-state phone recovery operations drifted" unless operation_ids == expected_ids
  errors << "end-state phone recovery operations contain duplicates" unless operation_ids.uniq == operation_ids
  rows.each do |operation_id, success, rejection, seam|
    next unless PHONE_RECOVERY_JOURNEY_TRANSITIONS.key?(operation_id)

    expected_success, expected_rejection, expected_seam = PHONE_RECOVERY_JOURNEY_TRANSITIONS.fetch(operation_id)
    errors << "end-state phone recovery success transition drifted for #{operation_id}" unless success == expected_success
    errors << "end-state phone recovery rejection transition drifted for #{operation_id}" unless rejection == expected_rejection
    errors << "end-state phone recovery seam transition drifted for #{operation_id}" unless seam == expected_seam
  end
  errors
end

def validate_phone_recovery_journey!(document)
  errors = phone_recovery_journey_errors(document)
  fail_check(errors.first) unless errors.empty?
  PHONE_RECOVERY_JOURNEY_TRANSITIONS.keys
end

def expect_phone_recovery_journey_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("phone recovery journey negative fixture #{label} was accepted") if errors.empty?
  unless errors.include?(expected_error)
    fail_check("phone recovery journey negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}")
  end
end

def validate_audit_retention_authority!(documents)
  unless documents.keys == AUDIT_RETENTION_AUTHORITY_REQUIREMENTS.keys
    fail_check("audit-retention authority fixture keys drifted")
  end
  AUDIT_RETENTION_AUTHORITY_REQUIREMENTS.each do |name, requirements|
    normalized = documents.fetch(name).gsub(/\s+/, " ")
    missing = requirements.reject { |required| normalized.include?(required) }
    unless missing.empty?
      fail_check("audit-retention authority #{name} contract drifted: #{missing.join(', ')}")
    end
  end
  true
end

def validate_configuration_rows!(document)
  rows = document.lines.filter_map do |line|
    next unless line.start_with?("| `")

    cells = line.split("|").map(&:strip)
    fail_check("reference configuration row has wrong column count: #{line.chomp}") unless cells.length == 5
    fail_check("reference configuration row is incomplete: #{line.chomp}") if cells.values_at(1, 2, 3).any?(&:empty?)
    path = cells[1][/\A`([^`]+)`\z/, 1]
    configuration_metadata(path, cells[2], cells[3], line.chomp)
  end
  fail_check("reference configuration contains invalid paths") if rows.any? { |row| row.fetch("row_id") == "ref." }
  row_ids = rows.map { |row| row.fetch("row_id") }
  fail_check("reference configuration paths are not unique") unless row_ids.uniq == row_ids
  missing_struct_targets = document.scan(/struct:ref\.[a-z0-9_.-]+/).uniq.filter_map do |target|
    target_row_id = "ref.#{target}"
    target_row_id unless row_ids.include?(target_row_id)
  end
  unless missing_struct_targets.empty?
    fail_check("reference configuration has dangling struct targets: #{missing_struct_targets.join(', ')}")
  end
  rows
end

def configuration_metadata(path, reference_value, semantics, source)
  fail_check("reference configuration contains invalid path: #{source}") unless path&.match?(/\A[a-z0-9_<>.:-]+\z/)
  text = "#{reference_value} #{semantics}"
  required = reference_value.include?("REQUIRED")
  secret = path.start_with?("secrets.") || path.end_with?(".client_secret") || path == "captcha.<id>.secret" || %w[postgres.dsn delivery.sender saml.sp_signing_key saml.sp_decryption_key].include?(path)
  type = if path.start_with?("struct:ref.")
           path
         elsif (struct_id = text[/struct:(ref\.[a-z0-9_.-]+)/, 1])
           "struct:#{struct_id}"
         elsif reference_value.match?(/\A`?(?:true|false)`?(?:\s|\z)/)
           "bool"
         elsif text.match?(/\bhandle\b/)
           "handle:#{path.split('.').last}"
         elsif secret
           "bytes:opaque"
         elsif path.end_with?("_bytes", ".max_bytes") || (reference_value.match?(/\A\d/) && semantics.match?(/\bbytes?\b/i))
           "uint64:bytes"
         elsif reference_value.include?("/duration")
           "rate:uint64-per-duration"
         elsif reference_value.match?(/\A\d+\.\d+\z/) && !text.match?(/\b(?:seconds?|minutes?|hours?|days?|bytes?)\b/)
           "decimal:fixed6"
         elsif reference_value.match?(/\A\d[\d,]*(?:\.\d+)?\s+(?:milliseconds?|seconds?|minutes?|hours?|days?|years?)\b/)
           "duration:nanoseconds"
         elsif reference_value.start_with?("https://")
           "url:absolute-https"
         elsif reference_value.start_with?("/")
           "path:absolute"
         elsif required
           noun = text[/\b(URL|URI|string|list|handle|identifier|owner)\b/, 1]
           fail_check("reference configuration REQUIRED row is not typeable: #{source}") unless noun
           {
             "URL" => "url:absolute-https", "URI" => "uri:absolute", "string" => "string:utf8",
             "list" => "list:string", "handle" => "handle:#{path.split('.').last}",
             "identifier" => "string:utf8", "owner" => "string:utf8"
           }.fetch(noun)
         elsif text.match?(/\b(?:list|allowlist|set)\b/i) || reference_value.include?(",")
           semantics.match?(/\bCIDR\b/i) ? "list:cidr" : semantics.match?(/\borigins?\b/i) ? "list:origin" : "list:string"
         elsif reference_value.match?(/\A\d/) && semantics.match?(/codepoints?/i)
           "uint64:codepoints"
         elsif reference_value.match?(/\A\d/)
           "uint64:count"
         else
           "enum:string"
         end
  bounds = semantics.match(/(?<minimum>[^;|]+?)\.\.(?<maximum>[^;|]+)/)
  numeric_type = type.start_with?("uint64:", "decimal:", "duration:", "rate:")
  bounded_by_rule = semantics.match?(/\b(?:exact|exactly|fixed|maximum|minimum|zero disables|MUST NOT exceed)\b/i)
  if numeric_type && !bounds && !bounded_by_rule
    fail_check("reference configuration numerical row lacks minimum/maximum metadata: #{source}")
  end
  enum_clause = semantics[/\benum\s+(.+?)(?:;|\z)/i, 1]
  enum_values = if type == "enum:string"
                  ([reference_value.delete("`")] + enum_clause.to_s.scan(/`([^`]+)`/).flatten).reject(&:empty?).uniq
                else
                  []
                end
  metadata = {
    "row_id" => "ref.#{path}", "type" => type, "default" => required ? nil : reference_value,
    "minimum" => bounds&.[](:minimum), "maximum" => bounds&.[](:maximum), "enum" => enum_values,
    "required_if" => required ? (reference_value[/\bwhen\s+(.+)\z/, 1] || "always") : "false",
    "secret" => secret, "reload_class" => text.match?(/\b(?:reloadable|atomic reload)\b/i) ? "atomic" : "startup-only",
    "validation_code" => "identity.reference.#{path.tr('.', '/')}/invalid",
    "fingerprint_group" => "identity-reference-v1/#{path.split('.').first}"
  }
  expected_keys = %w[row_id type default minimum maximum enum required_if secret reload_class validation_code fingerprint_group]
  fail_check("reference configuration metadata is incomplete for #{path}") unless metadata.keys == expected_keys
  fail_check("reference configuration enum row lacks a closed set: #{source}") if type == "enum:string" && enum_values.empty?
  metadata
end

def configuration_row(document, path)
  document.lines.find { |line| line.start_with?("| `#{path}` |") }.to_s
end

def configuration_reference_value(document, path)
  configuration_row(document, path).split("|").map(&:strip)[2].to_s
end

def identity_policy_set_errors(document)
  row = configuration_row(document, "struct:ref.identity.policy_set")
  member_cell = row.split("|").map(&:strip)[2].to_s
  expected_members = {
    "Authorize" => "authorization.Service",
    "AssessRisk" => "func(context.Context, RiskPolicyInput) (RiskPolicyDecision, error)",
    "MapClaims" => "func(context.Context, ClaimsPolicyInput) (ClaimsPolicyDecision, error)",
    "DecideRetention" => "func(context.Context, RetentionPolicyInput) (RetentionPolicyDecision, error)",
    "Redact" => "func(context.Context, RedactionPolicyInput) (RedactionPolicyDecision, error)",
    "nil_member" => "invalid",
    "side_effects" => "forbidden",
    "stores_transactions_routing_continuation" => "forbidden"
  }
  member_entries = member_cell.split(";")
  actual_members = member_entries.to_h do |entry|
    name, value = entry.delete("`").strip.split(/\s*=\s*/, 2)
    [name, value]
  end
  return [] if member_entries.length == expected_members.length && actual_members.length == member_entries.length && actual_members == expected_members

  ["identity PolicySet exact exported members drifted"]
end

def closed_list_value(document, path)
  value = configuration_reference_value(document, path)
  match = value.match(/\A`([^`]+)`\z/)
  match ? match[1].split(",") : []
end

def protocol_semantic_errors(configuration:, protocol:, conformance:, oauth_goal:, saml_goal:, api_operations:, reference_profile:, applicability:, public_contracts:)
  errors = []
  verified_errata = conformance["verified_errata"]
  errors << "SCIM verified errata manifest drifted" unless verified_errata == SCIM_VERIFIED_ERRATA
  SCIM_VERIFIED_ERRATA.each do |rfc, ids|
    baseline = "#{rfc.upcase.sub('-', ' ')} Verified errata IDs: #{ids.join(', ')}."
    errors << "SCIM verified errata baseline drifted for #{rfc}" unless protocol.include?(baseline)
  end

  scope_catalog = closed_list_value(configuration, "oauth_server.scopes")
  protected_scopes = closed_list_value(configuration, "oauth_server.protected_resource.supported_scopes")
  registration_scopes = closed_list_value(configuration, "oauth_server.dynamic_registration.allowed_scopes")
  errors << "OAuth authorization-server scope catalog drifted" unless scope_catalog == OAUTH_SERVER_SCOPE_CATALOG
  errors << "OAuth authorization-server scope catalog is not sorted and unique" unless scope_catalog == scope_catalog.sort.uniq
  errors << "OAuth protected-resource scope catalog drifted" unless protected_scopes == OAUTH_PROTECTED_RESOURCE_SCOPES
  errors << "OAuth protected-resource scopes exceed the authorization-server catalog" unless (protected_scopes - scope_catalog).empty?
  errors << "OAuth dynamic-registration scope policy drifted" unless registration_scopes == OAUTH_DYNAMIC_REGISTRATION_SCOPES
  errors << "OAuth dynamic-registration scopes exceed the authorization-server catalog" unless (registration_scopes - scope_catalog).empty?

  protected_resource = configuration_row(configuration, "struct:ref.oauth_server.protected_resource")
  resource_identifier = configuration_row(configuration, "oauth_server.protected_resource.resource")
  unless configuration_reference_value(configuration, "oauth_server.protected_resource.resource") == "REQUIRED URL" &&
         resource_identifier.include?("absolute HTTPS origin") &&
         resource_identifier.include?("MUST equal the origin of `http.external_base_url`")
    errors << "OAuth protected-resource identifier lacks a canonical typed origin authority"
  end
  unless protected_resource.include?("resource = oauth_server.protected_resource.resource") &&
         protected_resource.include?("scopes_supported = oauth_server.protected_resource.supported_scopes")
    errors << "OAuth protected-resource metadata authority is not linked"
  end
  audiences = configuration_row(configuration, "oauth_server.audiences")
  unless audiences.include?("`oauth_server.protected_resource.resource`, `oauth_server.issuer`") &&
         audiences.include?("access-token audience")
    errors << "OAuth protected-resource audience authority is not exact"
  end
  resource_consumers = applicability.filter_map do |unit, entry|
    unit if entry.fetch("configuration").include?("ref.oauth_server.protected_resource.resource")
  end.sort
  unless resource_consumers == %w[identity/reference oauth-server oauth-server/oidc]
    errors << "OAuth protected-resource identifier applicability drifted"
  end
  oauth_contract = [protocol, oauth_goal, api_operations, reference_profile].join("\n").split.join(" ")
  [
    "token issuance, introspection, and resource verification",
    "exact `oauth_server.protected_resource.resource` origin and MUST NOT derive it from an inbound host",
    "RFC 9728 metadata with `resource` byte-for-byte equal to the canonical `oauth_server.protected_resource.resource` origin",
    "requires audience/resource byte-for-byte equal to `oauth_server.protected_resource.resource`",
    "every protected-resource token audience are byte-for-byte equal to the canonical `oauth_server.protected_resource.resource` origin"
  ].each do |required|
    errors << "OAuth protected-resource authority chain drifted: #{required}" unless oauth_contract.include?(required)
  end
  if oauth_contract.match?(/RFC 9728 `?resource`? (?:is )?derived from (?:the )?request host/i)
    errors << "OAuth protected-resource identifier permits request-host derivation"
  end
  dynamic_registration = configuration_row(configuration, "struct:ref.oauth_server.dynamic_registration")
  unless dynamic_registration.include?("registration_protocol = RFC7591") &&
         dynamic_registration.include?("allowed_scopes = oauth_server.dynamic_registration.allowed_scopes") &&
         dynamic_registration.include?("management = unselected") &&
         dynamic_registration.include?("registration_access_token = not_issued")
    errors << "OAuth dynamic-registration closed policy drifted"
  end
  unless configuration_row(configuration, "oauth_server.dynamic_registration.management").empty?
    errors << "OAuth dynamic-registration management has duplicate configuration authority"
  end

  idp_initiated = configuration_row(configuration, "saml.idp_initiated")
  unless configuration_reference_value(configuration, "saml.idp_initiated") == "`false`" &&
         idp_initiated.include?("explicit Boolean") && idp_initiated.include?("`true` requires")
    errors << "SAML IdP-initiated configuration is not an explicit enableable Boolean"
  end
  normalized_protocol = protocol.split.join(" ")
  saml_contract = [protocol, saml_goal, api_operations, reference_profile].join("\n").split.join(" ")
  required_idp_init = "IdP-initiated login is disabled by default and becomes enabled only when `saml.idp_initiated=true`"
  errors << "SAML IdP-initiated baseline does not describe conditional enablement" unless normalized_protocol.include?(required_idp_init)
  if saml_contract.match?(/IdP-initiated (?:login|SAML) remains disabled/i) ||
     saml_contract.match?(/IdP-initiated (?:login|SAML) (?:is )?(?:unsupported|forbidden|not supported)/i) ||
     saml_contract.match?(/IdP-initiated (?:login|SAML) is disabled(?! by default)/i)
    errors << "SAML IdP-initiated baseline retains an unconditional prohibition"
  end
  redirect_algorithm = configuration_reference_value(configuration, "saml.redirect_signature_algorithm")
  unless redirect_algorithm == "`#{SAML_REDIRECT_SIGNATURE_ALGORITHM}`"
    errors << "SAML HTTP-Redirect SigAlg policy is missing or unsupported"
  end
  redirect_contract = [protocol, saml_goal, api_operations].join("\n").split.join(" ")
  [
    "SAMLRequest=value&RelayState=value&SigAlg=value",
    "SAMLRequest=value&SigAlg=value",
    "Missing, duplicated, unsupported, or mismatched `SigAlg`"
  ].each do |required|
    errors << "SAML HTTP-Redirect signing contract drifted: #{required}" unless redirect_contract.include?(required)
  end
  errors << "OAuth scope contract is absent from the oauth-server goal" unless oauth_goal.include?("oauth_server.scopes")
  required_registration_selection = "RFC 7591 registration is selected only when `oauth_server.dynamic_registration.enabled=true`"
  required_management_selection = "RFC 7592 management remains an unselected future profile"
  errors << "OAuth RFC 7591 selection is ambiguous" unless normalized_protocol.include?(required_registration_selection)
  errors << "OAuth RFC 7592 selection is ambiguous" unless normalized_protocol.include?(required_management_selection)
  {
    "reference profile" => reference_profile,
    "API catalog" => api_operations,
    "oauth-server goal" => oauth_goal
  }.each do |name, document|
    normalized = document.split.join(" ")
    errors << "OAuth RFC 7591/RFC 7592 selection drifted in #{name}" unless normalized.include?("RFC 7591 only") && normalized.include?("RFC 7592") && normalized.include?("unselected")
  end
  registration_contract = [protocol, oauth_goal, api_operations, reference_profile].join("\n").split.join(" ")
  if registration_contract.match?(/Enabling RFC 7591 (?:also |implicitly )?(?:enables|selects) RFC 7592/i) ||
     registration_contract.match?(/RFC 7592 management is (?:also )?(?:enabled|selected) with RFC 7591/i)
    errors << "OAuth RFC 7591 and RFC 7592 selections are conflated"
  end
  conflated_registration_selection = "Dynamic Client Registration, RFC 7591, and Client Registration Management, RFC 7592, only when the disabled-by-default registration profile is enabled"
  if normalized_protocol.include?(conflated_registration_selection)
    errors << "OAuth RFC 7591 and RFC 7592 selections are conflated"
  end

  contracts = JSON.parse(public_contracts)
  operations = contracts.fetch("operations").to_h { |operation| [operation.fetch("id"), operation] }
  operation = ->(id) { operations.fetch(id) }
  field = lambda do |id, side, name|
    operation.call(id).fetch(side).fetch("fields").find { |candidate| candidate.fetch("name") == name }
  end

  apple = operation.call("identity.oauth.callback-form-post")
  apple_fields = apple.fetch("request").fetch("fields").map { |candidate| candidate.fetch("name") }
  unless %w[Code IDToken State].all? { |name| apple_fields.include?(name) } &&
         apple.fetch("semantics").include?("front-channel ID token") &&
         apple.fetch("semantics").include?("issuer, audience, `azp`, nonce, `c_hash`, time and subject")
    errors << "Apple form-post callback contract omits the bound code, ID token, state or front-channel validation"
  end

  enterprise_oauth = operation.call("identity.sso.oauth-callback")
  issuer = field.call("identity.sso.oauth-callback", "request", "Issuer")
  unless issuer && issuer.fetch("required") && enterprise_oauth.fetch("semantics").include?("RFC 9207") &&
         enterprise_oauth.fetch("semantics").include?("expected issuer")
    errors << "enterprise OAuth callback omits RFC 9207 issuer validation"
  end

  normalized_reference_profile = reference_profile.split.join(" ")
  frontchannel_cookie_requirements = [
    "Apple and SAML cross-site HTTP-POST callbacks use a separate `__Secure-identity_frontchannel` correlation cookie",
    "with `Secure`, `HttpOnly`, `SameSite=None`, no `Domain`, an exact Apple or SAML callback `Path`, a five-minute maximum lifetime and one-time flow binding",
    "issued only for the selected cross-site POST flow",
    "It never authenticates a session",
    "normal `__Host-identity_session` and `__Host-identity_flow` cookies remain `SameSite=Lax`"
  ]
  unless frontchannel_cookie_requirements.all? { |requirement| normalized_reference_profile.include?(requirement) }
    errors << "cross-site POST flow correlation cookie contract is absent or weakens the normal session cookie"
  end

  %w[identity.oauth-server.dynamic-register identity.sso.saml-start].each do |id|
    row = api_operations.lines.find do |line|
      line.start_with?("| `#{id}` |") && line.split("|").length == 7
    end.to_s
    risk_and_replay = row.split("|").map(&:strip)[4].to_s
    errors << "#{id} does not use protocol-command replay identity" unless risk_and_replay.split("/").map(&:strip).last == "protocol-command"
  end
  saml_start = operation.call("identity.sso.saml-start")
  saml_start_command = field.call("identity.sso.saml-start", "request", "CommandID")
  unless saml_start_command && saml_start_command.fetch("required") &&
         saml_start_command.fetch("semantics").include?("server-owned protocol-command") &&
         saml_start.fetch("semantics").include?("client `Idempotency-Key` is optional")
    errors << "identity.sso.saml-start public contract omits protocol-command replay identity"
  end

  %w[identity.oauth-server.client-create identity.oauth-server.dynamic-register].each do |id|
    public_field = field.call(id, "request", "Public")
    unless public_field && public_field.fetch("type") == "bool" && public_field.fetch("required") &&
           public_field.fetch("zero_value").include?("false selects confidential")
      errors << "#{id} does not explicitly represent Public=false confidential clients"
    end
  end
  oauth_server_unit = contracts.fetch("units").find { |unit| unit.fetch("unit") == "oauth-server" }
  client_type = oauth_server_unit.fetch("types").find { |type| type.fetch("name") == "Client" }
  client_public = client_type.fetch("fields").find { |candidate| candidate.fetch("name") == "Public" }
  unless client_public && client_public.fetch("required") &&
         client_public.fetch("semantics").include?("false selects a confidential client") &&
         client_public.fetch("zero_value").include?("false is valid confidential")
    errors << "oauthserver.Client.Public=false is not a valid confidential-client state"
  end

  introspection = operation.call("identity.oauth-server.introspect")
  introspection_fields = introspection.fetch("result").fetch("fields").to_h { |candidate| [candidate.fetch("name"), candidate] }
  expected_introspection_fields = %w[Active ClientID Subject Scopes Audience ExpiresAt]
  unless introspection_fields.keys == expected_introspection_fields
    errors << "OAuth introspection result field closure drifted"
  end
  active = introspection_fields.fetch("Active")
  unless active.fetch("required") && active.fetch("type") == "bool" &&
         active.fetch("zero_value").include?("false is RFC 7662 inactive") &&
         introspection.fetch("semantics").include?("RFC 7662 inactive")
    errors << "OAuth introspection cannot represent RFC 7662 Active=false"
  end
  (introspection_fields.keys - ["Active"]).each do |name|
    candidate = introspection_fields.fetch(name)
    unless !candidate.fetch("required") && candidate.fetch("zero_value").include?("absent") &&
           candidate.fetch("semantics").include?("inactive token")
      errors << "OAuth inactive introspection metadata is not optional: #{name}"
    end
  end
  unless protocol.include?("inactive or unknown token as a normal\nsuccessful response with `active=false`") &&
         oauth_goal.include?("with `Active=false`; client, subject, scopes, audience, expiry and every other") &&
         api_operations.include?("RFC 7662 inactive returns `active=false` with token metadata absent")
    errors << "OAuth RFC 7662 inactive-token acceptance semantics drifted"
  end

  %w[identity.sso.oidc-logout identity.sso.oidc-logout-complete].each do |id|
    result_fields = operation.call(id).fetch("result").fetch("fields")
    outcome = result_fields.find { |candidate| candidate.fetch("name") == "Outcome" }
    required_bools = result_fields.select { |candidate| candidate.fetch("type") == "bool" && candidate.fetch("required") }
    unless outcome && outcome.fetch("semantics").include?("closed mutually exclusive") && required_bools.empty?
      errors << "#{id} does not use one exclusive logout outcome"
    end
  end

  %w[identity.sso.saml-acs identity.sso.saml-idp-init identity.sso.saml-slo identity.sso.saml-start].each do |id|
    relay_fields = operation.call(id).fetch("request").fetch("fields").select { |candidate| candidate.fetch("name") == "RelayState" }
    errors << "#{id} incorrectly requires RelayState" if relay_fields.any? { |candidate| candidate.fetch("required") }
  end
  start_relay = field.call("identity.sso.saml-start", "result", "RelayState")
  errors << "identity.sso.saml-start result incorrectly requires RelayState" if start_relay&.fetch("required")

  saml_unit = contracts.fetch("units").find { |unit| unit.fetch("unit") == "sso/saml" }
  saml_operations = contracts.fetch("operations").select { |candidate| candidate.fetch("owner") == "sso/saml" }
  declared_contract_types = contracts.fetch("units").flat_map do |unit|
    unit.fetch("types").map { |type| type.fetch("name") } + unit.fetch("interfaces").map { |interface| interface.fetch("name") }
  end
  declared_contract_types.concat(saml_operations.flat_map do |candidate|
    [candidate.dig("request", "name"), candidate.dig("result", "name"), candidate.dig("errors", "name")] +
      candidate.fetch("errors").fetch("variants").map { |variant| variant.fetch("type") }
  end.compact)
  referenced_type_strings = saml_unit.fetch("types").flat_map { |type| type.fetch("fields", []).map { |candidate| candidate.fetch("type") } }
  referenced_type_strings.concat(saml_unit.fetch("interfaces").flat_map { |interface| interface.fetch("methods").map { |method| method.fetch("signature") } })
  referenced_type_strings.concat(saml_unit.fetch("constructors").map { |constructor| constructor.fetch("signature") })
  referenced_type_strings.concat(saml_operations.flat_map do |candidate|
    [candidate.fetch("signature")] +
      candidate.fetch("request").fetch("fields").map { |entry| entry.fetch("type") } +
      candidate.fetch("result").fetch("fields").map { |entry| entry.fetch("type") } +
      candidate.fetch("errors").fetch("variants").flat_map { |variant| variant.fetch("fields").map { |entry| entry.fetch("type") } }
  end)
  referenced_saml_types = referenced_type_strings.flat_map do |value|
    value.sub(/\A[A-Za-z0-9_]+\(/, "(")
         .gsub(/\b(?:[a-z][A-Za-z0-9_]*\/)*[a-z][A-Za-z0-9_]*\.[A-Z][A-Za-z0-9_]*\b/, "")
         .scan(/\b[A-Z][A-Za-z0-9_]*\b/)
  end.uniq
  unresolved_saml_types = referenced_saml_types - declared_contract_types.uniq - %w[CommandID Completion]
  errors << "SAML public contract references undeclared types" unless unresolved_saml_types.empty?
  replay_method = saml_unit.fetch("interfaces").find { |interface| interface.fetch("name") == "ReplayStore" }.fetch("methods").first
  unless replay_method.fetch("name") == "ReserveSet" && replay_method.fetch("signature").include?("ReplaySet") &&
         replay_method.fetch("semantics").include?("Response ID and every consumed Assertion ID") &&
         replay_method.fetch("semantics").include?("none are reserved")
    errors << "SAML replay authority does not atomically reserve the complete response/assertion ID set"
  end

  idp_init = operation.call("identity.sso.saml-idp-init")
  if idp_init.fetch("authorization").fetch("access").match?(/relay state|pre-auth/i) ||
     idp_init.fetch("semantics").match?(/requires? .*RelayState/i)
    errors << "SAML IdP-initiated login requires impossible pre-auth or RelayState"
  end

  clause_pins = conformance.fetch("clause_pins")
  unless clause_pins.any? { |pin| pin.fetch("requirement_id") == "saml.single-logout-protocol" && pin.fetch("source_id") == "saml-core-2.0-os" && pin.fetch("locator") == "Section 3.7 Single Logout Protocol" } &&
         clause_pins.any? { |pin| pin.fetch("requirement_id") == "saml.single-logout-profile" && pin.fetch("source_id") == "saml-profiles-2.0-os" && pin.fetch("locator") == "Section 4.4 Single Logout Profile" }
    errors << "SAML Single Logout clause pins are incomplete"
  end
  errors
end

def expect_protocol_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("protocol negative fixture #{label} was accepted") if errors.empty?
  fail_check("protocol negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}") unless errors.include?(expected_error)
end

def conformance_tool_errors(tools, known_units)
  errors = []
  expected_keys = %w[id url revision sha256 license consumers]
  expected_ids = EXPECTED_CONFORMANCE_TOOLS.map { |tool| tool.fetch("id") }
  actual_ids = tools.filter_map { |tool| tool["id"] }
  errors << "protocol conformance tool order or IDs drifted" unless actual_ids == expected_ids
  tools.each do |tool|
    id = tool["id"] || "unknown"
    errors << "protocol conformance tool #{id} schema drifted" unless tool.keys == expected_keys
    errors << "protocol conformance tool #{id} lacks immutable revision" unless tool["revision"]&.match?(/\A[0-9a-f]{40}\z/)
    errors << "protocol conformance tool #{id} has invalid retrieved digest" unless tool["sha256"]&.match?(/\A[0-9a-f]{64}\z/)
    consumers = tool["consumers"]
    unless consumers.is_a?(Array) && !consumers.empty? && consumers == consumers.sort.uniq && consumers.all? { |unit| known_units.include?(unit) }
      errors << "protocol conformance tool #{id} consumers are not exact known units"
    end
  end
  EXPECTED_CONFORMANCE_TOOLS.each do |expected|
    actual = tools.find { |tool| tool["id"] == expected.fetch("id") }
    next unless actual

    id = expected.fetch("id")
    errors << "protocol conformance tool #{id} URL drifted" unless actual["url"] == expected.fetch("url")
    errors << "protocol conformance tool #{id} revision drifted" unless actual["revision"] == expected.fetch("revision")
    errors << "protocol conformance tool #{id} retrieved digest drifted" unless actual["sha256"] == expected.fetch("sha256")
    errors << "protocol conformance tool #{id} license drifted" unless actual["license"] == expected.fetch("license")
    errors << "protocol conformance tool #{id} consumer set drifted" unless actual["consumers"] == expected.fetch("consumers")
  end
  errors
end

def normative_rfc_errors(documents, source_ids)
  referenced = documents.values.flat_map do |document|
    document.scan(/\bRFC(?:\s+|-)?(\d{4})\b/i).flatten.map { |number| "rfc-#{number}" }
  end.to_set
  missing = referenced - source_ids.to_set
  missing.empty? ? [] : ["normative RFC manifest closure drifted: #{missing.to_a.sort.join(', ')}"]
end

def protocol_source_identity_errors(source_identity, sources)
  records = sources.map do |source|
    id = source.fetch("id")
    identity = source_identity.fetch(id)
    {
      "id" => id,
      "title" => identity.fetch("title"),
      "revision" => identity.fetch("revision"),
      "url" => source.fetch("url"),
      "sha256" => source.fetch("sha256"),
      "license" => source.fetch("license")
    }
  end
  digest = Digest::SHA256.hexdigest(JSON.generate(records))
  digest == EXPECTED_PROTOCOL_SOURCE_IDENTITY_SHA256 ? [] : ["protocol source identity digest drifted"]
end

def protocol_source_consumer_errors(sources, known_units)
  errors = []
  expected_ids = EXPECTED_PROTOCOL_SOURCE_CONSUMERS.keys.to_set
  actual_ids = sources.filter_map { |source| source["id"] }.to_set
  errors << "protocol source consumer expectation coverage drifted" unless expected_ids == actual_ids
  EXPECTED_PROTOCOL_SOURCE_CONSUMERS.each do |id, declared_consumers|
    source = sources.find { |candidate| candidate["id"] == id }
    next unless source

    expected_consumers = declared_consumers == :all_units ? known_units.to_a.sort : declared_consumers
    consumers = source["consumers"]
    unless consumers.is_a?(Array) && consumers == consumers.sort.uniq && consumers.all? { |unit| known_units.include?(unit) }
      errors << "protocol source #{id} consumers are not exact known units"
    end
    errors << "protocol source #{id} consumer set drifted" unless consumers == expected_consumers
  end
  errors
end

def capability_retention_errors(transaction_contract:, reference_configuration:)
  errors = []
  capability_contract = transaction_contract[/^Capability states are .*?(?=^## Required proof)/m].to_s.gsub(/\s+/, " ")
  {
    "`capability.terminal_retention` applies equally to `finalized`, `released`, `expired`, and `revoked`" =>
      "capability retention omits terminal revoked state",
    "A revoked record MUST remain through the later of its original capability expiry and the configured terminal-retention deadline" =>
      "capability retention omits revoked later-of retention bound",
    "During that interval `QueryCapability` MUST return its stable redacted revoked classification and reserve MUST continue to return the same non-enumerating denial" =>
      "capability retention omits stable retained-record denial",
    "Only after both bounds pass MAY cleanup atomically crypto-shred payload and subject linkage" =>
      "capability retention omits post-bound crypto-shredding",
    "It MUST retain a restricted tombstone containing only tenant, purpose, keyed digest, key version, original expiry, and `revoked`; the tombstone has no time-based deletion and every reserve/query continues the same denial" =>
      "capability retention omits persistent restricted tombstone",
    "The tombstone MAY be deleted only after the cryptographic key version is retired and proof shows no bearer under it can validate" =>
      "capability retention omits proved key-retirement deletion gate",
    "Post-disposal replay is therefore denied by the tombstone or, after that proved key retirement, by cryptographic validation before lookup" =>
      "capability retention omits post-disposal replay denial",
    "Issuance creates a new random bearer/digest and MUST NOT reconstruct `issued` from the presented bearer" =>
      "capability retention permits replay reconstruction"
  }.each do |required, error|
    errors << error unless capability_contract.include?(required)
  end

  terminal_retention_row = configuration_row(reference_configuration, "capability.terminal_retention")
  {
    "finalized/released/expired/revoked" => "capability retention configuration omits terminal revoked state",
    "later of original capability expiry and retention deadline" => "capability retention configuration omits revoked later-of retention bound",
    "crypto-shredded" => "capability retention configuration omits post-bound crypto-shredding",
    "minimal tenant/purpose/keyed-digest/key-version/expiry/revoked tombstone has no time-based deletion" =>
      "capability retention configuration omits persistent restricted tombstone",
    "replay denied" => "capability retention configuration omits replay denial",
    "proved key-version retirement" => "capability retention configuration omits proved key-retirement deletion gate"
  }.each do |required, error|
    errors << error unless terminal_retention_row.include?(required)
  end
  errors
end

def expect_capability_retention_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("capability retention negative fixture #{label} was accepted") if errors.empty?
  unless errors.include?(expected_error)
    fail_check("capability retention negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}")
  end
end

def without_normalized_phrase(document, phrase)
  document.sub(Regexp.new(phrase.split(/\s+/).map { |part| Regexp.escape(part) }.join("\\s+")), "")
end

def rfc_contradiction_errors(mfa_postgres_goal:, oauth_postgres_goal:, identity_postgres_goal:, lifecycle_contract:, applicability:)
  errors = []
  required_completion_roles = %w[
    tx.capability.apply tx.capability.finalize tx.capability.recover tx.capability.reserve
  ]

  mfa_primitives = metadata_values(mfa_postgres_goal, "Consumes existing primitives")
  errors << "identity/mfa/postgres omits capability/postgres prerequisite" unless mfa_primitives.include?("capability/postgres")
  mfa_transactions = applicability.fetch("identity/mfa/postgres").fetch("transaction")
  missing_mfa_roles = required_completion_roles - mfa_transactions
  unless missing_mfa_roles.empty?
    errors << "identity/mfa/postgres omits capability completion applicability: #{missing_mfa_roles.join(', ')}"
  end
  mfa_completion = "Continuation completion MUST treat capability validation as read-only and MUST use the enlisted `capability/postgres` participant to reserve the existing proof before applying the MFA transition and invoking the transaction-aware session issuer; it MUST finalize the capability and continuation in the same authoritative unit-of-work commit"
  errors << "identity/mfa/postgres lacks atomic capability completion ownership" unless mfa_postgres_goal.gsub(/\s+/, " ").include?(mfa_completion)

  social_link_row = lifecycle_contract.lines.find { |line| line.start_with?("| `lifecycle.dimension.social_link` |") }.to_s
  unless social_link_row.include?("| `identity/postgres` |")
    errors << "social_link lifecycle dimension has the wrong durable owner"
  end
  identity_link_ownership = "Provider-link rows, account-link metadata, provider-subject uniqueness, and the `lifecycle.dimension.social_link` authority version MUST remain owned here"
  errors << "identity/postgres lacks exact social-link ownership" unless identity_postgres_goal.gsub(/\s+/, " ").include?(identity_link_ownership)
  oauth_link_exclusion = "This adapter MUST NOT own provider-link rows, account-link metadata, provider-subject uniqueness constraints, or the `lifecycle.dimension.social_link` authority version"
  errors << "identity/oauth/postgres does not exclude social-link ownership" unless oauth_postgres_goal.gsub(/\s+/, " ").include?(oauth_link_exclusion)
  oauth_link_enlistment = "Token refresh, unlink, and token deletion MUST enlist `identity/postgres` to bump the authoritative `social_link` version in the same unit of work as the local token mutation"
  errors << "identity/oauth/postgres omits token-coupled social-link enlistment" unless oauth_postgres_goal.gsub(/\s+/, " ").include?(oauth_link_enlistment)

  identity_lifecycle = applicability.fetch("identity/postgres").fetch("lifecycle")
  oauth_lifecycle = applicability.fetch("identity/oauth/postgres").fetch("lifecycle")
  unless identity_lifecycle.include?("lifecycle.dimension.social_link") && !oauth_lifecycle.include?("lifecycle.dimension.social_link")
    errors << "social_link lifecycle applicability does not match its sole owner"
  end

  oauth_primitives = metadata_values(oauth_postgres_goal, "Consumes existing primitives")
  errors << "identity/oauth/postgres omits capability/postgres prerequisite" unless oauth_primitives.include?("capability/postgres")
  oauth_transactions = applicability.fetch("identity/oauth/postgres").fetch("transaction")
  missing_oauth_roles = required_completion_roles - oauth_transactions
  unless missing_oauth_roles.empty?
    errors << "identity/oauth/postgres omits capability callback applicability: #{missing_oauth_roles.join(', ')}"
  end
  oauth_callback = "Callback persistence MUST accept only an immutable `CapabilityProof` produced by read-only validation; that proof grants no authority and MUST NOT be already consumed. In the same authoritative unit of work, it MUST enlist `capability/postgres` to reserve the existing proof, apply its bound expiry, version, audience, origin, and risk checks to the identity link/token mutation, and finalize the capability with the command result"
  errors << "identity/oauth/postgres lacks reserve/apply/finalize callback semantics" unless oauth_postgres_goal.gsub(/\s+/, " ").include?(oauth_callback)
  if oauth_postgres_goal.include?("already verified and atomically consumed authorization-transaction identity") ||
     oauth_postgres_goal.include?("MUST NOT consume or reinterpret the capability proof")
    errors << "identity/oauth/postgres retains contradictory pre-consumed callback semantics"
  end
  errors
end

def contradiction_resolution_errors(api_operations:, security_events:, configuration:, reference_profile:, end_state:,
                                    parity:, http_goal:, reference_goal:, risk_goal:, risk_postgres_goal:, risk_valkey_goal:, applicability:)
  errors = []
  operation_event_map = {
    "identity.oauth.onetap-callback" => "identity.oauth.verify_one_tap",
    "identity.oauth.proxy-forward" => "identity.oauth.use_proxy",
    "identity.oauth-server.device-authorize" => "identity.oauth_server.authorize_device",
    "identity.oauth-server.device-approve" => "identity.oauth_server.approve_device",
    "identity.oauth-server.device-deny" => "identity.oauth_server.deny_device",
    "identity.oauth-server.device-token" => "identity.oauth_server.poll_device",
    "identity.oauth-server.session-token" => "identity.oauth_server.exchange_session",
    "identity.oauth-server.end-session" => "identity.oauth_server.end_session"
  }
  taxonomy = security_events[/^## Stable event taxonomy\n(.*?)(?=^## |\z)/m, 1].to_s
  operation_event_map.each do |operation_id, event_id|
    errors << "OAuth-server security taxonomy omits #{event_id}" unless taxonomy.include?("`#{event_id}`")
    rows = api_operations.lines.select { |line| line.start_with?("| `#{operation_id}` |") }.join
    errors << "#{operation_id} lacks exact security-event mapping" unless rows.include?("emits exactly `#{event_id}`")
  end
  {
    "identity/oauth/onetap" => operation_event_map.values_at("identity.oauth.onetap-callback"),
    "identity/oauth/proxy" => operation_event_map.values_at("identity.oauth.proxy-forward"),
    "oauth-server" => operation_event_map.values_at(
      "identity.oauth-server.session-token", "identity.oauth-server.end-session"
    ),
    "oauth-server/device" => operation_event_map.values_at(
      "identity.oauth-server.device-authorize", "identity.oauth-server.device-approve",
      "identity.oauth-server.device-deny", "identity.oauth-server.device-token"
    ),
    "oauth-server/oidc" => operation_event_map.values_at(
      "identity.oauth-server.session-token", "identity.oauth-server.end-session"
    ),
    "identity/session" => [operation_event_map.fetch("identity.oauth-server.end-session")]
  }.each do |unit, required_events|
    selected = applicability.fetch(unit).fetch("security_events")
    missing = required_events - selected
    errors << "#{unit} omits OAuth-server event applicability: #{missing.join(', ')}" unless missing.empty?
  end
  {
    "identity.oauth.verify_one_tap" => "identity/oauth/onetap",
    "identity.oauth.use_proxy" => "identity/oauth/proxy",
    "identity.oauth_server.authorize_device" => "oauth-server/device",
    "identity.oauth_server.approve_device" => "oauth-server/device",
    "identity.oauth_server.deny_device" => "oauth-server/device",
    "identity.oauth_server.poll_device" => "oauth-server/device"
  }.each do |event_id, expected_owner|
    owners = applicability.filter_map { |unit, selection| unit if selection.fetch("security_events").include?(event_id) }
    errors << "#{event_id} event ownership drifted: #{owners.join(', ')}" unless owners == [expected_owner]
  end

  deletion_configuration = configuration_row(configuration, "struct:ref.identity.delete.proof")
  deletion_end_state = end_state.split.join(" ")
  deletion_api = api_operations.lines.find { |line| line.start_with?("| `identity.deletion.request` |") }.to_s
  deletion_requirements = {
    "password user deletion proof" => [
      [deletion_configuration, "password_user = fresh_session_and_current_password"],
      [deletion_end_state, "current password plus fresh session for password users"],
      [deletion_api, "current password plus fresh session for a password user"]
    ],
    "passkey user deletion proof" => [
      [deletion_configuration, "passkey_user = fresh_uv_passkey"],
      [deletion_end_state, "fresh UV passkey for passkey users"],
      [deletion_api, "fresh UV passkey for a passkey user"]
    ],
    "provider-only verified-email deletion proof" => [
      [deletion_configuration, "provider_user = emailed_capability"],
      [deletion_end_state, "provider-only users with a verified email"],
      [deletion_api, "provider-only user with a verified email"]
    ],
    "no-verified-email deletion recovery" => [
      [deletion_configuration, "no_email_recovery = fresh_uv_passkey_or_administrator_recovery"],
      [deletion_end_state, "when no verified email exists"],
      [deletion_api, "when there is no verified email"]
    ]
  }
  deletion_requirements.each do |label, requirements|
    errors << "#{label} is not closed across deletion contracts" unless requirements.all? { |document, phrase| document.include?(phrase) }
  end

  rp_row = configuration.lines.find { |line| line.start_with?("| `struct:ref.oauth.rp_transaction`") }.to_s
  errors << "OAuth RP transaction omits remember_policy binding" unless rp_row[/binding = ([^`;]+)/, 1].to_s.split(",").include?("remember_policy")

  reference_contract = [reference_goal, reference_profile].join("\n").split.join(" ")
  normalized_reference_goal = reference_goal.split.join(" ")
  {
    "rate-limit/postgres authority" => "rate-limit/postgres",
    "rate-limit/valkey positive-cache-only boundary" => "rate-limit/valkey",
    "idempotency/postgres authority" => "idempotency/postgres",
    "idempotency/valkey non-authority boundary" => "idempotency/valkey"
  }.each do |label, phrase|
    errors << "reference composition omits #{label}" unless reference_contract.include?(phrase)
  end
  unless normalized_reference_goal.include?("Core HTTP rate limits MUST select `rate-limit/postgres`; `rate-limit/valkey` MAY cache only the positive metadata") &&
         normalized_reference_goal.include?("Mutating HTTP rows that declare key idempotency MUST select `idempotency/postgres` and MUST NOT use `idempotency/valkey` as authority") &&
         reference_contract.include?("Valkey may cache positive metadata only")
    errors << "reference composition does not close PostgreSQL/Valkey rate and idempotency authority"
  end

  risk_contract = [risk_postgres_goal, risk_valkey_goal, reference_profile].join("\n").split.join(" ")
  unless risk_contract.include?("PostgreSQL owns durable lockouts") &&
         risk_contract.include?("Ephemeral fixed/sliding velocity windows remain owned by `identity/risk/valkey`") &&
         risk_contract.include?("MUST NOT create, extend, clear or represent a durable lockout")
    errors << "risk persistence authority does not separate durable PostgreSQL state from ephemeral Valkey windows"
  end

  phone_reset = api_operations.lines.find do |line|
    line.start_with?("| `identity.phone.password-reset-complete` |")
  end.to_s
  unless phone_reset.include?("reset capability plus phone OTP plus independent factor") &&
         phone_reset.include?("canonical reset capability") &&
         phone_reset.include?("eligible independent factor") &&
         phone_reset.include?("phone/code/new-password alone is insufficient")
    errors << "phone password-reset completion lacks capability plus independent-factor authority"
  end

  normalized_api = api_operations.split.join(" ")
  unless normalized_api.include?("IPv6 uses the full canonical RFC 5952 address without subnet aggregation") &&
         normalized_api.include?("Subnet aggregation belongs only to a future, separately selected profile") &&
         !normalized_api.include?("configured reference `/64`")
    errors << "HTTP rate policy retains IPv6 subnet-aggregation ambiguity"
  end
  {
    "end state" => end_state,
    "parity catalog" => parity,
    "HTTP goal" => http_goal,
    "risk goal" => risk_goal,
    "risk PostgreSQL goal" => risk_postgres_goal,
    "risk Valkey goal" => risk_valkey_goal
  }.each do |name, document|
    normalized = document.split.join(" ")
    unless normalized.include?("full canonical RFC 5952 IPv6") &&
           normalized.include?("without subnet aggregation") &&
           normalized.include?("future, separately selected profile")
      errors << "#{name} retains selected-profile IPv6 aggregation ambiguity"
    end
  end
  rate_algorithm = configuration_row(configuration, "http.rate.algorithm")
  unless rate_algorithm.include?("PostgreSQL atomic token-bucket counters") &&
         rate_algorithm.include?("continuously refilled") &&
         !rate_algorithm.include?("fixed-window")
    errors << "reference HTTP rate algorithm conflicts with the token-bucket operation contract"
  end
  unless normalized_api.include?("`CSRF` below therefore means request `Origin` validation plus anti-CSRF token/session binding") &&
         normalized_api.include?("`origin` means callback or redirect allowlist validation")
    errors << "API notation conflates CSRF with callback/redirect origin policy"
  end

  preauth_rows = {
    "identity.password.signup" => "public plus pre-auth transaction",
    "identity.password.signin" => "public plus pre-auth transaction",
    "identity.username.signup" => "public plus pre-auth transaction",
    "identity.username.signin" => "public plus pre-auth transaction",
    "identity.magic-link.request" => "public plus pre-auth transaction",
    "identity.magic-link.consume" => "pre-auth-bound magic-link capability",
    "identity.email.verification-confirm" => "pre-auth-bound email capability when issuing a session, otherwise email capability",
    "identity.session.transfer-consume" => "pre-auth-bound transfer capability",
    "identity.otp.send" => "public plus pre-auth transaction for signin, session, or MFA continuation by purpose",
    "identity.otp.signin" => "pre-auth-bound public challenge",
    "identity.otp.email-verify" => "pre-auth-bound email challenge when issuing a session, otherwise email challenge",
    "identity.phone.signin" => "pre-auth-bound public challenge",
    "identity.phone.verify" => "pre-auth-bound phone challenge for signup/session issuance, or session-bound number-change challenge",
    "identity.anonymous.signin" => "public plus pre-auth transaction",
    "identity.passkey.signin-options" => "public plus pre-auth transaction",
    "identity.passkey.signin-verify" => "pre-auth-bound assertion challenge",
    "identity.passkey.register-verify" => "pre-auth-bound registration challenge for unauthenticated registration, otherwise session-bound registration challenge",
    "identity.oauth.signin-start" => "public plus pre-auth transaction",
    "identity.oauth.signin-token" => "public plus pre-auth transaction",
    "identity.oauth.callback" => "pre-auth-bound provider callback state",
    "identity.oauth.onetap-start" => "public plus pre-auth transaction",
    "identity.oauth.onetap-callback" => "pre-auth-bound One Tap state",
    "identity.sso.signin-start" => "public plus pre-auth transaction",
    "identity.sso.oidc-callback" => "pre-auth-bound OIDC state",
    "identity.sso.oauth-callback" => "pre-auth-bound OAuth state",
    "identity.sso.saml-start" => "public plus pre-auth transaction",
    "identity.sso.saml-acs" => "pre-auth-bound SAML response"
  }
  preauth_rows.each do |operation_id, access|
    rows = api_operations.lines.select { |line| line.start_with?("| `#{operation_id}` |") }.join
    errors << "#{operation_id} omits required pre-auth transaction binding" unless rows.include?("| #{access} /")
  end
  unless normalized_api.include?("`pre-auth transaction` is the five-minute, single-use unauthenticated session-issuance/linking transaction") &&
         normalized_api.include?("Public operations that cannot issue, replace, or link a session remain `public` without this transaction") &&
         normalized_api.include?("Enabled IdP-initiated SAML is the sole session-issuing exception")
    errors << "API notation lacks closed pre-auth transaction semantics and exclusion"
  end
  errors
end

def expect_contradiction_resolution_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("contradiction-resolution negative fixture #{label} was accepted") if errors.empty?
  unless errors.include?(expected_error)
    fail_check("contradiction-resolution negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}")
  end
end

def phone_contract_errors(configuration:, reference_profile:, api_operations:, phone_goal:, risk_goal:,
                          lifecycle_consumers:, security_events:, applicability:, inventory:, dependencies:)
  errors = []
  recovery_policy = configuration_row(configuration, "struct:ref.phone.recovery")
  %w[
    request_when_disabled=deny complete_when_disabled=deny
    proof=canonical_reset_capability_plus_purpose_bound_phone_otp_plus_eligible_independent_factor
    risk_authority=identity/risk risk_evidence=immutable_one_use
    risk_binding=tenant,subject,operation,purpose,canonical_number,preauth_transaction,attempt_id,policy_version
    risk_ttl=2m caller_signals=forbidden
    sim_swap=negative_allow,positive_deny,unknown_deny,unavailable_deny
    number_recycling=negative_allow,positive_deny,unknown_deny,unavailable_deny
    carrier=negative_allow,positive_deny,unknown_deny,unavailable_deny carrier_signal=required
  ].each do |member|
    errors << "phone recovery policy omits #{member}" unless recovery_policy.include?(member.sub("=", " = "))
  end
  if recovery_policy.match?(/(?:^|[ `;])enabled\s*=/)
    errors << "phone recovery policy duplicates the atomic enablement authority"
  end
  recovery_enabled = configuration_row(configuration, "phone.recovery.enabled")
  unless recovery_enabled.include?("| `false` |") && recovery_enabled.include?("request and completion are denied")
    errors << "phone recovery enablement is not explicitly disabled with fail-closed operations"
  end
  unless configuration_row(configuration, "phone.recovery.policy").include?("struct:ref.phone.recovery")
    errors << "phone recovery configuration does not reference the closed risk policy"
  end
  normalized_profile = reference_profile.split.join(" ")
  unless normalized_profile.include?("Phone password recovery is disabled by default") &&
         normalized_profile.include?("SIM-swap, number-recycling, or carrier signal is positive, unknown, or unavailable")
    errors << "reference profile lacks disabled fail-closed phone recovery behavior"
  end

  phone_rows = api_operations.lines.select { |line| line.start_with?("| `identity.phone.") }
  rows_by_id = phone_rows.to_h { |line| [line[/\| `([^`]+)`/, 1], line] }
  send_row = rows_by_id.fetch("identity.phone.send-verification", "")
  verify_row = rows_by_id.fetch("identity.phone.verify", "")
  signin_row = rows_by_id.fetch("identity.phone.signin", "")
  unless send_row.include?("public plus pre-auth transaction for signup/signin") &&
         send_row.include?("session-authenticated number-change") &&
         send_row.include?("tenant, purpose, canonical number, and resolved remember policy")
    errors << "phone verification initiation lacks exact public pre-auth and authenticated-change split"
  end
  unless verify_row.include?("pre-auth-bound phone challenge for signup/session issuance") &&
         verify_row.include?("session-bound number-change challenge") &&
         verify_row.include?("same tenant, purpose, canonical number, and resolved remember policy")
    errors << "phone verification completion lacks exact pre-auth binding"
  end
  unless signin_row.include?("pre-auth-bound public challenge") &&
         signin_row.include?("same tenant, signin purpose, canonical number, and resolved remember policy")
    errors << "phone signin completion lacks exact pre-auth binding"
  end
  normalized_api = api_operations.split.join(" ")
  unless normalized_api.include?("Phone signup initiation is `identity.phone.send-verification` with purpose `signup`")
    errors << "phone signup initiation is not assigned to the canonical pre-auth operation"
  end
  %w[request complete].each do |suffix|
    row = rows_by_id.fetch("identity.phone.password-reset-#{suffix}", "")
    unless row.include?("`phone.recovery.enabled=true`") && row.include?("denied while disabled")
      errors << "phone password-reset #{suffix} is not denied unless explicitly enabled"
    end
  end
  request_row = rows_by_id.fetch("identity.phone.password-reset-request", "")
  unless request_row.include?("explicitly enabled public recovery plus pre-auth transaction") &&
         request_row.include?("purpose-bound OTP challenge") &&
         request_row.include?("RiskEvidence` reference issued by `identity/risk`") &&
         request_row.include?("never raw carrier facts")
    errors << "phone password-reset request lacks authoritative pre-auth risk-evidence contract"
  end
  complete_row = rows_by_id.fetch("identity.phone.password-reset-complete", "")
  unless complete_row.include?("reset capability plus phone OTP plus independent factor") &&
         complete_row.include?("purpose-bound phone OTP proof") &&
         complete_row.include?("fresh one-use `RiskEvidence` issued by `identity/risk`") &&
         complete_row.include?("Raw caller carrier facts") && complete_row.include?("deny")
    errors << "phone password-reset completion lacks authoritative proof and risk-evidence contract"
  end

  normalized_phone_goal = phone_goal.split.join(" ")
  unless normalized_phone_goal.include?("Public signup/OTP-signin initiation MUST create or use the canonical single-use pre-auth transaction") &&
         normalized_phone_goal.include?("tenant, purpose, canonical number and resolved `RememberPolicy`") &&
         normalized_phone_goal.include?("Session-authenticated number-change challenges MUST NOT create or substitute a public pre-auth transaction") &&
         normalized_phone_goal.include?("Password signin MUST use the canonical password-signin pre-auth transaction") &&
         normalized_phone_goal.include?("MUST NOT reuse an OTP-signin transaction")
    errors << "identity/phone goal lacks exact pre-auth ownership and binding"
  end
  unless normalized_phone_goal.include?("canonical reset capability, a purpose-bound phone OTP, one eligible independent factor") &&
         normalized_phone_goal.include?("immutable `RiskEvidence` whose decision permits recovery") &&
         normalized_phone_goal.include?("issued by `identity/risk`, fresh, one-use")
    errors << "identity/phone goal lacks canonical recovery proof and authoritative risk evidence"
  end
  unless normalized_phone_goal.include?("caller-supplied carrier evidence MUST deny")
    errors << "identity/phone goal does not deny caller-supplied carrier evidence"
  end
  normalized_risk_goal = risk_goal.split.join(" ")
  unless normalized_risk_goal.include?("sole authority that issues immutable `RiskEvidence`") &&
         normalized_risk_goal.include?("MUST be atomically consumed at most once by `identity/risk`")
    errors << "identity/risk goal lacks immutable one-use phone RiskEvidence ownership"
  end
  unless normalized_risk_goal.include?("selected `risk_ttl` (two minutes in the reference profile)") &&
         normalized_risk_goal.include?("Stale, mismatched or replayed evidence MUST deny before credential mutation")
    errors << "identity/risk goal lacks phone RiskEvidence freshness and replay rejection"
  end
  risk_binding_phrase = "MUST bind tenant, subject, recovery operation, recovery purpose, canonical number, pre-auth transaction, attempt ID and risk-policy version"
  unless normalized_risk_goal.include?(risk_binding_phrase)
    errors << "identity/risk goal lacks exact phone RiskEvidence binding"
  end
  unless normalized_risk_goal.include?("Positive, unknown or unavailable carrier-risk decisions MUST deny issuance") &&
         normalized_risk_goal.include?("callers MUST NOT mint evidence or supply raw carrier facts or decisions as equivalent input")
    errors << "identity/risk goal lacks authoritative fail-closed carrier decision issuance"
  end
  if normalized_phone_goal.include?("optional session suppression") || api_operations.include?("session_suppression")
    errors << "identity/phone retains a session-suppression input"
  end
  unless normalized_phone_goal.include?("Phone operations do not expose session suppression") &&
         verify_row.include?("No session-suppression option exists") &&
         request_row.include?("no remember or session-suppression choice exists") &&
         complete_row.include?("no remember or session-suppression choice exists")
    errors << "identity/phone session-suppression removal is not closed across contracts"
  end

  inventory_phone_row = inventory.lines.find { |line| line.start_with?("| `identity/phone` |") }.to_s
  unless inventory_phone_row.include?("`identity/risk`") &&
         metadata_values(phone_goal, "Requires").include?("identity/risk") &&
         dependencies.include?("risk --> phone")
    errors << "identity/phone dependency on identity/risk is not closed across DAG contracts"
  end

  identifier_events = %w[
    identity.identifier.request_verification identity.identifier.verify
    identity.identifier.change identity.identifier.remove
  ]
  taxonomy = security_events[/^## Stable event taxonomy\n(.*?)(?=^## |\z)/m, 1].to_s
  identifier_events.each do |event|
    errors << "identifier security taxonomy omits #{event}" unless taxonomy.include?("`#{event}`")
  end
  {
    "identity.phone.send-verification" => "identity.identifier.request_verification",
    "identity.phone.verify" => "identity.identifier.verify",
    "identity.phone.update" => "identity.identifier.change",
    "identity.phone.remove" => "identity.identifier.remove"
  }.each do |operation, event|
    row = rows_by_id.fetch(operation, "")
    errors << "#{operation} lacks exact identifier security-event mapping" unless row.include?("emits exactly `#{event}`")
  end
  {
    "identity.phone.update" => "lifecycle.cascade.identifier_change",
    "identity.phone.remove" => "lifecycle.cascade.identifier_remove"
  }.each do |operation, cascade|
    row = rows_by_id.fetch(operation, "")
    errors << "#{operation} lacks exact identifier lifecycle-cascade mapping" unless row.include?("initiates exactly `#{cascade}`")
  end

  phone_applicability = applicability.fetch("identity/phone")
  required_cascades = %w[lifecycle.cascade.identifier_change lifecycle.cascade.identifier_remove]
  missing_lifecycle = required_cascades - phone_applicability.fetch("lifecycle")
  errors << "identity/phone omits identifier lifecycle cascades: #{missing_lifecycle.join(', ')}" unless missing_lifecycle.empty?
  missing_consumers = required_cascades - phone_applicability.fetch("lifecycle_consumers")
  errors << "identity/phone omits identifier lifecycle consumer checkpoints: #{missing_consumers.join(', ')}" unless missing_consumers.empty?
  missing_events = identifier_events - phone_applicability.fetch("security_events")
  errors << "identity/phone omits identifier security-event applicability: #{missing_events.join(', ')}" unless missing_events.empty?
  unless phone_applicability.fetch("security_events").include?("identity.risk.decide")
    errors << "identity/phone omits risk-decision security-event applicability: identity.risk.decide"
  end
  required_configuration = %w[
    ref.authentication.preauth_ttl ref.phone.recovery.enabled ref.phone.recovery.policy
    ref.risk.authority ref.risk.precedence ref.session.remember_default
    ref.struct:ref.phone.recovery ref.struct:ref.risk.authority ref.struct:ref.risk.precedence
  ]
  missing_configuration = required_configuration - phone_applicability.fetch("configuration")
  errors << "identity/phone omits phone recovery/pre-auth configuration applicability: #{missing_configuration.join(', ')}" unless missing_configuration.empty?
  expected_recovery_configuration = %w[
    ref.phone.recovery.enabled ref.phone.recovery.policy ref.struct:ref.phone.recovery
  ]
  actual_phone_recovery_configuration = phone_applicability.fetch("configuration").grep(/phone\.recovery/)
  unless actual_phone_recovery_configuration == expected_recovery_configuration
    errors << "identity/phone phone recovery configuration authority is not exact"
  end
  reference_configuration = applicability.fetch("identity/reference").fetch("configuration")
  missing_reference_configuration = required_configuration.grep(/phone\.recovery|struct:ref\.phone/) - reference_configuration
  unless missing_reference_configuration.empty?
    errors << "identity/reference omits phone recovery configuration applicability: #{missing_reference_configuration.join(', ')}"
  end
  unless reference_configuration.grep(/phone\.recovery/) == expected_recovery_configuration
    errors << "identity/reference phone recovery configuration authority is not exact"
  end

  required_cascades.each do |cascade|
    row = lifecycle_consumers.lines.find { |line| line.start_with?("| `#{cascade}` |") }.to_s
    errors << "#{cascade} omits identity/phone consumer" unless row.include?("`identity/phone`")
  end
  unless lifecycle_consumers.split.join(" ").include?("`identity/phone` uses `identity/postgres`")
    errors << "phone lifecycle checkpoint persistence owner is not explicit"
  end
  errors
end

def expect_phone_contract_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("phone contract negative fixture #{label} was accepted") if errors.empty?
  fail_check("phone contract negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}") unless errors.include?(expected_error)
end

def expect_rfc_contradiction_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("RFC contradiction negative fixture #{label} was accepted") if errors.empty?
  unless errors.include?(expected_error)
    fail_check("RFC contradiction negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}")
  end
end

def risk_evidence_contract_errors(transaction_contract:, api_operations:, risk_goal:, risk_postgres_goal:, phone_goal:, reference_goal:, end_state:, applicability:)
  errors = []
  normalized_transaction = transaction_contract.gsub(/\s+/, " ")
  normalized_risk_goal = risk_goal.gsub(/\s+/, " ")
  normalized_risk_postgres_goal = risk_postgres_goal.gsub(/\s+/, " ")
  normalized_phone_goal = phone_goal.gsub(/\s+/, " ")
  normalized_reference_goal = reference_goal.gsub(/\s+/, " ")
  normalized_end_state = end_state.gsub(/\s+/, " ")

  {
    "RiskEvidence states are `issued`, `reserved`, `finalized`, `released`, `expired`, and `revoked`" =>
      "RiskEvidence state set is not closed",
    "Legal transitions are only `absent` to `issued`, `issued` to `reserved`, `issued` to `expired` or `revoked`, `reserved` to `finalized`, and `reserved` to `released` or `revoked`" =>
      "RiskEvidence legal transitions are incomplete",
    "A read-only precheck grants no authority; command acceptance requires `tx.risk_evidence.reserve`" =>
      "RiskEvidence precheck can bypass durable reservation",
    "Two commands MAY precheck the same item concurrently, but the PostgreSQL row lock and unique keyed digest MUST give exactly one command the `issued` to `reserved` transition; every different command receives the same non-enumerating replay denial" =>
      "RiskEvidence concurrent reservation lacks one-winner denial",
    "The phone-recovery completion command MUST enlist `identity/risk/postgres`, `identity/otp/postgres`, `capability/postgres`, `identity/password/postgres`, and `identity/session/postgres` in one unit of work before the reservation transaction's first write" =>
      "phone recovery does not enlist every atomic participant",
    "The completion reservation transaction MUST transition the RiskEvidence, purpose-bound OTP, and reset capability together or none of them" =>
      "phone recovery reservation is not atomic across one-time participants",
    "the later domain commit MUST finalize the RiskEvidence reservation, purpose-bound OTP, reset capability, password mutation, session invalidation, outbox/audit records, and command result, or commit none of them" =>
      "phone recovery atomic finalization is incomplete",
    "`tx.risk_evidence.reserve` MUST run only in the coordinator's single `tx.uow.reserve` transaction that atomically reserves the command and the exact one-time participants declared by the operation profile; it MUST NOT open a separate or private transaction" =>
      "RiskEvidence reservation can escape the coordinator unit of work",
    "When an expired `pending` command is taken over, this same transaction MUST first increment the command generation and then CAS-rebind every already-`reserved` one-time participant from the exact prior generation to the new generation under the same command ID and fingerprint" =>
      "RiskEvidence takeover lacks guarded generation transfer",
    "A missing, terminal, different-command, different-fingerprint, or non-prior-generation participant rolls back the entire takeover; the stale generation retains no apply/finalize authority" =>
      "RiskEvidence takeover permits stale or partial authority",
    "A retryable transaction rollback under the same live command reservation retains the RiskEvidence reservation for that command; a different command never takes it over" =>
      "RiskEvidence retry ownership is undefined",
    "An ambiguous commit MUST leave the item `reserved`, return `Unknown`, and use `tx.risk_evidence.recover`; expiry or lease timeout alone MUST NOT release it" =>
      "RiskEvidence unknown outcome can reopen evidence",
    "Only authoritative proof that the owning command did not commit MAY transition `reserved` to `released`, after which that evidence remains terminal and a retry requires newly issued evidence" =>
      "RiskEvidence release can permit reuse",
    "Cleanup MUST expire untouched `issued` rows in bounded database-time batches and retain every `reserved` row through authoritative recovery. Only after the later of original evidence expiry and the configured `command.result_retention` deadline MAY cleanup crypto-shred terminal payload/linkage; it MUST preserve a restricted tenant/purpose/keyed-digest/key-version/original-expiry/terminal-state tombstone with no time-based deletion. The tombstone MAY be deleted only after every evidence-verification and keyed-digest key version it references is retired and proof shows every bearer fails cryptographic validation before lookup" =>
      "RiskEvidence cleanup can erase replay or recovery authority"
  }.each do |required, error|
    errors << error unless normalized_transaction.include?(required)
  end

  {
    "Phone password-reset initiation MUST use one initiation command and coordinator unit of work to reserve, apply, and finalize initiation RiskEvidence" =>
      "phone recovery initiation lacks authoritative RiskEvidence orchestration",
    "The initiation reservation MUST accept only purpose `phone-password-reset-initiate`" =>
      "phone recovery initiation lacks purpose-bound reservation",
    "Phone reset initiation reserves only the command and initiation RiskEvidence, because its OTP challenge and capability do not exist until the domain commit" =>
      "phone recovery initiation can reserve outputs before issuance",
    "The initiation domain commit MUST apply and finalize that initiation RiskEvidence in the same commit that issues the purpose-bound OTP challenge, canonical reset capability, outbox/audit records, and command result, or commit none of them" =>
      "phone recovery initiation can split RiskEvidence from challenge issuance",
    "A same-command, same-fingerprint initiation replay MUST return the exact recorded challenge and capability result without issuing replacements" =>
      "phone recovery initiation replay can drift",
    "Two concurrent initiation commands MAY precheck the same evidence, but exactly one MAY reserve it; the loser receives the stable non-enumerating replay denial" =>
      "phone recovery initiation lacks one-winner reservation",
    "An expired initiation-command takeover MUST CAS-rebind the initiation RiskEvidence from the exact prior generation to the new generation before apply or finalize authority is granted" =>
      "phone recovery initiation takeover lacks generation fencing",
    "An initiation rollback MUST NOT release its RiskEvidence without authoritative proof that the command did not commit" =>
      "phone recovery initiation rollback can release without proof",
    "An ambiguous initiation outcome MUST remain `reserved` until authoritative recovery resolves the owning command" =>
      "phone recovery initiation unknown outcome can reopen evidence",
    "Initiation and completion MUST use separate RiskEvidence references, keyed digests, reservations, and terminal records with purposes `phone-password-reset-initiate` and `phone-password-reset-complete`, respectively" =>
      "phone recovery phases do not require distinct RiskEvidence artifacts",
    "Neither phase's RiskEvidence MAY validate, reserve, replay, or substitute for the other phase" =>
      "phone recovery permits cross-phase RiskEvidence reuse",
    "Cleanup and tombstones MUST independently preserve replay and recovery authority for both phone-reset RiskEvidence purposes" =>
      "phone recovery phase retention is not distinct"
  }.each do |required, error|
    errors << error unless normalized_transaction.include?(required)
  end

  request_row = api_operations.lines.find { |line| line.start_with?("| `identity.phone.password-reset-request` |") }.to_s.gsub(/\s+/, " ")
  complete_row = api_operations.lines.find { |line| line.start_with?("| `identity.phone.password-reset-complete` |") }.to_s.gsub(/\s+/, " ")
  risk_evaluate_row = api_operations.lines.find { |line| line.start_with?("| `identity.risk.evaluate` |") }.to_s.gsub(/\s+/, " ")
  if risk_evaluate_row.empty?
    errors << "API operations omit canonical RiskEvidence issuance operation"
  else
    unless risk_evaluate_row.include?("phase `phone-reset-initiation` maps only to purpose `phone-password-reset-initiate`") &&
           risk_evaluate_row.include?("phase `phone-reset-completion` maps only to purpose `phone-password-reset-complete`")
      errors << "identity.risk.evaluate omits phase-specific RiskEvidence issuance"
    end
    unless risk_evaluate_row.include?("Issuance phase is exactly `none`, `phone-reset-initiation`, or `phone-reset-completion`") &&
           risk_evaluate_row.include?("Purpose is derived exclusively from the two issuing phases") &&
           risk_evaluate_row.include?("unknown/unsupported phases, a purpose with `none`, and any caller-supplied purpose are rejected before provider evaluation or state access")
      errors << "identity.risk.evaluate issuance phase catalog is not closed"
    end
    unless risk_evaluate_row.include?("authoritative server-resolved carrier facts") &&
           risk_evaluate_row.include?("tenant, subject, recovery operation, canonical number, pre-auth transaction, attempt ID, and risk-policy version")
      errors << "identity.risk.evaluate omits authoritative issuance inputs"
    end
    unless risk_evaluate_row.include?("opaque RiskEvidence reference plus purpose, issued-at, expires-at, and one-use metadata") &&
           risk_evaluate_row.include?("never raw signals, provider evidence, decision internals, embedded evidence payloads, digests, signatures, or journal identifiers")
      errors << "identity.risk.evaluate can leak RiskEvidence internals"
    end
    unless risk_evaluate_row.include?("Denied returns no reference") && risk_evaluate_row.include?("Failed proves no issuance") &&
           risk_evaluate_row.include?("Unknown returns no reference and requires same-command recovery") &&
           risk_evaluate_row.include?("same command and fingerprint replay the exact recorded result without issuing another artifact")
      errors << "identity.risk.evaluate issuance outcomes are not closed"
    end
  end
  unless request_row.include?("`phone-password-reset-initiate`") && request_row.include?("initiation-only `RiskEvidence`") &&
         request_row.include?("atomically finalized with OTP challenge and reset capability issuance")
    errors << "API operations omit initiation-specific RiskEvidence atomicity"
  end
  unless complete_row.include?("`phone-password-reset-complete`") && complete_row.include?("separate completion-only RiskEvidence") &&
         complete_row.include?("must not reuse the initiation artifact")
    errors << "API operations permit cross-phase RiskEvidence reuse"
  end

  risk_postgres_ownership = "The adapter owns the durable one-use RiskEvidence journal and its `issued`, `reserved`, `finalized`, `released`, `expired`, and `revoked` transitions"
  errors << "identity/risk/postgres lacks durable RiskEvidence ownership" unless normalized_risk_postgres_goal.include?(risk_postgres_ownership)
  risk_postgres_recovery = "Unknown completion MUST retain `reserved` and reconcile the owning command before finalizing or releasing; expiry, lease loss, cleanup, or another command MUST NOT make the evidence reusable"
  errors << "identity/risk/postgres lacks fail-closed RiskEvidence recovery" unless normalized_risk_postgres_goal.include?(risk_postgres_recovery)
  risk_postgres_reservation = "Reservation MUST run only through this adapter's predeclared contributor in the coordinator's single reservation transaction with the exact participants declared by the operation profile: initiation reserves only its command and RiskEvidence, while completion also reserves the existing purpose-bound OTP and reset capability. A separate/private RiskEvidence reservation transaction is forbidden"
  errors << "identity/risk/postgres permits private RiskEvidence reservation" unless normalized_risk_postgres_goal.include?(risk_postgres_reservation)
  risk_postgres_takeover = "Expired command-owner takeover MUST CAS the RiskEvidence reservation from the exact prior generation to the new generation in the same coordinator reservation transaction that transfers every other participant declared by the operation profile"
  errors << "identity/risk/postgres lacks guarded takeover generation transfer" unless normalized_risk_postgres_goal.include?(risk_postgres_takeover)
  phone_atomicity = "Phone password-reset completion MUST use one coordinator command and unit of work to reserve and finalize RiskEvidence with the purpose-bound OTP, reset capability, password mutation, and session invalidation"
  errors << "identity/phone lacks atomic RiskEvidence completion" unless normalized_phone_goal.include?(phone_atomicity)
  phone_initiation_atomicity = "Phone password-reset initiation MUST use one coordinator command and unit of work to reserve, apply, and finalize initiation-only RiskEvidence with purpose-bound OTP challenge and canonical reset capability issuance"
  errors << "identity/phone lacks atomic RiskEvidence initiation" unless normalized_phone_goal.include?(phone_initiation_atomicity)
  phone_phase_distinction = "Initiation and completion MUST use separate purpose-bound RiskEvidence artifacts and MUST NOT validate, reserve, replay, or substitute one for the other"
  errors << "identity/phone lacks phase-distinct RiskEvidence" unless normalized_phone_goal.include?(phone_phase_distinction)
  contributor_boundary = "This package MUST expose contributor interfaces for those effects without importing or naming concrete persistence adapters"
  errors << "identity/phone does not preserve its contributor-only ownership boundary" unless normalized_phone_goal.include?(contributor_boundary)
  module_adapters = JSON.parse(File.read(File.join(REPOSITORY_ROOT, "modules.json"))).fetch("modules").map do |record|
    record.fetch("directory").delete_prefix("pkg/")
  end
  package_prefix = "github.com/faustbrian/golib/pkg/"
  package_adapters = JSON.parse(File.read(File.join(REPOSITORY_ROOT, "packages.json"))).fetch("packages").filter_map do |record|
    import_path = record.fetch("import_path")
    import_path.delete_prefix(package_prefix) if import_path.start_with?(package_prefix)
  end
  concrete_phone_adapters = (REFERENCE_ADAPTERS.to_a + module_adapters + package_adapters).uniq.select { |adapter| adapter.end_with?("/postgres", "/valkey") }
  leaked_phone_adapters = concrete_phone_adapters.select { |adapter| normalized_phone_goal.include?("`#{adapter}`") }
  permitted_privacy_binding = "When the reference PostgreSQL profile is selected, `identity/postgres` MUST persist the phone anonymization/deletion checkpoint and privacy-export fragment"
  if normalized_phone_goal.include?(permitted_privacy_binding) && normalized_phone_goal.scan("`identity/postgres`").length == 1
    leaked_phone_adapters.delete("identity/postgres")
  end
  errors << "identity/phone names reference-only concrete adapters: #{leaked_phone_adapters.join(', ')}" unless leaked_phone_adapters.empty?
  reference_composition = "For `identity.phone.password-reset-complete`, the reference composition MUST enlist `identity/risk/postgres`, `identity/otp/postgres`, `capability/postgres`, `identity/password/postgres`, and `identity/session/postgres` in one coordinator unit of work before the reservation transaction's first write"
  errors << "identity/reference lacks exact phone recovery composition" unless normalized_reference_goal.include?(reference_composition)
  reference_acceptance = "Acceptance MUST prove all five contributors reserve and finalize together, retry and recover the same command generation, and never permit a partial subset or a second command to reuse RiskEvidence, OTP, or reset capability"
  errors << "identity/reference lacks phone recovery composition acceptance" unless normalized_reference_goal.include?(reference_acceptance)
  initiation_reference_composition = "For `identity.phone.password-reset-request`, the reference composition MUST enlist `identity/postgres`, `identity/risk/postgres`, `identity/otp/postgres`, `capability/postgres`, `audit/postgres`, and `outbox/postgres` in one coordinator unit of work before the reservation transaction's first write"
  errors << "identity/reference lacks exact phone recovery initiation composition" unless normalized_reference_goal.include?(initiation_reference_composition)
  initiation_reference_acceptance = "Acceptance MUST prove one concurrent reservation winner, stable same-command replay without replacement issuance, exact-generation takeover, fail-closed unknown recovery, and no partial challenge, capability, audit, outbox, command-result, or RiskEvidence finalization"
  errors << "identity/reference lacks phone recovery initiation acceptance" unless normalized_reference_goal.include?(initiation_reference_acceptance)
  initiation_composition_section = reference_goal[/^### Phone recovery initiation composition\n(.*?)(?=^### |^## |\z)/m, 1].to_s
  initiation_participants = initiation_composition_section.scan(/`([^`]+)`/).flatten.select { |value| value.end_with?("/postgres") }.sort
  expected_initiation_participants = %w[audit/postgres capability/postgres identity/otp/postgres identity/postgres identity/risk/postgres outbox/postgres]
  errors << "identity/reference phone recovery initiation participant set is not exact" unless initiation_participants == expected_initiation_participants
  phone_composition_section = reference_goal[/^### Phone recovery completion composition\n(.*?)(?=^### |^## |\z)/m, 1].to_s
  concrete_participants = phone_composition_section.scan(/`([^`]+)`/).flatten.select { |value| value.end_with?("/postgres") }.sort
  expected_participants = %w[capability/postgres identity/otp/postgres identity/password/postgres identity/risk/postgres identity/session/postgres]
  errors << "identity/reference phone recovery participant set is not exact" unless concrete_participants == expected_participants
  expected_reference_roles = %w[
    tx.capability.apply tx.capability.finalize tx.capability.issue
    tx.capability.recover tx.capability.reserve tx.capability.validate
    tx.captcha.apply tx.captcha.finalize tx.captcha.reserve
    tx.command.aborted tx.command.committed tx.command.conflict tx.command.expired
    tx.command.first tx.command.live tx.foundation tx.otp.apply tx.otp.attempt
    tx.otp.check tx.otp.finalize tx.otp.issue tx.otp.recover tx.otp.release
    tx.otp.reserve tx.risk_evidence.apply tx.risk_evidence.finalize
    tx.risk_evidence.issue tx.risk_evidence.recover tx.risk_evidence.release
    tx.risk_evidence.reserve
    tx.uow.begin tx.uow.commit tx.uow.contributor tx.uow.enlist tx.uow.locks
    tx.uow.query tx.uow.reserve tx.uow.rollback
  ]
  reference_roles = applicability.fetch("identity/reference").fetch("transaction")
  errors << "identity/reference phone recovery transaction applicability drifted" unless reference_roles == expected_reference_roles
  end_state_atomicity = "identity.phone uses one coordinator unit of work to reserve identity.risk/postgres evidence, OTP and capability, then atomically finalize them with the password mutation, session invalidation and command result; unknown remains reserved for authoritative recovery"
  errors << "END_STATE retains split RiskEvidence consumption" unless normalized_end_state.include?(end_state_atomicity)
  end_state_initiation = "identity.phone atomically reserves, applies and finalizes initiation-only RiskEvidence with OTP challenge and reset capability issuance; completion requires a separate completion-only artifact"
  errors << "END_STATE omits phase-distinct initiation atomicity" unless normalized_end_state.include?(end_state_initiation)
  risk_issuance = "Issuance MUST persist the one-use record through an injected durable contributor; the reference composition supplies `identity/risk/postgres`; an immutable bearer without that durable `issued` row is not valid RiskEvidence"
  errors << "identity/risk permits non-durable RiskEvidence issuance" unless normalized_risk_goal.include?(risk_issuance)
  canonical_risk_issuance = "`identity.risk.evaluate` is the canonical RiskEvidence issuance operation"
  errors << "identity/risk lacks canonical RiskEvidence issuance ownership" unless normalized_risk_goal.include?(canonical_risk_issuance)
  risk_issuance_outcomes = "Denied MUST return no reference; Failed MUST prove no issued row; Unknown MUST return no reference and recover the same command before any retry; same-command and same-fingerprint replay MUST return the exact recorded result without evaluating providers or issuing another artifact"
  errors << "identity/risk lacks closed issuance outcomes" unless normalized_risk_goal.include?(risk_issuance_outcomes)
  risk_phase_catalog = "The phase catalog MUST be exactly `none`, `phone-reset-initiation`, and `phone-reset-completion`; `none` is non-issuing"
  errors << "identity/risk issuance phase catalog is not closed" unless normalized_risk_goal.include?(risk_phase_catalog)
  risk_purpose_derivation = "Purpose MUST be derived exclusively from the issuing phase. Unknown/unsupported phases, a purpose with `none`, and every caller-supplied purpose MUST deny before provider evaluation or state access"
  errors << "identity/risk permits caller-selected issuance purpose" unless normalized_risk_goal.include?(risk_purpose_derivation)
  risk_issuance_secrecy = "The result MUST expose only an opaque RiskEvidence reference and safe purpose, issued-at, expires-at, and one-use metadata; raw signals, provider evidence, decision internals, embedded evidence payloads, keyed digests, signatures, and journal identifiers MUST NOT cross the public contract"
  errors << "identity/risk permits RiskEvidence result leakage" unless normalized_risk_goal.include?(risk_issuance_secrecy)
  risk_phase_distinction = "Phone reset initiation and completion MUST receive separate artifacts with purposes `phone-password-reset-initiate` and `phone-password-reset-complete`; their references, keyed digests, reservations, and terminal records MUST remain distinct"
  errors << "identity/risk lacks phase-distinct RiskEvidence issuance" unless normalized_risk_goal.include?(risk_phase_distinction)
  risk_postgres_issue_outcomes = "Issue MUST enlist in the identity command unit of work and atomically persist the exact `issued` row and committed command result before any opaque reference is returned"
  errors << "identity/risk/postgres lacks authoritative issuance commit" unless normalized_risk_postgres_goal.include?(risk_postgres_issue_outcomes)
  risk_postgres_phase_distinction = "The two phone-reset purposes MUST use distinct journal rows and keyed digests; one purpose MUST NOT validate, reserve, replay, or substitute for the other"
  errors << "identity/risk/postgres lacks phase-distinct RiskEvidence persistence" unless normalized_risk_postgres_goal.include?(risk_postgres_phase_distinction)

  required_risk_postgres_roles = %w[
    tx.risk_evidence.apply tx.risk_evidence.finalize tx.risk_evidence.issue
    tx.risk_evidence.recover tx.risk_evidence.release tx.risk_evidence.reserve
  ]
  risk_postgres_roles = applicability.fetch("identity/risk/postgres").fetch("transaction")
  missing_risk_postgres_roles = required_risk_postgres_roles - risk_postgres_roles
  unless missing_risk_postgres_roles.empty?
    errors << "identity/risk/postgres omits RiskEvidence applicability: #{missing_risk_postgres_roles.join(', ')}"
  end
  required_phone_roles = required_risk_postgres_roles - ["tx.risk_evidence.issue"]
  phone_roles = applicability.fetch("identity/phone").fetch("transaction")
  missing_phone_roles = required_phone_roles - phone_roles
  unless missing_phone_roles.empty?
    errors << "identity/phone omits RiskEvidence applicability: #{missing_phone_roles.join(', ')}"
  end
  unless applicability.fetch("identity/risk").fetch("transaction").include?("tx.risk_evidence.issue")
    errors << "identity/risk omits RiskEvidence issuance applicability"
  end
  expected_risk_issuance_roles = %w[
    tx.captcha.issue tx.captcha.reconcile tx.command.aborted tx.command.committed
    tx.command.conflict tx.command.expired tx.command.first tx.command.live tx.foundation tx.risk_evidence.issue
    tx.uow.begin tx.uow.commit tx.uow.contributor tx.uow.enlist tx.uow.locks
    tx.uow.query tx.uow.reserve tx.uow.rollback
  ]
  unless applicability.fetch("identity/risk").fetch("transaction") == expected_risk_issuance_roles
    errors << "identity/risk issuance transaction applicability drifted"
  end
  transaction_issuance = "Evidence-producing `identity.risk.evaluate` MUST enlist `identity/risk/postgres` in the identity command unit of work and atomically commit `tx.risk_evidence.issue` with the command result before returning an opaque reference"
  errors << "transaction contract omits canonical RiskEvidence issuance orchestration" unless normalized_transaction.include?(transaction_issuance)
  reference_issuance = "The reference composition MUST invoke `identity.risk.evaluate` for both phone-reset phases and MUST return only its opaque reference and safe freshness/one-use metadata; raw signals, provider evidence, embedded evidence payloads, digests, signatures, journal identifiers, and persistence records MUST NOT cross into `identity/phone`"
  errors << "identity/reference lacks non-leaking RiskEvidence issuance composition" unless normalized_reference_goal.include?(reference_issuance)
  end_state_issuance = "`identity.risk.evaluate` is the sole issuance operation for both phone-reset phases, persists `tx.risk_evidence.issue` before returning, and exposes only an opaque reference with safe freshness and one-use metadata"
  errors << "END_STATE omits canonical RiskEvidence issuance operation" unless normalized_end_state.include?(end_state_issuance)
  errors
end

def expect_risk_evidence_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("RiskEvidence negative fixture #{label} was accepted") if errors.empty?
  unless errors.include?(expected_error)
    fail_check("RiskEvidence negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}")
  end
end

def captcha_replay_contract_errors(transaction_contract:, configuration:, risk_goal:, risk_postgres_goal:, captcha_goal:, adapter_goals:, applicability:)
  errors = []
  normalized_transaction = transaction_contract.gsub(/\s+/, " ")
  normalized_risk = risk_goal.gsub(/\s+/, " ")
  normalized_postgres = risk_postgres_goal.gsub(/\s+/, " ")
  normalized_captcha = captcha_goal.gsub(/\s+/, " ")
  required_derivation = "The CAPTCHA replay fingerprint MUST be HMAC-SHA-256 under `secrets.captcha_replay_digest_key` over the ASCII domain `identity-captcha-replay-v1`"
  errors << "CAPTCHA replay fingerprint derivation is not exact" unless normalized_transaction.include?(required_derivation)
  required_uniqueness = "A unique constraint on tenant, provider, site ID, profile/configuration version, and replay fingerprint MUST give exactly one issuance command authority"
  errors << "CAPTCHA replay fingerprint is not durably unique" unless normalized_transaction.include?(required_uniqueness)
  required_tombstone = "Replay tombstones MUST survive evidence payload erasure and unresolved commands have no time-based release"
  errors << "CAPTCHA replay tombstone retention is incomplete" unless normalized_transaction.include?(required_tombstone)
  errors << "identity/risk does not own CAPTCHA replay derivation" unless normalized_risk.include?("`identity/risk` MUST derive the replay fingerprint itself from the raw provider token and trusted profile scope")
  errors << "identity/risk/postgres lacks CAPTCHA replay uniqueness ownership" unless normalized_postgres.include?("The adapter MUST enforce one durable replay-fingerprint winner across all issuance commands")
  errors << "CAPTCHA core permits caller-selected replay identity" unless normalized_captcha.include?("No caller or provider adapter may supply, override, or select the authoritative replay fingerprint")
  adapter_goals.each do |unit, goal|
    normalized = goal.gsub(/\s+/, " ")
    unless normalized.include?("The adapter MUST NOT derive or return the authoritative replay fingerprint")
      errors << "#{unit} lacks uniform CAPTCHA replay-fingerprint boundary"
    end
  end
  replay_row = configuration_row(configuration, "struct:ref.captcha.replay_fingerprint")
  unless replay_row.include?("algorithm = HMAC-SHA-256") && replay_row.include?("domain = identity-captcha-replay-v1") && replay_row.include?("owner = identity/risk")
    errors << "CAPTCHA replay-fingerprint configuration is not closed"
  end
  errors << "CAPTCHA replay tombstone retention configuration is missing" if configuration_row(configuration, "captcha.replay_tombstone_retention").empty?
  errors << "CAPTCHA replay digest key configuration is missing" if configuration_row(configuration, "secrets.captcha_replay_digest_key").empty?
  {
    "identity/risk" => %w[ref.captcha.replay_tombstone_retention ref.secrets.captcha_replay_digest_key ref.struct:ref.captcha.replay_fingerprint],
    "identity/risk/postgres" => %w[ref.captcha.replay_tombstone_retention ref.struct:ref.captcha.replay_fingerprint],
    "identity/risk/captcha" => %w[ref.struct:ref.captcha.replay_fingerprint],
    "identity/reference" => %w[ref.captcha.replay_tombstone_retention ref.secrets.captcha_replay_digest_key ref.struct:ref.captcha.replay_fingerprint]
  }.each do |unit, required|
    missing = required - applicability.fetch(unit).fetch("configuration")
    errors << "#{unit} omits CAPTCHA replay configuration applicability" unless missing.empty?
  end
  expected_onetap_roles = %w[tx.captcha.apply tx.captcha.finalize tx.captcha.reserve]
  onetap_roles = applicability.fetch("identity/oauth/onetap").fetch("transaction")
  unless (onetap_roles & expected_onetap_roles) == expected_onetap_roles
    errors << "identity/oauth/onetap omits CAPTCHA transaction applicability"
  end
  errors
end

def expect_captcha_replay_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("CAPTCHA replay negative fixture #{label} was accepted") if errors.empty?
  fail_check("CAPTCHA replay negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}") unless errors.include?(expected_error)
end

def scim_bulk_graph_contract_errors(transaction_contract:, scim_goal:)
  normalized_transaction = transaction_contract.gsub(/\s+/, " ")
  normalized_goal = scim_goal.gsub(/\s+/, " ")
  errors = []
  {
    "Admission MUST build the complete bounded dependency graph, reject unknown/cross-request references, compute SCCs, and persist the graph plus its deterministic execution plan before the first mutation" =>
      "SCIM Bulk graph plan is not durably admitted",
    "The SCC condensation graph executes in topological order; ties use the lowest original operation index" =>
      "SCIM Bulk graph order is not deterministic",
    "A cyclic SCC MUST preallocate final resource IDs for all of its create members" =>
      "SCIM Bulk circular references lack stable resource identity",
    "Its child results and SCC checkpoint commit atomically; any member failure rolls back the SCC" =>
      "SCIM Bulk circular component can partially commit",
    "Each acyclic singleton commits independently through the unit of work; each cyclic SCC commits through the bounded atomic SCC rule above" =>
      "SCIM Bulk execution boundary is not closed"
  }.each { |required, error| errors << error unless normalized_transaction.include?(required) }
  goal_rule = "build and durably persist the complete bounded graph and deterministic SCC plan before mutation, preallocate stable final resource IDs for create members, execute the SCC condensation graph topologically with original-index tie breaking, commit acyclic children independently, and commit each circular SCC as one bounded transaction with deferred within-SCC reference checks"
  errors << "SCIM goal omits bounded forward/circular graph execution" unless normalized_goal.include?(goal_rule)
  errors << "SCIM Bulk retains success-before-start contradiction" if normalized_transaction.include?("Only conclusively successful dependencies permit a child to enter `in-progress`")
  errors
end

def expect_scim_bulk_graph_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("SCIM Bulk graph negative fixture #{label} was accepted") if errors.empty?
  fail_check("SCIM Bulk graph negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}") unless errors.include?(expected_error)
end

def scim_rfc_contract_errors(api_operations:, protocol:, configuration:, transaction_contract:, scim_goal:, scim_postgres_goal:, applicability:, public_contract_fragment:, acceptance_profile:)
  errors = []
  normalized_api = api_operations.gsub(/\s+/, " ")
  normalized_protocol = protocol.gsub(/\s+/, " ")
  normalized_configuration = configuration.gsub(/\s+/, " ")
  normalized_transaction = transaction_contract.gsub(/\s+/, " ")
  normalized_goal = scim_goal.gsub(/\s+/, " ")
  normalized_postgres = scim_postgres_goal.gsub(/\s+/, " ")

  {
    "| `identity.scim.search` | `POST` | `/scim/v2/.search` |" => "SCIM generic POST search route is missing",
    "| `identity.scim.user-search` | `POST` | `/scim/v2/Users/.search` |" => "SCIM User POST search route is missing",
    "| `identity.scim.group-search` | `POST` | `/scim/v2/Groups/.search` |" => "SCIM Group POST search route is missing"
  }.each { |required, error| errors << error unless normalized_api.include?(required) }
  errors << "SCIM discovery collections permit bare arrays" unless normalized_protocol.include?("The Schemas and ResourceTypes collection endpoints also return RFC ListResponse messages; bare arrays are not conforming responses")
  errors << "SCIM scimType optionality is not exact" unless normalized_protocol.include?("`scimType` is OPTIONAL") && normalized_protocol.include?("errors such as not-found that have no registered type MUST omit it")
  external_id_rule = "`externalId` has RFC 7643 `uniqueness: none`"
  errors << "SCIM externalId protocol uniqueness drifted" unless normalized_protocol.include?(external_id_rule)
  errors << "SCIM externalId goal permits uniqueness" unless normalized_goal.include?("`externalId` MUST retain RFC 7643 `uniqueness: none`")
  errors << "SCIM PostgreSQL externalId constraint is not non-unique" unless normalized_postgres.include?("Equal values MAY identify multiple resources and MUST NOT be rejected by a unique constraint")
  errors << "SCIM externalId configuration is not non-unique" unless normalized_configuration.include?("non-unique lookup partition: tenant + organization + provider connection + resource type") && normalized_configuration.include?("RFC `uniqueness: none`; no unique constraint")
  errors << "SCIM failOnErrors maximum is not exactly 100" unless normalized_configuration.include?("exact request maximum 100") && normalized_protocol.include?("a larger value is rejected before admission")
  errors << "SCIM DELETE configuration makes extension keys authoritative" unless normalized_configuration.include?("the same connection, route, target and precondition fingerprint replays the original DELETE result with no extension key or with any newly supplied extension key") && normalized_configuration.include?("reuse of one extension key with a changed fingerprint conflicts")
  errors << "SCIM headerless DELETE replay is not server-owned" unless normalized_transaction.include?("For SCIM DELETE, admission MUST persist a server-owned replay lookup") && normalized_transaction.include?("A headerless retry with that identical lookup MUST recover the original terminal command")

  core_events = applicability.fetch("scim").fetch("security_events")
  organization_events = applicability.fetch("scim/organization").fetch("security_events")
  bulk_events = %w[identity.scim.bulk_admit identity.scim.bulk_apply_child identity.scim.bulk_skip_child]
  errors << "core SCIM does not own every Bulk audit action" unless (bulk_events - core_events).empty?
  errors << "SCIM organization still owns Bulk audit actions" unless organization_events == ["none"]

  scim_unit = public_contract_fragment.fetch("units").find { |row| row.fetch("package_name") == "scim" }
  types = scim_unit.fetch("types").to_h { |row| [row.fetch("name"), row] }
  operations = public_contract_fragment.fetch("operations").select { |row| row.fetch("id").start_with?("identity.scim.") }.to_h { |row| [row.fetch("id"), row] }
  server_methods = scim_unit.fetch("interfaces").find { |row| row.fetch("name") == "Server" }.fetch("methods").map { |row| row.fetch("name") }

  list_schemas = types.fetch("ListResponse").fetch("fields").find { |field| field.fetch("name") == "Schemas" }
  errors << "SCIM ListResponse lacks its exact schema URN" unless list_schemas.fetch("required") && list_schemas.fetch("bounds").include?("urn:ietf:params:scim:api:messages:2.0:ListResponse")
  writable_fields = types.fetch("WritableResource").fetch("fields").map { |field| field.fetch("name") }
  errors << "SCIM writable resource includes server-generated fields" unless writable_fields == %w[Schemas ExternalID Attributes]
  errors << "SCIM writable aliases are incomplete" unless types.dig("UserInput", "underlying") == "WritableResource" && types.dig("GroupInput", "underlying") == "WritableResource"
  errors << "SCIM SearchRequest schema is not exact" unless types.fetch("SearchRequest").fetch("fields").any? { |field| field.fetch("name") == "Schemas" && field.fetch("bounds").include?("urn:ietf:params:scim:api:messages:2.0:SearchRequest") }

  expected_search = {
    "identity.scim.search" => ["Search", "/scim/v2/.search"],
    "identity.scim.user-search" => ["UserSearch", "/scim/v2/Users/.search"],
    "identity.scim.group-search" => ["GroupSearch", "/scim/v2/Groups/.search"]
  }
  expected_search.each do |id, (method, path)|
    operation = operations[id]
    errors << "SCIM public contract omits #{id}" unless operation && operation.dig("transport", "http_method") == "POST" && operation.dig("transport", "http_path") == path
    errors << "SCIM Server omits #{method}" unless server_methods.include?(method)
  end
  errors << "SCIM Group search omits the organization mapping collaborator" unless operations.fetch("identity.scim.group-search").fetch("collaborators") == ["scim/organization"]

  %w[identity.scim.user-create identity.scim.user-replace].each do |id|
    field = operations.fetch(id).dig("request", "fields").find { |row| row.fetch("name") == "User" }
    errors << "#{id} requires a response resource as input" unless field.fetch("type") == "scim.UserInput"
  end
  %w[identity.scim.group-create identity.scim.group-replace].each do |id|
    field = operations.fetch(id).dig("request", "fields").find { |row| row.fetch("name") == "Group" }
    errors << "#{id} requires a response resource as input" unless field.fetch("type") == "scim.GroupInput"
  end
  bulk_fields = types.fetch("BulkOperation").fetch("fields").to_h { |row| [row.fetch("name"), row] }
  bulk_result_fields = types.fetch("BulkOperationResult").fetch("fields").to_h { |row| [row.fetch("name"), row] }
  errors << "SCIM Bulk request does not enforce writable method-specific data" unless bulk_fields.dig("Data", "type") == "BulkData" && bulk_fields.dig("BulkID", "semantics").include?("Required")
  errors << "SCIM BulkResponse operation omits method or conditional bulkId" unless bulk_result_fields.dig("Method", "required") && bulk_result_fields.dig("BulkID", "semantics").include?("Echoed exactly")
  errors << "SCIM BulkResponse lacks its exact schema URN" unless types.fetch("BulkResponse").fetch("fields").any? { |field| field.fetch("name") == "Schemas" && field.fetch("bounds").include?("urn:ietf:params:scim:api:messages:2.0:BulkResponse") }
  fail_on_errors = operations.fetch("identity.scim.bulk").dig("request", "fields").find { |row| row.fetch("name") == "FailOnErrors" }
  errors << "SCIM Bulk public contract does not cap failOnErrors at 100" unless fail_on_errors.fetch("bounds").include?("exact configured maximum 100")
  revoke_fields = operations.fetch("identity.scim.connection-token-revoke").dig("request", "fields")
  errors << "SCIM token revocation request lacks durable Command identity" unless revoke_fields.any? { |row| row.fetch("name") == "Command" && row.fetch("type") == "identity.Command" && row.fetch("required") == true }
  revoke_command_fields = types.fetch("ConnectionTokenRevokeCommand").fetch("fields")
  errors << "SCIM token revocation command lacks durable Command identity" unless revoke_command_fields.any? { |row| row.fetch("name") == "Command" && row.fetch("type") == "identity.Command" && row.fetch("required") == true }
  errors << "SCIM discovery result contracts are not ListResponse" unless %w[identity.scim.schemas-list identity.scim.resource-types-list].all? { |id| operations.fetch(id).dig("result", "fields").any? { |row| row.fetch("name") == "Response" && row.fetch("type") == "scim.ListResponse" } }
  scim_type_fields = operations.values.flat_map { |operation| operation.dig("errors", "variants") || [] }.flat_map { |variant| variant.fetch("fields", []) }.select { |field| field.fetch("name") == "SCIMType" }
  errors << "SCIM operation errors require scimType" unless scim_type_fields.any? && scim_type_fields.all? { |field| field.fetch("required") == false && field.fetch("zero_value") == "absent" }

  expected_acceptance_operations = %w[
    identity.scim.service-provider-config identity.scim.schemas-list identity.scim.schema-get
    identity.scim.resource-types-list identity.scim.resource-type-get identity.scim.search
    identity.scim.user-list identity.scim.user-search identity.scim.user-create identity.scim.user-get
    identity.scim.user-replace identity.scim.user-patch identity.scim.user-delete identity.scim.group-list
    identity.scim.group-search identity.scim.group-create identity.scim.group-get identity.scim.group-replace
    identity.scim.group-patch identity.scim.group-delete identity.scim.bulk
  ]
  errors << "SCIM conformance acceptance does not cover every advertised protocol operation" unless acceptance_profile.fetch("operation_ids") == expected_acceptance_operations
  required_claims = %w[ListResponse id meta externalId scimType method bulkId failOnErrors ETag filter sort pagination]
  acceptance_text = [acceptance_profile.fetch("initial_state"), acceptance_profile.fetch("invariant")].join(" ")
  errors << "SCIM conformance acceptance omits exact RFC claims" unless required_claims.all? { |claim| acceptance_text.include?(claim) }
  errors
end

def expect_scim_rfc_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("SCIM RFC negative fixture #{label} was accepted") if errors.empty?
  fail_check("SCIM RFC negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}") unless errors.include?(expected_error)
end

def otp_contract_errors(transaction_contract:, otp_goal:, otp_postgres_goal:, workflow_goals:, end_state:, applicability:)
  errors = []
  normalized_transaction = transaction_contract.gsub(/\s+/, " ")
  {
    "OTP participant states are `issued`, `reserved`, `finalized`, `released`, `expired`, `revoked`, and `exhausted`" =>
      "OTP participant state set is not closed",
    "Legal transitions are only `absent` to `issued`, `issued` to `issued` with one durable failed-attempt increment, `issued` to `reserved`, `issued` to `expired`, `revoked`, or `exhausted`, `reserved` to `finalized`, and `reserved` to `released` or `revoked`" =>
      "OTP participant transitions are incomplete",
    "The binding MUST include tenant, purpose, subject or channel target, challenge ID, workflow target, issued/expiry database time, attempt budget, keyed-code-digest version, and issuance fingerprint" =>
      "OTP participant omits exact purpose binding",
    "A replacement MAY transition an earlier `issued` row to `revoked` in the same issue transaction, but MUST NOT replace or revoke a `reserved` row without authoritative recovery of its owning command" =>
      "OTP replacement can invalidate an active reservation",
    "then bind consuming command ID, command fingerprint, reservation generation, and target versions" =>
      "OTP reservation omits command generation binding",
    "Two commands MAY perform a non-authoritative digest precheck, but exactly one command MAY transition the same `issued` row to `reserved`; every other command receives the same non-enumerating denial" =>
      "OTP reservation lacks one-winner concurrency",
    "The same command, fingerprint, and live generation replay the stable reservation without decrementing attempts or rerunning the workflow" =>
      "OTP same-command replay is not idempotent",
    "Expired-owner takeover MUST CAS-rebind the OTP reservation from the exact prior generation to the new command generation in the coordinator reservation transaction with every other reserved one-time participant" =>
      "OTP takeover lacks generation CAS",
    "`tx.otp.apply` MUST recheck reservation generation, purpose and all bound versions inside the domain transaction; `tx.otp.finalize` MUST transition `reserved` to `finalized` in the same commit as the owning mutation, session issuance or invalidation, outbox/audit, and command result" =>
      "OTP apply/finalize is not atomic with its workflow",
    "A retryable rollback retains `reserved` only for the same live command generation; authoritative non-commit MAY use `tx.otp.release`, which is terminal and requires a newly issued OTP" =>
      "OTP rollback/release can permit replay",
    "An ambiguous commit MUST leave OTP `reserved`, return `Unknown`, and use `tx.otp.recover`; timeout, lease loss, challenge expiry, or cleanup MUST NOT release it" =>
      "OTP unknown recovery is not fail-closed",
    "After the later of original OTP expiry and `command.result_retention`, cleanup MAY crypto-shred terminal payload/linkage but MUST retain a tenant/purpose/keyed-digest/key-version/original-expiry/terminal-state tombstone with no time-based deletion until every referenced digest key is retired and no code can validate before lookup" =>
      "OTP terminal cleanup can reopen replay"
  }.each { |required, error| errors << error unless normalized_transaction.include?(required) }

  normalized_otp_goal = otp_goal.gsub(/\s+/, " ")
  normalized_otp_postgres_goal = otp_postgres_goal.gsub(/\s+/, " ")
  errors << "identity/otp lacks durable participant ownership" unless normalized_otp_goal.include?("Every consuming workflow MUST treat OTP precheck as non-authoritative and use the durable issue/attempt/reserve/apply/finalize/release/recover protocol")
  errors << "identity/otp/postgres lacks exact durable OTP ownership" unless normalized_otp_postgres_goal.include?("The adapter owns the durable OTP participant state machine: `issued`, `reserved`, `finalized`, `released`, `expired`, `revoked`, and `exhausted`")
  errors << "identity/otp/postgres lacks generation-safe unknown recovery" unless normalized_otp_postgres_goal.include?("Takeover MUST CAS the exact prior generation with every one-time participant; unknown completion remains `reserved` until authoritative recovery")

  workflow_phrases = {
    "identity/email" => "When handling `identity.otp.email-verify`, `identity.otp.email-change-confirm`, or the optional current-address OTP branch of `identity.otp.email-change-request`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the public `identity/otp` contributor contract in the same coordinator unit of work as its owning mutation. The core MUST NOT import, require, or name a concrete OTP persistence adapter; reference composition selects that adapter. Non-OTP email operations MUST NOT enlist an OTP participant",
    "identity/password" => "When handling `identity.otp.password-reset` or `identity.phone.password-reset-complete`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the public `identity/otp` contributor contract in the same coordinator unit of work as its owning mutation. The core MUST NOT import, require, or name a concrete OTP persistence adapter; reference composition selects that adapter. Signup, signin, password change, and capability-only reset MUST NOT enlist an OTP participant",
    "identity/phone" => "When handling `identity.phone.verify`, `identity.phone.signin`, `identity.phone.update`, or `identity.phone.password-reset-complete`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the injected OTP transaction contributor in the same coordinator unit of work as its owning mutation. Non-consuming initiation/removal operations MUST NOT enlist an OTP participant",
    "identity/mfa" => "When handling `identity.mfa.otp-verify`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the public `identity/otp` contributor contract in the same coordinator unit of work as its owning mutation. The core MUST NOT import, require, or name a concrete OTP persistence adapter; reference composition selects that adapter. Other MFA methods and OTP-send initiation MUST NOT enlist an OTP consumption participant"
  }
  workflow_goals.each do |unit, goal|
    errors << "#{unit} lacks scoped atomic OTP workflow ownership" unless goal.gsub(/\s+/, " ").include?(workflow_phrases.fetch(unit))
  end
  end_state_phrase = "Every OTP-consuming signin, email verification/change, password reset, phone recovery, and MFA completion MUST reserve and finalize its purpose-bound OTP through the same coordinator unit of work as the owning mutation and session effect"
  errors << "END_STATE omits atomic OTP workflow closure" unless end_state.gsub(/\s+/, " ").include?(end_state_phrase)

  full_roles = %w[
    tx.otp.apply tx.otp.attempt tx.otp.check tx.otp.finalize tx.otp.issue
    tx.otp.recover tx.otp.release tx.otp.reserve
  ]
  consume_roles = %w[tx.otp.apply tx.otp.finalize tx.otp.recover tx.otp.release tx.otp.reserve]
  expected = {
    "identity/otp" => full_roles,
    "identity/otp/postgres" => full_roles,
    "identity/phone" => full_roles,
    "identity/reference" => full_roles,
    "identity/mfa" => full_roles,
    "identity/email" => consume_roles,
    "identity/password" => consume_roles
  }
  expected.each do |unit, required_roles|
    actual = applicability.fetch(unit).fetch("transaction").grep(/\Atx\.otp\./)
    errors << "#{unit} OTP applicability drifted" unless actual == required_roles
  end
  unexpected = applicability.filter_map do |unit, entry|
    unit if !expected.key?(unit) && entry.fetch("transaction").any? { |role| role.start_with?("tx.otp.") }
  end
  errors << "unexpected direct OTP workflow applicability: #{unexpected.join(',')}" unless unexpected.empty?
  errors
end

def expect_otp_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("OTP negative fixture #{label} was accepted") if errors.empty?
  fail_check("OTP negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}") unless errors.include?(expected_error)
end

def cross_cutting_remediation_errors(transaction_contract:, configuration:, reference_profile:, lifecycle_contract:,
                                      api_operations:, security_events:, passkey_goal:, applicability:,
                                      acceptance_catalog:, passkey_schema:, captcha_schema:, privacy_export_schema:, otp_schema:)
  errors = []
  normalized_transaction = transaction_contract.gsub(/\s+/, " ")
  normalized_profile = reference_profile.gsub(/\s+/, " ")
  normalized_lifecycle = lifecycle_contract.gsub(/\s+/, " ")
  normalized_passkey_goal = passkey_goal.gsub(/\s+/, " ")

  privacy_row = configuration_row(configuration, "struct:ref.identity.privacy_export")
  if privacy_row.include?("one_exported_repeatable_read") || normalized_profile.include?("one exported `REPEATABLE READ` snapshot")
    errors << "privacy export retains a non-restart-safe exported snapshot"
  end
  unless privacy_row.include?("postgres_snapshot = append_only_versioned_projection_or_transaction_staged_immutable_fragment")
    errors << "privacy export configuration omits restart-safe PostgreSQL snapshot authority"
  end
  unless normalized_profile.include?("PostgreSQL contributors MUST read from an append-only/versioned projection at the recorded checkpoint or use an immutable bounded fragment staged during the request transaction")
    errors << "reference profile omits restart-safe privacy-export reads"
  end
  unless normalized_lifecycle.include?("A restartable worker MUST NOT retain or depend on a long-lived exported PostgreSQL snapshot") &&
         normalized_lifecycle.include?("append-only/versioned projection capable of reproducing its exact recorded checkpoint, or atomically stage an immutable bounded fragment during the request transaction")
    errors << "privacy-export lifecycle authority is not restart-safe"
  end
  privacy_artifact = acceptance_catalog.fetch("artifacts").find do |artifact|
    artifact.fetch("artifact_id") == "privacy-export-lifecycle-report"
  end
  privacy_evidence = %w[
    contributor_checkpoint_count restart_resumed_contributor_count worker_restart_count
    long_lived_postgres_snapshot_count mixed_cut_rejected snapshot_version_vector_digest
    restart_reconstruction_digest
  ]
  unless privacy_artifact && (privacy_evidence - privacy_artifact.fetch("artifact_evidence_fields")).empty?
    errors << "privacy-export acceptance omits restart-safe evidence"
  end
  privacy_outcomes = privacy_artifact&.fetch("required_observations", [])&.map { |row| row.fetch("expected_outcome") }.to_a.join(" ")
  unless privacy_outcomes.include?("restart reconstructs every contributor at the recorded checkpoint without a long-lived exported PostgreSQL snapshot")
    errors << "privacy-export acceptance omits restart behavior"
  end
  privacy_schema_evidence = privacy_export_schema.dig("$defs", "artifact_evidence")
  privacy_schema_properties = privacy_schema_evidence&.fetch("properties", {})&.keys.to_a
  privacy_schema_required = privacy_schema_evidence&.fetch("required", []).to_a
  unless privacy_evidence.all? { |field| privacy_schema_properties.include?(field) && privacy_schema_required.include?(field) }
    errors << "privacy-export acceptance schema omits restart-safe evidence"
  end
  privacy_rules = privacy_schema_evidence&.fetch("x-semantic-rules", []).to_a
  privacy_rules_exact =
    privacy_rules.any? { |rule| rule["kind"] == "zero" && rule.fetch("fields", []).include?("long_lived_postgres_snapshot_count") } &&
    privacy_rules.include?({"kind" => "const", "field" => "worker_restart_count", "value" => 1}) &&
    privacy_rules.include?({"kind" => "equal", "left" => "restart_resumed_contributor_count", "right" => "contributor_checkpoint_count"}) &&
    privacy_rules.any? { |rule| rule["kind"] == "true" && rule.fetch("fields", []).include?("mixed_cut_rejected") }
  errors << "privacy-export acceptance semantic rules are incomplete" unless privacy_rules_exact

  otp_requirements = {
    "`tx.otp.attempt` MUST use a server-issued attempt ID bound to the tenant, purpose, challenge ID, consuming command ID, and canonical command fingerprint" =>
      "OTP wrong-code attempt lacks server-issued retry identity",
    "The wrong-code denial transaction is a narrow pre-reservation exception to `tx.uow.reserve`" =>
      "OTP wrong-code denial is not a narrow pre-reservation exception",
    "It MUST lock the command row and OTP row, verify the server-issued attempt ID and canonical command fingerprint, increment the durable attempt counter exactly once, transition to `exhausted` when the budget is reached, and store the stable `aborted` command result in the same commit" =>
      "OTP wrong-code denial does not atomically persist attempt and result",
    "After admission, the general `tx.uow.reserve` rule still rolls back every participant reservation on denial, error, or cancellation" =>
      "OTP denial exception weakens normal reservation rollback",
    "An ambiguous wrong-code denial commit MUST return `Unknown` and reconcile the same command and attempt ID on the primary before any retry" =>
      "OTP wrong-code denial lacks ambiguous-commit recovery"
  }
  otp_requirements.each { |required, error| errors << error unless normalized_transaction.include?(required) }
  otp_artifact = acceptance_catalog.fetch("artifacts").find do |artifact|
    artifact.fetch("artifact_id") == "otp-reservation-report"
  end
  otp_evidence = %w[
    attempt_id failed_attempt_count aborted_result_count duplicate_attempt_increment_count
    exhausted unknown_reconciliation_digest
  ]
  unless otp_artifact && (otp_evidence - otp_artifact.fetch("artifact_evidence_fields")).empty?
    errors << "OTP acceptance omits retry-safe denial evidence"
  end
  otp_outcomes = otp_artifact&.fetch("required_observations", [])&.map { |row| row.fetch("expected_outcome") }.to_a.join(" ")
  unless otp_outcomes.include?("server-issued attempt identity") &&
         otp_outcomes.include?("ambiguous denial commit returns unknown until primary reconciliation")
    errors << "OTP acceptance omits retry and ambiguous-denial behavior"
  end
  otp_schema_evidence = otp_schema.dig("$defs", "artifact_evidence")
  otp_schema_properties = otp_schema_evidence&.fetch("properties", {})&.keys.to_a
  otp_schema_required = otp_schema_evidence&.fetch("required", []).to_a
  unless otp_evidence.all? { |field| otp_schema_properties.include?(field) && otp_schema_required.include?(field) }
    errors << "OTP acceptance schema omits retry-safe denial evidence"
  end
  otp_rules = otp_schema_evidence&.fetch("x-semantic-rules", []).to_a
  otp_rules_exact =
    otp_rules.any? { |rule| rule["kind"] == "zero" && rule.fetch("fields", []).include?("duplicate_attempt_increment_count") } &&
    otp_rules.include?({"kind" => "equal", "left" => "aborted_result_count", "right" => "failed_attempt_count"}) &&
    otp_rules.any? { |rule| rule["kind"] == "true" && rule.fetch("fields", []).include?("exhausted") }
  errors << "OTP acceptance semantic rules are incomplete" unless otp_rules_exact

  captcha_row = configuration_row(configuration, "struct:ref.captcha.evidence")
  unless captcha_row.include?("flow_context = pre_auth_transaction_or_authenticated_subject_session_or_admin_actor")
    errors << "CAPTCHA evidence configuration omits flow-context variants"
  end
  captcha_binding = "The durable evidence row MUST bind tenant, exact subject or anonymous-flow ID, a flow-context variant of pre-auth transaction for unauthenticated flows or authenticated subject/session or administrator actor context for authenticated or administrative flows"
  errors << "CAPTCHA evidence mandates the wrong flow binding" unless normalized_transaction.include?(captcha_binding)
  captcha_recheck = "`tx.captcha.apply` MUST recheck command/generation ownership, PostgreSQL expiry, exact action, subject or anonymous flow, the applicable pre-auth or authenticated subject/session or administrator actor context"
  errors << "CAPTCHA apply omits flow-context recheck" unless normalized_transaction.include?(captcha_recheck)

  stateless_roles = applicability.fetch("identity/risk/captcha").fetch("transaction")
  durable_captcha_roles = %w[tx.captcha.apply tx.captcha.finalize tx.captcha.reserve]
  unless (stateless_roles & durable_captcha_roles).empty?
    errors << "stateless CAPTCHA verifier owns durable transaction roles"
  end
  postgres_roles = applicability.fetch("identity/risk/postgres").fetch("transaction")
  unless (postgres_roles & durable_captcha_roles) == durable_captcha_roles
    errors << "identity/risk/postgres omits durable CAPTCHA transaction roles"
  end
  captcha_artifact = acceptance_catalog.fetch("artifacts").find do |artifact|
    artifact.fetch("artifact_id") == "captcha-four-provider-report"
  end
  captcha_evidence = %w[
    protected_target_ids protected_target_results protected_target_count middleware_attached_target_count
    durable_reservation_count durable_apply_count durable_finalize_count stateless_verifier_durable_write_count
    flow_binding_digest durable_owner_digest provider_matrix_digest
  ]
  unless captcha_artifact && (captcha_evidence - captcha_artifact.fetch("artifact_evidence_fields")).empty?
    errors << "CAPTCHA acceptance omits exhaustive target and durable-owner evidence"
  end
  captcha_outcomes = captcha_artifact&.fetch("required_observations", [])&.map { |row| row.fetch("expected_outcome") }.to_a.join(" ")
  unless captcha_outcomes.include?("every configured CAPTCHA target from the canonical API attachment table has one middleware-attached result with its exact permitted flow contexts and attributable evidence") &&
         captcha_outcomes.include?("identity/risk/postgres alone reserves, applies, and finalizes durable evidence for every target while the stateless verifier performs no durable write")
    errors << "CAPTCHA acceptance omits exhaustive target and ownership cases"
  end
  captcha_schema_evidence = captcha_schema.dig("$defs", "artifact_evidence")
  captcha_schema_properties = captcha_schema_evidence&.fetch("properties", {})&.keys.to_a
  captcha_schema_required = captcha_schema_evidence&.fetch("required", []).to_a
  unless captcha_evidence.all? { |field| captcha_schema_properties.include?(field) && captcha_schema_required.include?(field) }
    errors << "CAPTCHA acceptance schema omits exhaustive target and durable-owner evidence"
  end
  captcha_target_row = api_operations.lines.find { |line| line.start_with?("| `identity.risk.captcha-verify` |") }
  captcha_target_ids = captcha_target_row.to_s.scan(/`([^`]+)`/).flatten
  captcha_target_ids.shift if captcha_target_ids.first == "identity.risk.captcha-verify"
  captcha_target_schema = captcha_schema_evidence&.dig("properties", "protected_target_ids")
  captcha_results_schema = captcha_schema_evidence&.dig("properties", "protected_target_results")
  captcha_result_variants = captcha_results_schema&.dig("items", "oneOf").to_a
  captcha_result_target_ids = captcha_result_variants.filter_map { |variant| variant.dig("properties", "target_id", "const") }
  captcha_results_exact = !captcha_target_ids.empty? && captcha_target_schema == {"const" => captcha_target_ids} &&
    captcha_results_schema&.fetch("minItems", nil) == captcha_target_ids.length &&
    captcha_results_schema&.fetch("maxItems", nil) == captcha_target_ids.length &&
    captcha_result_target_ids == captcha_target_ids &&
    captcha_result_variants.all? { |variant| variant.dig("properties", "middleware_attached", "const") == true }
  errors << "CAPTCHA acceptance schema target inventory is not exhaustive" unless captcha_results_exact
  captcha_rules = captcha_schema_evidence&.fetch("x-semantic-rules", []).to_a
  captcha_rules_exact =
    captcha_rules.any? { |rule| rule["kind"] == "zero" && rule.fetch("fields", []).include?("stateless_verifier_durable_write_count") } &&
    captcha_rules.include?({"kind" => "const", "field" => "protected_target_count", "value" => captcha_target_ids.length}) &&
    %w[middleware_attached_target_count durable_reservation_count durable_apply_count durable_finalize_count].all? do |field|
      captcha_rules.include?({"kind" => "equal", "left" => "protected_target_count", "right" => field})
    end
  errors << "CAPTCHA acceptance semantic rules are incomplete" unless captcha_rules_exact

  passkey_actions = %w[identity.passkey.create_credential identity.passkey.mark_compromised]
  passkey_actions.each do |action|
    errors << "passkey security taxonomy omits #{action}" unless security_events.include?("`#{action}`")
  end
  passkey_events = applicability.fetch("passkey").fetch("security_events")
  missing_passkey_actions = passkey_actions - passkey_events
  errors << "passkey applicability omits security actions: #{missing_passkey_actions.join(', ')}" unless missing_passkey_actions.empty?
  register_row = api_operations.lines.find { |line| line.start_with?("| `identity.passkey.register-verify` |") }.to_s
  signin_row = api_operations.lines.find { |line| line.start_with?("| `identity.passkey.signin-verify` |") }.to_s
  unless register_row.include?("emits exactly `identity.passkey.create_credential` when credential creation commits")
    errors << "passkey registration operation omits exact creation action"
  end
  unless signin_row.include?("emits exactly `identity.passkey.mark_compromised` when clone or counter evidence durably marks the credential compromised")
    errors << "passkey sign-in operation omits exact compromise action"
  end
  goal_mapping = "Creation MUST emit exactly `identity.passkey.create_credential`, compromise MUST emit exactly `identity.passkey.mark_compromised`"
  errors << "passkey goal omits exact creation and compromise actions" unless normalized_passkey_goal.include?(goal_mapping)

  passkey_artifact = acceptance_catalog.fetch("artifacts").find do |artifact|
    artifact.fetch("artifact_id") == "passkey-browser-ceremony-report"
  end
  required_evidence = %w[creation_event_count compromise_event_count ordinary_failure_compromise_event_count security_event_digest]
  unless passkey_artifact && (required_evidence - passkey_artifact.fetch("artifact_evidence_fields")).empty?
    errors << "passkey acceptance evidence omits security actions"
  end
  expected_outcomes = passkey_artifact&.fetch("required_observations", [])&.map { |row| row.fetch("expected_outcome") }.to_a.join(" ")
  unless passkey_actions.all? { |action| expected_outcomes.include?(action) }
    errors << "passkey acceptance invariant omits security-action mapping"
  end
  schema_evidence = passkey_schema.dig("$defs", "artifact_evidence")
  schema_properties = schema_evidence&.fetch("properties", {})&.keys.to_a
  schema_required = schema_evidence&.fetch("required", []).to_a
  unless required_evidence.all? { |field| schema_properties.include?(field) && schema_required.include?(field) }
    errors << "passkey acceptance schema omits security-action evidence"
  end
  passkey_rules = schema_evidence&.fetch("x-semantic-rules", []).to_a
  passkey_rules_exact =
    passkey_rules.any? { |rule| rule["kind"] == "zero" && rule.fetch("fields", []).include?("ordinary_failure_compromise_event_count") } &&
    %w[creation_event_count compromise_event_count].all? do |field|
      passkey_rules.include?({"kind" => "const", "field" => field, "value" => 1})
    end
  errors << "passkey acceptance semantic rules are incomplete" unless passkey_rules_exact
  errors
end

def expect_cross_cutting_fixture_rejection!(label, expected_error)
  errors = yield
  fail_check("cross-cutting remediation negative fixture #{label} was accepted") if errors.empty?
  unless errors.include?(expected_error)
    fail_check("cross-cutting remediation negative fixture #{label} missed #{expected_error}: #{errors.join('; ')}")
  end
end

def canonical_inventory_digest(items)
  Digest::SHA256.hexdigest(items.join("\n") + "\n")
end

def numbered_items(section)
  section.lines.filter_map { |line| line[/^\d+\. (.+?);?$/, 1] }
end

def metadata_values(body, label)
  entry = body[/^- #{Regexp.escape(label)}:(.*?)(?=^- |^## |\z)/m, 1].to_s
  entry.scan(/`([^`]+)`/).flatten
end

def goal_manifest_resolution(rows, manifest, bodies)
  errors = []
  resolved = {}
  errors << "goal manifest schema drifted" unless manifest.keys == %w[schema_version goals] && manifest["schema_version"] == 1
  entries = manifest["goals"]
  return [errors << "goal manifest goals are invalid", resolved] unless entries.is_a?(Array)

  expected_keys = %w[unit planning_path canonical_path sha256]
  errors << "goal manifest units do not exactly match inventory order" unless entries.map { |entry| entry["unit"] } == rows.map { |row| row[:unit] }
  entries_by_unit = entries.to_h { |entry| [entry["unit"], entry] }
  errors << "goal manifest contains duplicate units" unless entries_by_unit.length == entries.length
  allowed_paths = entries.flat_map { |entry| [entry["planning_path"], entry["canonical_path"]] }
  errors << "goal manifest contains duplicate paths" unless allowed_paths.uniq.length == allowed_paths.length

  rows.each do |row|
    entry = entries_by_unit[row[:unit]]
    next errors << "goal manifest omits #{row[:unit]}" unless entry
    errors << "goal manifest entry schema drifted for #{row[:unit]}" unless entry.keys == expected_keys
    planning = entry["planning_path"]
    canonical = entry["canonical_path"]
    errors << "goal manifest planning path is invalid for #{row[:unit]}" unless planning&.match?(%r{\Agoals/[a-z0-9-]+\.md\z})
    canonical_name = row[:unit].start_with?("primitive/") ? "GOAL_IDENTITY_CONTRACTS.md" : "GOAL.md"
    errors << "goal manifest canonical path drifted for #{row[:unit]}" unless canonical == "#{row[:module]}/.ai/#{canonical_name}"
    errors << "inventory goal path is not a canonical manifest location for #{row[:unit]}" unless [planning, canonical].include?(row[:goal])
    present = [planning, canonical].select { |path| bodies.key?(path) }
    errors << "goal is missing or duplicated for #{row[:unit]}" unless present == [row[:goal]]
    body = bodies[row[:goal]]
    next unless body
    errors << "goal semantic digest drifted for #{row[:unit]}" unless entry["sha256"]&.match?(/\A[0-9a-f]{64}\z/) && Digest::SHA256.hexdigest(body) == entry["sha256"]
    resolved[row[:unit]] = body
  end
  orphans = bodies.keys - allowed_paths
  errors << "orphan goal paths: #{orphans.sort.join(', ')}" unless orphans.empty?
  [errors, resolved]
end

def primitive_extension_inventory_errors(public_contracts:, rows:, goal_bodies:, ledger_units: nil)
  errors = []
  public_units = public_contracts.fetch("units", []).map { |row| row["unit"] }
  primitive_rows = rows.select { |row| row[:unit].start_with?("primitive/") }
  identity_rows = rows.reject { |row| row[:unit].start_with?("primitive/") }
  extension_requirements = public_contracts.dig("manifest_schema", "required_primitive_extensions")
  unless extension_requirements.is_a?(Array)
    return errors << "public contracts required primitive extensions are absent"
  end

  errors << "identity public-contract unit count drifted" unless public_units.length == EXPECTED_IDENTITY_UNITS
  errors << "identity inventory unit count drifted" unless identity_rows.length == EXPECTED_IDENTITY_UNITS
  errors << "primitive extensions leaked into the identity public-contract unit catalog" if public_units.any? { |unit| unit.to_s.start_with?("primitive/") }
  errors << "identity inventory/public-contract unit order drifted" unless identity_rows.map { |row| row[:unit] } == public_units
  errors << "required primitive-extension authority count drifted" unless extension_requirements.length == EXPECTED_PRIMITIVE_EXTENSION_AUTHORITIES

  required_keys = %w[unit extension_unit package_path depends_on consumers required_contract_sha256 required_symbols]
  derived_consumers_by_authority = {}
  extension_requirements.each do |requirement|
    authority = requirement["unit"]
    errors << "primitive extension authority schema drifted for #{authority}" unless requirement.keys == required_keys.sort
    errors << "primitive extension authority is invalid for #{authority}" unless authority.to_s.match?(%r{\A[a-z0-9-]+(?:/[a-z0-9-]+)*\z})
    extension_unit = requirement["extension_unit"]
    errors << "primitive extension scheduling unit is invalid for #{authority}" unless extension_unit.to_s.match?(%r{\Aprimitive/[a-z0-9-]+\z})
    errors << "primitive extension package path is invalid for #{authority}" unless requirement["package_path"].to_s.match?(%r{\Agithub\.com/faustbrian/golib/pkg/[a-z0-9/-]+\z})
    %w[depends_on consumers required_symbols].each do |field|
      values = requirement[field]
      errors << "primitive extension #{authority} #{field} is not sorted and unique" unless values.is_a?(Array) && values == values.sort_by(&:b).uniq
    end
    errors << "primitive extension #{authority} required digest is invalid" unless requirement["required_contract_sha256"].to_s.match?(/\Asha256:[0-9a-f]{64}\z/)
    derived_consumers = derived_primitive_consumers(public_contracts, requirement)
    derived_consumers_by_authority[authority] = requirement.fetch("consumers")
    missing_derived_consumers = derived_consumers - requirement.fetch("consumers")
    errors << "primitive extension #{authority} consumer derivation omits #{missing_derived_consumers}" unless missing_derived_consumers.empty?
  end

  authority_units = extension_requirements.map { |row| row["unit"] }
  errors << "primitive extension authorities are not sorted and unique" unless authority_units == authority_units.sort_by(&:b).uniq
  extension_units = extension_requirements.map { |row| row["extension_unit"] }.uniq
  errors << "primitive extension scheduling-unit count drifted" unless extension_units.length == EXPECTED_PRIMITIVE_EXTENSION_UNITS
  errors << "primitive extension inventory closure drifted" unless primitive_rows.map { |row| row[:unit] }.to_set == extension_units.to_set
  errors << "schedulable inventory count drifted" unless rows.length == EXPECTED_SCHEDULABLE_UNITS

  external_by_owner = public_contracts.fetch("external_contracts", []).to_h { |row| [row["owner"], row] }
  required_external_owners = public_contracts.fetch("external_contracts", []).select do |row|
    row["current_api_status"] == "requires-extension"
  end.map { |row| row["owner"] }
  errors << "requires-extension external-authority closure drifted" unless required_external_owners == authority_units

  authority_to_extension = extension_requirements.to_h { |row| [row["unit"], row["extension_unit"]] }
  extension_requirements.each do |requirement|
    authority = requirement["unit"]
    external = external_by_owner[authority]
    unless external
      errors << "primitive extension external authority is absent for #{authority}"
      next
    end
    errors << "primitive extension package path drifted for #{authority}" unless external["package_path"] == requirement["package_path"]
    errors << "primitive extension current digest is invalid for #{authority}" unless external["api_baseline_sha256"].to_s.match?(/\Asha256:[0-9a-f]{64}\z/)
    errors << "primitive extension required digest drifted for #{authority}" unless external["required_contract_sha256"] == requirement["required_contract_sha256"]
    errors << "primitive extension current API status drifted for #{authority}" unless external["current_api_status"] == "requires-extension"
  end

  extension_units.each do |extension_unit|
    row = primitive_rows.find { |candidate| candidate[:unit] == extension_unit }
    next errors << "primitive extension inventory omits #{extension_unit}" unless row

    requirements = extension_requirements.select { |requirement| requirement["extension_unit"] == extension_unit }
    expected_modules = requirements.map { |requirement| requirement["package_path"].delete_prefix("github.com/faustbrian/golib/") }
    expected_module = expected_modules.min_by { |candidate| [candidate.count("/"), candidate.b] }
    errors << "primitive extension canonical module drifted for #{extension_unit}" unless row[:module] == expected_module
    expected_requires = requirements.flat_map { |requirement| requirement["depends_on"] }
      .filter_map { |authority| authority_to_extension[authority] }
      .reject { |dependency| dependency == extension_unit }.sort_by(&:b).uniq
    errors << "primitive extension prerequisite closure drifted for #{extension_unit}" unless row[:requires] == expected_requires

    expected_consumers = requirements.flat_map { |requirement| derived_consumers_by_authority.fetch(requirement["unit"], []) }.sort_by(&:b).uniq
    actual_consumers = identity_rows.select { |candidate| candidate[:requires].include?(extension_unit) }.map { |candidate| candidate[:unit] }.sort_by(&:b)
    unexpected_prerequisite_consumers = actual_consumers - expected_consumers
    errors << "primitive extension consumer prerequisite closure drifted for #{extension_unit}: #{unexpected_prerequisite_consumers}" unless unexpected_prerequisite_consumers.empty?

    body = goal_bodies[extension_unit]
    unless body
      errors << "primitive extension goal is absent for #{extension_unit}"
      next
    end
    declared_consumer_line = body[/^- Exact identity-platform consumers(?: \([^\n]+\))?:.*$/]
    declared_consumers = declared_consumer_line.to_s.split(":", 2).last.to_s.scan(/`([^`]+)`/).flatten
    errors << "primitive extension goal consumer closure drifted for #{extension_unit}" unless declared_consumers == expected_consumers
    expected_digests = requirements.flat_map do |requirement|
      external = external_by_owner[requirement["unit"]]
      [external && external["api_baseline_sha256"], requirement["required_contract_sha256"]]
    end.compact.sort_by(&:b)
    actual_digests = body.scan(/sha256:[0-9a-f]{64}/).sort_by(&:b)
    errors << "primitive extension goal digest pins drifted for #{extension_unit}" unless actual_digests == expected_digests
  end

  if ledger_units
    errors << "primitive extension ledger closure drifted" unless ledger_units.to_set == rows.map { |row| row[:unit] }.to_set && ledger_units.length == EXPECTED_SCHEDULABLE_UNITS
  end
  errors
end

def worker_assignment_envelope_errors(attestation, expected, prompt_bytes, expected_prompt)
  errors = []
  {
    unit: expected.fetch(:unit), generation: expected.fetch(:generation), baseline: expected.fetch(:baseline),
    assignment: expected.fetch(:assignment), model: "gpt-5.6-sol", reasoning: "medium",
    fork_turns: "none", subagents: "false", package_scope: expected.fetch(:package_scope),
    reserved: expected.fetch(:reserved), goal_digest: expected.fetch(:goal_digest), goal_path: expected.fetch(:goal_path),
    authorized_by: "coordinator", status: "authorized"
  }.each do |field, value|
    errors << "worker assignment envelope #{field} drifted" unless attestation[field] == value
  end
  errors << "worker assignment envelope prompt digest drifted" unless attestation[:prompt_digest] == "sha256:#{Digest::SHA256.hexdigest(prompt_bytes)}"
  errors << "rendered worker prompt bytes drifted" unless prompt_bytes == expected_prompt
  errors
end

worker_envelope_prompt = "rendered prompt\n"
worker_envelope_expected = {unit: "fixture", generation: "1", baseline: "a" * 40, assignment: "b" * 40, package_scope: "pkg/fixture", reserved: ["pkg/fixture/child"], goal_digest: "sha256:#{'c' * 64}", goal_path: ".ai/identity-platform/goals/fixture.md"}
worker_envelope = worker_envelope_expected.merge(model: "gpt-5.6-sol", reasoning: "medium", fork_turns: "none", subagents: "false", authorized_by: "coordinator", status: "authorized", prompt_digest: "sha256:#{Digest::SHA256.hexdigest(worker_envelope_prompt)}")
fail_check("valid worker-envelope fixture was rejected") unless worker_assignment_envelope_errors(worker_envelope, worker_envelope_expected, worker_envelope_prompt, worker_envelope_prompt).empty?
{
  "prompt" => [worker_envelope, worker_envelope_prompt.sub("rendered", "altered")],
  "model" => [worker_envelope.merge(model: "gpt-5.6-terra"), worker_envelope_prompt],
  "scope" => [worker_envelope.merge(package_scope: "pkg"), worker_envelope_prompt],
  "subagents" => [worker_envelope.merge(subagents: "true"), worker_envelope_prompt]
}.each do |label, (fixture, prompt)|
  if worker_assignment_envelope_errors(fixture, worker_envelope_expected, prompt, worker_envelope_prompt).empty?
    fail_check("altered worker-envelope negative fixture #{label} was accepted")
  end
end

goal_fixture_body = "# Goal\n\nPinned bytes.\n"
goal_fixture_digest = Digest::SHA256.hexdigest(goal_fixture_body)
goal_fixture_rows = [{unit: "fixture", module: "pkg/fixture", goal: "pkg/fixture/.ai/GOAL.md"}]
goal_fixture_manifest = {"schema_version" => 1, "goals" => [{"unit" => "fixture", "planning_path" => "goals/fixture.md", "canonical_path" => "pkg/fixture/.ai/GOAL.md", "sha256" => goal_fixture_digest}]}
fixture_errors, = goal_manifest_resolution(goal_fixture_rows, goal_fixture_manifest, {"pkg/fixture/.ai/GOAL.md" => goal_fixture_body})
fail_check("moved-goal positive fixture was rejected: #{fixture_errors.join('; ')}") unless fixture_errors.empty?
{
  "rewritten body" => {"pkg/fixture/.ai/GOAL.md" => goal_fixture_body.sub("Pinned", "Weakened")},
  "mismatched inventory path" => {"goals/fixture.md" => goal_fixture_body},
  "duplicate locations" => {"goals/fixture.md" => goal_fixture_body, "pkg/fixture/.ai/GOAL.md" => goal_fixture_body},
  "orphan path" => {"pkg/fixture/.ai/GOAL.md" => goal_fixture_body, "goals/orphan.md" => goal_fixture_body}
}.each do |label, bodies|
  rows_fixture = Marshal.load(Marshal.dump(goal_fixture_rows))
  rows_fixture[0][:goal] = "pkg/wrong/.ai/GOAL.md" if label == "mismatched inventory path"
  errors, = goal_manifest_resolution(rows_fixture, goal_fixture_manifest, bodies)
  fail_check("goal-manifest negative fixture #{label} was accepted") if errors.empty?
end

def forbidden_task_worktree_paths
  [
    "/", File.expand_path("~"), File.dirname(File.expand_path("~")),
    Dir.tmpdir, "/tmp", "/private/tmp", "/var/tmp",
    PRIMARY_WORKTREE_ROOT, File.dirname(PRIMARY_WORKTREE_ROOT)
  ].filter_map do |candidate|
    File.realpath(candidate)
  rescue Errno::ENOENT
    nil
  end
end

def registered_worktree_paths
  output, _error, status = Open3.capture3("git", "-C", REPOSITORY_ROOT, "worktree", "list", "--porcelain")
  return [] unless status.success?

  output.lines.filter_map { |line| line[/\Aworktree (.+)\n?\z/, 1] }
    .filter_map do |candidate|
      File.realpath(candidate)
    rescue Errno::ENOENT
      nil
    end
end

def safe_task_worktree_identity(path, task_parent)
  return unless path.start_with?("/") && task_parent&.start_with?("/")

  clean = File.realpath(path)
  parent = File.realpath(task_parent)
  return if forbidden_task_worktree_paths.include?(clean) || forbidden_task_worktree_paths.include?(parent)
  return unless clean != parent && clean.start_with?(parent + File::SEPARATOR)
  return unless registered_worktree_paths.include?(clean)

  [clean, parent]
rescue Errno::ENOENT
  nil
end

def safe_worktree_path?(path, task_parent)
  identity = safe_task_worktree_identity(path, task_parent)
  return false unless identity

  clean, = identity
  current = File.realpath(REPOSITORY_ROOT)
  primary = File.realpath(PRIMARY_WORKTREE_ROOT)
  clean != current && clean != primary
end

def safe_integration_worktree_path?(path, task_parent)
  identity = safe_task_worktree_identity(path, task_parent)
  return false unless identity

  clean, = identity
  current = File.realpath(REPOSITORY_ROOT)
  primary = File.realpath(PRIMARY_WORKTREE_ROOT)
  return false if clean == primary
  return false if current != primary && clean != current

  true
rescue Errno::ENOENT
  false
end

def markdown_table(body, heading, expected_header)
  section = body[/^## #{Regexp.escape(heading)}\n(.*?)(?=^## |\z)/m, 1].to_s
  lines = section.lines.map(&:chomp)
  header_index = lines.index(expected_header)
  fail_check("#{heading} table header drifted") unless header_index
  fail_check("#{heading} table separator is missing") unless lines[header_index + 1]&.match?(/\A\|(?: --- \|)+\z/)

  lines[(header_index + 2)..].to_a.take_while { |line| line.start_with?("|") }.map do |line|
    line.split("|", -1).map(&:strip)[1...-1]
  end
end

def markdown_table_raw_rows(body, heading, expected_header)
  section = body[/^## #{Regexp.escape(heading)}\n(.*?)(?=^## |\z)/m, 1].to_s
  lines = section.lines
  header_index = lines.index { |line| line.chomp == expected_header }
  fail_check("#{heading} table header drifted") unless header_index

  lines[(header_index + 2)..].to_a.take_while { |line| line.start_with?("|") }
end

def markdown_table_append_only?(previous_body, current_body, heading, expected_header)
  previous_rows = markdown_table_raw_rows(previous_body, heading, expected_header)
  current_rows = markdown_table_raw_rows(current_body, heading, expected_header)
  current_rows.first(previous_rows.length) == previous_rows
end

def parse_execution_ledger(body, expected_header)
  markdown_table(body, "Unit execution ledger", expected_header).map do |cells|
    fail_check("execution ledger row has wrong column count") unless cells.length == 12
    unit, generation, task, branch, worktree, assignment, worker_commit, checkpoint, gate_revision, fingerprint, external, transition = cells
    fail_check("execution ledger unit cell is invalid") unless unit.match?(/\A`[^`]+`\z/)
    {
      unit: plain_cell(unit), generation: generation, task: task, branch: branch,
      worktree: worktree, assignment: assignment, worker_commit: worker_commit,
      checkpoint: checkpoint, gate_revision: gate_revision, fingerprint: fingerprint, external: external,
      transition: transition
    }
  end
end

raw_row_fixture_header = "| Value |"
raw_row_fixture_previous = "## Raw row fixture\n#{raw_row_fixture_header}\n| --- |\n| `value` |\n"
raw_row_fixture_reformatted = "## Raw row fixture\n#{raw_row_fixture_header}\n| --- |\n| value |\n"
if markdown_table_append_only?(raw_row_fixture_previous, raw_row_fixture_reformatted, "Raw row fixture", raw_row_fixture_header)
  fail_check("raw markdown row reformatting fixture was accepted")
end

def plain_cell(value)
  value.to_s.delete_prefix("`").delete_suffix("`")
end

def rfc3339?(value)
  Time.iso8601(value)
  value.match?(/\A\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\z/)
rescue ArgumentError
  false
end

def transition_version(entry)
  return 0 if entry[:transition] == "initial"

  entry[:transition][/\Av(\d+) /, 1]&.to_i
end

def ledger_transition_errors(previous_row, previous_entry, row, entry, abandonment_evidence: false)
  return [] if previous_row == row && previous_entry == entry

  errors = []
  edge = [previous_row[:status], row[:status]]
  allowed_edges = Set[
    %w[proposed ready], %w[ready proposed], %w[ready in-progress],
    %w[in-progress in-progress], %w[in-progress blocked], %w[in-progress ready],
    %w[in-progress implemented-unverified], %w[blocked blocked], %w[blocked in-progress],
    %w[blocked ready], %w[implemented-unverified implemented-unverified],
    %w[implemented-unverified in-progress], %w[implemented-unverified blocked],
    %w[implemented-unverified verified], %w[verified verified],
    %w[verified implemented-unverified]
  ]
  errors << "forbidden status edge #{edge.join(' -> ')}" unless allowed_edges.include?(edge)

  previous_version = transition_version(previous_entry)
  current_version = transition_version(entry)
  errors << "transition version did not increment exactly once" unless previous_version && current_version == previous_version + 1

  stable_inventory = [:unit, :module, :requires]
  stable_inventory << :goal unless edge == %w[in-progress implemented-unverified]
  errors << "inventory identity changed outside its permitted edge" unless stable_inventory.all? { |field| previous_row[field] == row[field] }

  fields = [:generation, :task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external]
  changed = fields.select { |field| previous_entry[field] != entry[field] }.to_set
  identity = Set[:task, :branch, :worktree, :assignment]
  proof = Set[:worker_commit, :checkpoint, :gate_revision, :fingerprint]
  permitted = case edge
              when %w[proposed ready], %w[ready proposed]
                Set.new
              when %w[ready in-progress]
                identity
              when %w[in-progress in-progress]
                Set[:assignment]
              when %w[in-progress blocked]
                Set[:worker_commit]
              when %w[implemented-unverified blocked]
                Set.new
              when %w[in-progress ready], %w[blocked ready]
                Set[:generation] | identity | proof | Set[:external]
              when %w[in-progress implemented-unverified]
                proof | Set[:external]
              when %w[blocked blocked]
                Set[:worker_commit, :external]
              when %w[blocked in-progress], %w[implemented-unverified in-progress]
                Set.new
              when %w[implemented-unverified implemented-unverified], %w[verified verified]
                Set[:external]
              when %w[implemented-unverified verified]
                Set[:gate_revision, :fingerprint, :external]
              when %w[verified implemented-unverified]
                Set[:gate_revision, :fingerprint, :external]
              else
                Set.new
              end
  errors << "transition changed forbidden ledger fields: #{(changed - permitted).to_a.sort.join(', ')}" unless changed.subset?(permitted)
  same_status = previous_row[:status] == row[:status]
  errors << "same-status finalization changed no permitted metadata" if same_status && changed.empty?

  previous_generation = previous_entry[:generation].to_i
  current_generation = entry[:generation].to_i
  abandonment = [%w[in-progress ready], %w[blocked ready]].include?(edge)
  expected_generation = abandonment ? previous_generation + 1 : previous_generation
  errors << "generation change does not match assignment abandonment" unless current_generation == expected_generation

  if edge == %w[in-progress in-progress]
    errors << "same-status assignment finalization must replace pending with a commit" unless previous_entry[:assignment] == "pending" && entry[:assignment].match?(/\A[0-9a-f]{40}\z/) && changed == Set[:assignment]
  end
  if edge == %w[in-progress blocked] && changed.include?(:worker_commit)
    errors << "blocked checkpoint finalization requires a worker commit" unless entry[:worker_commit].match?(/\A[0-9a-f]{40}\z/)
  end
  if edge == %w[implemented-unverified verified]
    previous_gate_empty = previous_entry[:gate_revision] == "—" && previous_entry[:fingerprint] == "—"
    current_gate_complete = entry[:gate_revision].match?(/\A[0-9a-f]{40}\z/) && entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
    errors << "verification transition did not atomically bind a fresh gate revision and input root" unless previous_gate_empty && current_gate_complete
  end
  if abandonment
    cleared = entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external).all? { |value| value == "—" }
    errors << "abandoned assignment did not clear every assignment/evidence field" unless cleared
    errors << "assignment abandonment lacks identity-bound disposition evidence" unless abandonment_evidence
  end
  errors
end

def execution_history_required?(entries:, recovery_rows:, goal_revision_rows:, dependency_revisions:, dependency_dispositions:, local_gate_bindings:)
  entries.any? { |entry| entry[:transition] != "initial" } || recovery_rows.any? || goal_revision_rows.any? ||
    dependency_revisions.any? || dependency_dispositions.any? || local_gate_bindings.any?
end

def previous_snapshot_binding_errors(
  previous_sources:, history_required:, candidate_fixture_mode:,
  resolve_parent: -> { git_output("rev-parse", "HEAD^1") },
  read_blob: ->(revision, path) { git_blob_bytes(revision, path) }
)
  required_paths = %w[
    .ai/identity-platform/INVENTORY.md
    .ai/identity-platform/EXECUTION_LEDGER.md
    .ai/identity-platform/PREFLIGHT_EVIDENCE.md
    .ai/identity-platform/GOAL_MANIFEST.json
  ]
  errors = []
  if history_required && previous_sources.keys.to_set != required_paths.to_set
    errors << "post-initial execution validation requires the complete previous artifact quartet"
  end
  return errors if previous_sources.empty? && !history_required

  expected_revision = candidate_fixture_mode ? "HEAD" : resolve_parent.call
  unless expected_revision
    errors << "previous execution fixtures cannot be bound because the current commit has no first parent"
    return errors
  end
  return errors if previous_sources.empty?

  required_paths.each do |path|
    unless previous_sources.key?(path)
      errors << "previous execution fixture is missing #{path}"
      next
    end
    committed = read_blob.call(expected_revision, path)
    if committed.nil?
      errors << "expected previous artifact object is missing at #{expected_revision}:#{path}"
    elsif previous_sources.fetch(path) != committed
      errors << "previous fixture bytes do not match exact expected parent #{expected_revision}:#{path}"
    end
  end
  errors
end

def integrated_commit_ancestry_errors(entry, head: "HEAD", commit_exists: method(:git_commit_exists?), ancestor: method(:git_ancestor?))
  errors = []
  assignment_exists = commit_exists.call(entry[:assignment])
  worker_exists = commit_exists.call(entry[:worker_commit])
  checkpoint_exists = commit_exists.call(entry[:checkpoint])
  errors << "assignment commit does not exist" unless assignment_exists
  errors << "worker commit does not exist" unless worker_exists
  errors << "integration checkpoint does not exist" unless checkpoint_exists
  errors << "worker commit excludes assignment" if assignment_exists && worker_exists && !ancestor.call(entry[:assignment], entry[:worker_commit])
  errors << "integration checkpoint excludes worker commit" if worker_exists && checkpoint_exists && !ancestor.call(entry[:worker_commit], entry[:checkpoint])
  errors << "integration checkpoint is not integrated" if checkpoint_exists && !ancestor.call(entry[:checkpoint], head)
  errors
end

gate_transition_row = {unit: "fixture", module: "pkg/fixture", requires: [], owner: "—", goal: "goals/fixture.md"}
gate_transition_previous_row = gate_transition_row.merge(status: "implemented-unverified")
gate_transition_row = gate_transition_row.merge(status: "verified")
gate_transition_previous_entry = {
  generation: "1", task: "fixture-worker", branch: "feature/fixture", worktree: "/fixture/worker",
  assignment: "a" * 40, worker_commit: "b" * 40, checkpoint: "c" * 40,
  gate_revision: "—", fingerprint: "—", external: "not-needed",
  transition: "v1 implemented-unverified owner=— at=2026-08-11T00:00:00Z"
}
gate_transition_entry = gate_transition_previous_entry.merge(
  gate_revision: "d" * 40, fingerprint: "sha256:#{'e' * 64}",
  transition: "v2 verified owner=— at=2026-08-11T00:00:01Z"
)
unless ledger_transition_errors(gate_transition_previous_row, gate_transition_previous_entry, gate_transition_row, gate_transition_entry).empty?
  fail_check("atomic local gate verification transition fixture was rejected")
end
partial_gate_transition = gate_transition_entry.merge(fingerprint: "—")
if ledger_transition_errors(gate_transition_previous_row, gate_transition_previous_entry, gate_transition_row, partial_gate_transition).empty?
  fail_check("partial local gate verification transition fixture was accepted")
end
history_initial_entry = gate_transition_previous_entry.merge(transition: "initial")
if execution_history_required?(
  entries: [history_initial_entry], recovery_rows: [], goal_revision_rows: [],
  dependency_revisions: [], dependency_dispositions: [], local_gate_bindings: []
)
  fail_check("initial execution history fixture unexpectedly requires prior snapshots")
end
unless execution_history_required?(
  entries: [gate_transition_previous_entry], recovery_rows: [], goal_revision_rows: [],
  dependency_revisions: [], dependency_dispositions: [], local_gate_bindings: []
)
  fail_check("post-initial execution history fixture omitted prior snapshots")
end
previous_artifact_fixture = {
  ".ai/identity-platform/INVENTORY.md" => "committed inventory\n",
  ".ai/identity-platform/EXECUTION_LEDGER.md" => "committed ledger\n",
  ".ai/identity-platform/PREFLIGHT_EVIDENCE.md" => "committed preflight\n",
  ".ai/identity-platform/GOAL_MANIFEST.json" => "{\"committed\":true}\n"
}
fabricated_previous_artifact_fixture = previous_artifact_fixture.transform_values { |bytes| "fabricated #{bytes}" }
if previous_snapshot_binding_errors(
  previous_sources: fabricated_previous_artifact_fixture, history_required: true, candidate_fixture_mode: true,
  resolve_parent: -> { raise "candidate fixture must bind to HEAD" },
  read_blob: ->(revision, path) { revision == "HEAD" ? previous_artifact_fixture[path] : nil }
).empty?
  fail_check("coherent fabricated previous snapshot negative fixture was accepted")
end
unless previous_snapshot_binding_errors(
  previous_sources: previous_artifact_fixture, history_required: true, candidate_fixture_mode: true,
  resolve_parent: -> { raise "candidate fixture must bind to HEAD" },
  read_blob: ->(revision, path) { revision == "HEAD" ? previous_artifact_fixture[path] : nil }
).empty?
  fail_check("exact candidate-parent previous snapshot fixture was rejected")
end
unless previous_snapshot_binding_errors(
  previous_sources: previous_artifact_fixture, history_required: true, candidate_fixture_mode: false,
  resolve_parent: -> { "parent-commit" },
  read_blob: ->(revision, path) { revision == "parent-commit" ? previous_artifact_fixture[path] : nil }
).empty?
  fail_check("exact committed first-parent previous snapshot fixture was rejected")
end
if previous_snapshot_binding_errors(
  previous_sources: previous_artifact_fixture, history_required: true, candidate_fixture_mode: false,
  resolve_parent: -> { nil }, read_blob: ->(_revision, _path) { nil }
).empty?
  fail_check("root commit previous snapshot negative fixture was accepted")
end
missing_previous_artifact_fixture = previous_artifact_fixture.dup
if previous_snapshot_binding_errors(
  previous_sources: missing_previous_artifact_fixture, history_required: true, candidate_fixture_mode: false,
  resolve_parent: -> { "parent-commit" },
  read_blob: ->(_revision, path) { path.end_with?("GOAL_MANIFEST.json") ? nil : previous_artifact_fixture[path] }
).empty?
  fail_check("missing committed previous artifact negative fixture was accepted")
end
unless previous_snapshot_binding_errors(
  previous_sources: {}, history_required: false, candidate_fixture_mode: false,
  resolve_parent: -> { raise "initial history must not require a parent" }, read_blob: ->(_revision, _path) { nil }
).empty?
  fail_check("parentless initial execution snapshot fixture was rejected")
end
root_history_errors = previous_snapshot_binding_errors(
  previous_sources: {}, history_required: true, candidate_fixture_mode: false,
  resolve_parent: -> { nil }, read_blob: ->(_revision, _path) { nil }
)
unless root_history_errors.any? { |error| error.include?("no first parent") }
  fail_check("parentless post-initial execution history did not fail at the root boundary")
end
fake_integrated_entry = gate_transition_previous_entry.merge(unit: "fixture")
if integrated_commit_ancestry_errors(
  fake_integrated_entry,
  commit_exists: ->(_revision) { false }, ancestor: ->(_ancestor, _descendant) { false }
).empty?
  fail_check("unreachable implemented-unverified commit chain negative fixture was accepted")
end
unless integrated_commit_ancestry_errors(
  fake_integrated_entry,
  commit_exists: ->(_revision) { true }, ancestor: ->(_ancestor, _descendant) { true }
).empty?
  fail_check("reachable implemented-unverified commit chain fixture was rejected")
end

def ordinary_abandonment_errors(disposition, previous_entry, previous_resources, resources)
  errors = []
  return ["assignment abandonment disposition is missing"] unless disposition
  {
    unit: previous_entry[:unit], generation: previous_entry[:generation], task: previous_entry[:task],
    branch: previous_entry[:branch], worktree: previous_entry[:worktree], assignment: previous_entry[:assignment]
  }.each do |field, expected|
    errors << "assignment abandonment disposition #{field} drifted" unless disposition[field] == expected
  end
  expected_commit = previous_entry[:worker_commit] == "—" ? previous_entry[:assignment] : previous_entry[:worker_commit]
  errors << "assignment abandonment preserved commit drifted" unless disposition[:preserved_commit] == expected_commit
  proof = disposition[:proof].match(/\Aclean-(checkpoint|baseline):([0-9a-f]{40})\z/)
  safe = disposition[:proof].match?(/\Asafe-abandonment:reason:[a-zA-Z0-9._-]+\z/)
  errors << "assignment abandonment lacks clean preservation proof" unless proof || safe
  if proof
    expected_kind = previous_entry[:worker_commit] == "—" ? "baseline" : "checkpoint"
    errors << "assignment abandonment preservation kind drifted" unless proof[1] == expected_kind
    errors << "assignment abandonment preservation proof drifted" unless proof[2] == expected_commit
  end
  owned_previous = previous_resources.select { |resource| resource[:owner] == previous_entry[:task] }.sort_by { |resource| resource[:id] }
  owned_current = resources.select { |resource| resource[:owner] == previous_entry[:task] }.sort_by { |resource| resource[:id] }
  errors << "assignment abandonment resource inventory drifted" unless owned_previous.map { |resource| resource.values_at(:id, :type, :owner, :target) } == owned_current.map { |resource| resource.values_at(:id, :type, :owner, :target) }
  errors << "assignment abandonment disposition resource states drifted" unless disposition[:resource_states] == owned_current.to_h { |resource| [resource[:id], resource[:state]] }
  errors << "safe abandonment retained task-owned resources" if safe && owned_current.any? { |resource| resource[:state] != "removed" }
  worktree = owned_current.find { |resource| resource[:type] == "worktree" && resource[:target] == previous_entry[:worktree] }
  errors << "assignment abandonment lacks its registered worktree" unless worktree
  if worktree && worktree[:state] == "retained-for-recovery"
    errors << "retained abandonment worktree is not clean" unless worktree[:clean]
    errors << "retained abandonment worktree HEAD drifted" unless worktree[:head] == expected_commit
  end
  errors.concat(dependency_disposition_evidence_errors(disposition, owned_previous, owned_current))
  errors
end

def ordinary_abandonment_units(previous_rows, rows, dependency_affected)
  rows.filter_map do |row|
    next if dependency_affected.include?(row[:unit])
    previous_row = previous_rows.find { |candidate| candidate[:unit] == row[:unit] }
    row[:unit] if [["in-progress", "ready"], ["blocked", "ready"]].include?([previous_row[:status], row[:status]])
  end
end

ordinary_coexistence_previous = [
  {unit: "dependency-reset", status: "blocked"}, {unit: "ordinary-reset", status: "in-progress"}
]
ordinary_coexistence_current = [
  {unit: "dependency-reset", status: "ready"}, {unit: "ordinary-reset", status: "ready"}
]
unless ordinary_abandonment_units(ordinary_coexistence_previous, ordinary_coexistence_current, Set["dependency-reset"]) == ["ordinary-reset"]
  fail_check("dependency/ordinary abandonment coexistence fixture was rejected")
end

def ledger_row_shape_errors(row, entry)
  errors = []
  assignment = entry.values_at(:task, :branch, :worktree, :assignment)
  integrated = entry.values_at(:worker_commit, :checkpoint)
  all_fields = entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external)
  errors << "assignment commit format is invalid" unless entry[:assignment] == "pending" || entry[:assignment].match?(/\A(?:—|[0-9a-f]{40})\z/)
  [:worker_commit, :checkpoint, :gate_revision].each do |field|
    errors << "#{field} format is invalid" unless entry[field].match?(/\A(?:—|[0-9a-f]{40})\z/)
  end
  errors << "worker task format is invalid" unless entry[:task] == "—" || entry[:task].match?(/\A[a-zA-Z0-9._\/-]+\z/)
  errors << "worker branch format is invalid" unless entry[:branch] == "—" || entry[:branch].match?(%r{\A(?:feature|bugfix|hotfix|release|chore|refactor)/[a-zA-Z0-9._/-]+\z})
  errors << "worker worktree format is invalid" unless entry[:worktree] == "—" || (entry[:worktree].start_with?("/") && entry[:worktree] != "/")
  external_ok = entry[:external] == "—" || ["not-needed", "available"].include?(entry[:external]) || entry[:external].match?(/\Aunavailable:[a-zA-Z0-9._-]+\z/) || repository_evidence_binding(entry[:external])
  errors << "external evidence disposition is invalid" unless external_ok
  case row[:status]
  when "proposed", "ready"
    errors << "unassigned row retains owner" unless row[:owner] == "—"
    errors << "unassigned row retains assignment or evidence" unless all_fields.all? { |value| value == "—" }
  when "in-progress"
    errors << "in-progress owner/task mismatch" unless row[:owner] == entry[:task] && row[:owner] != "—"
    errors << "in-progress assignment is incomplete" if assignment.any? { |value| value == "—" }
    if entry[:checkpoint] == "—"
      errors << "pre-integration in-progress row has gate evidence" unless entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
    else
      errors << "integrated repair row is incomplete" if integrated.any? { |value| value == "—" }
      gate_empty = entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
      gate_complete = entry[:gate_revision].match?(/\A[0-9a-f]{40}\z/) && entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
      errors << "integrated repair gate evidence is partial" unless gate_empty || gate_complete
    end
  when "blocked"
    errors << "blocked owner is unsafe" unless row[:owner].match?(/\Ablocker:[a-zA-Z0-9._-]+\z/)
    errors << "blocked assignment is incomplete" if assignment.any? { |value| value == "—" || value == "pending" }
    if entry[:checkpoint] == "—"
      errors << "pre-integration blocked row has gate evidence" unless entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
    else
      errors << "integrated blocked row lacks worker commit" if entry[:worker_commit] == "—"
      gate_empty = entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
      gate_complete = entry[:gate_revision].match?(/\A[0-9a-f]{40}\z/) && entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
      errors << "integrated blocked gate evidence is partial" unless gate_empty || gate_complete
    end
  when "implemented-unverified"
    errors << "integrated row retains owner" unless row[:owner] == "—"
    errors << "integrated row is incomplete" if (assignment + integrated).any? { |value| value == "—" || value == "pending" }
    errors << "implemented-unverified row prematurely records gate evidence" unless entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
    errors << "integrated external evidence disposition is missing" if entry[:external] == "—"
  when "verified"
    errors << "integrated row retains owner" unless row[:owner] == "—"
    errors << "integrated row is incomplete" if (assignment + integrated).any? { |value| value == "—" || value == "pending" }
    errors << "verified gate revision is invalid" unless entry[:gate_revision].match?(/\A[0-9a-f]{40}\z/)
    errors << "verified gate fingerprint is invalid" unless entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
    errors << "integrated external evidence disposition is missing" if entry[:external] == "—"
  end
  errors
end

def dependency_reverse_closure(unit, previous_rows, current_rows)
  reverse = Hash.new { |hash, key| hash[key] = Set.new }
  (previous_rows + current_rows).each do |row|
    row[:requires].each { |required| reverse[required] << row[:unit] }
  end
  closure = Set[unit]
  queue = [unit]
  until queue.empty?
    reverse[queue.shift].each do |dependent|
      next if closure.include?(dependent)
      closure << dependent
      queue << dependent
    end
  end
  closure.to_a.sort
end

def dependency_revision_digest(revision)
  payload = {
    "revision_id" => revision[:revision_id], "unit" => revision[:unit],
    "previous_requires" => revision[:previous_requires], "current_requires" => revision[:current_requires],
    "affected_units" => revision[:affected_units], "reason" => revision[:reason],
    "approver" => revision[:approver], "recorded_at" => revision[:recorded_at]
  }
  "sha256:#{Digest::SHA256.hexdigest(JSON.generate(payload))}"
end

def dependency_disposition_evidence_digest(record)
  "sha256:#{Digest::SHA256.hexdigest(JSON.pretty_generate(record) + "\n")}"
end

def dependency_disposition_evidence_digest_errors(disposition)
  return ["dependency assignment disposition evidence record is unavailable"] unless disposition[:evidence_record].is_a?(Hash)
  return [] if disposition[:evidence_digest] == dependency_disposition_evidence_digest(disposition[:evidence_record])

  ["dependency assignment disposition evidence digest drifted"]
end

def dependency_disposition_evidence_errors(disposition, previous_resources, resources)
  errors = []
  evidence = disposition[:evidence_record]
  return ["dependency assignment disposition evidence record is unavailable"] unless evidence.is_a?(Hash)
  errors.concat(dependency_disposition_evidence_digest_errors(disposition))

  expected_keys = %w[
    schema_version disposition_id revision_ids unit generation worker_task branch worktree
    assignment_commit preservation resources authorized_by recorded_at
  ]
  errors << "dependency assignment disposition evidence schema drifted" unless evidence.keys == expected_keys
  errors << "dependency assignment disposition evidence schema version drifted" unless evidence["schema_version"] == 1
  {
    "disposition_id" => disposition[:disposition_id], "revision_ids" => disposition[:revision_ids],
    "unit" => disposition[:unit], "generation" => disposition[:generation].to_i,
    "worker_task" => disposition[:task], "branch" => disposition[:branch],
    "worktree" => disposition[:worktree], "assignment_commit" => disposition[:assignment],
    "recorded_at" => disposition[:recorded_at]
  }.each do |field, expected|
    errors << "dependency assignment disposition evidence #{field} drifted" unless evidence[field] == expected
  end
  errors << "dependency assignment disposition evidence approver is not coordinator" unless evidence["authorized_by"] == "coordinator"

  proof_match = disposition[:proof].match(/\Aclean-(checkpoint|baseline):([0-9a-f]{40})\z/)
  expected_preservation = if proof_match
                            {"kind" => "clean-#{proof_match[1]}", "commit" => disposition[:preserved_commit]}
                          else
                            reason = disposition[:proof][/\Asafe-abandonment:(reason:[a-zA-Z0-9._-]+)\z/, 1]
                            {"kind" => "safe-abandonment", "reason" => reason,
                             "recoverable_commit" => disposition[:preserved_commit]}
                          end
  errors << "dependency assignment disposition evidence preservation drifted" unless evidence["preservation"] == expected_preservation

  previous_by_id = previous_resources.to_h { |resource| [resource[:id], resource] }
  current_by_id = resources.to_h { |resource| [resource[:id], resource] }
  evidence_resources = evidence["resources"]
  unless evidence_resources.is_a?(Array) && evidence_resources.map { |resource| resource["resource_id"] } == previous_by_id.keys.sort
    errors << "dependency assignment disposition evidence resource inventory drifted"
    return errors
  end
  evidence_resources.each do |record|
    expected_resource_keys = %w[
      resource_id type owner target previous_state current_state cleanup_evidence
      pre_removal_clean pre_removal_head
    ]
    errors << "dependency assignment disposition evidence resource schema drifted" unless record.keys == expected_resource_keys
    previous_resource = previous_by_id[record["resource_id"]]
    current_resource = current_by_id[record["resource_id"]]
    next unless previous_resource && current_resource
    {
      "type" => previous_resource[:type], "owner" => previous_resource[:owner], "target" => previous_resource[:target],
      "previous_state" => previous_resource[:state], "current_state" => current_resource[:state],
      "cleanup_evidence" => current_resource[:evidence]
    }.each do |field, expected|
      errors << "dependency assignment disposition evidence resource #{record['resource_id']} #{field} drifted" unless record[field] == expected
    end
    if current_resource[:type] == "worktree" && current_resource[:state] == "removed"
      errors << "removed dependency worktree lacks captured clean state" unless record["pre_removal_clean"] == true
      errors << "removed dependency worktree captured the wrong HEAD" unless record["pre_removal_head"] == disposition[:preserved_commit]
    elsif current_resource[:state] != "removed"
      errors << "retained dependency resource has pre-removal evidence" unless record["pre_removal_clean"].nil? && record["pre_removal_head"].nil?
    end
  end
  errors
end

def dependency_revision_transition_errors(previous_rows:, previous_entries:, rows:, entries:, revisions:, dispositions: [], resources: [], previous_resources: [])
  errors = []
  visiting = Set.new
  visited = Set.new
  visit = lambda do |unit|
    return if visited.include?(unit)
    if visiting.include?(unit)
      errors << "dependency revision introduces a cycle at #{unit}"
      return
    end
    visiting << unit
    row = rows.find { |candidate| candidate[:unit] == unit }
    row[:requires].each { |required| visit.call(required) }
    visiting.delete(unit)
    visited << unit
  end
  rows.each { |row| visit.call(row[:unit]) }
  changed_units = rows.filter_map do |row|
    previous = previous_rows.find { |candidate| candidate[:unit] == row[:unit] }
    row[:unit] if previous[:requires] != row[:requires]
  end.sort
  revision_units = revisions.map { |revision| revision[:unit] }
  errors << "dependency revision units do not match changed Requires rows" unless revision_units == changed_units && revision_units == revision_units.uniq.sort
  affected = Set.new
  revisions.each do |revision|
    previous = previous_rows.find { |row| row[:unit] == revision[:unit] }
    current = rows.find { |row| row[:unit] == revision[:unit] }
    errors << "dependency revision ID is unsafe" unless revision[:revision_id].match?(/\A[a-zA-Z0-9._-]+\z/)
    errors << "dependency revision reason is unsafe" unless revision[:reason].match?(/\Areason:[a-zA-Z0-9._-]+\z/)
    errors << "dependency revision approver is not coordinator" unless revision[:approver] == "coordinator"
    errors << "dependency revision timestamp is invalid" unless rfc3339?(revision[:recorded_at])
    errors << "dependency revision previous Requires drifted" unless revision[:previous_requires] == previous[:requires]
    errors << "dependency revision current Requires drifted" unless revision[:current_requires] == current[:requires]
    expected_closure = dependency_reverse_closure(revision[:unit], previous_rows, rows)
    errors << "dependency revision reverse closure drifted" unless revision[:affected_units] == expected_closure
    errors << "dependency revision change digest drifted" unless revision[:change_digest] == dependency_revision_digest(revision)
    affected.merge(expected_closure)
  end
  errors << "Requires changed without a dependency revision" if !changed_units.empty? && revisions.empty?
  errors << "dependency revision recorded without a Requires change" if changed_units.empty? && !revisions.empty?

  active_affected = previous_rows.select do |previous_row|
    affected.include?(previous_row[:unit]) && ["in-progress", "blocked"].include?(previous_row[:status])
  end
  active_units = active_affected.map { |row| row[:unit] }.sort
  disposition_units = dispositions.map { |disposition| disposition[:unit] }
  errors << "dependency assignment dispositions do not exactly match affected active assignments" unless disposition_units.sort == active_units && disposition_units.uniq == disposition_units
  active_affected.each do |previous_row|
    previous_entry = previous_entries.find { |candidate| candidate[:unit] == previous_row[:unit] }
    disposition = dispositions.find { |candidate| candidate[:unit] == previous_row[:unit] }
    if previous_row[:status] == "in-progress"
      errors << "affected active assignment #{previous_row[:unit]} must transition to blocked before dependency revision"
      next
    end
    next unless disposition

    relevant_revision_ids = revisions.select do |revision|
      dependency_reverse_closure(revision[:unit], previous_rows, rows).include?(previous_row[:unit])
    end.map { |revision| revision[:revision_id] }
    errors << "dependency assignment disposition #{previous_row[:unit]} revision IDs drifted" unless disposition[:revision_ids] == relevant_revision_ids
    {
      generation: previous_entry[:generation], task: previous_entry[:task],
      branch: previous_entry[:branch], worktree: previous_entry[:worktree],
      assignment: previous_entry[:assignment]
    }.each do |field, expected|
      errors << "dependency assignment disposition #{previous_row[:unit]} #{field} drifted" unless disposition[field] == expected
    end

    proof_match = disposition[:proof].match(/\Aclean-(checkpoint|baseline):([0-9a-f]{40})\z/)
    safe_abandonment = disposition[:proof].match?(/\Asafe-abandonment:reason:[a-zA-Z0-9._-]+\z/)
    expected_preserved_commit = previous_entry[:worker_commit] == "—" ? previous_entry[:assignment] : previous_entry[:worker_commit]
    errors << "dependency assignment disposition #{previous_row[:unit]} preserved commit drifted" unless disposition[:preserved_commit] == expected_preserved_commit
    if proof_match
      expected_proof = proof_match[1] == "checkpoint" ? previous_entry[:worker_commit] : previous_entry[:assignment]
      errors << "dependency assignment disposition #{previous_row[:unit]} preservation proof drifted" unless proof_match[2] == expected_proof
      if proof_match[1] == "baseline" && previous_entry[:worker_commit] != "—"
        errors << "dependency assignment disposition #{previous_row[:unit]} ignored its worker checkpoint"
      end
    elsif !safe_abandonment
      errors << "dependency assignment disposition #{previous_row[:unit]} lacks clean checkpoint/baseline or safe-abandonment proof"
    end

    previous_owned_resources = previous_resources.select { |resource| resource[:owner] == previous_entry[:task] }.sort_by { |resource| resource[:id] }
    owned_resources = resources.select { |resource| resource[:owner] == previous_entry[:task] }.sort_by { |resource| resource[:id] }
    errors << "dependency assignment disposition #{previous_row[:unit]} has no previously registered resources" if previous_owned_resources.empty?
    errors << "dependency assignment disposition #{previous_row[:unit]} lost or added registered resources" unless owned_resources.map { |resource| resource[:id] } == previous_owned_resources.map { |resource| resource[:id] }
    previous_owned_resources.each do |previous_resource|
      resource = owned_resources.find { |candidate| candidate[:id] == previous_resource[:id] }
      next unless resource
      errors << "dependency assignment resource #{resource[:id]} identity drifted" unless resource.values_at(:id, :type, :owner, :target) == previous_resource.values_at(:id, :type, :owner, :target)
      unless ["active", "retained-for-recovery"].include?(previous_resource[:state]) && ["retained-for-recovery", "removed"].include?(resource[:state])
        errors << "dependency assignment resource #{resource[:id]} has an invalid preservation/removal transition"
      end
    end
    expected_resource_states = owned_resources.to_h { |resource| [resource[:id], resource[:state]] }
    errors << "dependency assignment disposition #{previous_row[:unit]} resource set or state drifted" unless disposition[:resource_states] == expected_resource_states
    owned_resources.each do |resource|
      unless ["retained-for-recovery", "removed"].include?(resource[:state])
        errors << "dependency assignment resource #{resource[:id]} was neither preserved nor removed"
      end
    end
    if safe_abandonment && owned_resources.any? { |resource| resource[:state] != "removed" }
      errors << "safe-abandonment disposition retained a task-owned resource"
    end
    previous_worktree_resource = previous_owned_resources.find { |resource| resource[:type] == "worktree" && resource[:target] == previous_entry[:worktree] }
    worktree_resource = owned_resources.find { |resource| previous_worktree_resource && resource[:id] == previous_worktree_resource[:id] }
    errors << "dependency assignment disposition #{previous_row[:unit]} lacks its exact previously registered worktree" unless previous_worktree_resource
    errors << "dependency assignment disposition #{previous_row[:unit]} lacks its exact current worktree disposition" unless worktree_resource
    if worktree_resource && worktree_resource[:state] == "retained-for-recovery" && proof_match
      errors << "retained dependency worktree #{previous_row[:unit]} is not clean" unless worktree_resource[:clean]
      errors << "retained dependency worktree #{previous_row[:unit]} HEAD does not match preservation proof" unless worktree_resource[:head] == disposition[:preserved_commit]
    end
    dependency_disposition_evidence_errors(disposition, previous_owned_resources, owned_resources).each { |error| errors << error }
  end

  rows.each do |row|
    previous_row = previous_rows.find { |candidate| candidate[:unit] == row[:unit] }
    entry = entries.find { |candidate| candidate[:unit] == row[:unit] }
    previous_entry = previous_entries.find { |candidate| candidate[:unit] == row[:unit] }
    unless affected.include?(row[:unit])
      errors << "unchanged row #{row[:unit]} changed during dependency revision" unless row == previous_row && entry == previous_entry
      next
    end
    identity_fields = [:unit, :module, :goal]
    errors << "affected row #{row[:unit]} changed inventory identity" unless identity_fields.all? { |field| row[field] == previous_row[field] }
    unless changed_units.include?(row[:unit])
      errors << "reverse dependant #{row[:unit]} changed Requires" unless row[:requires] == previous_row[:requires]
    end
    expected_status = row[:requires].all? { |required| rows.find { |candidate| candidate[:unit] == required }[:status] == "verified" } ? "ready" : "proposed"
    errors << "affected row #{row[:unit]} was not demoted to #{expected_status}" unless row[:status] == expected_status && row[:owner] == "—"
    cleared = entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external).all? { |value| value == "—" }
    errors << "affected row #{row[:unit]} retained stale assignment or evidence" unless cleared
    assigned_before = previous_entry[:task] != "—"
    expected_generation = previous_entry[:generation].to_i + (assigned_before ? 1 : 0)
    errors << "affected row #{row[:unit]} generation did not reflect assignment abandonment" unless entry[:generation].to_i == expected_generation
    errors << "affected row #{row[:unit]} transition version did not increment exactly once" unless transition_version(entry) == transition_version(previous_entry) + 1
  end
  [errors, affected]
end

def dependency_disposition_evidence_fixture(disposition, previous_resources, resources)
  proof_match = disposition[:proof].match(/\Aclean-(checkpoint|baseline):([0-9a-f]{40})\z/)
  preservation = if proof_match
                   {"kind" => "clean-#{proof_match[1]}", "commit" => disposition[:preserved_commit]}
                 else
                   {"kind" => "safe-abandonment", "reason" => disposition[:proof].delete_prefix("safe-abandonment:"),
                    "recoverable_commit" => disposition[:preserved_commit]}
                 end
  current_by_id = resources.to_h { |resource| [resource[:id], resource] }
  evidence_resources = previous_resources.sort_by { |resource| resource[:id] }.map do |previous_resource|
    current_resource = current_by_id.fetch(previous_resource[:id])
    removed_worktree = current_resource[:type] == "worktree" && current_resource[:state] == "removed"
    {
      "resource_id" => previous_resource[:id], "type" => previous_resource[:type],
      "owner" => previous_resource[:owner], "target" => previous_resource[:target],
      "previous_state" => previous_resource[:state], "current_state" => current_resource[:state],
      "cleanup_evidence" => current_resource[:evidence],
      "pre_removal_clean" => removed_worktree ? true : nil,
      "pre_removal_head" => removed_worktree ? disposition[:preserved_commit] : nil
    }
  end
  {
    "schema_version" => 1, "disposition_id" => disposition[:disposition_id],
    "revision_ids" => disposition[:revision_ids], "unit" => disposition[:unit],
    "generation" => disposition[:generation].to_i, "worker_task" => disposition[:task],
    "branch" => disposition[:branch], "worktree" => disposition[:worktree],
    "assignment_commit" => disposition[:assignment], "preservation" => preservation,
    "resources" => evidence_resources, "authorized_by" => "coordinator",
    "recorded_at" => disposition[:recorded_at]
  }
end

def dependency_revision_fixture(previous_requires, current_requires, approver: "coordinator", active: false, resource_state: "retained-for-recovery")
  row = lambda do |unit, requires, status|
    {unit: unit, module: "pkg/#{unit}", requires: requires, status: status, owner: "—", goal: "goals/#{unit}.md"}
  end
  entry = lambda do |unit|
    {unit: unit, generation: "1", task: "worker-#{unit}", branch: "feature/#{unit}", worktree: "/fixture/#{unit}", assignment: "0" * 40,
     worker_commit: "0" * 40, checkpoint: "0" * 40, gate_revision: "0" * 40, fingerprint: "sha256:#{'0' * 64}", external: "not-needed",
     transition: "v2 verified owner=— at=2026-08-11T00:00:00Z"}
  end
  previous_rows = [row.call("a", [], "verified"), row.call("b", previous_requires, active ? "blocked" : "verified"), row.call("c", ["b"], "verified"), row.call("d", [], "verified")]
  previous_rows[1][:owner] = "blocker:dependency-review" if active
  rows = [previous_rows[0].dup, row.call("b", current_requires, "ready"), row.call("c", ["b"], "proposed"), previous_rows[3].dup]
  previous_entries = %w[a b c d].map { |unit| entry.call(unit) }
  if active
    previous_entries[1][:worker_commit] = "1" * 40
    previous_entries[1][:transition] = "v2 blocked owner=blocker:dependency-review at=2026-08-11T00:00:00Z"
  end
  cleared = lambda do |unit, status|
    {unit: unit, generation: "2", task: "—", branch: "—", worktree: "—", assignment: "—", worker_commit: "—", checkpoint: "—", gate_revision: "—", fingerprint: "—", external: "—",
     transition: "v3 #{status} owner=— at=2026-08-11T00:00:01Z"}
  end
  entries = [previous_entries[0].dup, cleared.call("b", "ready"), cleared.call("c", "proposed"), previous_entries[3].dup]
  revision = {revision_id: "fixture-b", unit: "b", previous_requires: previous_requires, current_requires: current_requires,
              affected_units: %w[b c], reason: "reason:fixture", change_digest: "", approver: approver,
              recorded_at: "2026-08-11T00:00:01Z"}
  revision[:change_digest] = dependency_revision_digest(revision)
  resources = []
  dispositions = []
  if active
    resources = [
      {id: "worktree-b", type: "worktree", owner: "worker-b", target: "/fixture/b", state: resource_state,
       evidence: resource_state == "removed" ? ".ai/evidence/worktree-b.md" : "not-yet-needed:worktree-b",
       clean: resource_state == "retained-for-recovery", head: "1" * 40}
    ]
    dispositions = [
      {disposition_id: "fixture-b-assignment", revision_ids: ["fixture-b"], unit: "b", generation: "1",
       task: "worker-b", branch: "feature/b", worktree: "/fixture/b", assignment: "0" * 40,
       proof: "clean-checkpoint:#{'1' * 40}", evidence_path: ".ai/evidence/fixture-b-disposition.json",
       preserved_commit: "1" * 40,
       evidence_digest: "",
       resource_states: {"worktree-b" => resource_state},
       recorded_at: "2026-08-11T00:00:01Z"}
    ]
  end
  previous_resources = resources.map { |resource| resource.merge(state: "active", evidence: "not-yet-needed:#{resource[:id]}", clean: true, head: "1" * 40) }
  if active
    dispositions[0][:evidence_record] = dependency_disposition_evidence_fixture(dispositions[0], previous_resources, resources)
    dispositions[0][:evidence_digest] = dependency_disposition_evidence_digest(dispositions[0][:evidence_record])
  end
  {previous_rows: previous_rows, previous_entries: previous_entries, rows: rows, entries: entries,
   revisions: [revision], dispositions: dispositions, resources: resources, previous_resources: previous_resources}
end

{
  "add" => dependency_revision_fixture(["a"], %w[a d]),
  "remove" => dependency_revision_fixture(%w[a d], ["a"]),
  "correction" => dependency_revision_fixture(["a"], ["d"])
}.each do |label, fixture|
  errors, = dependency_revision_transition_errors(**fixture)
  fail_check("valid dependency #{label} fixture was rejected: #{errors.join('; ')}") unless errors.empty?
end

phone_units = %w[identity identity/otp identity/delivery identity/risk identity/phone]
phone_previous_requires = %w[identity identity/otp identity/delivery]
phone_current_requires = %w[identity identity/otp identity/delivery identity/risk]
phone_row = lambda do |unit, requires, status|
  {unit: unit, module: "pkg/#{unit}", requires: requires, status: status, owner: "—", goal: "goals/#{unit.tr('/', '-')}.md"}
end
phone_entry = lambda do |unit, generation, status, transition_version_number|
  {unit: unit, generation: generation.to_s, task: status == "verified" ? "worker-#{unit.tr('/', '-')}" : "—",
   branch: status == "verified" ? "feature/#{unit.tr('/', '-')}" : "—", worktree: status == "verified" ? "/fixture/#{unit}" : "—",
   assignment: status == "verified" ? "0" * 40 : "—", worker_commit: status == "verified" ? "0" * 40 : "—",
   checkpoint: status == "verified" ? "0" * 40 : "—", gate_revision: status == "verified" ? "0" * 40 : "—", fingerprint: status == "verified" ? "sha256:#{'0' * 64}" : "—",
   external: status == "verified" ? "not-needed" : "—",
   transition: "v#{transition_version_number} #{status} owner=— at=2026-08-11T00:00:0#{transition_version_number - 1}Z"}
end
phone_previous_rows = phone_units.map { |unit| phone_row.call(unit, unit == "identity/phone" ? phone_previous_requires : [], "verified") }
phone_rows = phone_units.map { |unit| phone_row.call(unit, unit == "identity/phone" ? phone_current_requires : [], unit == "identity/phone" ? "ready" : "verified") }
phone_previous_entries = phone_units.map { |unit| phone_entry.call(unit, 1, "verified", 2) }
phone_entries = phone_previous_entries.map(&:dup)
phone_entries[-1] = phone_entry.call("identity/phone", 2, "ready", 3)
phone_revision_row = {
  revision_id: "fixture-identity-phone", unit: "identity/phone",
  previous_requires: phone_previous_requires, current_requires: phone_current_requires,
  affected_units: ["identity/phone"], reason: "reason:risk-composition", change_digest: "",
  approver: "coordinator", recorded_at: "2026-08-11T00:00:01Z"
}
phone_revision_row[:change_digest] = dependency_revision_digest(phone_revision_row)
phone_revision = {previous_rows: phone_previous_rows, previous_entries: phone_previous_entries,
                  rows: phone_rows, entries: phone_entries, revisions: [phone_revision_row]}
phone_errors, = dependency_revision_transition_errors(**phone_revision)
fail_check("valid identity/phone dependency fixture was rejected: #{phone_errors.join('; ')}") unless phone_errors.empty?

%w[retained-for-recovery removed].each do |resource_state|
  active_fixture = dependency_revision_fixture(["a"], %w[a d], active: true, resource_state: resource_state)
  active_errors, = dependency_revision_transition_errors(**active_fixture)
  fail_check("valid active #{resource_state} dependency fixture was rejected: #{active_errors.join('; ')}") unless active_errors.empty?
end
clean_baseline_removal = dependency_revision_fixture(["a"], %w[a d], active: true, resource_state: "removed")
clean_baseline_removal[:previous_entries][1].merge!(worker_commit: "—", checkpoint: "—", gate_revision: "—", fingerprint: "—")
clean_baseline_removal[:previous_resources][0][:head] = "0" * 40
clean_baseline_removal[:dispositions][0].merge!(proof: "clean-baseline:#{'0' * 40}", preserved_commit: "0" * 40)
clean_baseline_removal[:dispositions][0][:evidence_record] = dependency_disposition_evidence_fixture(
  clean_baseline_removal[:dispositions][0], clean_baseline_removal[:previous_resources], clean_baseline_removal[:resources]
)
clean_baseline_removal[:dispositions][0][:evidence_digest] = dependency_disposition_evidence_digest(clean_baseline_removal[:dispositions][0][:evidence_record])
clean_baseline_errors, = dependency_revision_transition_errors(**clean_baseline_removal)
fail_check("valid clean-baseline removal fixture was rejected: #{clean_baseline_errors.join('; ')}") unless clean_baseline_errors.empty?
recoverable_commit_removal = dependency_revision_fixture(["a"], %w[a d], active: true, resource_state: "removed")
recoverable_commit_removal[:dispositions][0][:proof] = "safe-abandonment:reason:fixture-abandonment"
recoverable_commit_removal[:dispositions][0][:evidence_record] = dependency_disposition_evidence_fixture(
  recoverable_commit_removal[:dispositions][0], recoverable_commit_removal[:previous_resources], recoverable_commit_removal[:resources]
)
recoverable_commit_removal[:dispositions][0][:evidence_digest] = dependency_disposition_evidence_digest(recoverable_commit_removal[:dispositions][0][:evidence_record])
recoverable_commit_errors, = dependency_revision_transition_errors(**recoverable_commit_removal)
fail_check("valid recoverable-commit removal fixture was rejected: #{recoverable_commit_errors.join('; ')}") unless recoverable_commit_errors.empty?
ordinary_fixture_errors = ordinary_abandonment_errors(
  recoverable_commit_removal[:dispositions][0], recoverable_commit_removal[:previous_entries][1].merge(unit: "b"),
  recoverable_commit_removal[:previous_resources], recoverable_commit_removal[:resources]
)
fail_check("valid ordinary-abandonment history fixture was rejected: #{ordinary_fixture_errors.join('; ')}") unless ordinary_fixture_errors.empty?
ordinary_wrong_identity = Marshal.load(Marshal.dump(recoverable_commit_removal[:dispositions][0]))
ordinary_wrong_identity[:evidence_record]["unit"] = "c"
ordinary_wrong_identity[:evidence_digest] = dependency_disposition_evidence_digest(ordinary_wrong_identity[:evidence_record])
if ordinary_abandonment_errors(
  ordinary_wrong_identity, recoverable_commit_removal[:previous_entries][1].merge(unit: "b"),
  recoverable_commit_removal[:previous_resources], recoverable_commit_removal[:resources]
).empty?
  fail_check("ordinary-abandonment mismatched-identity fixture was accepted")
end

dependency_negative_base = dependency_revision_fixture(["a"], %w[a d])
missing_demotion = Marshal.load(Marshal.dump(dependency_negative_base))
missing_demotion[:rows][2][:status] = "verified"
missing_demotion[:entries][2] = missing_demotion[:previous_entries][2].merge(transition: "v3 verified owner=— at=2026-08-11T00:00:01Z")
fail_check("dependency missing-demotion fixture was accepted") if dependency_revision_transition_errors(**missing_demotion).first.empty?
retained_assignment = Marshal.load(Marshal.dump(dependency_negative_base))
retained_assignment[:entries][1] = retained_assignment[:previous_entries][1].merge(transition: "v3 ready owner=— at=2026-08-11T00:00:01Z")
fail_check("dependency active-assignment fixture was accepted") if dependency_revision_transition_errors(**retained_assignment).first.empty?
dirty_active = dependency_revision_fixture(["a"], %w[a d], active: true)
dirty_active[:resources][0][:clean] = false
dirty_active[:dispositions][0][:proof] = "—"
fail_check("dependency dirty-active/no-proof fixture was accepted") if dependency_revision_transition_errors(**dirty_active).first.empty?
retained_safe_abandonment = dependency_revision_fixture(["a"], %w[a d], active: true)
retained_safe_abandonment[:resources][0][:clean] = false
retained_safe_abandonment[:dispositions][0][:proof] = "safe-abandonment:reason:fixture-abandonment"
retained_safe_abandonment[:dispositions][0][:evidence_record] = dependency_disposition_evidence_fixture(
  retained_safe_abandonment[:dispositions][0], retained_safe_abandonment[:previous_resources], retained_safe_abandonment[:resources]
)
retained_safe_abandonment[:dispositions][0][:evidence_digest] = dependency_disposition_evidence_digest(retained_safe_abandonment[:dispositions][0][:evidence_record])
fail_check("dependency retained safe-abandonment fixture was accepted") if dependency_revision_transition_errors(**retained_safe_abandonment).first.empty?
unrelated_abandonment_evidence = Marshal.load(Marshal.dump(recoverable_commit_removal))
unrelated_abandonment_evidence[:dispositions][0][:evidence_record]["unit"] = "c"
fail_check("dependency unrelated abandonment-evidence fixture was accepted") if dependency_revision_transition_errors(**unrelated_abandonment_evidence).first.empty?
historical_mutated_evidence = Marshal.load(Marshal.dump(recoverable_commit_removal))
historical_mutated_evidence[:dispositions][0][:evidence_record]["resources"][0]["pre_removal_head"] = "f" * 40
fail_check("dependency historical mutated-evidence fixture was accepted") if dependency_revision_transition_errors(**historical_mutated_evidence).first.empty?
removed_without_clean_evidence = dependency_revision_fixture(["a"], %w[a d], active: true, resource_state: "removed")
removed_without_clean_evidence[:dispositions][0][:evidence_record]["resources"][0]["pre_removal_clean"] = nil
removed_without_clean_evidence[:dispositions][0][:evidence_record]["resources"][0]["pre_removal_head"] = nil
removed_without_clean_evidence[:dispositions][0][:evidence_digest] = dependency_disposition_evidence_digest(removed_without_clean_evidence[:dispositions][0][:evidence_record])
fail_check("dependency removed-without-clean-evidence fixture was accepted") if dependency_revision_transition_errors(**removed_without_clean_evidence).first.empty?
dirty_abandonment = Marshal.load(Marshal.dump(recoverable_commit_removal))
dirty_abandonment[:dispositions][0][:evidence_record]["resources"][0]["pre_removal_clean"] = false
dirty_abandonment[:dispositions][0][:evidence_digest] = dependency_disposition_evidence_digest(dirty_abandonment[:dispositions][0][:evidence_record])
fail_check("dependency dirty-abandonment fixture was accepted") if dependency_revision_transition_errors(**dirty_abandonment).first.empty?
wrong_removed_head = Marshal.load(Marshal.dump(recoverable_commit_removal))
wrong_removed_head[:dispositions][0][:evidence_record]["resources"][0]["pre_removal_head"] = "f" * 40
wrong_removed_head[:dispositions][0][:evidence_digest] = dependency_disposition_evidence_digest(wrong_removed_head[:dispositions][0][:evidence_record])
fail_check("dependency wrong-removed-HEAD fixture was accepted") if dependency_revision_transition_errors(**wrong_removed_head).first.empty?
unpreserved_removal = Marshal.load(Marshal.dump(recoverable_commit_removal))
unpreserved_removal[:dispositions][0][:preserved_commit] = "f" * 40
unpreserved_removal[:dispositions][0][:evidence_record]["preservation"]["recoverable_commit"] = "f" * 40
unpreserved_removal[:dispositions][0][:evidence_record]["resources"][0]["pre_removal_head"] = "f" * 40
unpreserved_removal[:dispositions][0][:evidence_digest] = dependency_disposition_evidence_digest(unpreserved_removal[:dispositions][0][:evidence_record])
fail_check("dependency unpreserved-work removal fixture was accepted") if dependency_revision_transition_errors(**unpreserved_removal).first.empty?
unpaused_active = dependency_revision_fixture(["a"], %w[a d], active: true)
unpaused_active[:previous_rows][1].merge!(status: "in-progress", owner: "worker-b")
unpaused_active[:previous_entries][1][:transition] = "v2 in-progress owner=worker-b at=2026-08-11T00:00:00Z"
fail_check("dependency unpaused-active fixture was accepted") if dependency_revision_transition_errors(**unpaused_active).first.empty?
lost_resource = dependency_revision_fixture(["a"], %w[a d], active: true)
lost_resource[:resources].clear
fail_check("dependency lost-resource fixture was accepted") if dependency_revision_transition_errors(**lost_resource).first.empty?
unregistered_worktree = dependency_revision_fixture(["a"], %w[a d], active: true)
unregistered_worktree[:resources][0][:target] = "/fixture/other"
fail_check("dependency unregistered-worktree fixture was accepted") if dependency_revision_transition_errors(**unregistered_worktree).first.empty?
unauthorized_revision = dependency_revision_fixture(["a"], %w[a d], approver: "worker-b")
fail_check("dependency unauthorized-worker fixture was accepted") if dependency_revision_transition_errors(**unauthorized_revision).first.empty?
stale_evidence = Marshal.load(Marshal.dump(dependency_negative_base))
stale_evidence[:entries][2][:external] = "not-needed"
fail_check("dependency stale-evidence fixture was accepted") if dependency_revision_transition_errors(**stale_evidence).first.empty?
cycle_revision = dependency_revision_fixture(["a"], ["c"])
cycle_revision[:revisions][0][:current_requires] = ["c"]
cycle_revision[:revisions][0][:change_digest] = dependency_revision_digest(cycle_revision[:revisions][0])
fail_check("dependency cycle fixture was accepted") if dependency_revision_transition_errors(**cycle_revision).first.empty?

ledger_fixture_row = {unit: "identity", module: "pkg/identity", requires: [], status: "ready", owner: "—", goal: "goals/identity.md"}
ledger_fixture_entry = {generation: "0", task: "—", branch: "—", worktree: "—", assignment: "—", worker_commit: "—", checkpoint: "—", gate_revision: "—", fingerprint: "—", external: "—", transition: "v1 ready owner=— at=2026-08-11T00:00:00Z"}
progress_fixture_row = ledger_fixture_row.merge(status: "in-progress", owner: "fixture-worker")
progress_fixture_entry = ledger_fixture_entry.merge(task: "fixture-worker", branch: "feature/fixture", worktree: "/fixture/worker", assignment: "0" * 40, transition: "v2 in-progress owner=fixture-worker at=2026-08-11T00:00:01Z")
fail_check("valid ready-to-in-progress transition fixture was rejected") unless ledger_transition_errors(ledger_fixture_row, ledger_fixture_entry, progress_fixture_row, progress_fixture_entry).empty?
verified_fixture_row = ledger_fixture_row.merge(status: "verified")
verified_fixture_entry = progress_fixture_entry.merge(worker_commit: "0" * 40, checkpoint: "0" * 40, gate_revision: "0" * 40, fingerprint: "sha256:#{'0' * 64}", external: "not-needed", transition: "v2 verified owner=— at=2026-08-11T00:00:01Z")
fail_check("ready-to-verified transition negative fixture was accepted") if ledger_transition_errors(ledger_fixture_row, ledger_fixture_entry, verified_fixture_row, verified_fixture_entry).empty?
distinct_gate_fixture = verified_fixture_entry.merge(checkpoint: "1" * 40, gate_revision: "2" * 40)
unless ledger_row_shape_errors(verified_fixture_row, distinct_gate_fixture).empty?
  fail_check("distinct gate-execution revision fixture was rejected")
end
blocked_fixture_row = progress_fixture_row.merge(status: "blocked", owner: "blocker:fixture")
blocked_fixture_entry = progress_fixture_entry.merge(transition: "v2 blocked owner=blocker:fixture at=2026-08-11T00:00:01Z")
nonincremented_blocked = blocked_fixture_entry.merge(transition: "v2 blocked owner=blocker:fixture at=2026-08-11T00:00:02Z")
fail_check("nonincremented transition-version negative fixture was accepted") if ledger_transition_errors(progress_fixture_row, progress_fixture_entry, blocked_fixture_row, nonincremented_blocked).empty?
abandoned_fixture_row = ledger_fixture_row.merge(status: "ready", owner: "—")
abandoned_fixture_entry = ledger_fixture_entry.merge(generation: "1", transition: "v3 ready owner=— at=2026-08-11T00:00:02Z")
fail_check("naked assignment-abandonment fixture was accepted") if ledger_transition_errors(blocked_fixture_row, blocked_fixture_entry, abandoned_fixture_row, abandoned_fixture_entry).empty?
unless ledger_transition_errors(blocked_fixture_row, blocked_fixture_entry, abandoned_fixture_row, abandoned_fixture_entry, abandonment_evidence: true).empty?
  fail_check("evidenced assignment-abandonment history fixture was rejected")
end
repair_progress_entry = progress_fixture_entry.merge(worker_commit: "1" * 40)
repair_blocked_entry = blocked_fixture_entry.merge(worker_commit: "2" * 40, transition: "v3 blocked owner=blocker:fixture at=2026-08-11T00:00:02Z")
if ledger_transition_errors(progress_fixture_row, repair_progress_entry, blocked_fixture_row, repair_blocked_entry).any?
  fail_check("descendant repair-checkpoint replacement fixture was rejected")
end

def external_evidence_record_errors(record, profile:, claims:, consumers:, recorded_after:, evidence_commit: nil, module_roots: [], repository_root: nil, record_path: nil)
  errors = []
  errors << "record schema keys drifted" unless record.keys == %w[schema_version recorded_at profiles]
  errors << "schema version is not 2" unless record["schema_version"] == 2
  errors << "evidence commit is invalid" unless !repository_root || evidence_commit.to_s.match?(/\A[0-9a-f]{40}\z/)
  recorded_at = record["recorded_at"].to_s
  errors << "recorded_at is invalid" unless rfc3339?(recorded_at)
  if rfc3339?(recorded_at)
    errors << "record predates execution preflight" if Time.iso8601(recorded_at) < recorded_after
    errors << "record timestamp is in the future" if Time.iso8601(recorded_at) > Time.now.utc
  end
  profiles = record["profiles"]
  unless profiles.is_a?(Array)
    errors << "profiles are missing"
    return errors
  end
  entry = profiles.find { |candidate| candidate.is_a?(Hash) && candidate["profile_id"] == profile }
  unless entry
    errors << "profile #{profile} is missing"
    return errors
  end
  errors << "profile schema keys drifted" unless entry.keys.sort == %w[claim_ids profile_id unit_results]
  errors << "claim attribution drifted" unless entry["claim_ids"] == claims
  results = entry["unit_results"]
  unless results.is_a?(Array) && results.map { |result| result["unit"] } == consumers
    errors << "unit attribution drifted"
    return errors
  end
  results.each do |result|
    expected_result_keys = %w[unit outcome tested_revision gate_execution_revision revalidation_revision input_manifest input_root tool_environment artifacts]
    errors << "#{result['unit']} result schema keys drifted" unless result.keys == expected_result_keys
    errors << "#{result['unit']} outcome is not pass" unless result["outcome"] == "pass"
    tested_revision = result["tested_revision"]
    gate_revision = result["gate_execution_revision"]
    evidence_revision_reuse_errors(
      tested_revision: tested_revision, gate_execution_revision: gate_revision,
      revalidation_revision: result["revalidation_revision"], input_manifest: result["input_manifest"],
      input_root: result["input_root"], module_roots: module_roots, repository_root: repository_root
    ).each { |error| errors << "#{result['unit']} #{error}" }
    identity = result["tool_environment"]
    errors << "#{result['unit']} tool/environment identity is missing" unless identity.is_a?(Hash) && identity.keys == %w[tool environment] && identity.values.all? { |value| value.is_a?(String) && !value.empty? && !value.match?(/[\r\n]/) }
    artifacts = result["artifacts"]
    valid_artifacts = artifacts.is_a?(Array) && artifacts.any? && artifacts.all? do |artifact|
      next false unless artifact.is_a?(Hash) && artifact.keys == %w[name path sha256]
      name = artifact["name"]
      path = artifact["path"]
      digest = artifact["sha256"]
      next false unless name.match?(/\A[a-zA-Z0-9._-]+\z/) && path.match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z}) && !path.include?("..") && digest.match?(/\Asha256:[0-9a-f]{64}\z/)
      next true unless repository_root

      absolute = File.expand_path(path, repository_root)
      committed = git_blob_bytes(evidence_commit, path, repository: repository_root)
      absolute.start_with?(repository_root + File::SEPARATOR) && File.file?(absolute) && committed &&
        File.binread(absolute) == committed && digest == "sha256:#{Digest::SHA256.hexdigest(committed)}"
    end
    errors << "#{result['unit']} artifacts are invalid" unless valid_artifacts
    if repository_root && evidence_commit.to_s.match?(/\A[0-9a-f]{40}\z/)
      errors << "#{result['unit']} gate execution revision does not exist" unless git_commit_exists?(gate_revision)
      errors << "#{result['unit']} evidence commit does not descend from gate execution revision" unless git_commit_exists?(gate_revision) && git_ancestor?(gate_revision, evidence_commit)
    end
  end
  if repository_root && evidence_commit.to_s.match?(/\A[0-9a-f]{40}\z/)
    errors << "evidence commit does not exist" unless git_commit_exists?(evidence_commit)
    errors << "evidence commit is not integrated" unless git_ancestor?(evidence_commit, "HEAD")
  end
  if repository_root && record_path
    committed_record = git_blob_bytes("HEAD", record_path, repository: repository_root)
    absolute_record = File.expand_path(record_path, repository_root)
    errors << "external evidence record is not committed exactly" unless committed_record && File.file?(absolute_record) && File.binread(absolute_record) == committed_record
  end
  errors
end

def external_result_ledger_errors(result, gate_revision:, input_fingerprint:)
  errors = []
  errors << "external gate execution revision does not match ledger" unless result["gate_execution_revision"] == gate_revision
  errors << "external input root does not match gate fingerprint" unless result["input_root"] == input_fingerprint
  errors
end

def orchestration_policy_errors(orchestrator, worker)
  coordinator_section = orchestrator[/^## Coordinator-only role\n.*?(?=^## |\z)/m].to_s
  dependency_revision_section = orchestrator[/^## Dependency revision ownership\n.*?(?=^## |\z)/m].to_s
  worker_assignment = worker[/```text\n(.*?)```/m, 1].to_s
  required_orchestrator = "The coordinator MUST orchestrate and MUST NOT implement package production code, package tests, package migrations, provider adapters, or package-local documentation."
  required_worker = [
    "You are the sole implementation owner for <canonical-module>.",
    "Use model gpt-5.6-sol with medium reasoning. Do not spawn subagents.",
    "modify only <canonical-module-directory>;",
    "the canonical root does not grant ownership of another inventory unit nested beneath it",
    "MUST NOT edit any inventory `Requires` set, dependency edge, or goal dependency metadata",
    "do not edit another package, root manifests, root catalogs, global inventory, dependency graph, program documents, or another worktree;"
  ]
  errors = []
  errors << "coordinator-only policy digest drifted" unless Digest::SHA256.hexdigest(coordinator_section) == "bc9277b2d53aee15339a6d53394a5197412233e9c4ce1b3a3b71ffbf0eec6140"
  errors << "dependency-revision policy digest drifted" unless Digest::SHA256.hexdigest(dependency_revision_section) == "564cb53e19c5de9ee04cde3788fae20b812d7873a656ea8b086cbfb848ddea28"
  errors << "worker assignment policy digest drifted" unless Digest::SHA256.hexdigest(worker_assignment) == "cd8bfc86788aab30ff101c90dce000adce31569cce5838f30b832d69caf99e6b"
  errors << "coordinator implementation prohibition drifted" unless orchestrator.gsub(/\s+/, " ").include?(required_orchestrator)
  required_worker.each do |required|
    errors << "worker ownership restriction drifted: #{required}" unless worker.gsub(/\s+/, " ").include?(required)
  end
  errors
end

external_fixture_manifest = [{
  "path_or_environment_id" => "environment:fixture", "kind" => "environment",
  "content_identity" => "sha256:#{'0' * 64}", "owner" => "coordinator", "reason" => "fixture"
}]
external_record_fixture = {
  "schema_version" => 2,
  "recorded_at" => "2026-08-11T00:00:00Z",
  "profiles" => [{
    "profile_id" => "fixture-provider",
    "claim_ids" => ["claim.one"],
    "unit_results" => [{
      "unit" => "identity", "outcome" => "pass", "tested_revision" => "0" * 40,
      "gate_execution_revision" => "0" * 40, "revalidation_revision" => nil,
      "input_manifest" => external_fixture_manifest,
      "input_root" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(external_fixture_manifest)))}", "tool_environment" => {"tool" => "fixture-v1", "environment" => "fixture-environment"},
      "artifacts" => [{"name" => "response", "path" => ".ai/identity-platform/fixtures/local-gate-artifact.txt", "sha256" => "sha256:#{'0' * 64}"}]
    }]
  }]
}
external_fixture_args = {profile: "fixture-provider", claims: ["claim.one"], consumers: ["identity"], recorded_after: Time.iso8601("2026-08-11T00:00:00Z")}
fail_check("valid external evidence fixture was rejected") unless external_evidence_record_errors(external_record_fixture, **external_fixture_args).empty?
external_fixture_result = external_record_fixture.fetch("profiles").first.fetch("unit_results").first
external_fixture_fingerprint = external_fixture_result.fetch("input_root")
unless external_result_ledger_errors(external_fixture_result, gate_revision: "0" * 40, input_fingerprint: external_fixture_fingerprint).empty?
  fail_check("valid v2 external ledger binding fixture was rejected")
end
legacy_external_binding_fixture = external_fixture_result.merge(
  "gate_execution_revision" => nil, "input_root" => nil,
  "execution_revision" => "0" * 40, "complete_input_fingerprint" => external_fixture_fingerprint
)
if external_result_ledger_errors(legacy_external_binding_fixture, gate_revision: "0" * 40, input_fingerprint: external_fixture_fingerprint).empty?
  fail_check("legacy external ledger field fixture was accepted")
end
external_reuse_fixture = JSON.parse(JSON.generate(external_record_fixture))
external_reuse_result = external_reuse_fixture.fetch("profiles").first.fetch("unit_results").first
external_reuse_result["gate_execution_revision"] = "1" * 40
external_reuse_result["revalidation_revision"] = "1" * 40
fail_check("valid external fingerprint-reuse fixture was rejected") unless external_evidence_record_errors(external_reuse_fixture, **external_fixture_args).empty?
{
  "claim attribution" => lambda { |record| record["profiles"][0]["claim_ids"] = ["claim.other"] },
  "failed outcome" => lambda { |record| record["profiles"][0]["unit_results"][0]["outcome"] = "failed" },
  "missing artifact digest" => lambda { |record| record["profiles"][0]["unit_results"][0]["artifacts"] = [] },
  "reuse without revalidation revision" => lambda { |record| record["profiles"][0]["unit_results"][0]["gate_execution_revision"] = "1" * 40 },
  "revalidation revision without reuse" => lambda { |record| record["profiles"][0]["unit_results"][0]["revalidation_revision"] = "0" * 40 },
  "mismatched input root" => lambda { |record| record["profiles"][0]["unit_results"][0]["input_root"] = "sha256:#{'1' * 64}" },
  "stale timestamp" => lambda { |record| record["recorded_at"] = "2026-08-10T23:59:59Z" }
}.each do |label, mutate|
  fixture = JSON.parse(JSON.generate(external_record_fixture))
  mutate.call(fixture)
  fail_check("external evidence negative fixture #{label} was accepted") if external_evidence_record_errors(fixture, **external_fixture_args).empty?
end

def repository_evidence_path?(value)
  return false unless value.match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z}) && !value.split("/").include?("..")

  path = File.expand_path(value, REPOSITORY_ROOT)
  path.start_with?(REPOSITORY_ROOT + File::SEPARATOR) && File.file?(path)
end

def repository_evidence_binding(value)
  match = value.to_s.match(/\A(\.ai\/[a-zA-Z0-9._\/-]+)@([0-9a-f]{40})\z/)
  return unless match && !match[1].split("/").include?("..")

  [match[1], match[2]]
end

def safe_preflight_evidence_or_blocker?(value)
  repository_evidence_path?(value) || value.match?(/\A(?:blocker|not-yet-needed):[a-zA-Z0-9._-]+\z/)
end

def take_path_option!(arguments, option)
  index = arguments.index(option)
  return unless index

  path = arguments.delete_at(index + 1)
  arguments.delete_at(index)
  fail_check("#{option} requires a path") unless path && File.file?(path)
  path
end

def git_commit_exists?(revision)
  _output, _error, status = Open3.capture3("git", "-C", REPOSITORY_ROOT, "cat-file", "-e", "#{revision}^{commit}")
  status.success?
end

def git_ancestor?(ancestor, descendant)
  _output, _error, status = Open3.capture3("git", "-C", REPOSITORY_ROOT, "merge-base", "--is-ancestor", ancestor, descendant)
  status.success?
end

def git_output(*arguments, repository: REPOSITORY_ROOT)
  output, _error, status = Open3.capture3("git", "-C", repository, *arguments)
  status.success? ? output.strip : nil
end

def git_blob_bytes(revision, path, repository: REPOSITORY_ROOT)
  return unless revision&.match?(/\A(?:[0-9a-f]{40}|HEAD)\z/) && path&.match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z}) && !path.include?("..")

  output, _error, status = Open3.capture3("git", "-C", repository, "show", "#{revision}:#{path}")
  status.success? ? output : nil
end

def tracked_behavior_input_manifest(revision, module_roots, repository: REPOSITORY_ROOT)
  return unless revision&.match?(/\A[0-9a-f]{40}\z/)
  return unless git_output("cat-file", "-e", "#{revision}^{commit}", repository: repository)

  roots = [
    ".ai/identity-platform", "AGENTS.md", "Makefile", "modules.json", "packages.json",
    "go.work", "go.work.sum", "scripts"
  ] + module_roots
  listing = git_output("ls-tree", "-r", "--full-tree", revision, "--", *roots.uniq.sort_by(&:b), repository: repository)
  return unless listing

  listing.lines.filter_map do |line|
    match = line.match(/\A\d+ (\S+) ([0-9a-f]{40})\t(.+)\n?\z/)
    next unless match

    path = match[3]
    next if NON_BEHAVIORAL_IDENTITY_PLATFORM_INPUTS.any? do |excluded|
      excluded.end_with?("/") ? path.start_with?(excluded) : path == excluded
    end
    owner = module_roots.select { |root| path == root || path.start_with?("#{root}/") }.max_by(&:length) || "coordinator"
    reason = path.start_with?(".ai/identity-platform/") ? "normative identity-platform input" : "behavior-affecting repository input"
    {
      "path_or_environment_id" => path, "kind" => match[1],
      "content_identity" => "git-sha1:#{match[2]}", "owner" => owner, "reason" => reason
    }
  end.sort_by { |entry| entry.fetch("path_or_environment_id").b }
end

def behavior_input_manifest_errors(manifest, revision:, module_roots:, repository: REPOSITORY_ROOT)
  errors = []
  unless manifest.is_a?(Array) && manifest.any?
    return ["input manifest is absent"]
  end
  expected_keys = %w[path_or_environment_id kind content_identity owner reason]
  errors << "input manifest entry schema drifted" unless manifest.all? { |entry| entry.is_a?(Hash) && entry.keys == expected_keys }
  return errors unless errors.empty?

  identities = manifest.map { |entry| entry.fetch("path_or_environment_id") }
  errors << "input manifest identities are not sorted and unique" unless identities == identities.sort_by(&:b).uniq
  environment, tracked = manifest.partition { |entry| entry.fetch("path_or_environment_id").start_with?("environment:") }
  expected_tracked = tracked_behavior_input_manifest(revision, module_roots, repository: repository)
  errors << "tracked behavior input manifest drifted" unless expected_tracked && tracked == expected_tracked
  errors << "environment input closure drifted" unless environment.map { |entry| entry.fetch("path_or_environment_id") } == REQUIRED_ENVIRONMENT_INPUT_IDS
  environment.each do |entry|
    errors << "environment input kind drifted" unless entry["kind"] == "environment"
    errors << "environment input owner drifted" unless entry["owner"] == "coordinator"
    errors << "environment input identity is invalid" unless entry["content_identity"].match?(/\Asha256:[0-9a-f]{64}\z/)
    errors << "environment input reason is missing" unless entry["reason"].is_a?(String) && !entry["reason"].empty?
  end
  errors
end

def behavior_input_fingerprint(manifest)
  return unless manifest.is_a?(Array)

  "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(manifest)))}"
end

def identity_platform_base_tree(base)
  object = git_output("rev-parse", "#{base}:.ai/identity-platform")
  listing = git_output("ls-tree", "-r", "--full-tree", base, "--", ".ai/identity-platform")
  return unless object&.match?(/\A[0-9a-f]{40}\z/) && listing

  [object, "sha256:#{Digest::SHA256.hexdigest(listing + "\n")}"]
end

def base_tree_identity_errors(base, recorded_tree, recorded_digest)
  expected = identity_platform_base_tree(base)
  errors = []
  errors << "execution committed base lacks the identity-platform tree" unless expected
  errors << "execution identity-platform base tree object drifted" unless expected && recorded_tree == expected[0]
  errors << "execution identity-platform base tree digest drifted" unless expected && recorded_digest == expected[1]
  errors
end

def integration_cleanliness_errors(status_output:, status_success:, current_root: nil, integration_root: nil)
  errors = []
  errors << "execution integration worktree status is unavailable" unless status_success
  errors << "execution integration worktree is dirty" unless status_output.empty?
  if current_root && integration_root
    errors << "execution validator is not running from the registered integration worktree" unless current_root == integration_root
  end
  errors
end

def completion_mode_errors(execution_mode:, fixture_mode:, all_verified:, clean_integration_mode:)
  return [] unless execution_mode && !fixture_mode && all_verified

  clean_integration_mode ? [] : ["all-verified execution requires --clean-integration"]
end

def execution_identity_errors(identity_rows, require_clean: false)
  errors = []
  base = plain_cell(identity_rows.fetch("Recorded committed `main` base", ""))
  input_revision = plain_cell(identity_rows.fetch("Preflight input revision before the record commit", ""))
  branch = plain_cell(identity_rows.fetch("Integration branch", ""))
  integration_worktree = plain_cell(identity_rows.fetch("Integration worktree", ""))
  worktree_parent = plain_cell(identity_rows.fetch("Task-owned worktree parent", ""))
  recorded_tree = plain_cell(identity_rows.fetch("Identity-platform base tree object", ""))
  recorded_digest = plain_cell(identity_rows.fetch("Identity-platform base tree digest", ""))
  recorded_at = plain_cell(identity_rows.fetch("Preflight recorded at (RFC3339)", ""))
  errors << "execution preflight committed base is invalid or missing" unless base.match?(/\A[0-9a-f]{40}\z/) && git_commit_exists?(base)
  errors << "execution preflight input revision is invalid or missing" unless input_revision.match?(/\A[0-9a-f]{40}\z/) && git_commit_exists?(input_revision)
  errors << "execution preflight integration branch is not conventional" unless branch.match?(%r{\A(?:feature|bugfix|hotfix|release|chore|refactor)/[a-zA-Z0-9._/-]+\z})
  errors << "execution preflight worktree paths are unsafe" unless safe_integration_worktree_path?(integration_worktree, worktree_parent)
  errors << "execution preflight timestamp is invalid" unless rfc3339?(recorded_at)
  return errors unless errors.grep(/base|input|branch|worktree/).empty?

  branch_head = git_output("rev-parse", "refs/heads/#{branch}")
  worktree_head = git_output("rev-parse", "HEAD", repository: integration_worktree)
  worktree_branch = git_output("symbolic-ref", "--short", "HEAD", repository: integration_worktree)
  errors << "execution integration branch is not registered" unless branch_head&.match?(/\A[0-9a-f]{40}\z/)
  errors << "execution integration worktree branch identity drifted" unless worktree_branch == branch
  errors << "execution integration branch/worktree HEAD drifted" unless branch_head && worktree_head == branch_head
  errors << "execution integration HEAD does not descend from exact base" unless worktree_head && git_ancestor?(base, worktree_head)
  errors << "execution preflight input does not descend from exact base" unless git_ancestor?(base, input_revision)
  errors << "execution preflight input is not on integration history" unless worktree_head && git_ancestor?(input_revision, worktree_head)
  if require_clean
    status_output, _status_error, status = Open3.capture3(
      "git", "-C", integration_worktree, "status", "--porcelain", "--untracked-files=all"
    )
    errors.concat(integration_cleanliness_errors(
      status_output: status_output, status_success: status.success?,
      current_root: File.realpath(REPOSITORY_ROOT), integration_root: File.realpath(integration_worktree)
    ))
  end
  errors.concat(base_tree_identity_errors(base, recorded_tree, recorded_digest))
  errors
end


fake_execution_identity = {
  "Recorded committed `main` base" => "f" * 40,
  "Preflight input revision before the record commit" => "e" * 40,
  "Integration branch" => "feature/fixture", "Integration worktree" => "/fixture/integration",
  "Task-owned worktree parent" => "/fixture", "Identity-platform base tree object" => "d" * 40,
  "Identity-platform base tree digest" => "sha256:#{'c' * 64}",
  "Preflight recorded at (RFC3339)" => "2026-08-11T00:00:00Z"
}
unless execution_identity_errors(fake_execution_identity).any? { |error| error.include?("committed base is invalid or missing") }
  fail_check("fake preflight commit negative fixture was accepted")
end
current_base_tree = identity_platform_base_tree("HEAD")
fail_check("current identity-platform tree fixture is unavailable") unless current_base_tree
if base_tree_identity_errors("HEAD", "0" * 40, "sha256:#{'0' * 64}").empty?
  fail_check("divergent identity-platform tree negative fixture was accepted")
end
fail_check("clean integration fixture was rejected") unless integration_cleanliness_errors(status_output: "", status_success: true, current_root: "/fixture", integration_root: "/fixture").empty?
if integration_cleanliness_errors(status_output: " M pkg/identity/file.go\n", status_success: true).empty?
  fail_check("dirty integration worktree fixture was accepted")
end
if integration_cleanliness_errors(status_output: "", status_success: false).empty?
  fail_check("unreadable integration worktree status fixture was accepted")
end
if integration_cleanliness_errors(status_output: "", status_success: true, current_root: "/primary", integration_root: "/integration").empty?
  fail_check("clean integration validation from the wrong worktree was accepted")
end
if completion_mode_errors(execution_mode: true, fixture_mode: false, all_verified: true, clean_integration_mode: false).empty?
  fail_check("all-verified execution without clean integration mode was accepted")
end
unless completion_mode_errors(execution_mode: true, fixture_mode: false, all_verified: true, clean_integration_mode: true).empty?
  fail_check("all-verified clean integration fixture was rejected")
end
unless completion_mode_errors(execution_mode: true, fixture_mode: true, all_verified: true, clean_integration_mode: false).empty?
  fail_check("all-verified proposed-snapshot fixture incorrectly required clean integration mode")
end

def first_parent_commit_adding_line(line, path)
  output, _error, status = Open3.capture3(
    "git", "-C", REPOSITORY_ROOT, "log", "--first-parent", "--format=%H",
    "-S", line, "--", path
  )
  return unless status.success?

  output.lines.map(&:strip).find do |commit|
    diff, _diff_error, diff_status = Open3.capture3(
      "git", "-C", REPOSITORY_ROOT, "show", "--format=", "--unified=0", commit, "--", path
    )
    diff_status.success? && diff.lines.any? { |candidate| candidate.chomp == "+#{line}" }
  end
end

def first_parent_commit_with_blob(path, bytes)
  output, _error, status = Open3.capture3(
    "git", "-C", REPOSITORY_ROOT, "log", "--first-parent", "--reverse", "--format=%H", "--", path
  )
  return unless status.success?

  output.lines.map(&:strip).find { |commit| git_blob_bytes(commit, path) == bytes }
end

audit_authority_fixture_index = ARGV.index("--audit-retention-authority-fixture")
if audit_authority_fixture_index
  fixture_path = ARGV.delete_at(audit_authority_fixture_index + 1)
  ARGV.delete_at(audit_authority_fixture_index)
  fail_check("unknown arguments: #{ARGV.join(' ')}") unless ARGV.empty?
  unless fixture_path && File.readable?(fixture_path)
    fail_check("--audit-retention-authority-fixture requires a readable path")
  end
  validate_audit_retention_authority!(JSON.parse(File.read(fixture_path)))
  puts "identity-platform audit-retention authority validation: valid"
  exit 0
end

end_state_fixture_index = ARGV.index("--end-state-fixture")
if end_state_fixture_index
  fixture_path = ARGV.delete_at(end_state_fixture_index + 1)
  ARGV.delete_at(end_state_fixture_index)
  fail_check("unknown arguments: #{ARGV.join(' ')}") unless ARGV.empty?
  fail_check("--end-state-fixture requires a readable path") unless fixture_path && File.readable?(fixture_path)
  fixture = File.read(fixture_path)
  rows = validate_administration_journey!(fixture)
  phone_rows = validate_phone_recovery_journey!(fixture)
  puts "identity-platform end-state validation: #{rows.length + phone_rows.length} operations"
  exit 0
end

fixture_index = ARGV.index("--configuration-fixture")
if fixture_index
  fixture_path = ARGV.delete_at(fixture_index + 1)
  ARGV.delete_at(fixture_index)
  fail_check("unknown arguments: #{ARGV.join(' ')}") unless ARGV.empty?
  fail_check("--configuration-fixture requires a path") unless fixture_path && File.file?(fixture_path)
  rows = validate_configuration_rows!(File.read(fixture_path))
  puts "identity-platform configuration validation: #{rows.length} rows"
  exit 0
end

execution_fixture_path = take_path_option!(ARGV, "--execution-fixture")
execution_identity_fixture_path = take_path_option!(ARGV, "--execution-identity-fixture")
previous_execution_fixture_path = take_path_option!(ARGV, "--previous-execution-fixture")
inventory_fixture_path = take_path_option!(ARGV, "--inventory-fixture")
ledger_fixture_path = take_path_option!(ARGV, "--ledger-fixture")
previous_inventory_path = take_path_option!(ARGV, "--previous-inventory-fixture")
previous_ledger_path = take_path_option!(ARGV, "--previous-ledger-fixture")
goal_manifest_fixture_path = take_path_option!(ARGV, "--goal-manifest-fixture")
previous_goal_manifest_fixture_path = take_path_option!(ARGV, "--previous-goal-manifest-fixture")
upstream_leaves_fixture_path = take_path_option!(ARGV, "--upstream-leaves-fixture")
fail_check("previous transition fixtures must be supplied together") unless previous_inventory_path.nil? == previous_ledger_path.nil?
fail_check("previous execution fixture requires previous transition fixtures") if previous_execution_fixture_path && previous_inventory_path.nil?
fail_check("previous goal manifest fixture requires previous execution fixture") if previous_goal_manifest_fixture_path && !previous_execution_fixture_path
if previous_inventory_path && (!previous_execution_fixture_path || !previous_goal_manifest_fixture_path)
  fail_check("prior/current transition validation requires previous execution and goal manifest fixtures")
end
execution_mode = ARGV.delete("--execution") || execution_fixture_path
clean_integration_mode = ARGV.delete("--clean-integration")
fail_check("--clean-integration requires --execution or --execution-fixture") if clean_integration_mode && !execution_mode
fail_check("unknown arguments: #{ARGV.join(' ')}") unless ARGV.empty?
if execution_identity_fixture_path
  fixture_identity_rows = markdown_table(File.read(execution_identity_fixture_path), "Execution identity", "| Field | Value |").to_h
  fixture_identity_errors = execution_identity_errors(fixture_identity_rows)
  fail_check(fixture_identity_errors.join("; ")) unless fixture_identity_errors.empty?
  puts "identity-platform execution identity validation: valid"
  exit 0
end
validate_normative_markdown_notices!
end_state = File.read(File.join(ROOT, "END_STATE.md"))
end_state_journey_count_errors(end_state, 19).each { |error| fail_check(error) }
stale_end_state_count = end_state.sub("all nineteen journeys", "all eighteen journeys")
if end_state_journey_count_errors(stale_end_state_count, 19).empty?
  fail_check("stale end-state journey count negative fixture was accepted")
end
validate_administration_journey!(end_state)
validate_phone_recovery_journey!(end_state)
request_journey_row = end_state.lines.find do |line|
  line.strip.start_with?("| `identity.phone.password-reset-request` |")
end.to_s
missing_request_journey = end_state.sub(request_journey_row, "")
expect_phone_recovery_journey_fixture_rejection!(
  "missing request operation", "end-state phone recovery operations drifted"
) do
  phone_recovery_journey_errors(missing_request_journey)
end
drifted_request_success = end_state.sub(
  PHONE_RECOVERY_JOURNEY_TRANSITIONS.fetch("identity.phone.password-reset-request").first,
  "recovery request succeeds"
)
expect_phone_recovery_journey_fixture_rejection!(
  "request success transition",
  "end-state phone recovery success transition drifted for identity.phone.password-reset-request"
) do
  phone_recovery_journey_errors(drifted_request_success)
end
drifted_complete_rejection = end_state.sub(
  PHONE_RECOVERY_JOURNEY_TRANSITIONS.fetch("identity.phone.password-reset-complete")[1],
  "invalid evidence is retried"
)
expect_phone_recovery_journey_fixture_rejection!(
  "completion negative transition",
  "end-state phone recovery rejection transition drifted for identity.phone.password-reset-complete"
) do
  phone_recovery_journey_errors(drifted_complete_rejection)
end
%w[request complete].each do |suffix|
  operation_id = "identity.phone.password-reset-#{suffix}"
  canonical_seam = PHONE_RECOVERY_JOURNEY_TRANSITIONS.fetch(operation_id)[2]
  drifted_seam = end_state.sub(canonical_seam, "risk and phone are tested separately")
  expect_phone_recovery_journey_fixture_rejection!(
    "#{suffix} risk-phone seam",
    "end-state phone recovery seam transition drifted for #{operation_id}"
  ) do
    phone_recovery_journey_errors(drifted_seam)
  end
end
missing_journey_binding = end_state.sub(
  "risk-policy\n   version",
  "omitted binding"
)
expect_phone_recovery_journey_fixture_rejection!(
  "incomplete exact binding",
  "end-state phone recovery journey lacks composed proof: tenant, subject, recovery operation, recovery purpose, canonical number, pre-auth transaction, attempt ID and risk-policy version"
) do
  phone_recovery_journey_errors(missing_journey_binding)
end
missing_journey_session_closure = end_state.sub(
  "Both operations issue\n   no session and carry no remember choice",
  "operations may issue a session"
)
expect_phone_recovery_journey_fixture_rejection!(
  "session and remember choice",
  "end-state phone recovery journey lacks composed proof: Both operations issue no session and carry no remember choice"
) do
  phone_recovery_journey_errors(missing_journey_session_closure)
end
recovery_rows_for_validation = []
repair_rows_for_validation = []
worktree_resource_rows = []
task_owned_resource_rows = []
previous_task_owned_resource_rows = []
external_lanes = []
external_records = {}
worker_attestations = []
worker_runtime_attestations = []
acceptance_evidence_bindings = []
local_gate_evidence_bindings = []
goal_revision_rows = []
primitive_module_roots_by_consumer = Hash.new { |hash, key| hash[key] = Set.new }
resource_registry_header = "| Resource ID | Type | Owning unit/task | Exact path or safe external ID | State | Cleanup trigger | Last reconciled at | Cleanup evidence or attestation |"
parse_dependency_resource_snapshot = lambda do |body|
  markdown_table(body, "Task-owned resource registry", resource_registry_header).map do |resource_id, type, owner, target, state, _cleanup_trigger, _reconciled_at, cleanup_evidence|
    {id: plain_cell(resource_id), type: plain_cell(type), owner: plain_cell(owner), target: plain_cell(target),
     state: plain_cell(state), evidence: plain_cell(cleanup_evidence), clean: nil, head: nil}
  end
end
if execution_mode
  preflight_snapshot = File.read(execution_fixture_path || File.join(ROOT, "PREFLIGHT_EVIDENCE.md"))
  execution_sections = preflight_snapshot[/^## Execution identity\n.*?(?=^## Conflict-recovery baselines|\z)/m].to_s
  retains_pending = execution_sections.lines.any? do |line|
    line.start_with?("|") && line.split("|").map { |cell| plain_cell(cell.strip) }.include?("pending")
  end
  fail_check("execution preflight retains pending values") if retains_pending
end

inventory = File.read(inventory_fixture_path || File.join(ROOT, "INVENTORY.md"))
public_contracts_source = File.read(File.join(ROOT, "PUBLIC_CONTRACTS.json"))
public_contracts = JSON.parse(public_contracts_source)
public_contracts_helper = File.binread(File.join(ROOT, "public_contracts.rb"))
fail_check("PUBLIC_CONTRACTS.json is not canonical JSON") unless public_contracts_source == JSON.pretty_generate(public_contracts) + "\n"
public_contract_source_digest_errors(public_contracts, root: ROOT).each { |error| fail_check(error) }
public_contract_operation_closure_rule_errors(public_contracts).each { |error| fail_check(error) }
stale_operation_rule_fixture = JSON.parse(JSON.generate(public_contracts))
stale_operation_rule_fixture.fetch("validation_rules").map! do |rule|
  rule == PUBLIC_CONTRACT_OPERATION_CLOSURE_RULE ? rule.sub("every operation declared by API_OPERATIONS.md", "all 323 operations") : rule
end
if public_contract_operation_closure_rule_errors(stale_operation_rule_fixture).empty?
  fail_check("stale public-contract operation count negative fixture was accepted")
end
_contract_output, contract_error, contract_status = Open3.capture3(
  RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--check"
)
fail_check("public-contract helper check failed: #{contract_error.lines.first.to_s.strip}") unless contract_status.success?
Dir.mktmpdir("identity-public-contract-fragments-") do |directory|
  fixtures = {
    "duplicate-key.json" => [
      %({"units":[{"name":"first","\\u006eame":"second"}],"operations":[]}\n),
      "duplicate JSON object key \"name\""
    ],
    "noncanonical.json" => [
      %({"units":[],"operations":[]}\n),
      "is not canonical pretty JSON"
    ],
    "canonical-scalar.json" => [
      "1\n",
      "must be a JSON object"
    ],
    "canonical-empty-object.json" => [
      "{}\n",
      "has unexpected top-level keys"
    ]
  }
  fixtures.each do |name, (source, expected_error)|
    path = File.join(directory, name)
    File.binwrite(path, source)
    _output, error, status = Open3.capture3(
      RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", path
    )
    unless !status.success? && error.include?(expected_error)
      fail_check("public-contract #{name.delete_suffix('.json')} negative fixture was not rejected exactly")
    end
  end
  {
    "authn-bool-zero.json" => ["public_contracts_authn.json", "identity.mfa.otp-verify", "request", "TrustDevice"],
    "oauth-bool-zero.json" => ["public_contracts_oauth.json", nil, "Provider", "PKCERequired"]
  }.each do |name, (source_name, operation_id, owner_name, field_name)|
    fixture = JSON.parse(File.read(File.join(ROOT, "fragments", source_name)))
    field = if operation_id
              operation = fixture.fetch("operations").find { |row| row.fetch("id") == operation_id }
              operation.fetch(owner_name).fetch("fields").find { |row| row.fetch("name") == field_name }
            else
              owner = fixture.fetch("units").flat_map { |unit| unit.fetch("types") }.find { |row| row.fetch("name") == owner_name }
              owner.fetch("fields").find { |row| row.fetch("name") == field_name }
            end
    field["zero_value"] = "invalid or rejected when zero"
    path = File.join(directory, name)
    File.binwrite(path, JSON.pretty_generate(fixture) + "\n")
    _output, error, status = Open3.capture3(
      RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", path
    )
    expected_error = "documents false as valid but rejects its zero value"
    unless !status.success? && error.include?(expected_error)
      fail_check("public-contract #{name.delete_suffix('.json')} negative fixture was not rejected exactly")
    end
  end
  false_prose_bypass = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
  false_prose_field = false_prose_bypass.fetch("operations").find { |row| row.fetch("id") == "identity.mfa.otp-verify" }
    .fetch("request").fetch("fields").find { |row| row.fetch("name") == "TrustDevice" }
  false_prose_field["semantics"] = "False denotes the disabled state."
  false_prose_field["zero_value"] = "invalid when zero"
  false_prose_path = File.join(directory, "false-valid-prose-bypass.json")
  File.binwrite(false_prose_path, JSON.pretty_generate(false_prose_bypass) + "\n")
  _output, false_prose_error, false_prose_status = Open3.capture3(
    RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", false_prose_path
  )
  unless !false_prose_status.success? && false_prose_error.include?("documents false as valid but rejects its zero value")
    fail_check("public-contract false-valid prose bypass negative fixture was not rejected exactly")
  end
  nonboolean_false_valid = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_risk_delivery.json")))
  nonboolean_field = nonboolean_false_valid.fetch("units").flat_map { |unit| unit.fetch("types") }
    .flat_map { |type| type.fetch("fields", []) }.find { |field| field.fetch("type") != "bool" }
  nonboolean_field["false_valid"] = false
  nonboolean_path = File.join(directory, "nonboolean-false-valid.json")
  File.binwrite(nonboolean_path, JSON.pretty_generate(nonboolean_false_valid) + "\n")
  _output, nonboolean_error, nonboolean_status = Open3.capture3(
    RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", nonboolean_path
  )
  unless !nonboolean_status.success? && nonboolean_error.include?("non-boolean field #{nonboolean_field.fetch('name')} declares false-validity")
    fail_check("public-contract non-boolean false-valid negative fixture was not rejected exactly")
  end
  numeric_zero_fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
  numeric_zero_field = numeric_zero_fixture.fetch("units").flat_map { |unit| unit.fetch("types") }
    .flat_map { |type| type.fetch("fields", []) }
    .find { |field| field.fetch("type").match?(/\A(?:u?int(?:8|16|32|64)?|float(?:32|64)|time\.Duration)\z/) && field["zero_valid"] == true }
  fail_check("public-contract numeric zero-valid fixture source is missing") unless numeric_zero_field
  numeric_zero_field["zero_value"] = "0 is invalid"
  numeric_zero_path = File.join(directory, "numeric-zero-valid-contradiction.json")
  File.binwrite(numeric_zero_path, JSON.pretty_generate(numeric_zero_fixture) + "\n")
  _output, numeric_zero_error, numeric_zero_status = Open3.capture3(
    RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", numeric_zero_path
  )
  expected_numeric_zero_error = "numeric field #{numeric_zero_field.fetch('name')} zero_value must equal \"0 is valid\" when zero_valid=true"
  unless !numeric_zero_status.success? && numeric_zero_error.include?(expected_numeric_zero_error)
    fail_check("public-contract numeric zero-valid contradiction fixture was not rejected exactly")
  end
  numeric_zero_wording_fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
  numeric_zero_wording_field = numeric_zero_wording_fixture.fetch("units").flat_map { |unit| unit.fetch("types") }
    .flat_map { |type| type.fetch("fields", []) }
    .find { |field| field["zero_valid"] == true }
  numeric_zero_wording_field["zero_value"] = "zero is prohibited"
  numeric_zero_wording_path = File.join(directory, "numeric-zero-valid-alternate-wording.json")
  File.binwrite(numeric_zero_wording_path, JSON.pretty_generate(numeric_zero_wording_fixture) + "\n")
  _output, numeric_zero_wording_error, numeric_zero_wording_status = Open3.capture3(
    RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", numeric_zero_wording_path
  )
  unless !numeric_zero_wording_status.success? && numeric_zero_wording_error.include?(expected_numeric_zero_error)
    fail_check("public-contract numeric zero-valid alternate-wording fixture was not rejected exactly")
  end
  numeric_zero_inverse_fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
  numeric_zero_inverse_field = numeric_zero_inverse_fixture.fetch("units").flat_map { |unit| unit.fetch("types") }
    .flat_map { |type| type.fetch("fields", []) }
    .find { |field| field["zero_valid"] == false }
  fail_check("public-contract numeric zero-invalid fixture source is missing") unless numeric_zero_inverse_field
  numeric_zero_inverse_field["zero_value"] = "0 is valid"
  numeric_zero_inverse_path = File.join(directory, "numeric-zero-invalid-accepting-zero.json")
  File.binwrite(numeric_zero_inverse_path, JSON.pretty_generate(numeric_zero_inverse_fixture) + "\n")
  _output, numeric_zero_inverse_error, numeric_zero_inverse_status = Open3.capture3(
    RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", numeric_zero_inverse_path
  )
  expected_numeric_inverse_error = "numeric field #{numeric_zero_inverse_field.fetch('name')} zero_value must equal \"0 is invalid\" when zero_valid=false"
  unless !numeric_zero_inverse_status.success? && numeric_zero_inverse_error.include?(expected_numeric_inverse_error)
    fail_check("public-contract numeric zero-invalid inverse fixture was not rejected exactly")
  end
  nonnumeric_zero_valid = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_risk_delivery.json")))
  nonnumeric_zero_field = nonnumeric_zero_valid.fetch("units").flat_map { |unit| unit.fetch("types") }
    .flat_map { |type| type.fetch("fields", []) }
    .find { |field| !field.fetch("type").match?(/\A(?:u?int(?:8|16|32|64)?|float(?:32|64)|time\.Duration)\z/) }
  nonnumeric_zero_field["zero_valid"] = false
  nonnumeric_zero_path = File.join(directory, "nonnumeric-zero-valid.json")
  File.binwrite(nonnumeric_zero_path, JSON.pretty_generate(nonnumeric_zero_valid) + "\n")
  _output, nonnumeric_zero_error, nonnumeric_zero_status = Open3.capture3(
    RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", nonnumeric_zero_path
  )
  unless !nonnumeric_zero_status.success? && nonnumeric_zero_error.include?("non-numeric field #{nonnumeric_zero_field.fetch('name')} declares numeric zero-validity")
    fail_check("public-contract non-numeric zero-valid negative fixture was not rejected exactly")
  end
  {
    "nonboolean-required.json" => ["required", "yes", "required must be boolean"],
    "null-zero-value.json" => ["zero_value", nil, "zero_value must be a non-empty string"]
  }.each do |name, (key, value, expected_error)|
    fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_risk_delivery.json")))
    field = fixture.fetch("units").flat_map { |unit| unit.fetch("types") }
      .flat_map { |type| type.fetch("fields", []) }.first
    fail_check("public-contract structural field fixture source is missing") unless field
    field[key] = value
    path = File.join(directory, name)
    File.binwrite(path, JSON.pretty_generate(fixture) + "\n")
    _output, error, status = Open3.capture3(
      RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", path
    )
    unless !status.success? && error.include?(expected_error)
      fail_check("public-contract #{name.delete_suffix('.json')} negative fixture was not rejected exactly")
    end
  end
  closed_shape_mutations = {
    "metadata-extra-key.json" => lambda { |fixture| fixture.fetch("meta")["unexpected"] = true },
    "metadata-rule-extra-key.json" => lambda { |fixture| fixture.fetch("meta").fetch("validation_rules").first["unexpected"] = true },
    "unit-extra-key.json" => lambda { |fixture| fixture.fetch("units").first["unexpected"] = true },
    "type-extra-key.json" => lambda { |fixture| fixture.fetch("units").first.fetch("types").first["unexpected"] = true },
    "type-method-extra-key.json" => lambda do |fixture|
      fixture.fetch("units").first.fetch("types").first["methods"] = [{
        "name" => "Probe", "signature" => "Probe()", "semantics" => "Fixture-only exact method.", "unexpected" => true
      }]
    end,
    "field-extra-key.json" => lambda do |fixture|
      fixture.fetch("units").flat_map { |unit| unit.fetch("types") }.flat_map { |type| type.fetch("fields", []) }.first["unexpected"] = true
    end,
    "operation-extra-key.json" => lambda { |fixture| fixture.fetch("operations").first["unexpected"] = true }
  }
  closed_shape_mutations.each do |name, mutation|
    fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
    mutation.call(fixture)
    path = File.join(directory, name)
    File.binwrite(path, JSON.pretty_generate(fixture) + "\n")
    _output, error, status = Open3.capture3(
      RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", path
    )
    unless !status.success? && error.include?("has unexpected keys unexpected")
      fail_check("public-contract #{name.delete_suffix('.json')} negative fixture was not rejected exactly")
    end
  end
  {
    "backup-eligibility-invariant.json" => ["BackupEligible", "False is valid without any sibling invariant."],
    "backup-state-invariant.json" => ["BackupState", "False is valid and true is accepted without eligibility binding."]
  }.each do |name, (field_name, weakened_semantics)|
    fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
    verified_assertion = fixture.fetch("units").find { |unit| unit.fetch("unit") == "webauthn" }
      .fetch("types").find { |type| type.fetch("name") == "VerifiedAssertion" }
    verified_assertion.fetch("fields").find { |field| field.fetch("name") == field_name }["semantics"] = weakened_semantics
    path = File.join(directory, name)
    File.binwrite(path, JSON.pretty_generate(fixture) + "\n")
    _output, error, status = Open3.capture3(
      RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", path
    )
    expected_error = "backup flags omit the BE=false/BS=false validity and BS implies BE invariant"
    unless !status.success? && error.include?(expected_error)
      fail_check("public-contract #{name.delete_suffix('.json')} negative fixture was not rejected exactly")
    end
  end
  %w[BackupEligible BackupState].each do |field_name|
    fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
    credential = fixture.fetch("units").find { |unit| unit.fetch("unit") == "webauthn" }
      .fetch("types").find { |type| type.fetch("name") == "Credential" }
    credential.fetch("fields").delete_if { |field| field.fetch("name") == field_name }
    path = File.join(directory, "missing-#{field_name.downcase}.json")
    File.binwrite(path, JSON.pretty_generate(fixture) + "\n")
    _output, error, status = Open3.capture3(
      RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", path
    )
    expected_error = "backup flags omit the BE=false/BS=false validity and BS implies BE invariant"
    unless !status.success? && error.include?(expected_error)
      fail_check("public-contract missing #{field_name} negative fixture was not rejected exactly")
    end
  end
  passkey_state_fixture = JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_authn.json")))
  passkey_state = passkey_state_fixture.fetch("units").find { |unit| unit.fetch("unit") == "passkey" }
    .fetch("types").find { |type| type.fetch("name") == "BackupState" }
  passkey_state["constants"] = %w[Pending Committed Aborted Expired Revoked]
  passkey_state_path = File.join(directory, "passkey-transaction-backup-state.json")
  File.binwrite(passkey_state_path, JSON.pretty_generate(passkey_state_fixture) + "\n")
  _output, passkey_state_error, passkey_state_status = Open3.capture3(
    RbConfig.ruby, File.join(ROOT, "public_contracts.rb"), "--fragment-fixture", passkey_state_path
  )
  unless !passkey_state_status.success? && passkey_state_error.include?("passkey.BackupState does not represent the closed authenticator backup state")
    fail_check("public-contract passkey backup-state negative fixture was not rejected exactly")
  end
end
inventory_header = "| Unit | Canonical module | Requires verified | Status | Owner/blocker | Goal |"
fail_check("inventory table header drifted") unless inventory.lines.any? { |line| line.chomp == inventory_header }
rows = inventory.lines.filter_map do |line|
  next unless line.start_with?("| `")

  cells = line.split("|").map(&:strip)
  fail_check("inventory row has wrong column count: #{line.chomp}") unless cells.length == 8
  unit = cells[1]&.match(/\A`([^`]+)`\z/)&.[](1)
  next unless unit

  {
    unit: unit,
    module: cells[2][/`([^`]+)`/, 1],
    requires: cells[3].scan(/`([^`]+)`/).flatten,
    status: cells[4],
    owner: cells[5],
    goal: cells[6][/`([^`]+)`/, 1]
  }
end
fail_check("expected #{EXPECTED_SCHEDULABLE_UNITS} inventory rows, found #{rows.length}") unless rows.length == EXPECTED_SCHEDULABLE_UNITS
units = rows.map { |row| row[:unit] }
primitive_extension_rows = rows.select { |row| row[:unit].start_with?("primitive/") }
identity_rows = rows.reject { |row| row[:unit].start_with?("primitive/") }
identity_units = identity_rows.map { |row| row[:unit] }
fail_check("duplicate inventory unit") unless units.uniq.length == units.length
modules = rows.map { |row| row[:module] }
goals = rows.map { |row| row[:goal] }
fail_check("duplicate inventory canonical module") unless modules.uniq.length == modules.length
fail_check("duplicate inventory goal") unless goals.uniq.length == goals.length
%w[identity/delivery/postgres identity/anonymous/postgres sso/domain-verification].each do |unit|
  fail_check("required execution unit missing: #{unit}") unless units.include?(unit)
end
known = units.to_set

begin
  applicability_document = SharedContractApplicability.load_and_validate!(root: ROOT, units: units)
  unless SharedContractApplicability.canonical?(root: ROOT, units: units)
    fail_check("shared-contract applicability manifest is not canonical JSON")
  end
  missing_primitive_fixture = JSON.parse(JSON.generate(applicability_document))
  missing_primitive_unit = primitive_extension_rows.first.fetch(:unit)
  missing_primitive_fixture.fetch("units").delete(missing_primitive_unit)
  begin
    SharedContractApplicability.load_and_validate!(
      root: ROOT, units: units, document: missing_primitive_fixture
    )
    fail_check("missing primitive shared-contract applicability negative fixture was accepted")
  rescue ArgumentError => e
    unless e.message.include?("unit drift") && e.message.include?(missing_primitive_unit)
      fail_check("missing primitive shared-contract applicability negative fixture failed for the wrong reason")
    end
  end
  selected_primitive_fixture = JSON.parse(JSON.generate(applicability_document))
  selected_primitive_fixture.fetch("units").fetch(missing_primitive_unit)["transaction"] = ["tx.foundation"]
  begin
    SharedContractApplicability.load_and_validate!(
      root: ROOT, units: units, document: selected_primitive_fixture
    )
    fail_check("selected primitive shared-contract applicability negative fixture was accepted")
  rescue ArgumentError => e
    unless e.message.include?("primitive selectors must all be none")
      fail_check("selected primitive shared-contract applicability negative fixture failed for the wrong reason")
    end
  end
  %w[
    ref.frontchannel_post.cookie
    ref.oauth.rp.shared_redirect_issuer
    ref.oauth_server.client_class
    ref.saml.relay_state
    ref.saml.replay_set
    ref.struct:ref.frontchannel_post_cookie
    ref.struct:ref.oauth_server.client_class
    ref.struct:ref.oidc.logout_outcome
    ref.struct:ref.saml.replay_set
  ].each do |configuration_id|
    missing_configuration_owner = JSON.parse(JSON.generate(applicability_document))
    missing_configuration_owner.fetch("units").each_value do |entry|
      entry.fetch("configuration").delete(configuration_id)
    end
    begin
      SharedContractApplicability.load_and_validate!(
        root: ROOT, units: units, document: missing_configuration_owner
      )
      fail_check("unowned federation configuration negative fixture was accepted: #{configuration_id}")
    rescue ArgumentError => e
      expected = "configuration catalog rows have no applicable unit: #{configuration_id}"
      unless e.message.include?(expected)
        fail_check("unowned federation configuration negative fixture failed for the wrong reason: #{configuration_id}")
      end
    end
  end
  unexpected_configuration_owner = JSON.parse(JSON.generate(applicability_document))
  unexpected_configuration_owner.fetch("units").fetch("identity/session").fetch("configuration") << "ref.oauth_server.client_class"
  unexpected_configuration_owner.fetch("units").fetch("identity/session").fetch("configuration").sort!
  begin
    SharedContractApplicability.load_and_validate!(
      root: ROOT, units: units, document: unexpected_configuration_owner
    )
    fail_check("unexpected federation configuration owner negative fixture was accepted")
  rescue ArgumentError => e
    unless e.message.include?("configuration owner drift for ref.oauth_server.client_class")
      fail_check("unexpected federation configuration owner negative fixture failed for the wrong reason")
    end
  end
rescue ArgumentError => e
  fail_check(e.message)
end

rows.each do |row|
  fail_check("#{row[:unit]} has invalid status #{row[:status]}") unless ALLOWED_STATUSES.include?(row[:status])
  unknown = row[:requires].reject { |unit| known.include?(unit) }
  fail_check("#{row[:unit]} has unknown dependencies: #{unknown.join(', ')}") unless unknown.empty?
end

http_row = rows.find { |row| row[:unit] == "identity/http" }
reference_row = rows.find { |row| row[:unit] == "identity/reference" }
fail_check("identity/http feature dependency set drifted") unless http_row[:requires].to_set == HTTP_FEATURES
expected_reference = REFERENCE_ADAPTERS | Set[
  "identity/http", "sso/domain-verification", "primitive/authorization-identity-contracts",
  "primitive/capability-postgres-identity-contracts"
]
fail_check("identity/reference adapter dependency set drifted") unless reference_row[:requires].to_set == expected_reference
fail_check("identity/http imports a concrete reference adapter") unless (http_row[:requires].to_set & REFERENCE_ADAPTERS).empty?

depth = {}
visiting = Set.new
visit = lambda do |unit|
  return depth.fetch(unit) if depth.key?(unit)
  fail_check("dependency cycle at #{unit}") if visiting.include?(unit)

  visiting << unit
  row = rows.find { |candidate| candidate[:unit] == unit }
  value = row[:requires].empty? ? 0 : row[:requires].map { |dependency| visit.call(dependency) }.max + 1
  visiting.delete(unit)
  depth[unit] = value
end
units.each { |unit| visit.call(unit) }

rows.select { |row| row[:status] == "ready" }.each do |row|
  unmet = row[:requires].reject { |required| rows.find { |candidate| candidate[:unit] == required }[:status] == "verified" }
  fail_check("ready unit #{row[:unit]} has unverified prerequisites: #{unmet.join(', ')}") unless unmet.empty?
  fail_check("ready unit #{row[:unit]} has an owner/blocker") unless row[:owner] == "—"
end
rows.select { |row| ["in-progress", "verified"].include?(row[:status]) }.each do |row|
  unmet = row[:requires].reject { |required| rows.find { |candidate| candidate[:unit] == required }[:status] == "verified" }
  fail_check("#{row[:status]} unit #{row[:unit]} has unverified prerequisites: #{unmet.join(', ')}") unless unmet.empty?
end

seen_goals = Set.new
goal_manifest_path = goal_manifest_fixture_path || File.join(ROOT, "GOAL_MANIFEST.json")
fail_check("missing goal manifest") unless File.file?(goal_manifest_path)
goal_manifest_source = File.read(goal_manifest_path)
goal_manifest = JSON.parse(goal_manifest_source)
unless goal_manifest_fixture_path
  fail_check("goal manifest is not canonical JSON") unless goal_manifest_source == JSON.pretty_generate(goal_manifest) + "\n"
end
goal_path_bodies = {}
goal_manifest.fetch("goals").each do |entry|
  planning_path = entry.fetch("planning_path")
  canonical_path = entry.fetch("canonical_path")
  planning_absolute = File.join(ROOT, planning_path)
  canonical_absolute = File.join(REPOSITORY_ROOT, canonical_path)
  goal_path_bodies[planning_path] = File.binread(planning_absolute).force_encoding(Encoding::UTF_8) if File.file?(planning_absolute)
  goal_path_bodies[canonical_path] = File.binread(canonical_absolute).force_encoding(Encoding::UTF_8) if File.file?(canonical_absolute)
end
canonical_program_roots = goal_manifest.fetch("goals")
  .reject { |entry| entry.fetch("unit").start_with?("primitive/") }
  .map { |entry| entry.fetch("canonical_path").split("/")[0, 2].join("/") }.uniq
canonical_program_roots.each do |program_root|
  Dir[File.join(REPOSITORY_ROOT, program_root, "**", ".ai", "GOAL.md")].each do |path|
    relative = path.delete_prefix("#{REPOSITORY_ROOT}/")
    goal_path_bodies[relative] ||= File.binread(path).force_encoding(Encoding::UTF_8)
  end
end
Dir[File.join(ROOT, "goals", "*.md")].each do |path|
  relative = path.delete_prefix("#{ROOT}/")
  goal_path_bodies[relative] ||= File.binread(path).force_encoding(Encoding::UTF_8)
end
goal_resolution_errors, goal_bodies = goal_manifest_resolution(rows, goal_manifest, goal_path_bodies)
fail_check(goal_resolution_errors.join("; ")) unless goal_resolution_errors.empty?
consumed_primitives = Set.new
primitive_consumers = Hash.new { |hash, key| hash[key] = [] }
reverse = Hash.new { |hash, key| hash[key] = [] }
rows.each { |row| row[:requires].each { |dependency| reverse[dependency] << row[:unit] } }

modules_manifest = JSON.parse(File.read(File.join(REPOSITORY_ROOT, "modules.json")))
packages_manifest = JSON.parse(File.read(File.join(REPOSITORY_ROOT, "packages.json")))
repository_prefix = "#{modules_manifest.fetch('repository')}/pkg/"
registered_consumables = modules_manifest.fetch("modules").filter_map do |entry|
  directory = entry.fetch("directory")
  directory.delete_prefix("pkg/") if directory.start_with?("pkg/")
end.to_set
packages_manifest.fetch("packages").each do |entry|
  import_path = entry.fetch("import_path")
  registered_consumables << import_path.delete_prefix(repository_prefix) if import_path.start_with?(repository_prefix)
end
resolvable_consumables = registered_consumables

rows.each do |row|
  body = goal_bodies.fetch(row[:unit])
  actual_unit = body[/^- Unit: `([^`]+)`/, 1]
  actual_module = body[/^- Canonical module: `([^`]+)`/, 1]
  actual_goal = body[/^- Canonical goal after (?:scaffolding|scheduling): `([^`]+)`/, 1]
  fail_check("goal unit mismatch for #{row[:goal]}") unless actual_unit == row[:unit]
  fail_check("goal module mismatch for #{row[:unit]}") unless actual_module == row[:module]
  canonical_name = row[:unit].start_with?("primitive/") ? "GOAL_IDENTITY_CONTRACTS.md" : "GOAL.md"
  expected_goal = "#{row[:module]}/.ai/#{canonical_name}"
  fail_check("canonical goal mismatch for #{row[:unit]}") unless actual_goal == expected_goal
  requires_line = body[/^- Requires:.*$/].to_s
  if row[:unit] == "identity/http"
    fail_check("identity/http goal must delegate feature prerequisites to inventory") unless requires_line.include?("feature/protocol contract")
  elsif row[:unit] == "identity/reference"
    fail_check("identity/reference goal must delegate adapter prerequisites to inventory") unless requires_line.include?("concrete adapter")
  else
    actual_requires = metadata_values(body, "Requires")
    fail_check("requires mismatch for #{row[:unit]}: expected #{row[:requires]}, got #{actual_requires}") unless actual_requires == row[:requires]
  end
  unless row[:unit].start_with?("primitive/")
    consumes = metadata_values(body, "Consumes existing primitives")
    fail_check("#{row[:unit]} goal lacks consumed primitives") if consumes.empty?
    fail_check("#{row[:unit]} goal has duplicate consumed primitives") unless consumes.uniq == consumes
    planned_consumes = consumes.select { |name| known.include?(name) }
    unless planned_consumes.empty?
      fail_check("#{row[:unit]} consumes planned inventory units instead of declaring Requires: #{planned_consumes.join(', ')}")
    end
    unresolved_consumes = consumes.reject { |name| resolvable_consumables.include?(name) }
    fail_check("#{row[:unit]} consumes unregistered primitives: #{unresolved_consumes.join(', ')}") unless unresolved_consumes.empty?
    consumed_primitives.merge(consumes)
    consumes.each { |primitive| primitive_consumers[primitive] << row[:unit] }
  end
  fail_check("#{row[:unit]} retains ambiguous delegation language") if body.include?("where not delegated")
  fail_check("#{row[:unit]} goal lacks common-requirements start gate") unless body.include?("COMMON_REQUIREMENTS.md")
  fail_check("#{row[:unit]} goal has move-unsafe relative program references") if body.match?(%r{`\.\./(?:COMMON_REQUIREMENTS|INVENTORY)\.md`})
  fail_check("#{row[:unit]} goal retains worker-owned ready transition") if body.match?(/marks `#{Regexp.escape(row[:unit])}` as `ready`/)
  start_section = body[/^## (?:Start gate(?: and objective)?|Start gate and objective).*?(?=^## |\z)/m].to_s
  named_start_units = start_section.scan(/`([^`]+)`/).flatten.select { |name| known.include?(name) && name != row[:unit] }
  unexpected_start_units = named_start_units - row[:requires]
  fail_check("#{row[:unit]} start gate names non-prerequisites: #{unexpected_start_units.join(', ')}") unless unexpected_start_units.empty?
  expected_unlocks = reverse[row[:unit]]
  expected_unlocks = expected_unlocks.sort_by(&:b) if row[:unit].start_with?("primitive/")
  unlock_line = body[/^- Unlocks after verification:.*$/]
  actual_unlocks = unlock_line.to_s.scan(/`([^`]+)`/).flatten
  fail_check("unlock mismatch for #{row[:unit]}: expected #{expected_unlocks}, got #{actual_unlocks}") unless actual_unlocks == expected_unlocks
  seen_goals << row[:goal]
  normative_count = body.scan(/\b(?:MUST|MUST NOT|REQUIRED|SHALL|SHALL NOT)\b/).length
  fail_check("#{row[:unit]} goal is too thin: #{normative_count} normative requirements") if normative_count < 15
end
fail_check("expected #{EXPECTED_SCHEDULABLE_UNITS} resolved inventory goals, found #{seen_goals.length}") unless seen_goals.length == EXPECTED_SCHEDULABLE_UNITS
primitive_extension_inventory_errors(
  public_contracts: public_contracts, rows: rows, goal_bodies: goal_bodies
).each { |error| fail_check(error) }
unit_order_fixture = JSON.parse(JSON.generate(public_contracts))
unit_order_fixture.fetch("units")[0], unit_order_fixture.fetch("units")[1] =
  unit_order_fixture.fetch("units")[1], unit_order_fixture.fetch("units")[0]
unless primitive_extension_inventory_errors(
  public_contracts: unit_order_fixture, rows: rows, goal_bodies: goal_bodies
).include?("identity inventory/public-contract unit order drifted")
  fail_check("public-contract unit-order negative fixture was accepted")
end
public_contract_goal_binding_errors(public_contracts, rows, goal_bodies).each { |error| fail_check(error) }

primitive_fixture_rows = Marshal.load(Marshal.dump(rows))
primitive_fixture_rows.reject! { |row| row[:unit] == primitive_extension_rows.first[:unit] }
if primitive_extension_inventory_errors(public_contracts: public_contracts, rows: primitive_fixture_rows, goal_bodies: goal_bodies).empty?
  fail_check("primitive-extension row deletion negative fixture was accepted")
end
consumer_fixture_rows = Marshal.load(Marshal.dump(rows))
extension_requirement_fixture = public_contracts.dig("manifest_schema", "required_primitive_extensions").first
consumer_fixture_unit = extension_requirement_fixture.fetch("consumers").first
consumer_fixture_rows.find { |row| row[:unit] == consumer_fixture_unit }[:requires].delete(extension_requirement_fixture.fetch("extension_unit"))
if primitive_extension_inventory_errors(public_contracts: public_contracts, rows: consumer_fixture_rows, goal_bodies: goal_bodies).empty?
  fail_check("primitive-extension consumer prerequisite negative fixture was accepted")
end
consumer_catalog_fixture = JSON.parse(JSON.generate(public_contracts))
consumer_catalog_fixture.dig("manifest_schema", "required_primitive_extensions").first.fetch("consumers").pop
if primitive_extension_inventory_errors(public_contracts: consumer_catalog_fixture, rows: rows, goal_bodies: goal_bodies).empty?
  fail_check("primitive-extension derived consumer negative fixture was accepted")
end
digest_fixture_contracts = JSON.parse(JSON.generate(public_contracts))
digest_fixture_contracts.dig("manifest_schema", "required_primitive_extensions").first["required_contract_sha256"] = "sha256:#{'0' * 64}"
if primitive_extension_inventory_errors(public_contracts: digest_fixture_contracts, rows: rows, goal_bodies: goal_bodies).empty?
  fail_check("primitive-extension digest negative fixture was accepted")
end
identity_leak_fixture = JSON.parse(JSON.generate(public_contracts))
identity_leak_fixture.fetch("units") << {"unit" => primitive_extension_rows.first[:unit]}
if primitive_extension_inventory_errors(public_contracts: identity_leak_fixture, rows: rows, goal_bodies: goal_bodies).empty?
  fail_check("primitive-extension identity-unit leak negative fixture was accepted")
end
contract_id_fixture = JSON.parse(JSON.generate(public_contracts))
contract_id_fixture.fetch("units").first["contract_id"] = "contract:unit:forged:v1"
if public_contract_goal_binding_errors(contract_id_fixture, rows, goal_bodies).empty?
  fail_check("public-contract unit ID negative fixture was accepted")
end
operation_contract_id_fixture = JSON.parse(JSON.generate(public_contracts))
operation_contract_id_fixture.fetch("operations").first["contract_id"] = "contract:operation:forged:v1"
if public_contract_goal_binding_errors(operation_contract_id_fixture, rows, goal_bodies).empty?
  fail_check("public-contract operation ID negative fixture was accepted")
end
operation_interface_fixture = JSON.parse(JSON.generate(public_contracts))
operation_interface_fixture.fetch("operations").first.fetch("method")["interface"] = "go:forged:MissingService"
if public_contract_goal_binding_errors(operation_interface_fixture, rows, goal_bodies).empty?
  fail_check("public-contract operation interface negative fixture was accepted")
end
operation_method_fixture = JSON.parse(JSON.generate(public_contracts))
operation_method_fixture.fetch("operations").first.fetch("method")["signature"] = "Forged() error"
if public_contract_goal_binding_errors(operation_method_fixture, rows, goal_bodies).empty?
  fail_check("public-contract operation method negative fixture was accepted")
end
goal_contract_fixture_bodies = goal_bodies.dup
goal_contract_fixture_unit = identity_rows.first[:unit]
goal_contract_fixture_bodies[goal_contract_fixture_unit] = goal_contract_fixture_bodies.fetch(goal_contract_fixture_unit).sub(
  "contract:unit:#{goal_contract_fixture_unit}:v1", "contract:unit:forged:v1"
)
if public_contract_goal_binding_errors(public_contracts, rows, goal_contract_fixture_bodies).empty?
  fail_check("goal public-contract ID negative fixture was accepted")
end
external_goal_contract_fixture_bodies = goal_bodies.dup
external_goal_contract_unit = "primitive/authorization-identity-contracts"
external_goal_contract_fixture_bodies[external_goal_contract_unit] = external_goal_contract_fixture_bodies.fetch(external_goal_contract_unit).sub(
  "contract:operation:identity.admin.permission-check:v1", "contract:operation:forged:v1"
)
if public_contract_goal_binding_errors(public_contracts, rows, external_goal_contract_fixture_bodies).empty?
  fail_check("external-owner goal public-contract ID negative fixture was accepted")
end
source_digest_fixture = JSON.parse(JSON.generate(public_contracts))
source_digest_fixture.fetch("source_digests")["public_contracts.rb"] = "sha256:#{'0' * 64}"
if public_contract_source_digest_errors(source_digest_fixture, root: ROOT).empty?
  fail_check("public-contract helper digest negative fixture was accepted")
end

dependencies = File.read(File.join(ROOT, "DEPENDENCIES.md"))
aliases = {}
dependencies.each_line do |line|
  if (match = line.match(/^\s+(\w+)\[([^\]]+)\]\s*$/))
    aliases[match[1]] = match[2]
  elsif (match = line.match(/^\s+(\w+) --> (\w+)\[([^\]]+)\]\s*$/))
    aliases[match[2]] = match[3]
  end
end
mermaid_edges = dependencies.lines.filter_map do |line|
  match = line.match(/^\s+(\w+) --> (\w+)(?:\[[^\]]+\])?\s*$/)
  next unless match
  [aliases.fetch(match[1], match[1].tr("_", "/")), aliases.fetch(match[2], match[2].tr("_", "/"))]
end.to_set
inventory_edges = rows.flat_map { |row| row[:requires].map { |dependency| [dependency, row[:unit]] } }.to_set
missing_edges = inventory_edges - mermaid_edges
extra_edges = mermaid_edges - inventory_edges
fail_check("Mermaid mismatch; missing=#{missing_edges.to_a} extra=#{extra_edges.to_a}") unless missing_edges.empty? && extra_edges.empty?

readme = File.read(File.join(ROOT, "README.md"))
program = File.read(File.join(ROOT, "PROGRAM.md"))
program_journey_count_errors(program, 19).each { |error| fail_check(error) }
stale_program_count = program.sub("closes all 19 journeys", "closes the 18 journeys")
if program_journey_count_errors(stale_program_count, 19).empty?
  fail_check("stale program journey count negative fixture was accepted")
end
artifacts = {}
COORDINATOR_ARTIFACTS.each do |artifact|
  artifact_path = File.join(ROOT, artifact)
  fail_check("missing coordinator artifact: #{artifact}") unless File.file?(artifact_path)
  artifacts[artifact] = File.read(artifact_path)
  fail_check("program completion contract omits #{artifact}") unless program.include?("`#{artifact}`")
  headings = artifacts[artifact].scan(/^## (.+)$/).flatten
  missing_sections = REQUIRED_ARTIFACT_SECTIONS.fetch(artifact) - headings
  fail_check("#{artifact} missing required sections: #{missing_sections.join(', ')}") unless missing_sections.empty?
end

COORDINATOR_ARTIFACTS.grep(/\.json\z/).each do |artifact|
  document = JSON.parse(artifacts.fetch(artifact))
  fail_check("#{artifact} is not canonical JSON") unless artifacts.fetch(artifact) == JSON.pretty_generate(document) + "\n"
end

semantic_owners = identity_units.to_set | EXISTING_OWNERS | Set["audit"]
end_state_acceptance = JSON.parse(artifacts.fetch("END_STATE_ACCEPTANCE.json"))
fail_check("end-state acceptance authority drifted") unless end_state_acceptance.fetch("authority") == "END_STATE.md"
expected_acceptance_keys = %w[schema_version authority digest_algorithm digest_input evidence_record_contract artifact_observation_contract input_identity_contract journeys cross_cutting artifact_catalog]
fail_check("end-state acceptance top-level schema drifted") unless end_state_acceptance.keys == expected_acceptance_keys
evidence_contract = end_state_acceptance.fetch("evidence_record_contract")
expected_payload_fields = %w[schema_version artifact_id result tested_revision gate_execution_revision revalidation_revision input_manifest input_root tool_environment observations artifact_hashes recorded_at]
fail_check("end-state evidence payload contract drifted") unless evidence_contract.fetch("required_payload_fields") == expected_payload_fields
observation_contract = end_state_acceptance.fetch("artifact_observation_contract")
expected_observation_fields = %w[observation_id claim_id contract_reference scenario preconditions stimulus expected_outcome actual_outcome result artifact_sha256]
fail_check("end-state observation payload contract drifted") unless observation_contract.fetch("required_observation_fields") == expected_observation_fields
input_identity_contract = end_state_acceptance.fetch("input_identity_contract")
expected_manifest_fields = %w[path_or_environment_id kind content_identity owner reason]
fail_check("end-state input-manifest schema drifted") unless input_identity_contract.fetch("manifest_entry_fields") == expected_manifest_fields
fail_check("end-state input-manifest class closure is incomplete") unless input_identity_contract.fetch("required_input_classes").length == 5
fail_check("end-state non-behavioral provenance exclusions drifted") unless input_identity_contract.fetch("non_behavioral_provenance_exclusions") == NON_BEHAVIORAL_IDENTITY_PLATFORM_INPUTS
unless input_identity_contract.fetch("provenance_rule").include?("DEPENDENCIES.md remains a behavior input")
  fail_check("end-state input provenance rule omits dependency invalidation")
end
fail_check("end-state journey closure drifted") unless end_state_acceptance.fetch("journeys").map { |row| row.fetch("number") } == (1..19).to_a
end_state_acceptance_identity_errors(end_state_acceptance).each { |error| fail_check(error) }
acceptance_digest_resolver = ->(row) { end_state_semantic_digest(row, end_state) }
semantic_manifest_errors(end_state_acceptance, kind: "end-state acceptance", collections: %w[journeys cross_cutting], known_owners: semantic_owners, digest_resolver: acceptance_digest_resolver, schema_version: 2).each { |error| fail_check(error) }
acceptance_rows = end_state_acceptance.fetch("journeys") + end_state_acceptance.fetch("cross_cutting")
referenced_acceptance_artifacts = acceptance_rows.flat_map { |row| row.fetch("artifacts") }
artifact_catalog = end_state_acceptance.fetch("artifact_catalog")
artifact_ids = artifact_catalog.map { |row| row.fetch("id") }
fail_check("end-state artifact catalog is not sorted and unique") unless artifact_ids == artifact_ids.sort_by(&:b).uniq
fail_check("end-state artifact catalog closure drifted") unless artifact_ids.to_set == referenced_acceptance_artifacts.to_set
fail_check("end-state acceptance artifact references are duplicated") unless referenced_acceptance_artifacts.uniq == referenced_acceptance_artifacts
required_acceptance_evidence_errors(end_state_acceptance).each { |error| fail_check(error) }
acceptance_checker_output, acceptance_checker_status = Open3.capture2e(
  "ruby", File.join(ROOT, "acceptance", "check.rb"), chdir: REPOSITORY_ROOT
)
fail_check("artifact-specific acceptance checker failed: #{acceptance_checker_output.strip}") unless acceptance_checker_status.success?
artifact_paths = artifact_catalog.map { |row| row.fetch("path") }
artifact_schemas = artifact_catalog.map { |row| row.fetch("schema") }
fail_check("end-state artifact paths are not unique") unless artifact_paths.uniq == artifact_paths
fail_check("end-state artifact schemas are not unique") unless artifact_schemas.uniq == artifact_schemas
artifact_catalog.each do |artifact|
  id = artifact.fetch("id")
  source_rows = acceptance_rows.select { |row| row.fetch("artifacts").include?(id) }
  fail_check("end-state artifact #{id} producer drifted") unless source_rows.map { |row| row.fetch("owner") }.uniq == [artifact.fetch("producer_unit")]
  producer_row = rows.find { |row| row[:unit] == artifact.fetch("producer_unit") }
  fail_check("end-state artifact #{id} producer is not a registered unit") unless producer_row
  fail_check("end-state artifact #{id} producer goal drifted") unless artifact.fetch("producer_goal") == producer_row[:goal] && goal_bodies.key?(producer_row[:unit])
  fail_check("end-state artifact #{id} path is not canonical") unless artifact.fetch("path").match?(%r{\A\.ai/identity-platform/evidence/[a-z0-9-]+\.json\z})
  fail_check("end-state artifact #{id} schema is not versioned") unless artifact.fetch("schema").match?(/\.v1\z/)
  fail_check("end-state artifact #{id} claim closure drifted") unless artifact.fetch("claims") == source_rows.map { |row| row.fetch("id") }.sort_by(&:b)
  expected_observation_ids = (artifact.fetch("claims") + artifact.fetch("operation_claims", [])).sort_by(&:b).map do |claim|
    "#{id}.#{claim}.behavior"
  end
  fail_check("end-state artifact #{id} observation-ID closure drifted") unless artifact.fetch("observation_ids") == expected_observation_ids
  fail_check("end-state artifact #{id} gate/producer module drifted") unless artifact.fetch("gate") == "make check MODULES=#{producer_row[:module]}"
  if id == "audit-retention-plan-confirm-report"
    expected_operations = %w[identity.audit-retention.deletion.plan identity.audit-retention.deletion.confirm]
    fail_check("audit-retention acceptance artifact omits plan/confirm operations") unless artifact.fetch("operation_claims") == expected_operations
  elsif id == "phone-reset-risk-evidence-report"
    expected_operations = %w[
      identity.phone.password-reset-request identity.phone.password-reset-complete identity.risk.evaluate
    ]
    fail_check("phone-reset acceptance artifact operation closure drifted") unless artifact.fetch("operation_claims") == expected_operations
  end
end
deleted_journey = JSON.parse(JSON.generate(end_state_acceptance))
deleted_journey.fetch("journeys").pop
deleted_errors = semantic_manifest_errors(deleted_journey, kind: "end-state acceptance", collections: %w[journeys cross_cutting], known_owners: semantic_owners, digest_resolver: acceptance_digest_resolver, schema_version: 2)
deleted_errors.concat(end_state_acceptance_identity_errors(deleted_journey))
deleted_errors.concat(required_acceptance_evidence_errors(deleted_journey))
fail_check("end-state journey deletion mutation was accepted") if deleted_errors.empty?
REQUIRED_ACCEPTANCE_OPERATION_EVIDENCE.each do |artifact_id, required_operations|
  required_operations.each do |operation_id|
    missing_operation = JSON.parse(JSON.generate(end_state_acceptance))
    artifact = missing_operation.fetch("artifact_catalog").find { |row| row.fetch("id") == artifact_id }
    artifact.fetch("operation_claims").delete(operation_id)
    fail_check("#{artifact_id} missing #{operation_id} evidence mutation was accepted") if required_acceptance_evidence_errors(missing_operation).empty?
  end
end
drifted_journey = JSON.parse(JSON.generate(end_state_acceptance))
drifted_journey.fetch("journeys").first.fetch("artifacts")[0] = "weakened-artifact"
fail_check("end-state journey semantic mutation was accepted") if semantic_manifest_errors(drifted_journey, kind: "end-state acceptance", collections: %w[journeys cross_cutting], known_owners: semantic_owners, digest_resolver: acceptance_digest_resolver, schema_version: 2).empty?
byte_drifted_end_state = end_state.sub("1. **Identity lifecycle:**", "1.  **Identity lifecycle:**")
byte_drifted_resolver = ->(row) { end_state_semantic_digest(row, byte_drifted_end_state) }
if semantic_manifest_errors(end_state_acceptance, kind: "end-state acceptance", collections: %w[journeys cross_cutting], known_owners: semantic_owners, digest_resolver: byte_drifted_resolver, schema_version: 2).empty?
  fail_check("end-state authoritative byte mutation was accepted")
end
swapped_acceptance_identity = JSON.parse(JSON.generate(end_state_acceptance))
first_identity = swapped_acceptance_identity.fetch("journeys")[0].fetch("id")
second_identity = swapped_acceptance_identity.fetch("journeys")[1].fetch("id")
swapped_acceptance_identity.fetch("journeys")[0]["id"] = second_identity
swapped_acceptance_identity.fetch("journeys")[1]["id"] = first_identity
swapped_acceptance_identity.fetch("journeys").first(2).each do |row|
  row["semantic_digest"] = end_state_semantic_digest(row, end_state)
end
swapped_identity_errors = semantic_manifest_errors(
  swapped_acceptance_identity, kind: "end-state acceptance", collections: %w[journeys cross_cutting],
  known_owners: semantic_owners, digest_resolver: acceptance_digest_resolver, schema_version: 2
)
swapped_identity_errors.concat(end_state_acceptance_identity_errors(swapped_acceptance_identity))
fail_check("self-authenticated end-state semantic identity mutation was accepted") if swapped_identity_errors.empty?

parity_dispositions = JSON.parse(artifacts.fetch("PARITY_DISPOSITIONS.json"))
fail_check("parity disposition authority drifted") unless parity_dispositions.fetch("authority") == "BETTER_AUTH_PARITY.md"
parity_artifact_documents = artifacts.merge(
  "BETTER_AUTH_PARITY.md" => File.read(File.join(ROOT, "BETTER_AUTH_PARITY.md"))
)
parity_digest_resolver = ->(row) { authoritative_artifact_semantic_digest(row, parity_artifact_documents) }
semantic_manifest_errors(parity_dispositions, kind: "parity dispositions", collections: %w[dispositions ownership_reclassifications], known_owners: semantic_owners, digest_resolver: parity_digest_resolver).each { |error| fail_check(error) }
expected_parity_keys = %w[schema_version authority digest_algorithm digest_input dispositions ownership_reclassifications provider_native_token_modes captcha_owners]
fail_check("parity disposition schema drifted") unless parity_dispositions.keys == expected_parity_keys
expected_disposition_ids = %w[
  exclusion.billing.v1 exclusion.siwe.v1 exclusion.mcp-authentication.v1 exclusion.agent-authentication.v1
  divergence.additional-databases.v1 divergence.javascript-tooling.v1 divergence.community-plugin-catalog.v1
  divergence.personal-scim.v1 divergence.provider-token-cookies.v1 divergence.backup-code-review.v1
  exclusion.lead-tracking-analytics.v1 exclusion.javascript-framework-clients.v1
]
fail_check("parity disposition closure drifted") unless parity_dispositions.fetch("dispositions").map { |row| row.fetch("id") } == expected_disposition_ids
required_exclusions = %w[exclusion.billing.v1 exclusion.siwe.v1 exclusion.mcp-authentication.v1 exclusion.agent-authentication.v1]
fail_check("parity exact exclusion closure drifted") unless required_exclusions.all? { |id| parity_dispositions.fetch("dispositions").any? { |row| row.fetch("id") == id && row.fetch("kind") == "excluded" } }
configuration_catalog_document = JSON.parse(artifacts.fetch("CONFIGURATION_CATALOGS.json"))
parity_closure_errors(parity_dispositions, configuration_catalog_document).each { |error| fail_check(error) }
{
  "closed_default" => lambda { |fixture| fixture.fetch("provider_native_token_modes")["closed_default"] = ["forged"] },
  "required_count" => lambda { |fixture| fixture.fetch("captcha_owners")["required_count"] += 1 },
  "missing closure field" => lambda { |fixture| fixture.fetch("provider_native_token_modes").delete("closed_default") }
}.each do |label, mutate|
  fixture = JSON.parse(JSON.generate(parity_dispositions))
  mutate.call(fixture)
  fail_check("parity #{label} negative fixture was accepted") if parity_closure_errors(fixture, configuration_catalog_document).empty?
end
expected_reclassifications = [
  ["reclassification.remote-signing.v1", configuration_catalog_document.dig("jwt_profile_ownership", "remote_signing")],
  ["reclassification.hosted-jwks.v1", configuration_catalog_document.dig("jwt_profile_ownership", "hosted_jwks")]
]
expected_reclassifications.each do |id, expected|
  row = parity_dispositions.fetch("ownership_reclassifications").find { |candidate| candidate.fetch("id") == id }
  fail_check("parity JWT ownership reclassification drifted: #{id}") unless row && row.values_at("classification", "interface", "owner") == expected.values_at("classification", "interface", "owner")
end
drifted_disposition = JSON.parse(JSON.generate(parity_dispositions))
drifted_disposition.fetch("dispositions").first["owner"] = "identity/http"
fail_check("parity semantic mutation was accepted") if semantic_manifest_errors(drifted_disposition, kind: "parity dispositions", collections: %w[dispositions ownership_reclassifications], known_owners: semantic_owners, digest_resolver: parity_digest_resolver).empty?
meaning_insensitive_disposition = JSON.parse(JSON.generate(parity_dispositions))
meaning_insensitive_row = meaning_insensitive_disposition.fetch("dispositions").first
meaning_insensitive_row["artifact"] = "CONFIGURATION_CATALOGS.json"
meaning_insensitive_row["semantic_digest"] = semantic_row_digest(meaning_insensitive_row)
if semantic_manifest_errors(meaning_insensitive_disposition, kind: "parity dispositions", collections: %w[dispositions ownership_reclassifications], known_owners: semantic_owners, digest_resolver: parity_digest_resolver).empty?
  fail_check("meaning-insensitive parity semantic digest fixture was accepted")
end

verification = JSON.parse(artifacts.fetch("VERIFICATION_APPLICABILITY.json"))
verification_selectors = %w[race fuzz hostile leak benchmark infrastructure provider_interoperability]
fail_check("verification applicability schema drifted") unless verification.keys == %w[schema_version authority selectors units] && verification.fetch("schema_version") == 1
fail_check("verification applicability authority drifted") unless verification.fetch("authority") == "REFERENCE_CONFIGURATION.md#struct:ref.verification.applicability"
fail_check("verification applicability selectors drifted") unless verification.fetch("selectors") == verification_selectors
verification_rows = verification.fetch("units")
fail_check("verification applicability unit closure drifted") unless verification_rows.map { |row| row.fetch("unit") } == units
username_verification = verification_rows.find { |row| row.fetch("unit") == "identity/username" }.fetch("selectors")
username_verification_errors(username_verification, goal_body: goal_bodies.fetch("identity/username")).each { |error| fail_check(error) }
username_downgrade = JSON.parse(JSON.generate(username_verification))
username_downgrade["fuzz"] = {"status" => "not_applicable", "reviewed_reason" => "identity/username fixture incorrectly waives required property evidence"}
if username_verification_errors(username_downgrade).empty?
  fail_check("identity/username verification downgrade negative fixture was accepted")
end
verification_rows.each do |row|
  unit = row.fetch("unit")
  selectors = row.fetch("selectors")
  fail_check("#{unit} verification selector closure drifted") unless selectors.keys == verification_selectors
  selectors.each do |selector, value|
    fail_check("#{unit} #{selector} selector schema drifted") unless value.is_a?(Hash) && %w[required not_applicable].include?(value["status"])
    if value["status"] == "required"
      fail_check("#{unit} #{selector} required selector has extra fields") unless value.keys == ["status"]
    else
      reason = value["reviewed_reason"]
      fail_check("#{unit} #{selector} lacks a specific reviewed reason") unless value.keys == %w[status reviewed_reason] && reason.is_a?(String) && reason.length >= 24 && reason.include?(unit)
    end
  end
  goal_body = goal_bodies.fetch(unit)
  goal_verification_selector_errors(unit: unit, selectors: selectors, goal_body: goal_body).each { |error| fail_check(error) }
  inline = goal_body[/Verification applicability is exact for this unit:.*?provider_interoperability=(required|not_applicable)/m]
  next unless inline
  inline_values = inline.scan(/`?(race|fuzz|hostile|leak|benchmark|infrastructure|provider_interoperability)=(required|not_applicable)`?/).to_h
  manifest_values = selectors.transform_values { |value| value.fetch("status") }
  fail_check("#{unit} inline verification applicability contradicts canonical manifest") unless inline_values == manifest_values
end
goal_selector_fixture_unit = "identity/identitytest"
goal_selector_fixture = JSON.parse(JSON.generate(
  verification_rows.find { |row| row.fetch("unit") == goal_selector_fixture_unit }.fetch("selectors")
))
goal_selector_fixture["benchmark"] = {
  "status" => "not_applicable",
  "reviewed_reason" => "#{goal_selector_fixture_unit} fixture incorrectly waives its explicit benchmark gate"
}
if goal_verification_selector_errors(
  unit: goal_selector_fixture_unit, selectors: goal_selector_fixture,
  goal_body: goal_bodies.fetch(goal_selector_fixture_unit)
).empty?
  fail_check("goal-required verification selector downgrade negative fixture was accepted")
end
weakened_verification = JSON.parse(JSON.generate(verification))
weakened_verification.fetch("units").first.fetch("selectors").delete("race")
fail_check("verification applicability selector deletion mutation was accepted") if weakened_verification.fetch("units").all? { |row| row.fetch("selectors").keys == verification_selectors }

semantic_root_inputs = {
  goal_manifest: goal_manifest, acceptance: end_state_acceptance,
  acceptance_catalog: artifacts.fetch("ACCEPTANCE_ARTIFACTS.json"),
  acceptance_model: File.binread(File.join(ROOT, "acceptance/model.rb")),
  acceptance_check: File.binread(File.join(ROOT, "acceptance/check.rb")),
  acceptance_schema_validation: File.binread(File.join(ROOT, "acceptance/schema_validation.rb")),
  acceptance_schemas: Dir[File.join(ROOT, "acceptance/v1/schemas/*.json")].sort.to_h do |path|
    [path.delete_prefix("#{ROOT}/"), File.binread(path)]
  end,
  operations: JSON.parse(artifacts.fetch("OPERATION_SEMANTICS.json")),
  public_contracts: public_contracts_source,
  public_contracts_helper: public_contracts_helper,
  shared_applicability: applicability_document,
  shared_applicability_helper: File.binread(File.join(ROOT, "shared_contract_applicability.rb")),
  parity: parity_dispositions, verification: verification,
  configuration: configuration_catalog_document,
  protocol: JSON.parse(artifacts.fetch("PROTOCOL_CONFORMANCE_MANIFEST.json"))
}
semantic_root = program_semantic_root(**semantic_root_inputs)
fail_check("program semantic root drifted: #{semantic_root}") unless semantic_root == EXPECTED_PROGRAM_SEMANTIC_ROOT
if program_semantic_root(**semantic_root_inputs.merge(public_contracts: public_contracts_source.sub("\"schema_version\"", "\"schema_version_drift\""))) == EXPECTED_PROGRAM_SEMANTIC_ROOT
  fail_check("public-contract aggregate mutation bypassed program semantic root")
end
if program_semantic_root(**semantic_root_inputs.merge(public_contracts_helper: public_contracts_helper + "\n")) == EXPECTED_PROGRAM_SEMANTIC_ROOT
  fail_check("public-contract helper mutation bypassed program semantic root")
end
acceptance_schema_path, acceptance_schema_source = semantic_root_inputs.fetch(:acceptance_schemas).first
acceptance_contract_mutation = semantic_root_inputs.merge(
  acceptance_catalog: semantic_root_inputs.fetch(:acceptance_catalog) + "\n",
  acceptance_model: semantic_root_inputs.fetch(:acceptance_model) + "\n",
  acceptance_check: semantic_root_inputs.fetch(:acceptance_check) + "\n",
  acceptance_schema_validation: semantic_root_inputs.fetch(:acceptance_schema_validation) + "\n",
  acceptance_schemas: semantic_root_inputs.fetch(:acceptance_schemas).merge(acceptance_schema_path => acceptance_schema_source + "\n")
)
if program_semantic_root(**acceptance_contract_mutation) == EXPECTED_PROGRAM_SEMANTIC_ROOT
  fail_check("paired acceptance contract mutation bypassed program semantic root")
end
shared_applicability_mutation = JSON.parse(JSON.generate(applicability_document))
shared_applicability_mutation.fetch("units").fetch("identity/i18n").fetch("configuration").delete("ref.i18n.default")
shared_applicability_mutation.fetch("units").fetch("identity/http").fetch("configuration") << "ref.i18n.default"
shared_applicability_mutation.fetch("units").fetch("identity/http").fetch("configuration").sort!
if program_semantic_root(**semantic_root_inputs.merge(shared_applicability: shared_applicability_mutation)) == EXPECTED_PROGRAM_SEMANTIC_ROOT
  fail_check("shared-contract applicability ownership mutation bypassed program semantic root")
end
if program_semantic_root(**semantic_root_inputs.merge(shared_applicability_helper: semantic_root_inputs.fetch(:shared_applicability_helper) + "\n")) == EXPECTED_PROGRAM_SEMANTIC_ROOT
  fail_check("shared-contract applicability helper mutation bypassed program semantic root")
end
paired_acceptance_mutation = JSON.parse(JSON.generate(end_state_acceptance))
paired_row = paired_acceptance_mutation.fetch("journeys").first
paired_row.fetch("artifacts")[0] = "paired-weakened-artifact"
paired_row["semantic_digest"] = semantic_row_digest(paired_row)
fail_check("paired acceptance payload/digest mutation bypassed program semantic root") if program_semantic_root(**semantic_root_inputs.merge(acceptance: paired_acceptance_mutation)) == EXPECTED_PROGRAM_SEMANTIC_ROOT
paired_section_mutation = JSON.parse(JSON.generate(end_state_acceptance))
mutated_end_state = end_state.sub("create, retrieve, update", "create, retrieve")
paired_section_row = paired_section_mutation.fetch("journeys").first
paired_section_row["semantic_digest"] = end_state_semantic_digest(paired_section_row, mutated_end_state)
fail_check("paired END_STATE section/digest mutation bypassed program semantic root") if program_semantic_root(**semantic_root_inputs.merge(acceptance: paired_section_mutation)) == EXPECTED_PROGRAM_SEMANTIC_ROOT
verification_downgrade = JSON.parse(JSON.generate(verification))
downgraded = verification_downgrade.fetch("units").find { |row| row.fetch("selectors").fetch("provider_interoperability").fetch("status") == "required" }
downgraded.fetch("selectors")["provider_interoperability"] = {"status" => "not_applicable", "reviewed_reason" => "#{downgraded.fetch('unit')} paired downgrade fixture has no provider boundary"}
fail_check("required verification downgrade bypassed program semantic root") if program_semantic_root(**semantic_root_inputs.merge(verification: verification_downgrade)) == EXPECTED_PROGRAM_SEMANTIC_ROOT
authorization_mutation = JSON.parse(artifacts.fetch("OPERATION_SEMANTICS.json"))
authorization_mutation.fetch("operations").find { |row| row.fetch("access") != "public" }["authorization"] = "none"
fail_check("operation authorization mutation bypassed program semantic root") if program_semantic_root(**semantic_root_inputs.merge(operations: authorization_mutation)) == EXPECTED_PROGRAM_SEMANTIC_ROOT
protocol_pin_mutation = JSON.parse(JSON.generate(semantic_root_inputs.fetch(:protocol)))
protocol_pin_mutation.fetch("clause_pins").first["locator"] = "paired weakened locator"
fail_check("protocol pin mutation bypassed program semantic root") if program_semantic_root(**semantic_root_inputs.merge(protocol: protocol_pin_mutation)) == EXPECTED_PROGRAM_SEMANTIC_ROOT

fixture_declaration = artifact_catalog.first
fixture_revision = git_output("rev-parse", "HEAD")
fixture_environment_inputs = REQUIRED_ENVIRONMENT_INPUT_IDS.map do |identity|
  {"path_or_environment_id" => identity, "kind" => "environment", "content_identity" => "sha256:#{'e' * 64}", "owner" => "coordinator", "reason" => "fixture environment identity"}
end
fixture_manifest = (tracked_behavior_input_manifest(fixture_revision, modules) + fixture_environment_inputs).sort_by { |entry| entry.fetch("path_or_environment_id").b }
fixture_input = behavior_input_fingerprint(fixture_manifest)
fixture_acceptance_contract = IdentityPlatformAcceptance.catalog_document.fetch("artifacts").find { |entry| entry.fetch("artifact_id") == fixture_declaration.fetch("id") }
fixture_schema = IdentityPlatformAcceptance.schema_document(fixture_acceptance_contract)
fixture_artifact_payload = AcceptanceSchemaValidation.sample(fixture_schema.dig("$defs", "artifact_evidence"))
fixture_schema.dig("$defs", "artifact_evidence", "x-semantic-rules").each do |rule|
  case rule.fetch("kind")
  when "positive" then rule.fetch("fields").each { |field| fixture_artifact_payload[field] = 1 }
  when "zero" then rule.fetch("fields").each { |field| fixture_artifact_payload[field] = 0 }
  when "true" then rule.fetch("fields").each { |field| fixture_artifact_payload[field] = true }
  when "equal" then fixture_artifact_payload[rule.fetch("right")] = fixture_artifact_payload.fetch(rule.fetch("left"))
  when "const" then fixture_artifact_payload[rule.fetch("field")] = rule.fetch("value")
  end
end
fixture_execution = fixture_artifact_payload.fetch("execution")
fixture_execution.merge!(
  "tested_revision" => fixture_revision, "input_root" => fixture_input,
  "tool" => "fixture-tool@1", "environment" => "fixture-environment",
  "started_at" => "2026-08-11T00:00:00Z", "completed_at" => "2026-08-11T00:00:01Z",
  "stdout" => "affected acceptance gate completed with attributable evidence\n", "stderr" => ""
)
AcceptanceSchemaValidation.bind_execution_proof!(fixture_execution)
fixture_receipt_bytes = AcceptanceSchemaValidation.execution_receipt_bytes(fixture_execution)
fixture_receipt_path = fixture_execution.fetch("execution_receipt_path")
fixture_receipt_digest = "sha256:#{Digest::SHA256.hexdigest(fixture_receipt_bytes)}"
fixture_artifact_bytes = JSON.pretty_generate(fixture_artifact_payload) + "\n"
fixture_artifact_path = fixture_acceptance_contract.fetch("artifact_evidence_output_path")
fixture_artifact_digest = "sha256:#{Digest::SHA256.hexdigest(fixture_artifact_bytes)}"
fixture_artifact_payloads = {fixture_artifact_path => fixture_artifact_bytes, fixture_receipt_path => fixture_receipt_bytes}
fixture_evidence = {
  "schema_version" => 2, "artifact_id" => fixture_declaration.fetch("id"),
  "result" => {"status" => "pass", "gate" => fixture_declaration.fetch("gate")},
  "tested_revision" => fixture_revision, "gate_execution_revision" => fixture_revision,
  "revalidation_revision" => nil,
  "input_manifest" => fixture_manifest, "input_root" => fixture_input,
  "tool_environment" => {"tool" => "fixture-tool@1", "environment" => "fixture-environment"},
  "observations" => fixture_acceptance_contract.fetch("required_observations").map do |observation|
    observation.merge("artifact_sha256" => fixture_artifact_digest)
  end,
  "artifact_hashes" => [
    {"path" => fixture_artifact_path, "sha256" => fixture_artifact_digest},
    {"path" => fixture_receipt_path, "sha256" => fixture_receipt_digest}
  ],
  "recorded_at" => "2026-08-11T00:00:00Z"
}
fixture_acceptance_errors = lambda do |evidence, payloads = fixture_artifact_payloads|
  acceptance_evidence_errors(
    evidence, declaration: fixture_declaration, revision: fixture_revision,
    input_fingerprint: fixture_input, artifact_payloads: payloads
  )
end
fail_check("valid acceptance evidence fixture was rejected") unless fixture_acceptance_errors.call(fixture_evidence).empty?
acceptance_reuse_fixture = JSON.parse(JSON.generate(fixture_evidence))
acceptance_reuse_fixture["gate_execution_revision"] = "1" * 40
acceptance_reuse_fixture["revalidation_revision"] = "1" * 40
fail_check("valid acceptance fingerprint-reuse fixture was rejected") unless fixture_acceptance_errors.call(acceptance_reuse_fixture).empty?
%w[result tested_revision revalidation_revision input_manifest input_root observations artifact_hashes].each do |field|
  mutation = JSON.parse(JSON.generate(fixture_evidence))
  mutation[field] = %w[input_manifest observations artifact_hashes].include?(field) ? [] : "invalid"
  fail_check("acceptance evidence #{field} mutation was accepted") if fixture_acceptance_errors.call(mutation).empty?
end
acceptance_reuse_without_marker = JSON.parse(JSON.generate(fixture_evidence))
acceptance_reuse_without_marker["gate_execution_revision"] = "1" * 40
fail_check("acceptance reuse without revalidation revision was accepted") if fixture_acceptance_errors.call(acceptance_reuse_without_marker).empty?
acceptance_marker_without_reuse = JSON.parse(JSON.generate(fixture_evidence))
acceptance_marker_without_reuse["revalidation_revision"] = fixture_revision
fail_check("acceptance revalidation revision without reuse was accepted") if fixture_acceptance_errors.call(acceptance_marker_without_reuse).empty?
forged_claim_only = JSON.parse(JSON.generate(fixture_evidence)); forged_claim_only["artifact_hashes"] = []
fail_check("forged claim-only acceptance evidence was accepted") if fixture_acceptance_errors.call(forged_claim_only).empty?
{
  "foreign observation" => lambda { |evidence| evidence["observations"][0]["observation_id"] = "other-artifact.foreign.behavior" },
  "foreign claim" => lambda { |evidence| evidence["observations"][0]["claim_id"] = "identity.foreign.operation" },
  "foreign artifact" => lambda { |evidence| evidence["artifact_id"] = "foreign-artifact" },
  "missing expected outcome" => lambda { |evidence| evidence["observations"][0].delete("expected_outcome") },
  "missing actual outcome" => lambda { |evidence| evidence["observations"][0].delete("actual_outcome") },
  "non-pass observation" => lambda { |evidence| evidence["observations"][0]["result"] = "failed" },
  "unbound observation artifact" => lambda { |evidence| evidence["observations"][0]["artifact_sha256"] = "sha256:#{'f' * 64}" }
}.each do |label, mutate|
  mutation = JSON.parse(JSON.generate(fixture_evidence))
  mutate.call(mutation)
  if fixture_acceptance_errors.call(mutation).empty?
    fail_check("acceptance evidence #{label} mutation was accepted")
  end
end
invalid_artifact_payload = JSON.parse(JSON.generate(fixture_artifact_payload))
invalid_artifact_payload.delete(fixture_acceptance_contract.fetch("artifact_evidence_fields").first)
invalid_artifact_bytes = JSON.pretty_generate(invalid_artifact_payload) + "\n"
invalid_artifact_digest = "sha256:#{Digest::SHA256.hexdigest(invalid_artifact_bytes)}"
invalid_artifact_record = JSON.parse(JSON.generate(fixture_evidence))
invalid_artifact_record["artifact_hashes"][0]["sha256"] = invalid_artifact_digest
invalid_artifact_record["observations"].each { |observation| observation["artifact_sha256"] = invalid_artifact_digest }
if fixture_acceptance_errors.call(invalid_artifact_record, fixture_artifact_payloads.merge(fixture_artifact_path => invalid_artifact_bytes)).empty?
  fail_check("acceptance evidence invalid artifact payload schema mutation was accepted")
end
wrong_artifact_path_record = JSON.parse(JSON.generate(fixture_evidence))
wrong_artifact_path_record["artifact_hashes"][0]["path"] = ".ai/identity-platform/evidence/artifacts/wrong-artifact.json"
if fixture_acceptance_errors.call(wrong_artifact_path_record).empty?
  fail_check("acceptance evidence wrong artifact payload path mutation was accepted")
end
if fixture_acceptance_errors.call(fixture_evidence, {}).empty?
  fail_check("acceptance evidence absent artifact payload bytes mutation was accepted")
end
if fixture_acceptance_errors.call(fixture_evidence, {fixture_artifact_path => fixture_artifact_bytes}).empty?
  fail_check("acceptance evidence absent execution receipt bytes mutation was accepted")
end
digest_mismatch_bytes = fixture_artifact_bytes + "\n"
if fixture_acceptance_errors.call(fixture_evidence, fixture_artifact_payloads.merge(fixture_artifact_path => digest_mismatch_bytes)).empty?
  fail_check("acceptance evidence artifact digest-versus-bytes mutation was accepted")
end
malformed_artifact_bytes = "{\n"
malformed_artifact_digest = "sha256:#{Digest::SHA256.hexdigest(malformed_artifact_bytes)}"
malformed_artifact_record = JSON.parse(JSON.generate(fixture_evidence))
malformed_artifact_record["artifact_hashes"][0]["sha256"] = malformed_artifact_digest
malformed_artifact_record["observations"].each { |observation| observation["artifact_sha256"] = malformed_artifact_digest }
if fixture_acceptance_errors.call(malformed_artifact_record, fixture_artifact_payloads.merge(fixture_artifact_path => malformed_artifact_bytes)).empty?
  fail_check("acceptance evidence malformed artifact JSON mutation was accepted")
end
forged_pass_payload = JSON.parse(JSON.generate(fixture_artifact_payload))
forged_pass_payload["execution"]["exit_status"] = 1
AcceptanceSchemaValidation.bind_execution_proof!(forged_pass_payload.fetch("execution"))
forged_pass_bytes = JSON.pretty_generate(forged_pass_payload) + "\n"
forged_pass_digest = "sha256:#{Digest::SHA256.hexdigest(forged_pass_bytes)}"
forged_pass_record = JSON.parse(JSON.generate(fixture_evidence))
forged_pass_record["artifact_hashes"][0]["sha256"] = forged_pass_digest
forged_pass_record["observations"].each { |observation| observation["artifact_sha256"] = forged_pass_digest }
if fixture_acceptance_errors.call(forged_pass_record, fixture_artifact_payloads.merge(fixture_artifact_path => forged_pass_bytes)).empty?
  fail_check("acceptance evidence accepted self-declared pass after failed command")
end
coherent_forged_payload = JSON.parse(JSON.generate(fixture_artifact_payload))
coherent_forged_payload["execution"]["stdout"] = "FABRICATED: command was never executed\n"
AcceptanceSchemaValidation.bind_execution_proof!(coherent_forged_payload.fetch("execution"))
coherent_forged_bytes = JSON.pretty_generate(coherent_forged_payload) + "\n"
coherent_forged_digest = "sha256:#{Digest::SHA256.hexdigest(coherent_forged_bytes)}"
coherent_forged_record = JSON.parse(JSON.generate(fixture_evidence))
coherent_forged_record["artifact_hashes"].find { |row| row["path"] == fixture_artifact_path }["sha256"] = coherent_forged_digest
coherent_forged_record["observations"].each { |observation| observation["artifact_sha256"] = coherent_forged_digest }
coherent_forged_receipt = AcceptanceSchemaValidation.execution_receipt_bytes(coherent_forged_payload.fetch("execution"))
coherent_forged_receipt_digest = "sha256:#{Digest::SHA256.hexdigest(coherent_forged_receipt)}"
coherent_forged_record["artifact_hashes"].find { |row| row["path"] == fixture_receipt_path }["sha256"] = coherent_forged_receipt_digest
coherent_forged_payloads = fixture_artifact_payloads.merge(
  fixture_artifact_path => coherent_forged_bytes,
  fixture_receipt_path => coherent_forged_receipt
)
unless fixture_acceptance_errors.call(coherent_forged_record, coherent_forged_payloads).empty?
  fail_check("coherently recomputed synthetic provenance fixture is malformed")
end
if acceptance_evidence_errors(
  coherent_forged_record, declaration: fixture_declaration, revision: fixture_revision,
  input_fingerprint: fixture_input, artifact_payloads: coherent_forged_payloads, final_execution: true
).empty?
  fail_check("acceptance evidence accepted coherently recomputed synthetic execution without live runner capture")
end
zero_work_payload = JSON.parse(JSON.generate(fixture_artifact_payload))
progress_rule = fixture_schema.dig("$defs", "artifact_evidence", "x-semantic-rules").find { |rule| rule.fetch("kind") == "positive" } ||
  fixture_schema.dig("$defs", "artifact_evidence", "x-semantic-rules").find { |rule| rule.fetch("kind") == "true" }
fail_check("acceptance evidence fixture lacks non-zero semantic work rule") unless progress_rule
progress_rule.fetch("fields").each { |field| zero_work_payload[field] = progress_rule.fetch("kind") == "positive" ? 0 : false }
zero_work_bytes = JSON.pretty_generate(zero_work_payload) + "\n"
zero_work_digest = "sha256:#{Digest::SHA256.hexdigest(zero_work_bytes)}"
zero_work_record = JSON.parse(JSON.generate(fixture_evidence))
zero_work_record["artifact_hashes"][0]["sha256"] = zero_work_digest
zero_work_record["observations"].each { |observation| observation["artifact_sha256"] = zero_work_digest }
if fixture_acceptance_errors.call(zero_work_record, fixture_artifact_payloads.merge(fixture_artifact_path => zero_work_bytes)).empty?
  fail_check("acceptance evidence accepted zero-work artifact payload")
end
working_only_acceptance = JSON.parse(JSON.generate(fixture_evidence))
fail_check("working-tree-only acceptance artifact was accepted") if acceptance_evidence_errors(working_only_acceptance, declaration: fixture_declaration, revision: fixture_revision, input_fingerprint: fixture_input, repository_root: REPOSITORY_ROOT).empty?

gate_fixture_revision = git_output("rev-parse", "HEAD")
gate_fixture_artifact = ".ai/identity-platform/COMMON_REQUIREMENTS.md"
gate_fixture_manifest = (tracked_behavior_input_manifest(gate_fixture_revision, ["pkg/identity"]) + fixture_environment_inputs).sort_by { |entry| entry.fetch("path_or_environment_id").b }
gate_fixture_root = behavior_input_fingerprint(gate_fixture_manifest)
excluded_gate_inputs = gate_fixture_manifest.select do |entry|
  NON_BEHAVIORAL_IDENTITY_PLATFORM_INPUTS.any? do |excluded|
    path = entry.fetch("path_or_environment_id")
    excluded.end_with?("/") ? path.start_with?(excluded) : path == excluded
  end
end
fail_check("local gate manifest retained non-behavioral bookkeeping") unless excluded_gate_inputs.empty?
unless gate_fixture_manifest.any? { |entry| entry.fetch("path_or_environment_id") == ".ai/identity-platform/ORCHESTRATOR_GOAL.md" }
  fail_check("local gate manifest omitted a normative identity-platform input")
end
weakened_gate_manifest = gate_fixture_manifest.reject { |entry| entry.fetch("path_or_environment_id") == ".ai/identity-platform/ORCHESTRATOR_GOAL.md" }
if behavior_input_manifest_errors(weakened_gate_manifest, revision: gate_fixture_revision, module_roots: ["pkg/identity"], repository: REPOSITORY_ROOT).empty?
  fail_check("local gate manifest accepted a missing normative input")
end
gate_fixture = {
  "schema_version" => 2, "schema" => "identity-platform.local-gate.v2", "unit" => "identity",
  "tested_revision" => gate_fixture_revision, "gate_execution_revision" => gate_fixture_revision,
  "revalidation_revision" => nil, "input_manifest" => gate_fixture_manifest, "input_root" => gate_fixture_root,
  "evidence_record" => {"path" => ".ai/identity-platform/evidence/gates/identity.json"},
  "outcome" => "pass", "commands" => ["make check MODULES=pkg/identity"],
  "artifacts" => [{"path" => gate_fixture_artifact, "sha256" => "sha256:#{Digest::SHA256.file(File.join(REPOSITORY_ROOT, gate_fixture_artifact)).hexdigest}"}],
  "tool_identity" => "go1.fixture", "environment_identity" => "fixture-environment", "record_digest" => nil
}
gate_fixture["record_digest"] = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(gate_fixture.reject { |key, _| key == 'record_digest' })))}"
refresh_gate_fixture_digest = lambda do |record|
  record["record_digest"] = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(record.reject { |key, _| key == 'record_digest' })))}"
end
gate_fixture_args = {unit: "identity", revision: gate_fixture_revision, fingerprint: gate_fixture_root, module_roots: ["pkg/identity"], repository_root: REPOSITORY_ROOT}
fail_check("valid local gate evidence fixture was rejected") unless local_gate_evidence_errors(gate_fixture, **gate_fixture_args).empty?
%w[outcome tested_revision gate_execution_revision revalidation_revision input_manifest input_root evidence_record record_digest].each do |field|
  mutation = JSON.parse(JSON.generate(gate_fixture)); mutation[field] = "invalid"
  refresh_gate_fixture_digest.call(mutation) unless field == "record_digest"
  fail_check("local gate evidence #{field} mutation was accepted") if local_gate_evidence_errors(mutation, **gate_fixture_args).empty?
end
gate_reuse_fixture = JSON.parse(JSON.generate(gate_fixture))
gate_reuse_fixture["gate_execution_revision"] = "1" * 40
gate_reuse_fixture["revalidation_revision"] = "1" * 40
refresh_gate_fixture_digest.call(gate_reuse_fixture)
fail_check("valid local gate fingerprint-reuse fixture was rejected") unless local_gate_evidence_errors(gate_reuse_fixture, **gate_fixture_args.merge(revision: "1" * 40, repository_root: nil)).empty?
gate_reuse_without_marker = JSON.parse(JSON.generate(gate_fixture))
gate_reuse_without_marker["gate_execution_revision"] = "1" * 40
refresh_gate_fixture_digest.call(gate_reuse_without_marker)
fail_check("local gate reuse without revalidation revision was accepted") if local_gate_evidence_errors(gate_reuse_without_marker, **gate_fixture_args.merge(revision: "1" * 40, repository_root: nil)).empty?
[
  [],
  [{"path" => ".ai/identity-platform/fixtures/missing-artifact.txt", "sha256" => "sha256:#{'4' * 64}"}],
  [{"path" => ".ai/identity-platform/../identity-platform/COMMON_REQUIREMENTS.md", "sha256" => "sha256:#{'4' * 64}"}],
  [{"path" => gate_fixture_artifact, "sha256" => "sha256:#{'4' * 64}"}]
].each do |artifacts_mutation|
  mutation = JSON.parse(JSON.generate(gate_fixture)); mutation["artifacts"] = artifacts_mutation
  mutation["record_digest"] = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical_json_value(mutation.reject { |key, _| key == 'record_digest' })))}"
  fail_check("local gate artifact mutation was accepted") if local_gate_evidence_errors(mutation, **gate_fixture_args).empty?
end
fail_check("working-tree-only local gate record was accepted") if local_gate_evidence_errors(gate_fixture, **gate_fixture_args, record_path: ".ai/identity-platform/fixtures/fake-execution-identity.md").empty?
forged_ledger_entry = {unit: "identity", generation: "1", gate_revision: "0" * 40}
forged_binding = {
  unit: "identity", generation: "1", gate_revision: "0" * 40,
  path: ".ai/identity-platform/evidence/gates/identity.json", commit: "1" * 40,
  digest: "sha256:#{'2' * 64}", bound_at: "2026-08-11T00:00:00Z"
}
if local_gate_binding_errors(forged_binding, ledger_entry: forged_ledger_entry, repository_root: REPOSITORY_ROOT).empty?
  fail_check("hash-only verified ledger evidence negative fixture was accepted")
end
[
  [{"status" => "authorized"}, {"status" => "completed"}],
  [{"status" => "authorized"}, {"status" => "superseded"}, {"status" => "authorized"}],
  [{"status" => "authorized"}, {"status" => "superseded"}, {"status" => "authorized"}, {"status" => "completed"}],
  [{"status" => "authorized"}, {"status" => "completed"}, {"status" => "authorized"}, {"status" => "completed"}]
].each { |fixture| fail_check("valid recovery lifecycle fixture was rejected") unless recovery_lifecycle_errors(fixture).empty? }
[[{"status" => "completed"}], [{"status" => "authorized"}, {"status" => "superseded"}, {"status" => "completed"}]].each do |mutation|
  fail_check("invalid recovery lifecycle fixture was accepted") if recovery_lifecycle_errors(mutation).empty?
end
refreshed_recovery_fixture = [
  {"status" => "authorized", "identity" => %w[unit 1 integration-a checkpoint-a]},
  {"status" => "superseded", "identity" => %w[unit 1 integration-a checkpoint-a]},
  {"status" => "authorized", "identity" => %w[unit 1 integration-b checkpoint-b]},
  {"status" => "completed", "identity" => %w[unit 1 integration-b checkpoint-b]}
]
fail_check("refreshed recovery epoch fixture was rejected") unless recovery_epoch_identity_errors(refreshed_recovery_fixture).empty?
drifted_recovery_fixture = JSON.parse(JSON.generate(refreshed_recovery_fixture))
drifted_recovery_fixture.last["identity"] = %w[unit 1 integration-a checkpoint-a]
fail_check("recovery terminal from a prior epoch was accepted") if recovery_epoch_identity_errors(drifted_recovery_fixture).empty?
recovery_authorized_row = ["identity", "1", "a" * 40, "b" * 40, ".ai/evidence/recovery.json", "authorized", "2026-08-11T00:00:00Z", "recovery:identity:g1:e1"]
recovery_completed_row = recovery_authorized_row.dup.tap { |row| row[5] = "completed"; row[6] = "2026-08-11T00:00:01Z" }
if recovery_transition_errors([], [recovery_authorized_row, recovery_completed_row]).empty?
  fail_check("same-snapshot recovery authorization and terminal negative fixture was accepted")
end
unless recovery_transition_errors([recovery_authorized_row], [recovery_authorized_row, recovery_completed_row]).empty?
  fail_check("preceded recovery terminal fixture was rejected")
end
goal_revision_authorized = {
  revision_id: "goal:identity-username:g1", unit: "identity/username",
  previous_digest: "sha256:#{'1' * 64}", current_digest: "sha256:#{'2' * 64}",
  status: "authorized", authorized_by: "coordinator"
}
goal_revision_applied = goal_revision_authorized.merge(status: "applied")
fail_check("valid goal revision lifecycle fixture was rejected") unless goal_revision_lifecycle_errors([goal_revision_authorized, goal_revision_applied]).empty?
if goal_revision_lifecycle_errors([goal_revision_applied]).empty?
  fail_check("goal revision terminal without authorization negative fixture was accepted")
end
previous_goal_fixture = {"goals" => [{"unit" => "identity/username", "sha256" => "1" * 64}]}
current_goal_fixture = {"goals" => [{"unit" => "identity/username", "sha256" => "2" * 64}]}
if goal_digest_change_errors(previous_goal_fixture, current_goal_fixture, [], [goal_revision_applied]).empty?
  fail_check("unauthorized goal digest change negative fixture was accepted")
end
goal_revision_superseded = goal_revision_authorized.merge(status: "superseded")
if goal_digest_change_errors(previous_goal_fixture, previous_goal_fixture, [], [goal_revision_authorized, goal_revision_superseded]).empty?
  fail_check("same-snapshot goal authorization and superseded terminal negative fixture was accepted")
end
unless goal_digest_change_errors(
  previous_goal_fixture, previous_goal_fixture,
  [goal_revision_authorized], [goal_revision_authorized, goal_revision_superseded]
).empty?
  fail_check("preceded goal superseded terminal fixture was rejected")
end
frontier_fixture = [{unit: "a", status: "verified", requires: []}, {unit: "b", status: "proposed", requires: ["a"]}]
frontier_eligible = eligible_frontier_rows(frontier_fixture)
fail_check("eligible-frontier mutation fixture was accepted") if frontier_eligible.empty?
unless eligible_frontier_rows(frontier_fixture, Set["b"]).empty?
  fail_check("primitive-blocked frontier fixture was promoted")
end

completion_mode_errors(
  execution_mode: execution_mode, fixture_mode: !execution_fixture_path.nil?,
  all_verified: rows.all? { |row| row[:status] == "verified" },
  clean_integration_mode: clean_integration_mode
).each { |error| fail_check(error) }

if execution_mode && rows.all? { |row| row[:status] == "verified" }
  final_validation_revision = git_output("rev-parse", "HEAD")
  acceptance_execution_revisions = Set.new
  expected_binding_ids = artifact_catalog.map { |declaration| declaration.fetch("id") }
  fail_check("acceptance evidence binding closure drifted") unless acceptance_evidence_bindings.map { |binding| binding[:artifact_id] } == expected_binding_ids
  artifact_catalog.each do |declaration|
    binding = acceptance_evidence_bindings.find { |candidate| candidate[:artifact_id] == declaration.fetch("id") }
    fail_check("acceptance evidence binding path drifted for #{declaration.fetch('id')}") unless binding && binding[:path] == declaration.fetch("path")
    path = File.join(REPOSITORY_ROOT, declaration.fetch("path"))
    fail_check("completed program lacks acceptance evidence #{declaration.fetch('id')}") unless File.file?(path)
    source = File.read(path)
    document = JSON.parse(source)
    fail_check("acceptance evidence #{declaration.fetch('id')} is not canonical JSON") unless source == JSON.pretty_generate(document) + "\n"
    tested_revision = document["tested_revision"]
    tested_input = behavior_input_fingerprint(document["input_manifest"])
    fail_check("acceptance evidence #{declaration.fetch('id')} input manifest cannot be fingerprinted") unless tested_input
    behavior_input_manifest_errors(
      document["input_manifest"], revision: final_validation_revision,
      module_roots: modules, repository: REPOSITORY_ROOT
    ).each { |error| fail_check("acceptance evidence #{declaration.fetch('id')} current-input #{error}") }
    acceptance_execution_revisions << document["gate_execution_revision"]
    artifact_contract = IdentityPlatformAcceptance.catalog_document.fetch("artifacts").find { |row| row.fetch("artifact_id") == declaration.fetch("id") }
    Dir.mktmpdir("identity-acceptance-execution-") do |directory|
      live_capture = AcceptanceExecutionRunner.run(
        command: declaration.fetch("gate"), chdir: REPOSITORY_ROOT,
        receipt_path: File.join(directory, "execution-receipt.json"),
        artifact_capture_path: File.join(directory, "artifact.json"), artifact_id: declaration.fetch("id"),
        tested_revision: tested_revision, input_root: tested_input,
        tool: document.dig("tool_environment", "tool"), environment: document.dig("tool_environment", "environment"),
        output_artifact_path: artifact_contract.fetch("artifact_evidence_output_path")
      )
      acceptance_evidence_errors(
        document, declaration: declaration, revision: tested_revision, input_fingerprint: tested_input,
        evidence_commit: binding[:commit], module_roots: modules, repository_root: REPOSITORY_ROOT,
        record_path: declaration.fetch("path"), live_capture: live_capture, final_execution: true
      ).each do |error|
        fail_check("acceptance evidence #{declaration.fetch('id')} #{error}")
      end
    end
  end
  fail_check("final acceptance evidence spans multiple execution revisions") unless acceptance_execution_revisions.length == 1
  final_execution_revision = acceptance_execution_revisions.first
  ledger_rows.select { |entry| rows.find { |row| row[:unit] == entry[:unit] }[:status] == "verified" }.each do |entry|
    fail_check("final acceptance execution revision excludes #{entry[:unit]} gate execution") unless git_ancestor?(entry[:gate_revision], final_execution_revision)
  end
end

upstream_leaves_path = upstream_leaves_fixture_path || File.join(ROOT, "UPSTREAM_LEAVES.json")
fail_check("missing upstream leaf inventory") unless File.file?(upstream_leaves_path)
upstream_leaves_manifest = JSON.parse(File.read(upstream_leaves_path))

configuration_catalogs = JSON.parse(artifacts.fetch("CONFIGURATION_CATALOGS.json"))
configuration_document = artifacts.fetch("REFERENCE_CONFIGURATION.md")
{"providers" => "providers.catalog_version", "captcha" => "captcha.catalog_version"}.each do |catalog_name, row_path|
  version = configuration_catalogs.fetch(catalog_name).fetch("version")
  fail_check("#{catalog_name} catalog version row drifted") unless configuration_row(configuration_document, row_path).include?("`#{version}`")
end

transaction_contract = artifacts.fetch("TRANSACTION_CONTRACT.md")
transaction_contract_normalized = transaction_contract.gsub(/\s+/, " ")
[
  "lookup and uniqueness key is exactly tenant, actor, method, canonical route ID",
  "MUST NOT include the request body, request fingerprint",
  "`skipped-fail-on-errors`", "There is no later child admission",
  "`tx.effect.retry_cancel`", "`tx.effect.retry_expire`",
  "`tx.effect.retry_exhaust`", "`tx.effect.retry_reconcile`",
  "legacy terminal row that still retains ciphertext",
  "`issued` is the sole unconsumed capability state",
  "`tx.capability.issue` transition is `absent` to `issued`",
  "`tx.capability.reserve` transition is `issued` to `reserved`",
  "MUST NOT insert a missing capability record",
  "`issued` to `expired` or `revoked`",
  "`reserved` to `finalized`",
  "`reserved` to `released`, `revoked`, or, after the PostgreSQL expiry time has passed, `expired` once authoritative reconciliation proves the owning command did not commit",
  "The `reserved` to `revoked` transition additionally requires a lifecycle invalidation",
  "`finalized`, `released`, `expired`, and `revoked` are terminal and MUST NOT return to `issued` or `reserved`",
  "The parent checkpoint persists the failed count and `cutoff_active=true`",
  "a failed dependency transitions the child to durable `failed`",
  "a successful dependency transitions it to `skipped-fail-on-errors` when the cutoff is already active",
  "Such a child MUST NOT enter `in-progress` after the cutoff",
  "cannot emit a terminal Bulk response while any child is blocked",
  "Ciphertext MUST exist only while an effect is `planned`, `submitted`, `retry-wait`, or `outcome-unknown`, and only until the bearer/operation expiry bound",
  "Every terminal state (`confirmed`, `rejected`, `expired`, `cancelled`, `exhausted`, or `superseded`) MUST erase it atomically with the terminal checkpoint",
  "Entering `confirmed`, `rejected`, `expired`, `cancelled`, `exhausted`, or `superseded` MUST erase bearer or provider-response ciphertext in the same transaction as the terminal checkpoint"
].each do |required|
  fail_check("transaction contract lacks required invariant: #{required}") unless transaction_contract_normalized.include?(required)
end
unless transaction_contract.match?(/same scoped key digest with any\s+different canonical request fingerprint/)
  fail_check("transaction contract lacks same-key canonical-fingerprint conflict")
end
capability_state_contract = transaction_contract[/^Capability states are .*?(?=^## Required proof)/m].to_s.gsub(/\s+/, " ")
[
  "Capability states are `issued`, `reserved`, `finalized`, `released`, `expired`, and `revoked`",
  "Legal transitions are only `absent` to `issued`",
  "`issued` to `reserved`",
  "`issued` to `expired` or `revoked`",
  "`reserved` to `finalized`",
  "`reserved` to `released`, `revoked`, or, after the PostgreSQL expiry time has passed, `expired` once authoritative reconciliation proves the owning command did not commit",
  "The `reserved` to `revoked` transition additionally requires a lifecycle invalidation",
  "`finalized`, `released`, `expired`, and `revoked` are terminal and MUST NOT return to `issued` or `reserved`"
].each do |required|
  fail_check("capability state machine lacks required invariant: #{required}") unless capability_state_contract.include?(required)
end
capability_retention_inputs = {
  transaction_contract: transaction_contract,
  reference_configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md")
}
capability_retention_errors(**capability_retention_inputs).each { |error| fail_check(error) }

contract_retention_fixtures = {
  "missing revoked terminal state" => [
    "`capability.terminal_retention` applies equally to `finalized`, `released`, `expired`, and `revoked`",
    "capability retention omits terminal revoked state"
  ],
  "missing later-of bound" => [
    "A revoked record MUST remain through the later of its original capability expiry and the configured terminal-retention deadline",
    "capability retention omits revoked later-of retention bound"
  ],
  "missing stable denial" => [
    "During that interval `QueryCapability` MUST return its stable redacted revoked classification and reserve MUST continue to return the same non-enumerating denial",
    "capability retention omits stable retained-record denial"
  ],
  "missing crypto-shredding bound" => [
    "Only after both bounds pass MAY cleanup atomically crypto-shred payload and subject linkage",
    "capability retention omits post-bound crypto-shredding"
  ],
  "missing persistent tombstone" => [
    "It MUST retain a restricted tombstone containing only tenant, purpose, keyed digest, key version, original expiry, and `revoked`; the tombstone has no time-based deletion and every reserve/query continues the same denial",
    "capability retention omits persistent restricted tombstone"
  ],
  "missing key-retirement gate" => [
    "The tombstone MAY be deleted only after the cryptographic key version is retired and proof shows no bearer under it can validate",
    "capability retention omits proved key-retirement deletion gate"
  ],
  "missing post-disposal replay denial" => [
    "Post-disposal replay is therefore denied by the tombstone or, after that proved key retirement, by cryptographic validation before lookup",
    "capability retention omits post-disposal replay denial"
  ],
  "reconstructable issuance" => [
    "Issuance creates a new random bearer/digest and MUST NOT reconstruct `issued` from the presented bearer",
    "capability retention permits replay reconstruction"
  ]
}
contract_retention_fixtures.each do |label, (removed, expected_error)|
  mutated_contract = without_normalized_phrase(transaction_contract, removed)
  fail_check("capability retention negative fixture #{label} did not mutate its source") if mutated_contract == transaction_contract
  expect_capability_retention_fixture_rejection!(label, expected_error) do
    capability_retention_errors(**capability_retention_inputs.merge(transaction_contract: mutated_contract))
  end
end

configuration_retention_fixtures = {
  "configuration without revoked" => ["finalized/released/expired/revoked", "capability retention configuration omits terminal revoked state"],
  "configuration without later-of bound" => ["later of original capability expiry and retention deadline", "capability retention configuration omits revoked later-of retention bound"],
  "configuration without crypto-shredding" => ["crypto-shredded", "capability retention configuration omits post-bound crypto-shredding"],
  "configuration without persistent tombstone" => [
    "minimal tenant/purpose/keyed-digest/key-version/expiry/revoked tombstone has no time-based deletion",
    "capability retention configuration omits persistent restricted tombstone"
  ],
  "configuration without replay denial" => ["replay denied", "capability retention configuration omits replay denial"],
  "configuration without key-retirement gate" => ["proved key-version retirement", "capability retention configuration omits proved key-retirement deletion gate"]
}
configuration_retention_fixtures.each do |label, (removed, expected_error)|
  reference_configuration = capability_retention_inputs.fetch(:reference_configuration)
  terminal_retention_row = configuration_row(reference_configuration, "capability.terminal_retention")
  mutated_row = without_normalized_phrase(terminal_retention_row, removed)
  fail_check("capability retention negative fixture #{label} did not mutate its source") if mutated_row == terminal_retention_row
  mutated_configuration = reference_configuration.sub(terminal_retention_row, mutated_row)
  expect_capability_retention_fixture_rejection!(label, expected_error) do
    capability_retention_errors(**capability_retention_inputs.merge(reference_configuration: mutated_configuration))
  end
end

lifecycle_contract = artifacts.fetch("LIFECYCLE_CASCADES.md")
lifecycle_contract_normalized = lifecycle_contract.gsub(/\s+/, " ")
%w[applied not-applicable pending limited outcome-unknown].each do |status|
  fail_check("lifecycle acknowledgement enum omits #{status}") unless lifecycle_contract.include?("`#{status}`")
end
delivery_retention = "ciphertext is retained only in `planned`, `submitted`, `retry-wait`, or `outcome-unknown` before expiry and erases atomically at `confirmed`, `rejected`, `expired`, `cancelled`, `exhausted`, or `superseded`"
unless lifecycle_contract_normalized.include?(delivery_retention)
  fail_check("delivery lifecycle omits a terminal ciphertext-erasure state")
end
unless lifecycle_contract.include?("matching\n`LIFECYCLE_CONSUMERS.md`")
  fail_check("lifecycle acknowledgement enum does not bind consumer manifest")
end
lifecycle_consumers_contract = artifacts.fetch("LIFECYCLE_CONSUMERS.md").gsub(/\s+/, " ")
[
  "`capability/postgres` is the exact lifecycle consumer",
  "`issued` capability in that tenant, purpose, and subject scope to `revoked`",
  "MUST atomically set `revocation_pending=true`"
].each do |required|
  fail_check("lifecycle consumer contract lacks capability cleanup: #{required}") unless lifecycle_consumers_contract.include?(required)
end
privacy_persistence_binding = "When the reference PostgreSQL profile is selected, `identity/postgres` MUST persist the %s anonymization/deletion checkpoint and privacy-export fragment for the exact tenant, subject, snapshot ID, policy version, contributor version, content digest, and terminal outcome in the owning coordinator transaction"
privacy_applicability = JSON.parse(File.read(File.join(ROOT, SharedContractApplicability::FILE))).fetch("units")
%w[email phone].each do |kind|
  unit = "identity/#{kind}"
  fail_check("#{unit} lacks exact privacy persistence binding") unless goal_bodies.fetch(unit).gsub(/\s+/, " ").include?(format(privacy_persistence_binding, kind))
  selections = privacy_applicability.fetch(unit).fetch("lifecycle")
  fail_check("#{unit} omits privacy-export artifact applicability") unless selections.include?("lifecycle.artifact.privacy_export")
end

if execution_mode
  preflight = execution_fixture_path ? File.read(execution_fixture_path) : artifacts.fetch("PREFLIGHT_EVIDENCE.md")
  identity_rows = markdown_table(preflight, "Execution identity", "| Field | Value |").to_h
  execution_identity_errors(identity_rows, require_clean: clean_integration_mode).each { |error| fail_check(error) }
  branch = plain_cell(identity_rows.fetch("Integration branch", ""))
  integration_worktree = plain_cell(identity_rows.fetch("Integration worktree", ""))
  worktree_parent = plain_cell(identity_rows.fetch("Task-owned worktree parent", ""))
  recorded_at = plain_cell(identity_rows.fetch("Preflight recorded at (RFC3339)", ""))

  worker_attestation_header = "| Unit | Generation | Integration baseline | Assignment commit | Assignment goal path | Rendered prompt | Prompt digest | Model | Reasoning | Fork turns | Subagents | Package scope | Reserved descendants | Goal digest | Authorized by | Status |"
  worker_attestations = markdown_table(preflight, "Worker assignment authorizations", worker_attestation_header).map do |cells|
    fail_check("worker assignment authorization row has wrong column count") unless cells.length == 16
    unit, generation, baseline, assignment, goal_path, prompt_path, prompt_digest, model, reasoning, fork_turns, subagents, package_scope, reserved, goal_digest, authorized_by, status = cells
    {
      unit: plain_cell(unit), generation: plain_cell(generation), baseline: plain_cell(baseline),
      assignment: plain_cell(assignment), goal_path: plain_cell(goal_path),
      prompt_path: plain_cell(prompt_path), prompt_digest: plain_cell(prompt_digest),
      model: plain_cell(model), reasoning: plain_cell(reasoning), fork_turns: plain_cell(fork_turns),
      subagents: plain_cell(subagents), package_scope: plain_cell(package_scope),
      reserved: reserved == "none" ? [] : reserved.scan(/`([^`]+)`/).flatten,
      goal_digest: plain_cell(goal_digest), authorized_by: plain_cell(authorized_by), status: plain_cell(status),
      row_line: "| #{cells.join(' | ')} |"
    }
  end
  attestation_keys = worker_attestations.map { |row| [row[:unit], row[:generation]] }
  fail_check("worker assignment authorizations contain duplicate unit/generation rows") unless attestation_keys == attestation_keys.uniq

  runtime_header = "| Unit | Generation | Worker task | Agent ID | Model | Reasoning | Fork turns | Subagents | Platform source | Recorded at |"
  worker_runtime_attestations = markdown_table(preflight, "Worker runtime attestations", runtime_header).map do |cells|
    fail_check("worker runtime attestation row has wrong column count") unless cells.length == 10
    unit, generation, task, agent_id, model, reasoning, fork_turns, subagents, source, recorded_at = cells
    {
      unit: plain_cell(unit), generation: plain_cell(generation), task: plain_cell(task), agent_id: plain_cell(agent_id),
      model: plain_cell(model), reasoning: plain_cell(reasoning), fork_turns: plain_cell(fork_turns),
      subagents: plain_cell(subagents), source: plain_cell(source), recorded_at: plain_cell(recorded_at),
      row_line: "| #{cells.join(' | ')} |"
    }
  end
  runtime_keys = worker_runtime_attestations.map { |row| [row[:unit], row[:generation]] }
  fail_check("worker runtime attestations contain duplicate unit/generation rows") unless runtime_keys == runtime_keys.uniq
  runtime_agent_ids = worker_runtime_attestations.map { |row| row[:agent_id] }
  fail_check("worker runtime attestations reuse an immutable agent ID") unless runtime_agent_ids == runtime_agent_ids.uniq

  tool_rows = markdown_table(
    preflight,
    "Tool and environment lanes",
    "| Requirement/profile | Required by units or claims | Classification | Version/environment identity | Evidence path or blocking claim |"
  )
  fail_check("execution preflight has no tool/environment lanes") if tool_rows.empty?
  classifications = Set["available", "unavailable", "not-yet-needed"]
  tool_rows.each do |requirement, required_by, classification, identity, evidence|
    fail_check("tool/environment lane has an empty field") if [requirement, required_by, classification, identity, evidence].any?(&:empty?)
    fail_check("tool/environment lane #{requirement} has invalid classification") unless classifications.include?(plain_cell(classification))
    fail_check("unavailable tool/environment lane #{requirement} lacks blocking claims") if plain_cell(classification) == "unavailable" && plain_cell(evidence).match?(/\A(?:—|not-needed)\z/)
    fail_check("tool/environment lane #{requirement} has unsafe evidence or blocker") unless safe_preflight_evidence_or_blocker?(plain_cell(evidence))
  end

  external_rows = markdown_table(
    preflight,
    "External evidence lanes",
    "| Safe profile ID | Consuming units | Exact acceptance claim IDs | Classification | Credential source metadata | Evidence path or blocker | Evidence record commit | Evidence digest or blocker |"
  )
  fail_check("execution preflight has no external-evidence classifications") if external_rows.empty?
  external_rows.each do |profile, consumers, claims, classification, credential_source, evidence, evidence_commit, evidence_digest|
    profile = plain_cell(profile)
    fail_check("external evidence profile ID is unsafe") unless profile.match?(/\A[a-zA-Z0-9._-]+\z/)
    consumer_units = consumers.scan(/`([^`]+)`/).flatten
    claim_ids = claims.scan(/`([^`]+)`/).flatten
    fail_check("external evidence profile #{profile} has no consumers") if consumer_units.empty?
    fail_check("external evidence profile #{profile} consumers are not sorted and unique") unless consumer_units == consumer_units.sort.uniq
    fail_check("external evidence profile #{profile} has unknown consumers") unless consumer_units.all? { |unit| known.include?(unit) }
    fail_check("external evidence profile #{profile} lacks exact claim IDs") if claim_ids.empty?
    fail_check("external evidence profile #{profile} claim IDs are not sorted and unique") unless claim_ids == claim_ids.sort.uniq
    classification = plain_cell(classification)
    credential_source = plain_cell(credential_source)
    evidence = plain_cell(evidence)
    evidence_commit = plain_cell(evidence_commit)
    evidence_digest = plain_cell(evidence_digest)
    fail_check("external evidence profile #{profile} has invalid classification") unless classifications.include?(classification)
    fail_check("external evidence profile #{profile} lacks safe credential-source metadata") unless credential_source.match?(/\A(?:none|[a-zA-Z0-9._-]+:[a-zA-Z0-9._-]+)\z/)
    marker = classification == "unavailable" ? "blocker:#{profile}" : "not-yet-needed:#{profile}"
    if repository_evidence_path?(evidence)
      fail_check("external evidence profile #{profile} is unavailable but names a record") if classification == "unavailable"
      fail_check("external evidence profile #{profile} record commit is invalid") unless evidence_commit.match?(/\A[0-9a-f]{40}\z/) && git_commit_exists?(evidence_commit)
      committed_record = git_blob_bytes(evidence_commit, evidence)
      fail_check("external evidence profile #{profile} record is absent from its bound commit") unless committed_record
      working_record = File.binread(File.expand_path(evidence, REPOSITORY_ROOT))
      fail_check("external evidence profile #{profile} working record differs from its bound commit") unless committed_record == working_record
      digest = "sha256:#{Digest::SHA256.hexdigest(committed_record)}"
      fail_check("external evidence profile #{profile} record digest drifted") unless evidence_digest == digest
      begin
        external_records[evidence] = JSON.parse(committed_record)
      rescue JSON::ParserError
        fail_check("external evidence profile #{profile} record is not valid JSON")
      end
    else
      fail_check("external evidence profile #{profile} has unsafe or mismatched blocker") unless evidence == marker
      fail_check("external evidence profile #{profile} blocker unexpectedly names a record commit") unless evidence_commit == "—"
      fail_check("external evidence profile #{profile} digest/blocker drifted") unless evidence_digest == marker
    end
    external_lanes << {profile: profile, consumers: consumer_units, claims: claim_ids, classification: classification, evidence: evidence, evidence_commit: evidence_commit}
  end
  external_profiles = external_lanes.map { |lane| lane[:profile] }
  fail_check("execution preflight contains duplicate external profiles") unless external_profiles == external_profiles.uniq
  preflight_recorded_at = Time.iso8601(plain_cell(identity_rows.fetch("Preflight recorded at (RFC3339)")))
  external_lanes.select { |lane| external_records.key?(lane[:evidence]) }.each do |lane|
    errors = external_evidence_record_errors(
      external_records.fetch(lane[:evidence]), profile: lane[:profile], claims: lane[:claims], consumers: lane[:consumers],
      recorded_after: preflight_recorded_at, evidence_commit: lane[:evidence_commit],
      module_roots: modules, repository_root: REPOSITORY_ROOT, record_path: lane[:evidence]
    )
    fail_check("external evidence profile #{lane[:profile]} record invalid: #{errors.join('; ')}") unless errors.empty?
  end
  external_records.each do |path, record|
    expected_profiles = external_lanes.select { |lane| lane[:evidence] == path }.map { |lane| lane[:profile] }.sort
    actual_profiles = record.fetch("profiles").map { |profile| profile["profile_id"] }
    fail_check("external evidence bundle #{path} profile attribution drifted") unless actual_profiles == expected_profiles && actual_profiles == actual_profiles.uniq
  end

  acceptance_evidence_bindings = markdown_table(
    preflight,
    "Acceptance evidence bindings",
    "| Artifact ID | Evidence path | Evidence blob digest | Evidence record commit | Bound at |"
  ).map do |artifact_id, path, digest, commit, bound_at|
    binding = {
      artifact_id: plain_cell(artifact_id), path: plain_cell(path), digest: plain_cell(digest),
      commit: plain_cell(commit), bound_at: plain_cell(bound_at)
    }
    fail_check("acceptance evidence binding artifact ID is unsafe") unless binding[:artifact_id].match?(/\A[a-z0-9][a-z0-9.-]+\z/)
    fail_check("acceptance evidence binding path is unsafe or missing") unless repository_evidence_path?(binding[:path])
    fail_check("acceptance evidence binding commit is invalid") unless binding[:commit].match?(/\A[0-9a-f]{40}\z/) && git_commit_exists?(binding[:commit])
    committed = git_blob_bytes(binding[:commit], binding[:path])
    fail_check("acceptance evidence binding path is absent from its commit") unless committed
    fail_check("acceptance evidence binding working bytes differ from committed bytes") unless committed == File.binread(File.expand_path(binding[:path], REPOSITORY_ROOT))
    fail_check("acceptance evidence binding digest drifted") unless binding[:digest] == "sha256:#{Digest::SHA256.hexdigest(committed)}"
    fail_check("acceptance evidence binding timestamp is invalid") unless rfc3339?(binding[:bound_at])
    binding
  end
  binding_ids = acceptance_evidence_bindings.map { |binding| binding[:artifact_id] }
  fail_check("acceptance evidence bindings contain duplicate artifact IDs") unless binding_ids == binding_ids.uniq

  primitive_rows = markdown_table(
    preflight,
    "Existing primitive contracts",
    "| Primitive | Consuming units | Registered module/package | API input fingerprint | Gate fingerprint and result | Evidence path |"
  )
  primitive_name_list = primitive_rows.map { |row| plain_cell(row[0]) }
  fail_check("execution preflight contains duplicate primitive rows") unless primitive_name_list == primitive_name_list.uniq
  fail_check("execution preflight primitive inventory drifted") unless primitive_name_list.to_set == consumed_primitives
  noncurrent_primitives_by_consumer = Hash.new { |hash, key| hash[key] = [] }
  primitive_rows.each do |primitive, consumers, registered, api_fingerprint, gate_result, evidence|
    name = plain_cell(primitive)
    consumer_units = consumers.scan(/`([^`]+)`/).flatten
    fail_check("primitive #{name} has no consumers") if consumer_units.empty?
    fail_check("primitive #{name} consumers are not sorted and unique") unless consumer_units == consumer_units.sort.uniq
    fail_check("primitive #{name} has unknown consumers") unless consumer_units.all? { |unit| known.include?(unit) }
    expected_consumers = primitive_consumers.fetch(name).sort
    fail_check("primitive #{name} consumer inventory drifted: expected #{expected_consumers}, got #{consumer_units}") unless consumer_units == expected_consumers
    registered_name = plain_cell(registered).delete_prefix("pkg/")
    fail_check("primitive #{name} registered module/package was substituted") unless registered_name == name
    fail_check("primitive #{name} is not registered") unless resolvable_consumables.include?(registered_name)
    consumer_units.each { |unit| primitive_module_roots_by_consumer[unit] << "pkg/#{registered_name}" }
    fail_check("primitive #{name} API fingerprint is invalid") unless plain_cell(api_fingerprint).match?(/\Asha256:[0-9a-f]{64}\z/)
    gate_match = plain_cell(gate_result).match(/\Asha256:[0-9a-f]{64} (pass|failed|blocked|stale)\z/)
    fail_check("primitive #{name} gate fingerprint/result is invalid") unless gate_match
    fail_check("primitive #{name} evidence path is unsafe or missing") unless repository_evidence_path?(plain_cell(evidence))
    unless gate_match[1] == "pass"
      consumer_units.each { |unit| noncurrent_primitives_by_consumer[unit] << name }
    end
  end
  noncurrent_primitives_by_consumer.each do |unit, primitives|
    inventory_row = rows.find { |row| row[:unit] == unit }
    unless ["proposed", "blocked"].include?(inventory_row[:status])
      fail_check("#{unit} has non-current primitive evidence while #{inventory_row[:status]}: #{primitives.sort}")
    end
    next if inventory_row[:status] == "proposed"

    blocker_ids = primitives.map { |primitive| "blocker:primitive-#{primitive.tr('/', '-')}" }
    unless blocker_ids.include?(inventory_row[:owner])
      fail_check("#{unit} blocked primitive evidence lacks an exact blocker claim: expected one of #{blocker_ids.sort}")
    end
  end

  resource_rows = markdown_table(
    preflight,
    "Task-owned resource registry",
    resource_registry_header
  )
  fail_check("execution preflight has no registered task-owned resources") if resource_rows.empty?
  resource_states = Set["active", "retained-for-recovery", "removal-pending-after-final-commit", "removed"]
  local_resource_types = Set["go-cache", "temporary-directory", "browser-artifact"]
  external_resource_types = Set["process", "container", "image", "volume", "database-payload", "provider-fixture"]
  resource_types = local_resource_types | external_resource_types | Set["worktree"]
  resource_rows.each do |resource_id, type, owner, target, state, cleanup_trigger, reconciled_at, cleanup_evidence|
    resource_id = plain_cell(resource_id)
    type = plain_cell(type)
    owner = plain_cell(owner)
    target = plain_cell(target)
    state = plain_cell(state)
    cleanup_evidence = plain_cell(cleanup_evidence)
    fail_check("resource ID is unsafe") unless resource_id.match?(/\A[a-zA-Z0-9._-]+\z/)
    fail_check("resource #{resource_id} has incomplete ownership") if type.empty? || owner.empty?
    fail_check("resource #{resource_id} has unsupported type #{type}") unless resource_types.include?(type)
    fail_check("resource #{resource_id} has invalid state") unless resource_states.include?(state)
    local_target_exists = false
    local_path_entry_exists = false
    worktree_clean = nil
    worktree_head = nil
    safe_target = if type == "worktree"
      fail_check("worktree resource #{resource_id} owner is unsafe") unless owner.match?(/\A[a-zA-Z0-9._\/-]+\z/)
      if state == "removed"
        clean_parent = File.realpath(worktree_parent)
        clean_target = File.expand_path(target)
        integration_target = clean_target == File.expand_path(integration_worktree)
        owner_ok = integration_target ? owner == "coordinator" : owner != "coordinator"
        removed_safe = target.start_with?("/") && target == clean_target && clean_target != clean_parent &&
          clean_target.start_with?(clean_parent + File::SEPARATOR) &&
          !File.exist?(clean_target) && !File.symlink?(clean_target) &&
          !registered_worktree_paths.include?(clean_target) && owner_ok
        if removed_safe
          worktree_resource_rows << {id: resource_id, type: type, target: target, owner: owner, state: state,
                                     evidence: cleanup_evidence, integration: integration_target, clean: nil, head: nil}
        end
        removed_safe
      else
        identity = safe_task_worktree_identity(target, worktree_parent)
        clean_target = identity&.first
        registered = clean_target && registered_worktree_paths.include?(clean_target)
        exact = clean_target && target == clean_target
        integration_target = clean_target && clean_target == File.realpath(integration_worktree)
        identity_ok = if integration_target
                        owner == "coordinator" && safe_integration_worktree_path?(target, worktree_parent)
                      else
                        owner != "coordinator" && safe_worktree_path?(target, worktree_parent)
                      end
        if identity && registered && exact && identity_ok
          status_output, status_result = Open3.capture2("git", "-C", target, "status", "--porcelain")
          head_output, head_result = Open3.capture2("git", "-C", target, "rev-parse", "HEAD")
          worktree_clean = status_result.success? && status_output.empty?
          worktree_head = head_output.strip if head_result.success? && head_output.strip.match?(/\A[0-9a-f]{40}\z/)
          worktree_resource_rows << {id: resource_id, type: type, target: target, owner: owner, state: state,
                                     evidence: cleanup_evidence, integration: integration_target,
                                     clean: worktree_clean, head: worktree_head}
        end
        identity && registered && exact && identity_ok
      end
    elsif local_resource_types.include?(type)
      clean_target = File.expand_path(target)
      clean_parent = File.realpath(worktree_parent)
      canonical_local_target = if File.exist?(clean_target)
                                 File.realpath(clean_target)
                               else
                                 clean_target
                               end
      registered_worktree_target = if File.exist?(clean_target)
                                     registered_worktree_paths.include?(File.realpath(clean_target))
                                   else
                                     false
                                   end
      local_target_exists = File.exist?(clean_target)
      local_path_entry_exists = local_target_exists || File.symlink?(clean_target)
      target.start_with?("/") && target == clean_target && canonical_local_target == clean_target && !registered_worktree_target &&
        clean_target != clean_parent && clean_target.start_with?(clean_parent + File::SEPARATOR)
    else
      !target.start_with?("/") && target.match?(/\A[a-zA-Z0-9._:\/-]+\z/) && !target.split("/").include?("..")
    end
    fail_check("resource #{resource_id} target is unsafe") unless safe_target
    if state == "removal-pending-after-final-commit"
      integration_pending = type == "worktree" && worktree_resource_rows.last&.fetch(:target) == target &&
        worktree_resource_rows.last.fetch(:integration)
      fail_check("only the integration worktree may be removal-pending") unless integration_pending
    end
    fail_check("resource #{resource_id} lacks cleanup trigger") if cleanup_trigger.empty? || plain_cell(cleanup_trigger) == "—"
    fail_check("resource #{resource_id} reconciliation timestamp is invalid") unless rfc3339?(plain_cell(reconciled_at))
    if type == "worktree"
      evidence_ok = if state == "removed"
                      repository_evidence_path?(cleanup_evidence)
                    else
                      cleanup_evidence == "not-yet-needed:#{resource_id}"
                    end
      fail_check("worktree resource #{resource_id} lacks state-specific cleanup evidence") unless evidence_ok
    elsif local_resource_types.include?(type)
      existence_matches = state == "removed" ? !local_path_entry_exists : local_target_exists
      fail_check("local resource #{resource_id} existence does not match #{state}") unless existence_matches
      evidence_ok = if state == "removed"
                      repository_evidence_path?(cleanup_evidence)
                    else
                      cleanup_evidence == "not-yet-needed:#{resource_id}"
                    end
      fail_check("local resource #{resource_id} lacks state-specific cleanup evidence") unless evidence_ok
    else
      attestation = "attestation:#{state}:#{target}"
      unless cleanup_evidence == attestation || repository_evidence_path?(cleanup_evidence)
        fail_check("external resource #{resource_id} lacks state-specific cleanup attestation")
      end
    end
    task_owned_resource_rows << {
      id: resource_id, type: type, owner: owner, state: state, target: target,
      evidence: cleanup_evidence, clean: worktree_clean, head: worktree_head
    }
  end
  resource_ids = task_owned_resource_rows.map { |resource| resource[:id] }
  fail_check("task-owned resource registry contains duplicate resource IDs") unless resource_ids.uniq == resource_ids
  live_worktree_targets = worktree_resource_rows.map { |resource| resource[:target] }
  fail_check("task-owned resource registry contains duplicate live worktrees") unless live_worktree_targets.uniq == live_worktree_targets
  integration_resources = worktree_resource_rows.select { |resource| resource[:integration] }
  unless integration_resources.length == 1 && integration_resources.first[:owner] == "coordinator" && integration_resources.first[:state] != "removed"
    fail_check("integration worktree requires exactly one live coordinator-owned worktree resource")
  end
  if rows.all? { |row| row[:status] == "verified" }
    incomplete_cleanup = task_owned_resource_rows.reject do |resource|
      resource[:state] == "removed" ||
        (resource[:type] == "worktree" && resource[:target] == integration_worktree &&
          resource[:state] == "removal-pending-after-final-commit")
    end
    unless incomplete_cleanup.empty?
      fail_check("completed program retains active task-owned resources: #{incomplete_cleanup.map { |resource| resource[:id] }.sort}")
    end
  end

  goal_revision_rows = markdown_table(
    preflight,
    "Goal digest revisions",
    "| Revision ID | Unit | Previous goal digest | Current goal digest | Status | Authorized by | Recorded at |"
  ).map do |revision_id, unit, previous_digest, current_digest, status, authorized_by, recorded_at|
    row = {
      revision_id: plain_cell(revision_id), unit: plain_cell(unit),
      previous_digest: plain_cell(previous_digest), current_digest: plain_cell(current_digest),
      status: plain_cell(status), authorized_by: plain_cell(authorized_by), recorded_at: plain_cell(recorded_at)
    }
    expected_revision_prefix = "goal:#{row[:unit].tr('/', '-')}:g"
    fail_check("goal revision ID is unsafe or belongs to another unit") unless row[:revision_id].match?(/\A#{Regexp.escape(expected_revision_prefix)}[1-9]\d*\z/)
    fail_check("goal revision unit is unknown") unless known.include?(row[:unit])
    fail_check("goal revision previous digest is invalid") unless row[:previous_digest].match?(/\Asha256:[0-9a-f]{64}\z/)
    fail_check("goal revision current digest is invalid") unless row[:current_digest].match?(/\Asha256:[0-9a-f]{64}\z/)
    fail_check("goal revision status is invalid") unless %w[authorized applied superseded].include?(row[:status])
    fail_check("goal revision timestamp is invalid") unless rfc3339?(row[:recorded_at])
    row
  end
  goal_revision_lifecycle_errors(goal_revision_rows).each { |error| fail_check(error) }

  recovery_rows = markdown_table(
    preflight,
    "Conflict-recovery baselines",
    "| Recovery epoch | Unit | Generation | Integration commit | Worker checkpoint | Conflict evidence path | Status | Recorded at |"
  )
  recovery_statuses = Set["authorized", "superseded", "completed"]
  normalized_recovery_rows = recovery_rows.map do |epoch, unit, generation, integration_commit, worker_checkpoint, evidence, status, recorded_at|
    unit = plain_cell(unit)
    epoch = plain_cell(epoch)
    generation = plain_cell(generation)
    expected_epoch_prefix = "recovery:#{unit.tr('/', '-')}:g#{generation}:e"
    fail_check("conflict-recovery row for #{unit} has invalid epoch") unless epoch.match?(/\A#{Regexp.escape(expected_epoch_prefix)}[1-9]\d*\z/)
    fail_check("conflict-recovery row has unknown unit") unless known.include?(unit)
    fail_check("conflict-recovery row for #{unit} has invalid generation") unless generation.match?(/\A\d+\z/)
    fail_check("conflict-recovery row for #{unit} has invalid integration parent") unless plain_cell(integration_commit).match?(/\A[0-9a-f]{40}\z/)
    fail_check("conflict-recovery row for #{unit} has invalid worker checkpoint") unless plain_cell(worker_checkpoint).match?(/\A[0-9a-f]{40}\z/)
    fail_check("conflict-recovery row for #{unit} has unsafe or missing evidence path") unless repository_evidence_path?(plain_cell(evidence))
    fail_check("conflict-recovery row for #{unit} has invalid status") unless recovery_statuses.include?(plain_cell(status))
    fail_check("conflict-recovery row for #{unit} has invalid timestamp") unless rfc3339?(plain_cell(recorded_at))
    [unit, generation, plain_cell(integration_commit), plain_cell(worker_checkpoint), plain_cell(evidence), plain_cell(status), plain_cell(recorded_at), epoch]
  end
  normalized_recovery_rows.group_by { |row| row.values_at(0, 1) }.each do |(unit, generation), history|
    expected_sequence = 0
    active_epoch = nil
    history.each do |row|
      epoch_number = row[7][/:e(\d+)\z/, 1].to_i
      if row[5] == "authorized"
        expected_sequence += 1
        fail_check("recovery #{unit} generation #{generation} epoch sequence drifted") unless epoch_number == expected_sequence
        active_epoch = epoch_number
      else
        fail_check("recovery #{unit} generation #{generation} terminal epoch drifted") unless epoch_number == active_epoch
      end
    end
  end
  recovery_rows_for_validation = normalized_recovery_rows

  repair_header = "| Repair epoch | Unit | Generation | Integration baseline | Integration checkpoint | Worker checkpoint | Goal path | Goal digest | Rendered repair prompt | Prompt digest | Reserved descendants | Result worker commit | Result integration checkpoint | Status | Recorded at |"
  repair_rows_for_validation = markdown_table(preflight, "Integrated-repair authorizations", repair_header).map do |cells|
    fail_check("integrated-repair row has wrong column count") unless cells.length == 15
    epoch, unit, generation, baseline, checkpoint, worker_checkpoint, goal_path, goal_digest, prompt_path, prompt_digest, reserved, result_worker, result_checkpoint, status, recorded_at = cells.map { |value| plain_cell(value) }
    expected_prefix = "repair:#{unit.tr('/', '-')}:g#{generation}:e"
    fail_check("integrated-repair row for #{unit} has invalid epoch") unless epoch.match?(/\A#{Regexp.escape(expected_prefix)}[1-9]\d*\z/)
    fail_check("integrated-repair row has unknown unit") unless known.include?(unit)
    fail_check("integrated-repair row for #{unit} has invalid generation") unless generation.match?(/\A\d+\z/)
    [baseline, checkpoint, worker_checkpoint].each do |commit|
      fail_check("integrated-repair row for #{unit} has invalid commit identity") unless commit.match?(/\A[0-9a-f]{40}\z/)
    end
    fail_check("integrated-repair row for #{unit} has unsafe goal path") unless goal_path.match?(%r{\A(?:\.ai/identity-platform/goals|pkg/[a-zA-Z0-9._/-]+/\.ai)/[a-zA-Z0-9._/-]+\.md\z}) && !goal_path.include?("..")
    fail_check("integrated-repair row for #{unit} has invalid goal digest") unless goal_digest.match?(/\Asha256:[0-9a-f]{64}\z/)
    fail_check("integrated-repair row for #{unit} has unsafe prompt path") unless repository_evidence_path?(prompt_path)
    fail_check("integrated-repair row for #{unit} has invalid prompt digest") unless prompt_digest.match?(/\Asha256:[0-9a-f]{64}\z/)
    reserved_roots = reserved == "none" ? [] : cells[10].scan(/`([^`]+)`/).flatten
    fail_check("integrated-repair row for #{unit} has invalid status") unless %w[authorized superseded completed].include?(status)
    if status == "completed"
      fail_check("integrated-repair completed row for #{unit} has invalid result commits") unless [result_worker, result_checkpoint].all? { |commit| commit.match?(/\A[0-9a-f]{40}\z/) }
    else
      fail_check("integrated-repair non-completed row for #{unit} has result commits") unless result_worker == "—" && result_checkpoint == "—"
    end
    fail_check("integrated-repair row for #{unit} has invalid timestamp") unless rfc3339?(recorded_at)
    {epoch: epoch, unit: unit, generation: generation, baseline: baseline, checkpoint: checkpoint,
     worker_checkpoint: worker_checkpoint, goal_path: goal_path, goal_digest: goal_digest, prompt_path: prompt_path,
     prompt_digest: prompt_digest, reserved: reserved_roots, result_worker: result_worker,
     result_checkpoint: result_checkpoint, status: status, recorded_at: recorded_at,
     row_line: "| #{cells.join(' | ')} |"}
  end
  repair_rows_for_validation.group_by { |row| [row[:unit], row[:generation]] }.each do |(unit, generation), history|
    expected_epoch = 0
    authorization = nil
    history.each do |row|
      epoch = row[:epoch][/:e(\d+)\z/, 1].to_i
      if row[:status] == "authorized"
        fail_check("integrated-repair #{unit} generation #{generation} starts a new epoch before terminating the active epoch") if authorization
        expected_epoch += 1
        fail_check("integrated-repair #{unit} generation #{generation} epoch sequence drifted") unless epoch == expected_epoch
        authorization = row
      else
        fail_check("integrated-repair terminal lacks exact active authorization") unless authorization &&
          row.reject { |key, _| %i[status recorded_at row_line result_worker result_checkpoint].include?(key) } == authorization.reject { |key, _| %i[status recorded_at row_line result_worker result_checkpoint].include?(key) }
        authorization = nil
      end
    end
  end
end

upstream_manifest = JSON.parse(artifacts.fetch("UPSTREAM_SURFACE.json"))
fail_check("upstream surface manifest schema drifted") unless upstream_manifest.fetch("schema_version") == 2
upstream = upstream_manifest.fetch("upstream")
fail_check("upstream repository drifted") unless upstream.fetch("repository") == "https://github.com/better-auth/better-auth.git"
fail_check("upstream revision drifted") unless upstream.fetch("revision") == BASELINE
fail_check("upstream object format drifted") unless upstream.fetch("object_format") == "git-sha1"
upstream_sources = upstream_manifest.fetch("sources").to_h do |source|
  path = source.fetch("path")
  fail_check("duplicate upstream source path #{path}") if upstream_manifest.fetch("sources").count { |candidate| candidate.fetch("path") == path } != 1
  [path, [source.fetch("kind"), source.fetch("object_id"), source.fetch("disposition_section"), source.fetch("enumeration")]]
end
fail_check("pinned upstream source identities drifted") unless upstream_sources == EXPECTED_UPSTREAM_SOURCES
disposition_headings = artifacts.fetch("UPSTREAM_DISPOSITIONS.md").scan(/^## (.+)$/).flatten.to_set
missing_disposition_sections = upstream_sources.values.map { |source| source.fetch(2) }.to_set - disposition_headings
fail_check("upstream sources name missing disposition sections: #{missing_disposition_sections.to_a}") unless missing_disposition_sections.empty?

operation_catalog = artifacts.fetch("API_OPERATIONS.md")[
  /^## Core identity, password, and account operations\n(.*?)(?=^## Closure requirements)/m, 1
].to_s
operations = operation_catalog.lines.filter_map do |line|
  next unless line.start_with?("| `")

  cells = line.split("|").map(&:strip)
  fail_check("API operation row has wrong column count: #{line.chomp}") unless cells.length == 7
  operation_id = cells[1][/\A`([a-z0-9][a-z0-9._-]+)`\z/, 1]
  fail_check("API operation ID is invalid: #{cells[1]}") unless operation_id
  fail_check("API operation #{operation_id} has incomplete contract columns") if cells.values_at(2, 3, 4, 5).any?(&:empty?)
  exposure = cells[2][/ \/ (both|direct|protocol|middleware)\z/, 1]
  fail_check("API operation #{operation_id} has invalid exposure") unless exposure
  idempotency = cells[4].split(" / ", 2)[1]
  fail_check("API operation #{operation_id} has invalid idempotency") unless idempotency
  [operation_id, cells[2], cells[4].split(" /", 2).first, exposure, idempotency, cells[3], cells[5]]
end
operation_ids = operations.map(&:first)
fail_check("API operation IDs are not unique") unless operation_ids.uniq == operation_ids
fail_check("API operation catalog is unexpectedly thin") if operation_ids.length < 200
operation_inventory = upstream_manifest.fetch("operation_inventory")
canonical_operation_ids = operation_ids.sort
fail_check("pinned operation inventory source drifted") unless operation_inventory.fetch("source") == "API_OPERATIONS.md"
fail_check("pinned operation ID set drifted") unless operation_inventory.fetch("ids") == canonical_operation_ids
fail_check("pinned operation count drifted") unless operation_inventory.fetch("count") == canonical_operation_ids.length
fail_check("pinned operation ID digest drifted") unless operation_inventory.fetch("sha256") == canonical_inventory_digest(canonical_operation_ids)
previous_owners = []
operation_owners = Set.new
operation_owner_map = {}
operations.each do |operation_id, owner_cell, _rate_class, _exposure, _idempotency|
  owners = owner_cell.scan(/`([^`]+)`/).flatten
  owners = previous_owners if owners.empty? && owner_cell.start_with?("same ")
  fail_check("API operation #{operation_id} lacks an explicit resolvable owner") if owners.empty?
  unknown_owners = owners.reject { |owner| known.include?(owner) || registered_consumables.include?(owner) }
  fail_check("API operation #{operation_id} has unknown owners: #{unknown_owners.join(', ')}") unless unknown_owners.empty?
  operation_owner_map[operation_id] = owners.sort
  operation_owners.merge(owners)
  previous_owners = owners
end
operation_contracts = operations.to_h { |entry| [entry[0], entry] }
AUDIT_RETENTION_API_CONTRACTS.each do |operation_id, expected|
  exposure, access, idempotency, event_id = expected
  contract = operation_contracts.fetch(operation_id) { fail_check("audit-retention operation is missing: #{operation_id}") }
  fail_check("audit-retention operation owners drifted for #{operation_id}") unless operation_owner_map.fetch(operation_id) == %w[audit identity/reference]
  fail_check("audit-retention operation exposure drifted for #{operation_id}") unless contract[3] == exposure
  fail_check("audit-retention operation access drifted for #{operation_id}") unless contract[5] == access
  fail_check("audit-retention operation idempotency drifted for #{operation_id}") unless contract[4] == idempotency
  fail_check("audit-retention operation event drifted for #{operation_id}") unless contract[6].include?("exactly `#{event_id}`")
end
expected_owner_goals = rows.filter_map do |row|
  [row[:unit], row[:goal]] if operation_owners.include?(row[:unit])
end.to_h
{
  "audit" => "pkg/audit/.ai/GOAL.md",
  "authorization" => "pkg/authorization/.ai/GOAL.md",
  "capability" => "pkg/capability/.ai/GOAL.md",
  "capability/postgres" => "pkg/capability/.ai/GOAL.md"
}.each do |owner, goal|
  next unless operation_owners.include?(owner)

  fail_check("existing operation owner goal is missing: #{goal}") unless File.file?(File.join(REPOSITORY_ROOT, goal))
  expected_owner_goals[owner] = goal
end
fail_check("operation owner-to-goal closure drifted") unless upstream_manifest.fetch("owner_goal_closure") == expected_owner_goals

rate_section = artifacts.fetch("API_OPERATIONS.md")[
  /^## Normative HTTP rate-policy catalog\n(.*?)(?=^## Core identity, password, and account operations)/m, 1
].to_s
rate_policy_ids = rate_section.lines.filter_map do |line|
  cells = line.split("|").map(&:strip)
  next unless cells.length == 5 && cells[1]&.match?(/\A`rate\.[a-z0-9-]+`\z/)

  cells[1].delete("`")
end.to_set
rate_override_rows = rate_section.lines.filter_map do |line|
  cells = line.split("|").map(&:strip)
  next unless cells.length == 4 && cells[1]&.match?(/\A`rate\.[a-z0-9-]+`\z/)

  [cells[1].delete("`"), cells[2].scan(/`(identity\.[a-z0-9.-]+)`/).flatten]
end
expected_rate_override_policies = Set[
  "rate.signup", "rate.signin", "rate.delivery", "rate.verify",
  "rate.provider-callback", "rate.oauth-token", "rate.device-poll"
]
fail_check("rate override policy closure drifted") unless rate_override_rows.map(&:first).to_set == expected_rate_override_policies
rate_override_pairs = rate_override_rows.flat_map { |policy_id, operation_ids| operation_ids.map { |operation_id| [operation_id, policy_id] } }
fail_check("rate override operation closure drifted") unless rate_override_pairs.length == 46 && rate_override_pairs.map(&:first).uniq.length == 46
rate_overrides = rate_override_pairs.to_h
unknown_rate_override_operations = rate_overrides.keys - operation_ids
fail_check("rate overrides name unknown operations: #{unknown_rate_override_operations}") unless unknown_rate_override_operations.empty?
required_callback_overrides = %w[identity.oauth.callback-form-post identity.sso.oidc-logout-complete]
unless required_callback_overrides.all? { |operation_id| rate_overrides[operation_id] == "rate.provider-callback" }
  fail_check("new provider callback routes lack callback-specific rate policy")
end
expected_rate_policy = lambda do |operation_id, rate_class|
  if operation_id == "identity.session.last-method-record" || rate_class == "internal"
    nil
  else
    rate_overrides.fetch(operation_id, "rate.#{rate_class}")
  end
end
operations.each do |operation_id, _owner_cell, rate_class, _exposure, _idempotency|
  policy_id = expected_rate_policy.call(operation_id, rate_class)
  next unless policy_id

  fail_check("API operation #{operation_id} uses undefined rate policy #{policy_id}") unless rate_policy_ids.include?(policy_id)
end

route_section = artifacts.fetch("API_OPERATIONS.md")[
  /^## Normative route and OpenAPI mapping\n(.*?)(?=^## Normative HTTP rate-policy catalog)/m, 1
].to_s
route_tables = route_section.split(/^Every `protocol` row MUST occur exactly once/, 2)
fail_check("route mapping lacks closed both-exception table") unless route_tables.length == 2
both_exception_text, protocol_and_after = route_tables
both_exceptions = both_exception_text.lines.filter_map do |line|
  next unless line.start_with?("| `identity.")
  cells = line.split("|").map(&:strip)
  fail_check("both route exception has wrong column count: #{line.chomp}") unless cells.length == 6
  [cells[1].delete("`"), cells[2].delete("`"), cells[3].delete("`"), cells[4].delete("`")]
end
protocol_text, middleware_and_after = protocol_and_after.split(/^Every `middleware` row MUST occur exactly once/, 2)
fail_check("route mapping lacks closed middleware table") unless middleware_and_after
protocol_routes = protocol_text.lines.filter_map do |line|
  next unless line.start_with?("| `identity.")
  cells = line.split("|").map(&:strip)
  fail_check("protocol route has wrong column count: #{line.chomp}") unless cells.length == 6
  [cells[1].delete("`"), cells[2].delete("`"), cells[3].delete("`"), cells[4]]
end
middleware_text = middleware_and_after[/\n\| Middleware operation ID.*?(?=\nEvery `direct` row)/m].to_s
middleware_routes = middleware_text.lines.filter_map do |line|
  next unless line.start_with?("| `identity.")
  cells = line.split("|").map(&:strip)
  fail_check("middleware attachment has wrong column count: #{line.chomp}") unless cells.length == 4
  [cells[1].delete("`"), cells[2].scan(/`([^`]+)`/).flatten]
end

operation_by_id = operations.to_h { |entry| [entry[0], entry] }
exposure_ids = operations.group_by { |entry| entry[3] }.transform_values { |entries| entries.map(&:first).to_set }
both_exception_ids = both_exceptions.map(&:first)
fail_check("both route exceptions are duplicated") unless both_exception_ids.uniq == both_exception_ids
invalid_both_exceptions = both_exception_ids.reject { |operation_id| exposure_ids.fetch("both").include?(operation_id) }
fail_check("both route exception disposition drift: #{invalid_both_exceptions}") unless invalid_both_exceptions.empty?
protocol_ids = protocol_routes.map(&:first)
fail_check("protocol route IDs are duplicated") unless protocol_ids.uniq == protocol_ids
missing_protocol = exposure_ids.fetch("protocol") - protocol_ids.to_set
extra_protocol = protocol_ids.to_set - exposure_ids.fetch("protocol")
fail_check("protocol route closure drift: missing=#{missing_protocol.to_a} extra=#{extra_protocol.to_a}") unless missing_protocol.empty? && extra_protocol.empty?
middleware_ids = middleware_routes.map(&:first)
fail_check("middleware attachment IDs are duplicated") unless middleware_ids.uniq == middleware_ids
missing_middleware = exposure_ids.fetch("middleware") - middleware_ids.to_set
extra_middleware = middleware_ids.to_set - exposure_ids.fetch("middleware")
fail_check("middleware closure drift: missing=#{missing_middleware.to_a} extra=#{extra_middleware.to_a}") unless missing_middleware.empty? && extra_middleware.empty?

http_methods = %w[GET POST PUT PATCH DELETE].to_set
route_records = []
operations.select { |entry| entry[3] == "both" }.each do |operation_id, _owner, _rate, _exposure, idempotency|
  exception = both_exceptions.find { |entry| entry[0] == operation_id }
  method = exception ? exception[1] : (idempotency == "read" ? "GET" : "POST")
  path = exception ? exception[2] : "/v1/#{operation_id.delete_prefix('identity.').tr('.', '/')}"
  openapi_owner = exception ? exception[3] : operation_id
  route_records << [operation_id, method, path, openapi_owner]
end
protocol_routes.each do |operation_id, method, path, ownership_cell|
  owner = ownership_cell[/`([^`]+)`/, 1]
  fail_check("protocol route #{operation_id} lacks OpenAPI ownership") unless owner
  route_records << [operation_id, method, path, owner]
end
route_records.each do |operation_id, method, path, openapi_owner|
  fail_check("route #{operation_id} has invalid HTTP method #{method}") unless http_methods.include?(method)
  path_segments = path.delete_prefix("/").split("/", -1)
  valid_path = path.start_with?("/") && path_segments.all? do |segment|
    segment.match?(/\A[A-Za-z0-9._~-]+\z/) || segment.match?(/\A\{[a-z][a-z0-9_]*\}\z/)
  end
  fail_check("route #{operation_id} has invalid path #{path}") unless valid_path
  fail_check("route #{operation_id} has unknown OpenAPI owner #{openapi_owner}") unless operation_by_id.key?(openapi_owner)
  expected_owner = operation_id == "identity.oauth-server.device-token" ? "identity.oauth-server.token" : operation_id
  fail_check("route #{operation_id} has wrong OpenAPI owner #{openapi_owner}") unless openapi_owner == expected_owner
end
expected_http_ids = exposure_ids.fetch("both") | exposure_ids.fetch("protocol")
actual_http_ids = route_records.map(&:first).to_set
fail_check("HTTP operation closure drift") unless actual_http_ids == expected_http_ids
shared_routes = route_records.group_by { |entry| entry.values_at(1, 2) }.select { |_key, entries| entries.length > 1 }
expected_shared_route = [["POST", "/oauth2/token"], Set["identity.oauth-server.token", "identity.oauth-server.device-token"]]
fail_check("HTTP method/path ownership is not closed: #{shared_routes}") unless shared_routes.length == 1 && shared_routes.first[0] == expected_shared_route[0] && shared_routes.first[1].map(&:first).to_set == expected_shared_route[1]
openapi_owners = route_records.reject { |entry| entry[0] == "identity.oauth-server.device-token" }.map { |entry| entry[3] }
fail_check("OpenAPI operation ownership is duplicated") unless openapi_owners.uniq == openapi_owners
expected_openapi_owners = expected_http_ids - Set["identity.oauth-server.device-token"]
fail_check("OpenAPI operation ownership closure drift") unless openapi_owners.to_set == expected_openapi_owners
middleware_routes.each do |middleware_id, targets|
  fail_check("middleware #{middleware_id} has no targets") if targets.empty?
  fail_check("middleware #{middleware_id} has duplicate targets") unless targets.uniq == targets
  invalid_targets = targets.reject { |target| expected_http_ids.include?(target) }
  fail_check("middleware #{middleware_id} has non-HTTP or unknown targets: #{invalid_targets}") unless invalid_targets.empty?
end
direct_ids_in_routes = exposure_ids.fetch("direct") & route_records.map(&:first).to_set
fail_check("direct operations gained routes: #{direct_ids_in_routes.to_a}") unless direct_ids_in_routes.empty?

route_by_id = route_records.to_h { |operation_id, method, path, openapi_id| [operation_id, [method, path, openapi_id]] }
semantic_previous_owners = []
operation_semantic_contracts = operations.to_h do |operation_id, owner_cell, risk_class, exposure, idempotency, access_cell, event_semantics|
  access, csrf_origin = access_cell.split(" / ", 2)
  route = route_by_id[operation_id]
  owners = owner_cell.scan(/`([^`]+)`/).flatten
  owners = semantic_previous_owners if owners.empty? && owner_cell.start_with?("same ")
  semantic_previous_owners = owners
  [operation_id, {
    "id" => operation_id, "owners" => owners, "exposure" => exposure,
    "access" => access, "authorization" => nil, "csrf_origin" => csrf_origin,
    "risk_class" => risk_class, "rate_policy" => expected_rate_policy.call(operation_id, risk_class),
    "idempotency" => idempotency, "event_semantics" => event_semantics,
    "http_method" => route&.fetch(0), "http_path" => route&.fetch(1),
    "openapi_operation_id" => route&.fetch(2)
  }]
end
operation_semantics_document = JSON.parse(artifacts.fetch("OPERATION_SEMANTICS.json"))
operation_semantics_errors(operation_semantics_document, operation_semantic_contracts).each { |error| fail_check(error) }
contract_lines = operation_semantics_document.fetch("operations").map { |row| JSON.generate(canonical_json_value(row)) }
contract_inventory = upstream_manifest.fetch("operation_contract_inventory")
fail_check("operation contract inventory source drifted") unless contract_inventory.fetch("source") == "API_OPERATIONS.md"
fail_check("operation contract inventory fields drifted") unless contract_inventory.fetch("fields") == operation_semantics_document.fetch("closed_fields")
fail_check("operation contract inventory count drifted") unless contract_inventory.fetch("count") == contract_lines.length
fail_check("operation contract inventory digest drifted") unless contract_inventory.fetch("sha256") == canonical_inventory_digest(contract_lines)

%w[owners access csrf_origin risk_class rate_policy idempotency http_path].each do |field|
  mutated = JSON.parse(JSON.generate(operation_semantics_document))
  row = mutated.fetch("operations").find { |candidate| candidate.fetch("exposure") == "both" }
  row[field] = if field == "http_path"
                 "/v1/semantic-drift"
               elsif field == "owners"
                 ["identity/reference"]
               else
                 "valid-vocabulary-substitution"
               end
  errors = operation_semantics_errors(mutated, operation_semantic_contracts)
  fail_check("operation semantic mutation #{field} was accepted") if errors.empty?
end
deleted_operation_semantic = JSON.parse(JSON.generate(operation_semantics_document))
deleted_operation_semantic.fetch("operations").pop
if operation_semantics_errors(deleted_operation_semantic, operation_semantic_contracts).empty?
  fail_check("operation semantic deletion fixture was accepted")
end
extra_operation_semantic = JSON.parse(JSON.generate(operation_semantics_document))
extra_operation_semantic.fetch("operations") << JSON.parse(JSON.generate(extra_operation_semantic.fetch("operations").last)).merge("id" => "identity.z-extra")
if operation_semantics_errors(extra_operation_semantic, operation_semantic_contracts).empty?
  fail_check("operation semantic addition fixture was accepted")
end
legacy_readiness_id = "identity." + "ready"
legacy_readiness_surfaces = [
  artifacts.fetch("API_OPERATIONS.md"), File.read(File.join(ROOT, "END_STATE.md")),
  artifacts.fetch("OPERATION_SEMANTICS.json"), artifacts.fetch("UPSTREAM_SURFACE.json"),
  artifacts.fetch("UPSTREAM_LEAVES.json"), artifacts.fetch("REFERENCE_CONFIGURATION.md")
]
legacy_readiness_pattern = /#{Regexp.escape(legacy_readiness_id)}(?![a-z])/
fail_check("obsolete readiness operation ID remains selected") if legacy_readiness_surfaces.any? { |body| body.match?(legacy_readiness_pattern) }
fail_check("canonical readiness operation ID is missing") unless operation_semantic_contracts.key?("identity.readiness")
override_rate_mutation = JSON.parse(JSON.generate(operation_semantics_document))
override_rate_mutation.fetch("operations").find { |row| row.fetch("id") == "identity.password.signup" }["rate_policy"] = "rate.auth"
if operation_semantics_errors(override_rate_mutation, operation_semantic_contracts).empty?
  fail_check("operation semantic rate override mutation was accepted")
end
shared_parent_rate_mutation = JSON.parse(JSON.generate(operation_semantics_document))
shared_parent_rate_mutation.fetch("operations").find { |row| row.fetch("id") == "identity.session.last-method-record" }["rate_policy"] = "rate.signin"
if operation_semantics_errors(shared_parent_rate_mutation, operation_semantic_contracts).empty?
  fail_check("operation semantic shared-parent rate mutation was accepted")
end

configuration_rows = validate_configuration_rows!(artifacts.fetch("REFERENCE_CONFIGURATION.md"))
fail_check("reference configuration is unexpectedly thin") if configuration_rows.length < 100
identity_policy_set_errors(artifacts.fetch("REFERENCE_CONFIGURATION.md")).each { |error| fail_check(error) }
policy_set_case_mutation = artifacts.fetch("REFERENCE_CONFIGURATION.md").sub("`Authorize =", "`authorize =")
fail_check("identity PolicySet member case mutation was accepted") if identity_policy_set_errors(policy_set_case_mutation).empty?
platform_authority = configuration_row(artifacts.fetch("REFERENCE_CONFIGURATION.md"), "struct:ref.platform.authority")
platform_authority_operations = platform_authority.scan(/identity\.(?:platform\.(?:role|permission-statement)\.(?:create|update|delete)|admin\.user-role-set)/)
expected_platform_authority_operations = ADMINISTRATION_JOURNEY_TRANSITIONS.keys + ["identity.admin.user-role-set"]
unless platform_authority_operations == expected_platform_authority_operations
  fail_check("platform authority configuration operation closure drifted")
end
fail_check("platform authority configuration retains a sole change operation") if platform_authority.include?("change_operation")
configuration_metadata_by_id = configuration_rows.to_h { |row| [row.fetch("row_id"), row] }
{
  "ref.session.cookie.same_site" => %w[Lax Strict],
  "ref.session.cookie.name" => ["__Host-identity_session"],
  "ref.session.cookie.domain" => ["empty"]
}.each do |row_id, expected_enum|
  unless configuration_metadata_by_id.fetch(row_id).fetch("enum") == expected_enum
    fail_check("reference configuration enum metadata drifted for #{row_id}")
  end
end

security_taxonomy = artifacts.fetch("SECURITY_EVENTS.md")[/^## Stable event taxonomy\n(.*?)(?=^## |\z)/m, 1].to_s
security_events = security_taxonomy.lines.grep(/^\|/).join.scan(/`(identity\.[a-z0-9_]+(?:\.[a-z0-9_]+)+)`/).flatten
fail_check("security event taxonomy is unexpectedly thin") if security_events.length < 75
fail_check("security event taxonomy contains duplicates") unless security_events.uniq == security_events
%w[
  identity.platform.create_role identity.platform.update_role
  identity.platform.delete_role identity.platform.assign_role
  identity.platform.revoke_role identity.platform.create_permission_statement
  identity.platform.update_permission_statement identity.platform.delete_permission_statement
  identity.audit_retention.change_policy identity.audit_retention.create_legal_hold
  identity.audit_retention.update_legal_hold identity.audit_retention.release_legal_hold
  identity.audit_retention.delete_records
].each do |action|
  fail_check("security event taxonomy omits required action #{action}") unless security_events.include?(action)
end
break_glass_events = %w[identity.sso.issue_break_glass identity.sso.use_break_glass]
unless break_glass_events.all? { |event_id| security_events.include?(event_id) }
  fail_check("break-glass audit taxonomy is incomplete")
end
break_glass_operation_rows = %w[identity.sso.break-glass.issue identity.sso.break-glass.consume].map do |operation_id|
  operation_contracts.fetch(operation_id) { fail_check("break-glass operation is missing: #{operation_id}") }.fetch(6)
end
unless break_glass_operation_rows[0].include?("exactly the issuance-only `identity.sso.issue_break_glass`") &&
       break_glass_operation_rows[1].include?("exactly the distinct use-only `identity.sso.use_break_glass`")
  fail_check("break-glass operation audit IDs drift from canonical taxonomy")
end
legacy_break_glass_events = %w[identity.sso.break_glass_issued identity.sso.break_glass_used]
if legacy_break_glass_events.any? { |event_id| artifacts.fetch("API_OPERATIONS.md").include?(event_id) || security_events.include?(event_id) }
  fail_check("legacy break-glass audit IDs remain selected")
end
changes_mapping = artifacts.fetch("SECURITY_EVENTS.md").lines.find { |line| line.start_with?("| Changes |") }.to_s
unless changes_mapping.include?("`Redacted=true`") && changes_mapping.include?("both maps MUST be empty")
  fail_check("security event change redaction conflicts with audit.Record semantics")
end
validate_audit_retention_authority!(
  "configuration" => artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  "security_events" => artifacts.fetch("SECURITY_EVENTS.md"),
  "reference_goal" => goal_bodies.fetch("identity/reference"),
  "api_operations" => artifacts.fetch("API_OPERATIONS.md"),
  "end_state" => File.read(File.join(ROOT, "END_STATE.md"))
)

cascade_ids = artifacts.fetch("LIFECYCLE_CASCADES.md")
  .scan(/`(lifecycle\.cascade\.[a-z0-9_]+)`/).flatten.to_set
consumer_rows = artifacts.fetch("LIFECYCLE_CONSUMERS.md").lines.filter_map do |line|
  next unless line.start_with?("| `lifecycle.cascade.")

  cells = line.split("|").map(&:strip)
  fail_check("lifecycle consumer row has wrong column count: #{line.chomp}") unless cells.length == 5
  cascade_id = cells[1].delete("`")
  owner = cells[2][/`([^`]+)`/, 1]
  consumers = cells[3].scan(/`([^`]+)`/).flatten
  fail_check("lifecycle consumer row #{cascade_id} lacks owner or consumers") if owner.nil? || consumers.empty?
  unresolved = ([owner] + consumers).reject { |entry| known.include?(entry) || registered_consumables.include?(entry) }
  fail_check("lifecycle consumer row #{cascade_id} has unknown entries: #{unresolved.join(', ')}") unless unresolved.empty?
  cascade_id
end
fail_check("lifecycle consumer cascade IDs are duplicated") unless consumer_rows.uniq == consumer_rows
manifest_cascades = consumer_rows.to_set
fail_check("lifecycle consumer manifest drift: missing=#{(cascade_ids - manifest_cascades).to_a} extra=#{(manifest_cascades - cascade_ids).to_a}") unless cascade_ids == manifest_cascades

protocol = artifacts.fetch("PROTOCOL_BASELINES.md")
[
  "draft-ietf-oauth-v2-1-15", "RFC 9700", "OpenID Connect Core 1.0",
  "RFC 9728", "SAML 2.0", "RFC 7643", "RFC 7644", "Web Authentication Level 3",
  "RFC 8949", "RFC 9052", "RFC 9053"
].each do |baseline|
  fail_check("protocol baseline missing #{baseline}") unless protocol.include?(baseline)
end

bootstrap = operations.find { |entry| entry[0] == "identity.platform.bootstrap-administrator" }
fail_check("bootstrap administrator must remain direct-only") unless bootstrap && bootstrap[3] == "direct"
fail_check("bootstrap administrator gained an HTTP/OpenAPI mapping") if route_records.any? { |entry| entry[0] == bootstrap[0] }

required_configuration_paths = Set[
  "platform.bootstrap.enabled", "platform.bootstrap.transport", "platform.bootstrap.capability_ttl",
  "oauth_server.scopes", "oauth_server.protected_resource", "oauth_server.protected_resource.resource",
  "oauth_server.protected_resource.supported_scopes",
  "oauth_server.dynamic_registration", "oauth_server.dynamic_registration.allowed_scopes",
  "oauth_server.dynamic_registration.allowed_grant_types", "oauth_server.dynamic_registration.allowed_auth_methods",
  "oauth_server.dynamic_registration.metadata_bytes", "oauth_server.dynamic_registration.initial_access_token_ttl",
  "oauth_server.oidc.pairwise_subject",
  "oauth_server.issuer_path_policy", "oauth_server.device_code_bytes",
  "oauth_server.device_code_encoded_length", "oauth_server.device_code_digest_key",
  "saml.sp_idp_initiated_url", "saml.redirect_signature_algorithm",
  "webauthn.development_localhost_http", "webauthn.public_suffix_snapshot"
]
configuration_paths = configuration_rows.map { |row| row.fetch("row_id").delete_prefix("ref.") }.to_set
missing_configuration_paths = required_configuration_paths - configuration_paths
fail_check("protocol configuration closure drift: #{missing_configuration_paths.to_a.sort}") unless missing_configuration_paths.empty?

combined_protocol_decisions = [
  artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  File.read(File.join(ROOT, "REFERENCE_PROFILE.md")),
  protocol
].join("\n").split.join(" ")
[
  "fixed at exactly 5 seconds per RFC 8628 `slow_down`",
  "issuer path MUST be empty or `/`", "exact `http://localhost`",
  "any received value equal to or less than it is a suspected clone or reset", "saml.sp_idp_initiated_url"
].each do |required|
  fail_check("protocol semantic decision missing: #{required}") unless combined_protocol_decisions.include?(required)
end

rp_transaction_row = artifacts.fetch("REFERENCE_CONFIGURATION.md").lines.find do |line|
  line.start_with?("| `struct:ref.oauth.rp_transaction`")
end.to_s
actual_rp_bindings = rp_transaction_row[/binding = ([^`;]+)/, 1].to_s.split(",")
expected_rp_bindings = %w[
  issuer provider client_id redirect_uri response_mode pkce_commitment nonce requested_scopes
  tenant operation preauth_transaction initiating_subject popup_opener_origin
  popup_channel_id continuation_ref remember_policy
]
fail_check("OAuth RP transaction binding set drifted") unless actual_rp_bindings == expected_rp_bindings

applicability = JSON.parse(File.read(File.join(ROOT, SharedContractApplicability::FILE))).fetch("units")
rfc_contradiction_inputs = {
  mfa_postgres_goal: goal_bodies.fetch("identity/mfa/postgres"),
  oauth_postgres_goal: goal_bodies.fetch("identity/oauth/postgres"),
  identity_postgres_goal: goal_bodies.fetch("identity/postgres"),
  lifecycle_contract: artifacts.fetch("LIFECYCLE_CASCADES.md"),
  applicability: applicability
}
rfc_contradiction_errors(**rfc_contradiction_inputs).each { |error| fail_check(error) }

mfa_without_capability_prerequisite = rfc_contradiction_inputs.fetch(:mfa_postgres_goal).sub(
  ", `capability/postgres`", ""
)
expect_rfc_contradiction_fixture_rejection!(
  "MFA continuation without capability prerequisite",
  "identity/mfa/postgres omits capability/postgres prerequisite"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(mfa_postgres_goal: mfa_without_capability_prerequisite))
end

mfa_without_atomic_completion = without_normalized_phrase(
  rfc_contradiction_inputs.fetch(:mfa_postgres_goal),
  "Continuation completion MUST treat capability validation as read-only and MUST use the enlisted `capability/postgres` participant to reserve the existing proof before applying the MFA transition and invoking the transaction-aware session issuer; it MUST finalize the capability and continuation in the same authoritative unit-of-work commit"
)
expect_rfc_contradiction_fixture_rejection!(
  "MFA continuation without atomic completion",
  "identity/mfa/postgres lacks atomic capability completion ownership"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(mfa_postgres_goal: mfa_without_atomic_completion))
end

mfa_without_capability_reserve = JSON.parse(JSON.generate(applicability))
mfa_without_capability_reserve.fetch("identity/mfa/postgres").fetch("transaction").delete("tx.capability.reserve")
expect_rfc_contradiction_fixture_rejection!(
  "MFA continuation without reserve applicability",
  "identity/mfa/postgres omits capability completion applicability: tx.capability.reserve"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(applicability: mfa_without_capability_reserve))
end

wrong_social_link_owner = rfc_contradiction_inputs.fetch(:lifecycle_contract).sub(
  "| `lifecycle.dimension.social_link` | `social_link` | `identity/postgres` |",
  "| `lifecycle.dimension.social_link` | `social_link` | `identity/oauth/postgres` |"
)
expect_rfc_contradiction_fixture_rejection!(
  "social-link dimension owned by token adapter",
  "social_link lifecycle dimension has the wrong durable owner"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(lifecycle_contract: wrong_social_link_owner))
end

wrong_social_link_applicability = JSON.parse(JSON.generate(applicability))
wrong_social_link_applicability.fetch("identity/postgres").fetch("lifecycle").delete("lifecycle.dimension.social_link")
wrong_social_link_applicability.fetch("identity/oauth/postgres").fetch("lifecycle") << "lifecycle.dimension.social_link"
expect_rfc_contradiction_fixture_rejection!(
  "social-link applicability assigned to token adapter",
  "social_link lifecycle applicability does not match its sole owner"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(applicability: wrong_social_link_applicability))
end

identity_without_social_link_ownership = without_normalized_phrase(
  rfc_contradiction_inputs.fetch(:identity_postgres_goal),
  "Provider-link rows, account-link metadata, provider-subject uniqueness, and the `lifecycle.dimension.social_link` authority version MUST remain owned here"
)
expect_rfc_contradiction_fixture_rejection!(
  "identity store without exact social-link ownership",
  "identity/postgres lacks exact social-link ownership"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(identity_postgres_goal: identity_without_social_link_ownership))
end

oauth_without_social_link_exclusion = without_normalized_phrase(
  rfc_contradiction_inputs.fetch(:oauth_postgres_goal),
  "This adapter MUST NOT own provider-link rows, account-link metadata, provider-subject uniqueness constraints, or the `lifecycle.dimension.social_link` authority version"
)
expect_rfc_contradiction_fixture_rejection!(
  "token adapter claiming ambiguous social-link ownership",
  "identity/oauth/postgres does not exclude social-link ownership"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(oauth_postgres_goal: oauth_without_social_link_exclusion))
end

oauth_without_social_link_enlistment = without_normalized_phrase(
  rfc_contradiction_inputs.fetch(:oauth_postgres_goal),
  "Token refresh, unlink, and token deletion MUST enlist `identity/postgres` to bump the authoritative `social_link` version in the same unit of work as the local token mutation"
)
expect_rfc_contradiction_fixture_rejection!(
  "token mutation without social-link owner enlistment",
  "identity/oauth/postgres omits token-coupled social-link enlistment"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(oauth_postgres_goal: oauth_without_social_link_enlistment))
end

oauth_without_capability_prerequisite = rfc_contradiction_inputs.fetch(:oauth_postgres_goal).sub(
  ", `capability/postgres`", ""
)
expect_rfc_contradiction_fixture_rejection!(
  "OAuth callback without capability prerequisite",
  "identity/oauth/postgres omits capability/postgres prerequisite"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(oauth_postgres_goal: oauth_without_capability_prerequisite))
end

oauth_without_callback_protocol = without_normalized_phrase(
  rfc_contradiction_inputs.fetch(:oauth_postgres_goal),
  "Callback persistence MUST accept only an immutable `CapabilityProof` produced by read-only validation; that proof grants no authority and MUST NOT be already consumed. In the same authoritative unit of work, it MUST enlist `capability/postgres` to reserve the existing proof, apply its bound expiry, version, audience, origin, and risk checks to the identity link/token mutation, and finalize the capability with the command result"
)
expect_rfc_contradiction_fixture_rejection!(
  "OAuth callback without reserve apply finalize",
  "identity/oauth/postgres lacks reserve/apply/finalize callback semantics"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(oauth_postgres_goal: oauth_without_callback_protocol))
end

oauth_with_preconsumed_proof = rfc_contradiction_inputs.fetch(:oauth_postgres_goal) +
  "\nCallback persistence accepts an already verified and atomically consumed authorization-transaction identity.\n"
expect_rfc_contradiction_fixture_rejection!(
  "OAuth callback with pre-consumed proof",
  "identity/oauth/postgres retains contradictory pre-consumed callback semantics"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(oauth_postgres_goal: oauth_with_preconsumed_proof))
end

oauth_without_capability_finalize = JSON.parse(JSON.generate(applicability))
oauth_without_capability_finalize.fetch("identity/oauth/postgres").fetch("transaction").delete("tx.capability.finalize")
expect_rfc_contradiction_fixture_rejection!(
  "OAuth callback without finalize applicability",
  "identity/oauth/postgres omits capability callback applicability: tx.capability.finalize"
) do
  rfc_contradiction_errors(**rfc_contradiction_inputs.merge(applicability: oauth_without_capability_finalize))
end
contradiction_inputs = {
  api_operations: artifacts.fetch("API_OPERATIONS.md"),
  security_events: artifacts.fetch("SECURITY_EVENTS.md"),
  configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  reference_profile: File.read(File.join(ROOT, "REFERENCE_PROFILE.md")),
  end_state: File.read(File.join(ROOT, "END_STATE.md")),
  reference_goal: goal_bodies.fetch("identity/reference"),
  parity: File.read(File.join(ROOT, "BETTER_AUTH_PARITY.md")),
  http_goal: goal_bodies.fetch("identity/http"),
  risk_goal: goal_bodies.fetch("identity/risk"),
  risk_postgres_goal: goal_bodies.fetch("identity/risk/postgres"),
  risk_valkey_goal: goal_bodies.fetch("identity/risk/valkey"),
  applicability: applicability
}
contradiction_resolution_errors(**contradiction_inputs).each { |error| fail_check(error) }

missing_oauth_event = contradiction_inputs.fetch(:security_events).sub(
  ", `identity.oauth_server.deny_device`", ""
)
expect_contradiction_resolution_fixture_rejection!(
  "missing OAuth-server security action", "OAuth-server security taxonomy omits identity.oauth_server.deny_device"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(security_events: missing_oauth_event))
end
missing_oauth_event_mapping = contradiction_inputs.fetch(:api_operations).sub(
  "emits exactly `identity.oauth_server.poll_device`", "records the polling outcome"
)
expect_contradiction_resolution_fixture_rejection!(
  "missing OAuth operation event mapping", "identity.oauth-server.device-token lacks exact security-event mapping"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(api_operations: missing_oauth_event_mapping))
end
missing_oauth_event_applicability = JSON.parse(JSON.generate(applicability))
missing_oauth_event_applicability.fetch("oauth-server/oidc").fetch("security_events").delete(
  "identity.oauth_server.exchange_session"
)
expect_contradiction_resolution_fixture_rejection!(
  "missing OAuth event applicability",
  "oauth-server/oidc omits OAuth-server event applicability: identity.oauth_server.exchange_session"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(applicability: missing_oauth_event_applicability))
end
unqualified_deletion = contradiction_inputs.fetch(:api_operations).sub(
  "provider-only user with a verified email", "provider-only user"
)
expect_contradiction_resolution_fixture_rejection!(
  "unqualified provider deletion proof",
  "provider-only verified-email deletion proof is not closed across deletion contracts"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(api_operations: unqualified_deletion))
end
unbound_remember_policy = contradiction_inputs.fetch(:configuration).sub(
  ",continuation_ref,remember_policy", ",continuation_ref"
)
expect_contradiction_resolution_fixture_rejection!(
  "OAuth RP transaction without remember policy", "OAuth RP transaction omits remember_policy binding"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(configuration: unbound_remember_policy))
end
valkey_rate_authority = contradiction_inputs.fetch(:reference_goal).sub(
  "`rate-limit/valkey` MAY cache only the positive", "`rate-limit/valkey` MAY authoritatively store the"
)
expect_contradiction_resolution_fixture_rejection!(
  "Valkey rate authority", "reference composition does not close PostgreSQL/Valkey rate and idempotency authority"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(reference_goal: valkey_rate_authority))
end
valkey_durable_lockout = without_normalized_phrase(
  contradiction_inputs.fetch(:risk_valkey_goal),
  "MUST NOT create, extend, clear or represent a durable lockout"
) + "\nValkey MAY represent a durable lockout.\n"
expect_contradiction_resolution_fixture_rejection!(
  "Valkey durable lockout", "risk persistence authority does not separate durable PostgreSQL state from ephemeral Valkey windows"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(risk_valkey_goal: valkey_durable_lockout))
end
phone_only_reset = contradiction_inputs.fetch(:api_operations).sub(
  "reset capability plus phone OTP plus independent factor", "reset challenge"
)
expect_contradiction_resolution_fixture_rejection!(
  "phone-only password reset", "phone password-reset completion lacks capability plus independent-factor authority"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(api_operations: phone_only_reset))
end
aggregated_ipv6 = without_normalized_phrase(
  contradiction_inputs.fetch(:api_operations),
  "IPv6 uses the full canonical RFC 5952 address without subnet aggregation"
) + "\nIPv6 clients group by the configured reference `/64`.\n"
expect_contradiction_resolution_fixture_rejection!(
  "IPv6 subnet aggregation", "HTTP rate policy retains IPv6 subnet-aggregation ambiguity"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(api_operations: aggregated_ipv6))
end
selected_profile_aggregation = without_normalized_phrase(
  contradiction_inputs.fetch(:http_goal),
  "future, separately selected profile"
) + "\nThe selected profile uses configured IPv6 subnet dimensions.\n"
expect_contradiction_resolution_fixture_rejection!(
  "selected-profile IPv6 aggregation", "HTTP goal retains selected-profile IPv6 aggregation ambiguity"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(http_goal: selected_profile_aggregation))
end
fixed_window_rate = contradiction_inputs.fetch(:configuration).sub(
  "PostgreSQL atomic token-bucket counters", "PostgreSQL fixed-window counters"
)
expect_contradiction_resolution_fixture_rejection!(
  "fixed-window HTTP rate algorithm", "reference HTTP rate algorithm conflicts with the token-bucket operation contract"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(configuration: fixed_window_rate))
end
conflated_csrf = without_normalized_phrase(
  contradiction_inputs.fetch(:api_operations),
  "request `Origin` validation plus anti-CSRF token/session binding"
) + "\nCSRF uses callback origin allowlisting.\n"
expect_contradiction_resolution_fixture_rejection!(
  "conflated CSRF origin", "API notation conflates CSRF with callback/redirect origin policy"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(api_operations: conflated_csrf))
end
unbound_public_signin = contradiction_inputs.fetch(:api_operations).sub(
  "| `identity.password.signin` | `identity/password` / both | public plus pre-auth transaction /",
  "| `identity.password.signin` | `identity/password` / both | public /"
)
expect_contradiction_resolution_fixture_rejection!(
  "public signin without pre-auth binding",
  "identity.password.signin omits required pre-auth transaction binding"
) do
  contradiction_resolution_errors(**contradiction_inputs.merge(api_operations: unbound_public_signin))
end
phone_contract_inputs = {
  configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  reference_profile: File.read(File.join(ROOT, "REFERENCE_PROFILE.md")),
  api_operations: artifacts.fetch("API_OPERATIONS.md"),
  phone_goal: goal_bodies.fetch("identity/phone"),
  risk_goal: goal_bodies.fetch("identity/risk"),
  lifecycle_consumers: artifacts.fetch("LIFECYCLE_CONSUMERS.md"),
  security_events: artifacts.fetch("SECURITY_EVENTS.md"),
  applicability: applicability,
  inventory: File.read(File.join(ROOT, "INVENTORY.md")),
  dependencies: File.read(File.join(ROOT, "DEPENDENCIES.md"))
}
risk_evidence_contract_inputs = {
  transaction_contract: artifacts.fetch("TRANSACTION_CONTRACT.md"),
  api_operations: artifacts.fetch("API_OPERATIONS.md"),
  risk_goal: phone_contract_inputs.fetch(:risk_goal),
  risk_postgres_goal: goal_bodies.fetch("identity/risk/postgres"),
  phone_goal: phone_contract_inputs.fetch(:phone_goal),
  reference_goal: goal_bodies.fetch("identity/reference"),
  end_state: File.read(File.join(ROOT, "END_STATE.md")),
  applicability: applicability
}
captcha_replay_contract_inputs = {
  transaction_contract: artifacts.fetch("TRANSACTION_CONTRACT.md"),
  configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  risk_goal: goal_bodies.fetch("identity/risk"),
  risk_postgres_goal: goal_bodies.fetch("identity/risk/postgres"),
  captcha_goal: goal_bodies.fetch("identity/risk/captcha"),
  adapter_goals: %w[
    identity/risk/captcha/captchafox identity/risk/captcha/hcaptcha
    identity/risk/captcha/recaptcha identity/risk/captcha/turnstile
  ].to_h { |unit| [unit, goal_bodies.fetch(unit)] },
  applicability: applicability
}
captcha_replay_contract_errors(**captcha_replay_contract_inputs).each { |error| fail_check(error) }

without_captcha_replay_uniqueness = without_normalized_phrase(
  captcha_replay_contract_inputs.fetch(:transaction_contract),
  "A unique constraint on tenant, provider, site ID, profile/configuration version, and replay fingerprint MUST give exactly one issuance command authority"
)
expect_captcha_replay_fixture_rejection!("missing durable uniqueness", "CAPTCHA replay fingerprint is not durably unique") do
  captcha_replay_contract_errors(**captcha_replay_contract_inputs.merge(transaction_contract: without_captcha_replay_uniqueness))
end
caller_selected_captcha_replay = without_normalized_phrase(
  captcha_replay_contract_inputs.fetch(:captcha_goal),
  "No caller or provider adapter may supply, override, or select the authoritative replay fingerprint"
)
expect_captcha_replay_fixture_rejection!("caller-selected fingerprint", "CAPTCHA core permits caller-selected replay identity") do
  captcha_replay_contract_errors(**captcha_replay_contract_inputs.merge(captcha_goal: caller_selected_captcha_replay))
end
onetap_without_captcha = JSON.parse(JSON.generate(captcha_replay_contract_inputs.fetch(:applicability)))
onetap_without_captcha.fetch("identity/oauth/onetap").fetch("transaction").delete("tx.captcha.reserve")
expect_captcha_replay_fixture_rejection!("One Tap without CAPTCHA reservation", "identity/oauth/onetap omits CAPTCHA transaction applicability") do
  captcha_replay_contract_errors(**captcha_replay_contract_inputs.merge(applicability: onetap_without_captcha))
end

scim_bulk_graph_inputs = {
  transaction_contract: artifacts.fetch("TRANSACTION_CONTRACT.md"),
  scim_goal: goal_bodies.fetch("scim")
}
scim_bulk_graph_contract_errors(**scim_bulk_graph_inputs).each { |error| fail_check(error) }
scim_without_atomic_scc = without_normalized_phrase(
  scim_bulk_graph_inputs.fetch(:transaction_contract),
  "Its child results and SCC checkpoint commit atomically; any member failure rolls back the SCC"
)
expect_scim_bulk_graph_fixture_rejection!("partial circular component", "SCIM Bulk circular component can partially commit") do
  scim_bulk_graph_contract_errors(**scim_bulk_graph_inputs.merge(transaction_contract: scim_without_atomic_scc))
end
scim_rfc_contract_inputs = {
  api_operations: artifacts.fetch("API_OPERATIONS.md"),
  protocol: artifacts.fetch("PROTOCOL_BASELINES.md"),
  configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  transaction_contract: artifacts.fetch("TRANSACTION_CONTRACT.md"),
  scim_goal: goal_bodies.fetch("scim"),
  scim_postgres_goal: goal_bodies.fetch("scim/postgres"),
  applicability: applicability,
  public_contract_fragment: JSON.parse(File.read(File.join(ROOT, "fragments", "public_contracts_org_scim.json"))),
  acceptance_profile: IdentityPlatformAcceptance.profiles.fetch("scim-rfc-conformance-report")
}
scim_rfc_contract_errors(**scim_rfc_contract_inputs).each { |error| fail_check(error) }

scim_without_generic_search = scim_rfc_contract_inputs.fetch(:api_operations).sub(
  "| `identity.scim.search` | `POST` | `/scim/v2/.search` |", "| removed generic search |"
)
expect_scim_rfc_fixture_rejection!("missing generic search route", "SCIM generic POST search route is missing") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(api_operations: scim_without_generic_search))
end
scim_without_list_response = scim_rfc_contract_inputs.fetch(:protocol).sub(
  "The Schemas and ResourceTypes collection endpoints\nalso return RFC ListResponse messages; bare arrays are not conforming responses", "The Schemas and ResourceTypes collection endpoints return collections"
)
expect_scim_rfc_fixture_rejection!("bare discovery arrays", "SCIM discovery collections permit bare arrays") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(protocol: scim_without_list_response))
end
scim_required_type = scim_rfc_contract_inputs.fetch(:protocol).sub("`scimType` is OPTIONAL", "`scimType` is REQUIRED")
expect_scim_rfc_fixture_rejection!("required scimType", "SCIM scimType optionality is not exact") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(protocol: scim_required_type))
end
scim_unique_external_id = scim_rfc_contract_inputs.fetch(:scim_postgres_goal).sub(
  "Equal values MAY identify multiple resources and MUST NOT be\n  rejected by a unique constraint",
  "Equal values are rejected by a unique constraint"
)
expect_scim_rfc_fixture_rejection!("unique externalId", "SCIM PostgreSQL externalId constraint is not non-unique") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(scim_postgres_goal: scim_unique_external_id))
end
scim_unbounded_fail_on_errors = scim_rfc_contract_inputs.fetch(:configuration).sub("exact request maximum 100", "request maximum follows operation count")
expect_scim_rfc_fixture_rejection!("unbounded failOnErrors", "SCIM failOnErrors maximum is not exactly 100") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(configuration: scim_unbounded_fail_on_errors))
end
scim_without_headerless_replay = scim_rfc_contract_inputs.fetch(:transaction_contract).sub("A headerless retry with that identical lookup MUST", "A headerless retry MAY")
expect_scim_rfc_fixture_rejection!("headerless DELETE without replay", "SCIM headerless DELETE replay is not server-owned") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(transaction_contract: scim_without_headerless_replay))
end
scim_key_authoritative_delete = scim_rfc_contract_inputs.fetch(:configuration).sub(
  "with no extension key or with any newly supplied extension key",
  "only when the same extension key is supplied"
)
expect_scim_rfc_fixture_rejection!("extension-key-authoritative DELETE replay", "SCIM DELETE configuration makes extension keys authoritative") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(configuration: scim_key_authoritative_delete))
end
scim_misowned_bulk_events = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:applicability)))
scim_misowned_bulk_events.fetch("scim").fetch("security_events").delete("identity.scim.bulk_admit")
scim_misowned_bulk_events.fetch("scim/organization")["security_events"] = ["identity.scim.bulk_admit"]
expect_scim_rfc_fixture_rejection!("organization-owned Bulk audit", "core SCIM does not own every Bulk audit action") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(applicability: scim_misowned_bulk_events))
end
scim_response_input = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:public_contract_fragment)))
scim_response_input.fetch("units").find { |row| row.fetch("package_name") == "scim" }.fetch("types").find { |row| row.fetch("name") == "WritableResource" }.fetch("fields") << {"name" => "ID"}
expect_scim_rfc_fixture_rejection!("server-generated writable ID", "SCIM writable resource includes server-generated fields") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(public_contract_fragment: scim_response_input))
end
scim_bulk_without_method = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:public_contract_fragment)))
scim_bulk_without_method.fetch("units").find { |row| row.fetch("package_name") == "scim" }.fetch("types").find { |row| row.fetch("name") == "BulkOperationResult" }.fetch("fields").delete_if { |row| row.fetch("name") == "Method" }
expect_scim_rfc_fixture_rejection!("BulkResponse without method", "SCIM BulkResponse operation omits method or conditional bulkId") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(public_contract_fragment: scim_bulk_without_method))
end
scim_revoke_without_command = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:public_contract_fragment)))
scim_revoke_without_command.fetch("operations").find { |row| row.fetch("id") == "identity.scim.connection-token-revoke" }.dig("request", "fields").delete_if { |row| row.fetch("name") == "Command" }
expect_scim_rfc_fixture_rejection!("token revoke without Command", "SCIM token revocation request lacks durable Command identity") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(public_contract_fragment: scim_revoke_without_command))
end
scim_revoke_type_without_command = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:public_contract_fragment)))
scim_revoke_type_without_command.fetch("units").find { |row| row.fetch("package_name") == "scim" }.fetch("types").find { |row| row.fetch("name") == "ConnectionTokenRevokeCommand" }.fetch("fields").delete_if { |row| row.fetch("name") == "Command" }
expect_scim_rfc_fixture_rejection!("token revoke command type without Command", "SCIM token revocation command lacks durable Command identity") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(public_contract_fragment: scim_revoke_type_without_command))
end
scim_revoke_optional_command = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:public_contract_fragment)))
scim_revoke_optional_command.fetch("operations").find { |row| row.fetch("id") == "identity.scim.connection-token-revoke" }.dig("request", "fields").find { |row| row.fetch("name") == "Command" }["required"] = false
expect_scim_rfc_fixture_rejection!("token revoke with optional Command", "SCIM token revocation request lacks durable Command identity") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(public_contract_fragment: scim_revoke_optional_command))
end
scim_revoke_type_optional_command = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:public_contract_fragment)))
scim_revoke_type_optional_command.fetch("units").find { |row| row.fetch("package_name") == "scim" }.fetch("types").find { |row| row.fetch("name") == "ConnectionTokenRevokeCommand" }.fetch("fields").find { |row| row.fetch("name") == "Command" }["required"] = false
expect_scim_rfc_fixture_rejection!("token revoke command type with optional Command", "SCIM token revocation command lacks durable Command identity") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(public_contract_fragment: scim_revoke_type_optional_command))
end
scim_incomplete_acceptance = JSON.parse(JSON.generate(scim_rfc_contract_inputs.fetch(:acceptance_profile)))
scim_incomplete_acceptance.fetch("operation_ids").delete("identity.scim.group-search")
expect_scim_rfc_fixture_rejection!("incomplete conformance operation set", "SCIM conformance acceptance does not cover every advertised protocol operation") do
  scim_rfc_contract_errors(**scim_rfc_contract_inputs.merge(acceptance_profile: scim_incomplete_acceptance))
end
risk_evidence_contract_errors(**risk_evidence_contract_inputs).each { |error| fail_check(error) }
reference_without_phone_composition = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:reference_goal),
  "For `identity.phone.password-reset-complete`, the reference composition MUST enlist `identity/risk/postgres`, `identity/otp/postgres`, `capability/postgres`, `identity/password/postgres`, and `identity/session/postgres` in one coordinator unit of work before the reservation transaction's first write"
)
expect_risk_evidence_fixture_rejection!("reference without concrete phone composition", "identity/reference lacks exact phone recovery composition") do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(reference_goal: reference_without_phone_composition))
end
reference_without_phone_initiation_composition = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:reference_goal),
  "For `identity.phone.password-reset-request`, the reference composition MUST enlist `identity/postgres`, `identity/risk/postgres`, `identity/otp/postgres`, `capability/postgres`, `audit/postgres`, and `outbox/postgres` in one coordinator unit of work before the reservation transaction's first write"
)
expect_risk_evidence_fixture_rejection!(
  "reference without concrete phone initiation composition",
  "identity/reference lacks exact phone recovery initiation composition"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(reference_goal: reference_without_phone_initiation_composition))
end
reference_without_uow_reserve = JSON.parse(JSON.generate(applicability))
reference_without_uow_reserve.fetch("identity/reference").fetch("transaction").delete("tx.uow.reserve")
expect_risk_evidence_fixture_rejection!(
  "reference coordinator without UoW reserve applicability",
  "identity/reference phone recovery transaction applicability drifted"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(applicability: reference_without_uow_reserve))
end
phone_with_concrete_adapter = risk_evidence_contract_inputs.fetch(:phone_goal) + "\nThe package imports `identity/otp/postgres`.\n"
expect_risk_evidence_fixture_rejection!("phone with concrete adapter", "identity/phone names reference-only concrete adapters: identity/otp/postgres") do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(phone_goal: phone_with_concrete_adapter))
end
phone_with_unrelated_concrete_adapter = risk_evidence_contract_inputs.fetch(:phone_goal) + "\nThe package imports `identity/postgres`.\n"
expect_risk_evidence_fixture_rejection!("phone with unrelated concrete adapter", "identity/phone names reference-only concrete adapters: identity/postgres") do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(phone_goal: phone_with_unrelated_concrete_adapter))
end
reference_with_sixth_participant = risk_evidence_contract_inputs.fetch(:reference_goal).sub(
  "`identity/password/postgres`, and `identity/session/postgres` in one coordinator",
  "`identity/password/postgres`, `identity/session/postgres`, and `audit/postgres` in one coordinator"
)
expect_risk_evidence_fixture_rejection!("reference with sixth phone participant", "identity/reference phone recovery participant set is not exact") do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(reference_goal: reference_with_sixth_participant))
end
otp_contract_inputs = {
  transaction_contract: artifacts.fetch("TRANSACTION_CONTRACT.md"),
  otp_goal: goal_bodies.fetch("identity/otp"),
  otp_postgres_goal: goal_bodies.fetch("identity/otp/postgres"),
  workflow_goals: {
    "identity/email" => goal_bodies.fetch("identity/email"),
    "identity/password" => goal_bodies.fetch("identity/password"),
    "identity/phone" => phone_contract_inputs.fetch(:phone_goal),
    "identity/mfa" => goal_bodies.fetch("identity/mfa")
  },
  end_state: risk_evidence_contract_inputs.fetch(:end_state),
  applicability: applicability
}
otp_contract_errors(**otp_contract_inputs).each { |error| fail_check(error) }

otp_contract_mutations = {
  "missing purpose binding" => [
    "The binding MUST include tenant, purpose, subject or channel target, challenge ID, workflow target, issued/expiry database time, attempt budget, keyed-code-digest version, and issuance fingerprint",
    "OTP participant omits exact purpose binding"
  ],
  "replacement revokes active reservation" => [
    "A replacement MAY transition an earlier `issued` row to `revoked` in the same issue transaction, but MUST NOT replace or revoke a `reserved` row without authoritative recovery of its owning command",
    "OTP replacement can invalidate an active reservation"
  ],
  "reservation without generation binding" => [
    "then bind consuming command ID, command fingerprint, reservation generation, and target versions",
    "OTP reservation omits command generation binding"
  ],
  "two reservation winners" => [
    "Two commands MAY perform a non-authoritative digest precheck, but exactly one command MAY transition the same `issued` row to `reserved`; every other command receives the same non-enumerating denial",
    "OTP reservation lacks one-winner concurrency"
  ],
  "non-idempotent same-command replay" => [
    "The same command, fingerprint, and live generation replay the stable reservation without decrementing attempts or rerunning the workflow",
    "OTP same-command replay is not idempotent"
  ],
  "takeover without generation CAS" => [
    "Expired-owner takeover MUST CAS-rebind the OTP reservation from the exact prior generation to the new command generation in the coordinator reservation transaction with every other reserved one-time participant",
    "OTP takeover lacks generation CAS"
  ],
  "split apply finalize" => [
    "`tx.otp.apply` MUST recheck reservation generation, purpose and all bound versions inside the domain transaction; `tx.otp.finalize` MUST transition `reserved` to `finalized` in the same commit as the owning mutation, session issuance or invalidation, outbox/audit, and command result",
    "OTP apply/finalize is not atomic with its workflow"
  ],
  "reusable release" => [
    "A retryable rollback retains `reserved` only for the same live command generation; authoritative non-commit MAY use `tx.otp.release`, which is terminal and requires a newly issued OTP",
    "OTP rollback/release can permit replay"
  ],
  "unknown releases reservation" => [
    "An ambiguous commit MUST leave OTP `reserved`, return `Unknown`, and use `tx.otp.recover`; timeout, lease loss, challenge expiry, or cleanup MUST NOT release it",
    "OTP unknown recovery is not fail-closed"
  ],
  "cleanup without terminal tombstone" => [
    "After the later of original OTP expiry and `command.result_retention`, cleanup MAY crypto-shred terminal payload/linkage but MUST retain a tenant/purpose/keyed-digest/key-version/original-expiry/terminal-state tombstone with no time-based deletion until every referenced digest key is retired and no code can validate before lookup",
    "OTP terminal cleanup can reopen replay"
  ]
}
otp_contract_mutations.each do |label, (removed, expected_error)|
  mutated_contract = without_normalized_phrase(otp_contract_inputs.fetch(:transaction_contract), removed)
  expect_otp_fixture_rejection!(label, expected_error) do
    otp_contract_errors(**otp_contract_inputs.merge(transaction_contract: mutated_contract))
  end
end

%w[identity/otp identity/otp/postgres identity/phone identity/mfa identity/email identity/password].each do |unit|
  mutated_applicability = JSON.parse(JSON.generate(applicability))
  mutated_applicability.fetch(unit).fetch("transaction").delete("tx.otp.reserve")
  expect_otp_fixture_rejection!("#{unit} without reserve applicability", "#{unit} OTP applicability drifted") do
    otp_contract_errors(**otp_contract_inputs.merge(applicability: mutated_applicability))
  end
end

otp_contract_inputs.fetch(:workflow_goals).each do |unit, goal|
  workflow_phrases = {
    "identity/email" => "When handling `identity.otp.email-verify`, `identity.otp.email-change-confirm`, or the optional current-address OTP branch of `identity.otp.email-change-request`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the public `identity/otp` contributor contract in the same coordinator unit of work as its owning mutation. The core MUST NOT import, require, or name a concrete OTP persistence adapter; reference composition selects that adapter. Non-OTP email operations MUST NOT enlist an OTP participant",
    "identity/password" => "When handling `identity.otp.password-reset` or `identity.phone.password-reset-complete`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the public `identity/otp` contributor contract in the same coordinator unit of work as its owning mutation. The core MUST NOT import, require, or name a concrete OTP persistence adapter; reference composition selects that adapter. Signup, signin, password change, and capability-only reset MUST NOT enlist an OTP participant",
    "identity/phone" => "When handling `identity.phone.verify`, `identity.phone.signin`, `identity.phone.update`, or `identity.phone.password-reset-complete`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the injected OTP transaction contributor in the same coordinator unit of work as its owning mutation. Non-consuming initiation/removal operations MUST NOT enlist an OTP participant",
    "identity/mfa" => "When handling `identity.mfa.otp-verify`, this workflow MUST reserve/apply/finalize the purpose-bound OTP through the public `identity/otp` contributor contract in the same coordinator unit of work as its owning mutation. The core MUST NOT import, require, or name a concrete OTP persistence adapter; reference composition selects that adapter. Other MFA methods and OTP-send initiation MUST NOT enlist an OTP consumption participant"
  }
  mutated_workflow_goals = otp_contract_inputs.fetch(:workflow_goals).dup
  mutated_workflow_goals[unit] = without_normalized_phrase(
    goal, workflow_phrases.fetch(unit)
  )
  expect_otp_fixture_rejection!("#{unit} goal without scoped atomic OTP ownership", "#{unit} lacks scoped atomic OTP workflow ownership") do
    otp_contract_errors(**otp_contract_inputs.merge(workflow_goals: mutated_workflow_goals))
  end
end

otp_goal_without_protocol = without_normalized_phrase(
  otp_contract_inputs.fetch(:otp_goal),
  "Every consuming workflow MUST treat OTP precheck as non-authoritative and use the durable issue/attempt/reserve/apply/finalize/release/recover protocol"
)
expect_otp_fixture_rejection!("OTP core without participant ownership", "identity/otp lacks durable participant ownership") do
  otp_contract_errors(**otp_contract_inputs.merge(otp_goal: otp_goal_without_protocol))
end

otp_postgres_without_states = without_normalized_phrase(
  otp_contract_inputs.fetch(:otp_postgres_goal),
  "The adapter owns the durable OTP participant state machine: `issued`, `reserved`, `finalized`, `released`, `expired`, `revoked`, and `exhausted`"
)
expect_otp_fixture_rejection!("OTP store without closed states", "identity/otp/postgres lacks exact durable OTP ownership") do
  otp_contract_errors(**otp_contract_inputs.merge(otp_postgres_goal: otp_postgres_without_states))
end

otp_end_state_without_atomicity = without_normalized_phrase(
  otp_contract_inputs.fetch(:end_state),
  "Every OTP-consuming signin, email verification/change, password reset, phone recovery, and MFA completion MUST reserve and finalize its purpose-bound OTP through the same coordinator unit of work as the owning mutation and session effect"
)
expect_otp_fixture_rejection!("END_STATE without OTP atomicity", "END_STATE omits atomic OTP workflow closure") do
  otp_contract_errors(**otp_contract_inputs.merge(end_state: otp_end_state_without_atomicity))
end

passkey_schema_path = File.join(ROOT, "acceptance/v1/schemas/passkey-browser-ceremony-report.schema.json")
captcha_schema_path = File.join(ROOT, "acceptance/v1/schemas/captcha-four-provider-report.schema.json")
privacy_export_schema_path = File.join(ROOT, "acceptance/v1/schemas/privacy-export-lifecycle-report.schema.json")
otp_schema_path = File.join(ROOT, "acceptance/v1/schemas/otp-reservation-report.schema.json")
cross_cutting_inputs = {
  transaction_contract: artifacts.fetch("TRANSACTION_CONTRACT.md"),
  configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  reference_profile: File.read(File.join(ROOT, "REFERENCE_PROFILE.md")),
  lifecycle_contract: artifacts.fetch("LIFECYCLE_CASCADES.md"),
  api_operations: artifacts.fetch("API_OPERATIONS.md"),
  security_events: artifacts.fetch("SECURITY_EVENTS.md"),
  passkey_goal: goal_bodies.fetch("passkey"),
  applicability: applicability,
  acceptance_catalog: JSON.parse(artifacts.fetch("ACCEPTANCE_ARTIFACTS.json")),
  passkey_schema: JSON.parse(File.read(passkey_schema_path)),
  captcha_schema: JSON.parse(File.read(captcha_schema_path)),
  privacy_export_schema: JSON.parse(File.read(privacy_export_schema_path)),
  otp_schema: JSON.parse(File.read(otp_schema_path))
}
cross_cutting_remediation_errors(**cross_cutting_inputs).each { |error| fail_check(error) }

privacy_snapshot_restored = cross_cutting_inputs.fetch(:configuration).sub(
  "postgres_snapshot = append_only_versioned_projection_or_transaction_staged_immutable_fragment",
  "postgres_snapshot = one_exported_repeatable_read"
)
expect_cross_cutting_fixture_rejection!("exported PostgreSQL snapshot", "privacy export retains a non-restart-safe exported snapshot") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(configuration: privacy_snapshot_restored))
end
privacy_restart_rule_removed = without_normalized_phrase(
  cross_cutting_inputs.fetch(:lifecycle_contract),
  "A restartable worker MUST NOT retain or depend on a long-lived exported PostgreSQL snapshot"
)
expect_cross_cutting_fixture_rejection!("privacy restart rule removed", "privacy-export lifecycle authority is not restart-safe") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(lifecycle_contract: privacy_restart_rule_removed))
end
privacy_acceptance_without_restart = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:acceptance_catalog)))
privacy_acceptance = privacy_acceptance_without_restart.fetch("artifacts").find { |row| row.fetch("artifact_id") == "privacy-export-lifecycle-report" }
privacy_acceptance.fetch("artifact_evidence_fields").delete("worker_restart_count")
expect_cross_cutting_fixture_rejection!("privacy acceptance restart evidence removed", "privacy-export acceptance omits restart-safe evidence") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(acceptance_catalog: privacy_acceptance_without_restart))
end
privacy_acceptance_without_behavior = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:acceptance_catalog)))
privacy_acceptance_without_behavior.fetch("artifacts").find { |row| row.fetch("artifact_id") == "privacy-export-lifecycle-report" }
  .fetch("required_observations").each do |row|
    row["expected_outcome"] = row.fetch("expected_outcome").gsub(
      "restart reconstructs every contributor at the recorded checkpoint without a long-lived exported PostgreSQL snapshot",
      "restart continues the job"
    )
  end
expect_cross_cutting_fixture_rejection!("privacy acceptance restart behavior removed", "privacy-export acceptance omits restart behavior") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(acceptance_catalog: privacy_acceptance_without_behavior))
end
privacy_schema_without_checkpoint = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:privacy_export_schema)))
privacy_schema_without_checkpoint.fetch("$defs").fetch("artifact_evidence").fetch("properties").delete("contributor_checkpoint_count")
expect_cross_cutting_fixture_rejection!("privacy schema checkpoint evidence removed", "privacy-export acceptance schema omits restart-safe evidence") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(privacy_export_schema: privacy_schema_without_checkpoint))
end
privacy_schema_without_restart_equality = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:privacy_export_schema)))
privacy_schema_without_restart_equality.fetch("$defs").fetch("artifact_evidence").fetch("x-semantic-rules").delete_if do |rule|
  rule["kind"] == "equal" && rule["left"] == "restart_resumed_contributor_count"
end
expect_cross_cutting_fixture_rejection!("privacy restart equality removed", "privacy-export acceptance semantic rules are incomplete") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(privacy_export_schema: privacy_schema_without_restart_equality))
end

{
  "OTP attempt identity removed" => [
    "`tx.otp.attempt` MUST use a server-issued attempt ID bound to the tenant, purpose, challenge ID, consuming command ID, and canonical command fingerprint",
    "OTP wrong-code attempt lacks server-issued retry identity"
  ],
  "OTP denial atomicity removed" => [
    "It MUST lock the command row and OTP row, verify the server-issued attempt ID and canonical command fingerprint, increment the durable attempt counter exactly once, transition to `exhausted` when the budget is reached, and store the stable `aborted` command result in the same commit",
    "OTP wrong-code denial does not atomically persist attempt and result"
  ],
  "OTP denial forced through normal rollback" => [
    "The wrong-code denial transaction is a narrow pre-reservation exception to `tx.uow.reserve`",
    "OTP wrong-code denial is not a narrow pre-reservation exception"
  ],
  "OTP denial ambiguity removed" => [
    "An ambiguous wrong-code denial commit MUST return `Unknown` and reconcile the same command and attempt ID on the primary before any retry",
    "OTP wrong-code denial lacks ambiguous-commit recovery"
  ]
}.each do |label, (phrase, expected_error)|
  mutated = without_normalized_phrase(cross_cutting_inputs.fetch(:transaction_contract), phrase)
  expect_cross_cutting_fixture_rejection!(label, expected_error) do
    cross_cutting_remediation_errors(**cross_cutting_inputs.merge(transaction_contract: mutated))
  end
end
otp_acceptance_without_attempt = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:acceptance_catalog)))
otp_acceptance_without_attempt.fetch("artifacts").find { |row| row.fetch("artifact_id") == "otp-reservation-report" }
  .fetch("artifact_evidence_fields").delete("attempt_id")
expect_cross_cutting_fixture_rejection!("OTP acceptance attempt evidence removed", "OTP acceptance omits retry-safe denial evidence") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(acceptance_catalog: otp_acceptance_without_attempt))
end
otp_schema_without_attempt_equality = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:otp_schema)))
otp_schema_without_attempt_equality.fetch("$defs").fetch("artifact_evidence").fetch("x-semantic-rules").delete_if do |rule|
  rule["kind"] == "equal" && rule["left"] == "aborted_result_count"
end
expect_cross_cutting_fixture_rejection!("OTP attempt equality removed", "OTP acceptance semantic rules are incomplete") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(otp_schema: otp_schema_without_attempt_equality))
end

captcha_preauth_only = without_normalized_phrase(
  cross_cutting_inputs.fetch(:transaction_contract),
  "a flow-context variant of pre-auth transaction for unauthenticated flows or authenticated subject/session or administrator actor context for authenticated or administrative flows",
)
captcha_preauth_only << "\nThe durable evidence row binds a pre-auth transaction for every flow.\n"
expect_cross_cutting_fixture_rejection!("CAPTCHA pre-auth-only binding", "CAPTCHA evidence mandates the wrong flow binding") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(transaction_contract: captcha_preauth_only))
end
captcha_without_authenticated_recheck = without_normalized_phrase(
  cross_cutting_inputs.fetch(:transaction_contract),
  "`tx.captcha.apply` MUST recheck command/generation ownership, PostgreSQL expiry, exact action, subject or anonymous flow, the applicable pre-auth or authenticated subject/session or administrator actor context"
)
expect_cross_cutting_fixture_rejection!("CAPTCHA authenticated recheck removed", "CAPTCHA apply omits flow-context recheck") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(transaction_contract: captcha_without_authenticated_recheck))
end
captcha_with_durable_roles = JSON.parse(JSON.generate(applicability))
captcha_role_fixture = captcha_with_durable_roles.fetch("identity/risk/captcha").fetch("transaction")
captcha_role_fixture.concat(%w[tx.captcha.apply tx.captcha.finalize tx.captcha.reserve])
captcha_role_fixture.replace(captcha_role_fixture.uniq.sort)
expect_cross_cutting_fixture_rejection!("stateless CAPTCHA durable ownership", "stateless CAPTCHA verifier owns durable transaction roles") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(applicability: captcha_with_durable_roles))
end
captcha_acceptance_without_flow = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:acceptance_catalog)))
captcha_acceptance = captcha_acceptance_without_flow.fetch("artifacts").find { |row| row.fetch("artifact_id") == "captcha-four-provider-report" }
captcha_acceptance.fetch("artifact_evidence_fields").delete("protected_target_results")
expect_cross_cutting_fixture_rejection!("CAPTCHA acceptance target results removed", "CAPTCHA acceptance omits exhaustive target and durable-owner evidence") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(acceptance_catalog: captcha_acceptance_without_flow))
end
captcha_acceptance_without_admin = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:acceptance_catalog)))
captcha_acceptance_without_admin.fetch("artifacts").find { |row| row.fetch("artifact_id") == "captcha-four-provider-report" }
  .fetch("required_observations").each do |row|
    row["expected_outcome"] = row.fetch("expected_outcome").gsub(
      "every configured CAPTCHA target from the canonical API attachment table has one middleware-attached result with its exact permitted flow contexts and attributable evidence",
      "configured CAPTCHA targets are verified"
    )
  end
expect_cross_cutting_fixture_rejection!("CAPTCHA exhaustive target outcome removed", "CAPTCHA acceptance omits exhaustive target and ownership cases") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(acceptance_catalog: captcha_acceptance_without_admin))
end
captcha_schema_without_owner = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:captcha_schema)))
captcha_schema_without_owner.fetch("$defs").fetch("artifact_evidence").fetch("properties").delete("durable_owner_digest")
expect_cross_cutting_fixture_rejection!("CAPTCHA schema owner evidence removed", "CAPTCHA acceptance schema omits exhaustive target and durable-owner evidence") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(captcha_schema: captcha_schema_without_owner))
end
captcha_schema_without_context_count = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:captcha_schema)))
captcha_schema_without_context_count.fetch("$defs").fetch("artifact_evidence").fetch("x-semantic-rules").delete_if do |rule|
  rule["kind"] == "equal" && rule["left"] == "protected_target_count" && rule["right"] == "middleware_attached_target_count"
end
expect_cross_cutting_fixture_rejection!("CAPTCHA middleware attachment equality removed", "CAPTCHA acceptance semantic rules are incomplete") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(captcha_schema: captcha_schema_without_context_count))
end
captcha_schema_without_target = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:captcha_schema)))
captcha_schema_without_target.fetch("$defs").fetch("artifact_evidence").fetch("properties").fetch("protected_target_results")
  .fetch("items").fetch("oneOf").pop
expect_cross_cutting_fixture_rejection!("CAPTCHA target result removed", "CAPTCHA acceptance schema target inventory is not exhaustive") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(captcha_schema: captcha_schema_without_target))
end

%w[identity.passkey.create_credential identity.passkey.mark_compromised].each do |action|
  taxonomy_without_action = cross_cutting_inputs.fetch(:security_events).gsub("`#{action}`, ", "").gsub(", `#{action}`", "")
  expect_cross_cutting_fixture_rejection!("taxonomy without #{action}", "passkey security taxonomy omits #{action}") do
    cross_cutting_remediation_errors(**cross_cutting_inputs.merge(security_events: taxonomy_without_action))
  end
  applicability_without_action = JSON.parse(JSON.generate(applicability))
  applicability_without_action.fetch("passkey").fetch("security_events").delete(action)
  expect_cross_cutting_fixture_rejection!("applicability without #{action}", "passkey applicability omits security actions: #{action}") do
    cross_cutting_remediation_errors(**cross_cutting_inputs.merge(applicability: applicability_without_action))
  end
end

register_mapping_removed = cross_cutting_inputs.fetch(:api_operations).sub(
  "emits exactly `identity.passkey.create_credential` when credential creation commits", "records credential creation"
)
expect_cross_cutting_fixture_rejection!("passkey creation API mapping removed", "passkey registration operation omits exact creation action") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(api_operations: register_mapping_removed))
end
compromise_mapping_removed = cross_cutting_inputs.fetch(:api_operations).sub(
  "emits exactly `identity.passkey.mark_compromised` when clone or counter evidence durably marks the credential compromised", "records suspected clone evidence"
)
expect_cross_cutting_fixture_rejection!("passkey compromise API mapping removed", "passkey sign-in operation omits exact compromise action") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(api_operations: compromise_mapping_removed))
end
acceptance_without_event_field = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:acceptance_catalog)))
acceptance_without_event_field.fetch("artifacts").find { |row| row.fetch("artifact_id") == "passkey-browser-ceremony-report" }
  .fetch("artifact_evidence_fields").delete("creation_event_count")
expect_cross_cutting_fixture_rejection!("passkey acceptance evidence removed", "passkey acceptance evidence omits security actions") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(acceptance_catalog: acceptance_without_event_field))
end
schema_without_event_field = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:passkey_schema)))
schema_without_event_field.fetch("$defs").fetch("artifact_evidence").fetch("properties").delete("compromise_event_count")
expect_cross_cutting_fixture_rejection!("passkey acceptance schema evidence removed", "passkey acceptance schema omits security-action evidence") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(passkey_schema: schema_without_event_field))
end
passkey_schema_without_failure_zero = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:passkey_schema)))
passkey_schema_without_failure_zero.fetch("$defs").fetch("artifact_evidence").fetch("x-semantic-rules").each do |rule|
  rule.fetch("fields", []).delete("ordinary_failure_compromise_event_count") if rule["kind"] == "zero"
end
expect_cross_cutting_fixture_rejection!("passkey ordinary-failure zero rule removed", "passkey acceptance semantic rules are incomplete") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(passkey_schema: passkey_schema_without_failure_zero))
end
acceptance_without_event_mapping = JSON.parse(JSON.generate(cross_cutting_inputs.fetch(:acceptance_catalog)))
acceptance_without_event_mapping.fetch("artifacts").find { |row| row.fetch("artifact_id") == "passkey-browser-ceremony-report" }
  .fetch("required_observations").each { |row| row["expected_outcome"] = row.fetch("expected_outcome").gsub("identity.passkey.mark_compromised", "credential compromise event") }
expect_cross_cutting_fixture_rejection!("passkey acceptance mapping removed", "passkey acceptance invariant omits security-action mapping") do
  cross_cutting_remediation_errors(**cross_cutting_inputs.merge(acceptance_catalog: acceptance_without_event_mapping))
end

risk_evidence_without_reservation_transition = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "Legal transitions are only `absent` to `issued`, `issued` to `reserved`, `issued` to `expired` or `revoked`, `reserved` to `finalized`, and `reserved` to `released` or `revoked`"
)
expect_risk_evidence_fixture_rejection!(
  "state machine without reservation transition", "RiskEvidence legal transitions are incomplete"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: risk_evidence_without_reservation_transition))
end

concurrent_risk_evidence_reuse = risk_evidence_contract_inputs.fetch(:transaction_contract).sub(
  "MUST give exactly one command the `issued` to `reserved` transition",
  "MAY give both commands the `issued` to `reserved` transition"
)
expect_risk_evidence_fixture_rejection!(
  "two concurrent reservation winners", "RiskEvidence concurrent reservation lacks one-winner denial"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: concurrent_risk_evidence_reuse))
end

split_recovery_participants = risk_evidence_contract_inputs.fetch(:transaction_contract).sub(
  "`identity/otp/postgres`, `capability/postgres`, `identity/password/postgres`, and\n`identity/session/postgres`",
  "`capability/postgres` and `identity/password/postgres`"
)
expect_risk_evidence_fixture_rejection!(
  "phone recovery without OTP and session participants", "phone recovery does not enlist every atomic participant"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: split_recovery_participants))
end

split_one_time_reservation = risk_evidence_contract_inputs.fetch(:transaction_contract).sub(
  "The completion reservation transaction MUST transition the\nRiskEvidence, purpose-bound OTP, and reset capability together or none of them",
  "RiskEvidence MAY reserve before OTP and capability"
)
expect_risk_evidence_fixture_rejection!(
  "split RiskEvidence OTP capability reservation",
  "phone recovery reservation is not atomic across one-time participants"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: split_one_time_reservation))
end

split_risk_evidence_finalization = risk_evidence_contract_inputs.fetch(:transaction_contract).sub(
  "password mutation, session invalidation, outbox/audit records, and\ncommand result",
  "password mutation and command result"
)
expect_risk_evidence_fixture_rejection!(
  "RiskEvidence finalized apart from session invalidation", "phone recovery atomic finalization is incomplete"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: split_risk_evidence_finalization))
end

out_of_uow_risk_evidence_reservation = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "`tx.risk_evidence.reserve` MUST run only in the coordinator's single `tx.uow.reserve` transaction that atomically reserves the command and the exact one-time participants declared by the operation profile; it MUST NOT open a separate or private transaction"
)
expect_risk_evidence_fixture_rejection!(
  "out-of-UoW private RiskEvidence reservation",
  "RiskEvidence reservation can escape the coordinator unit of work"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: out_of_uow_risk_evidence_reservation))
end

stale_generation_risk_evidence_takeover = risk_evidence_contract_inputs.fetch(:transaction_contract).sub(
  "CAS-rebind\n   every already-`reserved` one-time participant from the exact prior generation\n   to the new generation under the same command ID and fingerprint",
  "accept every reserved participant under any old generation"
)
expect_risk_evidence_fixture_rejection!(
  "takeover accepts stale RiskEvidence generation",
  "RiskEvidence takeover lacks guarded generation transfer"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: stale_generation_risk_evidence_takeover))
end

risk_evidence_released_on_unknown = risk_evidence_contract_inputs.fetch(:transaction_contract).sub(
  "An ambiguous commit MUST leave the item `reserved`, return\n`Unknown`, and use `tx.risk_evidence.recover`; expiry or lease timeout alone\nMUST NOT release it",
  "An ambiguous commit MAY release the item after expiry"
)
expect_risk_evidence_fixture_rejection!(
  "unknown completion releases evidence", "RiskEvidence unknown outcome can reopen evidence"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: risk_evidence_released_on_unknown))
end

risk_evidence_reuse_after_release = risk_evidence_contract_inputs.fetch(:transaction_contract).sub(
  "remains terminal and a retry requires newly issued evidence",
  "returns to issued for retry"
)
expect_risk_evidence_fixture_rejection!(
  "released evidence becomes reusable", "RiskEvidence release can permit reuse"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: risk_evidence_reuse_after_release))
end

risk_evidence_cleanup_without_tombstone = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "Cleanup MUST expire untouched `issued` rows in bounded database-time batches and retain every `reserved` row through authoritative recovery. Only after the later of original evidence expiry and the configured `command.result_retention` deadline MAY cleanup crypto-shred terminal payload/linkage; it MUST preserve a restricted tenant/purpose/keyed-digest/key-version/original-expiry/terminal-state tombstone with no time-based deletion. The tombstone MAY be deleted only after every evidence-verification and keyed-digest key version it references is retired and proof shows every bearer fails cryptographic validation before lookup"
)
expect_risk_evidence_fixture_rejection!(
  "cleanup without replay tombstone", "RiskEvidence cleanup can erase replay or recovery authority"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: risk_evidence_cleanup_without_tombstone))
end

risk_postgres_without_reserve = JSON.parse(JSON.generate(applicability))
risk_postgres_without_reserve.fetch("identity/risk/postgres").fetch("transaction").delete("tx.risk_evidence.reserve")
expect_risk_evidence_fixture_rejection!(
  "risk store without reserve applicability",
  "identity/risk/postgres omits RiskEvidence applicability: tx.risk_evidence.reserve"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(applicability: risk_postgres_without_reserve))
end

phone_without_risk_finalize = JSON.parse(JSON.generate(applicability))
phone_without_risk_finalize.fetch("identity/phone").fetch("transaction").delete("tx.risk_evidence.finalize")
expect_risk_evidence_fixture_rejection!(
  "phone workflow without RiskEvidence finalize applicability",
  "identity/phone omits RiskEvidence applicability: tx.risk_evidence.finalize"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(applicability: phone_without_risk_finalize))
end

risk_without_durable_issue = JSON.parse(JSON.generate(applicability))
risk_without_durable_issue.fetch("identity/risk").fetch("transaction").delete("tx.risk_evidence.issue")
expect_risk_evidence_fixture_rejection!(
  "risk producer without issuance applicability", "identity/risk omits RiskEvidence issuance applicability"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(applicability: risk_without_durable_issue))
end

split_end_state_risk_evidence = risk_evidence_contract_inputs.fetch(:end_state).sub(
  "identity.phone uses one coordinator unit of work to reserve identity.risk/postgres evidence, OTP and capability, then atomically finalize them with the password mutation, session invalidation and command result; unknown remains reserved for authoritative recovery",
  "identity.risk consumes the evidence before identity.phone commits the credential and revocation transaction"
)
expect_risk_evidence_fixture_rejection!(
  "END_STATE split evidence consumption", "END_STATE retains split RiskEvidence consumption"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(end_state: split_end_state_risk_evidence))
end

risk_postgres_without_recovery = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:risk_postgres_goal),
  "Unknown completion MUST retain `reserved` and reconcile the owning command before finalizing or releasing; expiry, lease loss, cleanup, or another command MUST NOT make the evidence reusable"
)
expect_risk_evidence_fixture_rejection!(
  "risk store without fail-closed recovery", "identity/risk/postgres lacks fail-closed RiskEvidence recovery"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(risk_postgres_goal: risk_postgres_without_recovery))
end

phone_without_atomic_risk_evidence = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:phone_goal),
  "Phone password-reset completion MUST use one coordinator command and unit of work to reserve and finalize RiskEvidence with the purpose-bound OTP, reset capability, password mutation, and session invalidation"
)
expect_risk_evidence_fixture_rejection!(
  "phone goal with split RiskEvidence consumption", "identity/phone lacks atomic RiskEvidence completion"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(phone_goal: phone_without_atomic_risk_evidence))
end

initiation_without_orchestration = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "Phone password-reset initiation MUST use one initiation command and coordinator unit of work to reserve, apply, and finalize initiation RiskEvidence"
)
expect_risk_evidence_fixture_rejection!(
  "initiation without reserve apply finalize",
  "phone recovery initiation lacks authoritative RiskEvidence orchestration"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_without_orchestration))
end

initiation_split_from_issuance = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "The initiation domain commit MUST apply and finalize that initiation RiskEvidence in the same commit that issues the purpose-bound OTP challenge, canonical reset capability, outbox/audit records, and command result, or commit none of them"
)
expect_risk_evidence_fixture_rejection!(
  "initiation finalized before challenge issuance",
  "phone recovery initiation can split RiskEvidence from challenge issuance"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_split_from_issuance))
end

initiation_reserves_unissued_outputs = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "Phone reset initiation reserves only the command and initiation RiskEvidence, because its OTP challenge and capability do not exist until the domain commit"
)
expect_risk_evidence_fixture_rejection!(
  "initiation reserves OTP and capability before issuance",
  "phone recovery initiation can reserve outputs before issuance"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_reserves_unissued_outputs))
end

initiation_second_winner = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "Two concurrent initiation commands MAY precheck the same evidence, but exactly one MAY reserve it; the loser receives the stable non-enumerating replay denial"
)
expect_risk_evidence_fixture_rejection!(
  "concurrent initiation second winner",
  "phone recovery initiation lacks one-winner reservation"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_second_winner))
end

initiation_replay_drift = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "A same-command, same-fingerprint initiation replay MUST return the exact recorded challenge and capability result without issuing replacements"
)
expect_risk_evidence_fixture_rejection!("initiation replay drift", "phone recovery initiation replay can drift") do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_replay_drift))
end

initiation_stale_takeover = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "An expired initiation-command takeover MUST CAS-rebind the initiation RiskEvidence from the exact prior generation to the new generation before apply or finalize authority is granted"
)
expect_risk_evidence_fixture_rejection!(
  "initiation stale-generation takeover",
  "phone recovery initiation takeover lacks generation fencing"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_stale_takeover))
end

initiation_release_without_proof = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "An initiation rollback MUST NOT release its RiskEvidence without authoritative proof that the command did not commit"
)
expect_risk_evidence_fixture_rejection!(
  "initiation rollback releases without proof",
  "phone recovery initiation rollback can release without proof"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_release_without_proof))
end

initiation_unknown_reopens = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "An ambiguous initiation outcome MUST remain `reserved` until authoritative recovery resolves the owning command"
)
expect_risk_evidence_fixture_rejection!(
  "initiation unknown outcome reopens evidence",
  "phone recovery initiation unknown outcome can reopen evidence"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_unknown_reopens))
end

phase_reused_api_evidence = risk_evidence_contract_inputs.fetch(:api_operations).sub(
  "This separate completion-only RiskEvidence must not reuse the initiation artifact",
  "Completion reuses the initiation artifact"
)
expect_risk_evidence_fixture_rejection!(
  "same artifact reused across phases",
  "API operations permit cross-phase RiskEvidence reuse"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(api_operations: phase_reused_api_evidence))
end

initiation_without_phase_retention = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:transaction_contract),
  "Cleanup and tombstones MUST independently preserve replay and recovery authority for both phone-reset RiskEvidence purposes"
)
expect_risk_evidence_fixture_rejection!(
  "initiation tombstone retention omission",
  "phone recovery phase retention is not distinct"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(transaction_contract: initiation_without_phase_retention))
end

end_state_without_initiation_atomicity = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:end_state),
  "identity.phone atomically reserves, applies and finalizes initiation-only RiskEvidence with OTP challenge and reset capability issuance; completion requires a separate completion-only artifact"
)
expect_risk_evidence_fixture_rejection!(
  "END_STATE without initiation atomicity",
  "END_STATE omits phase-distinct initiation atomicity"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(end_state: end_state_without_initiation_atomicity))
end

phone_without_phase_distinction = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:phone_goal),
  "Initiation and completion MUST use separate purpose-bound RiskEvidence artifacts and MUST NOT validate, reserve, replay, or substitute one for the other"
)
expect_risk_evidence_fixture_rejection!(
  "phone goal without phase distinction",
  "identity/phone lacks phase-distinct RiskEvidence"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(phone_goal: phone_without_phase_distinction))
end

risk_without_phase_distinction = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:risk_goal),
  "Phone reset initiation and completion MUST receive separate artifacts with purposes `phone-password-reset-initiate` and `phone-password-reset-complete`; their references, keyed digests, reservations, and terminal records MUST remain distinct"
)
expect_risk_evidence_fixture_rejection!(
  "risk goal without phase distinction",
  "identity/risk lacks phase-distinct RiskEvidence issuance"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(risk_goal: risk_without_phase_distinction))
end

api_without_risk_evaluate = risk_evidence_contract_inputs.fetch(:api_operations).lines.reject do |line|
  line.start_with?("| `identity.risk.evaluate` |")
end.join
expect_risk_evidence_fixture_rejection!(
  "canonical issuance operation removed",
  "API operations omit canonical RiskEvidence issuance operation"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(api_operations: api_without_risk_evaluate))
end

risk_evaluate_without_completion_phase = risk_evidence_contract_inputs.fetch(:api_operations).sub(
  "phase `phone-reset-completion` maps only to purpose `phone-password-reset-complete`",
  "completion phase is omitted"
)
expect_risk_evidence_fixture_rejection!(
  "canonical issuance operation omits completion phase",
  "identity.risk.evaluate omits phase-specific RiskEvidence issuance"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(api_operations: risk_evaluate_without_completion_phase))
end

risk_evaluate_with_generic_phase = risk_evidence_contract_inputs.fetch(:api_operations).sub(
  "Issuance phase is exactly `none`, `phone-reset-initiation`, or `phone-reset-completion`",
  "Issuance phase is `none`, `phone-reset-initiation`, `phone-reset-completion`, or `generic`"
)
expect_risk_evidence_fixture_rejection!(
  "canonical issuance operation accepts generic phase",
  "identity.risk.evaluate issuance phase catalog is not closed"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(api_operations: risk_evaluate_with_generic_phase))
end

risk_evaluate_accepts_caller_purpose = risk_evidence_contract_inputs.fetch(:api_operations).sub(
  "unknown/unsupported phases, a purpose with `none`, and any caller-supplied purpose are rejected before provider evaluation or state access",
  "callers may supply any issuance purpose"
)
expect_risk_evidence_fixture_rejection!(
  "canonical issuance operation accepts caller purpose",
  "identity.risk.evaluate issuance phase catalog is not closed"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(api_operations: risk_evaluate_accepts_caller_purpose))
end

risk_postgres_without_authoritative_issue = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:risk_postgres_goal),
  "Issue MUST enlist in the identity command unit of work and atomically persist the exact `issued` row and committed command result before any opaque reference is returned"
)
expect_risk_evidence_fixture_rejection!(
  "RiskEvidence issuance without durable commit",
  "identity/risk/postgres lacks authoritative issuance commit"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(risk_postgres_goal: risk_postgres_without_authoritative_issue))
end

reference_with_risk_evidence_leakage = without_normalized_phrase(
  risk_evidence_contract_inputs.fetch(:reference_goal),
  "The reference composition MUST invoke `identity.risk.evaluate` for both phone-reset phases and MUST return only its opaque reference and safe freshness/one-use metadata; raw signals, provider evidence, embedded evidence payloads, digests, signatures, journal identifiers, and persistence records MUST NOT cross into `identity/phone`"
)
expect_risk_evidence_fixture_rejection!(
  "reference leaks RiskEvidence internals",
  "identity/reference lacks non-leaking RiskEvidence issuance composition"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(reference_goal: reference_with_risk_evidence_leakage))
end

reference_without_risk_issue = JSON.parse(JSON.generate(applicability))
reference_without_risk_issue.fetch("identity/reference").fetch("transaction").delete("tx.risk_evidence.issue")
expect_risk_evidence_fixture_rejection!(
  "reference omits RiskEvidence issue applicability",
  "identity/reference phone recovery transaction applicability drifted"
) do
  risk_evidence_contract_errors(**risk_evidence_contract_inputs.merge(applicability: reference_without_risk_issue))
end
phone_contract_errors(**phone_contract_inputs).each { |error| fail_check(error) }
enabled_by_default_phone_recovery = phone_contract_inputs.fetch(:configuration).sub(
  "| `phone.recovery.enabled` | `false` |", "| `phone.recovery.enabled` | `true` |"
)
expect_phone_contract_fixture_rejection!(
  "enabled-by-default recovery", "phone recovery enablement is not explicitly disabled with fail-closed operations"
) do
  phone_contract_errors(**phone_contract_inputs.merge(configuration: enabled_by_default_phone_recovery))
end
duplicate_phone_enablement = phone_contract_inputs.fetch(:configuration).sub(
  "`request_when_disabled = deny`", "`enabled = true`; `request_when_disabled = deny`"
)
expect_phone_contract_fixture_rejection!(
  "disagreeing duplicate enablement", "phone recovery policy duplicates the atomic enablement authority"
) do
  phone_contract_errors(**phone_contract_inputs.merge(configuration: duplicate_phone_enablement))
end
missing_phone_otp_proof = phone_contract_inputs.fetch(:configuration).sub(
  "canonical_reset_capability_plus_purpose_bound_phone_otp_plus_eligible_independent_factor",
  "canonical_reset_capability_plus_eligible_independent_factor"
)
expect_phone_contract_fixture_rejection!(
  "recovery proof without purpose-bound phone OTP",
  "phone recovery policy omits proof=canonical_reset_capability_plus_purpose_bound_phone_otp_plus_eligible_independent_factor"
) do
  phone_contract_errors(**phone_contract_inputs.merge(configuration: missing_phone_otp_proof))
end
wrong_phone_risk_authority = phone_contract_inputs.fetch(:configuration).sub(
  "`risk_authority = identity/risk`", "`risk_authority = caller`"
)
expect_phone_contract_fixture_rejection!(
  "caller-owned recovery risk authority", "phone recovery policy omits risk_authority=identity/risk"
) do
  phone_contract_errors(**phone_contract_inputs.merge(configuration: wrong_phone_risk_authority))
end
caller_phone_signals_allowed = phone_contract_inputs.fetch(:configuration).sub(
  "`caller_signals = forbidden`", "`caller_signals = allowed`"
)
expect_phone_contract_fixture_rejection!(
  "caller-supplied recovery signals", "phone recovery policy omits caller_signals=forbidden"
) do
  phone_contract_errors(**phone_contract_inputs.merge(configuration: caller_phone_signals_allowed))
end
mutable_phone_risk_evidence = phone_contract_inputs.fetch(:risk_goal).sub(
  "issues immutable `RiskEvidence`", "issues mutable `RiskEvidence`"
)
expect_phone_contract_fixture_rejection!(
  "mutable producer RiskEvidence", "identity/risk goal lacks immutable one-use phone RiskEvidence ownership"
) do
  phone_contract_errors(**phone_contract_inputs.merge(risk_goal: mutable_phone_risk_evidence))
end
reusable_phone_risk_evidence = phone_contract_inputs.fetch(:risk_goal).sub(
  "MUST be atomically consumed at most once by", "MAY be consumed multiple times by"
)
expect_phone_contract_fixture_rejection!(
  "reusable producer RiskEvidence", "identity/risk goal lacks immutable one-use phone RiskEvidence ownership"
) do
  phone_contract_errors(**phone_contract_inputs.merge(risk_goal: reusable_phone_risk_evidence))
end
wrong_phone_risk_ttl = phone_contract_inputs.fetch(:risk_goal).sub(
  "two minutes in the reference profile", "ten minutes in the reference profile"
)
expect_phone_contract_fixture_rejection!(
  "drifted producer RiskEvidence TTL", "identity/risk goal lacks phone RiskEvidence freshness and replay rejection"
) do
  phone_contract_errors(**phone_contract_inputs.merge(risk_goal: wrong_phone_risk_ttl))
end
risk_goal_binding = <<~TEXT.strip
  MUST bind tenant, subject, recovery
    operation, recovery purpose, canonical number, pre-auth transaction, attempt
    ID and risk-policy version
TEXT
%w[tenant subject operation purpose canonical_number preauth_transaction attempt_id policy_version].each do |binding|
  binding_text = {
    "canonical_number" => "canonical number",
    "preauth_transaction" => "pre-auth transaction",
    "attempt_id" => "attempt\n  ID",
    "policy_version" => "risk-policy version"
  }.fetch(binding, binding)
  drifted_binding = risk_goal_binding.sub(binding_text, "omitted binding")
  risk_goal_without_binding = phone_contract_inputs.fetch(:risk_goal).sub(risk_goal_binding, drifted_binding)
  expect_phone_contract_fixture_rejection!(
    "producer RiskEvidence without #{binding}", "identity/risk goal lacks exact phone RiskEvidence binding"
  ) do
    phone_contract_errors(**phone_contract_inputs.merge(risk_goal: risk_goal_without_binding))
  end
end
%w[sim_swap number_recycling carrier].each do |signal|
  canonical_mapping = "#{signal} = negative_allow,positive_deny,unknown_deny,unavailable_deny"
  %w[positive unknown unavailable].each do |state|
    unsafe_mapping = canonical_mapping.sub("#{state}_deny", "#{state}_allow")
    drifted_signal_policy = phone_contract_inputs.fetch(:configuration).sub(canonical_mapping, unsafe_mapping)
    expect_phone_contract_fixture_rejection!(
      "#{signal} #{state} allow", "phone recovery policy omits #{canonical_mapping.delete(' ')}"
    ) do
      phone_contract_errors(**phone_contract_inputs.merge(configuration: drifted_signal_policy))
    end
  end
end
unbound_phone_initiation = phone_contract_inputs.fetch(:api_operations).sub(
  "public plus pre-auth transaction for signup/signin", "public"
)
expect_phone_contract_fixture_rejection!(
  "public phone initiation without pre-auth", "phone verification initiation lacks exact public pre-auth and authenticated-change split"
) do
  phone_contract_errors(**phone_contract_inputs.merge(api_operations: unbound_phone_initiation))
end
enabled_only_recovery_request = phone_contract_inputs.fetch(:api_operations).sub(
  "Available only with `phone.recovery.enabled=true` and denied while disabled.",
  "Available by default."
)
expect_phone_contract_fixture_rejection!(
  "recovery request available while disabled", "phone password-reset request is not denied unless explicitly enabled"
) do
  phone_contract_errors(**phone_contract_inputs.merge(api_operations: enabled_only_recovery_request))
end
recovery_request_without_preauth = phone_contract_inputs.fetch(:api_operations).sub(
  "explicitly enabled public recovery plus pre-auth transaction", "explicitly enabled public recovery"
)
expect_phone_contract_fixture_rejection!(
  "recovery request without pre-auth binding",
  "phone password-reset request lacks authoritative pre-auth risk-evidence contract"
) do
  phone_contract_errors(**phone_contract_inputs.merge(api_operations: recovery_request_without_preauth))
end
raw_phone_risk_inputs = phone_contract_inputs.fetch(:api_operations).sub(
  "only an opaque fresh initiation-only `RiskEvidence` reference issued by `identity/risk` for purpose `phone-password-reset-initiate`, never raw carrier facts",
  "raw carrier facts"
)
expect_phone_contract_fixture_rejection!(
  "raw caller carrier inputs", "phone password-reset request lacks authoritative pre-auth risk-evidence contract"
) do
  phone_contract_errors(**phone_contract_inputs.merge(api_operations: raw_phone_risk_inputs))
end
missing_phone_risk_goal_dependency = phone_contract_inputs.fetch(:phone_goal).sub(
  ", `identity/risk`", ""
)
expect_phone_contract_fixture_rejection!(
  "goal without risk dependency", "identity/phone dependency on identity/risk is not closed across DAG contracts"
) do
  phone_contract_errors(**phone_contract_inputs.merge(phone_goal: missing_phone_risk_goal_dependency))
end
missing_phone_risk_diagram_edge = phone_contract_inputs.fetch(:dependencies).sub("  risk --> phone\n", "")
expect_phone_contract_fixture_rejection!(
  "diagram without risk dependency", "identity/phone dependency on identity/risk is not closed across DAG contracts"
) do
  phone_contract_errors(**phone_contract_inputs.merge(dependencies: missing_phone_risk_diagram_edge))
end
missing_phone_risk_event_applicability = JSON.parse(JSON.generate(applicability))
missing_phone_risk_event_applicability.fetch("identity/phone").fetch("security_events").delete("identity.risk.decide")
expect_phone_contract_fixture_rejection!(
  "risk-decision event applicability gap",
  "identity/phone omits risk-decision security-event applicability: identity.risk.decide"
) do
  phone_contract_errors(**phone_contract_inputs.merge(applicability: missing_phone_risk_event_applicability))
end
optional_phone_session_suppression = phone_contract_inputs.fetch(:phone_goal).sub(
  /purpose separation\.\s+Phone operations do not\s+expose session suppression\./,
  "purpose separation and optional session suppression."
)
fail_check("phone contract optional-session-suppression fixture setup drifted") if optional_phone_session_suppression == phone_contract_inputs.fetch(:phone_goal)
expect_phone_contract_fixture_rejection!(
  "optional session suppression", "identity/phone retains a session-suppression input"
) do
  phone_contract_errors(**phone_contract_inputs.merge(phone_goal: optional_phone_session_suppression))
end
phone_session_suppression_field = phone_contract_inputs.fetch(:api_operations).sub(
  "Number/code and the same tenant", "session_suppression and number/code with the same tenant"
)
expect_phone_contract_fixture_rejection!(
  "session suppression API field", "identity/phone retains a session-suppression input"
) do
  phone_contract_errors(**phone_contract_inputs.merge(api_operations: phone_session_suppression_field))
end
missing_phone_consumer = phone_contract_inputs.fetch(:lifecycle_consumers).sub(
  "`identity/email`, `identity/phone`, `identity/magiclink`",
  "`identity/email`, `identity/magiclink`"
)
expect_phone_contract_fixture_rejection!(
  "identifier change without phone consumer", "lifecycle.cascade.identifier_change omits identity/phone consumer"
) do
  phone_contract_errors(**phone_contract_inputs.merge(lifecycle_consumers: missing_phone_consumer))
end
missing_phone_event_mapping = phone_contract_inputs.fetch(:api_operations).sub(
  "emits exactly `identity.identifier.request_verification`", "records the request"
)
expect_phone_contract_fixture_rejection!(
  "phone operation without identifier event", "identity.phone.send-verification lacks exact identifier security-event mapping"
) do
  phone_contract_errors(**phone_contract_inputs.merge(api_operations: missing_phone_event_mapping))
end
%w[update remove].zip(%w[identifier_change identifier_remove]).each do |operation_suffix, cascade_suffix|
  mapping = "initiates exactly `lifecycle.cascade.#{cascade_suffix}`"
  missing_cascade_mapping = phone_contract_inputs.fetch(:api_operations).sub(mapping, "records the transition")
  expect_phone_contract_fixture_rejection!(
    "phone #{operation_suffix} without lifecycle cascade",
    "identity.phone.#{operation_suffix} lacks exact identifier lifecycle-cascade mapping"
  ) do
    phone_contract_errors(**phone_contract_inputs.merge(api_operations: missing_cascade_mapping))
  end
end
missing_phone_event_applicability = JSON.parse(JSON.generate(applicability))
missing_phone_event_applicability.fetch("identity/phone").fetch("security_events").delete("identity.identifier.verify")
expect_phone_contract_fixture_rejection!(
  "phone event applicability gap", "identity/phone omits identifier security-event applicability: identity.identifier.verify"
) do
  phone_contract_errors(**phone_contract_inputs.merge(applicability: missing_phone_event_applicability))
end
capability_roles = %w[
  tx.capability.apply tx.capability.finalize tx.capability.issue
  tx.capability.recover tx.capability.reserve tx.capability.validate tx.foundation
]
%w[sso sso/domain-verification sso/oidc sso/oauth2 sso/saml sso/postgres].each do |unit|
  missing = capability_roles - applicability.fetch(unit).fetch("transaction")
  fail_check("#{unit} omits one-time SSO capability roles: #{missing.join(', ')}") unless missing.empty?
end

conformance = JSON.parse(artifacts.fetch("PROTOCOL_CONFORMANCE_MANIFEST.json"))
fail_check("protocol conformance manifest top-level schema drifted") unless conformance.keys == %w[version retrieved_at verified_errata clause_pins source_identity sources tools]
fail_check("protocol conformance manifest version drifted") unless conformance.fetch("version") == 1
source_ids = conformance.fetch("sources").map { |source| source.fetch("id") }
fail_check("protocol conformance source IDs are duplicated") unless source_ids.uniq == source_ids
normative_rfc_documents = NORMATIVE_RFC_FILES.to_h do |filename|
  [filename, File.read(File.join(ROOT, filename))]
end
Dir.glob(File.join(ROOT, "goals", "*.md")).sort.each do |path|
  normative_rfc_documents[path.delete_prefix(ROOT + File::SEPARATOR)] = File.read(path)
end
normative_rfc_errors(normative_rfc_documents, source_ids).each { |error| fail_check(error) }
source_identity = conformance.fetch("source_identity")
fail_check("protocol conformance title/revision coverage drifted") unless source_identity.keys.to_set == source_ids.to_set
source_identity.each do |id, identity|
  fail_check("protocol source #{id} title/revision schema drifted") unless identity.keys == %w[title revision]
  fail_check("protocol source #{id} lacks title or revision") if identity.values.any?(&:empty?)
end
%w[rfc-7591 rfc-7592 rfc-8628 rfc-9728 public-suffix-list-e1b8015c webauthn-level-3-cr-20260526].each do |id|
  fail_check("protocol conformance manifest missing #{id}") unless source_ids.include?(id)
end
sources = conformance.fetch("sources")
sources.each do |source|
  fail_check("protocol source #{source['id']} has invalid digest") unless source.fetch("sha256").match?(/\A[0-9a-f]{64}\z/)
  fail_check("protocol source #{source['id']} lacks license") if source.fetch("license").empty?
  fail_check("protocol source #{source['id']} lacks consumers") unless source.fetch("consumers").is_a?(Array) && !source.fetch("consumers").empty?
end
protocol_source_identity_errors(source_identity, sources).each { |error| fail_check(error) }
protocol_source_consumer_errors(sources, identity_units.to_set).each { |error| fail_check(error) }
conformance_tool_errors(conformance.fetch("tools"), identity_units.to_set).each { |error| fail_check(error) }
clause_pins = conformance.fetch("clause_pins")
clause_ids = clause_pins.map { |pin| pin.fetch("requirement_id") }
fail_check("protocol clause pin IDs are not unique") unless clause_ids.uniq == clause_ids
fail_check("protocol source clause-pin closure drifted") unless clause_pins.map { |pin| pin.fetch("source_id") }.to_set == source_ids.to_set
required_clause_ids = %w[
  scim.duplicate-case-collision scim.password-write-unsupported
  scim.if-match-precondition scim.default-sorting
]
fail_check("protocol clause decision closure drifted") unless (required_clause_ids - clause_ids).empty?
clause_pins.each do |pin|
  fail_check("protocol clause pin source is unknown: #{pin['requirement_id']}") unless source_ids.include?(pin.fetch("source_id"))
  fail_check("protocol clause pin locator is empty: #{pin['requirement_id']}") unless pin.fetch("locator").is_a?(String) && !pin.fetch("locator").empty?
  fail_check("protocol clause pin disposition is invalid: #{pin['requirement_id']}") unless %w[required profile-decision unsupported].include?(pin.fetch("disposition"))
  consumers = pin.fetch("consumers")
  fail_check("protocol clause pin consumers drifted: #{pin['requirement_id']}") unless consumers.is_a?(Array) && consumers.any? && consumers == consumers.sort.uniq && consumers.all? { |unit| identity_units.include?(unit) }
end
expected_protocol_decisions = {
  "oidc.implicit-unsupported" => ["oidc-core-1.0-errata-2", "Sections 3.2 and 15.1 implicit flow", "unsupported"],
  "oidc.hybrid-unsupported" => ["oidc-core-1.0-errata-2", "Sections 3.3 and 15.1 hybrid flow", "unsupported"],
  "oidc.jarm-unsupported" => ["oauth-jarm-1.0-final", "Sections 2 and 3 response mode and client metadata; Section 4 authorization-server metadata; Section 5 security considerations", "unsupported"],
  "oidc.request-objects-unsupported" => ["oidc-core-1.0-errata-2", "Section 6 Request Object and request_uri parameters", "unsupported"],
  "oidc.encrypted-id-token-unsupported" => ["oidc-core-1.0-errata-2", "Sections 10.2 and 10.2.1 encrypted ID Tokens", "unsupported"],
  "oidc.form-post-response-mode-unsupported" => ["oauth-form-post-response-mode-1.0-final", "Section 2 Form Post Response Mode", "unsupported"],
  "oauth.apple-form-post-response-mode-required" => ["oauth-form-post-response-mode-1.0-final", "Section 2 Form Post Response Mode", "profile-decision"],
  "oidc.response-mode-fragment-unsupported" => ["oidc-multiple-response-types-1.0", "Section 2 fragment response mode", "unsupported"],
  "oidc.frontchannel-logout-unsupported" => ["oidc-frontchannel-logout-1.0-final", "Sections 2, 3, and 4 Front-Channel Logout", "unsupported"],
  "oidc.backchannel-logout-unsupported" => ["oidc-backchannel-logout-1.0-final", "Sections 2 and 3 Back-Channel Logout", "unsupported"]
}
expected_protocol_decisions.each do |id, expected|
  pin = clause_pins.find { |candidate| candidate.fetch("requirement_id") == id }
  fail_check("protocol decision pin drifted: #{id}") unless pin && pin.values_at("source_id", "locator", "disposition") == expected
end
expected_protocol_consumers = {
  "oauth.apple-form-post-response-mode-required" => %w[identity/oauth identity/oauth/providers],
  "oidc.form-post-response-mode-unsupported" => %w[oauth-server/oidc sso/oidc],
  "oidc.response-mode-fragment-unsupported" => %w[identity/oauth identity/oauth/providers oauth-server/oidc sso/oidc]
}
expected_protocol_consumers.each do |id, expected|
  pin = clause_pins.find { |candidate| candidate.fetch("requirement_id") == id }
  fail_check("protocol decision consumer set drifted: #{id}") unless pin && pin.fetch("consumers") == expected
end
{
  "saml-errata-05" => "Approved Errata 05 items E6, E13, E43, E55, E60, E62, E79, E90, E92 and E94 selected by the SAML profile",
  "google-authenticator-key-uri-8ba6e793" => "Key URI Format sections Label, Secret, Issuer, Algorithm, Digits, Counter and Period; unknown or conflicting parameters rejected"
}.each do |source_id, locator|
  fail_check("protocol locator drifted: #{source_id}") unless clause_pins.any? { |pin| pin.fetch("source_id") == source_id && pin.fetch("locator") == locator }
end
weakened_clause_pin = JSON.parse(JSON.generate(clause_pins))
weakened_clause_pin.find { |pin| pin.fetch("requirement_id") == "scim.if-match-precondition" }.delete("locator") if clause_ids.include?("scim.if-match-precondition")
if clause_ids.include?("scim.if-match-precondition")
  fail_check("protocol clause/source weakening mutation was accepted") if weakened_clause_pin.all? { |pin| pin["locator"].is_a?(String) && !pin["locator"].empty? }
end

tool_fixture = lambda do |label, expected_error, &mutate|
  tools = JSON.parse(JSON.generate(EXPECTED_CONFORMANCE_TOOLS))
  mutate.call(tools)
  expect_protocol_fixture_rejection!(label, expected_error) do
    conformance_tool_errors(tools, identity_units.to_set)
  end
end
tool_fixture.call("missing retrieved digest", "protocol conformance tool openid-conformance-suite has invalid retrieved digest") do |tools|
  tools.fetch(0).delete("sha256")
end
tool_fixture.call("mutable revision", "protocol conformance tool web-platform-tests lacks immutable revision") do |tools|
  tools.fetch(1)["revision"] = "main"
end
tool_fixture.call("short revision", "protocol conformance tool web-platform-tests lacks immutable revision") do |tools|
  tools.fetch(1)["revision"] = "0" * 39
end
tool_fixture.call("wrong retrieved digest", "protocol conformance tool unboundid-scim2-sdk retrieved digest drifted") do |tools|
  tools.fetch(2)["sha256"] = "0" * 64
end
tool_fixture.call("missing license", "protocol conformance tool simplesamlphp license drifted") do |tools|
  tools.fetch(3)["license"] = ""
end
tool_fixture.call("wrong license", "protocol conformance tool simplesamlphp license drifted") do |tools|
  tools.fetch(3)["license"] = "Apache-2.0"
end
tool_fixture.call("missing consumer", "protocol conformance tool openid-conformance-suite consumer set drifted") do |tools|
  tools.fetch(0)["consumers"].delete("sso/oidc")
end
tool_fixture.call("extra consumer", "protocol conformance tool web-platform-tests consumer set drifted") do |tools|
  tools.fetch(1)["consumers"] << "sso/saml"
end

fixture_source_ids = normative_rfc_documents.values.flat_map do |document|
  document.scan(/\bRFC(?:\s+|-)?(\d{4})\b/i).flatten.map { |number| "rfc-#{number}" }
end.uniq
{
  "program" => "PROGRAM.md",
  "configuration" => "REFERENCE_CONFIGURATION.md",
  "API" => "API_OPERATIONS.md",
  "goal" => "goals/identity.md"
}.each do |label, filename|
  documents = normative_rfc_documents.merge(filename => normative_rfc_documents.fetch(filename) + "\nRFC 9998 fixture.\n")
  expect_protocol_fixture_rejection!("unmanifested RFC in #{label}", "normative RFC manifest closure drifted: rfc-9998") do
    normative_rfc_errors(documents, fixture_source_ids)
  end
end
%w[rfc-5952 rfc-6901 rfc-7239].each do |id|
  expect_protocol_fixture_rejection!("missing #{id} source", "normative RFC manifest closure drifted: #{id}") do
    normative_rfc_errors(normative_rfc_documents, fixture_source_ids - [id])
  end
end
source_consumer_fixture = JSON.parse(JSON.generate(sources))
source_consumer_fixture.find { |source| source.fetch("id") == "rfc-5952" }.fetch("consumers").delete("identity/risk")
expect_protocol_fixture_rejection!("missing RFC source consumer", "protocol source rfc-5952 consumer set drifted") do
  protocol_source_consumer_errors(source_consumer_fixture, identity_units.to_set)
end
source_consumer_fixture = JSON.parse(JSON.generate(sources))
source_consumer_fixture.find { |source| source.fetch("id") == "rfc-7239" }.fetch("consumers") << "identity/risk"
expect_protocol_fixture_rejection!("extra RFC source consumer", "protocol source rfc-7239 consumer set drifted") do
  protocol_source_consumer_errors(source_consumer_fixture, identity_units.to_set)
end
source_identity_fixture = lambda do |label, &mutate|
  fixture_identity = JSON.parse(JSON.generate(source_identity))
  fixture_sources = JSON.parse(JSON.generate(sources))
  mutate.call(fixture_identity, fixture_sources)
  expect_protocol_fixture_rejection!(label, "protocol source identity digest drifted") do
    protocol_source_identity_errors(fixture_identity, fixture_sources)
  end
end
source_identity_fixture.call("substituted source URL") do |_identities, fixture_sources|
  fixture_sources.fetch(0)["url"] = "https://example.invalid/substitute"
end
source_identity_fixture.call("substituted source revision") do |fixture_identity, fixture_sources|
  fixture_identity.fetch(fixture_sources.fetch(0).fetch("id"))["revision"] = "substitute revision"
end
source_identity_fixture.call("substituted valid-shape source digest") do |_identities, fixture_sources|
  fixture_sources.fetch(0)["sha256"] = "0" * 64
end
source_identity_fixture.call("substituted source license") do |_identities, fixture_sources|
  fixture_sources.fetch(0)["license"] = "substitute license"
end
source_identity_fixture.call("substituted source title") do |fixture_identity, fixture_sources|
  fixture_identity.fetch(fixture_sources.fetch(0).fetch("id"))["title"] = "substitute title"
end

semantic_inputs = {
  configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  protocol: protocol,
  conformance: conformance,
  oauth_goal: goal_bodies.fetch("oauth-server"),
  saml_goal: goal_bodies.fetch("sso/saml"),
  api_operations: artifacts.fetch("API_OPERATIONS.md"),
  reference_profile: File.read(File.join(ROOT, "REFERENCE_PROFILE.md")),
  applicability: applicability,
  public_contracts: File.read(File.join(ROOT, "fragments/public_contracts_oauth.json"))
}
protocol_semantic_errors(**semantic_inputs).each { |error| fail_check(error) }

missing_errata = JSON.parse(JSON.generate(conformance))
missing_errata.fetch("verified_errata").fetch("rfc-7643").delete("5368")
expect_protocol_fixture_rejection!("missing SCIM erratum", "SCIM verified errata manifest drifted") do
  protocol_semantic_errors(**semantic_inputs.merge(conformance: missing_errata))
end
extra_errata = JSON.parse(JSON.generate(conformance))
extra_errata.fetch("verified_errata").fetch("rfc-7644") << "9999"
expect_protocol_fixture_rejection!("extra SCIM erratum", "SCIM verified errata manifest drifted") do
  protocol_semantic_errors(**semantic_inputs.merge(conformance: extra_errata))
end
protected_scope_escape = semantic_inputs.fetch(:configuration).sub(
  "`identity:read,identity:write`",
  "`identity:read,identity:write,platform:admin`"
)
expect_protocol_fixture_rejection!("protected-resource scope escape", "OAuth protected-resource scopes exceed the authorization-server catalog") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: protected_scope_escape))
end
unlinked_resource_authority = semantic_inputs.fetch(:configuration).sub(
  "resource = oauth_server.protected_resource.resource",
  "resource = external_api_origin"
)
expect_protocol_fixture_rejection!("unlinked protected-resource identifier", "OAuth protected-resource metadata authority is not linked") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: unlinked_resource_authority))
end
path_bearing_resource = semantic_inputs.fetch(:configuration).sub(
  "absolute HTTPS origin",
  "absolute HTTPS URL with optional path"
)
expect_protocol_fixture_rejection!("path-bearing protected-resource identifier", "OAuth protected-resource identifier lacks a canonical typed origin authority") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: path_bearing_resource))
end
drifted_resource_audience = semantic_inputs.fetch(:configuration).sub(
  "`oauth_server.protected_resource.resource`, `oauth_server.issuer`",
  "external API origin plus OAuth issuer"
)
expect_protocol_fixture_rejection!("protected-resource audience drift", "OAuth protected-resource audience authority is not exact") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: drifted_resource_audience))
end
drifted_resource_consumers = JSON.parse(JSON.generate(applicability))
drifted_resource_consumers.fetch("oauth-server").fetch("configuration").delete("ref.oauth_server.protected_resource.resource")
expect_protocol_fixture_rejection!("protected-resource consumer drift", "OAuth protected-resource identifier applicability drifted") do
  protocol_semantic_errors(**semantic_inputs.merge(applicability: drifted_resource_consumers))
end
request_host_resource = semantic_inputs.fetch(:api_operations) +
  "\nRFC 9728 `resource` is derived from the request host.\n"
expect_protocol_fixture_rejection!("request-host protected-resource derivation", "OAuth protected-resource identifier permits request-host derivation") do
  protocol_semantic_errors(**semantic_inputs.merge(api_operations: request_host_resource))
end
registration_scope_escape = semantic_inputs.fetch(:configuration).sub(
  "`email,identity:read,identity:write,offline_access,openid,profile` | closed registration subset",
  "`email,identity:read,identity:write,offline_access,openid,platform:admin,profile` | closed registration subset"
)
expect_protocol_fixture_rejection!("dynamic-registration scope escape", "OAuth dynamic-registration scopes exceed the authorization-server catalog") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: registration_scope_escape))
end
duplicate_management = semantic_inputs.fetch(:configuration) +
  "\n| `oauth_server.dynamic_registration.management` | `disabled` | forbidden duplicate authority fixture |\n"
expect_protocol_fixture_rejection!("duplicate dynamic-registration management authority", "OAuth dynamic-registration management has duplicate configuration authority") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: duplicate_management))
end
selected_registration_management = semantic_inputs.fetch(:configuration).sub(
  "management = unselected",
  "management = enabled"
)
expect_protocol_fixture_rejection!("selected RFC 7592 management", "OAuth dynamic-registration closed policy drifted") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: selected_registration_management))
end
ambiguous_idp_init = semantic_inputs.fetch(:configuration).sub(
  "| `saml.idp_initiated` | `false` |",
  "| `saml.idp_initiated` | disabled |"
)
expect_protocol_fixture_rejection!("ambiguous SAML IdP-init value", "SAML IdP-initiated configuration is not an explicit enableable Boolean") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: ambiguous_idp_init))
end
unconditional_idp_init = semantic_inputs.fetch(:protocol) + "\nIdP-initiated login remains disabled.\n"
expect_protocol_fixture_rejection!("unconditional SAML IdP-init prohibition", "SAML IdP-initiated baseline retains an unconditional prohibition") do
  protocol_semantic_errors(**semantic_inputs.merge(protocol: unconditional_idp_init))
end
continued_unconditional_idp_init = semantic_inputs.fetch(:api_operations) +
  "\nIdP-initiated login remains disabled in the reference profile.\n"
expect_protocol_fixture_rejection!("continued unconditional SAML IdP-init prohibition", "SAML IdP-initiated baseline retains an unconditional prohibition") do
  protocol_semantic_errors(**semantic_inputs.merge(api_operations: continued_unconditional_idp_init))
end
unsupported_idp_init = semantic_inputs.fetch(:reference_profile) + "\nIdP-initiated SAML is unsupported.\n"
expect_protocol_fixture_rejection!("unsupported SAML IdP-init", "SAML IdP-initiated baseline retains an unconditional prohibition") do
  protocol_semantic_errors(**semantic_inputs.merge(reference_profile: unsupported_idp_init))
end
forbidden_idp_init = semantic_inputs.fetch(:saml_goal) + "\nIdP-initiated login is forbidden.\n"
expect_protocol_fixture_rejection!("forbidden SAML IdP-init", "SAML IdP-initiated baseline retains an unconditional prohibition") do
  protocol_semantic_errors(**semantic_inputs.merge(saml_goal: forbidden_idp_init))
end
conflated_registration = semantic_inputs.fetch(:protocol) +
  "\nDynamic Client Registration, RFC 7591, and Client Registration Management, RFC 7592, only when the disabled-by-default registration profile is enabled.\n"
expect_protocol_fixture_rejection!("conflated RFC 7591 and RFC 7592 selection", "OAuth RFC 7591 and RFC 7592 selections are conflated") do
  protocol_semantic_errors(**semantic_inputs.merge(protocol: conflated_registration))
end
cross_artifact_registration = semantic_inputs.fetch(:api_operations) +
  "\nEnabling RFC 7591 also enables RFC 7592 management.\n"
expect_protocol_fixture_rejection!("cross-artifact RFC 7592 co-enablement", "OAuth RFC 7591 and RFC 7592 selections are conflated") do
  protocol_semantic_errors(**semantic_inputs.merge(api_operations: cross_artifact_registration))
end
missing_redirect_sigalg = semantic_inputs.fetch(:configuration).lines.reject do |line|
  line.start_with?("| `saml.redirect_signature_algorithm` |")
end.join
expect_protocol_fixture_rejection!("missing SAML Redirect SigAlg", "SAML HTTP-Redirect SigAlg policy is missing or unsupported") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: missing_redirect_sigalg))
end
unsupported_redirect_sigalg = semantic_inputs.fetch(:configuration).sub(
  SAML_REDIRECT_SIGNATURE_ALGORITHM,
  "http://www.w3.org/2000/09/xmldsig#rsa-sha1"
)
expect_protocol_fixture_rejection!("unsupported SAML Redirect SigAlg", "SAML HTTP-Redirect SigAlg policy is missing or unsupported") do
  protocol_semantic_errors(**semantic_inputs.merge(configuration: unsupported_redirect_sigalg))
end

mutate_public_contract = lambda do |id, &mutation|
  fixture = JSON.parse(semantic_inputs.fetch(:public_contracts))
  mutation.call(fixture.fetch("operations").find { |operation| operation.fetch("id") == id }, fixture)
  JSON.generate(fixture)
end
apple_missing_id_token = mutate_public_contract.call("identity.oauth.callback-form-post") do |operation|
  operation.fetch("request").fetch("fields").reject! { |field| field.fetch("name") == "IDToken" }
end
expect_protocol_fixture_rejection!("Apple callback missing ID token", "Apple form-post callback contract omits the bound code, ID token, state or front-channel validation") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: apple_missing_id_token))
end
apple_without_frontchannel_validation = mutate_public_contract.call("identity.oauth.callback-form-post") do |operation|
  operation["semantics"] = "Apple Form Post code, ID token and state without front-channel claim validation."
end
expect_protocol_fixture_rejection!("Apple callback without front-channel validation", "Apple form-post callback contract omits the bound code, ID token, state or front-channel validation") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: apple_without_frontchannel_validation))
end
oauth_missing_issuer = mutate_public_contract.call("identity.sso.oauth-callback") do |operation|
  operation.fetch("request").fetch("fields").reject! { |field| field.fetch("name") == "Issuer" }
end
expect_protocol_fixture_rejection!("enterprise OAuth callback missing issuer", "enterprise OAuth callback omits RFC 9207 issuer validation") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: oauth_missing_issuer))
end
oauth_without_expected_issuer_comparison = mutate_public_contract.call("identity.sso.oauth-callback") do |operation|
  operation["semantics"] = "Code/error/state plus RFC 9207 issuer without transaction comparison."
end
expect_protocol_fixture_rejection!("enterprise OAuth callback without expected issuer comparison", "enterprise OAuth callback omits RFC 9207 issuer validation") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: oauth_without_expected_issuer_comparison))
end
{
  "front-channel cookie without Secure" => ["with `Secure`,\n  `HttpOnly`", "with `HttpOnly`"],
  "front-channel cookie without HttpOnly" => ["`Secure`,\n  `HttpOnly`, `SameSite=None`", "`Secure`, `SameSite=None`"],
  "front-channel cookie without SameSite=None" => ["`SameSite=None`", "`SameSite=Lax`"],
  "front-channel cookie with Domain" => ["`SameSite=None`, no `Domain`, an exact Apple or SAML callback", "`SameSite=None`, shared `Domain`, an exact Apple or SAML callback"],
  "front-channel cookie without exact callback Path" => ["an exact Apple or SAML callback\n  `Path`", "`Path=/`"],
  "front-channel cookie without bounded lifetime" => ["a five-minute maximum lifetime", "an unbounded lifetime"],
  "front-channel cookie without one-use binding" => ["one-time flow binding", "reusable flow binding"],
  "front-channel cookie reused for normal flows" => ["issued only for the selected cross-site POST flow", "issued for every flow"],
  "front-channel cookie authenticates a session" => ["It never authenticates a session", "It authenticates a session"],
  "normal session cookie weakened" => ["cookies remain\n  `SameSite=Lax`", "cookies use `SameSite=None`"]
}.each do |label, (selected, weakened)|
  fixture = semantic_inputs.fetch(:reference_profile).sub(selected, weakened)
  expect_protocol_fixture_rejection!(label, "cross-site POST flow correlation cookie contract is absent or weakens the normal session cookie") do
    protocol_semantic_errors(**semantic_inputs.merge(reference_profile: fixture))
  end
end
keyed_dynamic_registration = semantic_inputs.fetch(:api_operations).sub(
  /^(\| `identity\.oauth-server\.dynamic-register` \|.*?\| protocol \/ )protocol-command( \|.*)$/,
  '\\1keyed\\2'
)
expect_protocol_fixture_rejection!("client-keyed RFC 7591 registration", "identity.oauth-server.dynamic-register does not use protocol-command replay identity") do
  protocol_semantic_errors(**semantic_inputs.merge(api_operations: keyed_dynamic_registration))
end
keyed_saml_start = semantic_inputs.fetch(:api_operations).sub(
  /^(\| `identity\.sso\.saml-start` \|.*?\| provider \/ )protocol-command( \|.*)$/,
  '\\1keyed\\2'
)
expect_protocol_fixture_rejection!("client-keyed browser SAML start", "identity.sso.saml-start does not use protocol-command replay identity") do
  protocol_semantic_errors(**semantic_inputs.merge(api_operations: keyed_saml_start))
end
missing_saml_start_command = mutate_public_contract.call("identity.sso.saml-start") do |operation|
  operation.fetch("request").fetch("fields").reject! { |field| field.fetch("name") == "CommandID" }
end
expect_protocol_fixture_rejection!("SAML start public contract missing protocol command", "identity.sso.saml-start public contract omits protocol-command replay identity") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: missing_saml_start_command))
end
missing_public_false = mutate_public_contract.call("identity.oauth-server.client-create") do |operation|
  operation.fetch("request").fetch("fields").reject! { |field| field.fetch("name") == "Public" }
end
expect_protocol_fixture_rejection!("confidential client discriminator omitted", "identity.oauth-server.client-create does not explicitly represent Public=false confidential clients") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: missing_public_false))
end
invalid_client_public_false = mutate_public_contract.call("identity.oauth-server.client-create") do |_operation, fixture|
  unit = fixture.fetch("units").find { |candidate| candidate.fetch("unit") == "oauth-server" }
  public_field = unit.fetch("types").find { |type| type.fetch("name") == "Client" }.fetch("fields").find { |field| field.fetch("name") == "Public" }
  public_field["zero_value"] = "invalid false"
end
expect_protocol_fixture_rejection!("oauthserver Client rejects confidential false", "oauthserver.Client.Public=false is not a valid confidential-client state") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: invalid_client_public_false))
end
inactive_introspection_rejected = mutate_public_contract.call("identity.oauth-server.introspect") do |operation|
  operation.fetch("result").fetch("fields").find { |field| field.fetch("name") == "Active" }["zero_value"] = "invalid false"
end
expect_protocol_fixture_rejection!("introspection rejects inactive false", "OAuth introspection cannot represent RFC 7662 Active=false") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: inactive_introspection_rejected))
end
extra_introspection_metadata = mutate_public_contract.call("identity.oauth-server.introspect") do |operation|
  operation.fetch("result").fetch("fields") << {
    "name" => "TokenMetadata", "type" => "string", "required" => false,
    "semantics" => "Additional token metadata.", "zero_value" => "absent when zero"
  }
end
expect_protocol_fixture_rejection!("introspection adds unclosed metadata", "OAuth introspection result field closure drifted") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: extra_introspection_metadata))
end
%w[ClientID Subject Scopes Audience ExpiresAt].each do |field_name|
  required_inactive_metadata = mutate_public_contract.call("identity.oauth-server.introspect") do |operation|
    field = operation.fetch("result").fetch("fields").find { |candidate| candidate.fetch("name") == field_name }
    field["required"] = true
    field["zero_value"] = "invalid when absent"
  end
  expect_protocol_fixture_rejection!("inactive introspection requires #{field_name}", "OAuth inactive introspection metadata is not optional: #{field_name}") do
    protocol_semantic_errors(**semantic_inputs.merge(public_contracts: required_inactive_metadata))
  end
  inactive_disclosure_undefined = mutate_public_contract.call("identity.oauth-server.introspect") do |operation|
    field = operation.fetch("result").fetch("fields").find { |candidate| candidate.fetch("name") == field_name }
    field["semantics"] = "Optional metadata without inactive-token disclosure policy."
  end
  expect_protocol_fixture_rejection!("inactive introspection disclosure undefined for #{field_name}", "OAuth inactive introspection metadata is not optional: #{field_name}") do
    protocol_semantic_errors(**semantic_inputs.merge(public_contracts: inactive_disclosure_undefined))
  end
end
weakened_inactive_acceptance = semantic_inputs.fetch(:protocol).sub(
  "inactive or unknown token as a normal\nsuccessful response with `active=false`",
  "inactive or unknown token as an error"
)
expect_protocol_fixture_rejection!("inactive introspection acceptance weakened", "OAuth RFC 7662 inactive-token acceptance semantics drifted") do
  protocol_semantic_errors(**semantic_inputs.merge(protocol: weakened_inactive_acceptance))
end
logout_boolean_contradiction = mutate_public_contract.call("identity.sso.oidc-logout") do |operation|
  operation.fetch("result").fetch("fields") << {"name" => "Succeeded", "type" => "bool", "required" => true, "semantics" => "contradictory", "zero_value" => "invalid"}
end
expect_protocol_fixture_rejection!("OIDC logout required Boolean", "identity.sso.oidc-logout does not use one exclusive logout outcome") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: logout_boolean_contradiction))
end
%w[identity.sso.oidc-logout identity.sso.oidc-logout-complete].each do |id|
  missing_exclusive_outcome = mutate_public_contract.call(id) do |operation|
    operation.fetch("result").fetch("fields").find { |field| field.fetch("name") == "Outcome" }["semantics"] = "Ambiguous outcome."
  end
  expect_protocol_fixture_rejection!("#{id} ambiguous outcome", "#{id} does not use one exclusive logout outcome") do
    protocol_semantic_errors(**semantic_inputs.merge(public_contracts: missing_exclusive_outcome))
  end
end
required_relay_state = mutate_public_contract.call("identity.sso.saml-idp-init") do |operation|
  operation.fetch("request").fetch("fields").find { |field| field.fetch("name") == "RelayState" }["required"] = true
end
expect_protocol_fixture_rejection!("required SAML RelayState", "identity.sso.saml-idp-init incorrectly requires RelayState") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: required_relay_state))
end
%w[identity.sso.saml-acs identity.sso.saml-slo identity.sso.saml-start].each do |id|
  required_relay = mutate_public_contract.call(id) do |operation|
    operation.fetch("request").fetch("fields").find { |field| field.fetch("name") == "RelayState" }["required"] = true
  end
  expect_protocol_fixture_rejection!("#{id} required RelayState", "#{id} incorrectly requires RelayState") do
    protocol_semantic_errors(**semantic_inputs.merge(public_contracts: required_relay))
  end
end
required_start_result_relay = mutate_public_contract.call("identity.sso.saml-start") do |operation|
  operation.fetch("result").fetch("fields").find { |field| field.fetch("name") == "RelayState" }["required"] = true
end
expect_protocol_fixture_rejection!("SAML start result required RelayState", "identity.sso.saml-start result incorrectly requires RelayState") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: required_start_result_relay))
end
single_replay_consume = mutate_public_contract.call("identity.sso.saml-acs") do |_operation, fixture|
  unit = fixture.fetch("units").find { |candidate| candidate.fetch("unit") == "sso/saml" }
  method = unit.fetch("interfaces").find { |interface| interface.fetch("name") == "ReplayStore" }.fetch("methods").first
  method["name"] = "Consume"
end
expect_protocol_fixture_rejection!("non-atomic SAML replay consume", "SAML replay authority does not atomically reserve the complete response/assertion ID set") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: single_replay_consume))
end
undeclared_saml_type = mutate_public_contract.call("identity.sso.saml-acs") do |_operation, fixture|
  operation = fixture.fetch("operations").find { |candidate| candidate.fetch("id") == "identity.sso.saml-idp-init" }
  operation.fetch("request").fetch("fields").find { |field| field.fetch("name") == "Confirmation" }["type"] = "UndeclaredLoginConfirmation"
end
expect_protocol_fixture_rejection!("undeclared SAML public type", "SAML public contract references undeclared types") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: undeclared_saml_type))
end
impossible_idp_init = mutate_public_contract.call("identity.sso.saml-idp-init") do |operation|
  operation.fetch("authorization")["access"] = "pre-auth-bound RelayState"
end
expect_protocol_fixture_rejection!("IdP-init pre-auth requirement", "SAML IdP-initiated login requires impossible pre-auth or RelayState") do
  protocol_semantic_errors(**semantic_inputs.merge(public_contracts: impossible_idp_init))
end
missing_slo_clause = JSON.parse(JSON.generate(semantic_inputs.fetch(:conformance)))
missing_slo_clause.fetch("clause_pins").reject! { |pin| pin.fetch("requirement_id") == "saml.single-logout-protocol" }
expect_protocol_fixture_rejection!("missing SAML SLO clause pin", "SAML Single Logout clause pins are incomplete") do
  protocol_semantic_errors(**semantic_inputs.merge(conformance: missing_slo_clause))
end

preflight = artifacts.fetch("PREFLIGHT_EVIDENCE.md")
[
  "| Field | Value |",
  "| Requirement/profile | Required by units or claims | Classification | Version/environment identity | Evidence path or blocking claim |",
  "| Safe profile ID | Consuming units | Exact acceptance claim IDs | Classification | Credential source metadata | Evidence path or blocker | Evidence record commit | Evidence digest or blocker |",
  "| Primitive | Consuming units | Registered module/package | API input fingerprint | Gate fingerprint and result | Evidence path |",
  "| Resource ID | Type | Owning unit/task | Exact path or safe external ID | State | Cleanup trigger | Last reconciled at | Cleanup evidence or attestation |",
  "| Revision ID | Unit | Previous goal digest | Current goal digest | Status | Authorized by | Recorded at |",
  "| Recovery epoch | Unit | Generation | Integration commit | Worker checkpoint | Conflict evidence path | Status | Recorded at |"
].each do |header|
  fail_check("preflight evidence schema missing: #{header}") unless preflight.lines.any? { |line| line.chomp == header }
end
effective_preflight = execution_mode ? preflight_snapshot : preflight
task_owned_worktree_parent = effective_preflight.lines.filter_map do |line|
  next unless line.start_with?("| Task-owned worktree parent |")

  line.split("|").map(&:strip)[2].delete("`")
end.first
readme_waves = {}
wave = nil
readme.each_line do |line|
  if (wave_match = line.match(/^### Wave (\d+)$/))
    wave = wave_match[1].to_i
  end
  next unless wave && (match = line.match(/^- `([^`]+)`$/))
  fail_check("duplicate README wave unit #{match[1]}") if readme_waves.key?(match[1])
  readme_waves[match[1]] = wave
end
fail_check("README waves do not contain every unit exactly once") unless readme_waves.keys.to_set == known
depth.each { |unit, value| fail_check("README wave for #{unit}: expected #{value}, got #{readme_waves[unit]}") unless readme_waves[unit] == value }

parity = File.read(File.join(ROOT, "BETTER_AUTH_PARITY.md"))
fail_check("pinned Better Auth baseline missing") unless parity.include?(BASELINE)
disposition_items = []
disposition_semantic_lines = []
disposition_section = nil
artifacts.fetch("UPSTREAM_DISPOSITIONS.md").each_line do |line|
  if (heading_match = line.match(/^## (.+)$/))
    disposition_section = heading_match[1]
  end
  next unless line.start_with?("| ") && !line.start_with?("| ---")

  cells = line.split("|").map(&:strip)
  next if cells[1].nil? || cells[1].match?(/\A(?:Pinned |Source |Profile$|Official |Provider |Package )/)

  disposition_items << [disposition_section, cells[1].delete("`")].join("\t")
  disposition_semantic_lines << [
    disposition_section,
    cells[1].delete("`"),
    cells[2].delete("`"),
    cells[3].delete("`")
  ].join("\t")
end
disposition_inventory = upstream_manifest.fetch("disposition_inventory")
fail_check("pinned disposition inventory source drifted") unless disposition_inventory.fetch("source") == "UPSTREAM_DISPOSITIONS.md"
fail_check("pinned disposition count drifted") unless disposition_inventory.fetch("count") == disposition_items.length
fail_check("pinned disposition digest drifted") unless disposition_inventory.fetch("sha256") == canonical_inventory_digest(disposition_semantic_lines)

canonical_capabilities = parity.lines.filter_map do |line|
  cells = line.split("|").map(&:strip)
  cells[1] if cells.length >= 6 && cells[2] == "In"
end.sort
capability_inventory = upstream_manifest.fetch("capability_inventory")
fail_check("pinned capability inventory source drifted") unless capability_inventory.fetch("source") == "BETTER_AUTH_PARITY.md"
fail_check("pinned capability count drifted") unless capability_inventory.fetch("count") == canonical_capabilities.length
fail_check("pinned capability digest drifted") unless capability_inventory.fetch("sha256") == canonical_inventory_digest(canonical_capabilities)

item_closure = upstream_manifest.fetch("item_closure")
closure_rows = item_closure.fetch("rows")
fail_check("upstream closure row count drifted") unless item_closure.fetch("row_count") == disposition_items.length && closure_rows.length == disposition_items.length
closure_row_ids = closure_rows.map { |record| record.fetch("disposition_row_id") }
fail_check("upstream closure disposition rows drifted") unless closure_row_ids == disposition_items
fail_check("upstream closure disposition rows are not unique") unless closure_row_ids.uniq == closure_row_ids
upstream_item_ids = closure_rows.map { |record| record.fetch("upstream_item_id") }
fail_check("upstream closure item IDs are not unique") unless upstream_item_ids.uniq == upstream_item_ids

source_by_section = {
  "Core documentation and route surface" => lambda do |item|
    if item.start_with?("concepts ")
      "docs/content/docs/concepts"
    elsif item.start_with?("authentication ")
      "docs/content/docs/authentication"
    else
      "packages/better-auth/src/api/routes"
    end
  end,
  "Official plugin documentation tree" => ->(_item) { "docs/content/docs/plugins" },
  "Source-exported and internal plugin surface" => ->(_item) { "packages/better-auth/src/plugins" },
  "Official top-level packages" => ->(_item) { "packages" },
  "Provider catalog disposition" => lambda do |item|
    item.include?("helper") ? "packages/better-auth/src/plugins" : "packages/core/src/social-providers"
  end
}.freeze
allowed_closure_classifications = Set[
  "in-scope", "provider-capability", "non-capability", "client-surface",
  "deployment-profile-divergence", "security-divergence",
  "superseded-contract", "product-exclusion", "in-scope+non-capability",
  "in-scope+deployment-profile-divergence", "in-scope+security-divergence"
].freeze
in_scope_classifications = Set[
  "in-scope", "provider-capability", "security-divergence",
  "superseded-contract", "in-scope+non-capability",
  "in-scope+deployment-profile-divergence", "in-scope+security-divergence"
].freeze
closure_rows.each do |record|
  section, item = record.fetch("disposition_row_id").split("\t", 2)
  source_resolver = source_by_section[section]
  fail_check("upstream closure row names unknown section: #{section}") unless source_resolver
  expected_source = source_resolver.call(item)
  fail_check("upstream closure source misassigned for #{record.fetch('disposition_row_id')}") unless record.fetch("source_path") == expected_source
  source_locator = record.fetch("source_locator")
  fail_check("upstream closure locator escapes pinned source for #{record.fetch('disposition_row_id')}") unless source_locator == expected_source || source_locator.start_with?("#{expected_source}/")
  fail_check("upstream closure source kind is invalid for #{record.fetch('disposition_row_id')}") unless %w[blob tree].include?(record.fetch("source_kind"))
  fail_check("upstream closure source object ID is invalid for #{record.fetch('disposition_row_id')}") unless record.fetch("source_object_id").match?(/\A[0-9a-f]{40}\z/)
  expected_item_id = if section == "Source-exported and internal plugin surface" && !["internal additional-fields", "plugin source index"].include?(item)
                       "#{source_locator}##{item}"
                     else
                       source_locator
                     end
  fail_check("upstream closure item ID misassigned for #{record.fetch('disposition_row_id')}") unless record.fetch("upstream_item_id") == expected_item_id
  if section == "Source-exported and internal plugin surface" && !["internal additional-fields", "plugin source index"].include?(item)
    fail_check("upstream export locator drifted for #{item}") unless source_locator == "packages/better-auth/src/plugins/index.ts"
  end
  classification = record.fetch("classification")
  fail_check("upstream closure classification is invalid for #{record.fetch('disposition_row_id')}") unless allowed_closure_classifications.include?(classification)
  capability_ids = record.fetch("capability_ids")
  exact_operation_ids = record.fetch("operation_ids")
  fail_check("upstream closure capabilities are not sorted and unique for #{record.fetch('disposition_row_id')}") unless capability_ids == capability_ids.sort.uniq
  fail_check("upstream closure operations are not sorted and unique for #{record.fetch('disposition_row_id')}") unless exact_operation_ids == exact_operation_ids.sort.uniq
  unknown_capabilities = capability_ids - canonical_capabilities
  unknown_operations = exact_operation_ids - canonical_operation_ids
  fail_check("upstream closure has unknown capabilities for #{record.fetch('disposition_row_id')}: #{unknown_capabilities}") unless unknown_capabilities.empty?
  fail_check("upstream closure has unknown operations for #{record.fetch('disposition_row_id')}: #{unknown_operations}") unless unknown_operations.empty?
  if in_scope_classifications.include?(classification)
    fail_check("in-scope upstream row lacks capability closure: #{record.fetch('disposition_row_id')}") if capability_ids.empty?
    fail_check("in-scope upstream row lacks exact operation closure: #{record.fetch('disposition_row_id')}") if exact_operation_ids.empty?
  else
    fail_check("non-capability upstream row gained capability closure: #{record.fetch('disposition_row_id')}") unless capability_ids.empty?
    fail_check("non-capability upstream row gained operation closure: #{record.fetch('disposition_row_id')}") unless exact_operation_ids.empty?
  end
end

fail_check("upstream leaf inventory schema drifted") unless upstream_leaves_manifest.fetch("schema_version") == 1
fail_check("upstream leaf inventory revision drifted") unless upstream_leaves_manifest.fetch("upstream") == upstream
leaf_sources = upstream_leaves_manifest.fetch("sources")
fail_check("upstream leaf source count drifted") unless upstream_leaves_manifest.fetch("source_count") == upstream_sources.length && leaf_sources.length == upstream_sources.length
leaf_source_paths = leaf_sources.map { |source| source.fetch("path") }
fail_check("upstream leaf sources are not sorted and unique") unless leaf_source_paths == leaf_source_paths.sort_by(&:b).uniq

tree_id_for = lambda do |root_path, entries, recursive|
  entries_by_parent = entries.group_by { |entry| File.dirname(entry.fetch("path")) }
  computed = {}
  tree_paths = if recursive
                 entries.select { |entry| entry.fetch("kind") == "tree" }.map { |entry| entry.fetch("path") }
               else
                 []
               end
  (tree_paths + [root_path]).uniq.sort_by { |path| [-path.count("/"), path.b] }.each do |tree_path|
    children = entries_by_parent.fetch(tree_path, [])
    fail_check("upstream leaf tree has no children: #{tree_path}") if children.empty?
    content = children.sort_by do |entry|
      suffix = entry.fetch("kind") == "tree" ? "/" : ""
      "#{File.basename(entry.fetch('path'))}#{suffix}".b
    end.map do |entry|
      child_id = if entry.fetch("kind") == "tree" && recursive
                   computed.fetch(entry.fetch("path"))
                 else
                   entry.fetch("object_id")
                 end
      mode = entry.fetch("kind") == "tree" ? entry.fetch("mode").delete_prefix("0") : entry.fetch("mode")
      "#{mode} #{File.basename(entry.fetch('path'))}\0".b + [child_id].pack("H*")
    end.join
    computed[tree_path] = Digest::SHA1.hexdigest("tree #{content.bytesize}\0".b + content)
    next if tree_path == root_path

    recorded = entries.find { |entry| entry.fetch("path") == tree_path }
    fail_check("upstream nested tree object substituted: #{tree_path}") unless recorded && recorded.fetch("object_id") == computed.fetch(tree_path)
  end
  computed.fetch(root_path)
end

expected_leaf_identities = []
canonical_source_lines = []
leaf_sources.each do |source|
  path = source.fetch("path")
  expected_source = upstream_sources.fetch(path)
  fail_check("upstream leaf source kind drifted: #{path}") unless source.fetch("kind") == expected_source.fetch(0)
  fail_check("upstream leaf source object drifted: #{path}") unless source.fetch("object_id") == expected_source.fetch(1)
  fail_check("upstream leaf source policy drifted: #{path}") unless source.fetch("enumeration") == expected_source.fetch(3)
  entries = source.fetch("entries")
  enumeration = source.fetch("enumeration")
  entry_paths = entries.map { |entry| entry.fetch("path") }
  fail_check("upstream leaf source entries are not sorted and unique: #{path}") unless entry_paths == entry_paths.sort_by(&:b).uniq
  entries.each do |entry|
    entry_path = entry.fetch("path")
    inside_source = entry_path.start_with?("#{path}/") || (enumeration == "exact_blob" && entry_path == path)
    fail_check("upstream leaf source entry escapes source: #{entry_path}") unless inside_source
    fail_check("upstream leaf source entry kind is invalid: #{entry_path}") unless %w[blob tree].include?(entry.fetch("kind"))
    fail_check("upstream leaf source entry object is invalid: #{entry_path}") unless entry.fetch("object_id").match?(/\A[0-9a-f]{40}\z/)
    fail_check("upstream leaf source tree mode drifted: #{entry_path}") if entry.fetch("kind") == "tree" && entry.fetch("mode") != "040000"
    canonical_source_lines << ["source", path, source.fetch("enumeration"), entry_path, entry.fetch("mode"), entry.fetch("kind"), entry.fetch("object_id")].join("\t")
  end
  recursive = enumeration == "recursive_blobs"
  if source.fetch("kind") == "tree"
    fail_check("upstream source tree object substitution: #{path}") unless tree_id_for.call(path, entries, recursive) == source.fetch("object_id")
  else
    exact_entry = entries.one? && entries.first
    exact_valid = exact_entry && exact_entry.fetch("path") == path && exact_entry.fetch("kind") == "blob" &&
      exact_entry.fetch("object_id") == source.fetch("object_id")
    fail_check("upstream exact blob object substitution: #{path}") unless exact_valid
  end
  selected = if recursive
               entries.select { |entry| entry.fetch("kind") == "blob" }
             else
               entries
             end
  expected_leaf_identities.concat(selected.map { |entry| [path, entry.fetch("path"), entry.fetch("kind"), entry.fetch("object_id")] })
end

leaf_dispositions = upstream_leaves_manifest.fetch("leaf_dispositions")
fail_check("upstream leaf count drifted") unless upstream_leaves_manifest.fetch("leaf_count") == expected_leaf_identities.length && leaf_dispositions.length == expected_leaf_identities.length
actual_leaf_identities = leaf_dispositions.map { |leaf| [leaf.fetch("source_path"), leaf.fetch("path"), leaf.fetch("kind"), leaf.fetch("object_id")] }
fail_check("upstream leaf identities are not sorted and unique") unless actual_leaf_identities == actual_leaf_identities.sort_by { |identity| [identity.fetch(1).b, identity.fetch(0).b] }.uniq
fail_check("upstream leaf set drifted") unless actual_leaf_identities.to_set == expected_leaf_identities.to_set

leaf_dispositions.combination(2) do |left, right|
  ancestor, descendant = [left, right].sort_by { |leaf| leaf.fetch("path").length }
  next unless descendant.fetch("path").start_with?("#{ancestor.fetch('path')}/")
  catalog_containment = ancestor.fetch("source_path") == "packages" && ancestor.fetch("kind") == "tree" && descendant.fetch("source_path") != "packages"
  fail_check("unexpected ancestor-overlapping upstream leaves: #{ancestor.fetch('path')} and #{descendant.fetch('path')}") unless catalog_containment
end

semantic_by_id = closure_rows.to_h { |row| [row.fetch("disposition_row_id"), row] }
leaf_dispositions.each do |leaf|
  path = leaf.fetch("path")
  disposition_row_id = leaf.fetch("disposition_row_id")
  semantic = semantic_by_id[disposition_row_id]
  fail_check("upstream leaf has unknown disposition row: #{path}") unless semantic
  fail_check("upstream leaf classification drifted: #{path}") unless leaf.fetch("classification") == semantic.fetch("classification")
  fail_check("upstream leaf capability closure drifted: #{path}") unless leaf.fetch("capability_ids") == semantic.fetch("capability_ids")
  fail_check("upstream leaf operation closure drifted: #{path}") unless leaf.fetch("operation_ids") == semantic.fetch("operation_ids")
  provider_implementation_path = if leaf.fetch("source_path") == "packages/core/src/social-providers" && path != "packages/core/src/social-providers/index.ts"
                                   path.sub(/\.test\.ts\z/, ".ts")
                                 elsif path.start_with?("packages/better-auth/src/plugins/generic-oauth/providers/") && !path.end_with?("/index.ts")
                                   path.sub(/\.test\.ts\z/, ".ts")
                                 end
  next unless provider_implementation_path

  exact_provider_rows = closure_rows.select { |row| row.fetch("source_locator") == provider_implementation_path }
  fail_check("upstream provider leaf lacks one exact provider disposition: #{path}") unless exact_provider_rows.length == 1
  fail_check("upstream provider leaf is mapped to the wrong provider disposition: #{path}") unless disposition_row_id == exact_provider_rows.fetch(0).fetch("disposition_row_id")
end

leaf_identity_set = actual_leaf_identities.map { |source_path, path, kind, object_id| [source_path, path, kind, object_id] }.to_set
closure_rows.each do |record|
  locator_identity = [record.fetch("source_path"), record.fetch("source_locator"), record.fetch("source_kind"), record.fetch("source_object_id")]
  fail_check("upstream closure locator is not one exact enumerated leaf: #{record.fetch('disposition_row_id')}") unless leaf_identity_set.include?(locator_identity)
end

canonical_leaf_lines = leaf_dispositions.map do |leaf|
  ["leaf", leaf.fetch("source_path"), leaf.fetch("path"), leaf.fetch("kind"), leaf.fetch("object_id"), leaf.fetch("disposition_row_id"), leaf.fetch("classification"), leaf.fetch("capability_ids").join(","), leaf.fetch("operation_ids").join(",")].join("\t")
end
leaf_digest = canonical_inventory_digest(canonical_source_lines + canonical_leaf_lines)
fail_check("upstream leaf inventory digest drifted") unless upstream_leaves_manifest.fetch("sha256") == EXPECTED_UPSTREAM_LEAVES_SHA256 && leaf_digest == EXPECTED_UPSTREAM_LEAVES_SHA256

capability_operations = item_closure.fetch("capability_operations")
referenced_capabilities = closure_rows.flat_map { |record| record.fetch("capability_ids") }.uniq.sort
fail_check("upstream capability-operation keys drifted") unless capability_operations.keys.sort == referenced_capabilities
capability_operation_edges = []
capability_operations.each do |capability_id, exact_operation_ids|
  fail_check("upstream capability operations are not sorted and unique for #{capability_id}") unless exact_operation_ids == exact_operation_ids.sort.uniq
  fail_check("upstream capability lacks exact operation closure: #{capability_id}") if exact_operation_ids.empty?
  missing_operation_ids = exact_operation_ids - canonical_operation_ids
  fail_check("upstream capability #{capability_id} has unknown operations: #{missing_operation_ids}") unless missing_operation_ids.empty?
  exact_operation_ids.each do |operation_id|
    owners = operation_owner_map.fetch(operation_id)
    unresolved = owners.reject { |owner| upstream_manifest.fetch("owner_goal_closure").key?(owner) }
    fail_check("upstream operation #{operation_id} lacks owner/goal closure: #{unresolved}") unless unresolved.empty?
    capability_operation_edges << "#{capability_id}\t#{operation_id}"
  end
end
closure_rows.each do |record|
  allowed_operation_ids = record.fetch("capability_ids").flat_map { |capability_id| capability_operations.fetch(capability_id) }.uniq
  misassigned_operation_ids = record.fetch("operation_ids") - allowed_operation_ids
  fail_check("upstream row has operations outside its capability closure for #{record.fetch('disposition_row_id')}: #{misassigned_operation_ids}") unless misassigned_operation_ids.empty?
end
unassigned_operation_ids = canonical_operation_ids - capability_operations.values.flatten.uniq
fail_check("canonical operations lack upstream capability closure: #{unassigned_operation_ids}") unless unassigned_operation_ids.empty?
fail_check("upstream capability-operation edge count drifted") unless item_closure.fetch("capability_operation_edge_count") == capability_operation_edges.length
canonical_closure_rows = closure_rows.map do |record|
  [record.fetch("upstream_item_id"), record.fetch("source_locator"), record.fetch("source_kind"), record.fetch("source_object_id"), record.fetch("disposition_row_id"), record.fetch("classification"), record.fetch("capability_ids").join(","), record.fetch("operation_ids").join(",")].join("\t")
end
owner_goal_edges = upstream_manifest.fetch("owner_goal_closure").sort.map { |owner, goal| "#{owner}\t#{goal}" }
operation_owner_edges = operation_owner_map.sort.flat_map do |operation_id, owners|
  owners.map { |owner| "#{operation_id}\t#{owner}" }
end
fail_check("upstream operation-owner edge count drifted") unless item_closure.fetch("operation_owner_edge_count") == operation_owner_edges.length
closure_digest = canonical_inventory_digest(canonical_closure_rows + capability_operation_edges.sort + operation_owner_edges + owner_goal_edges)
unless item_closure.fetch("sha256") == EXPECTED_UPSTREAM_CLOSURE_SHA256 && closure_digest == EXPECTED_UPSTREAM_CLOSURE_SHA256
  fail_check("upstream item closure digest drifted: #{closure_digest}")
end
parity_capabilities = parity.lines.filter_map do |line|
  cells = line.split("|").map(&:strip)
  cells[1] if cells.length >= 6 && cells[2] == "In"
end.to_set
missing_capabilities = REQUIRED_PARITY_CAPABILITIES - parity_capabilities
fail_check("required parity capabilities missing: #{missing_capabilities.to_a}") unless missing_capabilities.empty?
missing_providers = REQUIRED_PROVIDER_NAMES.reject { |name| parity.include?(name) }
fail_check("required provider profiles missing: #{missing_providers.to_a}") unless missing_providers.empty?
disposition_section = parity.split("## Exported plugin disposition", 2)[1].to_s.split(/^## /, 2)[0]
disposition_exports = disposition_section.lines.filter_map do |line|
  cells = line.split("|").map(&:strip)
  next unless cells.length >= 3 && cells[1]&.start_with?("`")
  cells[1].delete("`") if cells[2] && !cells[2].empty?
end.to_set
missing_exports = PINNED_PLUGIN_EXPORTS - disposition_exports
fail_check("pinned plugin exports lack disposition: #{missing_exports.to_a}") unless missing_exports.empty?
in_scope_parity_owners = Set.new
parity.each_line do |line|
  cells = line.split("|").map(&:strip)
  next unless cells.length >= 6 && cells[2] == "In"
  owners = cells[3].scan(/`([^`]+)`/).flatten
  fail_check("in-scope parity row lacks owner: #{cells[1]}") if owners.empty?
  unknown = owners.reject { |owner| known.include?(owner) || EXISTING_OWNERS.include?(owner) }
  fail_check("unknown parity owners for #{cells[1]}: #{unknown.join(', ')}") unless unknown.empty?
  fail_check("in-scope parity row lacks acceptance or verification: #{cells[1]}") if cells[4].empty? || cells[5].empty?
  in_scope_parity_owners.merge(owners)
end
parity.each_line do |line|
  cells = line.split("|").map(&:strip)
  next unless cells.length >= 4 && cells[2] == "Excluded"
  fail_check("excluded parity row lacks rationale: #{cells[1]}") if cells[3].empty?
end
parity_operation_owners = in_scope_parity_owners.select { |owner| known.include?(owner) && !REFERENCE_ADAPTERS.include?(owner) }
missing_operation_owners = parity_operation_owners.reject { |owner| operation_owners.include?(owner) }
fail_check("in-scope parity owners lack API operation closure: #{missing_operation_owners.to_a}") unless missing_operation_owners.empty?

orchestrator = File.read(File.join(ROOT, "ORCHESTRATOR_GOAL.md"))
fail_check("orchestrator contains unresolved placeholder") if orchestrator.match?(/<[^>]+>/)
readme_prompt = readme[/^## Give one goal to one orchestrator.*?```text\n(.*?)```/m, 1]
orchestrator_prompt = orchestrator[/^## Invocation.*?```text\n(.*?)```/m, 1]
fail_check("README lacks the exact orchestrator prompt") unless readme_prompt
fail_check("orchestrator lacks its canonical invocation") unless orchestrator_prompt
fail_check("README orchestrator prompt drifted") unless readme_prompt == orchestrator_prompt
fail_check("README permits per-package prompts") unless readme.include?("MUST NOT\ncreate, paste, or run per-package prompts")

readme_reading = readme[/complete read order.*?is:\n(.*?)(?=^## )/m, 1].to_s
orchestrator_reading = orchestrator[/^## Required reading\n(.*?)(?=^## )/m, 1].to_s
fail_check("README reading order drifted") unless numbered_items(readme_reading) == READING_ORDER
fail_check("orchestrator reading order drifted") unless numbered_items(orchestrator_reading) == READING_ORDER
transition_check = orchestrator[/^## Transition history validation\n(.*?)(?=^## |\z)/m, 1].to_s
transition_check_normalized = transition_check.gsub(/\s+/, " ")
[
  "first-parent", "before commit", "immediately afterward", "Static `validate.rb`",
  "prior-row versions", "assignment generations", "same-status",
  "commit ancestry", "live worktree uniqueness"
].each do |required|
  fail_check("orchestrator transition-check procedure omits #{required}") unless transition_check_normalized.include?(required)
end
[
  "gpt-5.6-sol", "reasoning effort `medium`", "fork_turns: \"none\"",
  "git merge --no-ff", "implemented-unverified", "REFERENCE_PROFILE.md",
  "Interruption and worker-failure recovery", "A unit MUST NOT jump directly",
  "validate.rb --execution", "resume/authorization commit"
].each do |required|
  fail_check("orchestrator missing required operation: #{required}") unless orchestrator.include?(required)
end
worker = File.read(File.join(ROOT, "WORKER_PROMPT.md"))
policy_errors = orchestration_policy_errors(orchestrator, worker)
fail_check(policy_errors.join("; ")) unless policy_errors.empty?
[
  [orchestrator.sub("MUST NOT implement", "MAY implement"), worker],
  [orchestrator, worker.sub("Do not spawn subagents.", "Subagents are permitted.")],
  [orchestrator, worker.sub("modify only <canonical-module-directory>;", "modify the repository;")]
].each_with_index do |(orchestrator_fixture, worker_fixture), index|
  fail_check("orchestration policy negative fixture #{index + 1} was accepted") if orchestration_policy_errors(orchestrator_fixture, worker_fixture).empty?
end
worker_placeholders = worker.scan(/<([^>]+)>/).flatten.to_set
unknown_placeholders = worker_placeholders - ALLOWED_WORKER_PLACEHOLDERS
missing_placeholders = ALLOWED_WORKER_PLACEHOLDERS - worker_placeholders
fail_check("worker placeholder mismatch; unknown=#{unknown_placeholders.to_a} missing=#{missing_placeholders.to_a}") unless unknown_placeholders.empty? && missing_placeholders.empty?
fail_check("worker does not read reference profile") unless worker.include?("REFERENCE_PROFILE.md")
worker_contract_read_order = %w[
  END_STATE_ACCEPTANCE.json ACCEPTANCE_ARTIFACTS.json API_OPERATIONS.md
  OPERATION_SEMANTICS.json PUBLIC_CONTRACTS.json public_contracts.rb
]
worker_contract_positions = worker_contract_read_order.map { |artifact| worker.index(artifact) }
fail_check("worker public-contract reading order drifted") unless worker_contract_positions.all? && worker_contract_positions == worker_contract_positions.sort
unless worker.include?("MUST NOT infer, add, broaden, substitute, or") && worker.include?("expose any public API beyond those exact contracts")
  fail_check("worker prompt permits inferred or additional public APIs")
end

reference_profile = File.read(File.join(ROOT, "REFERENCE_PROFILE.md"))
[
  "12 to 128 bytes", "7-day absolute expiry", "24-hour idle expiry",
  "PKCE S256", "API keys contain at least 32 random bytes",
  "`__Host-identity_session`", "PostgreSQL is authoritative"
].each do |required|
  fail_check("reference profile missing decision: #{required}") unless reference_profile.include?(required)
end

ledger = File.read(ledger_fixture_path || File.join(ROOT, "EXECUTION_LEDGER.md"))
dependency_revision_header = "| Revision ID | Unit | Previous Requires | Current Requires | Affected reverse closure | Reason | Change digest | Approver | Recorded at |"
parse_dependency_revisions = lambda do |body|
  markdown_table(body, "Dependency revisions", dependency_revision_header).map do |cells|
    fail_check("dependency revision row has wrong column count") unless cells.length == 9
    revision_id, unit, previous_requires, current_requires, affected_units, reason, change_digest, approver, recorded_at = cells
    {
      revision_id: plain_cell(revision_id), unit: plain_cell(unit),
      previous_requires: previous_requires.scan(/`([^`]+)`/).flatten,
      current_requires: current_requires.scan(/`([^`]+)`/).flatten,
      affected_units: affected_units.scan(/`([^`]+)`/).flatten,
      reason: plain_cell(reason), change_digest: plain_cell(change_digest),
      approver: plain_cell(approver), recorded_at: plain_cell(recorded_at)
    }
  end
end
dependency_revisions = parse_dependency_revisions.call(ledger)
revision_ids = dependency_revisions.map { |revision| revision[:revision_id] }
fail_check("dependency revision IDs are not unique") unless revision_ids == revision_ids.uniq
dependency_revisions.each do |revision|
  fail_check("dependency revision ID is unsafe") unless revision[:revision_id].match?(/\A[a-zA-Z0-9._-]+\z/)
  fail_check("dependency revision unit is unknown") unless known.include?(revision[:unit])
  [:previous_requires, :current_requires].each do |field|
    values = revision[field]
    fail_check("dependency revision #{revision[:revision_id]} #{field} contains duplicates") unless values == values.uniq
    fail_check("dependency revision #{revision[:revision_id]} #{field} contains unknown units") unless values.all? { |unit| known.include?(unit) }
  end
  fail_check("dependency revision #{revision[:revision_id]} affected_units is not sorted and unique") unless revision[:affected_units] == revision[:affected_units].sort.uniq
  fail_check("dependency revision #{revision[:revision_id]} affected_units contains unknown units") unless revision[:affected_units].all? { |unit| known.include?(unit) }
  fail_check("dependency revision #{revision[:revision_id]} omits its changed unit") unless revision[:affected_units].include?(revision[:unit])
  fail_check("dependency revision #{revision[:revision_id]} reason is unsafe") unless revision[:reason].match?(/\Areason:[a-zA-Z0-9._-]+\z/)
  fail_check("dependency revision #{revision[:revision_id]} approver is not coordinator") unless revision[:approver] == "coordinator"
  fail_check("dependency revision #{revision[:revision_id]} timestamp is invalid") unless rfc3339?(revision[:recorded_at])
  fail_check("dependency revision #{revision[:revision_id]} digest drifted") unless revision[:change_digest] == dependency_revision_digest(revision)
end
dependency_disposition_header = "| Disposition ID | Revision IDs | Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Preservation proof | Preserved commit | Disposition evidence | Evidence digest | Resource dispositions | Recorded at |"
parse_dependency_dispositions = lambda do |body|
  markdown_table(body, "Dependency assignment dispositions", dependency_disposition_header).map do |cells|
    fail_check("dependency assignment disposition row has wrong column count") unless cells.length == 14
    disposition_id, revision_ids_cell, unit, generation, task, branch, worktree, assignment, proof, preserved_commit, evidence_path, evidence_digest, resource_cell, recorded_at = cells
    resource_pairs = resource_cell.scan(/`([^`]+)`/).flatten.map do |value|
      match = value.match(/\A([a-zA-Z0-9._-]+)=(retained-for-recovery|removed)\z/)
      fail_check("dependency assignment disposition resource is invalid") unless match
      [match[1], match[2]]
    end
    fail_check("dependency assignment disposition resource IDs are not sorted and unique") unless resource_pairs.map(&:first) == resource_pairs.map(&:first).sort.uniq
    {
      disposition_id: plain_cell(disposition_id),
      revision_ids: revision_ids_cell.scan(/`([^`]+)`/).flatten,
      unit: plain_cell(unit), generation: plain_cell(generation), task: plain_cell(task),
      branch: plain_cell(branch), worktree: plain_cell(worktree), assignment: plain_cell(assignment),
      proof: plain_cell(proof), preserved_commit: plain_cell(preserved_commit), evidence_path: plain_cell(evidence_path),
      evidence_digest: plain_cell(evidence_digest),
      resource_states: resource_pairs.to_h, recorded_at: plain_cell(recorded_at)
    }
  end
end
dependency_dispositions = parse_dependency_dispositions.call(ledger)
disposition_ids = dependency_dispositions.map { |disposition| disposition[:disposition_id] }
fail_check("dependency assignment disposition IDs are not unique") unless disposition_ids == disposition_ids.uniq
dependency_dispositions.each do |disposition|
  fail_check("dependency assignment disposition ID is unsafe") unless disposition[:disposition_id].match?(/\A[a-zA-Z0-9._-]+\z/)
  fail_check("dependency assignment disposition unit is unknown") unless known.include?(disposition[:unit])
  fail_check("dependency assignment disposition generation is invalid") unless disposition[:generation].match?(/\A\d+\z/)
  fail_check("dependency assignment disposition worker task is unsafe") unless disposition[:task].match?(/\A[a-zA-Z0-9._\/-]+\z/)
  fail_check("dependency assignment disposition branch is unsafe") unless disposition[:branch].match?(%r{\A(?:feature|bugfix|hotfix|release|chore|refactor)/[a-zA-Z0-9._/-]+\z})
  fail_check("dependency assignment disposition worktree is unsafe") unless disposition[:worktree].start_with?("/") && disposition[:worktree] != "/"
  fail_check("dependency assignment disposition assignment commit is invalid") unless disposition[:assignment].match?(/\A[0-9a-f]{40}\z/)
  fail_check("dependency assignment disposition preserved commit is invalid") unless disposition[:preserved_commit].match?(/\A[0-9a-f]{40}\z/)
  fail_check("dependency assignment disposition evidence digest is invalid") unless disposition[:evidence_digest].match?(/\Asha256:[0-9a-f]{64}\z/)
  fail_check("assignment disposition revision IDs are empty or duplicated") unless disposition[:revision_ids].any? && disposition[:revision_ids] == disposition[:revision_ids].uniq
  ordinary_abandonment = disposition[:revision_ids].length == 1 && disposition[:revision_ids].first.match?(/\Aabandonment:[a-zA-Z0-9._-]+\z/)
  fail_check("dependency assignment disposition references an unknown revision") unless ordinary_abandonment || disposition[:revision_ids].all? { |revision_id| revision_ids.include?(revision_id) }
  disposition[:ordinary_abandonment] = ordinary_abandonment
  proof_format_valid = disposition[:proof].match?(/\A(?:clean-(?:checkpoint|baseline):[0-9a-f]{40}|safe-abandonment:reason:[a-zA-Z0-9._-]+)\z/)
  fail_check("dependency assignment disposition preservation proof is invalid") unless proof_format_valid
  fail_check("dependency assignment disposition evidence path is unsafe or missing") unless repository_evidence_path?(disposition[:evidence_path])
  begin
    evidence_source = File.read(File.expand_path(disposition[:evidence_path], REPOSITORY_ROOT))
    disposition[:evidence_record] = JSON.parse(evidence_source)
    fail_check("dependency assignment disposition evidence is not canonical JSON") unless evidence_source == JSON.pretty_generate(disposition[:evidence_record]) + "\n"
    fail_check("dependency assignment disposition evidence digest drifted") unless disposition[:evidence_digest] == "sha256:#{Digest::SHA256.hexdigest(evidence_source)}"
  rescue JSON::ParserError
    fail_check("dependency assignment disposition evidence is invalid JSON")
  end
  evidence = disposition[:evidence_record]
  evidence_identity = {
    "disposition_id" => disposition[:disposition_id], "revision_ids" => disposition[:revision_ids],
    "unit" => disposition[:unit], "generation" => disposition[:generation].to_i,
    "worker_task" => disposition[:task], "branch" => disposition[:branch],
    "worktree" => disposition[:worktree], "assignment_commit" => disposition[:assignment],
    "authorized_by" => "coordinator", "recorded_at" => disposition[:recorded_at]
  }
  fail_check("dependency assignment disposition evidence schema version drifted") unless evidence["schema_version"] == 1
  evidence_identity.each do |field, expected|
    fail_check("dependency assignment disposition evidence #{field} drifted") unless evidence[field] == expected
  end
  evidence_resources = evidence["resources"]
  fail_check("dependency assignment disposition evidence resources are invalid") unless evidence_resources.is_a?(Array)
  evidence_resource_ids = evidence_resources.map { |resource| resource["resource_id"] }
  fail_check("dependency assignment disposition evidence resource IDs are not sorted and unique") unless evidence_resource_ids == evidence_resource_ids.sort.uniq
  evidence_resource_keys = %w[resource_id type owner target previous_state current_state cleanup_evidence pre_removal_clean pre_removal_head]
  fail_check("dependency assignment disposition evidence resource schema drifted") unless evidence_resources.all? { |resource| resource.keys == evidence_resource_keys }
  evidence_resource_states = evidence_resources.to_h { |resource| [resource["resource_id"], resource["current_state"]] }
  fail_check("dependency assignment disposition evidence resource dispositions drifted") unless evidence_resource_states == disposition[:resource_states]
  expected_preservation = if (clean_match = disposition[:proof].match(/\Aclean-(checkpoint|baseline):([0-9a-f]{40})\z/))
                            fail_check("dependency assignment clean proof does not match preserved commit") unless clean_match[2] == disposition[:preserved_commit]
                            {"kind" => "clean-#{clean_match[1]}", "commit" => disposition[:preserved_commit]}
                          else
                            {"kind" => "safe-abandonment", "reason" => disposition[:proof].delete_prefix("safe-abandonment:"),
                             "recoverable_commit" => disposition[:preserved_commit]}
                          end
  fail_check("dependency assignment disposition evidence preservation drifted") unless evidence["preservation"] == expected_preservation
  fail_check("dependency assignment preservation commit does not exist") unless git_commit_exists?(disposition[:preserved_commit])
  fail_check("dependency assignment preservation commit excludes its assignment") unless git_ancestor?(disposition[:assignment], disposition[:preserved_commit])
  fail_check("dependency assignment disposition timestamp is invalid") unless rfc3339?(disposition[:recorded_at])
end
local_gate_binding_header = "| Unit | Generation | Gate execution revision | Evidence path | Evidence record commit | Evidence blob digest | Bound at |"
parse_local_gate_bindings = lambda do |body|
  markdown_table(body, "Local gate evidence bindings", local_gate_binding_header).map do |unit, generation, gate_revision, path, commit, digest, bound_at|
    binding = {
      unit: plain_cell(unit), generation: plain_cell(generation), gate_revision: plain_cell(gate_revision), path: plain_cell(path),
      commit: plain_cell(commit), digest: plain_cell(digest), bound_at: plain_cell(bound_at)
    }
    fail_check("local gate binding has unknown unit") unless known.include?(binding[:unit])
    fail_check("#{binding[:unit]} local gate binding generation is invalid") unless binding[:generation].match?(/\A\d+\z/)
    fail_check("#{binding[:unit]} local gate execution revision is invalid") unless binding[:gate_revision].match?(/\A[0-9a-f]{40}\z/) && git_commit_exists?(binding[:gate_revision])
    expected_path = ".ai/identity-platform/evidence/gates/#{binding[:unit].tr('/', '-')}.json"
    fail_check("#{binding[:unit]} local gate binding path drifted") unless binding[:path] == expected_path
    fail_check("#{binding[:unit]} local gate binding commit is invalid") unless binding[:commit].match?(/\A[0-9a-f]{40}\z/) && git_commit_exists?(binding[:commit])
    committed = git_blob_bytes(binding[:commit], binding[:path])
    fail_check("#{binding[:unit]} local gate binding commit lacks its record") unless committed
    fail_check("#{binding[:unit]} local gate binding commit is not integrated") unless git_ancestor?(binding[:commit], "HEAD")
    fail_check("#{binding[:unit]} local gate binding does not name the first exact record commit") unless committed && first_parent_commit_with_blob(binding[:path], committed) == binding[:commit]
    fail_check("#{binding[:unit]} local gate binding commit excludes its gate execution revision") unless git_ancestor?(binding[:gate_revision], binding[:commit])
    fail_check("#{binding[:unit]} local gate binding digest drifted") unless committed && binding[:digest] == "sha256:#{Digest::SHA256.hexdigest(committed)}"
    fail_check("#{binding[:unit]} local gate binding timestamp is invalid") unless rfc3339?(binding[:bound_at])
    binding
  end
end
local_gate_evidence_bindings = parse_local_gate_bindings.call(ledger)
local_gate_binding_keys = local_gate_evidence_bindings.map { |binding| [binding[:unit], binding[:generation], binding[:gate_revision]] }
fail_check("local gate evidence bindings contain duplicate unit/generation/revision rows") unless local_gate_binding_keys == local_gate_binding_keys.uniq
ledger_header = "| Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Worker commit | Integration checkpoint | Gate execution revision | Gate fingerprint | External evidence | Last transition |"
ledger_rows = parse_execution_ledger(ledger, ledger_header)
worker_runtime_attestations.each do |runtime_attestation|
  ledger_entry = ledger_rows.find do |entry|
    entry[:unit] == runtime_attestation[:unit] && entry[:generation] == runtime_attestation[:generation]
  end
  fail_check("worker runtime attestation has no matching ledger assignment") unless ledger_entry && ledger_entry[:task] != "—"
  worker_runtime_attestation_errors(
    runtime_attestation, unit: runtime_attestation[:unit], generation: runtime_attestation[:generation],
    task: ledger_entry&.fetch(:task, "—")
  ).each { |error| fail_check("#{runtime_attestation[:unit]} #{error}") }
  assignment_authorization = worker_attestations.find do |authorization|
    authorization[:unit] == runtime_attestation[:unit] && authorization[:generation] == runtime_attestation[:generation]
  end
  runtime_commit = first_parent_commit_adding_line(runtime_attestation[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  authorization_commit = assignment_authorization && first_parent_commit_adding_line(
    assignment_authorization[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md"
  )
  fail_check("#{runtime_attestation[:unit]} runtime attestation is not committed") unless runtime_commit
  fail_check("#{runtime_attestation[:unit]} runtime attestation lacks exact assignment authorization ancestry") unless
    runtime_commit && authorization_commit && git_output("rev-parse", "#{runtime_commit}^1") == authorization_commit
  fail_check("#{runtime_attestation[:unit]} worker branch excludes runtime attestation") unless
    runtime_commit && ledger_entry && git_ancestor?(runtime_commit, "refs/heads/#{ledger_entry[:branch]}")
  runtime_paths = runtime_commit ? git_output("diff-tree", "--no-commit-id", "--name-only", "-r", runtime_commit).to_s.lines.map(&:strip) : []
  fail_check("#{runtime_attestation[:unit]} runtime attestation commit changed unexpected paths") unless
    runtime_paths == [".ai/identity-platform/PREFLIGHT_EVIDENCE.md"]
end
if execution_mode
  applicability_document = SharedContractApplicability.load_and_validate!(root: ROOT, units: units)
  ledger_rows.reject { |entry| entry[:task] == "—" }.each do |entry|
    row = rows.find { |candidate| candidate[:unit] == entry[:unit] }
    attestation = worker_attestations.find { |candidate| candidate[:unit] == entry[:unit] && candidate[:generation] == entry[:generation] }
    fail_check("#{entry[:unit]} assignment lacks an authorized rendered-prompt envelope") unless attestation
    fail_check("#{entry[:unit]} assignment remains pending and MUST NOT spawn") if entry[:assignment] == "pending"
    fail_check("#{entry[:unit]} worker assignment is not authorized") unless attestation[:status] == "authorized"
    fail_check("#{entry[:unit]} worker attestation is not coordinator-authorized") unless attestation[:authorized_by] == "coordinator"
    fail_check("#{entry[:unit]} worker attestation assignment commit drifted") unless attestation[:assignment] == entry[:assignment]
    fail_check("#{entry[:unit]} worker attestation model drifted") unless attestation[:model] == "gpt-5.6-sol"
    fail_check("#{entry[:unit]} worker attestation reasoning drifted") unless attestation[:reasoning] == "medium"
    fail_check("#{entry[:unit]} worker attestation fork policy drifted") unless attestation[:fork_turns] == "none"
    fail_check("#{entry[:unit]} worker attestation subagent policy drifted") unless attestation[:subagents] == "false"
    fail_check("#{entry[:unit]} worker attestation package scope drifted") unless attestation[:package_scope] == row[:module]
    reserved = reserved_nested_roots(row[:module], modules, modules_manifest, packages_manifest)
    fail_check("#{entry[:unit]} worker attestation reserved descendants drifted") unless attestation[:reserved] == reserved
    manifest_goal = goal_manifest.fetch("goals").find { |candidate| candidate.fetch("unit") == entry[:unit] }
    assignment_goal_path = File.join(".ai/identity-platform", manifest_goal.fetch("planning_path"))
    fail_check("#{entry[:unit]} worker assignment did not preserve its planning goal path") unless attestation[:goal_path] == assignment_goal_path
    fail_check("#{entry[:unit]} worker attestation baseline commit is missing") unless git_commit_exists?(attestation[:baseline])
    fail_check("#{entry[:unit]} worker attestation assignment is not in its baseline") unless git_ancestor?(entry[:assignment], attestation[:baseline])
    fail_check("#{entry[:unit]} worker attestation baseline is not on integration history") unless git_ancestor?(attestation[:baseline], "HEAD")
    fail_check("#{entry[:unit]} worker attestation prompt path is unsafe or missing") unless repository_evidence_path?(attestation[:prompt_path])
    prompt_bytes = File.binread(File.expand_path(attestation[:prompt_path], REPOSITORY_ROOT))
    fail_check("#{entry[:unit]} worker attestation prompt digest drifted") unless attestation[:prompt_digest] == "sha256:#{Digest::SHA256.hexdigest(prompt_bytes)}"

    attestation_commit = first_parent_commit_adding_line(attestation[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
    fail_check("#{entry[:unit]} worker attestation row is not committed on integration history") unless attestation_commit
    if attestation_commit
      attestation_parent = git_output("rev-parse", "#{attestation_commit}^1")
      fail_check("#{entry[:unit]} worker attestation commit does not directly follow its rendered baseline") unless attestation_parent == attestation[:baseline]
      committed_prompt, _prompt_error, prompt_status = Open3.capture3("git", "-C", REPOSITORY_ROOT, "show", "#{attestation_commit}:#{attestation[:prompt_path]}")
      fail_check("#{entry[:unit]} rendered prompt is not committed with its attestation") unless prompt_status.success? && committed_prompt == prompt_bytes
      changed_paths = git_output("diff-tree", "--no-commit-id", "--name-only", "-r", attestation_commit).to_s.lines.map(&:strip)
      required_attestation_paths = [".ai/identity-platform/PREFLIGHT_EVIDENCE.md", attestation[:prompt_path]]
      fail_check("#{entry[:unit]} assignment authorization envelope changed unexpected paths") unless changed_paths.sort == required_attestation_paths.sort
      fail_check("#{entry[:unit]} worker branch does not contain the attestation commit") unless git_ancestor?(attestation_commit, "refs/heads/#{entry[:branch]}")
      committed_goal = git_blob_bytes(attestation_commit, attestation[:goal_path])
      fail_check("#{entry[:unit]} assignment authorization lacks its goal bytes") unless committed_goal
      fail_check("#{entry[:unit]} immutable assignment goal digest drifted") unless committed_goal && attestation[:goal_digest] == "sha256:#{Digest::SHA256.hexdigest(committed_goal)}"
    end

    goal_relative = attestation[:goal_path]
    values = {
      "unit" => entry[:unit], "canonical-module" => row[:module],
      "absolute-worktree-path" => entry[:worktree], "worker-branch" => entry[:branch],
      "integration-commit" => attestation[:baseline], "absolute-goal-path" => File.join(entry[:worktree], goal_relative),
      "verified-prerequisite-list" => (row[:requires].empty? ? "none" : row[:requires].map { |required| prerequisite = ledger_rows.find { |candidate| candidate[:unit] == required }; "- `#{required}` at `#{prerequisite[:checkpoint]}`" }.join("\n")),
      "canonical-module-directory" => row[:module],
      "reserved-descendant-module-directories" => (reserved.empty? ? "none" : reserved.map { |path| "- `#{path}`" }.join("\n")),
      "assignment-generation" => entry[:generation], "assignment-commit" => entry[:assignment],
      "shared-contract-applicability" => SharedContractApplicability.render(document: applicability_document, root: ROOT, unit: entry[:unit]).rstrip
    }
    expected_prompt = worker.dup
    values.each { |placeholder, value| expected_prompt.gsub!("<#{placeholder}>", value) }
    envelope_expected = {
      unit: entry[:unit], generation: entry[:generation], baseline: attestation[:baseline],
      assignment: entry[:assignment], package_scope: row[:module], reserved: reserved,
      goal_digest: attestation[:goal_digest], goal_path: attestation[:goal_path]
    }
    worker_assignment_envelope_errors(attestation, envelope_expected, prompt_bytes, expected_prompt).each do |error|
      fail_check("#{entry[:unit]} #{error}")
    end

    if entry[:worker_commit] != "—" || entry[:checkpoint] != "—"
      runtime_attestation = worker_runtime_attestations.find do |candidate|
        candidate[:unit] == entry[:unit] && candidate[:generation] == entry[:generation]
      end
      fail_check("#{entry[:unit]} worker return or integration lacks a platform runtime attestation") unless runtime_attestation
      worker_runtime_attestation_errors(
        runtime_attestation || {}, unit: entry[:unit], generation: entry[:generation], task: entry[:task]
      ).each { |error| fail_check("#{entry[:unit]} #{error}") }
      if runtime_attestation
        runtime_commit = first_parent_commit_adding_line(runtime_attestation[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
        fail_check("#{entry[:unit]} runtime attestation is not committed on integration history") unless runtime_commit
        fail_check("#{entry[:unit]} runtime attestation precedes assignment authorization") unless runtime_commit && attestation_commit && git_ancestor?(attestation_commit, runtime_commit)
        fail_check("#{entry[:unit]} runtime attestation does not directly follow assignment authorization") unless runtime_commit && attestation_commit && git_output("rev-parse", "#{runtime_commit}^1") == attestation_commit
        fail_check("#{entry[:unit]} worker branch excludes runtime attestation") unless runtime_commit && git_ancestor?(runtime_commit, "refs/heads/#{entry[:branch]}")
        fail_check("#{entry[:unit]} worker commit excludes runtime attestation checkpoint") unless runtime_commit && git_ancestor?(runtime_commit, entry[:worker_commit])
        runtime_paths = git_output("diff-tree", "--no-commit-id", "--name-only", "-r", runtime_commit).to_s.lines.map(&:strip)
        fail_check("#{entry[:unit]} runtime attestation checkpoint changed unexpected paths") unless runtime_paths == [".ai/identity-platform/PREFLIGHT_EVIDENCE.md"]
        repair_authorization = repair_rows_for_validation.reverse.find do |repair|
          repair[:unit] == entry[:unit] && repair[:generation] == entry[:generation] && repair[:status] == "authorized"
        end
        scope_checkpoint = if repair_authorization && entry[:worker_commit] != repair_authorization[:worker_checkpoint]
                             first_parent_commit_adding_line(
                               repair_authorization[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md"
                             )
                           end
        scope_checkpoint ||= runtime_commit
        if scope_checkpoint && git_commit_exists?(entry[:worker_commit])
          fail_check("#{entry[:unit]} worker commit excludes its current authorization checkpoint") unless git_ancestor?(scope_checkpoint, entry[:worker_commit])
          returned_paths = git_output("diff", "--name-only", scope_checkpoint, entry[:worker_commit]).to_s.lines.map(&:strip).reject(&:empty?)
          permitted_paths = returned_paths.all? do |path|
            (path == row[:module] || path.start_with?("#{row[:module]}/")) &&
              reserved.none? { |nested| path == nested || path.start_with?("#{nested}/") }
          end
          fail_check("#{entry[:unit]} authorization-checkpoint-to-worker-tip diff escapes assigned package scope") unless permitted_paths
          worker_range = git_output("rev-list", "--reverse", "--ancestry-path", "#{scope_checkpoint}..#{entry[:worker_commit]}").to_s.lines.map(&:strip)
          worker_range.each do |commit|
            parents = git_output("rev-list", "--parents", "-n", "1", commit).to_s.split.drop(1)
            first_parent = parents.first
            commit_paths = first_parent ? git_output("diff", "--name-only", first_parent, commit).to_s.lines.map(&:strip).reject(&:empty?) : []
            imports_checkpoint = parents.drop(1).include?(scope_checkpoint)
            if imports_checkpoint
              expected_envelope_paths = repair_authorization ? [
                ".ai/identity-platform/INVENTORY.md", ".ai/identity-platform/EXECUTION_LEDGER.md",
                ".ai/identity-platform/PREFLIGHT_EVIDENCE.md", repair_authorization[:prompt_path]
              ] : [".ai/identity-platform/PREFLIGHT_EVIDENCE.md"]
              fail_check("#{entry[:unit]} authorization checkpoint merge has unexpected parents") unless parents.length == 2 && parents[1] == scope_checkpoint
              fail_check("#{entry[:unit]} authorization checkpoint merge contains non-envelope changes") unless commit_paths.sort == expected_envelope_paths.sort
              next
            end
            parent_path_sets = parents.empty? ? [commit_paths] : parents.map do |parent|
              git_output("diff", "--name-only", parent, commit).to_s.lines.map(&:strip).reject(&:empty?)
            end
            commit_in_scope = parent_path_sets.flatten.all? do |path|
              (path == row[:module] || path.start_with?("#{row[:module]}/")) &&
                reserved.none? { |nested| path == nested || path.start_with?("#{nested}/") }
            end
            fail_check("#{entry[:unit]} worker-authored commit #{commit} escapes assigned package scope") unless commit_in_scope
          end
        end
      end
    end
    fail_check("#{entry[:unit]} rendered worker prompt retains an unresolved marker") if prompt_bytes.match?(/<[^>]+>/)
  end
  orphan_attestations = worker_attestations.reject do |attestation|
    ledger_rows.any? { |entry| entry[:task] != "—" && entry[:unit] == attestation[:unit] && entry[:generation] == attestation[:generation] }
  end
  fail_check("worker assignment attestation rows are orphaned") unless orphan_attestations.empty?
  orphan_runtime_attestations = worker_runtime_attestations.reject do |attestation|
    ledger_rows.any? { |entry| entry[:task] != "—" && entry[:unit] == attestation[:unit] && entry[:generation] == attestation[:generation] && entry[:task] == attestation[:task] }
  end
  fail_check("worker runtime attestation rows are orphaned") unless orphan_runtime_attestations.empty?
end
ledger_parser_fixture = <<~MARKDOWN
  ## Dependency revisions

  | Revision ID | Unit | Previous Requires | Current Requires | Affected reverse closure | Reason | Change digest | Approver | Recorded at |
  | --- | --- | --- | --- | --- | --- | --- | --- | --- |
  | `not-a-ledger-unit` | `identity` | — | `identity/phone` | `identity` | `reason:fixture` | `sha256:#{'0' * 64}` | `coordinator` | `2026-08-11T00:00:00Z` |

  ## Unit execution ledger

  #{ledger_header}
  | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
  | `identity` | 0 | — | — | — | — | — | — | — | — | — | initial |
MARKDOWN
ledger_parser_rows = parse_execution_ledger(ledger_parser_fixture, ledger_header)
fail_check("execution ledger scoped-table positive fixture was rejected") unless ledger_parser_rows.map { |row| row[:unit] } == ["identity"]
fail_check("execution ledger parser leaked a dependency revision row") if ledger_parser_rows.any? { |row| row[:unit] == "not-a-ledger-unit" }
ledger_units = ledger_rows.map { |row| row[:unit] }
fail_check("execution ledger units do not exactly match inventory") unless ledger_units == units
fail_check("execution ledger contains duplicate units") unless ledger_units.uniq.length == ledger_units.length
primitive_extension_inventory_errors(
  public_contracts: public_contracts, rows: rows, goal_bodies: goal_bodies, ledger_units: ledger_units
).each { |error| fail_check(error) }
fail_check("execution ledger lacks status/owner mirror rule") unless ledger.include?("mirror")
history_required = execution_mode && execution_history_required?(
  entries: ledger_rows, recovery_rows: recovery_rows_for_validation + repair_rows_for_validation, goal_revision_rows: goal_revision_rows,
  dependency_revisions: dependency_revisions, dependency_dispositions: dependency_dispositions,
  local_gate_bindings: local_gate_evidence_bindings
)
previous_sources = if previous_inventory_path
                     {
                       ".ai/identity-platform/INVENTORY.md" => File.binread(previous_inventory_path),
                       ".ai/identity-platform/EXECUTION_LEDGER.md" => File.binread(previous_ledger_path),
                       ".ai/identity-platform/PREFLIGHT_EVIDENCE.md" => File.binread(previous_execution_fixture_path),
                       ".ai/identity-platform/GOAL_MANIFEST.json" => File.binread(previous_goal_manifest_fixture_path)
                     }
                   else
                     {}
                   end
current_snapshot_fixture_mode = [
  inventory_fixture_path, ledger_fixture_path, execution_fixture_path, goal_manifest_fixture_path
].any?
previous_snapshot_binding_errors(
  previous_sources: previous_sources, history_required: history_required,
  candidate_fixture_mode: current_snapshot_fixture_mode
).each { |error| fail_check(error) }

effective_recoveries = recovery_rows_for_validation.each_with_object({}) do |row, latest|
  key = row.values_at(0, 1, 2, 3).map { |value| plain_cell(value) }
  latest[key] = row
end.values
authorized_recoveries = effective_recoveries.select { |row| plain_cell(row[5]) == "authorized" }
authorized_recoveries.each do |source_row|
  unit_cell, generation, integration_parent, worker_checkpoint, = source_row
  unit = plain_cell(unit_cell)
  integration_parent = plain_cell(integration_parent)
  worker_checkpoint = plain_cell(worker_checkpoint)
  inventory_row = rows.find { |row| row[:unit] == unit }
  ledger_entry = ledger_rows.find { |row| row[:unit] == unit }
  fail_check("authorized recovery #{unit} does not mirror in-progress state") unless inventory_row[:status] == "in-progress"
  fail_check("authorized recovery #{unit} generation does not match ledger") unless plain_cell(generation) == ledger_entry[:generation]
  fail_check("authorized recovery #{unit} checkpoint does not match ledger") unless worker_checkpoint == ledger_entry[:worker_commit]
  fail_check("authorized recovery #{unit} integration parent does not exist") unless git_commit_exists?(integration_parent)
  fail_check("authorized recovery #{unit} worker checkpoint does not exist") unless git_commit_exists?(worker_checkpoint)
  fail_check("authorized recovery #{unit} assignment commit does not exist") unless git_commit_exists?(ledger_entry[:assignment])
  fail_check("authorized recovery #{unit} checkpoint excludes its assignment") unless git_ancestor?(ledger_entry[:assignment], worker_checkpoint)
  row_line = "| #{([source_row[7]] + source_row[0, 7]).join(' | ')} |"
  resume_commit = first_parent_commit_adding_line(row_line, ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  fail_check("authorized recovery #{unit} has no first-parent authorization commit") unless resume_commit
  resume_parent, resume_parent_status = Open3.capture2("git", "-C", REPOSITORY_ROOT, "rev-parse", "#{resume_commit}^1")
  fail_check("authorized recovery #{unit} cannot resolve resume first parent") unless resume_parent_status.success?
  fail_check("authorized recovery #{unit} pins the wrong resume first parent") unless resume_parent.strip == integration_parent
  fail_check("authorized recovery #{unit} resume commit is not on current first-parent history") unless git_ancestor?(resume_commit, "HEAD")
end

active_repairs = repair_rows_for_validation.group_by { |row| row[:epoch] }.filter_map do |_epoch, history|
  history.last if history.last[:status] == "authorized"
end
repair_authorizations = repair_rows_for_validation.select { |repair| repair[:status] == "authorized" }
repair_prompt_paths = repair_authorizations.map { |repair| repair[:prompt_path] }
fail_check("integrated-repair epochs reuse a rendered prompt path") unless repair_prompt_paths == repair_prompt_paths.uniq
repair_authorizations.each do |repair|
  authorization_commit = first_parent_commit_adding_line(repair[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  fail_check("integrated-repair #{repair[:epoch]} lacks its authorization commit") unless authorization_commit
  fail_check("integrated-repair #{repair[:epoch]} pins the wrong first-parent baseline") unless authorization_commit && git_output("rev-parse", "#{authorization_commit}^1") == repair[:baseline]
  committed_prompt = authorization_commit && git_blob_bytes(authorization_commit, repair[:prompt_path])
  fail_check("integrated-repair #{repair[:epoch]} lacks committed prompt bytes") unless committed_prompt
  fail_check("integrated-repair #{repair[:epoch]} committed prompt digest drifted") unless committed_prompt && repair[:prompt_digest] == "sha256:#{Digest::SHA256.hexdigest(committed_prompt)}"
  committed_goal, _goal_error, goal_status = Open3.capture3("git", "-C", REPOSITORY_ROOT, "show", "#{authorization_commit}:#{repair[:goal_path]}") if authorization_commit
  fail_check("integrated-repair #{repair[:epoch]} lacks committed goal bytes") unless authorization_commit && goal_status.success?
  fail_check("integrated-repair #{repair[:epoch]} committed goal digest drifted") unless authorization_commit && goal_status.success? && repair[:goal_digest] == "sha256:#{Digest::SHA256.hexdigest(committed_goal)}"
end
completed_repairs = repair_rows_for_validation.group_by { |repair| repair[:epoch] }.filter_map do |_epoch, history|
  [history.find { |repair| repair[:status] == "authorized" }, history.last] if history.last[:status] == "completed"
end
completed_repairs.each do |authorization, terminal|
  authorization_commit = first_parent_commit_adding_line(authorization[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  fail_check("completed integrated repair #{terminal[:unit]} did not advance worker commit") unless terminal[:result_worker] != authorization[:worker_checkpoint]
  fail_check("completed integrated repair #{terminal[:unit]} did not advance integration checkpoint") unless terminal[:result_checkpoint] != authorization[:checkpoint]
  fail_check("completed integrated repair #{terminal[:unit]} worker commit excludes authorization") unless authorization_commit && git_ancestor?(authorization_commit, terminal[:result_worker])
  fail_check("completed integrated repair #{terminal[:unit]} integration checkpoint excludes repaired worker commit") unless git_ancestor?(terminal[:result_worker], terminal[:result_checkpoint])
  integration_parents = git_output("rev-list", "--parents", "-n", "1", terminal[:result_checkpoint]).to_s.split.drop(1)
  fail_check("completed integrated repair #{terminal[:unit]} result is not the exact authorized non-fast-forward merge") unless integration_parents == [authorization_commit, terminal[:result_worker]]
  terminal_commit = first_parent_commit_adding_line(terminal[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  fail_check("completed integrated repair #{terminal[:unit]} terminal commit does not directly follow its integration checkpoint") unless terminal_commit && git_output("rev-parse", "#{terminal_commit}^1") == terminal[:result_checkpoint]
  ledger_blob, _ledger_error, ledger_status = Open3.capture3("git", "-C", REPOSITORY_ROOT, "show", "#{terminal_commit}:.ai/identity-platform/EXECUTION_LEDGER.md") if terminal_commit
  fail_check("completed integrated repair #{terminal[:unit]} lacks a committed terminal ledger") unless terminal_commit && ledger_status.success?
  if terminal_commit && ledger_status.success?
    terminal_entry = parse_execution_ledger(ledger_blob, ledger_header).find { |entry| entry[:unit] == terminal[:unit] }
    fail_check("completed integrated repair #{terminal[:unit]} terminal ledger result drifted") unless terminal_entry &&
      terminal_entry[:generation] == terminal[:generation] && terminal_entry[:worker_commit] == terminal[:result_worker] &&
      terminal_entry[:checkpoint] == terminal[:result_checkpoint]
  end
end
active_repairs.each do |repair|
  inventory_row = rows.find { |row| row[:unit] == repair[:unit] }
  ledger_entry = ledger_rows.find { |row| row[:unit] == repair[:unit] }
  fail_check("authorized integrated repair #{repair[:unit]} does not mirror in-progress state") unless inventory_row[:status] == "in-progress"
  fail_check("authorized integrated repair #{repair[:unit]} generation drifted") unless ledger_entry[:generation] == repair[:generation]
  fail_check("authorized integrated repair #{repair[:unit]} integration checkpoint drifted") unless ledger_entry[:checkpoint] == repair[:checkpoint]
  fail_check("authorized integrated repair #{repair[:unit]} worker checkpoint drifted") unless ledger_entry[:worker_commit] == repair[:worker_checkpoint]
  manifest_goal = goal_manifest.fetch("goals").find { |candidate| candidate.fetch("unit") == repair[:unit] }
  fail_check("authorized integrated repair #{repair[:unit]} does not use canonical goal") unless repair[:goal_path] == manifest_goal.fetch("canonical_path")
  fail_check("authorized integrated repair #{repair[:unit]} goal digest drifted") unless repair[:goal_digest] == "sha256:#{manifest_goal.fetch('sha256')}"
  inventory_row_module = inventory_row[:module]
  expected_reserved = reserved_nested_roots(inventory_row_module, modules, modules_manifest, packages_manifest)
  fail_check("authorized integrated repair #{repair[:unit]} reserved roots drifted") unless repair[:reserved] == expected_reserved
  prompt_bytes = File.binread(File.expand_path(repair[:prompt_path], REPOSITORY_ROOT)) if repository_evidence_path?(repair[:prompt_path])
  fail_check("authorized integrated repair #{repair[:unit]} prompt digest drifted") unless prompt_bytes && repair[:prompt_digest] == "sha256:#{Digest::SHA256.hexdigest(prompt_bytes)}"
  repair_values = {
    "unit" => repair[:unit], "canonical-module" => inventory_row_module,
    "absolute-worktree-path" => ledger_entry[:worktree], "worker-branch" => ledger_entry[:branch],
    "integration-commit" => repair[:baseline], "absolute-goal-path" => File.join(ledger_entry[:worktree], repair[:goal_path]),
    "verified-prerequisite-list" => (inventory_row[:requires].empty? ? "none" : inventory_row[:requires].map do |required|
      prerequisite = ledger_rows.find { |candidate| candidate[:unit] == required }
      "- `#{required}` at `#{prerequisite[:checkpoint]}`"
    end.join("\n")),
    "canonical-module-directory" => inventory_row_module,
    "reserved-descendant-module-directories" => (expected_reserved.empty? ? "none" : expected_reserved.map { |path| "- `#{path}`" }.join("\n")),
    "assignment-generation" => repair[:generation], "assignment-commit" => ledger_entry[:assignment],
    "shared-contract-applicability" => SharedContractApplicability.render(document: applicability_document, root: ROOT, unit: repair[:unit]).rstrip
  }
  expected_repair_prompt = worker.dup
  repair_values.each { |placeholder, value| expected_repair_prompt.gsub!("<#{placeholder}>", value) }
  fail_check("authorized integrated repair #{repair[:unit]} rendered prompt drifted") unless prompt_bytes == expected_repair_prompt
  authorization_commit = first_parent_commit_adding_line(repair[:row_line], ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  fail_check("authorized integrated repair #{repair[:unit]} lacks an exact authorization commit") unless authorization_commit
  fail_check("authorized integrated repair #{repair[:unit]} pins wrong first-parent baseline") unless authorization_commit && git_output("rev-parse", "#{authorization_commit}^1") == repair[:baseline]
  fail_check("authorized integrated repair #{repair[:unit]} baseline advanced without superseding the epoch") unless authorization_commit == git_output("rev-parse", "HEAD")
  fail_check("authorized integrated repair #{repair[:unit]} checkpoint excludes assignment") unless git_ancestor?(ledger_entry[:assignment], repair[:worker_checkpoint])
  fail_check("authorized integrated repair #{repair[:unit]} branch excludes authorization commit") unless authorization_commit && git_ancestor?(authorization_commit, "refs/heads/#{ledger_entry[:branch]}")
  if authorization_commit
    committed_prompt = git_blob_bytes(authorization_commit, repair[:prompt_path])
    fail_check("authorized integrated repair #{repair[:unit]} prompt was not committed with its row") unless committed_prompt == prompt_bytes
    changed_paths = git_output("diff-tree", "--no-commit-id", "--name-only", "-r", authorization_commit).to_s.lines.map(&:strip)
    required_paths = [
      ".ai/identity-platform/INVENTORY.md", ".ai/identity-platform/EXECUTION_LEDGER.md",
      ".ai/identity-platform/PREFLIGHT_EVIDENCE.md", repair[:prompt_path]
    ]
    fail_check("authorized integrated repair #{repair[:unit]} authorization envelope changed unexpected paths") unless changed_paths.sort == required_paths.sort
  end
end

historical_proposed_entries = ledger_rows.select do |entry|
  inventory_row = rows.find { |row| row[:unit] == entry[:unit] }
  inventory_row[:status] == "proposed" && entry[:transition] != "initial"
end
if historical_proposed_entries.any? && previous_inventory_path.nil?
  fail_check("post-initial proposed rows require previous transition fixtures: #{historical_proposed_entries.map { |entry| entry[:unit] }.join(', ')}")
end

if previous_inventory_path
  previous_inventory = previous_sources.fetch(".ai/identity-platform/INVENTORY.md")
  previous_inventory_rows = previous_inventory.lines.filter_map do |line|
    next unless line.start_with?("| `")
    cells = line.split("|").map(&:strip)
    fail_check("previous inventory row has wrong column count: #{line.chomp}") unless cells.length == 8
    unit = cells[1]&.match(/\A`([^`]+)`\z/)&.[](1)
    {
      unit: unit, module: cells[2][/`([^`]+)`/, 1],
      requires: cells[3].scan(/`([^`]+)`/).flatten,
      status: cells[4], owner: cells[5], goal: cells[6][/`([^`]+)`/, 1]
    } if unit
  end
  previous_ledger = previous_sources.fetch(".ai/identity-platform/EXECUTION_LEDGER.md")
  previous_preflight = previous_sources.fetch(".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  assignment_authorization_header = "| Unit | Generation | Integration baseline | Assignment commit | Assignment goal path | Rendered prompt | Prompt digest | Model | Reasoning | Fork turns | Subagents | Package scope | Reserved descendants | Goal digest | Authorized by | Status |"
  unless markdown_table_append_only?(previous_preflight, preflight_snapshot, "Worker assignment authorizations", assignment_authorization_header)
    fail_check("worker assignment authorization history rows are not preserved exactly")
  end
  runtime_attestation_header = "| Unit | Generation | Worker task | Agent ID | Model | Reasoning | Fork turns | Subagents | Platform source | Recorded at |"
  unless markdown_table_append_only?(previous_preflight, preflight_snapshot, "Worker runtime attestations", runtime_attestation_header)
    fail_check("worker runtime attestation history rows are not preserved exactly")
  end
  goal_revision_header = "| Revision ID | Unit | Previous goal digest | Current goal digest | Status | Authorized by | Recorded at |"
  unless markdown_table_append_only?(previous_preflight, preflight_snapshot, "Goal digest revisions", goal_revision_header)
    fail_check("goal digest revision history rows are not preserved exactly")
  end
  previous_goal_revisions = markdown_table(previous_preflight, "Goal digest revisions", goal_revision_header).map do |revision_id, unit, previous_digest, current_digest, status, authorized_by, recorded_at|
    {
      revision_id: plain_cell(revision_id), unit: plain_cell(unit),
      previous_digest: plain_cell(previous_digest), current_digest: plain_cell(current_digest),
      status: plain_cell(status), authorized_by: plain_cell(authorized_by), recorded_at: plain_cell(recorded_at)
    }
  end
  previous_goal_manifest = JSON.parse(previous_sources.fetch(".ai/identity-platform/GOAL_MANIFEST.json"))
  goal_digest_change_errors(previous_goal_manifest, goal_manifest, previous_goal_revisions, goal_revision_rows).each do |error|
    fail_check(error)
  end
  recovery_header = "| Recovery epoch | Unit | Generation | Integration commit | Worker checkpoint | Conflict evidence path | Status | Recorded at |"
  unless markdown_table_append_only?(previous_preflight, preflight_snapshot, "Conflict-recovery baselines", recovery_header)
    fail_check("conflict-recovery history rows are not preserved exactly")
  end
  previous_recoveries = markdown_table(previous_preflight, "Conflict-recovery baselines", recovery_header).map do |epoch, unit, generation, integration_commit, worker_checkpoint, evidence, status, recorded_at|
    [plain_cell(unit), plain_cell(generation), plain_cell(integration_commit), plain_cell(worker_checkpoint),
     plain_cell(evidence), plain_cell(status), plain_cell(recorded_at), plain_cell(epoch)]
  end
  recovery_transition_errors(previous_recoveries, recovery_rows_for_validation).each { |error| fail_check(error) }
  previous_recovery_raw_rows = markdown_table_raw_rows(previous_preflight, "Conflict-recovery baselines", recovery_header)
  recovery_rows_for_validation.drop(previous_recoveries.length).reject { |row| row[5] == "authorized" }.each do |terminal|
    identity = terminal.values_at(7, 0, 1, 2, 3)
    authorization_index = previous_recoveries.index do |candidate|
      candidate[5] == "authorized" && candidate.values_at(7, 0, 1, 2, 3) == identity
    end
    next unless authorization_index

    authorization_commit = first_parent_commit_adding_line(
      previous_recovery_raw_rows.fetch(authorization_index).chomp,
      ".ai/identity-platform/PREFLIGHT_EVIDENCE.md"
    )
    unless authorization_commit && git_ancestor?(authorization_commit, "HEAD")
      fail_check("recovery terminal authorization is not a preceding first-parent commit")
    end
  end
  repair_header = "| Repair epoch | Unit | Generation | Integration baseline | Integration checkpoint | Worker checkpoint | Goal path | Goal digest | Rendered repair prompt | Prompt digest | Reserved descendants | Result worker commit | Result integration checkpoint | Status | Recorded at |"
  unless markdown_table_append_only?(previous_preflight, preflight_snapshot, "Integrated-repair authorizations", repair_header)
    fail_check("integrated-repair authorization history rows are not preserved exactly")
  end
  previous_repairs = markdown_table(previous_preflight, "Integrated-repair authorizations", repair_header)
  repair_rows_for_validation.drop(previous_repairs.length).reject { |row| row[:status] == "authorized" }.each do |terminal|
    authorization = repair_rows_for_validation.first(previous_repairs.length).find do |candidate|
      candidate[:status] == "authorized" && candidate[:epoch] == terminal[:epoch] &&
        candidate.reject { |key, _| %i[status recorded_at row_line result_worker result_checkpoint].include?(key) } == terminal.reject { |key, _| %i[status recorded_at row_line result_worker result_checkpoint].include?(key) }
    end
    fail_check("integrated-repair terminal lacks a preceding committed exact authorization") unless authorization
  end
  previous_task_owned_resource_rows = parse_dependency_resource_snapshot.call(previous_preflight)
  previous_resource_ids = previous_task_owned_resource_rows.map { |resource| resource[:id] }
  fail_check("previous task-owned resource registry contains duplicate resource IDs") unless previous_resource_ids == previous_resource_ids.uniq
  previous_dependency_revisions = parse_dependency_revisions.call(previous_ledger)
  unless markdown_table_append_only?(previous_ledger, ledger, "Dependency revisions", dependency_revision_header)
    fail_check("dependency revision history rows are not preserved exactly")
  end
  unless dependency_revisions.first(previous_dependency_revisions.length) == previous_dependency_revisions
    fail_check("dependency revision history is not append-only")
  end
  new_dependency_revisions = dependency_revisions.drop(previous_dependency_revisions.length)
  previous_dependency_dispositions = parse_dependency_dispositions.call(previous_ledger)
  previous_dependency_dispositions.each do |disposition|
    disposition[:ordinary_abandonment] = disposition[:revision_ids].length == 1 && disposition[:revision_ids].first.match?(/\Aabandonment:[a-zA-Z0-9._-]+\z/)
  end
  unless markdown_table_append_only?(previous_ledger, ledger, "Dependency assignment dispositions", dependency_disposition_header)
    fail_check("dependency assignment disposition history rows are not preserved exactly")
  end
  unless dependency_dispositions.first(previous_dependency_dispositions.length) == previous_dependency_dispositions
    fail_check("dependency assignment disposition history is not append-only")
  end
  new_assignment_dispositions = dependency_dispositions.drop(previous_dependency_dispositions.length)
  new_dependency_dispositions = new_assignment_dispositions.reject { |disposition| disposition[:ordinary_abandonment] }
  new_ordinary_abandonments = new_assignment_dispositions.select { |disposition| disposition[:ordinary_abandonment] }
  previous_local_gate_bindings = parse_local_gate_bindings.call(previous_ledger)
  unless markdown_table_append_only?(previous_ledger, ledger, "Local gate evidence bindings", local_gate_binding_header)
    fail_check("local gate evidence binding history rows are not preserved exactly")
  end
  unless local_gate_evidence_bindings.first(previous_local_gate_bindings.length) == previous_local_gate_bindings
    fail_check("local gate evidence binding history is not append-only")
  end
  new_local_gate_bindings = local_gate_evidence_bindings.drop(previous_local_gate_bindings.length)
  previous_ledger_rows = parse_execution_ledger(previous_ledger, ledger_header)
  fail_check("previous transition fixture units do not match inventory") unless previous_inventory_rows.map { |row| row[:unit] } == units && previous_ledger_rows.map { |row| row[:unit] } == units
  previous_statuses = previous_inventory_rows.to_h { |row| [row[:unit], row[:status]] }
  previous_generations = previous_ledger_rows.to_h { |row| [row[:unit], row[:generation]] }
  verified_gate_bindings = rows.filter_map do |row|
    previous_row = previous_inventory_rows.find { |candidate| candidate[:unit] == row[:unit] }
    next unless previous_row[:status] == "implemented-unverified" && row[:status] == "verified"

    entry = ledger_rows.find { |candidate| candidate[:unit] == row[:unit] }
    [row[:unit], entry[:generation], entry[:gate_revision]]
  end
  unless new_local_gate_bindings.map { |binding| [binding[:unit], binding[:generation], binding[:gate_revision]] }.sort == verified_gate_bindings.sort
    fail_check("new local gate evidence bindings do not exactly match implemented-unverified to verified transitions")
  end
  previous_ledger_rows.each do |entry|
    inventory_row = previous_inventory_rows.find { |row| row[:unit] == entry[:unit] }
    fail_check("previous #{entry[:unit]} ledger generation is invalid") unless entry[:generation].match?(/\A\d+\z/)
    if entry[:transition] == "initial"
      empty_fields = entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external)
      fail_check("previous #{entry[:unit]} initial row is invalid") unless entry[:generation] == "0" && empty_fields.all? { |value| value == "—" }
      expected_initial = inventory_row[:requires].empty? ? "ready" : "proposed"
      fail_check("previous #{entry[:unit]} initial status must be #{expected_initial}") unless inventory_row[:status] == expected_initial && inventory_row[:owner] == "—"
      next
    end

    match = entry[:transition].match(/\Av(\d+) (#{ALLOWED_STATUSES.to_a.join('|')}) owner=(\S+) at=(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z)\z/)
    fail_check("previous #{entry[:unit]} ledger transition format is invalid") unless match
    fail_check("previous #{entry[:unit]} ledger transition version must be positive") unless match[1].to_i.positive?
    fail_check("previous #{entry[:unit]} ledger status drift") unless match[2] == inventory_row[:status]
    fail_check("previous #{entry[:unit]} ledger owner/blocker drift") unless match[3] == inventory_row[:owner]
    ledger_row_shape_errors(inventory_row, entry).each { |error| fail_check("previous #{entry[:unit]} row invalid: #{error}") }
  end

  requires_changed = rows.any? do |row|
    previous_inventory_rows.find { |candidate| candidate[:unit] == row[:unit] }[:requires] != row[:requires]
  end
  dependency_affected = Set.new
  if requires_changed || new_dependency_revisions.any? || new_dependency_dispositions.any?
    dependency_errors, dependency_affected = dependency_revision_transition_errors(
      previous_rows: previous_inventory_rows, previous_entries: previous_ledger_rows,
      rows: rows, entries: ledger_rows, revisions: new_dependency_revisions,
      dispositions: new_dependency_dispositions, resources: task_owned_resource_rows,
      previous_resources: previous_task_owned_resource_rows
    )
    fail_check("dependency revision invalid: #{dependency_errors.join('; ')}") unless dependency_errors.empty?
    inventory_lines = inventory.lines.select { |line| line.start_with?("| `") }.to_h { |line| [line[/^\| `([^`]+)`/, 1], line] }
    previous_inventory_lines = previous_inventory.lines.select { |line| line.start_with?("| `") }.to_h { |line| [line[/^\| `([^`]+)`/, 1], line] }
    ledger_lines = markdown_table_raw_rows(ledger, "Unit execution ledger", ledger_header).to_h { |line| [line[/^\| `([^`]+)`/, 1], line] }
    previous_ledger_lines = markdown_table_raw_rows(previous_ledger, "Unit execution ledger", ledger_header).to_h { |line| [line[/^\| `([^`]+)`/, 1], line] }
    (units.to_set - dependency_affected).each do |unit|
      fail_check("unchanged inventory row #{unit} was not preserved exactly") unless inventory_lines[unit] == previous_inventory_lines[unit]
      fail_check("unchanged ledger row #{unit} was not preserved exactly") unless ledger_lines[unit] == previous_ledger_lines[unit]
    end
  end
  rows.each do |row|
    next if dependency_affected.include?(row[:unit])
    previous_row = previous_inventory_rows.find { |candidate| candidate[:unit] == row[:unit] }
    entry = ledger_rows.find { |candidate| candidate[:unit] == row[:unit] }
    previous_entry = previous_ledger_rows.find { |candidate| candidate[:unit] == row[:unit] }
    abandonment_edge = [["in-progress", "ready"], ["blocked", "ready"]].include?([previous_row[:status], row[:status]])
    abandonment = new_ordinary_abandonments.find { |disposition| disposition[:unit] == row[:unit] }
    if abandonment_edge
      fail_check("#{row[:unit]} abandonment requires previous execution-resource evidence") unless previous_execution_fixture_path
      ordinary_abandonment_errors(abandonment, previous_entry.merge(unit: row[:unit]), previous_task_owned_resource_rows, task_owned_resource_rows).each do |error|
        fail_check("#{row[:unit]} transition invalid: #{error}")
      end
    elsif abandonment
      fail_check("#{row[:unit]} has an abandonment disposition without an abandonment edge")
    end
    ledger_transition_errors(previous_row, previous_entry, row, entry, abandonment_evidence: !abandonment.nil?).each do |error|
      fail_check("#{row[:unit]} transition invalid: #{error}")
    end
    if execution_mode && previous_entry[:worker_commit] != "—" && entry[:worker_commit] != previous_entry[:worker_commit]
      fail_check("#{row[:unit]} replacement worker checkpoint does not exist") unless git_commit_exists?(entry[:worker_commit])
      unless git_commit_exists?(previous_entry[:worker_commit]) && git_ancestor?(previous_entry[:worker_commit], entry[:worker_commit])
        fail_check("#{row[:unit]} replacement worker checkpoint is not a descendant of the preserved checkpoint")
      end
    end
  end
  used_abandonment_units = ordinary_abandonment_units(previous_inventory_rows, rows, dependency_affected)
  fail_check("ordinary abandonment dispositions do not exactly match abandonment edges") unless new_ordinary_abandonments.map { |disposition| disposition[:unit] }.sort == used_abandonment_units.sort

  historical_proposed_entries.each do |entry|
    next if dependency_affected.include?(entry[:unit])

    row = rows.find { |candidate| candidate[:unit] == entry[:unit] }
    previous_row = previous_inventory_rows.find { |candidate| candidate[:unit] == entry[:unit] }
    previous_entry = previous_ledger_rows.find { |candidate| candidate[:unit] == entry[:unit] }
    if previous_row[:status] == "proposed"
      fail_check("#{row[:unit]} persisted proposed inventory row changed") unless row == previous_row
      fail_check("#{row[:unit]} persisted proposed ledger row changed") unless entry == previous_entry
      next
    end

    fail_check("#{row[:unit]} has forbidden transition to proposed from #{previous_row[:status]}") unless previous_row[:status] == "ready"
    fail_check("#{row[:unit]} ready-to-proposed transition changed inventory identity") unless row.values_at(:unit, :module, :requires, :owner, :goal) == previous_row.values_at(:unit, :module, :requires, :owner, :goal)
    previous_assignment_fields = previous_entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external)
    fail_check("#{row[:unit]} previous ready snapshot retained assignment or evidence") unless previous_row[:owner] == "—" && previous_assignment_fields.all? { |value| value == "—" }
    fail_check("#{row[:unit]} previous ready snapshot had unmet prerequisites") unless previous_row[:requires].all? { |required| previous_statuses[required] == "verified" }
    fail_check("#{row[:unit]} ready-to-proposed transition changed generation") unless entry[:generation] == previous_generations[row[:unit]]
    fail_check("#{row[:unit]} returned to proposed without an invalidated prerequisite") if previous_row[:requires].all? { |required| rows.find { |candidate| candidate[:unit] == required }[:status] == "verified" }
  end
end

start_gate_blocked_units = execution_mode ? noncurrent_primitives_by_consumer.keys.to_set : Set.new
eligible_proposed = eligible_frontier_rows(rows, start_gate_blocked_units)
fail_check("eligible proposed units were not promoted to ready: #{eligible_proposed.map { |row| row[:unit] }.join(', ')}") unless eligible_proposed.empty?

if execution_mode
  recovery_rows_for_validation.group_by { |row| row.values_at(0, 1) }.each do |(unit, generation), history|
    statuses = history.map { |row| plain_cell(row[5]) }
    recovery_lifecycle_errors(statuses.map { |status| {"status" => status} }).each { |error| fail_check("recovery #{unit} generation #{generation} #{error}") }
    epoch_history = history.map do |row|
      {"status" => plain_cell(row[5]), "identity" => row.values_at(7, 0, 1, 2, 3).map { |value| plain_cell(value) }}
    end
    recovery_epoch_identity_errors(epoch_history).each { |error| fail_check("recovery #{unit} generation #{generation} #{error}") }
  end
end

active_ledger_rows = ledger_rows.select do |entry|
  status = rows.find { |row| row[:unit] == entry[:unit] }[:status]
  ["in-progress", "blocked"].include?(status)
end
gate_module_roots_for = lambda do |unit|
  closure = Set.new
  visit = lambda do |candidate|
    next if closure.include?(candidate)

    closure << candidate
    rows.find { |row| row[:unit] == candidate }.fetch(:requires).each { |required| visit.call(required) }
  end
  visit.call(unit)
  inventory_roots = closure.map { |candidate| rows.find { |row| row[:unit] == candidate }.fetch(:module) }
  primitive_roots = closure.flat_map { |candidate| primitive_module_roots_by_consumer[candidate].to_a }
  (inventory_roots + primitive_roots).uniq.sort_by(&:b)
end
if execution_mode
  ledger_rows.reject { |entry| entry[:task] == "—" }.each do |entry|
    resource = worktree_resource_rows.find { |candidate| candidate[:target] == entry[:worktree] }
    fail_check("#{entry[:unit]} worker worktree lacks a task-owned resource registration") unless resource
    fail_check("#{entry[:unit]} worker worktree cleanup owner does not match worker task") unless resource[:owner] == entry[:task]
    if active_ledger_rows.include?(entry)
      fail_check("#{entry[:unit]} active worker worktree is recorded as removed") if resource[:state] == "removed"
    end
  end
end
[:task, :branch, :worktree].each do |field|
  assigned = active_ledger_rows.map { |row| row[field] }.reject { |value| value == "—" }
  fail_check("execution ledger contains duplicate #{field.to_s.tr('_', ' ')}") unless assigned.uniq.length == assigned.length
end

hash_or_dash = /\A(?:—|[0-9a-f]{40})\z/
ledger_rows.each do |entry|
  inventory_row = rows.find { |row| row[:unit] == entry[:unit] }
  fail_check("#{entry[:unit]} ledger generation is invalid") unless entry[:generation].match?(/\A\d+\z/)
  if entry[:transition] == "initial"
    fail_check("#{entry[:unit]} initial ledger row has generation or assignment data") unless entry[:generation] == "0" && entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external).all? { |value| value == "—" }
    fail_check("#{entry[:unit]} initial inventory owner is not empty") unless inventory_row[:owner] == "—"
    expected_initial = inventory_row[:requires].empty? ? "ready" : "proposed"
    fail_check("#{entry[:unit]} initial status must be #{expected_initial}") unless inventory_row[:status] == expected_initial
    next
  end

  match = entry[:transition].match(/\Av(\d+) (#{ALLOWED_STATUSES.to_a.join('|')}) owner=(\S+) at=(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z)\z/)
  fail_check("#{entry[:unit]} ledger transition format is invalid") unless match
  fail_check("#{entry[:unit]} ledger transition version must be positive") unless match[1].to_i.positive?
  fail_check("#{entry[:unit]} ledger status drift") unless match[2] == inventory_row[:status]
  fail_check("#{entry[:unit]} ledger owner/blocker drift") unless match[3] == inventory_row[:owner]
  fail_check("#{entry[:unit]} assignment commit is invalid") unless entry[:assignment] == "pending" || entry[:assignment].match?(hash_or_dash)
  [:worker_commit, :checkpoint, :gate_revision].each do |field|
    fail_check("#{entry[:unit]} #{field} is invalid") unless entry[field].match?(hash_or_dash)
  end
  assignment_fields = entry.values_at(:task, :branch, :worktree, :assignment)
  integrated_fields = entry.values_at(:worker_commit, :checkpoint)
  case inventory_row[:status]
  when "proposed"
    fail_check("#{entry[:unit]} proposed owner is not empty") unless inventory_row[:owner] == "—"
    fail_check("#{entry[:unit]} proposed row retains assignment or evidence") unless entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external).all? { |value| value == "—" }
  when "ready"
    fail_check("#{entry[:unit]} ready owner is not empty") unless inventory_row[:owner] == "—"
    fail_check("#{entry[:unit]} ready row retains assignment or evidence") unless entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :gate_revision, :fingerprint, :external).all? { |value| value == "—" }
  when "in-progress"
    fail_check("#{entry[:unit]} in-progress assignment fields are incomplete") if assignment_fields.any? { |value| value == "—" }
    fail_check("#{entry[:unit]} in-progress owner must equal worker task") unless inventory_row[:owner] != "—" && inventory_row[:owner] == entry[:task]
    fail_check("#{entry[:unit]} in-progress assignment commit is missing") if entry[:assignment] == "—"
    if entry[:checkpoint] == "—"
      fail_check("#{entry[:unit]} pre-integration row has gate evidence") unless entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
      unless entry[:worker_commit] == "—"
        matching_recovery = authorized_recoveries.any? do |source_row|
          plain_cell(source_row[0]) == entry[:unit] &&
            plain_cell(source_row[1]) == entry[:generation] &&
            plain_cell(source_row[3]) == entry[:worker_commit]
        end
        fail_check("#{entry[:unit]} pre-integration recovery lacks exact authorization") unless matching_recovery
      end
    else
      fail_check("#{entry[:unit]} integrated repair lacks worker commit") if entry[:worker_commit] == "—"
      matching_repair = active_repairs.any? do |repair|
        repair[:unit] == entry[:unit] && repair[:generation] == entry[:generation] &&
          repair[:checkpoint] == entry[:checkpoint] && repair[:worker_checkpoint] == entry[:worker_commit]
      end
      fail_check("#{entry[:unit]} integrated repair lacks exact authorized repair epoch") unless matching_repair
      gate_empty = entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
      gate_complete = entry[:gate_revision].match?(/\A[0-9a-f]{40}\z/) && entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
      fail_check("#{entry[:unit]} integrated repair gate evidence is partial") unless gate_empty || gate_complete
    end
  when "blocked"
    fail_check("#{entry[:unit]} blocked owner must be a safe blocker ID") unless inventory_row[:owner].match?(/\Ablocker:[a-zA-Z0-9._-]+\z/)
    fail_check("#{entry[:unit]} blocked row lost its assignment") if assignment_fields.any? { |value| value == "—" || value == "pending" }
    if entry[:checkpoint] == "—"
      fail_check("#{entry[:unit]} pre-integration blocked row has gate evidence") unless entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
    else
      fail_check("#{entry[:unit]} integrated blocked row lacks worker commit") if entry[:worker_commit] == "—"
      gate_empty = entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
      gate_complete = entry[:gate_revision].match?(/\A[0-9a-f]{40}\z/) && entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
      fail_check("#{entry[:unit]} integrated blocked gate evidence is partial") unless gate_empty || gate_complete
    end
  when "implemented-unverified"
    fail_check("#{entry[:unit]} integrated owner is not empty") unless inventory_row[:owner] == "—"
    fail_check("#{entry[:unit]} integrated fields are incomplete") if (assignment_fields + integrated_fields).any? { |value| value == "—" || value == "pending" }
    fail_check("#{entry[:unit]} implemented-unverified row prematurely records gate evidence") unless entry[:gate_revision] == "—" && entry[:fingerprint] == "—"
    fail_check("#{entry[:unit]} integrated external evidence disposition is missing") if entry[:external] == "—"
  when "verified"
    fail_check("#{entry[:unit]} integrated owner is not empty") unless inventory_row[:owner] == "—"
    fail_check("#{entry[:unit]} integrated fields are incomplete") if (assignment_fields + integrated_fields).any? { |value| value == "—" || value == "pending" }
    fail_check("#{entry[:unit]} gate execution revision is invalid") unless entry[:gate_revision].match?(/\A[0-9a-f]{40}\z/)
    fail_check("#{entry[:unit]} gate fingerprint is invalid") unless entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
    fail_check("#{entry[:unit]} integrated external evidence disposition is missing") if entry[:external] == "—"
  end
  unless entry[:task] == "—"
    fail_check("#{entry[:unit]} worker task is unsafe") unless entry[:task].match?(/\A[a-zA-Z0-9._\/-]+\z/)
    fail_check("#{entry[:unit]} worker branch is not conventional") unless entry[:branch].match?(%r{\A(?:feature|bugfix|hotfix|release|chore|refactor)/[a-zA-Z0-9._/-]+\z})
    historical_removed_worktree = execution_mode && worktree_resource_rows.any? do |resource|
      resource[:target] == entry[:worktree] && resource[:state] == "removed" && !resource[:integration]
    end
    unless safe_worktree_path?(entry[:worktree], task_owned_worktree_parent) || historical_removed_worktree
      fail_check("#{entry[:unit]} worker worktree is not beneath the safe task-owned preflight parent")
    end
  end
  external_ok = entry[:external] == "—" || ["not-needed", "available"].include?(entry[:external]) || entry[:external].match?(/\Aunavailable:[a-zA-Z0-9._-]+\z/) || repository_evidence_binding(entry[:external])
  fail_check("#{entry[:unit]} external evidence field is invalid") unless external_ok
  if (binding = repository_evidence_binding(entry[:external]))
    evidence_relative, evidence_commit = binding
    evidence_path = File.expand_path(evidence_relative, REPOSITORY_ROOT)
    fail_check("#{entry[:unit]} external evidence path escapes repository") unless evidence_path.start_with?(REPOSITORY_ROOT + File::SEPARATOR)
    fail_check("#{entry[:unit]} external evidence path is missing") unless File.file?(evidence_path)
    committed = git_blob_bytes(evidence_commit, evidence_relative)
    fail_check("#{entry[:unit]} external evidence binding commit does not contain the record") unless committed
    fail_check("#{entry[:unit]} external evidence binding differs from working bytes") unless committed == File.binread(evidence_path)
  end
  if execution_mode && %w[implemented-unverified verified].include?(inventory_row[:status])
    integrated_commit_ancestry_errors(entry).each do |error|
      fail_check("#{entry[:unit]} #{inventory_row[:status]} #{error}")
    end
  end
  if inventory_row[:status] == "verified"
    if execution_mode
      fail_check("#{entry[:unit]} verified gate execution revision does not exist") unless git_commit_exists?(entry[:gate_revision])
      fail_check("#{entry[:unit]} gate execution revision excludes integration checkpoint") unless git_ancestor?(entry[:checkpoint], entry[:gate_revision])
      fail_check("#{entry[:unit]} gate execution revision is not integrated") unless git_ancestor?(entry[:gate_revision], "HEAD")
      gate_path = File.join(REPOSITORY_ROOT, ".ai/identity-platform/evidence/gates/#{entry[:unit].tr('/', '-')}.json")
      gate_binding = local_gate_evidence_bindings.find do |binding|
        binding[:unit] == entry[:unit] && binding[:generation] == entry[:generation] &&
          binding[:gate_revision] == entry[:gate_revision]
      end
      fail_check("#{entry[:unit]} verified local gate evidence lacks an attributable binding") unless gate_binding
      local_gate_binding_errors(gate_binding, ledger_entry: entry, repository_root: REPOSITORY_ROOT).each do |error|
        fail_check("#{entry[:unit]} local gate binding #{error}")
      end if gate_binding
      fail_check("#{entry[:unit]} verified local gate evidence is missing") unless File.file?(gate_path)
      gate_source = File.read(gate_path)
      gate = JSON.parse(gate_source)
      fail_check("#{entry[:unit]} local gate evidence is not canonical JSON") unless gate_source == JSON.pretty_generate(gate) + "\n"
      local_gate_evidence_errors(
        gate, unit: entry[:unit], revision: entry[:gate_revision], fingerprint: entry[:fingerprint],
        module_roots: gate_module_roots_for.call(entry[:unit]), repository_root: REPOSITORY_ROOT,
        record_path: ".ai/identity-platform/evidence/gates/#{entry[:unit].tr('/', '-')}.json",
        evidence_commit: gate_binding&.fetch(:commit)
      ).each do |error|
        fail_check("#{entry[:unit]} local gate evidence #{error}")
      end
      unless execution_fixture_path
        current_revision = git_output("rev-parse", "HEAD")
        environment_inputs = gate.fetch("input_manifest", []).select do |input|
          input["path_or_environment_id"].to_s.start_with?("environment:")
        end
        current_manifest = (tracked_behavior_input_manifest(
          current_revision, gate_module_roots_for.call(entry[:unit]), repository: REPOSITORY_ROOT
        ) + environment_inputs).sort_by { |input| input.fetch("path_or_environment_id").b }
        behavior_input_manifest_errors(
          current_manifest, revision: current_revision,
          module_roots: gate_module_roots_for.call(entry[:unit]), repository: REPOSITORY_ROOT
        ).each { |error| fail_check("#{entry[:unit]} current verified input #{error}") }
        current_verified_gate_root_errors(gate["input_manifest"], entry[:fingerprint], current_manifest).each do |error|
          fail_check("#{entry[:unit]} #{error}")
        end
      end
      fail_check("#{entry[:unit]} local gate binding differs from record bytes") unless gate_binding && gate_binding[:digest] == "sha256:#{Digest::SHA256.hexdigest(gate_source)}"
    end
    required_lanes = external_lanes.select { |lane| lane[:consumers].include?(entry[:unit]) }
    if required_lanes.empty?
      fail_check("#{entry[:unit]} without external claims must record not-needed") unless entry[:external] == "not-needed"
    else
      ledger_external_binding = repository_evidence_binding(entry[:external])
      fail_check("#{entry[:unit]} required external claims lack an attributable evidence record") unless ledger_external_binding
      required_lanes.each do |lane|
        fail_check("#{entry[:unit]} external evidence path does not match lane #{lane[:profile]}") unless lane[:evidence] == ledger_external_binding[0]
        fail_check("#{entry[:unit]} external evidence commit does not match lane #{lane[:profile]}") unless lane[:evidence_commit] == ledger_external_binding[1]
        fail_check("#{entry[:unit]} external lane #{lane[:profile]} is not available") unless lane[:classification] == "available"
        record = external_records[ledger_external_binding[0]]
        fail_check("#{entry[:unit]} external evidence record is unavailable") unless record
        profile_record = record.fetch("profiles").find { |candidate| candidate["profile_id"] == lane[:profile] }
        result = profile_record.fetch("unit_results").find { |candidate| candidate["unit"] == entry[:unit] }
        external_result_ledger_errors(result, gate_revision: entry[:gate_revision], input_fingerprint: entry[:fingerprint]).each do |error|
          fail_check("#{entry[:unit]} #{error}")
        end
      end
    end
  end
end

puts "identity-platform validation: #{rows.length} schedulable units (#{identity_rows.length} identity public-contract units plus #{primitive_extension_rows.length} primitive extensions), #{inventory_edges.length} edges, #{depth.values.max + 1} waves, #{operation_ids.length} operations, #{route_records.length} HTTP mappings, #{openapi_owners.length} OpenAPI operations, parity baseline #{BASELINE}"
