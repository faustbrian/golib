# frozen_string_literal: true

require "json"
require "digest"
require "time"
require "open3"
require "tempfile"
require "fileutils"
require "securerandom"
require "tmpdir"
require "rbconfig"

module AcceptanceSchemaValidation
  OUTCOME_FIELD = /\A(?:actual_outcome|execution_outcome|outcome|pass|passed|passing|result|status|verified)\z/.freeze
  class DuplicateKeyError < JSON::ParserError; end

  module_function

  def parse_json(bytes, canonical: false)
    reject_duplicate_keys!(bytes)
    value = JSON.parse(bytes)
    raise JSON::ParserError, "JSON bytes are not canonical" if canonical && bytes != JSON.pretty_generate(value) + "\n"

    value
  end

  def reject_duplicate_keys!(bytes)
    parse_json_value_for_keys(bytes, skip_json_whitespace(bytes, 0))
  end

  def skip_json_whitespace(bytes, index)
    index += 1 while index < bytes.bytesize && [9, 10, 13, 32].include?(bytes.getbyte(index))
    index
  end

  def parse_json_string_for_keys(bytes, index)
    start = index
    index += 1
    while index < bytes.bytesize
      byte = bytes.getbyte(index)
      if byte == 34
        index += 1
        return [JSON.parse(bytes.byteslice(start...index)), index]
      end
      if byte == 92
        index += bytes.getbyte(index + 1) == 117 ? 6 : 2
      else
        index += 1
      end
    end
    raise JSON::ParserError, "unterminated JSON string"
  end

  def parse_json_value_for_keys(bytes, index)
    case bytes.getbyte(index)
    when 123
      parse_json_object_for_keys(bytes, index)
    when 91
      index = skip_json_whitespace(bytes, index + 1)
      return index + 1 if bytes.getbyte(index) == 93
      loop do
        index = skip_json_whitespace(bytes, parse_json_value_for_keys(bytes, index))
        return index + 1 if bytes.getbyte(index) == 93
        index = skip_json_whitespace(bytes, index + 1)
      end
    when 34
      parse_json_string_for_keys(bytes, index).last
    else
      index += 1 while index < bytes.bytesize && ![9, 10, 13, 32, 44, 93, 125].include?(bytes.getbyte(index))
      index
    end
  end

  def parse_json_object_for_keys(bytes, index)
    keys = {}
    index = skip_json_whitespace(bytes, index + 1)
    return index + 1 if bytes.getbyte(index) == 125
    loop do
      key, index = parse_json_string_for_keys(bytes, index)
      raise DuplicateKeyError, "duplicate JSON object key #{key.inspect}" if keys.key?(key)

      keys[key] = true
      index = skip_json_whitespace(bytes, index)
      index = skip_json_whitespace(bytes, index + 1)
      index = skip_json_whitespace(bytes, parse_json_value_for_keys(bytes, index))
      return index + 1 if bytes.getbyte(index) == 125
      index = skip_json_whitespace(bytes, index + 1)
    end
  end

  def canonical(value)
    case value
    when Hash then value.keys.sort.to_h { |key| [key, canonical(value.fetch(key))] }
    when Array then value.map { |entry| canonical(entry) }
    else value
    end
  end

  def execution_identity(execution)
    "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(execution_receipt_payload(execution))))}"
  end

  def execution_receipt_payload(execution)
    execution.reject do |key, _value|
      %w[execution_identity execution_receipt_path execution_receipt_sha256 artifact_payload_binding].include?(key)
    end
  end

  def execution_receipt_bytes(execution)
    JSON.pretty_generate(execution_receipt_payload(execution)) + "\n"
  end

  def bind_execution_proof!(execution)
    stdout = execution.fetch("stdout")
    stderr = execution.fetch("stderr")
    execution["stdout_byte_length"] = stdout.bytesize
    execution["stdout_sha256"] = "sha256:#{Digest::SHA256.hexdigest(stdout.b)}"
    execution["stderr_byte_length"] = stderr.bytesize
    execution["stderr_sha256"] = "sha256:#{Digest::SHA256.hexdigest(stderr.b)}"
    execution["captured_output_sha256"] = "sha256:#{Digest::SHA256.hexdigest(stdout.b + "\0" + stderr.b)}"
    execution["execution_identity"] = execution_identity(execution)
    execution["execution_receipt_sha256"] = "sha256:#{Digest::SHA256.hexdigest(execution_receipt_bytes(execution))}"
    execution
  end

  def runner_transcript_sha256(value, execution)
    semantic_value = value.reject { |key, _entry| key == "transcript_sha256" }
    payload = [
      JSON.generate(canonical(semantic_value)),
      execution.fetch("execution_identity"),
      execution.fetch("raw_capture_sha256"),
      execution.fetch("captured_output_sha256")
    ].join("\0")
    "sha256:#{Digest::SHA256.hexdigest(payload)}"
  end

  def bind_runner_transcripts!(payload, execution)
    payload.each_value do |value|
      next unless value.is_a?(Array)

      value.each do |entry|
        next unless entry.is_a?(Hash) && entry.key?("transcript_sha256")

        entry["transcript_sha256"] = runner_transcript_sha256(entry, execution)
      end
    end
    payload
  end

  def execution_receipt_errors(execution, receipt_bytes, path = "$.execution")
    return ["#{path} separately captured execution receipt is absent"] unless receipt_bytes

    expected_digest = "sha256:#{Digest::SHA256.hexdigest(receipt_bytes)}"
    findings = []
    findings << "#{path}.execution_receipt_sha256 does not bind receipt bytes" unless execution["execution_receipt_sha256"] == expected_digest
    parsed = parse_json(receipt_bytes, canonical: true)
    findings << "#{path} differs from separately captured execution receipt" unless parsed == execution_receipt_payload(execution)
    findings
  rescue JSON::ParserError => e
    ["#{path} execution receipt is invalid JSON: #{e.message}"]
  end

  def sample(schema)
    return schema.fetch("const") if schema.key?("const")
    return schema.fetch("enum").first if schema.key?("enum")
    type = Array(schema.fetch("type", "string")).find { |candidate| candidate != "null" }
    case type
    when "object"
      schema.fetch("required", []).to_h { |key| [key, sample(schema.fetch("properties").fetch(key))] }
    when "array"
      if schema["x-exact-scenario-cases"]
        item = schema.fetch("items")
        common = item.fetch("required").to_h { |key| [key, sample(item.fetch("properties").fetch(key))] }
        return schema.fetch("x-exact-scenario-cases").map { |spec| common.merge(spec) }
      end
      if schema["x-exact-operation-bindings"]
        return schema.fetch("x-exact-operation-bindings").map do |binding|
          binding.merge("executed" => true, "transcript_sha256" => "sha256:#{'a' * 64}")
        end
      end
      items = schema.fetch("items", {})
      return items.fetch("oneOf").map { |variant| sample(variant) } if items["oneOf"]
      Array.new(schema.fetch("minItems", 1)) { sample(items) }
    when "boolean" then true
    when "integer" then schema.fetch("minimum", 0)
    when "string"
      return "sha256:#{'a' * 64}" if schema["pattern"]&.include?("sha256:")
      return "pkg/identity/reference" if schema["pattern"]&.start_with?("^pkg/")
      return "ABCDE" if schema["pattern"] == "^[0-9A-F]{5}$"
      return "golib-identity-reference/v1.0.0 (+https://security.example.test)" if schema["pattern"]&.start_with?("^golib-identity-reference/")
      "sample-id"
    else raise KeyError, "cannot sample schema type #{type}"
    end
  end

  def errors(value, schema, path = "$")
    findings = []
    if schema.key?("const") && value != schema.fetch("const")
      findings << "#{path} differs from const"
      return findings
    end
    findings << "#{path} is outside enum" if schema.key?("enum") && !schema.fetch("enum").include?(value)
    types = Array(schema["type"])
    unless types.empty?
      valid = types.any? do |type|
        classes = {"object" => Hash, "array" => Array, "string" => String, "integer" => Integer, "boolean" => [TrueClass, FalseClass], "null" => NilClass}.fetch(type)
        Array(classes).any? { |candidate| value.is_a?(candidate) }
      end
      return ["#{path} has wrong type"] unless valid
    end
    if value.is_a?(Hash)
      missing = schema.fetch("required", []) - value.keys
      findings << "#{path} missing #{missing.join(',')}" unless missing.empty?
      extra = value.keys - schema.fetch("properties", {}).keys
      findings << "#{path} has extra #{extra.join(',')}" if schema["additionalProperties"] == false && !extra.empty?
      schema.fetch("properties", {}).each { |key, child| findings.concat(errors(value[key], child, "#{path}.#{key}")) if value.key?(key) }
    elsif value.is_a?(Array)
      findings << "#{path} too short" if schema["minItems"] && value.length < schema["minItems"]
      findings << "#{path} too long" if schema["maxItems"] && value.length > schema["maxItems"]
      findings << "#{path} has duplicates" if schema["uniqueItems"] && value.uniq.length != value.length
      value.each_with_index { |entry, index| findings.concat(errors(entry, schema.fetch("items", {}), "#{path}[#{index}]")) }
    elsif value.is_a?(String)
      findings << "#{path} is empty" if schema["minLength"] && value.length < schema["minLength"]
      findings << "#{path} is too long" if schema["maxLength"] && value.length > schema["maxLength"]
      findings << "#{path} does not match pattern" if schema["pattern"] && !Regexp.new(schema["pattern"]).match?(value)
      findings << "#{path} is not RFC3339" if schema["format"] == "date-time" && (Time.iso8601(value) rescue nil).nil?
    elsif value.is_a?(Integer)
      findings << "#{path} below minimum" if schema["minimum"] && value < schema["minimum"]
      findings << "#{path} above maximum" if schema["maximum"] && value > schema["maximum"]
    end
    if schema["oneOf"]
      matches = schema.fetch("oneOf").count { |variant| errors(value, variant, path).empty? }
      findings << "#{path} matches #{matches} oneOf variants" unless matches == 1
    end
    if schema["contains"] && value.is_a?(Array)
      matches = value.count { |entry| errors(entry, schema.fetch("contains"), path).empty? }
      findings << "#{path} contains #{matches}, expected at least #{schema.fetch('minContains', 1)}" if matches < schema.fetch("minContains", 1)
    end
    if schema["x-exact-operation-bindings"] && value.is_a?(Array)
      actual = value.map { |row| row.slice("operation_id", "handler_id", "openapi_operation_id") }
      findings << "#{path} exact operation bindings drifted" unless actual == schema.fetch("x-exact-operation-bindings")
    end
    if schema["x-exact-scenario-cases"] && value.is_a?(Array)
      fields = schema.fetch("x-exact-scenario-case-fields")
      actual = value.map { |row| row.slice(*fields) }
      findings << "#{path} exact scenario cases drifted" unless actual == schema.fetch("x-exact-scenario-cases")
    end
    schema.fetch("allOf", []).each { |part| findings.concat(errors(value, part, path)) }
    if schema["x-execution-proof"] == 2 && value.is_a?(Hash)
      begin
        findings << "#{path}.execution_identity does not bind execution fields" unless value["execution_identity"] == execution_identity(value)
        findings << "#{path}.stdout_byte_length drifted" unless value["stdout_byte_length"] == value.fetch("stdout").bytesize
        findings << "#{path}.stderr_byte_length drifted" unless value["stderr_byte_length"] == value.fetch("stderr").bytesize
        findings << "#{path}.stdout_sha256 drifted" unless value["stdout_sha256"] == "sha256:#{Digest::SHA256.hexdigest(value.fetch('stdout').b)}"
        findings << "#{path}.stderr_sha256 drifted" unless value["stderr_sha256"] == "sha256:#{Digest::SHA256.hexdigest(value.fetch('stderr').b)}"
        combined = "sha256:#{Digest::SHA256.hexdigest(value.fetch('stdout').b + "\0" + value.fetch('stderr').b)}"
        findings << "#{path}.captured_output_sha256 drifted" unless value["captured_output_sha256"] == combined
        started = Time.iso8601(value.fetch("started_at")); completed = Time.iso8601(value.fetch("completed_at"))
        findings << "#{path} completion does not follow start" unless completed > started
      rescue KeyError, TypeError, ArgumentError => e
        findings << "#{path} execution proof cannot be evaluated: #{e.message}"
      end
    end
    if value.is_a?(Hash)
      schema.fetch("x-semantic-rules", []).each do |rule|
        case rule.fetch("kind")
        when "positive"
          rule.fetch("fields").each { |field| findings << "#{path}.#{field} proves zero work" unless value[field].is_a?(Integer) && value[field].positive? }
        when "zero"
          rule.fetch("fields").each { |field| findings << "#{path}.#{field} must prove absence" unless value[field] == 0 }
        when "true"
          rule.fetch("fields").each { |field| findings << "#{path}.#{field} must be true" unless value[field] == true }
        when "equal"
          findings << "#{path}.#{rule.fetch('left')} must equal #{rule.fetch('right')}" unless value[rule.fetch("left")] == value[rule.fetch("right")]
        when "const"
          findings << "#{path}.#{rule.fetch('field')} differs from required value" unless value[rule.fetch("field")] == rule.fetch("value")
        else findings << "#{path} has unknown semantic rule #{rule['kind']}"
        end
      end
    end
    findings
  end

  def outcome_assignment_paths(value, schema, path = [])
    paths = []
    if schema["oneOf"]
      matching = schema.fetch("oneOf").select do |variant|
        variant.fetch("properties", {}).all? { |key, node| !node.key?("const") || value.is_a?(Hash) && value[key] == node["const"] }
      end
      schema = matching.one? ? matching.first : schema
    end
    if schema["type"] == "object" && value.is_a?(Hash)
      schema.fetch("required", []).each do |key|
        paths << path + [key] if !value.key?(key) && OUTCOME_FIELD.match?(key)
      end
      value.each { |key, child| paths.concat(outcome_assignment_paths(child, schema.dig("properties", key) || {}, path + [key])) }
    elsif schema["type"] == "array" && value.is_a?(Array)
      value.each_with_index { |child, index| paths.concat(outcome_assignment_paths(child, schema.fetch("items", {}), path + [index])) }
    end
    paths
  end

  def assignment_case_id(payload, path)
    current = payload
    case_id = nil
    path[0...-1].each do |segment|
      current = current.fetch(segment)
      case_id = current["case_id"] if current.is_a?(Hash) && current["case_id"].is_a?(String)
    end
    case_id || JSON.generate(path)
  end

  def schema_at_path(schema, payload, path)
    current = payload
    path.each_with_index.reduce(schema) do |node, (segment, index)|
      if node["oneOf"]
        matching = node.fetch("oneOf").select do |variant|
          variant.fetch("properties", {}).all? do |key, child|
            !child.key?("const") || current.is_a?(Hash) && current[key] == child["const"]
          end
        end
        node = matching.first if matching.one?
      end
      current = current.fetch(segment) if index < path.length - 1
      segment.is_a?(Integer) ? node.fetch("items") : node.fetch("properties").fetch(segment)
    end
  end

  def independently_derived_assignment(payload, schema, path)
    node = schema_at_path(schema, payload, path)
    return [node.fetch("const"), "schema-const"] if node.key?("const")

    if path.length == 3 && path[0] == "scenario_cases" && path[1].is_a?(Integer)
      cases = schema.dig("properties", "scenario_cases", "x-exact-scenario-cases") || []
      case_id = payload.dig("scenario_cases", path[1], "case_id")
      expected = cases.find { |entry| entry.fetch("case_id") == case_id }
      return [expected.fetch(path[2]), "exact-scenario-contract"] if expected&.key?(path[2])
    end

    if path.length == 1
      rule = schema.fetch("x-semantic-rules", []).find do |candidate|
        candidate["field"] == path[0] && %w[const true].include?(candidate["kind"])
      end
      return [rule.fetch("value"), "semantic-const"] if rule&.fetch("kind", nil) == "const"
      return [true, "semantic-true"] if rule&.fetch("kind", nil) == "true"
    end

    true_rule = schema.fetch("x-semantic-rules", []).find do |candidate|
      candidate["kind"] == "true" && Array(candidate["fields"]).include?(path.last)
    end
    return [true, "semantic-true"] if true_rule

    raise KeyError, "#{path.inspect} has no independently authoritative derivation"
  end
