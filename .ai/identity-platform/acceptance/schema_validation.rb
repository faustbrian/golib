# frozen_string_literal: true

require "json"
require "digest"
require "time"
require "open3"
require "tempfile"
require "fileutils"

module AcceptanceSchemaValidation
  class DuplicateKeyError < JSON::ParserError; end

  module_function

  def parse_json(bytes, canonical: false)
    value = JSON.parse(bytes)
    reject_duplicate_keys!(bytes)
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
end

module AcceptanceExecutionRunner
  class Capture
    attr_reader :execution, :receipt_bytes, :artifact_bytes, :artifact_sha256

    def initialize(execution, receipt_bytes, artifact_bytes)
      @execution = execution.freeze
      @receipt_bytes = receipt_bytes.freeze
      @artifact_bytes = artifact_bytes&.freeze
      @artifact_sha256 = artifact_bytes && "sha256:#{Digest::SHA256.hexdigest(artifact_bytes)}"
      freeze
    end

    private_class_method :new
  end

  module_function

  def run(command:, chdir:, receipt_path:, artifact_capture_path:, artifact_id:, tested_revision:, input_root:, tool:, environment:, output_artifact_path:)
    started_at = Time.now.utc
    FileUtils.mkdir_p(File.dirname(artifact_capture_path))
    raise ArgumentError, "live artifact capture path already exists" if File.exist?(artifact_capture_path)
    runner_environment = {
      "IDENTITY_ACCEPTANCE_ARTIFACT_ID" => artifact_id,
      "IDENTITY_ACCEPTANCE_OUTPUT_PATH" => artifact_capture_path,
      "IDENTITY_ACCEPTANCE_INPUT_ROOT" => input_root,
      "IDENTITY_ACCEPTANCE_TESTED_REVISION" => tested_revision
    }
    stdout, stderr, status = Open3.capture3(runner_environment, "sh", "-lc", command, chdir: chdir)
    completed_at = Time.now.utc
    artifact_bytes = File.binread(artifact_capture_path) if File.file?(artifact_capture_path)
    execution = {
      "execution_identity" => "sha256:#{'0' * 64}",
      "execution_receipt_path" => receipt_path,
      "execution_receipt_sha256" => "sha256:#{'0' * 64}",
      "capture_authority" => "coordinator-owned-execution-runner/v1",
      "command" => command,
      "tested_revision" => tested_revision,
      "input_root" => input_root,
      "tool" => tool,
      "environment" => environment,
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
      "output_artifact_path" => output_artifact_path,
      "artifact_payload_binding" => "record.artifact_hashes[path,sha256]"
    }
    AcceptanceSchemaValidation.bind_execution_proof!(execution)
    artifact_sha256 = artifact_bytes && "sha256:#{Digest::SHA256.hexdigest(artifact_bytes)}"
    receipt_bytes = live_receipt_bytes(execution, artifact_sha256)
    execution["execution_receipt_sha256"] = "sha256:#{Digest::SHA256.hexdigest(receipt_bytes)}"
    write_atomically(receipt_path, receipt_bytes)
    Capture.send(:new, execution, receipt_bytes, artifact_bytes)
  end

  def live_capture_errors(declared, capture, artifact_bytes: nil, path: "$.execution")
    return ["#{path} lacks a live coordinator-owned runner capture"] unless capture.is_a?(Capture)

    actual = capture.execution
    findings = []
    %w[command tested_revision input_root tool environment exit_status stdout stdout_byte_length stdout_sha256 stderr stderr_byte_length stderr_sha256 captured_output_sha256 output_artifact_path].each do |field|
      findings << "#{path}.#{field} differs from live runner capture" unless declared[field] == actual[field]
    end
    findings << "#{path} live runner command failed" unless actual["exit_status"] == 0
    expected_receipt = live_receipt_bytes(actual, capture.artifact_sha256)
    findings << "#{path} live runner receipt bytes drifted" unless capture.receipt_bytes == expected_receipt
    expected_receipt_digest = "sha256:#{Digest::SHA256.hexdigest(capture.receipt_bytes)}"
    findings << "#{path} live runner receipt digest drifted" unless actual["execution_receipt_sha256"] == expected_receipt_digest
    if capture.artifact_bytes.nil?
      findings << "#{path} live runner did not produce its task-owned artifact"
    elsif artifact_bytes.nil?
      findings << "#{path} committed artifact bytes are absent from live comparison"
    else
      findings << "#{path} live artifact digest drifted" unless capture.artifact_sha256 == "sha256:#{Digest::SHA256.hexdigest(capture.artifact_bytes)}"
      begin
        live_payload = AcceptanceSchemaValidation.parse_json(capture.artifact_bytes, canonical: true)
        committed_payload = AcceptanceSchemaValidation.parse_json(artifact_bytes, canonical: true)
        live_semantics = live_payload.reject { |key, _value| key == "execution" }
        committed_semantics = committed_payload.reject { |key, _value| key == "execution" }
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

  def live_receipt_bytes(execution, artifact_sha256)
    payload = {
      "execution" => AcceptanceSchemaValidation.execution_receipt_payload(execution),
      "captured_artifact_sha256" => artifact_sha256
    }
    JSON.pretty_generate(payload) + "\n"
  end
  private_class_method :live_receipt_bytes

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
