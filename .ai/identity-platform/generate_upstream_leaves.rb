#!/usr/bin/env ruby
# frozen_string_literal: true

require "digest"
require "json"
require "open3"
require "set"

ROOT = File.expand_path(__dir__)
SURFACE_PATH = File.join(ROOT, "UPSTREAM_SURFACE.json")
OUTPUT_PATH = File.join(ROOT, "UPSTREAM_LEAVES.json")
DISPOSITIONS_PATH = File.join(ROOT, "UPSTREAM_DISPOSITIONS.md")

REQUIRED_SCOPE_DISPOSITIONS = {
  "Official plugin documentation tree\tagent-auth" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tautumn" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tchargebee" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tcommet" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tcreem" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tdodopayments" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tmcp" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tpolar" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tsiwe" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tstripe" => ["Excluded", "product-exclusion"],
  "Source-exported and internal plugin surface\tmcp" => ["Excluded", "product-exclusion"],
  "Source-exported and internal plugin surface\tsiwe" => ["Excluded", "product-exclusion"],
  "Official top-level packages\tstripe" => ["Excluded", "product-exclusion"],
  "Official plugin documentation tree\tcaptcha" => ["In", "in-scope"],
  "Official plugin documentation tree\thave-i-been-pwned" => ["In", "in-scope"],
  "Source-exported and internal plugin surface\tcaptcha" => ["In", "in-scope"],
  "Source-exported and internal plugin surface\thaveibeenpwned" => ["In", "in-scope"]
}.freeze

check = ARGV.delete("--check")
upstream_repository = ARGV.shift
abort "usage: generate_upstream_leaves.rb [--check] BETTER_AUTH_REPOSITORY" unless upstream_repository && ARGV.empty?

surface = JSON.parse(File.read(SURFACE_PATH))
revision = surface.fetch("upstream").fetch("revision")
semantic_rows = surface.fetch("item_closure").fetch("rows")
semantic_by_id = semantic_rows.to_h { |row| [row.fetch("disposition_row_id"), row] }

