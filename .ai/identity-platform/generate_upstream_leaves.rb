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

leaf_dispositions = leaves.map do |leaf|
  disposition_row_id = semantic_id_for.call(leaf.fetch("path"), leaf.fetch("source_path"))
  semantic = semantic_by_id.fetch(disposition_row_id)
  {
    "path" => leaf.fetch("path"),
    "kind" => leaf.fetch("kind"),
    "object_id" => leaf.fetch("object_id"),
    "source_path" => leaf.fetch("source_path"),
    "disposition_row_id" => disposition_row_id,
    "classification" => semantic.fetch("classification"),
    "capability_ids" => semantic.fetch("capability_ids"),
    "operation_ids" => semantic.fetch("operation_ids")
  }
end.sort_by { |leaf| [leaf.fetch("path").b, leaf.fetch("source_path").b] }

canonical_lines = source_records.flat_map do |source|
  source.fetch("entries").map do |entry|
    ["source", source.fetch("path"), source.fetch("enumeration"), entry.fetch("path"), entry.fetch("mode"), entry.fetch("kind"), entry.fetch("object_id")].join("\t")
  end
end + leaf_dispositions.map do |leaf|
  ["leaf", leaf.fetch("source_path"), leaf.fetch("path"), leaf.fetch("kind"), leaf.fetch("object_id"), leaf.fetch("disposition_row_id"), leaf.fetch("classification"), leaf.fetch("capability_ids").join(","), leaf.fetch("operation_ids").join(",")].join("\t")
end

non_provider_path_lines = leaf_dispositions.filter_map do |leaf|
  next if leaf.fetch("disposition_row_id").start_with?("Provider catalog disposition\t")

  [leaf.fetch("source_path"), leaf.fetch("path"), leaf.fetch("disposition_row_id"), leaf.fetch("classification")].join("\t")
end

document = {
  "schema_version" => 1,
  "upstream" => surface.fetch("upstream"),
  "source_count" => source_records.length,
  "leaf_count" => leaf_dispositions.length,
  "non_provider_path_classification_count" => non_provider_path_lines.length,
  "non_provider_path_classifications_sha256" => Digest::SHA256.hexdigest(non_provider_path_lines.join("\n") + "\n"),
  "sha256" => Digest::SHA256.hexdigest(canonical_lines.join("\n") + "\n"),
  "sources" => source_records,
  "leaf_dispositions" => leaf_dispositions
}
rendered = JSON.pretty_generate(document) + "\n"

if check
  abort "#{OUTPUT_PATH} is stale; regenerate from the pinned Better Auth repository" unless File.file?(OUTPUT_PATH) && File.binread(OUTPUT_PATH) == rendered
  puts "verified #{OUTPUT_PATH}: #{leaf_dispositions.length} exact leaf dispositions"
else
  File.write(OUTPUT_PATH, rendered)
  puts "generated #{OUTPUT_PATH}: #{leaf_dispositions.length} exact leaf dispositions"
end
