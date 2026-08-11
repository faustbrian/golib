#!/usr/bin/env ruby
# frozen_string_literal: true

require "digest"
require "json"
require "set"

ROOT = File.expand_path(__dir__)
OUTPUT = File.join(ROOT, "PUBLIC_CONTRACTS.json")
GOALS = File.join(ROOT, "GOAL_MANIFEST.json")
OPERATIONS = File.join(ROOT, "OPERATION_SEMANTICS.json")
API = File.join(ROOT, "API_OPERATIONS.md")
PARITY = File.join(ROOT, "PARITY_DISPOSITIONS.json")
META = File.join(ROOT, "fragments/public_contracts_meta.json")
FAMILIES = %w[core authn oauth org_scim risk_delivery].map { |name| File.join(ROOT, "fragments/public_contracts_#{name}.json") }.freeze
NUMERIC_FIELD_TYPE = /\A(?:u?int(?:8|16|32|64)?|float(?:32|64)|time\.Duration)\z/.freeze

def canonical(value)
  case value
  when Hash then value.keys.sort.to_h { |key| [key, canonical(value.fetch(key))] }
  when Array then value.map { |item| canonical(item) }
  else value
  end
end

def encoded(value)
  JSON.pretty_generate(canonical(value)) + "\n"
end

def sha(value)
  Digest::SHA256.hexdigest(value.is_a?(String) ? value : encoded(value))
end

def fail_contract(message)
  abort("PUBLIC_CONTRACTS: #{message}")
end

def validate_closed_object!(label, object, required:, optional: [])
  fail_contract("#{label} must be a JSON object") unless object.is_a?(Hash)
  allowed = required + optional
  missing = required - object.keys
  unexpected = object.keys - allowed
  fail_contract("#{label} lacks #{missing.join(', ')}") unless missing.empty?
  fail_contract("#{label} has unexpected keys #{unexpected.join(', ')}") unless unexpected.empty?
end

class DuplicateJSONKeyDetector
  def initialize(source)
    @source = source
    @index = 0
  end

  def detect!
    skip_whitespace
    parse_value
    skip_whitespace
    raise ArgumentError, "unexpected trailing JSON content" unless @index == @source.bytesize
  end

  private

  def parse_value
    case byte
    when 0x7b then parse_object
    when 0x5b then parse_array
    when 0x22 then parse_string
    else parse_scalar
    end
  end

  def parse_object
    @index += 1
    keys = Set.new
    skip_whitespace
    return @index += 1 if byte == 0x7d

    loop do
      key = parse_string
      raise ArgumentError, "duplicate JSON object key #{key.inspect}" unless keys.add?(key)
      skip_whitespace
      expect!(0x3a)
      skip_whitespace
      parse_value
      skip_whitespace
      break expect!(0x7d) unless byte == 0x2c

      @index += 1
      skip_whitespace
    end
  end

  def parse_array
    @index += 1
    skip_whitespace
    return @index += 1 if byte == 0x5d

    loop do
      parse_value
      skip_whitespace
      break expect!(0x5d) unless byte == 0x2c

      @index += 1
      skip_whitespace
    end
  end

  def parse_string
    start = @index
    expect!(0x22)
    loop do
      case byte
      when 0x5c then @index += 2
      when 0x22
        @index += 1
        return JSON.parse(@source.byteslice(start...@index))
      else @index += 1
      end
    end
  end

  def parse_scalar
    @index += 1 while byte && ![0x2c, 0x5d, 0x7d, 0x20, 0x09, 0x0a, 0x0d].include?(byte)
  end

  def skip_whitespace
    @index += 1 while [0x20, 0x09, 0x0a, 0x0d].include?(byte)
  end

  def expect!(expected)
    raise ArgumentError, "malformed JSON structure" unless byte == expected

    @index += 1
  end

  def byte
    @source.getbyte(@index)
  end
end

def parse_fragment_source!(source, label:)
  fragment = JSON.parse(source)
  DuplicateJSONKeyDetector.new(source).detect!
  expected = JSON.pretty_generate(fragment) + "\n"
  raise ArgumentError, "#{label} is not canonical pretty JSON" unless source == expected

  fragment
rescue JSON::ParserError => e
  raise ArgumentError, "#{label} is invalid JSON: #{e.message}"
end

def parse_fragment!(path)
  parse_fragment_source!(File.binread(path), label: File.basename(path))
rescue ArgumentError => e
  fail_contract(e.message)
end

def validate_meta_fragment!(fragment)
  expected_keys = %w[schema_version manifest_schema external_contracts validation_rules product_exclusions]
  validate_closed_object!("public_contracts_meta.json", fragment, required: expected_keys)
  fail_contract("public_contracts_meta.json schema version drifted") unless fragment.fetch("schema_version") == 1
  fail_contract("public_contracts_meta.json lacks validation rules") unless fragment.fetch("validation_rules").is_a?(Array) && !fragment.fetch("validation_rules").empty?
  manifest_schema = fragment.fetch("manifest_schema")
  validate_closed_object!("public_contracts_meta.json manifest_schema", manifest_schema, required: %w[
    allowed_top_level_keys authority_aliases bounds_rules canonical_ordering external_contract_pin_rules
    forbidden_placeholders operation_required_keys reference_rules required_primitive_extensions
    standard_library_go_version type_kinds unit_required_keys
  ])
  manifest_schema.fetch("authority_aliases").each_with_index do |row, index|
    validate_closed_object!("public_contracts_meta.json authority_aliases[#{index}]", row, required: %w[alias authority package_path])
  end
  manifest_schema.fetch("required_primitive_extensions").each_with_index do |row, index|
    validate_closed_object!("public_contracts_meta.json required_primitive_extensions[#{index}]", row,
      required: %w[unit extension_unit package_path depends_on consumers required_contract_sha256 required_symbols])
  end
  fragment.fetch("external_contracts").each_with_index do |contract, index|
    validate_closed_object!("public_contracts_meta.json external_contracts[#{index}]", contract,
      required: %w[owner package_path current_api_status types interfaces methods errors], optional: %w[required_contract_sha256])
    contract.fetch("types").each_with_index do |type, type_index|
      validate_closed_object!("public_contracts_meta.json external_contracts[#{index}].types[#{type_index}]", type,
        required: %w[name declaration semantics])
    end
    contract.fetch("interfaces").each_with_index do |interface, interface_index|
      validate_closed_object!("public_contracts_meta.json external_contracts[#{index}].interfaces[#{interface_index}]", interface,
        required: %w[name methods semantics])
    end
  end
  fragment.fetch("product_exclusions").each_with_index do |row, index|
    validate_closed_object!("public_contracts_meta.json product_exclusions[#{index}]", row, required: %w[id disposition authority scope])
  end
