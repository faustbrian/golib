#!/usr/bin/env ruby
# frozen_string_literal: true

require "json"
require "digest"
require "open3"
require "set"
require "time"
require "tmpdir"
require_relative "shared_contract_applicability"

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
EXPECTED_UNITS = 61
BASELINE = "b8077b74ef9a80a7757220b72834349bd8de05c0"
NORMATIVE_KEYWORD_PATTERN = /\b(?:MUST(?: NOT)?|REQUIRED|SHALL(?: NOT)?|SHOULD(?: NOT)?|RECOMMENDED|NOT RECOMMENDED|MAY|OPTIONAL)\b/
BCP14_NOTICE = <<~NOTICE.chomp.freeze
  The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
  "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
  "OPTIONAL" in this document are to be interpreted as described in BCP 14
  [RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
  shown here.
NOTICE
EXPECTED_UPSTREAM_CLOSURE_SHA256 = "2c08ffcbc834d71454732beed4263651d65906ffb715b7e311d80f0dade735b4"
EXPECTED_UPSTREAM_SOURCES = {
  "docs/content/docs/concepts" => ["tree", "9b98359e8415bc9a2f71617639cf2d61f15a0679", "Core documentation and route surface"],
  "docs/content/docs/authentication" => ["tree", "b5132040e1543221912db02871153ed6ab57fc4c", "Core documentation and route surface"],
  "docs/content/docs/plugins" => ["tree", "7f95f309b87e8737d2677df693a6763bfbf91907", "Official plugin documentation tree"],
  "packages/better-auth/src/api/routes" => ["tree", "474bc1eab9bda2fa20ba6d1605a75a786c710447", "Core documentation and route surface"],
  "packages/better-auth/src/plugins" => ["tree", "4e750e9a538dc8b75fe96dbfd7ba411a1deb3d1e", "Source-exported and internal plugin surface"],
  "packages/better-auth/src/plugins/index.ts" => ["blob", "c6c80709f5b472468b5919c3ac6eea7c452f2091", "Source-exported and internal plugin surface"],
  "packages/core/src/social-providers" => ["tree", "39f9b83ca3681164e9eb8f8ef77f2ea5d5938e4c", "Provider catalog disposition"],
  "packages" => ["tree", "2cc84b5f623da92e892bd3288243a8c3ec4a5110", "Official top-level packages"]
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
  "authentication", "authentication/jwt", "authorization", "capability",
  "telemetry"
].freeze
HTTP_FEATURES = Set[
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
  API_OPERATIONS.md UPSTREAM_DISPOSITIONS.md UPSTREAM_SURFACE.json PROTOCOL_BASELINES.md
  PROTOCOL_CONFORMANCE_MANIFEST.json CONFIGURATION_CATALOGS.json
  SECURITY_EVENTS.md TRANSACTION_CONTRACT.md LIFECYCLE_CASCADES.md
  LIFECYCLE_CONSUMERS.md REFERENCE_CONFIGURATION.md PREFLIGHT_EVIDENCE.md
].freeze
REQUIRED_ARTIFACT_SECTIONS = {
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
    "Execution identity", "Tool and environment lanes", "External evidence lanes",
    "Existing primitive contracts", "Task-owned resource registry",
    "Conflict-recovery baselines"
  ]
}.freeze
READING_ORDER = [
  "repository `AGENTS.md`", "`README.md`", "`PROGRAM.md`",
  "`COMMON_REQUIREMENTS.md`", "`END_STATE.md`", "`REFERENCE_PROFILE.md`",
  "`BETTER_AUTH_PARITY.md`", "`API_OPERATIONS.md`",
  "`UPSTREAM_DISPOSITIONS.md`", "`UPSTREAM_SURFACE.json`",
  "`PROTOCOL_BASELINES.md`", "`PROTOCOL_CONFORMANCE_MANIFEST.json`",
  "`SECURITY_EVENTS.md`", "`TRANSACTION_CONTRACT.md`",
  "`LIFECYCLE_CASCADES.md`", "`LIFECYCLE_CONSUMERS.md`",
  "`REFERENCE_CONFIGURATION.md`", "`CONFIGURATION_CATALOGS.json`",
  "`PREFLIGHT_EVIDENCE.md`", "`DEPENDENCIES.md`", "`INVENTORY.md`",
  "`EXECUTION_LEDGER.md`", "`WORKER_PROMPT.md`",
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
EXPECTED_PROTOCOL_SOURCE_CONSUMERS = {
  "oauth-2.1-draft-15" => %w[identity/oauth identity/oauth/providers oauth-server sso/oauth2 sso/oidc],
  "rfc-2119" => :all_units,
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
  "oidc-multiple-response-types-1.0" => %w[identity/oauth identity/oauth/providers oauth-server/oidc],
  "oidc-rp-initiated-logout-1.0" => %w[identity/oauth oauth-server/oidc sso/oidc],
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
           noun = text[/\b(URL|URI|string|list|handle)\b/, 1]
           fail_check("reference configuration REQUIRED row is not typeable: #{source}") unless noun
           {"URL" => "url:absolute-https", "URI" => "uri:absolute", "string" => "string:utf8", "list" => "list:string", "handle" => "handle:#{path.split('.').last}"}.fetch(noun)
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

def closed_list_value(document, path)
  value = configuration_reference_value(document, path)
  match = value.match(/\A`([^`]+)`\z/)
  match ? match[1].split(",") : []
end

def protocol_semantic_errors(configuration:, protocol:, conformance:, oauth_goal:, saml_goal:, api_operations:, reference_profile:, applicability:)
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
  unless resource_consumers == %w[identity/reference oauth-server]
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
    "oauth-server" => operation_event_map.values,
    "oauth-server/device" => operation_event_map.values_at(
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
  unless phone_reset.include?("reset capability plus independent factor") &&
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

def phone_contract_errors(configuration:, reference_profile:, api_operations:, phone_goal:, lifecycle_consumers:,
                          security_events:, applicability:)
  errors = []
  recovery_policy = configuration_row(configuration, "struct:ref.phone.recovery")
  %w[
    request_when_disabled=deny complete_when_disabled=deny
    proof=canonical_reset_capability_plus_eligible_independent_factor
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

  normalized_phone_goal = phone_goal.split.join(" ")
  unless normalized_phone_goal.include?("Public signup/signin initiation MUST create or use the canonical single-use pre-auth transaction") &&
         normalized_phone_goal.include?("tenant, purpose, canonical number and resolved `RememberPolicy`") &&
         normalized_phone_goal.include?("Session-authenticated number-change challenges MUST NOT create or substitute a public pre-auth transaction")
    errors << "identity/phone goal lacks exact pre-auth ownership and binding"
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
  required_configuration = %w[
    ref.authentication.preauth_ttl ref.phone.recovery.enabled ref.phone.recovery.policy
    ref.session.remember_default ref.struct:ref.phone.recovery
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

def plain_cell(value)
  value.to_s.delete_prefix("`").delete_suffix("`")
end

def rfc3339?(value)
  Time.iso8601(value)
  value.match?(/\A\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\z/)
rescue ArgumentError
  false
end

def repository_evidence_path?(value)
  return false unless value.match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z}) && !value.split("/").include?("..")

  path = File.expand_path(value, REPOSITORY_ROOT)
  path.start_with?(REPOSITORY_ROOT + File::SEPARATOR) && File.file?(path)
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
  rows = validate_administration_journey!(File.read(fixture_path))
  puts "identity-platform end-state administration validation: #{rows.length} operations"
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
inventory_fixture_path = take_path_option!(ARGV, "--inventory-fixture")
ledger_fixture_path = take_path_option!(ARGV, "--ledger-fixture")
previous_inventory_path = take_path_option!(ARGV, "--previous-inventory-fixture")
previous_ledger_path = take_path_option!(ARGV, "--previous-ledger-fixture")
fail_check("previous transition fixtures must be supplied together") unless previous_inventory_path.nil? == previous_ledger_path.nil?
execution_mode = ARGV.delete("--execution") || execution_fixture_path
fail_check("unknown arguments: #{ARGV.join(' ')}") unless ARGV.empty?
validate_normative_markdown_notices!
validate_administration_journey!(File.read(File.join(ROOT, "END_STATE.md")))
recovery_rows_for_validation = []
worktree_resource_rows = []
if execution_mode
  preflight_snapshot = File.read(execution_fixture_path || File.join(ROOT, "PREFLIGHT_EVIDENCE.md"))
  execution_sections = preflight_snapshot[/^## Execution identity\n.*?(?=^## Conflict-recovery baselines|\z)/m].to_s
  retains_pending = execution_sections.lines.any? do |line|
    line.start_with?("|") && line.split("|").map { |cell| plain_cell(cell.strip) }.include?("pending")
  end
  fail_check("execution preflight retains pending values") if retains_pending
end

inventory = File.read(inventory_fixture_path || File.join(ROOT, "INVENTORY.md"))
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
fail_check("expected #{EXPECTED_UNITS} inventory rows, found #{rows.length}") unless rows.length == EXPECTED_UNITS
units = rows.map { |row| row[:unit] }
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
  SharedContractApplicability.load_and_validate!(root: ROOT, units: units)
  unless SharedContractApplicability.canonical?(root: ROOT, units: units)
    fail_check("shared-contract applicability manifest is not canonical JSON")
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
expected_reference = REFERENCE_ADAPTERS | Set["identity/http", "sso/domain-verification"]
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
consumed_primitives = Set.new
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
  path = if row[:goal].start_with?("goals/")
    File.join(ROOT, row[:goal])
  else
    File.join(REPOSITORY_ROOT, row[:goal])
  end
  fail_check("missing goal for #{row[:unit]}: #{row[:goal]}") unless File.file?(path)
  body = File.read(path)
  actual_unit = body[/^- Unit: `([^`]+)`/, 1]
  actual_module = body[/^- Canonical module: `([^`]+)`/, 1]
  actual_goal = body[/^- Canonical goal after scaffolding: `([^`]+)`/, 1]
  fail_check("goal unit mismatch for #{row[:goal]}") unless actual_unit == row[:unit]
  fail_check("goal module mismatch for #{row[:unit]}") unless actual_module == row[:module]
  expected_goal = "#{row[:module]}/.ai/GOAL.md"
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
  fail_check("#{row[:unit]} retains ambiguous delegation language") if body.include?("where not delegated")
  fail_check("#{row[:unit]} goal lacks common-requirements start gate") unless body.include?("COMMON_REQUIREMENTS.md")
  fail_check("#{row[:unit]} goal has move-unsafe relative program references") if body.match?(%r{`\.\./(?:COMMON_REQUIREMENTS|INVENTORY)\.md`})
  fail_check("#{row[:unit]} goal retains worker-owned ready transition") if body.match?(/marks `#{Regexp.escape(row[:unit])}` as `ready`/)
  start_section = body[/^## (?:Start gate(?: and objective)?|Start gate and objective).*?(?=^## |\z)/m].to_s
  named_start_units = start_section.scan(/`([^`]+)`/).flatten.select { |name| known.include?(name) && name != row[:unit] }
  unexpected_start_units = named_start_units - row[:requires]
  fail_check("#{row[:unit]} start gate names non-prerequisites: #{unexpected_start_units.join(', ')}") unless unexpected_start_units.empty?
  expected_unlocks = reverse[row[:unit]]
  unlock_line = body[/^- Unlocks after verification:.*$/]
  actual_unlocks = unlock_line.to_s.scan(/`([^`]+)`/).flatten
  fail_check("unlock mismatch for #{row[:unit]}: expected #{expected_unlocks}, got #{actual_unlocks}") unless actual_unlocks == expected_unlocks
  seen_goals << File.realpath(path)
  normative_count = body.scan(/\b(?:MUST|MUST NOT|REQUIRED|SHALL|SHALL NOT)\b/).length
  fail_check("#{row[:unit]} goal is too thin: #{normative_count} normative requirements") if normative_count < 15
end
fail_check("expected #{EXPECTED_UNITS} resolved inventory goals, found #{seen_goals.length}") unless seen_goals.length == EXPECTED_UNITS
planning_goal_files = Dir[File.join(ROOT, "goals", "*.md")]
orphans = planning_goal_files.map { |path| File.realpath(path) }.to_set - seen_goals
fail_check("orphan goals: #{orphans.to_a.join(', ')}") unless orphans.empty?

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
artifacts = {}
COORDINATOR_ARTIFACTS.each do |artifact|
  artifact_path = if artifact == "UPSTREAM_SURFACE.json" && ENV.key?("IDENTITY_PLATFORM_UPSTREAM_SURFACE_FIXTURE")
                    File.expand_path(ENV.fetch("IDENTITY_PLATFORM_UPSTREAM_SURFACE_FIXTURE"))
                  elsif artifact == "API_OPERATIONS.md" && ENV.key?("IDENTITY_PLATFORM_API_OPERATIONS_FIXTURE")
                    File.expand_path(ENV.fetch("IDENTITY_PLATFORM_API_OPERATIONS_FIXTURE"))
                  else
                    File.join(ROOT, artifact)
                  end
  fail_check("missing coordinator artifact: #{artifact}") unless File.file?(artifact_path)
  artifacts[artifact] = File.read(artifact_path)
  fail_check("program completion contract omits #{artifact}") unless program.include?("`#{artifact}`")
  headings = artifacts[artifact].scan(/^## (.+)$/).flatten
  missing_sections = REQUIRED_ARTIFACT_SECTIONS.fetch(artifact) - headings
  fail_check("#{artifact} missing required sections: #{missing_sections.join(', ')}") unless missing_sections.empty?
end

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

if execution_mode
  preflight = execution_fixture_path ? File.read(execution_fixture_path) : artifacts.fetch("PREFLIGHT_EVIDENCE.md")
  identity_rows = markdown_table(preflight, "Execution identity", "| Field | Value |").to_h
  base = plain_cell(identity_rows.fetch("Recorded committed `main` base", ""))
  input_revision = plain_cell(identity_rows.fetch("Preflight input revision before the record commit", ""))
  branch = plain_cell(identity_rows.fetch("Integration branch", ""))
  integration_worktree = plain_cell(identity_rows.fetch("Integration worktree", ""))
  worktree_parent = plain_cell(identity_rows.fetch("Task-owned worktree parent", ""))
  recorded_at = plain_cell(identity_rows.fetch("Preflight recorded at (RFC3339)", ""))
  fail_check("execution preflight committed base is invalid") unless base.match?(/\A[0-9a-f]{40}\z/)
  fail_check("execution preflight input revision is invalid") unless input_revision.match?(/\A[0-9a-f]{40}\z/)
  fail_check("execution preflight integration branch is not conventional") unless branch.match?(%r{\A(?:feature|bugfix|hotfix|release|chore|refactor)/[a-zA-Z0-9._/-]+\z})
  fail_check("execution preflight worktree paths are unsafe") unless safe_integration_worktree_path?(integration_worktree, worktree_parent)
  fail_check("execution preflight timestamp is invalid") unless rfc3339?(recorded_at)

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
    "| Safe profile ID | Consuming units | Exact acceptance claims | Classification | Credential source metadata | Evidence path or blocker |"
  )
  fail_check("execution preflight has no external-evidence classifications") if external_rows.empty?
  external_rows.each do |profile, consumers, claims, classification, credential_source, evidence|
    profile = plain_cell(profile)
    fail_check("external evidence profile ID is unsafe") unless profile.match?(/\A[a-zA-Z0-9._-]+\z/)
    consumer_units = consumers.scan(/`([^`]+)`/).flatten
    fail_check("external evidence profile #{profile} has no consumers") if consumer_units.empty?
    fail_check("external evidence profile #{profile} has unknown consumers") unless consumer_units.all? { |unit| known.include?(unit) }
    fail_check("external evidence profile #{profile} lacks exact claims") if claims.empty? || plain_cell(claims) == "—"
    fail_check("external evidence profile #{profile} has invalid classification") unless classifications.include?(plain_cell(classification))
    fail_check("external evidence profile #{profile} lacks credential-source metadata") if credential_source.empty? || plain_cell(credential_source) == "—"
    fail_check("external evidence profile #{profile} lacks evidence or blocker") if evidence.empty? || plain_cell(evidence) == "—"
    fail_check("external evidence profile #{profile} has unsafe evidence or blocker") unless safe_preflight_evidence_or_blocker?(plain_cell(evidence))
  end

  primitive_rows = markdown_table(
    preflight,
    "Existing primitive contracts",
    "| Primitive | Consuming units | Registered module/package | API input fingerprint | Gate fingerprint and result | Evidence path |"
  )
  primitive_names = primitive_rows.map { |row| plain_cell(row[0]) }.to_set
  fail_check("execution preflight primitive inventory drifted") unless primitive_names == consumed_primitives
  primitive_rows.each do |primitive, consumers, registered, api_fingerprint, gate_result, evidence|
    name = plain_cell(primitive)
    consumer_units = consumers.scan(/`([^`]+)`/).flatten
    fail_check("primitive #{name} has no consumers") if consumer_units.empty?
    fail_check("primitive #{name} has unknown consumers") unless consumer_units.all? { |unit| known.include?(unit) }
    registered_name = plain_cell(registered).delete_prefix("pkg/")
    fail_check("primitive #{name} is not registered") unless resolvable_consumables.include?(registered_name)
    fail_check("primitive #{name} API fingerprint is invalid") unless plain_cell(api_fingerprint).match?(/\Asha256:[0-9a-f]{64}\z/)
    fail_check("primitive #{name} gate fingerprint/result is invalid") unless plain_cell(gate_result).match?(/\Asha256:[0-9a-f]{64} (?:pass|failed|blocked|stale)\z/)
    fail_check("primitive #{name} evidence path is unsafe or missing") unless repository_evidence_path?(plain_cell(evidence))
  end

  resource_rows = markdown_table(
    preflight,
    "Task-owned resource registry",
    "| Resource ID | Type | Owning unit/task | Exact path or safe external ID | State | Cleanup trigger | Last reconciled at |"
  )
  fail_check("execution preflight has no registered task-owned resources") if resource_rows.empty?
  resource_states = Set["active", "retained-for-recovery", "removal-pending-after-final-commit", "removed"]
  resource_rows.each do |resource_id, type, owner, target, state, cleanup_trigger, reconciled_at|
    resource_id = plain_cell(resource_id)
    type = plain_cell(type)
    owner = plain_cell(owner)
    target = plain_cell(target)
    state = plain_cell(state)
    fail_check("resource ID is unsafe") unless resource_id.match?(/\A[a-zA-Z0-9._-]+\z/)
    fail_check("resource #{resource_id} has incomplete ownership") if type.empty? || owner.empty?
    fail_check("resource #{resource_id} has invalid state") unless resource_states.include?(state)
    safe_target = if type == "worktree"
      fail_check("worktree resource #{resource_id} owner is unsafe") unless owner.match?(/\A[a-zA-Z0-9._\/-]+\z/)
      if state == "removed"
        clean_parent = File.realpath(worktree_parent)
        clean_target = File.expand_path(target)
        integration_target = clean_target == File.expand_path(integration_worktree)
        owner_ok = integration_target ? owner == "coordinator" : owner != "coordinator"
        removed_safe = target.start_with?("/") && target == clean_target && clean_target != clean_parent &&
          clean_target.start_with?(clean_parent + File::SEPARATOR) &&
          !File.exist?(clean_target) && !registered_worktree_paths.include?(clean_target) && owner_ok
        worktree_resource_rows << {target: target, owner: owner, state: state, integration: integration_target} if removed_safe
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
        worktree_resource_rows << {target: target, owner: owner, state: state, integration: integration_target} if identity && registered && exact && identity_ok
        identity && registered && exact && identity_ok
      end
    elsif target.start_with?("/")
      clean_target = File.expand_path(target)
      clean_parent = File.expand_path(worktree_parent)
      registered_worktree_target = if File.exist?(clean_target)
                                     registered_worktree_paths.include?(File.realpath(clean_target))
                                   else
                                     false
                                   end
      !registered_worktree_target && clean_target != clean_parent &&
        clean_target.start_with?(clean_parent + File::SEPARATOR)
    else
      target.match?(/\A[a-zA-Z0-9._:\/-]+\z/) && !target.split("/").include?("..")
    end
    fail_check("resource #{resource_id} target is unsafe") unless safe_target
    if type == "worktree" && state == "removal-pending-after-final-commit"
      fail_check("only the integration worktree may be removal-pending") unless worktree_resource_rows.last&.fetch(:target) == target && worktree_resource_rows.last.fetch(:integration)
    end
    fail_check("resource #{resource_id} lacks cleanup trigger") if cleanup_trigger.empty? || plain_cell(cleanup_trigger) == "—"
    fail_check("resource #{resource_id} reconciliation timestamp is invalid") unless rfc3339?(plain_cell(reconciled_at))
  end
  live_worktree_targets = worktree_resource_rows.map { |resource| resource[:target] }
  fail_check("task-owned resource registry contains duplicate live worktrees") unless live_worktree_targets.uniq == live_worktree_targets
  integration_resources = worktree_resource_rows.select { |resource| resource[:integration] }
  unless integration_resources.length == 1 && integration_resources.first[:owner] == "coordinator" && integration_resources.first[:state] != "removed"
    fail_check("integration worktree requires exactly one live coordinator-owned worktree resource")
  end

  recovery_rows = markdown_table(
    preflight,
    "Conflict-recovery baselines",
    "| Unit | Generation | Integration commit | Worker checkpoint | Conflict evidence path | Status | Recorded at |"
  )
  recovery_statuses = Set["authorized", "superseded", "completed"]
  recovery_rows.each do |unit, generation, integration_commit, worker_checkpoint, evidence, status, recorded_at|
    unit = plain_cell(unit)
    fail_check("conflict-recovery row has unknown unit") unless known.include?(unit)
    fail_check("conflict-recovery row for #{unit} has invalid generation") unless plain_cell(generation).match?(/\A\d+\z/)
    fail_check("conflict-recovery row for #{unit} has invalid integration parent") unless plain_cell(integration_commit).match?(/\A[0-9a-f]{40}\z/)
    fail_check("conflict-recovery row for #{unit} has invalid worker checkpoint") unless plain_cell(worker_checkpoint).match?(/\A[0-9a-f]{40}\z/)
    fail_check("conflict-recovery row for #{unit} has unsafe or missing evidence path") unless repository_evidence_path?(plain_cell(evidence))
    fail_check("conflict-recovery row for #{unit} has invalid status") unless recovery_statuses.include?(plain_cell(status))
    fail_check("conflict-recovery row for #{unit} has invalid timestamp") unless rfc3339?(plain_cell(recorded_at))
  end
  recovery_rows_for_validation = recovery_rows
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
  [path, [source.fetch("kind"), source.fetch("object_id"), source.fetch("disposition_section")]]
end
fail_check("pinned upstream source identities drifted") unless upstream_sources == EXPECTED_UPSTREAM_SOURCES
disposition_headings = artifacts.fetch("UPSTREAM_DISPOSITIONS.md").scan(/^## (.+)$/).flatten.to_set
missing_disposition_sections = upstream_sources.values.map(&:last).to_set - disposition_headings
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
  "capability" => "pkg/capability/.ai/GOAL.md"
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
operations.each do |operation_id, _owner_cell, rate_class, _exposure, _idempotency|
  next if rate_class == "internal"

  policy_id = "rate.#{rate_class}"
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

configuration_rows = validate_configuration_rows!(artifacts.fetch("REFERENCE_CONFIGURATION.md"))
fail_check("reference configuration is unexpectedly thin") if configuration_rows.length < 100
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
changes_mapping = artifacts.fetch("SECURITY_EVENTS.md").lines.find { |line| line.start_with?("| Changes |") }.to_s
unless changes_mapping.include?("`Redacted=true`") && changes_mapping.include?("both maps MUST be empty")
  fail_check("security event change redaction conflicts with audit.Record semantics")
end
validate_audit_retention_authority!(
  "configuration" => artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  "security_events" => artifacts.fetch("SECURITY_EVENTS.md"),
  "reference_goal" => File.read(File.join(ROOT, "goals/identity-reference.md")),
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

[
  "fixed at exactly 5 seconds per RFC 8628 `slow_down`",
  "issuer path MUST be empty or `/`", "exact `http://localhost`",
  "equal or decreased received counter", "saml.sp_idp_initiated_url"
].each do |required|
  combined = artifacts.fetch("REFERENCE_CONFIGURATION.md") + File.read(File.join(ROOT, "REFERENCE_PROFILE.md")) + protocol
  fail_check("protocol semantic decision missing: #{required}") unless combined.include?(required)
end

rp_transaction_row = artifacts.fetch("REFERENCE_CONFIGURATION.md").lines.find do |line|
  line.start_with?("| `struct:ref.oauth.rp_transaction`")
end.to_s
actual_rp_bindings = rp_transaction_row[/binding = ([^`;]+)/, 1].to_s.split(",")
expected_rp_bindings = %w[
  issuer provider client_id redirect_uri response_mode pkce nonce requested_scopes
  tenant operation preauth_transaction initiating_subject popup_opener_origin
  popup_channel_id continuation_ref remember_policy
]
fail_check("OAuth RP transaction binding set drifted") unless actual_rp_bindings == expected_rp_bindings

applicability = JSON.parse(File.read(File.join(ROOT, SharedContractApplicability::FILE))).fetch("units")
rfc_contradiction_inputs = {
  mfa_postgres_goal: File.read(File.join(ROOT, "goals/identity-mfa-postgres.md")),
  oauth_postgres_goal: File.read(File.join(ROOT, "goals/identity-oauth-postgres.md")),
  identity_postgres_goal: File.read(File.join(ROOT, "goals/identity-postgres.md")),
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
  reference_goal: File.read(File.join(ROOT, "goals/identity-reference.md")),
  parity: File.read(File.join(ROOT, "BETTER_AUTH_PARITY.md")),
  http_goal: File.read(File.join(ROOT, "goals/identity-http.md")),
  risk_goal: File.read(File.join(ROOT, "goals/identity-risk.md")),
  risk_postgres_goal: File.read(File.join(ROOT, "goals/identity-risk-postgres.md")),
  risk_valkey_goal: File.read(File.join(ROOT, "goals/identity-risk-valkey.md")),
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
  "reset capability plus independent factor", "reset challenge"
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
  phone_goal: File.read(File.join(ROOT, "goals/identity-phone.md")),
  lifecycle_consumers: artifacts.fetch("LIFECYCLE_CONSUMERS.md"),
  security_events: artifacts.fetch("SECURITY_EVENTS.md"),
  applicability: applicability
}
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
fail_check("protocol conformance manifest top-level schema drifted") unless conformance.keys == %w[version retrieved_at verified_errata source_identity sources tools]
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
protocol_source_consumer_errors(sources, units.to_set).each { |error| fail_check(error) }
conformance_tool_errors(conformance.fetch("tools"), units.to_set).each { |error| fail_check(error) }

tool_fixture = lambda do |label, expected_error, &mutate|
  tools = JSON.parse(JSON.generate(EXPECTED_CONFORMANCE_TOOLS))
  mutate.call(tools)
  expect_protocol_fixture_rejection!(label, expected_error) do
    conformance_tool_errors(tools, units.to_set)
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
  protocol_source_consumer_errors(source_consumer_fixture, units.to_set)
end
source_consumer_fixture = JSON.parse(JSON.generate(sources))
source_consumer_fixture.find { |source| source.fetch("id") == "rfc-7239" }.fetch("consumers") << "identity/risk"
expect_protocol_fixture_rejection!("extra RFC source consumer", "protocol source rfc-7239 consumer set drifted") do
  protocol_source_consumer_errors(source_consumer_fixture, units.to_set)
end

semantic_inputs = {
  configuration: artifacts.fetch("REFERENCE_CONFIGURATION.md"),
  protocol: protocol,
  conformance: conformance,
  oauth_goal: File.read(File.join(ROOT, "goals/oauth-server.md")),
  saml_goal: File.read(File.join(ROOT, "goals/sso-saml.md")),
  api_operations: artifacts.fetch("API_OPERATIONS.md"),
  reference_profile: File.read(File.join(ROOT, "REFERENCE_PROFILE.md")),
  applicability: applicability
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

preflight = artifacts.fetch("PREFLIGHT_EVIDENCE.md")
[
  "| Field | Value |",
  "| Requirement/profile | Required by units or claims | Classification | Version/environment identity | Evidence path or blocking claim |",
  "| Safe profile ID | Consuming units | Exact acceptance claims | Classification | Credential source metadata | Evidence path or blocker |",
  "| Primitive | Consuming units | Registered module/package | API input fingerprint | Gate fingerprint and result | Evidence path |",
  "| Resource ID | Type | Owning unit/task | Exact path or safe external ID | State | Cleanup trigger | Last reconciled at |",
  "| Unit | Generation | Integration commit | Worker checkpoint | Conflict evidence path | Status | Recorded at |"
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
disposition_section = nil
artifacts.fetch("UPSTREAM_DISPOSITIONS.md").each_line do |line|
  if (heading_match = line.match(/^## (.+)$/))
    disposition_section = heading_match[1]
  end
  next unless line.start_with?("| ") && !line.start_with?("| ---")

  cells = line.split("|").map(&:strip)
  next if cells[1].nil? || cells[1].match?(/\A(?:Pinned |Source |Profile$|Official |Provider |Package )/)

  disposition_items << "#{disposition_section}\t#{cells[1].delete('`')}"
end
disposition_inventory = upstream_manifest.fetch("disposition_inventory")
fail_check("pinned disposition inventory source drifted") unless disposition_inventory.fetch("source") == "UPSTREAM_DISPOSITIONS.md"
fail_check("pinned disposition count drifted") unless disposition_inventory.fetch("count") == disposition_items.length
fail_check("pinned disposition digest drifted") unless disposition_inventory.fetch("sha256") == canonical_inventory_digest(disposition_items)

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
  "Source-exported and internal plugin surface" => lambda do |item|
    item.start_with?("internal ") ? "packages/better-auth/src/plugins" : "packages/better-auth/src/plugins/index.ts"
  end,
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
  expected_item_id = if section == "Source-exported and internal plugin surface" && item != "internal additional-fields"
                       "#{source_locator}##{item}"
                     else
                       source_locator
                     end
  fail_check("upstream closure item ID misassigned for #{record.fetch('disposition_row_id')}") unless record.fetch("upstream_item_id") == expected_item_id
  if section == "Source-exported and internal plugin surface" && item != "internal additional-fields"
    pinned_export_source = upstream_manifest.fetch("sources").find { |source| source.fetch("path") == "packages/better-auth/src/plugins/index.ts" }
    fail_check("upstream export locator drifted for #{item}") unless source_locator == pinned_export_source.fetch("path")
    fail_check("upstream export object drifted for #{item}") unless record.fetch("source_kind") == pinned_export_source.fetch("kind") && record.fetch("source_object_id") == pinned_export_source.fetch("object_id")
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
fail_check("upstream item closure digest drifted") unless item_closure.fetch("sha256") == EXPECTED_UPSTREAM_CLOSURE_SHA256 && closure_digest == EXPECTED_UPSTREAM_CLOSURE_SHA256
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
worker_placeholders = worker.scan(/<([^>]+)>/).flatten.to_set
unknown_placeholders = worker_placeholders - ALLOWED_WORKER_PLACEHOLDERS
missing_placeholders = ALLOWED_WORKER_PLACEHOLDERS - worker_placeholders
fail_check("worker placeholder mismatch; unknown=#{unknown_placeholders.to_a} missing=#{missing_placeholders.to_a}") unless unknown_placeholders.empty? && missing_placeholders.empty?
fail_check("worker does not read reference profile") unless worker.include?("REFERENCE_PROFILE.md")

reference_profile = File.read(File.join(ROOT, "REFERENCE_PROFILE.md"))
[
  "12 to 128 bytes", "7-day absolute expiry", "24-hour idle expiry",
  "PKCE S256", "API keys contain at least 32 random bytes",
  "`__Host-identity_session`", "PostgreSQL is authoritative"
].each do |required|
  fail_check("reference profile missing decision: #{required}") unless reference_profile.include?(required)
end

ledger = File.read(ledger_fixture_path || File.join(ROOT, "EXECUTION_LEDGER.md"))
ledger_header = "| Unit | Generation | Worker task | Branch | Worktree | Assignment commit | Worker commit | Integration checkpoint | Gate fingerprint | External evidence | Last transition |"
fail_check("execution ledger table header drifted") unless ledger.lines.any? { |line| line.chomp == ledger_header }
ledger_rows = ledger.lines.filter_map do |line|
  next unless line.start_with?("| `")
  cells = line.split("|").map(&:strip)
  fail_check("execution ledger row has wrong column count: #{line.chomp}") unless cells.length == 13
  {
    unit: cells[1][/`([^`]+)`/, 1], generation: cells[2], task: cells[3],
    branch: cells[4], worktree: cells[5], assignment: cells[6],
    worker_commit: cells[7], checkpoint: cells[8], fingerprint: cells[9],
    external: cells[10], transition: cells[11]
  }
end
ledger_units = ledger_rows.map { |row| row[:unit] }
fail_check("execution ledger units do not exactly match inventory") unless ledger_units == units
fail_check("execution ledger contains duplicate units") unless ledger_units.uniq.length == ledger_units.length
fail_check("execution ledger lacks status/owner mirror rule") unless ledger.include?("mirror")

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
  row_line = "| #{source_row.join(' | ')} |"
  resume_commit = first_parent_commit_adding_line(row_line, ".ai/identity-platform/PREFLIGHT_EVIDENCE.md")
  fail_check("authorized recovery #{unit} has no first-parent authorization commit") unless resume_commit
  resume_parent, resume_parent_status = Open3.capture2("git", "-C", REPOSITORY_ROOT, "rev-parse", "#{resume_commit}^1")
  fail_check("authorized recovery #{unit} cannot resolve resume first parent") unless resume_parent_status.success?
  fail_check("authorized recovery #{unit} pins the wrong resume first parent") unless resume_parent.strip == integration_parent
  fail_check("authorized recovery #{unit} resume commit is not on current first-parent history") unless git_ancestor?(resume_commit, "HEAD")
end

historical_proposed_entries = ledger_rows.select do |entry|
  inventory_row = rows.find { |row| row[:unit] == entry[:unit] }
  inventory_row[:status] == "proposed" && entry[:transition] != "initial"
end
if historical_proposed_entries.any? && previous_inventory_path.nil?
  fail_check("post-initial proposed rows require previous transition fixtures: #{historical_proposed_entries.map { |entry| entry[:unit] }.join(', ')}")
end

if previous_inventory_path
  previous_inventory = File.read(previous_inventory_path)
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
  previous_ledger = File.read(previous_ledger_path)
  fail_check("previous execution ledger table header drifted") unless previous_ledger.lines.any? { |line| line.chomp == ledger_header }
  previous_ledger_rows = previous_ledger.lines.filter_map do |line|
    next unless line.start_with?("| `")
    cells = line.split("|").map(&:strip)
    fail_check("previous execution ledger row has wrong column count: #{line.chomp}") unless cells.length == 13
    unit = cells[1]&.match(/\A`([^`]+)`\z/)&.[](1)
    {
      unit: unit, generation: cells[2], task: cells[3], branch: cells[4],
      worktree: cells[5], assignment: cells[6], worker_commit: cells[7],
      checkpoint: cells[8], fingerprint: cells[9], external: cells[10],
      transition: cells[11]
    } if unit
  end
  fail_check("previous transition fixture units do not match inventory") unless previous_inventory_rows.map { |row| row[:unit] } == units && previous_ledger_rows.map { |row| row[:unit] } == units
  previous_statuses = previous_inventory_rows.to_h { |row| [row[:unit], row[:status]] }
  previous_generations = previous_ledger_rows.to_h { |row| [row[:unit], row[:generation]] }
  previous_ledger_rows.each do |entry|
    inventory_row = previous_inventory_rows.find { |row| row[:unit] == entry[:unit] }
    fail_check("previous #{entry[:unit]} ledger generation is invalid") unless entry[:generation].match?(/\A\d+\z/)
    if entry[:transition] == "initial"
      empty_fields = entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :fingerprint, :external)
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
  end

  historical_proposed_entries.each do |entry|
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
    previous_assignment_fields = previous_entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :fingerprint, :external)
    fail_check("#{row[:unit]} previous ready snapshot retained assignment or evidence") unless previous_row[:owner] == "—" && previous_assignment_fields.all? { |value| value == "—" }
    fail_check("#{row[:unit]} previous ready snapshot had unmet prerequisites") unless previous_row[:requires].all? { |required| previous_statuses[required] == "verified" }
    fail_check("#{row[:unit]} ready-to-proposed transition changed generation") unless entry[:generation] == previous_generations[row[:unit]]
    fail_check("#{row[:unit]} returned to proposed without an invalidated prerequisite") if previous_row[:requires].all? { |required| rows.find { |candidate| candidate[:unit] == required }[:status] == "verified" }
  end
end

active_ledger_rows = ledger_rows.select do |entry|
  status = rows.find { |row| row[:unit] == entry[:unit] }[:status]
  ["in-progress", "blocked"].include?(status)
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
    fail_check("#{entry[:unit]} initial ledger row has generation or assignment data") unless entry[:generation] == "0" && entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :fingerprint, :external).all? { |value| value == "—" }
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
  [:worker_commit, :checkpoint].each do |field|
    fail_check("#{entry[:unit]} #{field} is invalid") unless entry[field].match?(hash_or_dash)
  end
  assignment_fields = entry.values_at(:task, :branch, :worktree, :assignment)
  integrated_fields = entry.values_at(:worker_commit, :checkpoint)
  case inventory_row[:status]
  when "proposed"
    fail_check("#{entry[:unit]} proposed owner is not empty") unless inventory_row[:owner] == "—"
    fail_check("#{entry[:unit]} proposed row retains assignment or evidence") unless entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :fingerprint, :external).all? { |value| value == "—" }
  when "ready"
    fail_check("#{entry[:unit]} ready owner is not empty") unless inventory_row[:owner] == "—"
    fail_check("#{entry[:unit]} ready row retains assignment or evidence") unless entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint, :fingerprint, :external).all? { |value| value == "—" }
  when "in-progress"
    fail_check("#{entry[:unit]} in-progress assignment fields are incomplete") if assignment_fields.any? { |value| value == "—" }
    fail_check("#{entry[:unit]} in-progress owner must equal worker task") unless inventory_row[:owner] != "—" && inventory_row[:owner] == entry[:task]
    fail_check("#{entry[:unit]} in-progress assignment commit is missing") if entry[:assignment] == "—"
    if entry[:checkpoint] == "—"
      fail_check("#{entry[:unit]} pre-integration row has a gate fingerprint") unless entry[:fingerprint] == "—"
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
      fail_check("#{entry[:unit]} integrated repair gate fingerprint is invalid") unless entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
    end
  when "blocked"
    fail_check("#{entry[:unit]} blocked owner must be a safe blocker ID") unless inventory_row[:owner].match?(/\Ablocker:[a-zA-Z0-9._-]+\z/)
    fail_check("#{entry[:unit]} blocked row lost its assignment") if assignment_fields.any? { |value| value == "—" || value == "pending" }
    if entry[:checkpoint] == "—"
      fail_check("#{entry[:unit]} pre-integration blocked row has a fingerprint") unless entry[:fingerprint] == "—"
    else
      fail_check("#{entry[:unit]} integrated blocked row lacks worker commit") if entry[:worker_commit] == "—"
      fail_check("#{entry[:unit]} integrated blocked gate fingerprint is invalid") unless entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
    end
  when "implemented-unverified", "verified"
    fail_check("#{entry[:unit]} integrated owner is not empty") unless inventory_row[:owner] == "—"
    fail_check("#{entry[:unit]} integrated fields are incomplete") if (assignment_fields + integrated_fields).any? { |value| value == "—" || value == "pending" }
    fail_check("#{entry[:unit]} gate fingerprint is invalid") unless entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
    fail_check("#{entry[:unit]} integrated external evidence disposition is missing") if entry[:external] == "—"
  end
  unless entry[:task] == "—"
    fail_check("#{entry[:unit]} worker task is unsafe") unless entry[:task].match?(/\A[a-zA-Z0-9._\/-]+\z/)
    fail_check("#{entry[:unit]} worker branch is not conventional") unless entry[:branch].match?(%r{\A(?:feature|bugfix|hotfix|release|chore|refactor)/[a-zA-Z0-9._/-]+\z})
    unless safe_worktree_path?(entry[:worktree], task_owned_worktree_parent)
      fail_check("#{entry[:unit]} worker worktree is not beneath the safe task-owned preflight parent")
    end
  end
  external_ok = entry[:external] == "—" || ["not-needed", "available"].include?(entry[:external]) || entry[:external].match?(/\Aunavailable:[a-zA-Z0-9._-]+\z/) || entry[:external].match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z})
  fail_check("#{entry[:unit]} external evidence field is invalid") unless external_ok
  if entry[:external].start_with?(".ai/")
    evidence_path = File.expand_path(entry[:external], REPOSITORY_ROOT)
    fail_check("#{entry[:unit]} external evidence path escapes repository") unless evidence_path.start_with?(REPOSITORY_ROOT + File::SEPARATOR)
    fail_check("#{entry[:unit]} external evidence path is missing") unless File.file?(evidence_path)
  end
  if inventory_row[:status] == "verified"
    fail_check("#{entry[:unit]} verified with unavailable external evidence") if entry[:external] == "—" || entry[:external].start_with?("unavailable:")
    fail_check("#{entry[:unit]} verified external claim lacks attributable evidence path") if entry[:external] == "available"
  end
end

puts "identity-platform validation: #{rows.length} units, #{inventory_edges.length} edges, #{depth.values.max + 1} waves, #{operation_ids.length} operations, #{route_records.length} HTTP mappings, #{openapi_owners.length} OpenAPI operations, parity baseline #{BASELINE}"