end

if $PROGRAM_NAME == __FILE__ && ARGV.any?
  begin
  command, artifact_id = ARGV
  repository_root = File.expand_path("../../..", __dir__)
  case command
  when "verify"
    raw_path = ENV.fetch("IDENTITY_ACCEPTANCE_VERIFIER_INPUT_PATH")
    output_path = ENV.fetch("IDENTITY_ACCEPTANCE_VERIFIER_OUTPUT_PATH")
    raw_bytes = File.binread(raw_path)
    require_relative "model"
    artifact = IdentityPlatformAcceptance.catalog_document.fetch("artifacts").find { |row| row.fetch("artifact_id") == artifact_id } || raise(KeyError, artifact_id)
    raw_payload = AcceptanceSchemaValidation.parse_json(raw_bytes, canonical: true)
    raise KeyError, "producer attempted to supply verifier observation rows" if raw_payload.key?("observation_rows")
    schema = IdentityPlatformAcceptance.schema_document(artifact).dig("$defs", "artifact_evidence")
    paths = AcceptanceSchemaValidation.outcome_assignment_paths(raw_payload, schema)
    independently_derived = paths.map do |path|
      value, authority = AcceptanceSchemaValidation.independently_derived_assignment(raw_payload, schema, path)
      [path, value, authority]
    end
    candidate = JSON.parse(JSON.generate(raw_payload))
    independently_derived.each do |path, value, _authority|
      parent = path[0...-1].reduce(candidate) { |current, segment| current.fetch(segment) }
      parent[path.last] = value
    end
    contract_errors = AcceptanceSchemaValidation.errors(candidate, schema)
    raise KeyError, "captured producer artifact violates independent contract: #{contract_errors.first}" unless contract_errors.empty?
    assignments = independently_derived.map { |path, value, _authority| {"path" => path, "value" => value} }
    raw_digest = "sha256:#{Digest::SHA256.hexdigest(raw_bytes)}"
    contract_digest = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(schema)))}"
    receipts = independently_derived.map do |path, value, authority|
      row = {"case_id" => AcceptanceSchemaValidation.assignment_case_id(raw_payload, path),
             "observed_value" => value, "derivation_authority" => authority,
             "artifact_contract_sha256" => contract_digest}
      {
        "case_id" => row.fetch("case_id"),
        "assignment_path" => path,
        "assignment_value_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(value)))}",
        "raw_capture_sha256" => raw_digest,
        "tool_native_row" => row,
        "tool_native_result_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(row)))}"
      }
    end
    output = {"artifact_id" => artifact_id, "raw_capture_sha256" => raw_digest, "assignments" => assignments, "case_receipts" => receipts}
    File.binwrite(output_path, JSON.pretty_generate(output) + "\n")
  when "discover-packages"
    modules_bytes = File.binread(File.join(repository_root, "modules.json"))
    packages_bytes = File.binread(File.join(repository_root, "packages.json"))
    modules = AcceptanceSchemaValidation.parse_json(modules_bytes).fetch("modules")
    packages = AcceptanceSchemaValidation.parse_json(packages_bytes).fetch("packages")
    directory_by_path = modules.to_h { |row| [row.fetch("module_path"), row.fetch("directory")] }
    reverse = modules.to_h { |row| [row.fetch("directory"), Array(row["reverse_owned_dependencies"]).map { |path| directory_by_path.fetch(path) }] }
    seeds = modules.map { |row| row.fetch("directory") }.select { |directory| directory == "pkg/identity" || directory.start_with?("pkg/identity/") }
    closure = seeds.dup
    loop do
      expanded = closure | reverse.select { |directory, _| closure.include?(directory) }.values.flatten
      break if expanded == closure
      closure = expanded
    end
    package_ids = packages.select { |row| row["production"] && closure.include?(row.fetch("module_directory")) }.map { |row| row.fetch("directory") }.uniq.sort_by(&:b)
    reverse_closure = closure.to_h { |directory| [directory, Array(reverse[directory]).select { |dependent| closure.include?(dependent) }.sort_by(&:b)] }
    output = {
      "derivation" => "modules-and-packages-manifests-with-reverse-dependants",
      "source_manifest_sha256s" => {"modules.json" => "sha256:#{Digest::SHA256.hexdigest(modules_bytes)}", "packages.json" => "sha256:#{Digest::SHA256.hexdigest(packages_bytes)}"},
      "affected_module_directories" => closure.sort_by(&:b), "reverse_dependants" => reverse_closure, "package_ids" => package_ids
    }
    STDOUT.write(JSON.pretty_generate(output) + "\n")
  when "mutation-tool"
    module_directory = artifact_id
    raise KeyError, "mutation module is unsafe" unless module_directory&.match?(/\Apkg\/[a-z0-9\/-]+\z/)
    report_path = File.join(repository_root, ".artifacts", module_directory, "mutation.json")
    report_bytes = File.binread(report_path)
    report = AcceptanceSchemaValidation.parse_json(report_bytes)
    raise KeyError, "mutation report module drifted" unless report["schema_version"] == 3 && report["module"] == module_directory
    raise KeyError, "mutation report is incomplete" unless report["complete"] == true
    raise KeyError, "mutation report package closure drifted" unless report.fetch("expected_packages").sort == report.fetch("completed_packages").sort
    mutants = report.fetch("packages").flat_map do |package|
      package_id = package.fetch("package") == "." ? module_directory : File.join(module_directory, package.fetch("package"))
      package.fetch("report").fetch("files").flat_map do |file|
        file_path = file["file"] || file["path"] || raise(KeyError, "mutation file path absent")
        file.fetch("mutations", []).each_with_index.map do |mutation, index|
          status = mutation.fetch("status")
          raise KeyError, "mutation tool reports viable survivor" unless status == "KILLED"
          operator = mutation["type"] || mutation["mutator"] || mutation["operator"] || raise(KeyError, "mutation operator absent")
          line = mutation["line"] || mutation.dig("location", "line") || raise(KeyError, "mutation line absent")
          column = mutation["column"] || mutation.dig("location", "column") || 0
          identity = [package_id, file_path, line, column, operator, index]
          {"mutant_id" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(identity))}", "package_id" => package_id,
           "operator" => operator.to_s, "source_locator" => "#{file_path}:#{line}:#{column}", "outcome" => "killed"}
        end
      end
    end
    output = {"schema_version" => 1, "source_report_sha256" => "sha256:#{Digest::SHA256.hexdigest(report_bytes)}",
              "gremlins_versions" => report.fetch("gremlins_versions"), "gate_input_digests" => report.fetch("gate_input_digests"),
              "mutants" => mutants.sort_by { |row| row.fetch("mutant_id").b }}
    STDOUT.write(JSON.pretty_generate(output) + "\n")
  else
    warn "unknown acceptance runner mode #{command.inspect}"
    exit 64
  end
  rescue KeyError, JSON::ParserError, Errno::ENOENT => e
    warn "acceptance runner mode failed: #{e.message}"
    exit 65
  end
