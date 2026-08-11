#!/usr/bin/env ruby
# frozen_string_literal: true

require "set"

ROOT = File.expand_path(__dir__)
REPOSITORY_ROOT = File.expand_path("../..", ROOT)
EXPECTED_UNITS = 58
BASELINE = "b8077b74ef9a80a7757220b72834349bd8de05c0"
ALLOWED_WORKER_PLACEHOLDERS = Set[
  "unit", "canonical-module", "absolute-worktree-path", "worker-branch",
  "integration-commit", "absolute-goal-path", "verified-prerequisite-list",
  "canonical-module-directory", "reserved-descendant-module-directories",
  "assignment-generation", "assignment-commit"
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
  "organization", "sso", "sso/oidc", "sso/oauth2", "sso/saml", "scim",
  "scim/organization", "oauth-server", "oauth-server/oidc",
  "oauth-server/device", "identity/i18n"
].freeze
REFERENCE_ADAPTERS = Set[
  "identity/postgres", "identity/session/postgres", "identity/session/valkey",
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

def fail_check(message)
  warn "identity-platform validation: #{message}"
  exit 1
end

inventory = File.read(File.join(ROOT, "INVENTORY.md"))
rows = inventory.lines.filter_map do |line|
  next unless line.start_with?("| `")

  cells = line.split("|").map(&:strip)
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
known = units.to_set

rows.each do |row|
  fail_check("#{row[:unit]} has invalid status #{row[:status]}") unless ALLOWED_STATUSES.include?(row[:status])
  unknown = row[:requires].reject { |unit| known.include?(unit) }
  fail_check("#{row[:unit]} has unknown dependencies: #{unknown.join(', ')}") unless unknown.empty?
end

http_row = rows.find { |row| row[:unit] == "identity/http" }
reference_row = rows.find { |row| row[:unit] == "identity/reference" }
fail_check("identity/http feature dependency set drifted") unless http_row[:requires].to_set == HTTP_FEATURES
expected_reference = REFERENCE_ADAPTERS | Set["identity/http"]
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
reverse = Hash.new { |hash, key| hash[key] = [] }
rows.each { |row| row[:requires].each { |dependency| reverse[dependency] << row[:unit] } }

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
    actual_requires = requires_line.scan(/`([^`]+)`/).flatten
    fail_check("requires mismatch for #{row[:unit]}: expected #{row[:requires]}, got #{actual_requires}") unless actual_requires == row[:requires]
  end
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
parity.each_line do |line|
  cells = line.split("|").map(&:strip)
  next unless cells.length >= 6 && cells[2] == "In"
  owners = cells[3].scan(/`([^`]+)`/).flatten
  fail_check("in-scope parity row lacks owner: #{cells[1]}") if owners.empty?
  unknown = owners.reject { |owner| known.include?(owner) || EXISTING_OWNERS.include?(owner) }
  fail_check("unknown parity owners for #{cells[1]}: #{unknown.join(', ')}") unless unknown.empty?
  fail_check("in-scope parity row lacks acceptance or verification: #{cells[1]}") if cells[4].empty? || cells[5].empty?
end
parity.each_line do |line|
  cells = line.split("|").map(&:strip)
  next unless cells.length >= 4 && cells[2] == "Excluded"
  fail_check("excluded parity row lacks rationale: #{cells[1]}") if cells[3].empty?
end

orchestrator = File.read(File.join(ROOT, "ORCHESTRATOR_GOAL.md"))
fail_check("orchestrator contains unresolved placeholder") if orchestrator.match?(/<[^>]+>/)
[
  "gpt-5.6-sol", "reasoning effort `medium`", "fork_turns: \"none\"",
  "git merge --no-ff", "implemented-unverified", "REFERENCE_PROFILE.md",
  "Interruption and worker-failure recovery", "A unit MUST NOT jump directly"
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
  "6 decimal digits", "PKCE S256", "API keys contain at least 32 random bytes",
  "`__Host-identity_session`", "PostgreSQL is authoritative"
].each do |required|
  fail_check("reference profile missing decision: #{required}") unless reference_profile.include?(required)
end

ledger = File.read(File.join(ROOT, "EXECUTION_LEDGER.md"))
ledger_rows = ledger.lines.filter_map do |line|
  next unless line.start_with?("| `")
  cells = line.split("|").map(&:strip)
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
  fail_check("#{entry[:unit]} ledger status drift") unless match[2] == inventory_row[:status]
  fail_check("#{entry[:unit]} ledger owner/blocker drift") unless match[3] == inventory_row[:owner]
  fail_check("#{entry[:unit]} assignment commit is invalid") unless entry[:assignment] == "pending" || entry[:assignment].match?(hash_or_dash)
  [:worker_commit, :checkpoint].each do |field|
    fail_check("#{entry[:unit]} #{field} is invalid") unless entry[field].match?(hash_or_dash)
  end
  if inventory_row[:status] == "in-progress"
    fail_check("#{entry[:unit]} in-progress assignment fields are incomplete") if entry.values_at(:task, :branch, :worktree).any? { |value| value == "—" }
    fail_check("#{entry[:unit]} in-progress owner must equal worker task") unless inventory_row[:owner] != "—" && inventory_row[:owner] == entry[:task]
    fail_check("#{entry[:unit]} in-progress assignment commit is missing") if entry[:assignment] == "—"
  elsif ["implemented-unverified", "verified"].include?(inventory_row[:status])
    fail_check("#{entry[:unit]} integrated fields are incomplete") if entry.values_at(:task, :branch, :worktree, :assignment, :worker_commit, :checkpoint).any? { |value| value == "—" || value == "pending" }
    fail_check("#{entry[:unit]} gate fingerprint is invalid") unless entry[:fingerprint].match?(/\Asha256:[0-9a-f]{64}\z/)
    fail_check("#{entry[:unit]} integrated external evidence disposition is missing") if entry[:external] == "—"
  end
  unless entry[:task] == "—"
    fail_check("#{entry[:unit]} worker task is unsafe") unless entry[:task].match?(/\A[a-zA-Z0-9._\/-]+\z/)
    fail_check("#{entry[:unit]} worker branch is not conventional") unless entry[:branch].match?(%r{\A(?:feature|bugfix|hotfix|release|chore|refactor)/[a-zA-Z0-9._/-]+\z})
    fail_check("#{entry[:unit]} worker worktree is not absolute") unless entry[:worktree].start_with?("/")
  end
  external_ok = entry[:external] == "—" || ["not-needed", "available"].include?(entry[:external]) || entry[:external].match?(/\Aunavailable:[a-zA-Z0-9._-]+\z/) || entry[:external].match?(%r{\A\.ai/[a-zA-Z0-9._/-]+\z})
  fail_check("#{entry[:unit]} external evidence field is invalid") unless external_ok
  if inventory_row[:status] == "verified"
    fail_check("#{entry[:unit]} verified with unavailable external evidence") if entry[:external] == "—" || entry[:external].start_with?("unavailable:")
  end
end

puts "identity-platform validation: #{rows.length} units, #{inventory_edges.length} edges, #{depth.values.max + 1} waves, parity baseline #{BASELINE}"
