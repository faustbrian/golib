#!/usr/bin/env ruby
# frozen_string_literal: true

require "set"

ROOT = File.expand_path(__dir__)
EXPECTED_UNITS = 48
BASELINE = "b8077b74ef9a80a7757220b72834349bd8de05c0"
ALLOWED_WORKER_PLACEHOLDERS = Set[
  "unit", "canonical-module", "absolute-worktree-path", "worker-branch",
  "integration-commit", "absolute-goal-path", "verified-prerequisite-list",
  "canonical-module-directory"
].freeze
EXISTING_OWNERS = Set[
  "authentication", "authentication/jwt", "authorization", "capability"
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
    goal: cells[6][/`([^`]+)`/, 1]
  }
end
fail_check("expected #{EXPECTED_UNITS} inventory rows, found #{rows.length}") unless rows.length == EXPECTED_UNITS
units = rows.map { |row| row[:unit] }
fail_check("duplicate inventory unit") unless units.uniq.length == units.length
known = units.to_set

rows.each do |row|
  unknown = row[:requires].reject { |unit| known.include?(unit) }
  fail_check("#{row[:unit]} has unknown dependencies: #{unknown.join(', ')}") unless unknown.empty?
end

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

ready = rows.select { |row| row[:status] == "ready" }.map { |row| row[:unit] }.to_set
roots = rows.select { |row| row[:requires].empty? }.map { |row| row[:unit] }.to_set
fail_check("ready units do not exactly equal dependency-free roots") unless ready == roots

goal_files = Dir[File.join(ROOT, "goals", "*.md")]
fail_check("expected #{EXPECTED_UNITS} goal files, found #{goal_files.length}") unless goal_files.length == EXPECTED_UNITS
seen_goals = Set.new
reverse = Hash.new { |hash, key| hash[key] = [] }
rows.each { |row| row[:requires].each { |dependency| reverse[dependency] << row[:unit] } }

rows.each do |row|
  path = File.join(ROOT, row[:goal])
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
    fail_check("identity/http goal must delegate its explicit prerequisites to inventory") unless requires_line.include?("every preceding inventory unit")
  else
    actual_requires = requires_line.scan(/`([^`]+)`/).flatten
    fail_check("requires mismatch for #{row[:unit]}: expected #{row[:requires]}, got #{actual_requires}") unless actual_requires == row[:requires]
  end
  fail_check("#{row[:unit]} goal lacks common-requirements start gate") unless body.include?("COMMON_REQUIREMENTS.md")
  fail_check("#{row[:unit]} goal retains worker-owned ready transition") if body.match?(/marks `#{Regexp.escape(row[:unit])}` as `ready`/)
  expected_unlocks = reverse[row[:unit]]
  unlock_line = body[/^- Unlocks after verification:.*$/]
  actual_unlocks = unlock_line.to_s.scan(/`([^`]+)`/).flatten
  fail_check("unlock mismatch for #{row[:unit]}: expected #{expected_unlocks}, got #{actual_unlocks}") unless actual_unlocks == expected_unlocks
  seen_goals << File.realpath(path)
end
orphans = goal_files.map { |path| File.realpath(path) }.to_set - seen_goals
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
worker = File.read(File.join(ROOT, "WORKER_PROMPT.md"))
worker_placeholders = worker.scan(/<([^>]+)>/).flatten.to_set
unknown_placeholders = worker_placeholders - ALLOWED_WORKER_PLACEHOLDERS
missing_placeholders = ALLOWED_WORKER_PLACEHOLDERS - worker_placeholders
fail_check("worker placeholder mismatch; unknown=#{unknown_placeholders.to_a} missing=#{missing_placeholders.to_a}") unless unknown_placeholders.empty? && missing_placeholders.empty?

puts "identity-platform validation: #{rows.length} units, #{inventory_edges.length} edges, #{depth.values.max + 1} waves, parity baseline #{BASELINE}"