end

def field_inventory(fragment)
  fields = []
  fragment.fetch("units").each do |unit|
    unit.fetch("types").each { |type| fields.concat(type.fetch("fields", [])) }
    unit.fetch("errors").each { |error| fields.concat(error.fetch("fields", [])) }
  end
  fragment.fetch("operations").each do |operation|
    fields.concat(operation.dig("request", "fields") || [])
    fields.concat(operation.dig("result", "fields") || [])
    operation.dig("errors", "variants").to_a.each { |error| fields.concat(error.fetch("fields", [])) }
  end
  fields
end

def validate_fragment_metadata!(path, metadata)
  fail_contract("#{File.basename(path)} meta must be a non-empty object") unless metadata.is_a?(Hash) && !metadata.empty?
  validate_closed_object!("#{File.basename(path)} meta", metadata,
    required: [], optional: %w[external_contracts negative_fixtures positive_examples type_kinds validation_rules])
  if metadata.key?("validation_rules")
    rules = metadata.fetch("validation_rules")
    valid = rules.is_a?(Array) && !rules.empty? && rules.each_with_index.all? do |rule, index|
      validate_closed_object!("#{File.basename(path)} meta validation_rules[#{index}]", rule, required: %w[id rule scope error])
      %w[id rule scope error].all? { |key| rule[key].is_a?(String) && !rule[key].empty? }
    end
    fail_contract("#{File.basename(path)} meta validation_rules are malformed") unless valid
    fail_contract("#{File.basename(path)} meta validation rule IDs are duplicated") unless rules.map { |rule| rule.fetch("id") }.uniq.length == rules.length
  end
  %w[negative_fixtures positive_examples].each do |collection|
    next unless metadata.key?(collection)
    rows = metadata.fetch(collection)
    valid = rows.is_a?(Array) && !rows.empty? && rows.all? { |row| row.is_a?(Hash) && row["id"].is_a?(String) && !row["id"].empty? }
    fail_contract("#{File.basename(path)} meta #{collection} are malformed") unless valid
    fail_contract("#{File.basename(path)} meta #{collection} IDs are duplicated") unless rows.map { |row| row.fetch("id") }.uniq.length == rows.length
    if collection == "negative_fixtures"
      complete = rows.each_with_index.all? do |row, index|
        validate_closed_object!("#{File.basename(path)} meta negative_fixtures[#{index}]", row, required: %w[id expected_error mutation])
        mutation = row["mutation"]
        validate_closed_object!("#{File.basename(path)} meta negative_fixtures[#{index}].mutation", mutation, required: %w[selector set]) if mutation.is_a?(Hash)
        row["expected_error"].is_a?(String) && !row["expected_error"].empty? &&
          mutation.is_a?(Hash) && mutation["selector"].is_a?(String) && !mutation["selector"].empty? &&
          mutation["set"].is_a?(Hash) && !mutation["set"].empty?
      end
      fail_contract("#{File.basename(path)} meta negative_fixtures lack executable mutation shape") unless complete
    else
      complete = rows.each_with_index.all? do |row, index|
        validate_closed_object!("#{File.basename(path)} meta positive_examples[#{index}]", row,
          required: %w[id expected], optional: %w[field fields value values])
        row["expected"].is_a?(String) && !row["expected"].empty?
      end
      fail_contract("#{File.basename(path)} meta positive_examples lack expected outcomes") unless complete
    end
  end
  metadata.fetch("external_contracts", []).each_with_index do |row, index|
    validate_closed_object!("#{File.basename(path)} meta external_contracts[#{index}]", row, required: %w[package types])
  end
  metadata.fetch("type_kinds", []).each_with_index do |row, index|
    validate_closed_object!("#{File.basename(path)} meta type_kinds[#{index}]", row, required: %w[name semantics])
  end
end

