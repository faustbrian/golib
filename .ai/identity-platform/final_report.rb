#!/usr/bin/env ruby
# frozen_string_literal: true

require "digest"
require "json"
require "open3"
require "tempfile"

module IdentityPlatformFinalReport
  ROOT = File.expand_path("../..", __dir__)
  SCHEMA_PATH = File.join(__dir__, "FINAL_REPORT.schema.json")
  REPORT_PATH = File.join(__dir__, "evidence", "final-report.json")
  HEX = "^[0-9a-f]{64}$"
  REVISION = "^[0-9a-f]{40,64}$"
  STATUSES = %w[blocked implemented-unverified in-progress proposed ready verified].freeze
  FINAL_GATES = %w[
    affected-release-gates
    complete-repository-gate
    final-complete-diff-review
    inventory-validation
    pinned-upstream-validation
    program-final-input-acceptance
    structural-validation
  ].freeze
  FINAL_GATE_COMMANDS = {
    "affected-release-gates" => "make ci-changed",
    "complete-repository-gate" => "make ci",
    "final-complete-diff-review" => "ruby .ai/identity-platform/final_review_gate.rb <recorded-base>",
    "inventory-validation" => "make inventory",
    "pinned-upstream-validation" => "ruby .ai/identity-platform/generate_upstream_leaves.rb --check <pinned-better-auth-repository>",
    "program-final-input-acceptance" => "ruby .ai/identity-platform/validate.rb --execution --clean-integration",
    "structural-validation" => "ruby .ai/identity-platform/validate.rb"
  }.freeze
  COMPLETE_REQUIREMENTS = (1..9).map { |number| "program-complete.#{number}" }.freeze
  BLOCKED_REQUIREMENTS = (1..6).map { |number| "program-blocked.#{number}" }.freeze
  TERMINAL_REQUIREMENTS = (COMPLETE_REQUIREMENTS + BLOCKED_REQUIREMENTS).freeze
  INDEPENDENT_EXTERNAL_PROFILES = %w[
    hibp-pwned-passwords protocol-openid-conformance-suite protocol-simplesamlphp
    protocol-unboundid-scim2-sdk protocol-web-platform-tests
  ].freeze
  AUTHORIZATION_TABLES = {
    "Semantic authorization requests" => [7, 3],
    "User semantic authorizations" => [8, 6],
    "Goal digest revisions" => [7, 4],
    "Conflict-recovery baselines" => [12, 10],
    "Integrated-repair authorizations" => [15, 13]
  }.freeze

  class Error < StandardError; end

  class UniqueHash < Hash
    def []=(key, value)
      raise Error, "duplicate JSON key: #{key}" if key?(key)

      super
    end
  end

  module_function

  def parse_json(path)
    JSON.parse(File.read(path), object_class: UniqueHash, allow_duplicate_key: true)
  rescue JSON::ParserError => e
    raise Error, "#{path}: #{e.message}"
  end

  def source(name)
    parse_json(File.join(__dir__, name))
  end

  def markdown_rows(path, heading, columns)
    text = File.read(File.join(__dir__, path))
    body = text.split(/^## #{Regexp.escape(heading)}\s*$/, 2).fetch(1) do
      raise Error, "#{path}: missing #{heading} table"
    end
    rows = []
    body.each_line do |line|
      break if line.start_with?("## ")
      next unless line.start_with?("| `")

      cells = line.strip.sub(/^\|\s*/, "").sub(/\s*\|$/, "").split(/\s*\|\s*/, -1)
      next unless cells.length == columns

      rows << cells.map { |cell| cell.gsub(/\A`|`\z/, "") }
    end
    rows
  end

  def inventory
    markdown_rows("INVENTORY.md", "Coordinator checklist", 6) if false
    text = File.read(File.join(__dir__, "INVENTORY.md"))
    rows = text.lines.filter_map do |line|
      next unless line.start_with?("| `")
      cells = line.strip.sub(/^\|\s*/, "").sub(/\s*\|$/, "").split(/\s*\|\s*/, -1)
      next unless cells.length == 6

      {
        "unit" => cells[0].delete_prefix("`").delete_suffix("`"),
        "module" => cells[1].delete_prefix("`").delete_suffix("`"), "status" => cells[3],
        "blocker" => cells[4]
      }
    end
    raise Error, "INVENTORY.md: expected 67 unit rows, found #{rows.length}" unless rows.length == 67

    rows
  end

  def ledger
    rows = markdown_rows("EXECUTION_LEDGER.md", "Unit execution ledger", 12).map do |cells|
      {
        "unit" => cells[0], "generation" => Integer(cells[1]),
        "worker_task" => cells[2], "worker_commit" => cells[6],
        "integration_checkpoint" => cells[7], "gate_execution_revision" => cells[8],
        "gate_fingerprint" => cells[9], "external_evidence" => cells[10]
      }
    rescue ArgumentError
      raise Error, "EXECUTION_LEDGER.md: invalid generation for #{cells[0]}"
    end
    raise Error, "EXECUTION_LEDGER.md: expected 67 unit rows, found #{rows.length}" unless rows.length == 67

    rows.to_h { |row| [row.fetch("unit"), row] }
  end

  def catalogs
    acceptance = source("END_STATE_ACCEPTANCE.json")
    configuration = source("CONFIGURATION_CATALOGS.json")
    {
      units: source("GOAL_MANIFEST.json").fetch("goals").map { |row| row.fetch("unit") }.sort,
      parity: (better_auth_capabilities + source("PARITY_DISPOSITIONS.json").fetch("dispositions")).sort_by { |row| row.fetch("id") },
      journeys: acceptance.fetch("journeys").sort_by { |row| row.fetch("id") },
      cross_cutting: acceptance.fetch("cross_cutting").sort_by { |row| row.fetch("id") },
      artifacts: acceptance.fetch("artifact_catalog").sort_by { |row| row.fetch("id") },
      providers: configuration.fetch("provider_matrix").fetch("rows").map { |row| row.fetch("id") }.sort,
      captcha: configuration.fetch("captcha").fetch("ids").sort,
      independent_external_profiles: INDEPENDENT_EXTERNAL_PROFILES,
      deployments: source("PARITY_DISPOSITIONS.json").fetch("dispositions")
        .select { |row| row.fetch("kind") == "deployment-profile" }.sort_by { |row| row.fetch("id") },
      resources: preflight_rows("Task-owned resource registry", 8).map do |cells|
        { "resource_id" => cells[0], "type" => cells[1], "owner" => cells[2], "target" => cells[3],
          "state" => cells[4], "cleanup_trigger" => (cells[5] == "—" ? nil : cells[5]), "evidence" => cells[7] }
      end.sort_by { |row| row.fetch("resource_id") },
      authorization_boundaries: authorization_boundaries
    }
  end

  def upstream_baseline
    surface = source("UPSTREAM_SURFACE.json")
    surface.fetch("upstream").merge(
      "source_objects" => surface.fetch("sources").map do |row|
        row.slice("path", "kind", "object_id", "enumeration")
      end.sort_by { |row| row.fetch("path").b }
    )
  end

  def slug(value)
    value.downcase.gsub(/`/, "").gsub(/[^a-z0-9]+/, "-").gsub(/\A-|\z/, "")
  end

  def better_auth_capabilities
    File.readlines(File.join(__dir__, "BETTER_AUTH_PARITY.md")).filter_map do |line|
      next unless line.start_with?("|")
      cells = line.strip.sub(/^\|\s*/, "").sub(/\s*\|$/, "").split(/\s*\|\s*/, -1)
      next unless cells.length == 5 && cells[1] == "In"
      capability = cells[0]
      canonical = cells.join("|")
      { "id" => "capability.#{slug(capability)}.v1", "kind" => "in-scope-capability",
        "owner" => cells[2], "artifact" => "BETTER_AUTH_PARITY.md",
        "semantic_digest" => Digest::SHA256.hexdigest(canonical), "capability" => capability }
    end
  end

  def preflight_rows(heading, columns)
    markdown_rows("PREFLIGHT_EVIDENCE.md", heading, columns)
  end

  def authorization_boundaries
    AUTHORIZATION_TABLES.flat_map do |heading, (columns, status_index)|
      preflight_rows(heading, columns).map do |cells|
        authorization_boundary(heading, cells, status_index)
      end
    end.sort_by { |row| row.fetch("id") }
  end

  def authorization_boundary(heading, cells, status_index)
    { "id" => "#{slug(heading)}:#{cells.fetch(0)}", "kind" => slug(heading),
      "subject" => cells.fetch(0), "status" => cells.fetch(status_index) }
  end

  def parity_baseline_revision
    text = File.read(File.join(__dir__, "BETTER_AUTH_PARITY.md"))
    text[/The comparison baseline is Better Auth commit\s+\[`([0-9a-f]{40})`\]/m, 1] ||
      raise(Error, "BETTER_AUTH_PARITY.md: pinned comparison revision missing")
  end

  def closed_object(properties, required = properties.keys)
    { "type" => "object", "additionalProperties" => false, "required" => required, "properties" => properties }
  end

  def exact_array(rows, &block)
    schemas = rows.map(&block)
    { "type" => "array", "minItems" => rows.length, "maxItems" => rows.length, "prefixItems" => schemas, "items" => false }
  end

  def evidence_schema
    closed_object(
      "outcome" => { "enum" => %w[failed not-applicable passed unavailable] },
      "evidence" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                      "items" => { "type" => "string", "minLength" => 1 } }
    )
  end

  def schema
    c = catalogs
    upstream = upstream_baseline
    unit_rows = c.fetch(:units).map do |unit|
      closed_object(
        "unit" => { "const" => unit }, "status" => { "enum" => STATUSES },
        "generation" => { "type" => "integer", "minimum" => 0 },
        "worker_task" => { "type" => ["string", "null"], "minLength" => 1 },
        "blocker" => { "type" => ["string", "null"], "minLength" => 1 },
        "worker_commit" => { "type" => ["string", "null"], "pattern" => REVISION },
        "integration_checkpoint" => { "type" => ["string", "null"], "pattern" => REVISION },
        "gate_execution_revision" => { "type" => ["string", "null"], "pattern" => REVISION },
        "evidence_disposition" => { "enum" => %w[attributable blocked missing not-needed unavailable] },
        "evidence_bindings" => { "type" => "array", "uniqueItems" => true,
                                 "items" => { "type" => "string", "minLength" => 1 } }
      )
    end
    parity_rows = c.fetch(:parity).map do |row|
      properties = {
        "id" => { "const" => row.fetch("id") }, "kind" => { "const" => row.fetch("kind") },
        "owner" => { "const" => row.fetch("owner") }, "semantic_digest" => { "const" => row.fetch("semantic_digest") },
        "outcome" => { "enum" => %w[failed pinned-disposition-matched unavailable verified] },
        "evidence" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                        "items" => { "type" => "string", "minLength" => 1 } }
      }
      properties["capability"] = { "const" => row.fetch("capability") } if row["capability"]
      closed_object(properties)
    end
    claim_schema = lambda do |row|
      closed_object(
        "id" => { "const" => row.fetch("id") }, "owner" => { "const" => row.fetch("owner") },
        "semantic_digest" => { "const" => row.fetch("semantic_digest") },
        "artifacts" => { "const" => row.fetch("artifacts") },
        "outcome" => { "enum" => %w[failed passed unavailable] },
        "evidence_bindings" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                                 "items" => { "type" => "string", "minLength" => 1 } }
      )
    end
    artifact_rows = c.fetch(:artifacts).map do |row|
      closed_object(
        "id" => { "const" => row.fetch("id") }, "path" => { "const" => row.fetch("path") },
        "schema" => { "const" => row.fetch("schema") }, "claims" => { "const" => row.fetch("claims") },
        "outcome" => { "enum" => %w[failed passed unavailable] },
        "evidence_binding" => { "type" => ["string", "null"], "minLength" => 1 }
      )
    end
    boundary = lambda do |id, kind|
      closed_object(
        "id" => { "const" => id }, "kind" => { "const" => kind },
        "outcome" => { "enum" => %w[pinned-disposition-matched proven unproven] },
        "evidence" => { "type" => "array", "uniqueItems" => true,
                        "items" => { "type" => "string", "minLength" => 1 } },
        "blocker" => { "type" => ["string", "null"], "minLength" => 1 }
      )
    end
    gate_rows = FINAL_GATES.map do |id|
      closed_object(
        "id" => { "const" => id }, "outcome" => { "enum" => %w[failed passed unavailable] },
        "tested_revision" => { "type" => ["string", "null"], "pattern" => REVISION },
        "evidence" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                        "items" => { "type" => "string", "minLength" => 1 } }
      )
    end
    terminal_rows = TERMINAL_REQUIREMENTS.map do |id|
      closed_object(
        "id" => { "const" => id }, "outcome" => { "enum" => %w[failed not-applicable passed] },
        "evidence" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                        "items" => { "type" => "string", "minLength" => 1 } }
      )
    end

    closed_object(
      "$schema" => { "const" => "https://json-schema.org/draft/2020-12/schema" },
      "schema_version" => { "const" => 1 },
      "terminal_predicate" => { "enum" => %w[PROGRAM-BLOCKED PROGRAM-COMPLETE] },
      "evidence_records" => { "type" => "array", "items" => closed_object(
        "id" => { "type" => "string", "pattern" => "^[a-z0-9][a-z0-9._:-]*$" },
        "subject_id" => { "type" => "string", "minLength" => 1 },
        "command_or_profile" => { "type" => "string", "minLength" => 1 },
        "outcome" => { "const" => "passed" },
        "kind" => { "enum" => %w[acceptance-artifact external-evidence final-gate unit-gate] },
        "claim_ids" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                         "items" => { "type" => "string", "minLength" => 1 } },
        "record_path" => { "type" => "string", "pattern" => "^\\.ai/identity-platform/evidence/[A-Za-z0-9._/-]+\\.json$" },
        "record_commit" => { "type" => "string", "pattern" => REVISION },
        "record_sha256" => { "type" => "string", "pattern" => HEX },
        "receipt_path" => { "type" => "string", "pattern" => "^\\.ai/identity-platform/evidence/[A-Za-z0-9._/-]+\\.json$" },
        "receipt_commit" => { "type" => "string", "pattern" => REVISION },
        "receipt_sha256" => { "type" => "string", "pattern" => HEX },
        "tested_revision" => { "type" => "string", "pattern" => REVISION },
        "gate_execution_revision" => { "type" => "string", "pattern" => REVISION },
        "input_root" => { "type" => "string", "pattern" => "^sha256:[0-9a-f]{64}$" }
      ) },
      "integration" => closed_object(
        "branch" => { "type" => "string", "minLength" => 1 },
        "final_input_revision" => { "type" => "string", "pattern" => REVISION },
        "parity_baseline_revision" => { "type" => "string", "pattern" => REVISION }
      ),
      "push_boundary" => closed_object(
        "push_prohibited" => { "const" => true },
        "coordinator_assertion" => { "const" => "coordinator-asserts-no-push" },
        "assertion_verified" => { "const" => false },
        "observed_local_branch" => { "type" => "string", "minLength" => 1 },
        "observed_local_head" => { "type" => "string", "pattern" => REVISION },
        "configured_remotes" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                                   "items" => { "type" => "string", "minLength" => 1 } },
        "limitation" => { "const" => "no-complete-command-audit-or-remote-non-delivery-proof" }
      ),
      "upstream_baseline" => closed_object(
        "repository" => { "const" => upstream.fetch("repository") },
        "revision" => { "const" => upstream.fetch("revision") },
        "object_format" => { "const" => upstream.fetch("object_format") },
        "source_objects" => exact_array(upstream.fetch("source_objects")) do |row|
          closed_object(
            "path" => { "const" => row.fetch("path") }, "kind" => { "const" => row.fetch("kind") },
            "object_id" => { "const" => row.fetch("object_id") },
            "enumeration" => { "const" => row.fetch("enumeration") }
          )
        end
      ),
      "units" => { "type" => "array", "minItems" => 67, "maxItems" => 67,
                     "prefixItems" => unit_rows, "items" => false },
      "parity" => { "type" => "array", "minItems" => parity_rows.length, "maxItems" => parity_rows.length,
                      "prefixItems" => parity_rows, "items" => false },
      "journeys" => { "type" => "array", "minItems" => c.fetch(:journeys).length,
                        "maxItems" => c.fetch(:journeys).length,
                        "prefixItems" => c.fetch(:journeys).map(&claim_schema), "items" => false },
      "cross_cutting" => { "type" => "array", "minItems" => c.fetch(:cross_cutting).length,
                            "maxItems" => c.fetch(:cross_cutting).length,
                            "prefixItems" => c.fetch(:cross_cutting).map(&claim_schema), "items" => false },
      "artifacts" => { "type" => "array", "minItems" => artifact_rows.length, "maxItems" => artifact_rows.length,
                         "prefixItems" => artifact_rows, "items" => false },
      "final_gates" => { "type" => "array", "minItems" => gate_rows.length, "maxItems" => gate_rows.length,
                           "prefixItems" => gate_rows, "items" => false },
      "provider_boundaries" => {
        "type" => "array",
        "maxItems" => c.fetch(:providers).length + c.fetch(:captcha).length + c.fetch(:independent_external_profiles).length,
        "minItems" => c.fetch(:providers).length + c.fetch(:captcha).length + c.fetch(:independent_external_profiles).length,
        "prefixItems" => (c.fetch(:providers).map { |id| boundary.call(id, "oauth-provider") } +
                          c.fetch(:captcha).map { |id| boundary.call(id, "captcha-provider") } +
                          c.fetch(:independent_external_profiles).map { |id| boundary.call(id, "external-profile") }), "items" => false
      },
      "deployment_boundaries" => {
        "type" => "array", "minItems" => c.fetch(:deployments).length, "maxItems" => c.fetch(:deployments).length,
        "prefixItems" => c.fetch(:deployments).map { |row| boundary.call(row.fetch("id"), "deployment-profile") },
        "items" => false
      },
      "cleanup" => closed_object(
        "resources_reconciled" => { "type" => "boolean" },
        "integration_worktree_removed" => { "type" => "boolean" },
        "entries" => exact_array(c.fetch(:resources)) do |resource|
          closed_object(
            "resource_id" => { "const" => resource.fetch("resource_id") },
            "type" => { "const" => resource.fetch("type") }, "owner" => { "const" => resource.fetch("owner") },
            "target" => { "const" => resource.fetch("target") }, "state" => { "const" => resource.fetch("state") },
            "cleanup_trigger" => { "const" => resource.fetch("cleanup_trigger") },
            "evidence" => { "const" => resource.fetch("evidence") }
          )
        end
      ),
      "authorizations" => closed_object(
        "unauthorized_semantic_changes" => { "type" => "integer", "minimum" => 0 },
        "authorization_history_valid" => { "type" => "boolean" },
        "transition_history_valid" => { "type" => "boolean" },
        "assignment_topology_valid" => { "type" => "boolean" },
        "evidence" => { "type" => "array", "minItems" => 1, "uniqueItems" => true,
                        "items" => { "type" => "string", "minLength" => 1 } },
        "boundaries" => exact_array(c.fetch(:authorization_boundaries)) do |boundary_row|
          closed_object(
            "id" => { "const" => boundary_row.fetch("id") }, "kind" => { "const" => boundary_row.fetch("kind") },
            "subject" => { "const" => boundary_row.fetch("subject") }, "status" => { "const" => boundary_row.fetch("status") },
            "evidence" => { "type" => "string", "minLength" => 1 }
          )
        end
      ),
      "terminal_requirements" => { "type" => "array", "minItems" => TERMINAL_REQUIREMENTS.length, "maxItems" => TERMINAL_REQUIREMENTS.length,
                                     "prefixItems" => terminal_rows, "items" => false },
      "blockers" => { "type" => "array", "items" => closed_object(
        "id" => { "type" => "string", "minLength" => 1 },
        "category" => { "enum" => %w[credential external-infrastructure product-decision user-authority] },
        "unit" => { "type" => ["string", "null"], "enum" => c.fetch(:units) + [nil] },
        "evidence" => { "type" => "string", "minLength" => 1 },
        "required_user_action" => { "type" => "string", "minLength" => 1 }
      ) }
    ).merge(
      "$id" => "https://github.com/faustbrian/golib/identity-platform/final-report.v1.schema.json",
      "title" => "Identity Platform Terminal Final Report",
      "allOf" => [
        {
          "if" => { "properties" => { "terminal_predicate" => { "const" => "PROGRAM-COMPLETE" } },
                    "required" => ["terminal_predicate"] },
          "then" => {
            "properties" => {
              "units" => { "items" => { "properties" => { "status" => { "const" => "verified" },
                                                           "evidence_disposition" => { "const" => "attributable" } } } },
              "parity" => { "items" => { "properties" => { "outcome" => { "enum" => %w[pinned-disposition-matched verified] } } } },
              "journeys" => { "items" => { "properties" => { "outcome" => { "const" => "passed" } } } },
              "cross_cutting" => { "items" => { "properties" => { "outcome" => { "const" => "passed" } } } },
              "artifacts" => { "items" => { "properties" => { "outcome" => { "const" => "passed" },
                                                               "evidence_binding" => { "type" => "string", "minLength" => 1 } } } },
              "final_gates" => { "items" => { "properties" => { "outcome" => { "const" => "passed" },
                                                                 "tested_revision" => { "type" => "string", "pattern" => REVISION } } } },
              "provider_boundaries" => { "items" => { "properties" => { "outcome" => { "const" => "proven" },
                                                                          "blocker" => { "type" => "null" } } } },
              "deployment_boundaries" => { "items" => { "properties" => { "outcome" => { "enum" => %w[pinned-disposition-matched proven] },
                                                                            "blocker" => { "type" => "null" } } } },
              "cleanup" => { "properties" => { "resources_reconciled" => { "const" => true },
                                                  "integration_worktree_removed" => { "const" => false } } },
              "authorizations" => { "properties" => { "unauthorized_semantic_changes" => { "const" => 0 },
                                                        "authorization_history_valid" => { "const" => true },
                                                        "transition_history_valid" => { "const" => true },
                                                        "assignment_topology_valid" => { "const" => true } } },
              "blockers" => { "maxItems" => 0 }
            }
          }
        },
        {
          "if" => { "properties" => { "terminal_predicate" => { "const" => "PROGRAM-BLOCKED" } },
                    "required" => ["terminal_predicate"] },
          "then" => { "properties" => { "blockers" => { "minItems" => 1 } } }
        }
      ]
    )
  end

  def canonical_schema
    JSON.pretty_generate(schema) + "\n"
  end

  def authoritative_schema
    expected = canonical_schema
    assert(File.file?(SCHEMA_PATH), "FINAL_REPORT.schema.json is missing; run --write-schema")
    bytes = File.binread(SCHEMA_PATH)
    assert(bytes == expected, "FINAL_REPORT.schema.json drifted; run --write-schema")
    parse_json_bytes(bytes, SCHEMA_PATH)
  end
  private_class_method :authoritative_schema

  def assert(condition, message)
    raise Error, message unless condition
  end

  def schema_valid?(value, contract)
    validate_schema(value, contract, "$", raise_errors: false)
  end

  def validate_schema(value, contract, path = "$", raise_errors: true)
    fail_schema = lambda do |message|
      raise Error, "#{path}: #{message}" if raise_errors
      return false
    end
    if contract.key?("const") && value != contract["const"]
      return fail_schema.call("does not equal const")
    end
    if contract.key?("enum") && !contract["enum"].include?(value)
      return fail_schema.call("is not in enum")
    end
    types = Array(contract["type"]).compact
    unless types.empty?
      matched = types.any? do |type|
        { "object" => Hash, "array" => Array, "string" => String,
          "integer" => Integer, "number" => Numeric, "boolean" => [TrueClass, FalseClass],
          "null" => NilClass }.fetch(type).then { |klass| Array(klass).any? { |candidate| value.is_a?(candidate) } }
      end
      return fail_schema.call("has wrong type") unless matched
    end
    if value.is_a?(String)
      return fail_schema.call("is too short") if contract["minLength"] && value.length < contract["minLength"]
      return fail_schema.call("does not match pattern") if contract["pattern"] && !Regexp.new(contract["pattern"]).match?(value)
    elsif value.is_a?(Numeric)
      return fail_schema.call("is below minimum") if contract["minimum"] && value < contract["minimum"]
    elsif value.is_a?(Hash)
      required = contract.fetch("required", [])
      missing = required - value.keys
      return fail_schema.call("missing fields #{missing.join(",")}") unless missing.empty?
      properties = contract.fetch("properties", {})
      extras = value.keys - properties.keys
      return fail_schema.call("extra fields #{extras.join(",")}") if contract["additionalProperties"] == false && !extras.empty?
      properties.each do |key, child|
        next unless value.key?(key)
        return false unless validate_schema(value[key], child, "#{path}.#{key}", raise_errors: raise_errors)
      end
    elsif value.is_a?(Array)
      return fail_schema.call("has too few items") if contract["minItems"] && value.length < contract["minItems"]
      return fail_schema.call("has too many items") if contract["maxItems"] && value.length > contract["maxItems"]
      return fail_schema.call("contains duplicates") if contract["uniqueItems"] && value.uniq.length != value.length
      prefix = contract.fetch("prefixItems", [])
      prefix.each_with_index do |child, index|
        next unless index < value.length
        return false unless validate_schema(value[index], child, "#{path}[#{index}]", raise_errors: raise_errors)
      end
      item_contract = contract["items"]
      if item_contract == false && value.length > prefix.length
        return fail_schema.call("contains additional items")
      elsif item_contract.is_a?(Hash)
        value.each_with_index do |item, index|
          return false unless validate_schema(item, item_contract, "#{path}[#{index}]", raise_errors: raise_errors)
        end
      end
    end
    contract.fetch("allOf", []).each do |child|
      return false unless validate_schema(value, child, path, raise_errors: raise_errors)
    end
    if contract["if"] && schema_valid?(value, contract["if"])
      return false unless validate_schema(value, contract.fetch("then", {}), path, raise_errors: raise_errors)
    end
    true
  end
  private_class_method :schema_valid?, :validate_schema

  def assert_exact_rows(actual, expected, label)
    assert(actual.is_a?(Array), "#{label} must be an array")
    ids = actual.map { |row| row.is_a?(Hash) ? row["id"] || row["unit"] : nil }
    assert(ids == expected, "#{label} must contain the exact sorted canonical rows")
  end

  def unit_evidence(execution, status, unit)
    bindings = []
    gate_revision = execution.fetch("gate_execution_revision")
    gate_fingerprint = execution.fetch("gate_fingerprint")
    if gate_revision != "—" || gate_fingerprint != "—"
      assert(gate_revision != "—" && gate_fingerprint != "—", "partial unit gate evidence in EXECUTION_LEDGER.md")
      bindings << "unit-gate:#{unit.tr("/", "-")}"
    end
    external = execution.fetch("external_evidence")
    bindings << "external-evidence:#{unit.tr("/", "-")}" if external.include?("@")
    disposition = if status == "verified"
                    "attributable"
                  elsif status == "blocked"
                    "blocked"
                  elsif external.start_with?("unavailable:")
                    "unavailable"
                  elsif bindings.empty?
                    "missing"
                  else
                    "attributable"
                  end
    [disposition, bindings.sort]
  end

  # These assertions validate only the report's declared terminal shape. The
  # authoritative terminal predicate and evidence projection are derived and
  # compared inside validate.rb; this helper never authenticates caller facts.
  def assert_program_complete(report)
    integration = report.fetch("integration")
    cleanup = report.fetch("cleanup")
    auth = report.fetch("authorizations")
    assert(integration.fetch("parity_baseline_revision") == parity_baseline_revision, "PROGRAM-COMPLETE parity baseline drifted")
    assert(report.fetch("units").all? { |row| row.fetch("status") == "verified" && row.fetch("evidence_disposition") == "attributable" && !row.fetch("evidence_bindings").empty? }, "PROGRAM-COMPLETE requires 67 verified attributable units")
    assert(report.fetch("parity").all? { |row| %w[pinned-disposition-matched verified].include?(row.fetch("outcome")) && !row.fetch("evidence").empty? }, "PROGRAM-COMPLETE requires exact successful parity dispositions")
    %w[journeys cross_cutting].each { |key| assert(report.fetch(key).all? { |row| row.fetch("outcome") == "passed" && !row.fetch("evidence_bindings").empty? }, "PROGRAM-COMPLETE requires every #{key} claim to pass") }
    assert(report.fetch("artifacts").all? { |row| row.fetch("outcome") == "passed" && row.fetch("evidence_binding").is_a?(String) && !row.fetch("evidence_binding").empty? }, "PROGRAM-COMPLETE requires every artifact binding")
    assert(report.fetch("final_gates").all? { |row| row.fetch("outcome") == "passed" && row.fetch("tested_revision") == integration.fetch("final_input_revision") && !row.fetch("evidence").empty? }, "PROGRAM-COMPLETE requires every final gate at final_input_revision")
    assert(report.fetch("provider_boundaries").all? { |row| row.fetch("outcome") == "proven" && row.fetch("blocker").nil? && !row.fetch("evidence").empty? }, "PROGRAM-COMPLETE has an unproven provider boundary")
    assert(report.fetch("deployment_boundaries").all? { |row| %w[pinned-disposition-matched proven].include?(row.fetch("outcome")) && row.fetch("blocker").nil? && !row.fetch("evidence").empty? }, "PROGRAM-COMPLETE has an unresolved deployment boundary")
    entries = cleanup.fetch("entries")
    pending = entries.select { |row| row.fetch("state") == "removal-pending-after-final-commit" }
    assert(cleanup.fetch("resources_reconciled") == true && cleanup.fetch("integration_worktree_removed") == false,
      "PROGRAM-COMPLETE requires the integration worktree retained for post-report handoff")
    assert(pending.length == 1 && pending.first.fetch("type") == "worktree" &&
      pending.first.fetch("owner") == "coordinator" &&
      pending.first.fetch("cleanup_trigger") == "user-authorized-post-report-removal" &&
      !pending.first.fetch("evidence").to_s.empty?,
      "PROGRAM-COMPLETE requires one exact pending integration-worktree handoff")
    assert((entries - pending).all? { |row| row.fetch("state") == "removed" },
      "PROGRAM-COMPLETE retains a non-integration task-owned resource")
    assert(auth.fetch("unauthorized_semantic_changes") == 0 && auth.values_at("authorization_history_valid", "transition_history_valid", "assignment_topology_valid").all?(true), "PROGRAM-COMPLETE requires valid authorization and topology histories")
    terminal = report.fetch("terminal_requirements").to_h { |row| [row.fetch("id"), row] }
    assert(COMPLETE_REQUIREMENTS.all? { |id| terminal.fetch(id).fetch("outcome") == "passed" && !terminal.fetch(id).fetch("evidence").empty? }, "PROGRAM-COMPLETE requires all nine predicate clauses")
    assert(BLOCKED_REQUIREMENTS.all? { |id| terminal.fetch(id).fetch("outcome") == "not-applicable" }, "PROGRAM-COMPLETE requires blocked clauses to be not-applicable")
    assert(report.fetch("blockers").empty?, "PROGRAM-COMPLETE cannot contain blockers")
  end

  def assert_program_blocked(report)
    blockers = report.fetch("blockers")
    assert(!blockers.empty?, "PROGRAM-BLOCKED requires at least one precise blocker")
    terminal_rows = report.fetch("terminal_requirements").to_h { |row| [row.fetch("id"), row] }
    assert(BLOCKED_REQUIREMENTS.all? { |id| terminal_rows.fetch(id).fetch("outcome") == "passed" && !terminal_rows.fetch(id).fetch("evidence").empty? }, "PROGRAM-BLOCKED requires all six blocked predicate clauses")
    assert(COMPLETE_REQUIREMENTS.any? { |id| terminal_rows.fetch(id).fetch("outcome") == "failed" }, "PROGRAM-BLOCKED requires PROGRAM-COMPLETE to be false")
    unfinished = report.fetch("units").reject { |row| row.fetch("status") == "verified" }
    assert(unfinished.all? { |row| row.fetch("status") == "blocked" }, "PROGRAM-BLOCKED requires every unfinished unit to be durably blocked")
    blocker_units = blockers.filter_map { |row| row.fetch("unit") }.uniq.sort
    assert(blocker_units == unfinished.map { |row| row.fetch("unit") }.sort, "PROGRAM-BLOCKED blocker set does not equal unfinished unit set")
    assert(report.dig("cleanup", "resources_reconciled") == true, "PROGRAM-BLOCKED requires resource reconciliation")
    assert(report.dig("cleanup", "entries").all? { |row| row.fetch("state") == "removed" || (row.fetch("state") == "retained-for-recovery" && row.fetch("cleanup_trigger").is_a?(String) && !row.fetch("cleanup_trigger").empty?) }, "PROGRAM-BLOCKED has an unreconciled resource")
    blockers.each do |row|
      assert(%w[credential external-infrastructure product-decision user-authority].include?(row.fetch("category")), "invalid blocker category")
      assert(row.fetch("required_user_action").is_a?(String) && !row.fetch("required_user_action").empty?, "blocker lacks precise user action")
    end
  end
  private_class_method :assert_program_complete, :assert_program_blocked

  def git(*args)
    stdout, stderr, status = Open3.capture3("git", *args, chdir: ROOT)
    raise Error, "git #{args.join(" ")} failed: #{stderr.strip}" unless status.success?

    stdout
  end

  def recursive_value?(value, target)
    return true if value == target
    return value.any? { |key, child| key == target || recursive_value?(child, target) } if value.is_a?(Hash)
    return value.any? { |child| recursive_value?(child, target) } if value.is_a?(Array)

    false
  end

  def verify_repository_state(report, expected: {})
    integration = report.fetch("integration")
    head = git("rev-parse", "HEAD").strip
    branch = git("branch", "--show-current").strip
    clean = git("status", "--porcelain").empty?
    assert(expected.fetch(:head, head) == head, "validator-derived HEAD mismatch")
    assert(expected.fetch(:branch, branch) == branch, "validator-derived branch mismatch")
    assert(expected.fetch(:clean, clean) == clean, "validator-derived clean-state mismatch")
    verify_repository_facts!(report, head: head, branch: branch, clean: clean)
    final_input_revision = integration.fetch("final_input_revision")
    git("cat-file", "-e", "#{final_input_revision}^{commit}")
    git("merge-base", "--is-ancestor", final_input_revision, head)
    changed_after_input = git("diff", "--name-only", "#{final_input_revision}..#{head}").lines.map(&:strip).reject(&:empty?)
    assert(post_input_paths_valid?(changed_after_input), "behavior-affecting path changed after final_input_revision")
    assert(integration.fetch("parity_baseline_revision") == parity_baseline_revision, "parity baseline revision drifted")
    if report.fetch("terminal_predicate") == "PROGRAM-COMPLETE"
      report.fetch("evidence_records").each do |row|
        assert(row.fetch("gate_execution_revision") == final_input_revision, "#{row.fetch("id")}: evidence is not bound to final_input_revision")
      end
    end
  end

  def post_input_paths_valid?(paths)
    permitted = %w[
      .ai/identity-platform/EXECUTION_LEDGER.md
      .ai/identity-platform/INVENTORY.md
      .ai/identity-platform/PREFLIGHT_EVIDENCE.md
      .ai/identity-platform/evidence/
    ]
    paths.all? { |path| permitted.any? { |allowed| path == allowed || (allowed.end_with?("/") && path.start_with?(allowed)) } }
  end

  def verify_repository_facts!(report, head:, branch:, clean:)
    integration = report.fetch("integration")
    assert(branch == integration.fetch("branch"), "integration branch does not equal validator-derived branch")
    assert(clean == true, "validator-derived worktree state is dirty")
    true
  end
  private_class_method :verify_repository_state, :verify_repository_facts!

  def parse_json_bytes(bytes, label)
    JSON.parse(bytes, object_class: UniqueHash, allow_duplicate_key: true)
  rescue JSON::ParserError => e
    raise Error, "#{label}: #{e.message}"
  end

  def validate_report(report)
    validate_report_shape(report, validate_terminal_shape: true, contract: authoritative_schema)
  end

  def validate_report_shape(report, validate_terminal_shape:, contract:)
    c = catalogs
    required = %w[$schema schema_version terminal_predicate integration push_boundary upstream_baseline evidence_records units parity journeys cross_cutting artifacts final_gates provider_boundaries deployment_boundaries cleanup authorizations terminal_requirements blockers]
    assert(report.is_a?(Hash) && report.keys.sort == required.sort, "report has missing or extra top-level fields")
    validate_schema(report, contract)
    assert(report["$schema"] == "https://json-schema.org/draft/2020-12/schema", "invalid $schema")
    assert(report["schema_version"] == 1, "invalid schema_version")
    reject_newline_strings!(report)
    terminal = report["terminal_predicate"]
    assert(%w[PROGRAM-BLOCKED PROGRAM-COMPLETE].include?(terminal), "invalid terminal predicate")

    assert_exact_rows(report["units"], c.fetch(:units), "units")
    assert(report.fetch("upstream_baseline") == upstream_baseline, "upstream baseline repository, revision, or source objects drifted")
    inventory_by_unit = inventory.to_h { |row| [row.fetch("unit"), row] }
    ledger_by_unit = ledger
    report.fetch("units").each do |row|
      unit = row.fetch("unit")
      current = inventory_by_unit.fetch(unit)
      execution = ledger_by_unit.fetch(unit)
      assert(row.fetch("status") == current.fetch("status"), "#{unit}: status does not match INVENTORY.md")
      assert(row.fetch("generation") == execution.fetch("generation"), "#{unit}: generation does not match EXECUTION_LEDGER.md")
      %w[worker_task worker_commit integration_checkpoint gate_execution_revision].each do |field|
        expected = execution.fetch(field) == "—" ? nil : execution.fetch(field)
        assert(row.fetch(field) == expected, "#{unit}: #{field} does not match EXECUTION_LEDGER.md")
      end
      blocker = current.fetch("blocker") == "—" ? nil : current.fetch("blocker")
      assert(row.fetch("blocker") == blocker, "#{unit}: blocker does not match INVENTORY.md")
      assert(STATUSES.include?(row.fetch("status")), "#{unit}: invalid status")
      disposition, bindings = unit_evidence(execution, current.fetch("status"), unit)
      assert(row.fetch("evidence_disposition") == disposition, "#{unit}: evidence disposition does not match EXECUTION_LEDGER.md")
      assert(row.fetch("evidence_bindings") == bindings, "#{unit}: evidence bindings do not match EXECUTION_LEDGER.md")
    end

    assert_exact_rows(report["parity"], c.fetch(:parity).map { |row| row.fetch("id") }, "parity")
    report.fetch("parity").zip(c.fetch(:parity)).each do |actual, expected|
      %w[id kind owner semantic_digest].each { |field| assert(actual.fetch(field) == expected.fetch(field), "#{expected.fetch("id")}: stale parity #{field}") }
      assert(actual["capability"] == expected["capability"], "#{expected.fetch("id")}: stale parity capability") if expected["capability"]
    end
    { "journeys" => :journeys, "cross_cutting" => :cross_cutting }.each do |label, key|
      assert_exact_rows(report[label], c.fetch(key).map { |row| row.fetch("id") }, label)
      report.fetch(label).zip(c.fetch(key)).each do |actual, expected|
        %w[id owner semantic_digest artifacts].each { |field| assert(actual.fetch(field) == expected.fetch(field), "#{expected.fetch("id")}: stale #{label} #{field}") }
      end
    end
    assert_exact_rows(report["artifacts"], c.fetch(:artifacts).map { |row| row.fetch("id") }, "artifacts")
    report.fetch("artifacts").zip(c.fetch(:artifacts)).each do |actual, expected|
      { "id" => "id", "path" => "path", "schema" => "schema", "claims" => "claims" }.each do |actual_key, expected_key|
        assert(actual.fetch(actual_key) == expected.fetch(expected_key), "#{expected.fetch("id")}: stale artifact #{actual_key}")
      end
    end
    assert_exact_rows(report["final_gates"], FINAL_GATES, "final_gates")
    provider_ids = c.fetch(:providers) + c.fetch(:captcha) + c.fetch(:independent_external_profiles)
    assert_exact_rows(report["provider_boundaries"], provider_ids, "provider_boundaries")
    assert(report.fetch("provider_boundaries").map { |row| row.fetch("kind") } ==
      Array.new(c.fetch(:providers).length, "oauth-provider") + Array.new(c.fetch(:captcha).length, "captcha-provider") + Array.new(c.fetch(:independent_external_profiles).length, "external-profile"),
      "provider boundary kinds are not canonical")
    assert_exact_rows(report["deployment_boundaries"], c.fetch(:deployments).map { |row| row.fetch("id") }, "deployment_boundaries")
    assert_exact_rows(report["terminal_requirements"], TERMINAL_REQUIREMENTS, "terminal_requirements")
    assert(report.dig("cleanup", "entries").map { |row| row.fetch("resource_id") } == c.fetch(:resources).map { |row| row.fetch("resource_id") },
      "cleanup entries do not close the resource registry")
    assert(report.dig("authorizations", "boundaries").map { |row| row.fetch("id") } == c.fetch(:authorization_boundaries).map { |row| row.fetch("id") },
      "authorization boundaries do not close preflight history")
    blockers = report.fetch("blockers")
    assert(blockers.is_a?(Array), "blockers must be an array")
    if validate_terminal_shape
      terminal == "PROGRAM-COMPLETE" ? assert_program_complete(report) : assert_program_blocked(report)
    end
    true
  rescue KeyError => e
    raise Error, "missing report field: #{e.key}"
  end
  private_class_method :validate_report_shape

  def reject_newline_strings!(value, path = "$")
    case value
    when Hash
      value.each { |key, child| reject_newline_strings!(child, "#{path}.#{key}") }
    when Array
      value.each_with_index { |child, index| reject_newline_strings!(child, "#{path}[#{index}]") }
    when String
      raise Error, "#{path}: embedded CR/LF is forbidden" if value.include?("\n") || value.include?("\r")
    end
  end

  def blocked_fixture
    c = catalogs
    inv = inventory.to_h { |row| [row.fetch("unit"), row] }
    led = ledger
    evidence = ["fixture:evidence"]
    {
      "$schema" => "https://json-schema.org/draft/2020-12/schema", "schema_version" => 1,
      "terminal_predicate" => "PROGRAM-BLOCKED",
      "evidence_records" => [],
      "integration" => { "branch" => "feature/identity-platform", "final_input_revision" => "0" * 40,
                         "parity_baseline_revision" => parity_baseline_revision },
      "push_boundary" => {
        "push_prohibited" => true, "coordinator_assertion" => "coordinator-asserts-no-push",
        "assertion_verified" => false, "observed_local_branch" => "feature/identity-platform",
        "observed_local_head" => "0" * 40, "configured_remotes" => ["origin"],
        "limitation" => "no-complete-command-audit-or-remote-non-delivery-proof"
      },
      "upstream_baseline" => upstream_baseline,
      "units" => c.fetch(:units).map do |unit|
        i = inv.fetch(unit); l = led.fetch(unit)
        { "unit" => unit, "status" => i.fetch("status"), "generation" => l.fetch("generation"),
          "worker_task" => l.fetch("worker_task") == "—" ? nil : l.fetch("worker_task"),
          "blocker" => i.fetch("blocker") == "—" ? nil : i.fetch("blocker"),
          "worker_commit" => l.fetch("worker_commit") == "—" ? nil : l.fetch("worker_commit"),
          "integration_checkpoint" => l.fetch("integration_checkpoint") == "—" ? nil : l.fetch("integration_checkpoint"),
          "gate_execution_revision" => l.fetch("gate_execution_revision") == "—" ? nil : l.fetch("gate_execution_revision"),
          "evidence_disposition" => unit_evidence(l, i.fetch("status"), unit).first,
          "evidence_bindings" => unit_evidence(l, i.fetch("status"), unit).last }
      end,
      "parity" => c.fetch(:parity).map { |row| row.slice("id", "kind", "owner", "semantic_digest", "capability").merge("outcome" => "unavailable", "evidence" => evidence) },
      "journeys" => c.fetch(:journeys).map { |row| row.slice("id", "owner", "semantic_digest", "artifacts").merge("outcome" => "unavailable", "evidence_bindings" => evidence) },
      "cross_cutting" => c.fetch(:cross_cutting).map { |row| row.slice("id", "owner", "semantic_digest", "artifacts").merge("outcome" => "unavailable", "evidence_bindings" => evidence) },
      "artifacts" => c.fetch(:artifacts).map { |row| row.slice("id", "path", "schema", "claims").merge("outcome" => "unavailable", "evidence_binding" => nil) },
      "final_gates" => FINAL_GATES.map { |id| { "id" => id, "outcome" => "unavailable", "tested_revision" => nil, "evidence" => evidence } },
      "provider_boundaries" => c.fetch(:providers).map { |id| { "id" => id, "kind" => "oauth-provider", "outcome" => "unproven", "evidence" => [], "blocker" => "not-run" } } +
        c.fetch(:captcha).map { |id| { "id" => id, "kind" => "captcha-provider", "outcome" => "unproven", "evidence" => [], "blocker" => "not-run" } } +
        c.fetch(:independent_external_profiles).map { |id| { "id" => id, "kind" => "external-profile", "outcome" => "unproven", "evidence" => [], "blocker" => "not-run" } },
      "deployment_boundaries" => c.fetch(:deployments).map { |row| { "id" => row.fetch("id"), "kind" => "deployment-profile", "outcome" => "pinned-disposition-matched", "evidence" => evidence, "blocker" => nil } },
      "cleanup" => { "resources_reconciled" => true, "integration_worktree_removed" => false,
                       "entries" => c.fetch(:resources).map(&:dup) },
      "authorizations" => { "unauthorized_semantic_changes" => 0, "authorization_history_valid" => true,
                              "transition_history_valid" => true, "assignment_topology_valid" => true, "evidence" => evidence,
                              "boundaries" => c.fetch(:authorization_boundaries).map { |row| row.merge("evidence" => "fixture:evidence") } },
      "terminal_requirements" => TERMINAL_REQUIREMENTS.map do |id|
        { "id" => id, "outcome" => BLOCKED_REQUIREMENTS.include?(id) ? "passed" : "failed", "evidence" => evidence }
      end,
      "blockers" => [{ "id" => "fixture-blocker", "category" => "user-authority", "unit" => nil,
                       "evidence" => "fixture:evidence", "required_user_action" => "Provide fixture authority" }]
    }
  end

  def deep_copy(value)
    JSON.parse(JSON.generate(value))
  end

  def completed_fixture
    report = blocked_fixture
    report["terminal_predicate"] = "PROGRAM-COMPLETE"
    report["units"].each do |row|
      row["status"] = "verified"
      row["evidence_disposition"] = "attributable"
      row["evidence_bindings"] = ["fixture:unit-evidence"]
    end
    report["parity"].each { |row| row["outcome"] = "pinned-disposition-matched" }
    %w[journeys cross_cutting].each { |key| report[key].each { |row| row["outcome"] = "passed" } }
    report["artifacts"].each { |row| row["outcome"] = "passed"; row["evidence_binding"] = "fixture:artifact-evidence" }
    report["final_gates"].each { |row| row["outcome"] = "passed"; row["tested_revision"] = report.dig("integration", "final_input_revision") }
    report["provider_boundaries"].each { |row| row["outcome"] = "proven"; row["evidence"] = ["fixture:provider-evidence"]; row["blocker"] = nil }
    report["cleanup"] = {
      "resources_reconciled" => true, "integration_worktree_removed" => false,
      "entries" => [{
        "resource_id" => "integration-worktree", "type" => "worktree", "owner" => "coordinator",
        "target" => "/tmp/identity-platform-integration", "state" => "removal-pending-after-final-commit",
        "cleanup_trigger" => "user-authorized-post-report-removal", "evidence" => "fixture:handoff"
      }]
    }
    report["terminal_requirements"].each { |row| row["outcome"] = "passed" }
    report["terminal_requirements"].each { |row| row["outcome"] = "not-applicable" if BLOCKED_REQUIREMENTS.include?(row["id"]) }
    report["blockers"] = []
    report
  end

  def terminal_blocked_fixture
    report = blocked_fixture
    report["units"].each do |row|
      row["status"] = "blocked"
      row["blocker"] = "blocker:fixture"
      row["evidence_disposition"] = "blocked"
    end
    report["blockers"] = report["units"].map do |row|
      { "id" => "fixture-#{row.fetch("unit").tr("/", "-")}", "category" => "user-authority",
        "unit" => row.fetch("unit"), "evidence" => "fixture:evidence", "required_user_action" => "Provide fixture authority" }
    end
    report
  end

  def self_check
    contract = authoritative_schema
    %i[
      assert_program_blocked assert_program_complete validate_report_shape
      validate_schema verify_repository_facts! verify_repository_state
    ].each do |method|
      assert(!respond_to?(method), "public final-report validation bypass exposed: #{method}")
    end
    AUTHORIZATION_TABLES.each do |heading, (columns, status_index)|
      synthetic = Array.new(columns) { |index| "field-#{index}" }
      synthetic[0] = "boundary-subject"
      synthetic[status_index] = "authorized"
      boundary = authorization_boundary(heading, synthetic, status_index)
      assert(boundary == {
        "id" => "#{slug(heading)}:boundary-subject", "kind" => slug(heading),
        "subject" => "boundary-subject", "status" => "authorized"
      }, "#{heading} nonempty boundary row is misparsed")
    end
    base = blocked_fixture
    validate_report_shape(base, validate_terminal_shape: false, contract: contract)
    original_schema_path = SCHEMA_PATH
    schema_authority_rejected = false
    Tempfile.create(["drifted-final-report-schema", ".json"]) do |file|
      file.write("{}\n")
      file.flush
      IdentityPlatformFinalReport.send(:remove_const, :SCHEMA_PATH)
      IdentityPlatformFinalReport.const_set(:SCHEMA_PATH, file.path)
      begin
        validate_report(base)
      rescue Error => e
        schema_authority_rejected = e.message.include?("FINAL_REPORT.schema.json drifted")
      ensure
        IdentityPlatformFinalReport.send(:remove_const, :SCHEMA_PATH)
        IdentityPlatformFinalReport.const_set(:SCHEMA_PATH, original_schema_path)
      end
    end
    assert(schema_authority_rejected, "negative fixture accepted: drifted checked-in schema")
    noncanonical = JSON.generate(base)
    assert(noncanonical != JSON.pretty_generate(base) + "\n", "noncanonical fixture setup failed")
    duplicate_key = '{"schema_version":1,"schema_version":1}'
    begin
      parse_json_bytes(duplicate_key, "duplicate-key fixture")
      raise Error, "negative fixture accepted: duplicate JSON key"
    rescue Error => e
      raise unless e.message.include?("duplicate JSON key")
    end
    blocked_terminal = terminal_blocked_fixture
    assert_program_blocked(blocked_terminal)
    mutations = {
      "false PROGRAM-COMPLETE" => ->(r) { r["terminal_predicate"] = "PROGRAM-COMPLETE" },
      "missing unit" => ->(r) { r["units"].pop },
      "unit substitution" => ->(r) { r["units"][0]["unit"] = r["units"][1]["unit"] },
      "unit evidence forgery" => ->(r) { r["units"][0]["evidence_bindings"] = ["forged"] },
      "stale parity digest" => ->(r) { r["parity"][0]["semantic_digest"] = "0" * 64 },
      "missing journey" => ->(r) { r["journeys"].pop },
      "missing cross-cutting claim" => ->(r) { r["cross_cutting"].pop },
      "missing artifact" => ->(r) { r["artifacts"].pop },
      "missing final gate" => ->(r) { r["final_gates"].pop },
      "upstream source object forgery" => ->(r) { r["upstream_baseline"]["source_objects"][0]["object_id"] = "0" * 40 },
      "provider substitution" => ->(r) { r["provider_boundaries"][0]["id"] = "invented" },
      "deployment substitution" => ->(r) { r["deployment_boundaries"][0]["id"] = "invented" },
      "missing terminal clause" => ->(r) { r["terminal_requirements"].pop },
      "blocked without blocker" => ->(r) { r["blockers"] = [] },
      "forged blocked detail" => ->(r) { r["blockers"][0]["required_user_action"] = "Forged action" },
      "verified no-push overclaim" => ->(r) { r["push_boundary"]["assertion_verified"] = true },
      "missing no-push boundary" => ->(r) { r.delete("push_boundary") },
      "missing no-push limitation" => ->(r) { r["push_boundary"].delete("limitation") },
      "rewritten no-push limitation" => ->(r) { r["push_boundary"]["limitation"] = "verified" },
      "extra nested field" => ->(r) { r["integration"]["invented"] = true },
      "newline revision suffix" => ->(r) { r["integration"]["final_input_revision"] += "\n" }
    }
    mutations.each do |name, mutate|
      candidate = deep_copy(base)
      mutate.call(candidate)
      begin
        validate_report_shape(candidate, validate_terminal_shape: false, contract: contract)
        assert_program_blocked(candidate) if
          name.start_with?("blocked") || name == "forged blocked detail"
      rescue Error
        next
      end
      raise Error, "negative fixture accepted: #{name}"
    end
    weak_blocked = deep_copy(blocked_terminal)
    weak_blocked["terminal_requirements"].find { |row| row["id"] == BLOCKED_REQUIREMENTS.first }["outcome"] = "failed"
    begin
      assert_program_blocked(weak_blocked)
      raise Error, "negative fixture accepted: weak blocked predicate"
    rescue Error => e
      raise if e.message.include?("negative fixture accepted")
    end
    complete = completed_fixture
    assert_program_complete(complete)
    verify_repository_facts!(complete, head: "1" * 40, branch: complete.dig("integration", "branch"), clean: true)
    complete_mutations = {
      "complete unit not verified" => ->(r) { r["units"][0]["status"] = "implemented-unverified" },
      "complete parity unavailable" => ->(r) { r["parity"][0]["outcome"] = "unavailable" },
      "complete journey failed" => ->(r) { r["journeys"][0]["outcome"] = "failed" },
      "complete cross-cutting failed" => ->(r) { r["cross_cutting"][0]["outcome"] = "failed" },
      "complete artifact unbound" => ->(r) { r["artifacts"][0]["evidence_binding"] = nil },
      "complete gate stale" => ->(r) { r["final_gates"][0]["tested_revision"] = "1" * 40 },
      "complete provider unproven" => ->(r) { r["provider_boundaries"][0]["outcome"] = "unproven" },
      "complete deployment not-required" => ->(r) { r["deployment_boundaries"][0]["outcome"] = "not-required" },
      "complete extra retained resource" => ->(r) { r["cleanup"]["entries"] << { "resource_id" => "r", "type" => "temporary-directory", "owner" => "worker", "target" => "/tmp/r", "state" => "retained-for-recovery", "evidence" => "e", "cleanup_trigger" => "later" } },
      "complete wrong retained resource" => ->(r) { r["cleanup"]["entries"][0]["type"] = "temporary-directory" },
      "complete wrong handoff reason" => ->(r) { r["cleanup"]["entries"][0]["cleanup_trigger"] = "later" },
      "complete integration falsely removed" => ->(r) { r["cleanup"]["integration_worktree_removed"] = true },
      "complete unauthorized change" => ->(r) { r["authorizations"]["unauthorized_semantic_changes"] = 1 },
      "complete terminal clause failed" => ->(r) { r["terminal_requirements"][0]["outcome"] = "failed" },
      "complete blocker present" => ->(r) { r["blockers"] << { "id" => "b" } }
    }
    complete_mutations.each do |name, mutate|
      candidate = deep_copy(complete)
      mutate.call(candidate)
      begin
        assert_program_complete(candidate)
      rescue Error
        next
      end
      raise Error, "false PROGRAM-COMPLETE fixture accepted: #{name}"
    end
    repository_mutations = {
      "dirty worktree" => { head: "1" * 40, branch: complete.dig("integration", "branch"), clean: false },
      "branch mismatch" => { head: "1" * 40, branch: "feature/not-the-integration", clean: true }
    }
    repository_mutations.each do |name, facts|
      begin
        verify_repository_facts!(complete, **facts)
      rescue Error
        next
      end
      raise Error, "false PROGRAM-COMPLETE fixture accepted: #{name}"
    end
    assert(post_input_paths_valid?([".ai/identity-platform/evidence/final-report.json"]), "valid post-input evidence path rejected")
    assert(!post_input_paths_valid?(["pkg/identity/backdoor.go"]), "negative fixture accepted: production change after final input")
    password_module = inventory.find { |row| row.fetch("unit") == "identity/password" }.fetch("module")
    assert("make check MODULES=#{password_module}" != "make check MODULES=pkg/unrelated", "negative fixture accepted: unrelated unit gate")
    puts "final report contract valid: 67 units, #{catalogs.fetch(:parity).length} parity rows, " \
         "#{catalogs.fetch(:journeys).length} journeys, #{catalogs.fetch(:cross_cutting).length} cross-cutting claims, " \
         "#{catalogs.fetch(:artifacts).length} artifacts, #{FINAL_GATES.length} final gates, " \
         "#{catalogs.fetch(:providers).length + catalogs.fetch(:captcha).length + catalogs.fetch(:independent_external_profiles).length} provider boundaries, " \
         "#{mutations.length + complete_mutations.length + repository_mutations.length + 6} negative fixtures"
  end

  def run(argv)
    case argv.first
    when "--schema"
      return 2 unless argv.length == 1
      print canonical_schema
    when "--write-schema"
      return 2 unless argv.length == 1
      File.write(SCHEMA_PATH, canonical_schema)
      puts "wrote #{SCHEMA_PATH}"
    when "--check"
      return 2 unless argv.length == 1
      self_check
    when "--validate"
      raise Error, "terminal validation requires validator-derived trusted state; run validate.rb --execution"
    else
      warn "usage: ruby final_report.rb --check | --schema | --write-schema | --validate REPORT.json"
      return 2
    end
    0
  rescue Error => e
    warn "final report invalid: #{e.message}"
    1
  end
end

exit IdentityPlatformFinalReport.run(ARGV) if $PROGRAM_NAME == __FILE__