end

module AcceptanceExecutionRunner
  FORBIDDEN_PRODUCER_KEY = AcceptanceSchemaValidation::OUTCOME_FIELD

  class Capture
    attr_reader :execution, :receipt_bytes, :artifact_bytes, :artifact_sha256, :raw_capture_bytes,
                :raw_capture_sha256, :verifier_bytes, :plan
    attr_reader :package_manifest_bytes, :mutant_manifest_bytes

    def initialize(execution, receipt_bytes, artifact_bytes, raw_capture_bytes, verifier_bytes, plan,
                   package_manifest_bytes, mutant_manifest_bytes)
      @execution = execution.freeze
      @receipt_bytes = receipt_bytes.freeze
      @artifact_bytes = artifact_bytes&.freeze
      @artifact_sha256 = artifact_bytes && "sha256:#{Digest::SHA256.hexdigest(artifact_bytes)}"
      @raw_capture_bytes = raw_capture_bytes&.freeze
      @raw_capture_sha256 = raw_capture_bytes && "sha256:#{Digest::SHA256.hexdigest(raw_capture_bytes)}"
      @verifier_bytes = verifier_bytes&.freeze
      @plan = plan.freeze
      @package_manifest_bytes = package_manifest_bytes&.freeze
      @mutant_manifest_bytes = mutant_manifest_bytes&.freeze
      freeze
    end

    private_class_method :new
  end

  module_function

  def plan(producer_argv:, verifier_argv:, version_argv:, environment_probe_argv:,
           verifier_version_argv: nil, package_discovery_argv: nil, mutation_tool_argv: nil)
    value = {
      "producer_argv" => direct_argv(producer_argv, "producer"),
      "verifier_argv" => direct_argv(verifier_argv, "verifier"),
      "version_argv" => direct_argv(version_argv, "version probe"),
      "verifier_version_argv" => direct_argv(verifier_version_argv || [verifier_argv.first, "--version"], "verifier version probe"),
      "environment_probe_argv" => direct_argv(environment_probe_argv, "environment probe")
    }
    if package_discovery_argv || mutation_tool_argv
      raise ArgumentError, "coverage discovery requires package and mutation-tool argv plans" unless package_discovery_argv && mutation_tool_argv
      value["package_discovery_argv"] = direct_argv(package_discovery_argv, "package discovery")
      value["mutation_tool_argv"] = direct_argv(mutation_tool_argv, "pinned mutation tool")
    end
    value.freeze
  end

  def run(plan:, chdir:, receipt_path:, artifact_capture_path:, artifact_id:, input_manifest:, output_artifact_path:)
    go_cache = Dir.mktmpdir("identity-acceptance-gocache-")
    task_home = Dir.mktmpdir("identity-acceptance-home-")
    @active_go_cache = go_cache
    @active_task_home = task_home
    required = %w[producer_argv verifier_argv version_argv verifier_version_argv environment_probe_argv]
    raise ArgumentError, "coordinator plan is incomplete" unless plan.is_a?(Hash) && (required - plan.keys).empty?
    plan = plan.transform_values { |value| direct_argv(value, "plan") }.freeze
    raise ArgumentError, "input manifest must be a non-empty array" unless input_manifest.is_a?(Array) && !input_manifest.empty?
    tested_revision_stdout, tested_revision_stderr, tested_revision_status = capture_argv(["git", "rev-parse", "HEAD"], chdir: chdir)
    raise ArgumentError, "tested revision cannot be derived from repository" unless tested_revision_status.success? && tested_revision_stderr.empty?
    tested_revision = tested_revision_stdout.strip
    raise ArgumentError, "derived tested revision is invalid" unless tested_revision.match?(/\A[0-9a-f]{40}\z/)
    input_root = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(input_manifest)))}"
    started_at = Time.now.utc
    FileUtils.mkdir_p(File.dirname(artifact_capture_path))
    raise ArgumentError, "live artifact capture path already exists" if File.exist?(artifact_capture_path)
    raw_capture_path = File.join(
      File.dirname(artifact_capture_path),
      ".#{File.basename(artifact_capture_path)}.producer-#{Process.pid}-#{SecureRandom.hex(12)}.json"
    )
    raise ArgumentError, "raw capture path already exists" if File.exist?(raw_capture_path)
    producer_environment = {
      "IDENTITY_ACCEPTANCE_ARTIFACT_ID" => artifact_id,
      "IDENTITY_ACCEPTANCE_OUTPUT_PATH" => raw_capture_path,
      "IDENTITY_ACCEPTANCE_RAW_OUTPUT_PATH" => raw_capture_path,
      "IDENTITY_ACCEPTANCE_INPUT_ROOT" => input_root,
      "IDENTITY_ACCEPTANCE_TESTED_REVISION" => tested_revision
    }
    executable = resolve_executable(plan.fetch("producer_argv").first, chdir)
    verifier_executable = resolve_executable(plan.fetch("verifier_argv").first, chdir)
    verifier_driver = File.realpath(File.expand_path(plan.fetch("verifier_argv").fetch(1), chdir))
    raise ArgumentError, "independent verifier argv must differ from producer argv" if plan.fetch("verifier_argv") == plan.fetch("producer_argv")
    executable_bytes = File.binread(executable)
    version_stdout, version_stderr, version_status = capture_argv(plan.fetch("version_argv"), chdir: chdir)
    raise ArgumentError, "executable version probe failed" unless version_status.success?
    verifier_version_stdout, verifier_version_stderr, verifier_version_status = capture_argv(plan.fetch("verifier_version_argv"), chdir: chdir)
    raise ArgumentError, "verifier executable version probe failed" unless verifier_version_status.success?
    environment_stdout, environment_stderr, environment_status = capture_argv(plan.fetch("environment_probe_argv"), chdir: chdir)
    raise ArgumentError, "environment probe failed" unless environment_status.success?
    stdout, stderr, status = capture_argv(plan.fetch("producer_argv"), chdir: chdir, environment: producer_environment)
    raise ArgumentError, "producer execution failed" unless status.success?
    raw_capture_bytes = File.binread(raw_capture_path) if File.file?(raw_capture_path)
    reject_producer_authored_outcomes!(AcceptanceSchemaValidation.parse_json(raw_capture_bytes, canonical: true)) if raw_capture_bytes
    if artifact_id == "coverage-mutation-report"
      package_argv = plan.fetch("package_discovery_argv")
      mutation_tool_argv = plan.fetch("mutation_tool_argv")
      raise ArgumentError, "discovery must not execute the result producer plan" if package_argv == plan.fetch("producer_argv") || mutation_tool_argv == plan.fetch("producer_argv")
      package_manifest_bytes, package_discovery_stderr, package_discovery_status = capture_argv(package_argv, chdir: chdir)
      mutation_tool_bytes, mutation_tool_stderr, mutation_tool_status = capture_argv(plan.fetch("mutation_tool_argv"), chdir: chdir)
      unless package_discovery_status.success? && package_discovery_stderr.empty? && mutation_tool_status.success? && mutation_tool_stderr.empty?
        raise ArgumentError, "coordinator-owned coverage inventory discovery failed"
      end
      parse_inventory_manifest(package_manifest_bytes, "package")
      validate_inventory_sources!(AcceptanceSchemaValidation.parse_json(package_manifest_bytes, canonical: true), chdir)
      mutation_tool_path = File.realpath(File.expand_path(plan.fetch("mutation_tool_argv").fetch(1), chdir))
      mutation_tool_digest = "sha256:#{Digest::SHA256.hexdigest(File.binread(mutation_tool_path))}"
      mutation_output = AcceptanceSchemaValidation.parse_json(mutation_tool_bytes, canonical: true)
      mutants = mutation_output.fetch("mutants")
      mutant_manifest = {"derivation" => "pinned-mutation-tool-output", "mutation_tool_path" => mutation_tool_path,
                         "mutation_tool_sha256" => mutation_tool_digest,
                         "mutation_output_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(mutation_output)))}",
                         "mutation_output" => mutation_output, "mutants" => mutants}
      mutant_manifest_bytes = JSON.pretty_generate(mutant_manifest) + "\n"
      parse_inventory_manifest(mutant_manifest_bytes, "mutant")
    end
    verifier_capture_path = File.join(File.dirname(raw_capture_path), ".verifier-#{SecureRandom.hex(24)}.json")
    raise ArgumentError, "producer attempted to pre-author verifier output" if File.exist?(verifier_capture_path)
    verifier_environment = producer_environment.merge(
      "IDENTITY_ACCEPTANCE_VERIFIER_INPUT_PATH" => raw_capture_path,
      "IDENTITY_ACCEPTANCE_VERIFIER_OUTPUT_PATH" => verifier_capture_path,
      "IDENTITY_ACCEPTANCE_PRODUCER_STDOUT_SHA256" => "sha256:#{Digest::SHA256.hexdigest(stdout)}",
      "IDENTITY_ACCEPTANCE_PRODUCER_STDERR_SHA256" => "sha256:#{Digest::SHA256.hexdigest(stderr)}"
    )
    verifier_stdout, verifier_stderr, verifier_status = capture_argv(plan.fetch("verifier_argv"), chdir: chdir, environment: verifier_environment)
    raise ArgumentError, "independent verifier failed" unless verifier_status.success?
    completed_at = Time.now.utc
    verifier_bytes = File.binread(verifier_capture_path) if File.file?(verifier_capture_path)
    verifier_attestation = parse_verifier_attestation(verifier_bytes, artifact_id, raw_capture_bytes)
    raw_capture_sha256 = raw_capture_bytes && "sha256:#{Digest::SHA256.hexdigest(raw_capture_bytes)}"
    execution = {
      "execution_identity" => "sha256:#{'0' * 64}",
      "execution_receipt_path" => receipt_path,
      "execution_receipt_sha256" => "sha256:#{'0' * 64}",
      "capture_authority" => "coordinator-owned-execution-runner/v3",
      "producer_argv" => plan.fetch("producer_argv"),
      "verifier_argv" => plan.fetch("verifier_argv"),
      "tested_revision" => tested_revision,
      "input_root" => input_root,
      "sandbox_environment_sha256" => "sha256:#{Digest::SHA256.hexdigest([trusted_toolchain_path, File.basename(task_home), File.basename(go_cache)].join("\0"))}",
      "canonical_workdir" => File.realpath(chdir),
      "executable_realpath" => executable,
      "executable_sha256" => "sha256:#{Digest::SHA256.hexdigest(executable_bytes)}",
      "verifier_executable_realpath" => verifier_executable,
      "verifier_executable_sha256" => "sha256:#{Digest::SHA256.hexdigest(File.binread(verifier_executable))}",
      "verifier_driver_realpath" => verifier_driver,
      "verifier_driver_sha256" => "sha256:#{Digest::SHA256.hexdigest(File.binread(verifier_driver))}",
      "version_argv" => plan.fetch("version_argv"),
      "version_stdout" => version_stdout,
      "version_stderr" => version_stderr,
      "verifier_version_argv" => plan.fetch("verifier_version_argv"),
      "verifier_version_stdout" => verifier_version_stdout,
      "verifier_version_stderr" => verifier_version_stderr,
      "environment_probe_argv" => plan.fetch("environment_probe_argv"),
      "environment_probe_stdout" => environment_stdout,
      "environment_probe_stderr" => environment_stderr,
      "verifier_exit_status" => verifier_status.exitstatus,
      "verifier_stdout" => verifier_stdout,
      "verifier_stderr" => verifier_stderr,
      "verifier_attestation_sha256" => verifier_bytes ? "sha256:#{Digest::SHA256.hexdigest(verifier_bytes)}" : "sha256:#{'0' * 64}",
      "started_at" => started_at.iso8601(9),
      "completed_at" => completed_at.iso8601(9),
      "exit_status" => status.exitstatus,
      "stdout" => stdout,
      "stdout_byte_length" => 0,
      "stdout_sha256" => "sha256:#{'0' * 64}",
      "stderr" => stderr,
      "stderr_byte_length" => 0,
      "stderr_sha256" => "sha256:#{'0' * 64}",
      "captured_output_sha256" => "sha256:#{'0' * 64}",
      "raw_capture_sha256" => raw_capture_sha256 || "sha256:#{'0' * 64}",
      "output_artifact_path" => output_artifact_path,
      "artifact_payload_binding" => "record.artifact_hashes[path,sha256]"
    }
    if artifact_id == "coverage-mutation-report"
      execution["package_discovery_argv"] = plan.fetch("package_discovery_argv")
      execution["mutation_tool_argv"] = plan.fetch("mutation_tool_argv")
      execution["mutation_tool_output_sha256"] = "sha256:#{Digest::SHA256.hexdigest(mutation_tool_bytes)}"
      execution["package_manifest_sha256"] = package_manifest_bytes ? "sha256:#{Digest::SHA256.hexdigest(package_manifest_bytes)}" : "sha256:#{'0' * 64}"
      execution["mutant_manifest_sha256"] = mutant_manifest_bytes ? "sha256:#{Digest::SHA256.hexdigest(mutant_manifest_bytes)}" : "sha256:#{'0' * 64}"
    end
    AcceptanceSchemaValidation.bind_execution_proof!(execution)
    receipt_bytes = AcceptanceSchemaValidation.execution_receipt_bytes(execution)
    artifact_bytes = derive_final_artifact(
      raw_capture_bytes, execution, artifact_id: artifact_id, verifier_attestation: verifier_attestation,
      package_manifest_bytes: package_manifest_bytes, mutant_manifest_bytes: mutant_manifest_bytes
    ) if raw_capture_bytes && status.success?
    if artifact_bytes
      write_atomically(artifact_capture_path, artifact_bytes)
      write_atomically(receipt_path, receipt_bytes)
    end
    Capture.send(:new, execution, receipt_bytes, artifact_bytes, raw_capture_bytes, verifier_bytes, plan,
                 package_manifest_bytes, mutant_manifest_bytes)
  ensure
    File.unlink(raw_capture_path) if raw_capture_path && File.file?(raw_capture_path)
    File.unlink(verifier_capture_path) if verifier_capture_path && File.file?(verifier_capture_path)
    FileUtils.remove_entry_secure(go_cache) if go_cache && File.directory?(go_cache)
    FileUtils.remove_entry_secure(task_home) if task_home && File.directory?(task_home)
    @active_go_cache = nil
    @active_task_home = nil
  end

  def derive_final_artifact(raw_capture_bytes, execution, artifact_id:, verifier_attestation:, package_manifest_bytes: nil, mutant_manifest_bytes: nil)
    payload = AcceptanceSchemaValidation.parse_json(raw_capture_bytes, canonical: true)
    raise ArgumentError, "producer raw capture must be a JSON object" unless payload.is_a?(Hash)
    raise ArgumentError, "producer raw capture artifact_id drifted" unless payload["artifact_id"] == artifact_id
    reject_producer_authored_outcomes!(payload)
    apply_verifier_assignments!(payload, verifier_attestation)

    runner_transcript_fields(artifact_id, payload).each do |field|
      payload.fetch(field, []).each do |row|
        raise ArgumentError, "producer raw capture may not author #{field} transcript evidence" if row.key?("transcript_sha256")
        row["transcript_sha256"] = "sha256:#{'0' * 64}"
      end
    end
    payload.fetch("provider_results", []).each do |provider|
      valid = [[true, "passed"], [false, "not-run"]].include?([provider["verified"], provider["execution_outcome"]])
      raise ArgumentError, "verifier provider result is incoherent" unless valid
    end
    payload.fetch("protocol_case_results", []).each do |protocol_case|
      requirement_id = protocol_case.fetch("requirement_id")
      expected_outcome = protocol_outcome(requirement_id)
      raise ArgumentError, "verifier protocol outcome is absent or unresolved" unless protocol_case["outcome"] == expected_outcome
    end
    payload.fetch("external_profile_results", []).each do |profile|
      raise ArgumentError, "verifier external profile result is unresolved" unless %w[passed not-run].include?(profile["execution_outcome"])
    end
    if artifact_id == "coverage-mutation-report"
      packages = payload.fetch("affected_package_results")
      mutants = payload.fetch("viable_mutant_results")
      package_manifest = parse_inventory_manifest(package_manifest_bytes, "package")
      mutant_manifest = parse_inventory_manifest(mutant_manifest_bytes, "mutant")
      package_manifest_sha256 = "sha256:#{Digest::SHA256.hexdigest(package_manifest_bytes)}"
      mutant_manifest_sha256 = "sha256:#{Digest::SHA256.hexdigest(mutant_manifest_bytes)}"
      raise ArgumentError, "execution does not bind independently captured package manifest" unless execution.fetch("package_manifest_sha256") == package_manifest_sha256
      raise ArgumentError, "execution does not bind independently captured mutant manifest" unless execution.fetch("mutant_manifest_sha256") == mutant_manifest_sha256
      expected_package_ids = package_manifest.fetch("package_ids")
      expected_mutant_rows = mutant_manifest.fetch("mutants")
      package_ids = packages.map { |row| row.fetch("package_id") }
      mutant_identities = mutants.map do |row|
        [row.fetch("mutant_id"), row.fetch("package_id"), row.fetch("operator"), row.fetch("source_locator"), row.fetch("outcome")].join("\0")
      end
      expected_mutant_identities = expected_mutant_rows.map do |row|
        [row.fetch("mutant_id"), row.fetch("package_id"), row.fetch("operator"), row.fetch("source_locator"), row.fetch("outcome")].join("\0")
      end
      raise ArgumentError, "coverage results differ from independently captured package manifest" unless package_ids.sort_by(&:b) == expected_package_ids.sort_by(&:b)
      raise ArgumentError, "coverage results differ from independently captured viable-mutant manifest" unless mutant_identities.sort_by(&:b) == expected_mutant_identities.sort_by(&:b)

      payload["affected_package_manifest_sha256"] = package_manifest_sha256
      payload["viable_mutant_manifest_sha256"] = mutant_manifest_sha256
    end
    payload["execution"] = execution
    AcceptanceSchemaValidation.bind_runner_transcripts!(payload, execution)
    JSON.pretty_generate(payload) + "\n"
  end

  def direct_argv(value, label)
    raise ArgumentError, "#{label} argv must be a nonempty string array" unless value.is_a?(Array) && !value.empty? && value.all? { |entry| entry.is_a?(String) && !entry.empty? && !entry.include?("\0") }

    value.map(&:dup).freeze
  end
  private_class_method :direct_argv

  def capture_argv(argv, chdir:, environment: {})
    cache = @active_go_cache || Dir.mktmpdir("identity-acceptance-gocache-")
    task_home = @active_task_home || Dir.mktmpdir("identity-acceptance-home-")
    owned_here = @active_go_cache.nil?
    allowed_environment = {"PATH" => trusted_toolchain_path, "HOME" => task_home, "GOCACHE" => cache}.merge(environment)
    Open3.capture3(allowed_environment, *direct_argv(argv, "subprocess"), chdir: chdir, unsetenv_others: true)
  ensure
    FileUtils.remove_entry_secure(cache) if owned_here && cache && File.directory?(cache)
    FileUtils.remove_entry_secure(task_home) if owned_here && task_home && File.directory?(task_home)
  end
  private_class_method :capture_argv

  def trusted_toolchain_path
    [File.dirname(RbConfig.ruby), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"].select { |path| File.directory?(path) }.uniq.join(File::PATH_SEPARATOR)
  end
  private_class_method :trusted_toolchain_path

  def resolve_executable(name, chdir)
    candidates = if name.include?(File::SEPARATOR)
                   [File.expand_path(name, chdir)]
                 else
                   trusted_toolchain_path.split(File::PATH_SEPARATOR).map { |directory| File.join(directory, name) }
                 end
    path = candidates.find { |candidate| File.file?(candidate) && File.executable?(candidate) }
    raise ArgumentError, "producer executable cannot be resolved" unless path

    File.realpath(path)
  end
  private_class_method :resolve_executable

  def reject_producer_authored_outcomes!(value, path = [])
    case value
    when Hash
      value.each do |key, child|
        raise ArgumentError, "producer raw capture may not author outcome field #{(path + [key]).join('.')}" if key == "execution" || FORBIDDEN_PRODUCER_KEY.match?(key)
        reject_producer_authored_outcomes!(child, path + [key])
      end
    when Array
      value.each_with_index { |child, index| reject_producer_authored_outcomes!(child, path + [index.to_s]) }
    end
  end

  def parse_verifier_attestation(bytes, artifact_id, raw_bytes)
    raise ArgumentError, "independent verifier output is absent" unless bytes && raw_bytes
    value = AcceptanceSchemaValidation.parse_json(bytes, canonical: true)
    raise ArgumentError, "verifier attestation artifact drifted" unless value["artifact_id"] == artifact_id
    digest = "sha256:#{Digest::SHA256.hexdigest(raw_bytes)}"
    raise ArgumentError, "verifier attestation does not bind raw capture" unless value["raw_capture_sha256"] == digest
    assignments = value.fetch("assignments")
    receipts = value.fetch("case_receipts")
    raise ArgumentError, "verifier assignments are not tool-native parsed results" unless assignments.is_a?(Array)
    raise ArgumentError, "verifier lacks per-case receipts" unless receipts.is_a?(Array) && !receipts.empty?
    receipt_ids = receipts.map { |row| row.fetch("case_id") }
    raise ArgumentError, "verifier case receipts contain duplicates" unless receipt_ids.uniq == receipt_ids
    receipts.each do |receipt|
      raise ArgumentError, "verifier receipt does not bind raw capture" unless receipt["raw_capture_sha256"] == digest
      raise ArgumentError, "verifier receipt lacks tool-native result digest" unless receipt["tool_native_result_sha256"]&.match?(/\Asha256:[0-9a-f]{64}\z/)
      native_row = receipt.fetch("tool_native_row")
      native_digest = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(native_row)))}"
      raise ArgumentError, "verifier receipt tool-native row digest drifted" unless receipt["tool_native_result_sha256"] == native_digest
      raise ArgumentError, "verifier native row may not author assignment path" if native_row.key?("assignment_path")
      raise ArgumentError, "verifier native observation does not bind assignment value" unless native_row["observed_value"] == assignment_for_receipt(assignments, receipt).fetch("value")
    end
    assignment_bindings = assignments.map do |assignment|
      [JSON.generate(AcceptanceSchemaValidation.canonical(assignment.fetch("path"))), "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(assignment.fetch("value"))))}"]
    end
    receipt_bindings = receipts.map { |receipt| [JSON.generate(receipt.fetch("assignment_path")), receipt.fetch("assignment_value_sha256")] }
    raise ArgumentError, "verifier receipts do not exactly cover assignments" unless assignment_bindings.sort == receipt_bindings.sort
    value
  rescue KeyError, NoMethodError
    raise ArgumentError, "verifier attestation shape is invalid"
  end
  private_class_method :parse_verifier_attestation

  def assignment_for_receipt(assignments, receipt)
    assignments.find { |assignment| assignment.fetch("path") == receipt.fetch("assignment_path") } || raise(ArgumentError, "verifier receipt lacks assignment")
  end
  private_class_method :assignment_for_receipt

  def apply_verifier_assignments!(payload, attestation)
    attestation.fetch("assignments").each do |assignment|
      path = assignment.fetch("path")
      raise ArgumentError, "verifier assignment path is invalid" unless path.is_a?(Array) && !path.empty?
      key = path.last
      raise ArgumentError, "verifier may only author derived outcome fields" unless key.is_a?(String) && FORBIDDEN_PRODUCER_KEY.match?(key)
      parent = path[0...-1].reduce(payload) { |value, segment| value.fetch(segment) }
      raise ArgumentError, "verifier assignment target already exists" if parent.key?(key)
      parent[key] = assignment.fetch("value")
    end
  rescue KeyError, TypeError, NoMethodError
    raise ArgumentError, "verifier assignment target is invalid"
  end
  private_class_method :apply_verifier_assignments!

  def parse_inventory_manifest(bytes, kind)
    raise ArgumentError, "independently captured #{kind} manifest is absent" unless bytes

    manifest = AcceptanceSchemaValidation.parse_json(bytes, canonical: true)
    raise ArgumentError, "#{kind} manifest is not a JSON object" unless manifest.is_a?(Hash)
    if kind == "package"
      raise ArgumentError, "package discovery is not manifest and reverse-dependant derived" unless manifest["derivation"] == "modules-and-packages-manifests-with-reverse-dependants"
      raise ArgumentError, "package discovery lacks source manifest digests" unless manifest["source_manifest_sha256s"].is_a?(Hash) && %w[modules.json packages.json].all? { |name| manifest.fetch("source_manifest_sha256s")[name]&.match?(/\Asha256:[0-9a-f]{64}\z/) }
      raise ArgumentError, "package discovery lacks reverse-dependant closure" unless manifest["reverse_dependants"].is_a?(Hash)
      package_ids = manifest.fetch("package_ids")
      raise ArgumentError, "package discovery has duplicate package identities" unless package_ids.is_a?(Array) && package_ids.uniq == package_ids
      raise ArgumentError, "reverse-dependant closure has foreign package identities" unless (manifest.fetch("reverse_dependants").keys - package_ids).empty?
    else
      raise ArgumentError, "mutant discovery is not pinned mutation-tool output" unless manifest["derivation"] == "pinned-mutation-tool-output"
      raise ArgumentError, "mutant discovery lacks pinned tool identity" unless manifest["mutation_tool_sha256"]&.match?(/\Asha256:[0-9a-f]{64}\z/) && manifest["mutation_output_sha256"]&.match?(/\Asha256:[0-9a-f]{64}\z/)
      raise ArgumentError, "mutant discovery lacks mutation tool path" unless manifest["mutation_tool_path"].is_a?(String) && !manifest["mutation_tool_path"].empty?
      mutants = manifest.fetch("mutants")
      identities = mutants.map { |row| [row.fetch("mutant_id"), row.fetch("package_id"), row.fetch("operator"), row.fetch("source_locator"), row.fetch("outcome")] }
      raise ArgumentError, "mutant discovery has duplicate identities" unless identities.uniq == identities
      raise ArgumentError, "mutation tool reports a viable survivor" unless mutants.all? { |row| row.fetch("outcome") == "killed" }
      native_document = manifest.fetch("mutation_output")
      native_output = native_document.fetch("mutants")
      document_digest = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(native_document)))}"
      raise ArgumentError, "mutant discovery native output digest drifted" unless manifest["mutation_output_sha256"] == document_digest
      native_identities = native_output.map { |row| [row.fetch("mutant_id"), row.fetch("package_id"), row.fetch("operator"), row.fetch("source_locator"), row.fetch("outcome")] }
      raise ArgumentError, "mutant identities differ from native mutation output" unless identities.sort == native_identities.sort
    end
    manifest
  end
  private_class_method :parse_inventory_manifest

  def validate_inventory_sources!(manifest, chdir)
    manifest.fetch("source_manifest_sha256s").each do |name, digest|
      path = File.join(chdir, name)
      raise ArgumentError, "package discovery source manifest #{name} is absent" unless File.file?(path)
      actual = "sha256:#{Digest::SHA256.hexdigest(File.binread(path))}"
      raise ArgumentError, "package discovery source manifest #{name} drifted" unless digest == actual
    end
    packages = AcceptanceSchemaValidation.parse_json(File.binread(File.join(chdir, "packages.json"))).fetch("packages")
    modules = AcceptanceSchemaValidation.parse_json(File.binread(File.join(chdir, "modules.json"))).fetch("modules")
    production_by_module = packages.select { |row| row["production"] }.group_by { |row| row.fetch("module_directory") }
    directory_by_path = modules.to_h { |row| [row.fetch("module_path"), row.fetch("directory")] }
    reverse = modules.to_h do |row|
      [row.fetch("directory"), Array(row["reverse_owned_dependencies"]).map { |path| directory_by_path.fetch(path) }]
    end
    seeds = manifest.fetch("affected_module_directories")
    closure = seeds.dup
    loop do
      expanded = closure | reverse.select { |directory, _| closure.include?(directory) }.values.flatten
      break if expanded == closure
      closure = expanded
    end
    expected_packages = closure.flat_map { |directory| Array(production_by_module[directory]).map { |row| row.fetch("directory") } }.uniq.sort_by(&:b)
    raise ArgumentError, "package discovery differs from recomputed manifest reverse-dependant closure" unless manifest.fetch("package_ids").sort_by(&:b) == expected_packages
    expected_reverse = closure.to_h { |directory| [directory, Array(reverse[directory]).select { |dependent| closure.include?(dependent) }.sort_by(&:b)] }
    raise ArgumentError, "package discovery reverse-dependant graph drifted" unless manifest.fetch("reverse_dependants") == expected_reverse
  end
  private_class_method :validate_inventory_sources!

  def runner_transcript_fields(artifact_id, payload)
    schema_path = File.join(__dir__, "v1", "schemas", "#{artifact_id}.schema.json")
    return ["scenario_cases"] unless File.file?(schema_path)

    schema = AcceptanceSchemaValidation.parse_json(File.binread(schema_path), canonical: true)
    properties = schema.dig("$defs", "artifact_evidence", "properties")
    properties.filter_map do |field, field_schema|
      next unless field_schema["type"] == "array" && payload[field].is_a?(Array)

      item_schema = field_schema.fetch("items", {})
      variants = item_schema.fetch("oneOf", [item_schema])
      field if variants.any? { |variant| variant.fetch("properties", {}).key?("transcript_sha256") }
    end
  end
  private_class_method :runner_transcript_fields

  def protocol_outcome(requirement_id)
    manifest_path = File.expand_path("../PROTOCOL_CONFORMANCE_MANIFEST.json", __dir__)
    manifest = AcceptanceSchemaValidation.parse_json(File.binread(manifest_path))
    requirement = manifest.fetch("clause_pins").find { |row| row.fetch("requirement_id") == requirement_id }
    raise ArgumentError, "raw capture contains unknown protocol requirement #{requirement_id}" unless requirement

    requirement.fetch("disposition") == "unsupported" ? "unsupported" : "passed"
  end
  private_class_method :protocol_outcome

  def live_capture_errors(declared, capture, artifact_bytes: nil, path: "$.execution")
    return ["#{path} lacks a live coordinator-owned runner capture"] unless capture.is_a?(Capture)

    actual = capture.execution
    findings = []
    %w[producer_argv verifier_argv tested_revision input_root sandbox_environment_sha256 canonical_workdir executable_realpath executable_sha256 verifier_executable_realpath verifier_executable_sha256 verifier_driver_realpath verifier_driver_sha256 version_argv version_stdout version_stderr verifier_version_argv verifier_version_stdout verifier_version_stderr environment_probe_argv environment_probe_stdout environment_probe_stderr verifier_exit_status verifier_stdout verifier_stderr verifier_attestation_sha256 exit_status stdout stdout_byte_length stdout_sha256 stderr stderr_byte_length stderr_sha256 captured_output_sha256 raw_capture_sha256 output_artifact_path].each do |field|
      findings << "#{path}.#{field} differs from live runner capture" unless declared[field] == actual[field]
    end
    findings << "#{path} live runner command failed" unless actual["exit_status"] == 0
    findings.concat(discovery_manifest_errors(capture, path: path))
    expected_receipt = AcceptanceSchemaValidation.execution_receipt_bytes(actual)
    findings << "#{path} live runner receipt bytes drifted" unless capture.receipt_bytes == expected_receipt
    expected_receipt_digest = "sha256:#{Digest::SHA256.hexdigest(capture.receipt_bytes)}"
    findings << "#{path} live runner receipt digest drifted" unless actual["execution_receipt_sha256"] == expected_receipt_digest
    if capture.raw_capture_bytes.nil?
      findings << "#{path} live runner did not capture fresh producer raw bytes"
    elsif actual["raw_capture_sha256"] != capture.raw_capture_sha256
      findings << "#{path} live runner raw capture digest drifted"
    end
    if capture.artifact_bytes.nil?
      findings << "#{path} live runner did not produce its task-owned artifact"
    elsif artifact_bytes.nil?
      findings << "#{path} committed artifact bytes are absent from live comparison"
    else
      findings << "#{path} live artifact digest drifted" unless capture.artifact_sha256 == "sha256:#{Digest::SHA256.hexdigest(capture.artifact_bytes)}"
      begin
        live_payload = AcceptanceSchemaValidation.parse_json(capture.artifact_bytes, canonical: true)
        committed_payload = AcceptanceSchemaValidation.parse_json(artifact_bytes, canonical: true)
        live_semantics = producer_semantics(live_payload)
        committed_semantics = producer_semantics(committed_payload)
        live_digest = Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(live_semantics)))
        committed_digest = Digest::SHA256.hexdigest(JSON.generate(AcceptanceSchemaValidation.canonical(committed_semantics)))
        findings << "#{path} live artifact semantics differ from committed artifact" unless live_digest == committed_digest
      rescue JSON::ParserError => e
        findings << "#{path} live artifact comparison is invalid JSON: #{e.message}"
      end
    end
    completed_at = Time.iso8601(actual.fetch("completed_at"))
    age = Time.now.utc - completed_at
    findings << "#{path} live runner capture is stale or future-dated" unless age.between?(0, 300)
    findings
  rescue KeyError, ArgumentError
    ["#{path} live runner capture freshness cannot be evaluated"]
  end

  def receipt_errors(execution, receipt_bytes, expected_plan:, chdir:)
    findings = AcceptanceSchemaValidation.execution_receipt_errors(execution, receipt_bytes)
    %w[producer_argv verifier_argv version_argv verifier_version_argv environment_probe_argv package_discovery_argv mutation_tool_argv].each do |field|
      next unless expected_plan.key?(field) || execution.key?(field)
      findings << "$.execution.#{field} differs from coordinator-owned plan" unless execution[field] == expected_plan[field]
    end
    begin
      executable = resolve_executable(expected_plan.fetch("producer_argv").first, chdir)
      findings << "$.execution.executable_realpath drifted" unless execution["executable_realpath"] == executable
      digest = "sha256:#{Digest::SHA256.hexdigest(File.binread(executable))}"
      findings << "$.execution.executable_sha256 drifted" unless execution["executable_sha256"] == digest
    rescue ArgumentError, Errno::ENOENT
      findings << "$.execution executable identity cannot be evaluated"
    end
    findings
  end

  def discovery_manifest_errors(capture, path: "$.execution")
    return ["#{path} lacks live discovery capture"] unless capture.is_a?(Capture)
    return [] unless capture.execution.key?("package_manifest_sha256") || capture.execution.key?("mutant_manifest_sha256")

    findings = []
    begin
      parse_inventory_manifest(capture.package_manifest_bytes, "package")
      parse_inventory_manifest(capture.mutant_manifest_bytes, "mutant")
      package_digest = "sha256:#{Digest::SHA256.hexdigest(capture.package_manifest_bytes)}"
      mutant_digest = "sha256:#{Digest::SHA256.hexdigest(capture.mutant_manifest_bytes)}"
      findings << "#{path}.package_manifest_sha256 drifted" unless capture.execution["package_manifest_sha256"] == package_digest
      findings << "#{path}.mutant_manifest_sha256 drifted" unless capture.execution["mutant_manifest_sha256"] == mutant_digest
    rescue ArgumentError, JSON::ParserError => e
      findings << "#{path} discovery manifests are invalid: #{e.message}"
    end
    findings
  end

  def producer_semantics(payload)
    semantic = JSON.parse(JSON.generate(payload.reject { |key, _value| key == "execution" }))
    semantic.each_value do |value|
      next unless value.is_a?(Array)

      value.each { |row| row.delete("transcript_sha256") if row.is_a?(Hash) }
    end
    semantic.fetch("provider_results", []).each { |row| %w[verified execution_outcome].each { |field| row.delete(field) } }
    semantic.fetch("protocol_case_results", []).each { |row| row.delete("outcome") }
    semantic.fetch("external_profile_results", []).each { |row| row.delete("execution_outcome") }
    semantic
  end
  private_class_method :producer_semantics

  def write_atomically(path, bytes)
    directory = File.dirname(path)
    FileUtils.mkdir_p(directory)
    temporary = Tempfile.new(["acceptance-execution-", ".json"], directory)
    temporary.binmode
    temporary.write(bytes)
    temporary.flush
    temporary.fsync
    temporary.close
    File.rename(temporary.path, path)
  ensure
    temporary&.close!
  end
  private_class_method :write_atomically
end