def api_operation_ids
  File.readlines(API).filter_map do |line|
    cells = line.split("|").map(&:strip)
    next unless cells.length == 7
    id = cells[1][/\A`(identity\.[^`]+)`\z/, 1]
    next unless id && cells[2].match?(%r{/ (both|direct|protocol|middleware)\z})
    id
  end
end

def validate_fragment!(path, fragment, allowed_kinds)
  fail_contract("#{File.basename(path)} must be a JSON object") unless fragment.is_a?(Hash)
  placeholder = /typed field required by|typed result required by|"semantics":"Typed field\."|"semantics":"Canonical bounded value\."|"semantics":"Opaque bounded identity\."|"semantics":"Opaque scoped identifier\."|Operation-specific stable/i
  fail_contract("#{File.basename(path)} contains placeholder contract prose") if JSON.generate(fragment).match?(placeholder)
  expected_keys = %w[operations units]
  expected_keys << "external_contracts" if fragment.key?("external_contracts")
  expected_keys << "meta" if fragment.key?("meta")
  fail_contract("#{File.basename(path)} has unexpected top-level keys") unless fragment.keys.sort == expected_keys.sort
  validate_fragment_metadata!(path, fragment.fetch("meta")) if fragment.key?("meta")
  fail_contract("#{File.basename(path)} has no units") if fragment.fetch("units").empty?
  fragment.fetch("units").each do |unit|
    validate_closed_object!("#{File.basename(path)} unit #{unit['unit'] || '<unnamed>'}", unit,
      required: %w[unit package_name types interfaces constructors errors zero_value_semantics concurrency_ownership])
    fail_contract("#{unit["unit"]} has no public type or interface contract") if unit.fetch("types").empty? && unit.fetch("interfaces").empty?
    fail_contract("#{unit["unit"]} has no constructor") if unit.fetch("constructors").empty?
    fail_contract("#{unit["unit"]} has no stable errors") if unit.fetch("errors").empty?
    fail_contract("#{unit["unit"]} lacks zero-value semantics") if unit.fetch("zero_value_semantics").empty?
    fail_contract("#{unit["unit"]} lacks concurrency/ownership semantics") if unit.fetch("concurrency_ownership").empty?
    unit.fetch("types").each do |type|
      validate_closed_object!("#{unit['unit']} type #{type['name'] || '<unnamed>'}", type,
        required: %w[name kind zero_value ownership concurrency],
        optional: %w[bounds constants discriminator fields methods representation semantics signature underlying variants])
      %w[name kind zero_value ownership concurrency].each { |key| fail_contract("#{unit["unit"]} type lacks #{key}") unless type.key?(key) && !type[key].to_s.empty? }
      fail_contract("#{unit["unit"]}.#{type["name"]} uses forbidden type kind #{type["kind"]}") unless allowed_kinds.include?(type.fetch("kind"))
      if type.fetch("kind") == "struct" && type.fetch("fields", []).empty?
        fail_contract("#{unit["unit"]}.#{type["name"]} is an unexplained empty struct") unless type["representation"] == "unexported"
      end
      type_method_names = type.fetch("methods", []).map.with_index do |method, index|
        validate_closed_object!("#{unit['unit']}.#{type['name']} method[#{index}]", method,
          required: %w[name signature semantics])
        %w[name signature semantics].each do |key|
          fail_contract("#{unit['unit']}.#{type['name']} method[#{index}] lacks textual #{key}") unless method[key].is_a?(String) && !method[key].empty?
        end
        method.fetch("name")
      end
      fail_contract("#{unit['unit']}.#{type['name']} has duplicate methods") unless type_method_names.uniq.length == type_method_names.length
      backup_flag_type = unit.fetch("unit") == "webauthn" && %w[Credential VerifiedRegistration VerifiedAssertion].include?(type.fetch("name"))
      if backup_flag_type
        backup_eligible = type.fetch("fields", []).find { |field| field["name"] == "BackupEligible" }
        backup_state = type.fetch("fields", []).find { |field| field["name"] == "BackupState" }
        fail_contract("#{unit['unit']}.#{type['name']} backup flags omit the BE=false/BS=false validity and BS implies BE invariant") unless backup_eligible && backup_state
        eligible_contract = backup_eligible.fetch("semantics").match?(/false.*valid/i) &&
          backup_eligible.fetch("semantics").include?("BackupState=false") && backup_eligible.fetch("zero_value").match?(/false.*valid/i)
        state_contract = backup_state.fetch("semantics").match?(/false.*valid/i) &&
          backup_state.fetch("semantics").include?("true is valid only when BackupEligible=true") && backup_state.fetch("zero_value").match?(/false.*valid/i)
        fail_contract("#{unit['unit']}.#{type['name']} backup flags omit the BE=false/BS=false validity and BS implies BE invariant") unless eligible_contract && state_contract
      end
    end
    if unit.fetch("unit") == "passkey"
      backup_state = unit.fetch("types").find { |type| type.fetch("name") == "BackupState" }
      credential_profile = unit.fetch("types").find { |type| type.fetch("name") == "CredentialProfile" }
      profile_backup_state = credential_profile&.fetch("fields", [])&.find { |field| field["name"] == "BackupState" }
      valid_projection = backup_state&.fetch("kind", nil) == "enum" &&
        backup_state&.fetch("constants", nil) == %w[NotEligible EligibleNotBackedUp EligibleBackedUp] &&
        profile_backup_state&.fetch("type", nil) == "BackupState"
      fail_contract("passkey.BackupState does not represent the closed authenticator backup state") unless valid_projection
    end
    symbol_names = unit.fetch("types").map { |type| type.fetch("name") } + unit.fetch("interfaces").map { |interface| interface.fetch("name") } + unit.fetch("errors").map { |error| error.fetch("name") }
    fail_contract("#{unit["unit"]} has duplicate exported symbols") unless symbol_names.uniq.length == symbol_names.length
    unit.fetch("interfaces").each do |interface|
      validate_closed_object!("#{unit['unit']} interface #{interface['name'] || '<unnamed>'}", interface, required: %w[name methods])
      fail_contract("#{unit["unit"]}.#{interface["name"]} is an empty interface") if interface.fetch("methods").empty?
      names = interface.fetch("methods").map do |method|
        validate_closed_object!("#{unit['unit']}.#{interface['name']} method #{method['name'] || '<unnamed>'}", method,
          required: %w[name signature semantics])
        %w[name signature semantics].each { |key| fail_contract("#{unit["unit"]}.#{interface["name"]} method lacks #{key}") unless method.key?(key) && !method[key].to_s.empty? }
        method.fetch("name")
      end
      fail_contract("#{unit["unit"]}.#{interface["name"]} has duplicate methods") unless names.uniq.length == names.length
    end
    unit.fetch("constructors").each_with_index do |constructor, index|
      validate_closed_object!("#{unit['unit']} constructor[#{index}]", constructor, required: %w[signature semantics])
      %w[signature semantics].each { |key| fail_contract("#{unit["unit"]} constructor lacks #{key}") unless !constructor[key].to_s.empty? }
    end
    identities = unit.fetch("errors").map do |error|
      validate_closed_object!("#{unit['unit']} error #{error['name'] || '<unnamed>'}", error,
        required: %w[name class identity semantics], optional: %w[fields])
      %w[name class identity semantics].each { |key| fail_contract("#{unit["unit"]} error lacks #{key}") unless error.key?(key) && !error[key].to_s.empty? }
      error.fetch("identity")
    end
    fail_contract("#{unit["unit"]} has duplicate stable error identities") unless identities.uniq.length == identities.length
  end
  fields = field_inventory(fragment)
  fields.each do |field|
    validate_closed_object!("#{File.basename(path)} field #{field['name'] || '<unnamed>'}", field,
      required: %w[name type required zero_value semantics], optional: %w[bounds false_valid zero_valid zero_value_detail])
    %w[name type required zero_value semantics].each { |key| fail_contract("#{File.basename(path)} field #{field["name"] || "<unnamed>"} lacks #{key}") unless field.key?(key) && (!%w[name type semantics].include?(key) || !field[key].to_s.empty?) }
    fail_contract("#{File.basename(path)} field #{field['name']} required must be boolean") unless [true, false].include?(field.fetch("required"))
    unless field.fetch("zero_value").is_a?(String) && !field.fetch("zero_value").empty?
      fail_contract("#{File.basename(path)} field #{field['name']} zero_value must be a non-empty string")
    end
    fail_contract("#{File.basename(path)} contains placeholder semantics for #{field["name"]}") if field.fetch("semantics").match?(/typed field required by|typed result required by|\ATyped field\.\z|\ACanonical bounded value\.\z|\AOpaque bounded identity\.\z|\AOpaque scoped identifier\.\z|Operation-specific stable/i)
    fail_contract("#{File.basename(path)} contains unexported Go field #{field["name"]}") unless field.fetch("name").match?(/\A[A-Z][A-Za-z0-9]*\z/)
    type = field.fetch("type")
    fail_contract("#{File.basename(path)} contains forbidden type #{type}") if type.match?(/(?:\bany\b|interface\{\}|map\[|json\.RawMessage)/)
    if type == "bool"
      fail_contract("#{File.basename(path)} boolean field #{field['name']} lacks explicit false-validity") unless [true, false].include?(field["false_valid"])
      zero_value = field.fetch("zero_value")
      rejects_false = zero_value.match?(/\b(?:invalid|rejected?)\b/i) &&
        !zero_value.match?(/\bfalse\b.*\bvalid\b|\bnot\b.*\binvalid\b/i)
      fail_contract("#{File.basename(path)} boolean field #{field['name']} documents false as valid but rejects its zero value") if field.fetch("false_valid") && rejects_false
      fail_contract("#{File.basename(path)} boolean field #{field['name']} documents false as invalid but accepts its zero value") if !field.fetch("false_valid") && !rejects_false
    elsif field.key?("false_valid")
      fail_contract("#{File.basename(path)} non-boolean field #{field['name']} declares false-validity")
    end
    if type.match?(NUMERIC_FIELD_TYPE)
      fail_contract("#{File.basename(path)} numeric field #{field['name']} lacks explicit zero-validity") unless [true, false].include?(field["zero_valid"])
      expected_zero_value = field.fetch("zero_valid") ? "0 is valid" : "0 is invalid"
      unless field.fetch("zero_value") == expected_zero_value
        fail_contract("#{File.basename(path)} numeric field #{field['name']} zero_value must equal #{expected_zero_value.inspect} when zero_valid=#{field.fetch('zero_valid')}")
      end
      unless field["zero_value_detail"].is_a?(String) && !field.fetch("zero_value_detail").empty?
        fail_contract("#{File.basename(path)} numeric field #{field['name']} lacks textual zero-value detail")
      end
    elsif field.key?("zero_valid") || field.key?("zero_value_detail")
      fail_contract("#{File.basename(path)} non-numeric field #{field['name']} declares numeric zero-validity")
    end
    if type.match?(/\A(?:string|\[\]|u?int(?:8|16|32|64)?|time\.Duration)/)
      fail_contract("#{File.basename(path)} field #{field["name"]} lacks an explicit finite bound") unless field["bounds"].to_s.match?(/\d|named|profile|constant/i)
    end
    if type == "[]byte" || type.end_with?(".Payload") || type == "Payload"
      text = "#{field["semantics"]} #{field["bounds"]}"
      fail_contract("#{File.basename(path)} contains unclosed payload field #{field["name"]}") unless text.match?(/protocol|wire|outbox/i) && text.match?(/version/i) && field["bounds"].to_s.match?(/\d/)
    end
  end
  fragment.fetch("operations").each do |operation|
    validate_closed_object!("#{File.basename(path)} operation #{operation['id'] || '<unnamed>'}", operation,
      required: %w[id owner collaborators service_interface method signature request result errors authorization transport semantics],
      optional: %w[dependency_authority])
    %w[id owner collaborators service_interface method signature request result errors authorization transport semantics].each { |key| fail_contract("#{operation["id"] || "operation"} lacks #{key}") unless operation.key?(key) }
    %w[request result].each do |kind|
      schema = operation.fetch(kind)
      validate_closed_object!("#{operation['id']} #{kind}", schema, required: %w[schema_id name fields])
      %w[schema_id name fields].each { |key| fail_contract("#{operation["id"]} #{kind} lacks #{key}") unless schema.key?(key) }
    end
    errors = operation.fetch("errors")
    validate_closed_object!("#{operation['id']} errors", errors, required: %w[name schema_id variants])
    fail_contract("#{operation["id"]} has no exact error variants") if errors.fetch("variants").empty?
    variants = errors.fetch("variants").map do |variant|
      validate_closed_object!("#{operation['id']} error variant #{variant['identity'] || '<unnamed>'}", variant,
        required: %w[type identity semantics], optional: %w[fields])
      %w[type identity semantics].each { |key| fail_contract("#{operation["id"]} error variant lacks #{key}") unless variant.key?(key) && !variant[key].to_s.empty? }
      variant.fetch("identity")
    end
    validate_closed_object!("#{operation['id']} authorization", operation.fetch("authorization"), required: %w[access csrf_origin policy])
    validate_closed_object!("#{operation['id']} transport", operation.fetch("transport"), required: %w[exposure http_method http_path openapi_operation_id])
    fail_contract("#{operation["id"]} has duplicate error identities") unless variants.uniq.length == variants.length
  end
  fragment.fetch("external_contracts", []).each_with_index do |contract, index|
    validate_closed_object!("#{File.basename(path)} external_contracts[#{index}]", contract, required: %w[owner package_path symbols])
  end
end

def type_entry(owner, category, definition, id)
  {"category"=>category, "definition"=>definition, "id"=>id, "owner"=>owner}
end

def operation_service_name(operation)
  operation.fetch("service_interface").split(".").last
end

def operation_method_definition(operation)
  {
    "name"=>operation.fetch("method"),
    "signature"=>operation.fetch("signature"),
    "semantics"=>operation.fetch("semantics")
  }
end

def composed_service_interface(owner, name, declared, operations)
  definition = declared ? JSON.parse(JSON.generate(declared)) : {"name"=>name, "methods"=>[]}
  methods = definition.fetch("methods")
  operations.sort_by { |operation| operation.fetch("id").b }.each do |operation|
    method = operation_method_definition(operation)
    existing = methods.find { |candidate| candidate.fetch("name") == method.fetch("name") }
    if existing
      unless existing.fetch("signature") == method.fetch("signature")
        fail_contract("#{owner}.#{name}.#{method['name']} conflicts with its operation signature")
      end
      existing["operation_semantics"] = method.fetch("semantics")
    else
      methods << method.merge("operation_semantics"=>method.fetch("semantics"))
    end
  end
  definition["methods"] = methods.sort_by { |method| method.fetch("name").b }
  definition
end

def digest_api_files(relative_paths)
  bytes = relative_paths.sort.map do |relative|
    contents = File.binread(File.join(File.dirname(ROOT), "..", relative))
    "#{relative}\0#{contents.bytesize}\0#{contents}\0"
  end.join
  "sha256:#{Digest::SHA256.hexdigest(bytes)}"
end

def local_contract_pin(relative)
  repository = File.expand_path(File.join(File.dirname(ROOT), ".."))
  directory = File.expand_path(File.join(repository, relative))
  fail_contract("external package path #{relative} does not exist") unless Dir.exist?(directory)
  module_directory = directory
  module_directory = File.dirname(module_directory) until File.file?(File.join(module_directory, "go.mod")) || module_directory == repository
  fail_contract("external package #{relative} has no owning go.mod") unless File.file?(File.join(module_directory, "go.mod"))
  api_files = Dir.glob(File.join(directory, "api", "**", "*")).select { |path| File.file?(path) }
  source_files = Dir.glob(File.join(directory, "*.go")).reject { |path| path.end_with?("_test.go") }
  files = [File.join(module_directory, "go.mod"), *source_files, *api_files].uniq.sort
  fail_contract("external package #{relative} has no pinnable public source") if source_files.empty?
  relative_files = files.map { |path| path.delete_prefix("#{repository}/") }.sort
  {"api_baseline_files"=>relative_files, "api_baseline_sha256"=>digest_api_files(relative_files)}
end

LOCAL_ALIASES = {
  "audit"=>"pkg/audit", "authentication"=>"pkg/authentication", "authorization"=>"pkg/authorization",
  "capability"=>"pkg/capability", "capabilitypostgres"=>"pkg/capability/postgres", "clock"=>"pkg/clock",
  "golibpassword"=>"pkg/password", "httpclient"=>"pkg/http-client", "idempotency"=>"pkg/idempotency", "identifier"=>"pkg/identifier",
  "limit"=>"pkg/limit", "outbox"=>"pkg/outbox", "pagination"=>"pkg/pagination", "rate_limit"=>"pkg/rate-limit", "request"=>"pkg/request",
  "secretenvelope"=>"pkg/secret-envelope", "tenancy"=>"pkg/tenancy", "workflow"=>"pkg/workflow"
}.freeze
STDLIB_ALIASES = %w[context errors http io netip sql testing time url].freeze
THIRD_PARTY = {
  "pgxpool"=>{"module"=>"github.com/jackc/pgx/v5", "package_path"=>"github.com/jackc/pgx/v5/pgxpool", "version"=>"v5.10.0"},
  "valkeyclient"=>{"module"=>"github.com/valkey-io/valkey-go", "package_path"=>"github.com/valkey-io/valkey-go", "version"=>"v1.0.76"}
}.freeze

def referenced_qualified_symbols(fragments)
  expressions = []
  fragments.each do |_path, fragment|
    fragment.fetch("units").each do |unit|
      unit.fetch("types").each do |type|
        expressions << type["underlying"] if type["underlying"]
        type.fetch("fields", []).each { |field| expressions << field.fetch("type") }
      end
      unit.fetch("interfaces").each { |interface| interface.fetch("methods").each { |method| expressions << method.fetch("signature") } }
      unit.fetch("constructors").each { |constructor| expressions << constructor.fetch("signature") }
      unit.fetch("errors").each { |error| error.fetch("fields", []).each { |field| expressions << field.fetch("type") } }
    end
    fragment.fetch("operations").each do |operation|
      expressions << operation.fetch("service_interface")
      expressions << operation.fetch("signature")
      if LOCAL_ALIASES.key?(operation.fetch("owner"))
        owner = operation.fetch("owner")
        expressions << "#{owner}.#{operation.dig('request', 'name')}"
        expressions << "#{owner}.#{operation.dig('result', 'name')}"
        expressions << "#{owner}.#{operation.dig('errors', 'name')}"
      end
      operation.dig("request", "fields").to_a.each { |field| expressions << field.fetch("type") }
      operation.dig("result", "fields").to_a.each { |field| expressions << field.fetch("type") }
      operation.dig("errors", "variants").to_a.each { |error| error.fetch("fields", []).each { |field| expressions << field.fetch("type") } }
    end
  end
  text = expressions.compact.join("\n")
  text.scan(/\b([a-z][a-z0-9_]*)\.([A-Z][A-Za-z0-9]*)/).map { |prefix, symbol| [prefix, "#{prefix}.#{symbol}"] }.uniq
end

def pinned_external_contracts(meta_contracts, fragments, units)
  unit_aliases = units.flat_map { |unit| [unit.fetch("package_name"), unit.fetch("unit").delete("/-")] }.uniq
  references = referenced_qualified_symbols(fragments).reject { |prefix, _symbol| unit_aliases.include?(prefix) }
  grouped = references.group_by(&:first).transform_values { |rows| rows.map(&:last).sort }
  known = (LOCAL_ALIASES.keys + STDLIB_ALIASES + THIRD_PARTY.keys)
  unknown = grouped.keys - known
  fail_contract("unresolved external package aliases: #{unknown.sort.join(", ")}") unless unknown.empty?

  meta_by_alias = meta_contracts.to_h do |contract|
    alias_name = contract.fetch("owner").tr("/-", "_").sub("capability_postgres", "capabilitypostgres").sub("secret_envelope", "secretenvelope").sub("password", "golibpassword")
    [alias_name, contract]
  end
  grouped.map do |alias_name, symbols|
    if STDLIB_ALIASES.include?(alias_name)
      package_path = {"netip"=>"net/netip", "sql"=>"database/sql", "url"=>"net/url", "http"=>"net/http"}.fetch(alias_name, alias_name)
      {"alias"=>alias_name, "go_version"=>"go1.26.5", "owner"=>"stdlib:#{alias_name}", "package_path"=>package_path, "symbols"=>symbols}
    elsif THIRD_PARTY.key?(alias_name)
      entry = THIRD_PARTY.fetch(alias_name)
      {"alias"=>alias_name, "module_path"=>entry.fetch("module"), "module_version"=>entry.fetch("version"), "owner"=>"third-party:#{alias_name}", "package_path"=>entry.fetch("package_path"), "symbols"=>symbols}
    else
      relative = LOCAL_ALIASES.fetch(alias_name)
      base = meta_by_alias[alias_name] || {"owner"=>alias_name, "package_path"=>"github.com/faustbrian/golib/#{relative}"}
      base.merge("alias"=>alias_name, "symbols"=>symbols).merge(local_contract_pin(relative))
    end
  end.sort_by { |entry| entry.fetch("owner") }
end

def validate_required_extensions!(meta, external, units, fragments)
  schedule = meta.dig("manifest_schema", "required_primitive_extensions")
  fail_contract("required_primitive_extensions is missing") unless schedule.is_a?(Array)
  fail_contract("required_primitive_extensions is not ordered") unless schedule == schedule.sort_by { |row| row.fetch("unit") }
  fail_contract("required_primitive_extensions contains duplicate authorities") unless schedule.map { |row| row.fetch("unit") }.uniq.length == schedule.length

  required_external = external.select { |row| row["current_api_status"] == "requires-extension" }
  scheduled_authorities = schedule.map { |row| row.fetch("unit") }
  referenced_authorities = required_external.map { |row| row.fetch("owner") }.sort
  fail_contract("required extension authority inventory drifted: scheduled #{scheduled_authorities}, referenced #{referenced_authorities}") unless scheduled_authorities == referenced_authorities
  known_units = units.map { |unit| unit.fetch("unit") }
  extension_units = schedule.map { |row| row.fetch("extension_unit") }.uniq.sort
  fail_contract("expected five primitive extension units") unless extension_units.length == 5
  fail_contract("invalid primitive extension unit") unless extension_units.all? { |unit| unit.match?(%r{\Aprimitive/[a-z0-9-]+\z}) }

  external_by_owner = required_external.to_h { |row| [row.fetch("owner"), row] }
  schedule.each do |row|
    authority = row.fetch("unit")
    contract = external_by_owner.fetch(authority)
    fail_contract("#{authority} required-contract digest drifted") unless row.fetch("required_contract_sha256") == contract.fetch("required_contract_sha256")
    contract_payload = contract.slice("types", "interfaces", "methods", "errors")
    recomputed_contract_digest = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(contract_payload)))}"
    fail_contract("#{authority} required-contract digest is invalid") unless contract.fetch("required_contract_sha256") == recomputed_contract_digest
    fail_contract("#{authority} package path drifted") unless row.fetch("package_path") == contract.fetch("package_path")
    required_symbols = row.fetch("required_symbols")
    fail_contract("#{authority} required symbols are not sorted and unique") unless required_symbols == required_symbols.sort.uniq
    fail_contract("#{authority} required symbols are not referenced") unless (required_symbols - contract.fetch("symbols")).empty?
    consumers = row.fetch("consumers")
    fail_contract("#{authority} consumers are not sorted and unique") unless consumers == consumers.sort.uniq
    fail_contract("#{authority} has unknown consumers") unless (consumers - known_units).empty?
    fail_contract("#{authority} has no blocked consumers") if consumers.empty?
    derived_consumers = []
    fragments.each do |_path, fragment|
      fragment.fetch("units").each do |unit|
        derived_consumers << unit.fetch("unit") if required_symbols.any? { |symbol| JSON.generate(unit).include?(symbol) }
      end
      fragment.fetch("operations").each do |operation|
        next unless required_symbols.any? { |symbol| JSON.generate(operation).include?(symbol) }
        derived_consumers.concat(([operation.fetch("owner")] + operation.fetch("collaborators")).select { |unit| known_units.include?(unit) })
      end
    end
    derived_consumers = derived_consumers.uniq.sort
    fail_contract("#{authority} consumer closure drifted: declared #{consumers}, derived #{derived_consumers}") unless consumers == derived_consumers
  end

  capability_extensions = schedule.select { |row| %w[capability capability/postgres].include?(row.fetch("unit")) }
  fail_contract("capability extension authorities must share one schedulable unit") unless capability_extensions.map { |row| row.fetch("extension_unit") }.uniq == ["primitive/capability-identity-contracts"]
  fail_contract("capability/postgres extension must follow capability") unless schedule.find { |row| row.fetch("unit") == "capability/postgres" }.fetch("depends_on") == ["capability"]
end

def build
  meta = parse_fragment!(META)
  goals = JSON.parse(File.read(GOALS)).fetch("goals")
  semantics = JSON.parse(File.read(OPERATIONS)).fetch("operations")
  api_ids = api_operation_ids
  fail_contract("API_OPERATIONS contains duplicate operation IDs") unless api_ids.uniq.length == api_ids.length
  fail_contract("API_OPERATIONS and OPERATION_SEMANTICS operation inventories differ") unless api_ids.sort == semantics.map { |operation| operation.fetch("id") }.sort
  fragments = FAMILIES.map { |path| [path, parse_fragment!(path)] }
  allowed_kinds = meta.dig("manifest_schema", "type_kinds")
  fragments.each { |path, fragment| validate_fragment!(path, fragment, allowed_kinds) }

  units = fragments.flat_map { |_path, fragment| fragment.fetch("units") }
  operations = fragments.flat_map { |_path, fragment| fragment.fetch("operations") }
  fail_contract("unit count is #{units.length}, expected 61") unless units.length == 61
  fail_contract("duplicate unit") unless units.map { |unit| unit.fetch("unit") }.uniq.length == units.length
  expected_units = goals.reject { |goal| goal.fetch("unit").start_with?("primitive/") }.map { |goal| goal.fetch("unit") }
  fail_contract("unit inventory differs from GOAL_MANIFEST") unless units.map { |unit| unit.fetch("unit") }.sort == expected_units.sort
  unit_by_name = units.to_h { |unit| [unit.fetch("unit"), unit] }
  units = expected_units.map { |unit| unit_by_name.fetch(unit) }
  fail_contract("operation count is #{operations.length}, expected #{semantics.length}") unless operations.length == semantics.length
  fail_contract("duplicate operation") unless operations.map { |operation| operation.fetch("id") }.uniq.length == operations.length
  expected_operations = semantics.to_h { |operation| [operation.fetch("id"), operation] }
  fail_contract("operation inventory differs from OPERATION_SEMANTICS") unless operations.map { |operation| operation.fetch("id") }.sort == expected_operations.keys.sort

  operations.each do |operation|
    expected = expected_operations.fetch(operation.fetch("id"))
    declared = [operation.fetch("owner"), *operation.fetch("collaborators")]
    fail_contract("#{operation["id"]} owner/collaborator drift") unless declared == expected.fetch("owners")
    expected_authorization = expected.slice("access", "authorization", "csrf_origin")
    actual_authorization = {"access"=>operation.dig("authorization", "access"), "authorization"=>operation.dig("authorization", "policy"), "csrf_origin"=>operation.dig("authorization", "csrf_origin")}
    fail_contract("#{operation["id"]} authorization drift") unless actual_authorization == expected_authorization
    expected_transport = expected.slice("exposure", "http_method", "http_path", "openapi_operation_id")
    fail_contract("#{operation["id"]} transport drift") unless operation.fetch("transport") == expected_transport
    fail_contract("#{operation["id"]} semantic drift") unless operation.fetch("semantics") == expected.fetch("event_semantics")
  end

  goal_by_unit = goals.to_h { |goal| [goal.fetch("unit"), goal] }
  service_operations = operations.group_by { |operation| [operation.fetch("owner"), operation_service_name(operation)] }
  service_interface_ids = service_operations.to_h do |(owner, name), _owned_operations|
    [[owner, name], "go:#{owner}:#{name}"]
  end
  types = []
  unit_rows = units.map do |unit|
    owner = unit.fetch("unit")
    type_ids = unit.fetch("types").map do |definition|
      id = "go:#{owner}:#{definition.fetch("name")}"
      types << type_entry(owner, "type", definition, id)
      id
    end
    interface_ids = unit.fetch("interfaces").map do |declared|
      name = declared.fetch("name")
      definition = composed_service_interface(owner, name, declared, service_operations.fetch([owner, name], []))
      id = "go:#{owner}:#{definition.fetch("name")}"
      types << type_entry(owner, "interface", definition, id)
      id
    end
    declared_interface_names = unit.fetch("interfaces").map { |definition| definition.fetch("name") }
    service_operations.each do |(service_owner, name), owned_operations|
      next unless service_owner == owner && !declared_interface_names.include?(name)

      id = service_interface_ids.fetch([owner, name])
      types << type_entry(owner, "interface", composed_service_interface(owner, name, nil, owned_operations), id)
      interface_ids << id
    end
    error_ids = unit.fetch("errors").map do |definition|
      id = "go:#{owner}:#{definition.fetch("name")}"
      types << type_entry(owner, "error", definition, id)
      id
    end
    goal = goal_by_unit.fetch(owner)
    {
      "contract_id"=>"contract:unit:#{owner}:v1", "constructors"=>unit.fetch("constructors"), "errors"=>error_ids.sort, "functions"=>unit.fetch("functions", []),
      "goal_sha256"=>goal.fetch("sha256"), "interfaces"=>interface_ids.sort,
      "module_path"=>"github.com/faustbrian/golib/pkg/#{owner}", "operations"=>operations.select { |operation| operation.fetch("owner") == owner }.map { |operation| operation.fetch("id") }.sort,
      "package_name"=>unit.fetch("package_name"), "types"=>type_ids.sort, "unit"=>owner,
      "zero_value_semantics"=>unit.fetch("zero_value_semantics"), "concurrency_ownership"=>unit.fetch("concurrency_ownership")
    }
  end

  unit_names = units.map { |unit| unit.fetch("unit") }.to_set
  service_operations.each do |(owner, name), owned_operations|
    next if unit_names.include?(owner)

    id = service_interface_ids.fetch([owner, name])
    types << type_entry(owner, "interface", composed_service_interface(owner, name, nil, owned_operations), id)
  end

  operation_rows = operations.map do |operation|
    owner = operation.fetch("owner")
    request_id = operation.dig("request", "schema_id")
    result_id = operation.dig("result", "schema_id")
    error_id = operation.dig("errors", "schema_id")
    [["operation_request", operation.fetch("request"), request_id], ["operation_result", operation.fetch("result"), result_id], ["operation_errors", operation.fetch("errors"), error_id]].each do |category, definition, id|
      existing = types.find { |type| type.fetch("id") == id }
      if existing
        same_contract = existing.dig("definition", "name") == definition.fetch("name") && existing.dig("definition", "fields") == definition.fetch("fields", [])
        fail_contract("schema #{id} conflicts with its declared Go type") unless same_contract
      else
        types << type_entry(owner, category, definition, id)
      end
    end
    expected = expected_operations.fetch(operation.fetch("id"))
    semantic_source = expected.slice("owners", "exposure", "access", "authorization", "csrf_origin", "risk_class", "rate_policy", "idempotency", "event_semantics", "http_method", "http_path", "openapi_operation_id")
    {
      "authorization"=>operation.fetch("authorization"), "declared_owners"=>[owner, *operation.fetch("collaborators")],
      "contract_id"=>"contract:operation:#{operation.fetch("id")}:v1", "error_types"=>[error_id], "id"=>operation.fetch("id"), "method"=>{"interface"=>service_interface_ids.fetch([owner, operation_service_name(operation)]), "name"=>operation.fetch("method"), "signature"=>operation.fetch("signature")},
      "owner"=>owner, "request_type"=>request_id, "result_type"=>result_id,
      "semantic_digest"=>"sha256:#{sha(semantic_source)}", "semantics"=>operation.fetch("semantics"), "transport"=>operation.fetch("transport")
    }
  end
  fail_contract("duplicate unit contract ID") unless unit_rows.map { |unit| unit.fetch("contract_id") }.uniq.length == unit_rows.length
  fail_contract("duplicate operation contract ID") unless operation_rows.map { |operation| operation.fetch("contract_id") }.uniq.length == operation_rows.length
  fail_contract("duplicate schema/type ID") unless types.map { |type| type.fetch("id") }.uniq.length == types.length

  external = pinned_external_contracts(meta.fetch("external_contracts"), fragments, units)
  fail_contract("external contract aliases are duplicated") unless external.map { |entry| entry.fetch("alias") }.uniq.length == external.length
  authority_aliases = meta.dig("manifest_schema", "authority_aliases")
  fail_contract("authority aliases are duplicated") unless authority_aliases.map { |entry| entry.fetch("alias") }.uniq.length == authority_aliases.length
  alias_by_name = authority_aliases.to_h { |entry| [entry.fetch("alias"), entry] }
  external.each do |entry|
    declared_alias = alias_by_name.fetch(entry.fetch("alias")) { fail_contract("#{entry["alias"]} lacks authority alias declaration") }
    fail_contract("#{entry["alias"]} authority package path drifted") unless declared_alias.fetch("package_path") == entry.fetch("package_path")
    next if entry.fetch("owner").start_with?("stdlib:", "third-party:")
    status = entry.fetch("current_api_status")
    fail_contract("#{entry["owner"]} has invalid current API status") unless %w[exact requires-extension].include?(status)
    contract_roots = entry.fetch("types", []) + entry.fetch("interfaces", []) + entry.fetch("methods", [])
    fail_contract("#{entry["owner"]} has an empty external contract") if contract_roots.empty?
    fail_contract("#{entry["owner"]} lacks exact API baseline files") if entry.fetch("api_baseline_files").empty?
    fail_contract("#{entry["owner"]} has invalid API baseline digest") unless entry.fetch("api_baseline_sha256").match?(/\Asha256:[0-9a-f]{64}\z/)
    if status == "requires-extension"
      fail_contract("#{entry["owner"]} lacks required contract digest") unless entry.fetch("required_contract_sha256").match?(/\Asha256:[0-9a-f]{64}\z/)
    else
      fail_contract("#{entry["owner"]} exact contract declares a required extension") if entry.key?("required_contract_sha256")
    end
  end
  validate_required_extensions!(meta, external, units, fragments)
  parity_rows = JSON.parse(File.read(PARITY)).fetch("dispositions")
  exclusion_by_id = meta.fetch("product_exclusions").to_h { |entry| [entry.fetch("id"), entry] }
  fail_contract("product-exclusion inventory differs from PARITY_DISPOSITIONS") unless exclusion_by_id.keys.sort == parity_rows.map { |row| row.fetch("id") }.sort
  exclusions = parity_rows.map do |row|
    declaration = exclusion_by_id.fetch(row.fetch("id"))
    fail_contract("#{row["id"]} disposition drift") unless declaration.fetch("disposition") == row.fetch("kind")
    declaration.merge("artifact"=>row.fetch("artifact"), "owner"=>row.fetch("owner"), "semantic_digest"=>row.fetch("semantic_digest"))
  end

  {
    "external_contracts"=>external.sort_by { |entry| entry.fetch("owner") },
    "manifest_schema"=>meta.fetch("manifest_schema"),
    "operations"=>operation_rows.sort_by { |operation| operation.fetch("id") },
    "product_exclusions"=>exclusions.sort_by { |entry| entry.fetch("id") },
    "schema_version"=>2,
    "source_digests"=>{
      "API_OPERATIONS.md"=>"sha256:#{Digest::SHA256.file(API).hexdigest}",
      "GOAL_MANIFEST.json"=>"sha256:#{Digest::SHA256.file(GOALS).hexdigest}",
      "OPERATION_SEMANTICS.json"=>"sha256:#{Digest::SHA256.file(OPERATIONS).hexdigest}",
      "PARITY_DISPOSITIONS.json"=>"sha256:#{Digest::SHA256.file(PARITY).hexdigest}",
      "public_contracts.rb"=>"sha256:#{Digest::SHA256.file(__FILE__).hexdigest}",
      "public_contracts_meta.json"=>"sha256:#{Digest::SHA256.file(META).hexdigest}",
      "fragments"=>FAMILIES.to_h { |path| [File.basename(path), "sha256:#{Digest::SHA256.file(path).hexdigest}"] }
    },
    "types"=>types.sort_by { |type| type.fetch("id") },
    "units"=>unit_rows,
    "validation_rules"=>meta.fetch("validation_rules")
  }
end

if ARGV.first == "--fragment-fixture"
  fail_contract("usage: public_contracts.rb --fragment-fixture PATH") unless ARGV.length == 2
  fixture_path = ARGV.fetch(1)
  fragment = parse_fragment!(fixture_path)
  if File.basename(fixture_path) == File.basename(META)
    validate_meta_fragment!(fragment)
  else
    meta = parse_fragment!(META)
    validate_fragment!(fixture_path, fragment, meta.dig("manifest_schema", "type_kinds"))
  end
  puts "PUBLIC_CONTRACTS fragment: valid and canonical"
  exit 0
end

expected = encoded(build)
if ARGV == ["--check"]
  fail_contract("PUBLIC_CONTRACTS.json missing") unless File.file?(OUTPUT)
  fail_contract("PUBLIC_CONTRACTS.json stale or noncanonical") unless File.binread(OUTPUT) == expected
  parsed = JSON.parse(expected)
  fail_contract("top-level schema drift") unless parsed.keys.sort == parsed.dig("manifest_schema", "allowed_top_level_keys").sort
  puts "PUBLIC_CONTRACTS.json: #{parsed["units"].length} units, #{parsed["operations"].length} operations, #{parsed["types"].length} exact Go/schema contracts"
else
  $stdout.write(expected)
end