disposition_rows = {}
disposition_lines = []
section = nil
File.foreach(DISPOSITIONS_PATH) do |line|
  section = Regexp.last_match(1) if line.match(/^## (.+)$/)
  next unless line.start_with?("| ") && !line.start_with?("| ---")

  cells = line.split("|").map(&:strip)
  next if cells[1].nil? || cells[1].match?(/\A(?:Pinned |Source |Profile$|Official |Provider |Package )/)

  values = cells[1..3].map { |value| value.delete("`") }
  id = "#{section}\t#{values.fetch(0)}"
  abort "duplicate semantic disposition row: #{id}" if disposition_rows.key?(id)
  disposition_rows[id] = {"disposition" => values.fetch(1), "rationale" => values.fetch(2)}
  disposition_lines << ([section] + values).join("\t")
end
inventory = surface.fetch("disposition_inventory")
abort "disposition inventory count drifted" unless inventory.fetch("count") == disposition_lines.length
abort "disposition inventory semantic digest drifted" unless inventory.fetch("sha256") == Digest::SHA256.hexdigest(disposition_lines.join("\n") + "\n")
abort "surface and Markdown disposition rows differ" unless disposition_rows.keys.to_set == semantic_by_id.keys.to_set
REQUIRED_SCOPE_DISPOSITIONS.each do |id, (disposition, classification)|
  abort "required scope disposition drifted: #{id}" unless disposition_rows.dig(id, "disposition") == disposition
  abort "required scope classification drifted: #{id}" unless semantic_by_id.dig(id, "classification") == classification
end

git = lambda do |*arguments|
  output, status = Open3.capture2e("git", "-C", upstream_repository, *arguments)
  abort output unless status.success?
  output
end

abort "pinned Better Auth revision is unavailable" unless git.call("rev-parse", "#{revision}^{commit}").strip == revision

row_id = lambda do |section, item|
  id = "#{section}\t#{item}"
  abort "unknown semantic disposition row: #{id}" unless semantic_by_id.key?(id)
  id
end

parse_entries = lambda do |output|
  output.lines.map do |line|
    metadata, path = line.chomp.split("\t", 2)
    mode, kind, object_id = metadata.split(" ", 3)
    {"path" => path, "mode" => mode, "kind" => kind, "object_id" => object_id}
  end.sort_by { |entry| entry.fetch("path").b }
end

source_records = surface.fetch("sources").map do |source|
  path = source.fetch("path")
  policy = source.fetch("enumeration")
  actual_id = git.call("rev-parse", "#{revision}:#{path}").strip
  abort "pinned source object drifted: #{path}" unless actual_id == source.fetch("object_id")
  if policy == "recursive_blobs"
    entries = parse_entries.call(git.call("ls-tree", "-r", "-t", revision, "--", path))
    entries.select! { |entry| entry.fetch("path").start_with?("#{path}/") }
  elsif policy == "exact_blob"
    entries = parse_entries.call(git.call("ls-tree", revision, "--", path))
  else
    entries = parse_entries.call(git.call("ls-tree", "#{revision}:#{path}"))
    entries.each { |entry| entry["path"] = "#{path}/#{entry.fetch('path')}" }
  end
  abort "empty pinned source: #{path}" if entries.empty?
  {
    "path" => path,
    "kind" => source.fetch("kind"),
    "object_id" => source.fetch("object_id"),
    "enumeration" => policy,
    "entries" => entries
  }
end.sort_by { |source| source.fetch("path").b }

leaves = source_records.flat_map do |source|
  entries = source.fetch("entries")
  selected = if source.fetch("enumeration") == "recursive_blobs"
               entries.select { |entry| entry.fetch("kind") == "blob" }
             else
               entries
             end
  selected.map { |entry| entry.merge("source_path" => source.fetch("path")) }
end

# Declared sources intentionally overlap: the packages tree proves the complete
# monorepo package closure while narrower route, plugin, provider, and exact-file
# sources provide the more precise semantic owner. A physical blob has one
# canonical leaf disposition, selected by the longest matching source path.
leaves = leaves.group_by { |leaf| leaf.fetch("path") }.values.map do |candidates|
  candidates.max_by { |leaf| [leaf.fetch("source_path").length, leaf.fetch("source_path").b] }
end

plugin_export_items = semantic_rows.filter_map do |row|
  section, item = row.fetch("disposition_row_id").split("\t", 2)
  item if section == "Source-exported and internal plugin surface" && !item.start_with?("internal ")
end
provider_rows = semantic_rows.select { |row| row.fetch("disposition_row_id").start_with?("Provider catalog disposition\t") }

semantic_id_for = lambda do |path, source_path|
  case source_path
  when "docs/content/docs/concepts"
    row_id.call("Core documentation and route surface", "concepts #{File.basename(path, ".mdx")}")
  when "docs/content/docs/authentication"
    item = if path.end_with?("/email-password.mdx")
             "authentication email-password"
           elsif path.end_with?("/apple.mdx")
             "authentication Apple provider page"
           else
             "authentication provider pages"
           end
    row_id.call("Core documentation and route surface", item)
  when "docs/content/docs/plugins"
    relative = path.delete_prefix("docs/content/docs/plugins/")
    item = case relative
           when "api-key/advanced.mdx" then "api-key advanced"
           when "api-key/reference.mdx" then "api-key reference"
           when "api-key/meta.json" then "api-key metadata"
           when "api-key/index.mdx" then "api-key"
           else
             if File.basename(relative) == "meta.json" || relative == "index.mdx"
               "plugin index/meta pages"
             else
               relative.delete_suffix(".mdx").delete_suffix("/index")
             end
           end
    row_id.call("Official plugin documentation tree", item)
  when "packages/better-auth/src/api/routes"
    basename = File.basename(path)
    route = case basename
            when "index.ts" then "index/meta"
            when "cookie-cache-fallback.test.ts", "session-api.test.ts" then "session.ts"
            else basename.sub(/\.test(?=\.ts\z)/, "")
            end
    row_id.call("Core documentation and route surface", "route #{route}")
  when "packages/better-auth/src/plugins"
    relative = path.delete_prefix("packages/better-auth/src/plugins/")
    if relative == "index.ts"
      row_id.call("Source-exported and internal plugin surface", "plugin source index")
    elsif relative.start_with?("generic-oauth/providers/") && !relative.end_with?("/index.ts")
      provider_path = path.sub(/\.test\.ts\z/, ".ts")
      match = provider_rows.find { |row| row.fetch("source_locator", "") == provider_path }
      match ? match.fetch("disposition_row_id") : row_id.call("Source-exported and internal plugin surface", "generic-oauth")
    elsif relative.start_with?("additional-fields/")
      row_id.call("Source-exported and internal plugin surface", "internal additional-fields")
    else
      directory = relative.split("/", 2).first
      export_item = {"have-i-been-pwned" => "haveibeenpwned", "two-factor" => "two-factor"}.fetch(directory, directory)
      abort "unclassified plugin source leaf: #{path}" unless plugin_export_items.include?(export_item)
      row_id.call("Source-exported and internal plugin surface", export_item)
    end
  when "packages/better-auth/src/types/plugins.ts", "packages/better-auth/src/utils/hide-metadata.ts"
    row_id.call("Source-exported and internal plugin surface", "types/plugins, hide-metadata")
  when "packages/core/src/social-providers"
    basename = File.basename(path, ".ts").delete_suffix(".test")
    if basename == "index"
      row_id.call("Provider catalog disposition", "provider source index")
    else
      provider_path = path.sub(/\.test\.ts\z/, ".ts")
      match = provider_rows.find { |row| row.fetch("source_locator", "") == provider_path }
      abort "unclassified provider source leaf: #{path}" unless match
      match.fetch("disposition_row_id")
    end
  when "packages"
    row_id.call("Official top-level packages", path.delete_prefix("packages/").split("/", 2).first)
  else
    abort "unclassified pinned source leaf: #{path} (#{source_path})"
  end
end


package_semantics = semantic_rows.filter_map do |row|
  section, package = row.fetch("disposition_row_id").split("\t", 2)
  [package, row] if section == "Official top-level packages"
end.to_h
capability_operations = surface.fetch("item_closure").fetch("capability_operations")

classification = lambda do |id, semantic, override = {}|
  {
    "exact_disposition_id" => id,
    "disposition_row_id" => semantic.fetch("disposition_row_id"),
    "classification" => override.fetch("classification", semantic.fetch("classification")),
    "capability_ids" => override.fetch("capability_ids", semantic.fetch("capability_ids")),
    "operation_ids" => override.fetch("operation_ids", semantic.fetch("operation_ids")),
    "classification_basis" => override.fetch("classification_basis", "exact semantic disposition row")
  }
end

empty_override = lambda do |classification_name, basis|
  {
    "classification" => classification_name,
    "capability_ids" => [],
    "operation_ids" => [],
    "classification_basis" => basis
  }
end

in_scope_override = lambda do |capability_ids, basis|
  unknown = capability_ids - capability_operations.keys
  abort "unknown exact capability classification: #{unknown}" unless unknown.empty?
  {
    "classification" => "in-scope",
    "capability_ids" => capability_ids.sort_by(&:b),
    "operation_ids" => capability_ids.flat_map { |id| capability_operations.fetch(id) }.uniq.sort_by(&:b),
    "classification_basis" => basis
  }
end

oauth_provider_server_capabilities = ["OAuth 2.1 provider", "OIDC provider"].freeze
oauth_resource_capabilities = ["OAuth protected resources"].freeze
oauth_resource_operations = %w[
  identity.oauth-server.protected-resource-metadata
  identity.oauth-server.resource-verify
].freeze
oauth_resource_override = lambda do |basis|
  {
    "classification" => "in-scope",
    "capability_ids" => oauth_resource_capabilities,
    "operation_ids" => oauth_resource_operations,
    "classification_basis" => basis
  }
end

package_for_path = lambda do |path|
  package = path.delete_prefix("packages/").split("/", 2).first
  abort "unknown top-level package leaf: #{path}" unless package_semantics.key?(package)
  package
end

package_leaf_classification = lambda do |path|
  package = package_for_path.call(path)
  semantic = package_semantics.fetch(package)
  relative = path.delete_prefix("packages/#{package}/")

  packaging_metadata = relative.match?(%r{\A(?:CHANGELOG\.md|README\.md|package\.json|tsconfig(?:\.[^.]+)?\.json|tsdown\.config\.ts|vitest\.config(?:\.[^.]+)?\.ts|src/version\.ts)\z})
  if packaging_metadata
    next classification.call(
      "package/#{package}/packaging-metadata",
      semantic,
      empty_override.call("non-capability", "package documentation, manifest, version, or build/test configuration; exported entries are disposed separately")
    )
  end

  if package == "cli"
    database_tooling_profile = relative == "src/api.ts" ||
                               relative.start_with?("src/generators/") ||
                               relative == "src/commands/generate.ts" ||
                               relative == "src/commands/migrate.ts" ||
                               relative.match?(%r{\Atest/(?:generate(?:-all-db)?|migrate)\.test\.ts\z}) ||
                               relative.start_with?("test/__snapshots__/")
    if database_tooling_profile
      next classification.call(
        "package/cli/unselected-or-mixed-database-tooling-profile",
        package_semantics.fetch("drizzle-adapter"),
        "classification_basis" => "JavaScript ORM/dialect schema or migration tooling is an unselected deployment profile; mixed artifacts are conservatively divergent because their PostgreSQL portion is not isolated as an exact upstream item"
      )
    end

    cli_operations = case relative
                     when "src/commands/info.ts", "test/info.test.ts"
                       ["Operational tooling", ["identity.reference.diagnostics"]]
                     when "src/commands/secret.ts"
                       ["Operational tooling", ["identity.reference.secret-generate"]]
                     when "src/utils/get-config.ts", "test/get-config.test.ts"
                       ["Operational tooling", ["identity.reference.config-validate"]]
                     end
    if cli_operations
      capability_id, operation_ids = cli_operations
      next classification.call(
        "package/cli/direct-backend-operational-api",
        semantic,
        "classification" => "in-scope",
        "capability_ids" => [capability_id],
        "operation_ids" => operation_ids,
        "classification_basis" => "schema generation, migration, secret, or redacted diagnostic behavior retained as direct identity/reference APIs rather than a CLI product surface"
      )
    end

    excluded_remote = relative.match?(%r{\Asrc/commands/(?:ai|login|mcp)\.ts\z})
    if excluded_remote
      next classification.call(
        "package/cli/excluded-remote-command",
        semantic,
        empty_override.call("product-exclusion", "AI, hosted-account login, and MCP command products are outside the selected backend identity scope")
      )
    end

    next classification.call(
      "package/cli/javascript-project-management-tooling",
      semantic,
      empty_override.call("non-capability-tooling", "JavaScript project initialization, source rewriting, dependency installation/upgrading, prompts, and command wiring are excluded tooling")
    )
  end

  if package == "better-auth"
    if relative.start_with?("src/client/") || relative.start_with?("src/integrations/")
      next classification.call(
        "package/better-auth/javascript-client-or-framework-integration",
        semantic,
        empty_override.call("client-surface", "JavaScript reactive clients and framework integrations are outside the Go backend surface")
      )
    end
    if relative.start_with?("src/adapters/")
      next classification.call(
        "package/better-auth/unselected-adapter-export",
        semantic,
        empty_override.call("deployment-profile-divergence", "embedded JavaScript database adapters are outside the selected PostgreSQL and Valkey deployment profile")
      )
    end
    if relative.start_with?("src/test-utils/")
      test_semantic = package_semantics.fetch("test-utils")
      next classification.call(
        "package/better-auth/test-utilities",
        test_semantic,
        "classification_basis" => "backend test helpers retained through identity/identitytest"
      )
    end
    if relative.match?(%r{\Asrc/db/(?:get-migration(?:-schema)?(?:\.test)?|adapter-kysely|internal-adapter\.test)\.ts\z})
      next classification.call(
        "package/better-auth/unselected-or-mixed-database-migration",
        package_semantics.fetch("kysely-adapter"),
        "classification_basis" => "JavaScript Kysely or mixed PostgreSQL/MySQL/SQLite/MSSQL schema-migration artifact is an unselected deployment profile; it does not prove the local direct Go PostgreSQL migration contract"
      )
    end
    capability_ids = case relative
                     when %r{\Asrc/api/(?:middlewares|rate-limiter)/}
                       ["Core HTTP rate limiting", "Dynamic base URL and trusted origins", "Typed extension modules"]
                     when %r{\Asrc/api/(?:dispatch|index|to-auth-endpoints|check-endpoint-conflicts|server-only-endpoints)}
                       ["Core HTTP rate limiting", "Typed extension modules"]
                     when %r{\Asrc/api/state/oauth\.ts\z}, %r{\Asrc/oauth2/}, %r{\Asrc/social-providers/}, "src/social.test.ts"
                       ["Social OAuth / generic OAuth"]
                     when %r{\Asrc/auth/}, %r{\Asrc/context/}, %r{\Asrc/call(?:\.test)?\.ts\z}
                       ["Dynamic base URL and trusted origins", "Hooks and request extension", "Typed extension modules"]
                     when %r{\Asrc/cookies/}, "src/api/state/should-session-refresh.ts", "src/state.ts"
                       ["Stateful, cached, and stateless sessions"]
                     when %r{\Asrc/crypto/password(?:\.test)?\.ts\z}
                       ["Email/password"]
                     when "src/crypto/jwt.ts"
                       ["JWT issuance and JWKS"]
                     when %r{\Asrc/crypto/}
                       ["One-time workflow tokens"]
                     when %r{\Asrc/db/}
                       ["Hooks and request extension", "One-time workflow tokens", "Stateful, cached, and stateless sessions", "Typed additional fields", "User/account lifecycle"]
                     when %r{\Asrc/instrumentation(?:\.|/)}
                       ["Safe instrumentation"]
                     end
    if capability_ids
      next classification.call(
        "package/better-auth/backend-server/#{capability_ids.join('+')}",
        semantic,
        in_scope_override.call(capability_ids, "explicit server subsystem classification with only the subsystem's backend operation closure")
      )
    end

    next classification.call(
      "package/better-auth/backend-aggregation-or-utility",
      semantic,
      empty_override.call("non-capability", "backend export aggregation, type, or implementation utility with no independent identity operation; declared exports are classified separately")
    )
  end

  if package == "core"
    if relative == "src/types/plugin-client.ts"
      next classification.call(
        "package/core/javascript-client-type",
        semantic,
        empty_override.call("client-surface", "JavaScript client-plugin type surface is outside the Go backend contract")
      )
    end
    if relative.start_with?("src/instrumentation/")
      telemetry_semantic = package_semantics.fetch("telemetry")
      next classification.call(
        "package/core/instrumentation",
        telemetry_semantic,
        "classification_basis" => "instrumentation retained through the safer bounded local contract"
      )
    end
    capability_ids = case relative
                     when %r{\Asrc/oauth2/}
                       ["Social OAuth / generic OAuth"]
                     when %r{\Asrc/db/}
                       ["Hooks and request extension", "Stateful, cached, and stateless sessions", "Typed additional fields", "User/account lifecycle"]
                     when %r{\Asrc/(?:api|async_hooks|context)/}
                       ["Hooks and request extension", "Typed extension modules"]
                     when %r{\Asrc/utils/(?:host|ip|redirect-uri|url)(?:\.test)?\.ts\z}
                       ["Core HTTP rate limiting", "Dynamic base URL and trusted origins"]
                     end
    if capability_ids
      next classification.call(
        "package/core/backend-contract/#{capability_ids.join('+')}",
        semantic,
        in_scope_override.call(capability_ids, "explicit core subsystem classification with only the subsystem's backend operation closure")
      )
    end
    next classification.call(
      "package/core/backend-type-or-utility",
      semantic,
      empty_override.call("non-capability", "shared backend type, environment, error, or utility code with no independent identity operation")
    )
  end

  if %w[drizzle-adapter kysely-adapter memory-adapter mongo-adapter prisma-adapter].include?(package)
    next classification.call(
      "package/#{package}/unselected-adapter",
      semantic,
      "classification_basis" => "explicitly inspected unselected database adapter package"
    )
  end

  if %w[electron expo].include?(package)
    next classification.call(
      "package/#{package}/javascript-client-integration",
      semantic,
      "classification_basis" => "explicitly inspected desktop or mobile JavaScript client integration"
    )
  end

  if package == "stripe"
    next classification.call(
      "package/stripe/billing-integration",
      semantic,
      "classification_basis" => "explicitly inspected billing and payment integration"
    )
  end

  if package == "oauth-provider" && relative == "src/client-resource.ts"
    next classification.call(
      "package/oauth-provider/protected-resource-client",
      semantic,
      oauth_resource_override.call("standards-generic protected-resource metadata and access-token verification client")
    )
  end

  if package == "oauth-provider" && relative.match?(%r{\Asrc/mcp(?:\.test)?\.ts\z})
    mcp_semantic = semantic_by_id.fetch("Source-exported and internal plugin surface\tmcp")
    next classification.call(
      "package/oauth-provider/mcp-extension",
      mcp_semantic,
      "classification_basis" => "MCP-specific authorization-server extension excluded while standards-generic OAuth/OIDC remains in scope"
    )
  end

  if package == "oauth-provider" && relative == "src/index.ts"
    next classification.call(
      "package/oauth-provider/mixed-server-index",
      semantic,
      "classification" => "in-scope+product-exclusion",
      "capability_ids" => oauth_provider_server_capabilities,
      "operation_ids" => oauth_provider_server_capabilities.flat_map { |id| capability_operations.fetch(id) }.uniq.sort_by(&:b),
      "classification_basis" => "mixed server index: OAuth/OIDC provider exports are retained while the explicitly re-exported MCP handler remains excluded"
    )
  end

  if relative.match?(%r{\A(?:src|test)/.*(?:^|/)client(?:\.test)?\.ts\z}) || relative == "src/client.ts"
    next classification.call(
      "package/#{package}/javascript-client",
      semantic,
      empty_override.call("client-surface", "JavaScript client helper is excluded while its backend HTTP operations remain closed elsewhere")
    )
  end


  if package == "oauth-provider"
    next classification.call(
      "package/oauth-provider/backend-capability",
      semantic,
      in_scope_override.call(oauth_provider_server_capabilities, "OAuth/OIDC authorization-server implementation, protocol fixture, or behavioral test")
    )
  end

  classification.call(
    "package/#{package}/backend-capability",
    semantic,
    "classification_basis" => "package-specific backend implementation, protocol fixture, or behavioral test"
  )
end

leaf_dispositions = leaves.map do |leaf|
  disposition_row_id = semantic_id_for.call(leaf.fetch("path"), leaf.fetch("source_path"))
  semantic = semantic_by_id.fetch(disposition_row_id)
  plugin_client_leaf = leaf.fetch("source_path") == "packages/better-auth/src/plugins" &&
                       semantic.fetch("classification").split("+").include?("in-scope") &&
                       (File.basename(leaf.fetch("path")) == "client.ts" ||
                        File.basename(leaf.fetch("path")).include?(".client.test.") ||
                        leaf.fetch("path").include?("/client/"))
  exact = if plugin_client_leaf
            classification.call(
              "package/better-auth/plugin-javascript-client",
              semantic,
              empty_override.call("client-surface", "JavaScript plugin client declaration or test; the corresponding backend operation remains closed by its server leaves")
            )
          elsif leaf.fetch("source_path") == "packages"
            package_leaf_classification.call(leaf.fetch("path"))
          else
            classification.call("semantic/#{disposition_row_id}", semantic)
          end
  classification_rule_id = exact.delete("exact_disposition_id")
  {
    "exact_disposition_id" => "blob/#{leaf.fetch('path')}",
    "classification_rule_id" => classification_rule_id,
    "path" => leaf.fetch("path"),
    "kind" => leaf.fetch("kind"),
    "object_id" => leaf.fetch("object_id"),
    "source_path" => leaf.fetch("source_path"),
  }.merge(exact)
end.sort_by { |leaf| [leaf.fetch("path").b, leaf.fetch("source_path").b] }

package_json_entries = source_records.find { |source| source.fetch("path") == "packages" }.fetch("entries").select do |entry|
  entry.fetch("path").match?(%r{\Apackages/[^/]+/package\.json\z})
end

export_dispositions = package_json_entries.flat_map do |entry|
  package = package_for_path.call(entry.fetch("path"))
  manifest = JSON.parse(git.call("show", "#{revision}:#{entry.fetch('path')}"))
  exports = manifest.fetch("exports", {})
  exports = {"." => exports} unless exports.is_a?(Hash)
  records = exports.map do |export_name, declaration|
    target = if declaration.is_a?(String)
               declaration
             elsif declaration.is_a?(Hash)
               declaration["dev-source"] || declaration["default"] || declaration["import"] || declaration["types"]
             end
    abort "unresolved package export: #{package} #{export_name}" unless target.is_a?(String)
    [export_name, "library-export", target]
  end
  manifest.fetch("bin", {}).each do |name, target|
    records << [name, "command-bin", target]
  end

  records.map do |export_name, export_kind, target|
    synthetic_path = "packages/#{package}/#{target.delete_prefix('./')}"
    exact = if export_kind == "command-bin"
              classification.call(
                "package/#{package}/javascript-command-bin/#{export_name}",
                package_semantics.fetch(package),
                empty_override.call("non-capability-tooling", "JavaScript command entry point; direct backend operational APIs are classified independently")
              )
            elsif package == "cli" && export_name == "./api"
              classification.call(
                "package/cli/unselected-database-tooling-export",
                package_semantics.fetch("drizzle-adapter"),
                "classification_basis" => "importable JavaScript ORM/dialect schema-generation export is an unselected deployment profile; the local direct Go schema contract is specified independently"
              )
            elsif package == "better-auth" && export_name.start_with?("./plugins/mcp")
              classification.call(
                "package/better-auth/mcp-client-export",
                semantic_by_id.fetch("Source-exported and internal plugin surface\tmcp"),
                "classification_basis" => "MCP-specific client export is excluded"
              )
            elsif package == "better-auth" && export_name == "./plugins/siwe"
              classification.call(
                "package/better-auth/siwe-export",
                semantic_by_id.fetch("Source-exported and internal plugin surface\tsiwe"),
                "classification_basis" => "SIWE export is excluded"
              )
            elsif package == "better-auth" && export_name == "./plugins"
              aggregate_rows = semantic_rows.select do |row|
                section, item = row.fetch("disposition_row_id").split("\t", 2)
                section == "Source-exported and internal plugin surface" &&
                  !item.start_with?("internal ") &&
                  !["plugin source index", "types/plugins, hide-metadata"].include?(item) &&
                  row.fetch("classification").split("+").include?("in-scope")
              end
              aggregate_capabilities = aggregate_rows.flat_map { |row| row.fetch("capability_ids") }.uniq.sort_by(&:b)
              aggregate_operations = aggregate_rows.flat_map { |row| row.fetch("operation_ids") }.uniq.sort_by(&:b)
              classification.call(
                "package/better-auth/plugin-aggregation-export",
                semantic_by_id.fetch("Source-exported and internal plugin surface\tplugin source index"),
                "classification" => "in-scope+product-exclusion",
                "capability_ids" => aggregate_capabilities,
                "operation_ids" => aggregate_operations,
                "classification_basis" => "mixed plugin aggregation export: exact in-scope plugin closure is retained while MCP and SIWE symbols remain explicitly excluded"
              )
            elsif package == "better-auth" && export_name.start_with?("./plugins/")
              plugin = export_name.delete_prefix("./plugins/").split("/", 2).first
              plugin_semantic = semantic_by_id.fetch("Source-exported and internal plugin surface\t#{plugin}")
              classification.call(
                "package/better-auth/plugin-export/#{plugin}",
                plugin_semantic,
                "classification_basis" => "exact exported backend plugin surface"
              )
            elsif package == "better-auth" && export_name == "./social-providers"
              classification.call(
                "package/better-auth/social-provider-export",
                semantic_by_id.fetch("Core documentation and route surface\tauthentication provider pages"),
                "classification_basis" => "built-in social-provider profile export closed by the provider matrix"
              )
            elsif package == "better-auth" && export_name == "."
              classification.call(
                "package/better-auth/server-factory-export",
                package_semantics.fetch(package),
                "classification_basis" => "top-level backend server factory and core backend contract aggregation; client, adapter, plugin, and framework exports are classified separately"
              )
            elsif package == "better-auth" && export_name.match?(%r{\A\./(?:client|react|solid|lynx|vue|svelte|next-js|svelte-kit|solid-start|tanstack-start|node)})
              classification.call(
                "package/better-auth/javascript-client-export",
                package_semantics.fetch(package),
                empty_override.call("client-surface", "JavaScript client or framework export")
              )
            elsif package == "better-auth" && export_name.start_with?("./adapters")
              classification.call(
                "package/better-auth/unselected-adapter-export",
                package_semantics.fetch(package),
                empty_override.call("deployment-profile-divergence", "JavaScript database adapter export outside the selected deployment profile")
              )
            elsif package == "better-auth" && export_name.match?(%r{\A\./db/(?:adapter|adapter/minimal|migration)\z})
              classification.call(
                "package/better-auth/unselected-database-tooling-export",
                package_semantics.fetch("kysely-adapter"),
                "classification_basis" => "JavaScript Kysely or mixed-dialect database tooling export is an unselected deployment profile; local direct Go PostgreSQL contracts are specified independently"
              )
            elsif package == "better-auth" && export_name == "./test"
              classification.call(
                "package/better-auth/test-utilities-export",
                package_semantics.fetch("test-utils"),
                "classification_basis" => "backend test utility export retained through identity/identitytest"
              )
            elsif package == "stripe"
              classification.call(
                "package/stripe/billing-export",
                package_semantics.fetch(package),
                "classification_basis" => "billing and payment integration export is excluded as a product"
              )
            elsif package == "oauth-provider" && export_name == "."
              classification.call(
                "package/oauth-provider/mixed-server-export",
                package_semantics.fetch(package),
                "classification" => "in-scope+product-exclusion",
                "capability_ids" => oauth_provider_server_capabilities,
                "operation_ids" => oauth_provider_server_capabilities.flat_map { |id| capability_operations.fetch(id) }.uniq.sort_by(&:b),
                "classification_basis" => "OAuth/OIDC provider export is retained, but the co-exported MCP handler remains explicitly excluded"
              )
            elsif package == "oauth-provider" && export_name == "./resource-client"
              classification.call(
                "package/oauth-provider/protected-resource-client-export",
                package_semantics.fetch(package),
                oauth_resource_override.call("standards-generic protected-resource metadata and access-token verification export")
              )
            elsif export_name == "./client" || target.match?(%r{(?:^|/)client\.ts\z})
              classification.call(
                "package/#{package}/javascript-client-export",
                package_semantics.fetch(package),
                empty_override.call("client-surface", "JavaScript client export; backend operations remain closed separately")
              )
            else
              package_leaf_classification.call(synthetic_path)
            end
    classification_rule_id = exact.delete("exact_disposition_id")

    {
      "exact_disposition_id" => "export/#{package}/#{export_kind}/#{export_name}",
      "classification_rule_id" => classification_rule_id,
      "package" => package,
      "export" => export_name,
      "kind" => export_kind,
      "target" => target,
      "manifest_path" => entry.fetch("path"),
      "manifest_object_id" => entry.fetch("object_id")
    }.merge(exact)
  end
end.sort_by { |record| [record.fetch("package").b, record.fetch("kind").b, record.fetch("export").b] }

(leaf_dispositions + export_dispositions).each do |record|
  next unless record.fetch("classification").split("+").include?("in-scope")

  abort "in-scope exact disposition has no operation closure: #{record.fetch('exact_disposition_id')}" if record.fetch("operation_ids").empty?
end
exact_disposition_ids = (leaf_dispositions + export_dispositions).map { |record| record.fetch("exact_disposition_id") }
abort "exact blob/export disposition IDs are not unique" unless exact_disposition_ids.uniq.length == exact_disposition_ids.length

package_summaries = package_semantics.keys.sort_by(&:b).map do |package|
  semantic = package_semantics.fetch(package)
  package_leaves = leaf_dispositions.select { |leaf| leaf.fetch("path").start_with?("packages/#{package}/") }
  package_exports = export_dispositions.select { |record| record.fetch("package") == package }
  exact_items = package_leaves + package_exports
  exact_lines = exact_items.map do |item|
    [
      item.fetch("exact_disposition_id"), item.fetch("classification_rule_id"),
      item.fetch("disposition_row_id"), item.fetch("classification"),
      item.fetch("capability_ids").join(","), item.fetch("operation_ids").join(",")
    ].join("\t")
  end.sort_by(&:b)
  {
    "package" => package,
    "disposition_row_id" => semantic.fetch("disposition_row_id"),
    "declared_classification" => semantic.fetch("classification"),
    "tree_object_id" => semantic.fetch("source_object_id"),
    "blob_count" => package_leaves.length,
    "export_count" => package_exports.length,
    "exact_dispositions_sha256" => Digest::SHA256.hexdigest(exact_lines.join("\n") + "\n"),
    "derived_classifications" => exact_items.map { |item| item.fetch("classification") }.uniq.sort_by(&:b),
    "capability_ids" => exact_items.flat_map { |item| item.fetch("capability_ids") }.uniq.sort_by(&:b),
    "operation_ids" => exact_items.flat_map { |item| item.fetch("operation_ids") }.uniq.sort_by(&:b)
  }
end

canonical_lines = source_records.flat_map do |source|
  source.fetch("entries").map do |entry|
    ["source", source.fetch("path"), source.fetch("enumeration"), entry.fetch("path"), entry.fetch("mode"), entry.fetch("kind"), entry.fetch("object_id")].join("\t")
  end
end + leaf_dispositions.map do |leaf|
  ["leaf", leaf.fetch("source_path"), leaf.fetch("path"), leaf.fetch("kind"), leaf.fetch("object_id"), leaf.fetch("exact_disposition_id"), leaf.fetch("classification_rule_id"), leaf.fetch("disposition_row_id"), leaf.fetch("classification"), leaf.fetch("capability_ids").join(","), leaf.fetch("operation_ids").join(",")].join("\t")
end + export_dispositions.map do |record|
  ["export", record.fetch("package"), record.fetch("kind"), record.fetch("export"), record.fetch("target"), record.fetch("manifest_object_id"), record.fetch("exact_disposition_id"), record.fetch("classification_rule_id"), record.fetch("disposition_row_id"), record.fetch("classification"), record.fetch("capability_ids").join(","), record.fetch("operation_ids").join(",")].join("\t")
end

non_provider_path_lines = leaf_dispositions.filter_map do |leaf|
  next if leaf.fetch("disposition_row_id").start_with?("Provider catalog disposition\t")

  [leaf.fetch("source_path"), leaf.fetch("path"), leaf.fetch("exact_disposition_id"), leaf.fetch("classification_rule_id"), leaf.fetch("disposition_row_id"), leaf.fetch("classification")].join("\t")
end

document = {
  "schema_version" => 2,
  "upstream" => surface.fetch("upstream"),
  "canonical_leaf_policy" => "one disposition per unique physical blob path; when declared sources overlap, select the source with the longest path, breaking equal lengths by bytewise source-path order",
  "package_export_policy" => "enumerate every key in exports and bin from every packages/*/package.json blob; bind each disposition to the declaring manifest blob object ID",
  "source_count" => source_records.length,
  "leaf_count" => leaf_dispositions.length,
  "export_count" => export_dispositions.length,
  "non_provider_path_classification_count" => non_provider_path_lines.length,
  "non_provider_path_classifications_sha256" => Digest::SHA256.hexdigest(non_provider_path_lines.join("\n") + "\n"),
  "sha256" => Digest::SHA256.hexdigest(canonical_lines.join("\n") + "\n"),
  "sources" => source_records,
  "leaf_dispositions" => leaf_dispositions,
  "export_dispositions" => export_dispositions,
  "package_summaries" => package_summaries
}
rendered = JSON.pretty_generate(document) + "\n"

if check
  abort "#{OUTPUT_PATH} is stale; regenerate from the pinned Better Auth repository" unless File.file?(OUTPUT_PATH) && File.binread(OUTPUT_PATH) == rendered
  puts "verified #{OUTPUT_PATH}: #{leaf_dispositions.length} exact blob and #{export_dispositions.length} export dispositions"
else
  File.write(OUTPUT_PATH, rendered)
  puts "generated #{OUTPUT_PATH}: #{leaf_dispositions.length} exact blob and #{export_dispositions.length} export dispositions"
end
