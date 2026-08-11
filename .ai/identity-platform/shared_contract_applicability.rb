# frozen_string_literal: true

require "json"
require "set"
require "digest"

module SharedContractApplicability
  FILE = "SHARED_CONTRACT_APPLICABILITY.json"
  KEYS = %w[transaction lifecycle lifecycle_consumers configuration security_events].freeze
  SOURCES = {
    "transaction" => "TRANSACTION_CONTRACT.md", "lifecycle" => "LIFECYCLE_CASCADES.md",
    "lifecycle_consumers" => "LIFECYCLE_CONSUMERS.md", "configuration" => "REFERENCE_CONFIGURATION.md",
    "security_events" => "SECURITY_EVENTS.md"
  }.freeze
  CACHE_CONSUMER_PARENTS = {
    "identity/session/valkey" => "identity/session",
    "identity/apikey/valkey" => "identity/apikey"
  }.freeze
  CHECKPOINT_PARTICIPANT_PARENTS = {
    "identity/risk/valkey" => "identity/risk"
  }.freeze

  module_function

  def load_and_validate!(root:, units:)
    path = File.join(root, FILE)
    fail_contract("missing canonical manifest #{FILE}") unless File.file?(path)
    document = JSON.parse(File.read(path))
    fail_contract("#{FILE} is not canonical JSON") unless File.read(path) == JSON.pretty_generate(document) + "\n"
    fail_contract("manifest top-level keys must be version and units") unless document.keys == %w[version units]
    fail_contract("manifest version must be 1") unless document["version"] == 1
    selections = document["units"]
    fail_contract("manifest units must be an object") unless selections.is_a?(Hash)
    expected_units = units.to_set
    actual_units = selections.keys.to_set
    unless actual_units == expected_units
      fail_contract("unit drift: missing=#{(expected_units - actual_units).to_a.sort} extra=#{(actual_units - expected_units).to_a.sort}")
    end
    fail_contract("manifest unit order must match inventory") unless selections.keys == units

    configuration_catalogs = load_configuration_catalogs!(root)
    known_catalogs = catalogs(root)
    selections.each do |unit, entry|
      fail_contract("#{unit} selectors must be an object") unless entry.is_a?(Hash)
      actual_keys = entry.keys.to_set
      expected_keys = KEYS.to_set
      unless actual_keys == expected_keys
        fail_contract("#{unit} selector drift: missing=#{(expected_keys - actual_keys).to_a.sort} extra=#{(actual_keys - expected_keys).to_a.sort}")
      end
      fail_contract("#{unit} selector order is not canonical") unless entry.keys == KEYS
      KEYS.each do |key|
        values = entry[key]
        valid_array = values.is_a?(Array) && !values.empty? && values.all? { |value| value.is_a?(String) && !value.empty? }
        fail_contract("#{unit} #{key} must be a non-empty string array") unless valid_array
        fail_contract("#{unit} #{key} is not sorted bytewise") unless values == values.sort
        fail_contract("#{unit} #{key} contains duplicates") unless values.uniq == values
        fail_contract("#{unit} #{key} mixes none with selected IDs") if values.include?("none") && values != ["none"]
        unknown = values.reject { |value| value == "none" || known_catalogs.fetch(key).key?(value) }
        fail_contract("#{unit} #{key} contains stale or unknown IDs: #{unknown.join(', ')}") unless unknown.empty?
      end
      templates = entry.fetch("configuration").grep(/<id>/)
      expansions = templates.flat_map do |template|
        catalog_instance_ids(unit: unit, template: template, configuration_catalogs: configuration_catalogs)
      end
      fail_contract("#{unit} catalog instance expansions collide") unless expansions == expansions.uniq
      expansions.each do |instance_id|
        fail_contract("#{unit} generated invalid catalog instance ID #{instance_id}") unless instance_id.match?(/\Aref\.[a-z0-9-]+\.[a-z0-9_.-]+\z/)
      end
      transaction = entry.fetch("transaction")
      fail_contract("#{unit} transaction selection omits tx.foundation") if transaction != ["none"] && !transaction.include?("tx.foundation")
      lifecycle = entry.fetch("lifecycle")
      fail_contract("#{unit} lifecycle selection omits lifecycle.foundation") if lifecycle != ["none"] && !lifecycle.include?("lifecycle.foundation")
      missing_cascades = (entry.fetch("lifecycle_consumers") - ["none"]) - lifecycle
      fail_contract("#{unit} lifecycle omits selected consumer rows: #{missing_cascades.join(', ')}") unless missing_cascades.empty?
    end
    KEYS.each do |key|
      selected = selections.values.flat_map { |entry| entry.fetch(key) }.to_set
      omitted = known_catalogs.fetch(key).keys.to_set - selected
      fail_contract("#{key} catalog rows have no applicable unit: #{omitted.to_a.sort.join(', ')}") unless omitted.empty?
    end
    validate_consumer_bijection!(root, selections, expected_units)
    document
  rescue JSON::ParserError => e
    fail_contract("#{FILE} is invalid JSON: #{e.message}")
  end

  def load_configuration_catalogs!(root)
    path = if ENV.key?("IDENTITY_PLATFORM_CONFIGURATION_CATALOGS_FIXTURE")
             File.expand_path(ENV.fetch("IDENTITY_PLATFORM_CONFIGURATION_CATALOGS_FIXTURE"))
           else
             File.join(root, "CONFIGURATION_CATALOGS.json")
           end
    fail_contract("missing CONFIGURATION_CATALOGS.json") unless File.file?(path)
    document = JSON.parse(File.read(path))
    fail_contract("CONFIGURATION_CATALOGS.json is not canonical JSON") unless File.read(path) == JSON.pretty_generate(document) + "\n"
    fail_contract("configuration catalog top-level keys drifted") unless document.keys == %w[schema_version providers captcha]
    fail_contract("configuration catalog schema version drifted") unless document.fetch("schema_version") == 1
    {"providers" => "provider-catalog-v1", "captcha" => "captcha-catalog-v1"}.each do |name, version|
      catalog = document.fetch(name)
      fail_contract("#{name} catalog keys drifted") unless catalog.keys == %w[version ids sha256]
      ids = catalog.fetch("ids")
      fail_contract("#{name} catalog version drifted") unless catalog.fetch("version") == version
      fail_contract("#{name} catalog IDs must be sorted and unique") unless ids == ids.sort.uniq
      fail_contract("#{name} catalog contains invalid IDs") unless ids.all? { |id| id.match?(/\A[a-z0-9]+(?:-[a-z0-9]+)*\z/) }
      digest = Digest::SHA256.hexdigest(ids.join("\n") + "\n")
      fail_contract("#{name} catalog checksum drifted") unless catalog.fetch("sha256") == digest
    end
    document
  rescue JSON::ParserError => e
    fail_contract("CONFIGURATION_CATALOGS.json is invalid JSON: #{e.message}")
  end

  def canonical?(root:, units:)
    document = load_and_validate!(root: root, units: units)
    File.read(File.join(root, FILE)) == JSON.pretty_generate(document) + "\n"
  end

  def render(document:, root:, unit:)
    entry = document.fetch("units").fetch(unit) { fail_contract("unknown unit #{unit}") }
    known_catalogs = catalogs(root)
    configuration_catalogs = load_configuration_catalogs!(root)
    lines = ["## Exact shared-contract applicability", ""]
    KEYS.each do |key|
      lines << "### #{key.tr('_', ' ')}"
      entry.fetch(key).each do |id|
        if id == "none"
          lines << "- `none`"
        else
          source = known_catalogs.fetch(key).fetch(id)
          lines << "- `#{id}` — `#{SOURCES.fetch(key)}:#{source.fetch(:line)}` — #{source.fetch(:text)}"
          if key == "configuration" && id.include?("<id>")
            catalog_instance_ids(unit: unit, template: id, configuration_catalogs: configuration_catalogs).each do |instance_id|
              lines << "  - `#{instance_id}` — generated catalog instance of `#{id}`"
            end
          end
        end
      end
      lines << ""
    end
    lines.join("\n")
  end

  def catalog_instance_ids(unit:, template:, configuration_catalogs:)
    catalog_name = template.start_with?("ref.providers.") ? "providers" : "captcha"
    catalog = configuration_catalogs.fetch(catalog_name)
    ids = catalog.fetch("ids")
    if catalog_name == "captcha" && unit.start_with?("identity/risk/captcha/")
      adapter_id = unit.delete_prefix("identity/risk/captcha/")
      ids = [adapter_id] if ids.include?(adapter_id)
    end
    ids.map do |id|
      expanded_path = template.sub("<id>", id).delete_prefix("ref.")
      "ref.#{catalog.fetch('version')}.#{expanded_path}"
    end
  end

  def catalogs(root)
    @catalogs ||= {}
    @catalogs[root] ||= {
      "transaction" => backtick_catalog(File.join(root, SOURCES.fetch("transaction")), /\Atx\.[a-z0-9_.]+\z/),
      "lifecycle" => lifecycle_catalog(File.join(root, SOURCES.fetch("lifecycle"))),
      "lifecycle_consumers" => table_catalog(File.join(root, SOURCES.fetch("lifecycle_consumers")), "lifecycle.cascade."),
      "configuration" => configuration_catalog(File.join(root, SOURCES.fetch("configuration"))),
      "security_events" => security_event_catalog(File.join(root, SOURCES.fetch("security_events")))
    }
  end

  def backtick_catalog(path, pattern)
    result = {}
    File.readlines(path).each_with_index do |line, index|
      line.scan(/`([^`]+)`/).flatten.grep(pattern).each { |id| result[id] ||= { line: index + 1, text: line.strip } }
    end
    result
  end

  def table_catalog(path, prefix)
    result = {}
    File.readlines(path).each_with_index do |line, index|
      next unless line.start_with?("| `#{prefix}")
      result[line[/\| `([^`]+)`/, 1]] = { line: index + 1, text: line.strip }
    end
    result
  end

  def lifecycle_catalog(path)
    result = {}
    File.readlines(path).each_with_index do |line, index|
      id = if line.include?("`lifecycle.foundation`")
        "lifecycle.foundation"
      elsif line.start_with?("| `lifecycle.")
        line[/\| `([^`]+)`/, 1]
      end
      result[id] ||= { line: index + 1, text: line.strip } if id
    end
    result
  end

  def configuration_catalog(path)
    result = {}
    File.readlines(path).each_with_index do |line, index|
      next unless line.start_with?("| `")
      result["ref.#{line[/\| `([^`]+)`/, 1]}"] = { line: index + 1, text: line.strip }
    end
    result
  end

  def security_event_catalog(path)
    result = {}
    lines = File.readlines(path)
    start = lines.index { |line| line == "## Stable event taxonomy\n" }
    fail_contract("SECURITY_EVENTS.md lacks stable event taxonomy") unless start
    lines[(start + 1)..].each_with_index do |line, offset|
      break if line.start_with?("## ")
      next unless line.start_with?("|")
      line.scan(/`(identity(?:\.[a-z0-9_]+)+)`/).flatten.each do |id|
        result[id] = { line: start + offset + 2, text: line.strip }
      end
    end
    result
  end

  def validate_consumer_bijection!(root, selections, known_units)
    expected = Hash.new { |hash, key| hash[key] = Set.new }
    declared = Hash.new { |hash, key| hash[key] = Set.new }
    File.readlines(File.join(root, SOURCES.fetch("lifecycle_consumers"))).each do |line|
      next unless line.start_with?("| `lifecycle.cascade.")
      cells = line.split("|").map(&:strip)
      cascade = cells[1].delete("`")
      [cells[2], cells[3]].join(" ").scan(/`([^`]+)`/).flatten.each do |unit|
        declared[unit] << cascade
        expected[unit] << cascade if known_units.include?(unit)
      end
    end
    selections.each do |unit, entry|
      actual = (entry.fetch("lifecycle_consumers") - ["none"]).to_set
      next if actual == expected[unit]
      fail_contract("#{unit} lifecycle consumer drift: missing=#{(expected[unit] - actual).to_a.sort} extra=#{(actual - expected[unit]).to_a.sort}")
    end
    CACHE_CONSUMER_PARENTS.each do |cache_unit, semantic_unit|
      cache_rows = (selections.fetch(cache_unit).fetch("lifecycle_consumers") - ["none"]).to_set
      semantic_rows = (selections.fetch(semantic_unit).fetch("lifecycle_consumers") - ["none"]).to_set
      next if cache_rows == semantic_rows
      fail_contract("#{cache_unit} must checkpoint every #{semantic_unit} lifecycle row: missing=#{(semantic_rows - cache_rows).to_a.sort} extra=#{(cache_rows - semantic_rows).to_a.sort}")
    end
    CHECKPOINT_PARTICIPANT_PARENTS.each do |adapter_unit, semantic_unit|
      adapter_rows = selections.fetch(adapter_unit).fetch("lifecycle").grep(/\Alifecycle\.cascade\./).to_set
      semantic_rows = (selections.fetch(semantic_unit).fetch("lifecycle_consumers") - ["none"]).to_set
      next if adapter_rows == semantic_rows
      fail_contract("#{adapter_unit} must persist every #{semantic_unit} subordinate checkpoint: missing=#{(semantic_rows - adapter_rows).to_a.sort} extra=#{(adapter_rows - semantic_rows).to_a.sort}")
    end
    unless declared["authorization/valkey"] == declared["authorization"]
      fail_contract("authorization/valkey must checkpoint every authorization lifecycle row")
    end
    capability_cleanup = declared["capability/postgres"]
    required_capability_cleanup = Set[
      "lifecycle.cascade.password_reset", "lifecycle.cascade.password_compromise"
    ]
    unless capability_cleanup == required_capability_cleanup
      fail_contract("capability/postgres lifecycle cleanup must be exactly password reset and compromise")
    end

    global_line = File.readlines(File.join(root, SOURCES.fetch("lifecycle_consumers"))).find do |line|
      line.start_with?("| `lifecycle.cascade.global_compromise`")
    end
    global_owner = global_line&.split("|")&.map(&:strip)&.fetch(2, nil)
    fail_contract("global compromise must be owned by identity") unless global_owner == "`identity`"
  end

  def fail_contract(message)
    raise ArgumentError, "shared-contract applicability: #{message}"
  end
end
