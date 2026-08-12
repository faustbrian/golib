#!/usr/bin/env ruby
# frozen_string_literal: true

require "json"
require "digest"
require "tmpdir"
require "rbconfig"
require "shellwords"
require "time"
require_relative "model"
require_relative "schema_validation"

module AcceptanceCatalogCheck
  BANNED_TEXT = [
    "valid inputs and all declared dependencies", "exercise the complete", "documented result",
    "durable side effects", "fixture scenario", "fixture preconditions", "fixture stimulus",
    "fixture outcome", ".behavior contract", "generic behavior"
  ].freeze
  REQUIRED_OPERATION_EVIDENCE = {
    "operational-api-redaction-report" => %w[identity.audit.export identity.audit.get identity.audit.list identity.audit.search],
    "reference-http-password-journey" => %w[identity.email.address-get identity.email.address-list identity.email.address-remove identity.phone.password-signin],
    "mfa-recovery-report" => %w[identity.admin.mfa-recovery-issue identity.admin.mfa-reset],
    "api-key-http-lifecycle-report" => %w[identity.apikey.session-authenticate],
    "captcha-four-provider-report" => IdentityPlatformAcceptance.captcha_protected_target_ids,
    "oauth-oidc-conformance-report" => %w[identity.oauth-server.authorize identity.oauth-server.continue identity.oauth-server.token identity.oauth-server.session-token identity.oauth-server.userinfo identity.oauth-server.introspect identity.oauth-server.revoke identity.oauth-server.dynamic-register identity.oauth-server.discovery-oauth identity.oauth-server.discovery-oidc identity.oauth-server.jwks identity.oauth-server.end-session identity.oauth-server.protected-resource-metadata],
    "passkey-browser-ceremony-report" => %w[identity.passkey.register-options identity.passkey.register-verify identity.passkey.signin-options identity.passkey.signin-verify identity.passkey.list identity.passkey.update identity.passkey.delete],
    "sso-provider-lifecycle-report" => %w[identity.sso.oauth-callback identity.sso.oidc-callback identity.sso.saml-metadata identity.sso.saml-start identity.sso.saml-acs identity.sso.saml-idp-init identity.sso.saml-logout-start identity.sso.saml-slo]
  }.freeze

  module_function

  def fail_check(message)
    warn "acceptance catalog: #{message}"
    exit 1
  end

  def load_canonical(path)
    fail_check("missing #{path}") unless File.file?(path)
    bytes = File.read(path)
    AcceptanceSchemaValidation.parse_json(bytes, canonical: true)
  rescue JSON::ParserError => e
    fail_check("invalid JSON #{path}: #{e.message}")
  end

  def canonical(value)
    case value
    when Hash then value.keys.sort.to_h { |key| [key, canonical(value.fetch(key))] }
    when Array then value.map { |entry| canonical(entry) }
    else value
    end
  end

  def schema_errors(value, schema, path = "$")
    AcceptanceSchemaValidation.errors(value, schema, path)
  end

  def sample_value(schema)
    return schema.fetch("const") if schema.key?("const")
    return schema.fetch("enum").first if schema.key?("enum")
    type = Array(schema.fetch("type", "string")).find { |candidate| candidate != "null" }
    case type
    when "boolean" then true
    when "integer" then schema.fetch("minimum", 1)
    when "string"
      return "sha256:#{'a' * 64}" if schema["pattern"]&.include?("sha256:")
      "sample-id"
    else fail_check("cannot sample schema type #{type}")
    end
  end

  def representative_evidence(artifact, schema)
    evidence_schema = schema.dig("$defs", "artifact_evidence")
    value = AcceptanceSchemaValidation.sample(evidence_schema)
    evidence_schema.fetch("x-semantic-rules").each do |rule|
      case rule.fetch("kind")
      when "positive" then rule.fetch("fields").each { |field| value[field] = 1 }
      when "zero" then rule.fetch("fields").each { |field| value[field] = 0 }
      when "true" then rule.fetch("fields").each { |field| value[field] = true }
      when "equal" then value[rule.fetch("right")] = value.fetch(rule.fetch("left"))
      when "const" then value[rule.fetch("field")] = rule.fetch("value")
      end
    end
    if artifact.fetch("artifact_id") == "coverage-mutation-report"
      value["affected_package_manifest_sha256"] = "sha256:#{'a' * 64}"
      value["viable_mutant_manifest_sha256"] = "sha256:#{'b' * 64}"
    end
    execution = value.fetch("execution")
    execution.merge!(
      "tested_revision" => "d" * 40, "input_root" => "sha256:#{'e' * 64}",
      "sandbox_environment_sha256" => "sha256:#{'9' * 64}",
      "executable_realpath" => File.realpath(RbConfig.ruby),
      "executable_sha256" => "sha256:#{Digest::SHA256.hexdigest(File.binread(File.realpath(RbConfig.ruby)))}",
      "verifier_executable_realpath" => File.realpath(RbConfig.ruby),
      "verifier_executable_sha256" => "sha256:#{Digest::SHA256.hexdigest(File.binread(File.realpath(RbConfig.ruby)))}",
      "verifier_driver_realpath" => File.join(IdentityPlatformAcceptance::REPOSITORY_ROOT, ".ai/identity-platform/acceptance/schema_validation.rb"),
      "verifier_driver_sha256" => "sha256:#{Digest::SHA256.hexdigest(File.binread(File.join(IdentityPlatformAcceptance::REPOSITORY_ROOT, '.ai/identity-platform/acceptance/schema_validation.rb')))}",
      "canonical_workdir" => IdentityPlatformAcceptance::REPOSITORY_ROOT,
      "version_stdout" => RUBY_DESCRIPTION, "version_stderr" => "",
      "verifier_version_stdout" => RUBY_DESCRIPTION, "verifier_version_stderr" => "",
      "environment_probe_stdout" => "#{RUBY_PLATFORM}/#{RUBY_ENGINE}", "environment_probe_stderr" => "",
      "verifier_exit_status" => 0, "verifier_stdout" => "", "verifier_stderr" => "",
      "verifier_attestation_sha256" => "sha256:#{'f' * 64}",
      "started_at" => "2026-08-11T00:00:00Z", "completed_at" => "2026-08-11T00:00:01Z",
      "stdout" => "#{artifact.fetch('artifact_id')} gate completed with attributable evidence\n", "stderr" => ""
    )
    AcceptanceSchemaValidation.bind_execution_proof!(execution)
    value.fetch("scenario_cases").each do |scenario|
      scenario["observed_values"].keys.each { |name| scenario.fetch("observed_values")[name] = deep_copy(value.fetch(name)) }
    end
    AcceptanceSchemaValidation.bind_runner_transcripts!(value, execution)
    value
  end

  def representative_record(artifact, evidence_digest, receipt_digest)
    manifest = [{
      "path_or_environment_id" => ".ai/identity-platform/END_STATE_ACCEPTANCE.json", "kind" => "tracked",
      "content_identity" => "sha256:#{'c' * 64}", "owner" => "coordinator", "reason" => "acceptance contract input"
    }]
    root = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(manifest)))}"
    observations = artifact.fetch("required_observations").map { |row| row.merge("artifact_sha256" => evidence_digest) }
    {
      "schema_version" => 2, "artifact_id" => artifact.fetch("artifact_id"),
      "result" => {"status" => IdentityPlatformAcceptance.artifact_result_status(artifact.fetch("artifact_id")),
        "gate" => artifact.dig("producer", "operation")},
      "tested_revision" => "d" * 40, "gate_execution_revision" => "d" * 40,
      "revalidation_revision" => nil, "input_manifest" => manifest, "input_root" => root,
      "observations" => observations,
      "artifact_hashes" => [
        {"path" => artifact.fetch("artifact_evidence_output_path"), "sha256" => evidence_digest},
        {"path" => IdentityPlatformAcceptance.execution_receipt_path(artifact), "sha256" => receipt_digest}
      ],
      "recorded_at" => "2026-08-11T00:00:00Z"
    }
  end

  def record_artifact_errors(record, artifact, schema, evidence_bytes, receipt_bytes, live_capture: nil, final_execution: false)
    errors = schema_errors(record, schema)
    expected_root = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(record.fetch('input_manifest'))))}"
    errors << "input root drifted" unless record.fetch("input_root") == expected_root
    exact_path = artifact.fetch("artifact_evidence_output_path")
    artifact_row = record.fetch("artifact_hashes").find { |entry| entry.fetch("path") == exact_path }
    errors << "exact artifact evidence path is unbound" unless artifact_row
    evidence_digest = evidence_bytes && "sha256:#{Digest::SHA256.hexdigest(evidence_bytes)}"
    errors << "artifact payload bytes are absent" unless evidence_bytes
    errors << "validated artifact bytes differ from bound hash" unless artifact_row && evidence_digest && artifact_row.fetch("sha256") == evidence_digest
    hashes = record.fetch("artifact_hashes").map { |entry| entry.fetch("sha256") }
    errors << "observation artifact hash is unbound" unless record.fetch("observations").all? { |row| hashes.include?(row.fetch("artifact_sha256")) && row.fetch("artifact_sha256") == evidence_digest }
    same_revision = record.fetch("tested_revision") == record.fetch("gate_execution_revision")
    valid_revision = same_revision ? record["revalidation_revision"].nil? : record["revalidation_revision"] == record["gate_execution_revision"]
    errors << "revision relation drifted" unless valid_revision
    begin
      return errors unless evidence_bytes
      evidence = AcceptanceSchemaValidation.parse_json(evidence_bytes, canonical: true)
      errors.concat(schema_errors(evidence, schema.dig("$defs", "artifact_evidence"))).map! { |error| "artifact payload #{error}" }
      execution = evidence.fetch("execution")
      evidence.fetch("scenario_cases").each do |scenario|
        observed_names = scenario.fetch("observed_values").keys
        expected_values = observed_names.to_h { |name| [name, evidence.fetch(name)] }
        if scenario.fetch("scenario") == "success" && scenario.fetch("observed_values") != expected_values
          errors << "artifact payload #{scenario.fetch('case_id')} success values drifted from top-level aggregate"
        end
        errors << "artifact payload #{scenario.fetch('case_id')} observed value count drifted" unless scenario.fetch("observed_value_count") == scenario.fetch("observed_values").length
        expected_transcript = AcceptanceSchemaValidation.runner_transcript_sha256(scenario, execution)
        errors << "artifact payload #{scenario.fetch('case_id')} transcript is not exact-value execution-bound" unless scenario.fetch("transcript_sha256") == expected_transcript
      end
      evidence.each do |field, value|
        next unless value.is_a?(Array)

        value.each do |entry|
          next unless entry.is_a?(Hash) && entry.key?("transcript_sha256")

          expected_transcript = AcceptanceSchemaValidation.runner_transcript_sha256(entry, execution)
          errors << "artifact payload #{field} row transcript is not exact-value execution-bound" unless entry.fetch("transcript_sha256") == expected_transcript
        end
      end
      if artifact.fetch("artifact_id") == "coverage-mutation-report"
        packages = evidence.fetch("affected_package_results")
        mutants = evidence.fetch("viable_mutant_results")
        package_ids = packages.map { |row| row.fetch("package_id") }
        mutant_ids = mutants.map { |row| row.fetch("mutant_id") }
        errors << "artifact payload package inventory count drifted" unless evidence.fetch("package_count") == packages.length
        errors << "artifact payload viable mutant inventory count drifted" unless evidence.fetch("viable_mutant_count") == mutants.length
        errors << "artifact payload killed mutant inventory count drifted" unless evidence.fetch("killed_mutant_count") == mutants.length
        errors << "artifact payload package inventory contains duplicate IDs" unless package_ids.uniq == package_ids
        errors << "artifact payload mutant inventory contains duplicate IDs" unless mutant_ids.uniq == mutant_ids
        errors << "artifact payload mutant inventory names an unaffected package" unless mutants.all? { |row| package_ids.include?(row.fetch("package_id")) }
        errors << "artifact payload package inventory contains incomplete coverage" unless packages.all? do |row|
          row.fetch("covered_statement_count") == row.fetch("statement_count") && row.fetch("statement_coverage_basis_points") == 10_000
        end
      end
      errors << "artifact execution revision is not record-bound" unless execution["tested_revision"] == record["tested_revision"]
      errors << "artifact execution input root is not record-bound" unless execution["input_root"] == record["input_root"]
      errors << "artifact execution output path is not record-bound" unless execution["output_artifact_path"] == artifact.fetch("artifact_evidence_output_path")
      receipt_path = IdentityPlatformAcceptance.execution_receipt_path(artifact)
      receipt_row = record.fetch("artifact_hashes").find { |entry| entry.fetch("path") == receipt_path }
      errors << "separate execution receipt path is unbound" unless receipt_row
      errors.concat(AcceptanceSchemaValidation.execution_receipt_errors(execution, receipt_bytes))
      receipt_digest = receipt_bytes && "sha256:#{Digest::SHA256.hexdigest(receipt_bytes)}"
      errors << "separate execution receipt bytes differ from bound hash" unless receipt_row && receipt_digest && receipt_row.fetch("sha256") == receipt_digest
      errors.concat(AcceptanceExecutionRunner.live_capture_errors(execution, live_capture, artifact_bytes: evidence_bytes)) if final_execution
    rescue JSON::ParserError => e
      errors << "artifact payload is invalid JSON: #{e.message}"
    end
    errors
  rescue KeyError => e
    ["record cross-field shape error: #{e.message}"]
  end

  def deep_copy(value)
    JSON.parse(JSON.generate(value))
  end

  def assert_live_runner_contract!
    runner_keywords = AcceptanceExecutionRunner.method(:run).parameters.map(&:last)
    fail_check("coordinator runner still accepts caller-authored command/tool/environment labels") unless
      runner_keywords.include?(:plan) && !runner_keywords.include?(:command) &&
      !runner_keywords.include?(:tool) && !runner_keywords.include?(:environment) &&
      runner_keywords.include?(:input_manifest) && !runner_keywords.include?(:tested_revision) &&
      !runner_keywords.include?(:input_root)
    Dir.mktmpdir("identity-acceptance-runner-") do |directory|
      script = 'cache = ENV.fetch("GOCACHE"); home = ENV.fetch("HOME"); raise unless File.directory?(cache) && File.directory?(home) && !ENV.fetch("PATH").include?(File.join(ENV.fetch("ORIGINAL_HOME", "/nonexistent"), ".dotfiles")); payload = {"artifact_id" => "runner-fixture", "probe" => "live", "scenario_cases" => [{"case_id" => "runner-case", "observed_values" => {"value" => 1}}]}; File.binwrite(ENV.fetch("IDENTITY_ACCEPTANCE_RAW_OUTPUT_PATH"), JSON.pretty_generate(payload) + "\n"); STDOUT.write([cache, home].join("\n"))'
      verifier_script = 'raw = File.binread(ENV.fetch("IDENTITY_ACCEPTANCE_VERIFIER_INPUT_PATH")); digest = "sha256:#{Digest::SHA256.hexdigest(raw)}"; path = ["scenario_cases", 0, "actual_outcome"]; value = "passed"; native = {"case_id" => "runner-case", "observed_value" => value}; payload = {"artifact_id" => "runner-fixture", "raw_capture_sha256" => digest, "assignments" => [{"path" => path, "value" => value}], "case_receipts" => [{"case_id" => "runner-case", "assignment_path" => path, "assignment_value_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(value))}", "raw_capture_sha256" => digest, "tool_native_row" => native, "tool_native_result_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(native.sort.to_h))}"}]}; File.binwrite(ENV.fetch("IDENTITY_ACCEPTANCE_VERIFIER_OUTPUT_PATH"), JSON.pretty_generate(payload) + "\n")'
      verifier_path = File.join(directory, "verifier.rb")
      File.binwrite(verifier_path, "require \"json\"\nrequire \"digest\"\n#{verifier_script}\n")
      plan = AcceptanceExecutionRunner.plan(
        producer_argv: [RbConfig.ruby, "-rjson", "-e", script],
        verifier_argv: [RbConfig.ruby, verifier_path],
        version_argv: [RbConfig.ruby, "--version"],
        environment_probe_argv: [RbConfig.ruby, "-e", 'STDOUT.write([RUBY_PLATFORM, RUBY_ENGINE].join("/"))']
      )
      fail_check("runner accepted identical producer and verifier argv") if plan.fetch("producer_argv") == plan.fetch("verifier_argv")
      receipt_path = File.join(directory, "execution-receipt.json")
      artifact_capture_path = File.join(directory, "artifact.json")
      input_manifest = [{"path_or_environment_id" => "runner-fixture", "kind" => "fixture",
                         "content_identity" => "sha256:#{'a' * 64}", "owner" => "acceptance-check",
                         "reason" => "prove coordinator-derived input identity"}]
      capture = AcceptanceExecutionRunner.run(
        plan: plan, chdir: IdentityPlatformAcceptance::REPOSITORY_ROOT, receipt_path: receipt_path,
        artifact_capture_path: artifact_capture_path, artifact_id: "runner-fixture",
        input_manifest: input_manifest,
        output_artifact_path: ".ai/identity-platform/evidence/artifacts/runner-fixture.json"
      )
      capture.execution.fetch("stdout").lines(chomp: true).each { |path| fail_check("coordinator runner retained disposable sandbox resource after success") if File.exist?(path) }
      fail_check("coordinator runner did not atomically persist its exact receipt") unless File.binread(receipt_path) == capture.receipt_bytes
      fail_check("coordinator runner did not derive and persist final evidence") unless File.binread(artifact_capture_path) == capture.artifact_bytes
      raw_payload = AcceptanceSchemaValidation.parse_json(capture.raw_capture_bytes, canonical: true)
      final_payload = AcceptanceSchemaValidation.parse_json(capture.artifact_bytes, canonical: true)
      fail_check("producer raw capture authored final execution evidence") if raw_payload.key?("execution")
      fail_check("independent verifier did not supply parsed result") unless final_payload.dig("scenario_cases", 0, "actual_outcome") == "passed"
      fail_check("runner did not bind executable identity") unless File.realpath(RbConfig.ruby) == capture.execution.fetch("executable_realpath")
      fail_check("runner accepted caller tool label") if capture.execution.key?("tool") || capture.execution.key?("environment")
      expected_input_root = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(input_manifest)))}"
      fail_check("runner did not derive tested revision") unless capture.execution.fetch("tested_revision") == `git rev-parse HEAD`.strip
      fail_check("runner did not derive input root") unless capture.execution.fetch("input_root") == expected_input_root
      original_path = ENV["PATH"]
      ENV["PATH"] = directory
      resolved_ruby = AcceptanceExecutionRunner.send(:resolve_executable, "ruby", IdentityPlatformAcceptance::REPOSITORY_ROOT)
      fail_check("executable identity used ambient PATH") unless resolved_ruby == File.realpath(RbConfig.ruby)
      ENV["PATH"] = original_path
      forged_verifier = AcceptanceSchemaValidation.parse_json(capture.verifier_bytes, canonical: true)
      forged_receipt = forged_verifier.fetch("case_receipts").first
      forged_receipt.fetch("tool_native_row")["assignment_path"] = ["forged"]
      forged_receipt["tool_native_result_sha256"] = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(forged_receipt.fetch('tool_native_row'))))}"
      forged_verifier_bytes = JSON.pretty_generate(forged_verifier) + "\n"
      begin
        AcceptanceExecutionRunner.send(:parse_verifier_attestation, forged_verifier_bytes, "runner-fixture", capture.raw_capture_bytes)
        fail_check("independent verifier accepted producer-selected assignment path")
      rescue ArgumentError => e
        raise unless e.message.include?("may not author assignment path")
      end
      forged_raw_path = File.join(directory, "producer-authored-observations.json")
      forged_attestation_path = File.join(directory, "forged-attestation.json")
      File.binwrite(forged_raw_path, JSON.pretty_generate({"artifact_id" => "affected-gate-report", "observation_rows" => [{"case_id" => "forged", "observed_value" => "pass"}]}) + "\n")
      _stdout, verifier_stderr, verifier_status = Open3.capture3(
        {"IDENTITY_ACCEPTANCE_VERIFIER_INPUT_PATH" => forged_raw_path,
         "IDENTITY_ACCEPTANCE_VERIFIER_OUTPUT_PATH" => forged_attestation_path},
        RbConfig.ruby, File.join(IdentityPlatformAcceptance::ROOT, "acceptance", "schema_validation.rb"),
        "verify", "affected-gate-report"
      )
      fail_check("independent verifier accepted producer-authored observation rows") if verifier_status.success?
      fail_check("independent verifier negative fixture failed for the wrong reason") unless verifier_stderr.include?("producer attempted to supply verifier observation rows")
      forged_discovery_plan = AcceptanceExecutionRunner.plan(
        producer_argv: plan.fetch("producer_argv"), verifier_argv: plan.fetch("verifier_argv"),
        version_argv: plan.fetch("version_argv"), environment_probe_argv: plan.fetch("environment_probe_argv"),
        package_discovery_argv: plan.fetch("producer_argv"), mutation_tool_argv: [RbConfig.ruby, "-e", "exit 0"]
      )
      begin
        AcceptanceExecutionRunner.run(
          plan: forged_discovery_plan, chdir: IdentityPlatformAcceptance::REPOSITORY_ROOT,
          receipt_path: File.join(directory, "forged-discovery-receipt.json"),
          artifact_capture_path: File.join(directory, "forged-discovery-artifact.json"), artifact_id: "coverage-mutation-report",
          input_manifest: input_manifest, output_artifact_path: ".ai/identity-platform/evidence/artifacts/coverage-mutation-report.json"
        )
        fail_check("coverage runner accepted producer-controlled discovery")
      rescue ArgumentError => e
        raise unless e.message.include?("result producer plan")
      end
      forged_mutation_plan = AcceptanceExecutionRunner.plan(
        producer_argv: plan.fetch("producer_argv"), verifier_argv: plan.fetch("verifier_argv"),
        version_argv: plan.fetch("version_argv"), environment_probe_argv: plan.fetch("environment_probe_argv"),
        package_discovery_argv: [RbConfig.ruby, "-e", 'STDOUT.write("{}\\n")'],
        mutation_tool_argv: plan.fetch("producer_argv")
      )
      begin
        AcceptanceExecutionRunner.run(
          plan: forged_mutation_plan, chdir: IdentityPlatformAcceptance::REPOSITORY_ROOT,
          receipt_path: File.join(directory, "forged-mutation-receipt.json"),
          artifact_capture_path: File.join(directory, "forged-mutation-artifact.json"), artifact_id: "coverage-mutation-report",
          input_manifest: input_manifest, output_artifact_path: ".ai/identity-platform/evidence/artifacts/coverage-mutation-report.json"
        )
        fail_check("coverage runner accepted result producer as mutation authority")
      rescue ArgumentError => e
        raise unless e.message.include?("result producer plan")
      end
      fail_check("coordinator runner did not own final execution evidence") unless final_payload.fetch("execution") == capture.execution
      fail_check("coordinator runner did not derive per-case transcript evidence") unless final_payload.dig("scenario_cases", 0, "transcript_sha256") == AcceptanceSchemaValidation.runner_transcript_sha256(final_payload.fetch("scenario_cases").first, capture.execution)
      unless AcceptanceExecutionRunner.live_capture_errors(capture.execution, capture, artifact_bytes: capture.artifact_bytes).empty?
        fail_check("live coordinator runner capture was rejected")
      end
      coherent_forgery = deep_copy(capture.execution)
      coherent_forgery["stdout"] = "FABRICATED: command was never executed\n"
      AcceptanceSchemaValidation.bind_execution_proof!(coherent_forgery)
      if AcceptanceExecutionRunner.live_capture_errors(coherent_forgery, capture, artifact_bytes: capture.artifact_bytes).empty?
        fail_check("live coordinator runner accepted coherently recomputed synthetic output")
      end
      synthetic_artifact = JSON.pretty_generate({"artifact_id" => "runner-fixture", "probe" => "synthetic", "execution" => capture.execution, "scenario_cases" => final_payload.fetch("scenario_cases")}) + "\n"
      if AcceptanceExecutionRunner.live_capture_errors(capture.execution, capture, artifact_bytes: synthetic_artifact).empty?
        fail_check("live coordinator runner accepted synthetic committed artifact semantics")
      end
      %w[status actual_outcome execution_outcome verified passed result].each do |forged_key|
        forged = JSON.pretty_generate({"artifact_id" => "runner-fixture", forged_key => (forged_key == "verified" ? true : "passed")}) + "\n"
        begin
          AcceptanceExecutionRunner.derive_final_artifact(forged, capture.execution, artifact_id: "runner-fixture", verifier_attestation: {})
          fail_check("coordinator runner accepted producer-authored #{forged_key}")
        rescue ArgumentError => e
          raise unless e.message.include?("may not author outcome field")
        end
      end
      failed_receipt = File.join(directory, "failed-receipt.json")
      failed_artifact = File.join(directory, "failed-artifact.json")
      failed_cache_record = File.join(directory, "failed-cache-path")
      failed_plan = AcceptanceExecutionRunner.plan(
        producer_argv: [RbConfig.ruby, "-e", 'File.binwrite(ARGV.fetch(0), ENV.fetch("GOCACHE")); raise unless File.directory?(ENV.fetch("GOCACHE")); exit 1', failed_cache_record], verifier_argv: plan.fetch("verifier_argv"),
        version_argv: plan.fetch("version_argv"), environment_probe_argv: plan.fetch("environment_probe_argv")
      )
      begin
        AcceptanceExecutionRunner.run(
        plan: failed_plan, chdir: IdentityPlatformAcceptance::REPOSITORY_ROOT,
        receipt_path: failed_receipt, artifact_capture_path: failed_artifact, artifact_id: "runner-fixture",
        input_manifest: input_manifest,
        output_artifact_path: ".ai/identity-platform/evidence/artifacts/runner-fixture.json"
        )
      rescue ArgumentError
        # A failed producer cannot yield verifier-authorized final evidence.
      end
      fail_check("failed producer execution persisted final evidence") if File.exist?(failed_receipt) || File.exist?(failed_artifact)
      failed_cache_path = File.binread(failed_cache_record)
      fail_check("coordinator runner retained disposable GOCACHE after failure") if File.exist?(failed_cache_path)
    end
  end

  def assert_negative_fixtures!(artifact, schema, record, evidence, receipt_bytes)
    evidence_bytes = JSON.pretty_generate(evidence) + "\n"
    mutations = {
      "missing observation" => [lambda { |value| value.fetch("observations").pop }, evidence_bytes],
      "foreign claim" => [lambda { |value| value.fetch("observations").first["claim_id"] = "identity.foreign.operation" }, evidence_bytes],
      "foreign artifact" => [lambda { |value| value["artifact_id"] = "foreign-artifact" }, evidence_bytes],
      "missing expected outcome" => [lambda { |value| value.fetch("observations").first.delete("expected_outcome") }, evidence_bytes],
      "missing actual outcome" => [lambda { |value| value.fetch("observations").first.delete("actual_outcome") }, evidence_bytes],
      "non-pass observation" => [lambda { |value| value.fetch("observations").first["result"] = "failed" }, evidence_bytes],
      "unbound observation hash" => [lambda { |value| value.fetch("observations").first["artifact_sha256"] = "sha256:#{'f' * 64}" }, evidence_bytes],
      "invalid input root" => [lambda { |value| value["input_root"] = "sha256:#{'e' * 64}" }, evidence_bytes]
    }
    mutations.each do |label, (mutation, bytes)|
      candidate = deep_copy(record)
      mutation.call(candidate)
      fail_check("negative fixture accepted: #{label}") if record_artifact_errors(candidate, artifact, schema, bytes, receipt_bytes).empty?
    end
    invalid_evidence = deep_copy(evidence)
    invalid_evidence.delete(invalid_evidence.keys.find { |key| artifact.fetch("artifact_evidence_fields").include?(key) })
    invalid_bytes = JSON.pretty_generate(invalid_evidence) + "\n"
    invalid_record = deep_copy(record)
    invalid_digest = "sha256:#{Digest::SHA256.hexdigest(invalid_bytes)}"
    invalid_record.fetch("artifact_hashes").first["sha256"] = invalid_digest
    invalid_record.fetch("observations").each { |row| row["artifact_sha256"] = invalid_digest }
    fail_check("negative fixture accepted: invalid artifact payload schema") if record_artifact_errors(invalid_record, artifact, schema, invalid_bytes, receipt_bytes).empty?
    wrong_path = deep_copy(record)
    wrong_path.fetch("artifact_hashes").first["path"] = ".ai/identity-platform/evidence/artifacts/wrong-artifact.json"
    fail_check("negative fixture accepted: wrong artifact payload path") if record_artifact_errors(wrong_path, artifact, schema, evidence_bytes, receipt_bytes).empty?
    fail_check("negative fixture accepted: absent artifact payload bytes") if record_artifact_errors(deep_copy(record), artifact, schema, nil, receipt_bytes).empty?
    fail_check("negative fixture accepted: absent execution receipt bytes") if record_artifact_errors(deep_copy(record), artifact, schema, evidence_bytes, nil).empty?
    compact_receipt = JSON.generate(AcceptanceSchemaValidation.parse_json(receipt_bytes))
    fail_check("negative fixture accepted: non-canonical execution receipt JSON") if record_artifact_errors(deep_copy(record), artifact, schema, evidence_bytes, compact_receipt).empty?
    duplicate_receipt = receipt_bytes.sub("{\n", "{\n  \"command\": \"duplicate\",\n")
    fail_check("negative fixture accepted: duplicate execution receipt JSON key") if record_artifact_errors(deep_copy(record), artifact, schema, evidence_bytes, duplicate_receipt).empty?
    digest_mismatch_bytes = evidence_bytes + "\n"
    fail_check("negative fixture accepted: artifact digest differs from bytes") if record_artifact_errors(deep_copy(record), artifact, schema, digest_mismatch_bytes, receipt_bytes).empty?
    malformed_bytes = "{\n"
    malformed_digest = "sha256:#{Digest::SHA256.hexdigest(malformed_bytes)}"
    malformed_record = deep_copy(record)
    malformed_record.fetch("artifact_hashes").first["sha256"] = malformed_digest
    malformed_record.fetch("observations").each { |row| row["artifact_sha256"] = malformed_digest }
    fail_check("negative fixture accepted: malformed artifact JSON") if record_artifact_errors(malformed_record, artifact, schema, malformed_bytes, receipt_bytes).empty?

    compact_bytes = JSON.generate(evidence)
    compact_record = deep_copy(record)
    compact_digest = "sha256:#{Digest::SHA256.hexdigest(compact_bytes)}"
    compact_record.fetch("artifact_hashes").first["sha256"] = compact_digest
    compact_record.fetch("observations").each { |row| row["artifact_sha256"] = compact_digest }
    fail_check("negative fixture accepted: non-canonical artifact JSON") if record_artifact_errors(compact_record, artifact, schema, compact_bytes, receipt_bytes).empty?

    duplicate_bytes = evidence_bytes.sub("{\n", "{\n  \"artifact_id\": #{JSON.generate(evidence.fetch('artifact_id'))},\n")
    duplicate_record = deep_copy(record)
    duplicate_digest = "sha256:#{Digest::SHA256.hexdigest(duplicate_bytes)}"
    duplicate_record.fetch("artifact_hashes").first["sha256"] = duplicate_digest
    duplicate_record.fetch("observations").each { |row| row["artifact_sha256"] = duplicate_digest }
    fail_check("negative fixture accepted: duplicate artifact JSON key") if record_artifact_errors(duplicate_record, artifact, schema, duplicate_bytes, receipt_bytes).empty?
  end

  def record_for_evidence(record, evidence)
    bytes = JSON.pretty_generate(evidence) + "\n"
    digest = "sha256:#{Digest::SHA256.hexdigest(bytes)}"
    candidate = deep_copy(record)
    candidate.fetch("artifact_hashes").find { |row| row.fetch("path") == candidate.fetch("artifact_hashes").first.fetch("path") }["sha256"] = digest
    candidate.fetch("observations").each { |row| row["artifact_sha256"] = digest }
    [candidate, bytes]
  end

  def assert_execution_semantic_negative_fixtures!(artifact, schema, record, evidence, receipt_bytes)
    forged_exit = deep_copy(evidence)
    forged_exit.fetch("execution")["exit_status"] = 1
    AcceptanceSchemaValidation.bind_execution_proof!(forged_exit.fetch("execution"))
    forged_record, forged_bytes = record_for_evidence(record, forged_exit)
    fail_check("#{artifact.fetch('artifact_id')} accepted self-declared pass after failed command") if record_artifact_errors(forged_record, artifact, schema, forged_bytes, receipt_bytes).empty?

    forged_identity = deep_copy(evidence)
    forged_identity.fetch("execution")["execution_identity"] = "sha256:#{'f' * 64}"
    identity_record, identity_bytes = record_for_evidence(record, forged_identity)
    fail_check("#{artifact.fetch('artifact_id')} accepted forged execution identity") if record_artifact_errors(identity_record, artifact, schema, identity_bytes, receipt_bytes).empty?

    coherent_forgery = deep_copy(evidence)
    coherent_forgery.fetch("execution")["stdout"] = "FABRICATED: command was never executed\n"
    AcceptanceSchemaValidation.bind_execution_proof!(coherent_forgery.fetch("execution"))
    AcceptanceSchemaValidation.bind_runner_transcripts!(coherent_forgery, coherent_forgery.fetch("execution"))
    coherent_record, coherent_bytes = record_for_evidence(record, coherent_forgery)
    coherent_receipt_bytes = AcceptanceSchemaValidation.execution_receipt_bytes(coherent_forgery.fetch("execution"))
    coherent_receipt_digest = "sha256:#{Digest::SHA256.hexdigest(coherent_receipt_bytes)}"
    coherent_record.fetch("artifact_hashes").find { |row| row.fetch("path") == coherent_forgery.dig("execution", "execution_receipt_path") }["sha256"] = coherent_receipt_digest
    structural_errors = record_artifact_errors(coherent_record, artifact, schema, coherent_bytes, coherent_receipt_bytes)
    fail_check("#{artifact.fetch('artifact_id')} coherent provenance fixture is malformed: #{structural_errors.first}") unless structural_errors.empty?
    if record_artifact_errors(coherent_record, artifact, schema, coherent_bytes, coherent_receipt_bytes, final_execution: true).empty?
      fail_check("#{artifact.fetch('artifact_id')} accepted coherent synthetic command execution without live runner capture")
    end

    semantic_rules = schema.dig("$defs", "artifact_evidence", "x-semantic-rules")
    progress_rule = semantic_rules.find { |rule| rule.fetch("kind") == "positive" } || semantic_rules.find { |rule| rule.fetch("kind") == "true" }
    fail_check("#{artifact.fetch('artifact_id')} has no non-zero semantic work rule") unless progress_rule
    semantic_mutations = []
    semantic_rules.each do |rule|
      case rule.fetch("kind")
      when "positive"
        rule.fetch("fields").each { |field| semantic_mutations << ["zero #{field}", field, 0] }
      when "zero"
        rule.fetch("fields").each { |field| semantic_mutations << ["nonzero #{field}", field, 1] }
      when "true"
        rule.fetch("fields").each { |field| semantic_mutations << ["false #{field}", field, false] }
      when "equal"
        left = rule.fetch("left")
        replacement = evidence.fetch(left).is_a?(Integer) ? evidence.fetch(left) + 1 : "sha256:#{'f' * 64}"
        semantic_mutations << ["unequal #{left}", left, replacement]
      when "const"
        field = rule.fetch("field")
        replacement = rule.fetch("value").is_a?(Integer) ? rule.fetch("value") - 1 : "invalid"
        semantic_mutations << ["wrong exact #{field}", field, replacement]
      end
    end
    semantic_mutations.each do |label, field, replacement|
      invalid = deep_copy(evidence)
      invalid[field] = replacement
      invalid_record, invalid_bytes = record_for_evidence(record, invalid)
      if record_artifact_errors(invalid_record, artifact, schema, invalid_bytes, receipt_bytes).empty?
        fail_check("#{artifact.fetch('artifact_id')} accepted semantic mutation: #{label}")
      end
    end

    assert_special_rejected = lambda do |label, invalid|
      invalid_record, invalid_bytes = record_for_evidence(record, invalid)
      if record_artifact_errors(invalid_record, artifact, schema, invalid_bytes, receipt_bytes).empty?
        fail_check("#{artifact.fetch('artifact_id')} accepted parity bypass: #{label}")
      end
    end
    case artifact.fetch("artifact_id")
    when "captcha-four-provider-report"
      count_only = deep_copy(evidence); count_only["provider_count"] = 1; count_only["verified_provider_count"] = 1
      assert_special_rejected.call("one-provider count proof", count_only)
      missing_provider_ids = deep_copy(evidence); missing_provider_ids["provider_ids"] = ["recaptcha"]
      assert_special_rejected.call("incomplete canonical provider IDs", missing_provider_ids)
      digest_only = deep_copy(evidence); digest_only["provider_results"] = [digest_only.fetch("provider_results").first]
      assert_special_rejected.call("digest-only provider matrix", digest_only)
      duplicate_provider = deep_copy(evidence); duplicate_provider.fetch("provider_results")[-1] = deep_copy(duplicate_provider.fetch("provider_results").first)
      assert_special_rejected.call("duplicate provider result", duplicate_provider)
      unverified_provider = deep_copy(evidence); unverified_provider.fetch("provider_results").first["verified"] = false
      assert_special_rejected.call("unverified provider result", unverified_provider)
      leaked_remote_ip = deep_copy(evidence); leaked_remote_ip.fetch("provider_results").first["remote_ip_sent"] = true
      assert_special_rejected.call("CAPTCHA remote IP sent without configured disclosure", leaked_remote_ip)
      unsupported_evidence = deep_copy(evidence); unsupported_evidence.fetch("provider_results").first["evidence_mode"] = "declaration"
      assert_special_rejected.call("self-declared provider result", unsupported_evidence)
      missing_target = deep_copy(evidence); missing_target["protected_target_results"].pop
      assert_special_rejected.call("missing CAPTCHA protected target", missing_target)
      extra_target = deep_copy(evidence); extra_target["protected_target_results"] << deep_copy(extra_target.fetch("protected_target_results").first).merge("target_id" => "identity.foreign.target")
      assert_special_rejected.call("extra CAPTCHA protected target", extra_target)
      swapped_context = deep_copy(evidence)
      first = swapped_context.fetch("protected_target_results").find { |row| row.fetch("target_id") == "identity.password.change" }
      second = swapped_context.fetch("protected_target_results").find { |row| row.fetch("target_id") == "identity.password.signup" }
      first["flow_contexts"], second["flow_contexts"] = second.fetch("flow_contexts"), first.fetch("flow_contexts")
      assert_special_rejected.call("swapped CAPTCHA target flow contexts", swapped_context)
      zero_targets = deep_copy(evidence); zero_targets["protected_target_count"] = 0; zero_targets["middleware_attached_target_count"] = 0
      assert_special_rejected.call("zero CAPTCHA protected targets", zero_targets)
    when "api-key-http-lifecycle-report"
      missing_reduction = deep_copy(evidence); missing_reduction.fetch("permission_reduction_results").pop
      assert_special_rejected.call("missing API-key permission reduction outcome", missing_reduction)
      reapplied = deep_copy(evidence); reapplied.fetch("permission_reduction_results").find { |row| row.fetch("case_id") == "narrow-unknown-reconciled" }["reapplication_count"] = 1
      assert_special_rejected.call("unknown API-key permission reduction reapplied", reapplied)
      mismatched = deep_copy(evidence); mismatched.fetch("permission_reduction_results").find { |row| row.fetch("case_id") == "unknown-fingerprint-mismatch" }["cascade_outcome"] = "reconciled-committed"
      assert_special_rejected.call("mismatched API-key fingerprint reconciled", mismatched)
      mismatch_event = deep_copy(evidence); mismatch_event.fetch("permission_reduction_results").find { |row| row.fetch("case_id") == "unknown-fingerprint-mismatch" }["event_type"] = "api_key.permissions_changed.v1"
      assert_special_rejected.call("mismatched API-key fingerprint emitted lifecycle event", mismatch_event)
      forged_input = deep_copy(evidence); forged_input.fetch("permission_reduction_results").find { |row| row.fetch("case_id") == "narrow-unknown-reconciled" }["query_canonical_input_sha256"] = "sha256:#{'f' * 64}"
      assert_special_rejected.call("API-key query fingerprint not bound to canonical input", forged_input)
      forged_fingerprint = deep_copy(evidence); forged_fingerprint.fetch("permission_reduction_results").find { |row| row.fetch("case_id") == "narrow-committed" }["stored_fingerprint"] = "sha256:#{'f' * 64}"
      assert_special_rejected.call("API-key stored fingerprint not canonically derived", forged_fingerprint)
    when "api-key-postgres-valkey-race-report"
      different_attempt = deep_copy(evidence); different_attempt.fetch("possibly_debited_results").last["journal_attempt_sha256"] = "sha256:#{'f' * 64}"
      assert_special_rejected.call("API-key debit reconciliation used different attempt", different_attempt)
      retried = deep_copy(evidence); retried.fetch("possibly_debited_results").first["retry_blocked"] = false
      assert_special_rejected.call("possibly-debited API-key verification retried", retried)
    when "hibp-interoperability-report"
      direct_only = deep_copy(evidence); direct_only["operation_ids"] = ["identity.risk.hibp-check"]
      assert_special_rejected.call("direct-check-only integration", direct_only)
      http_endpoint = deep_copy(evidence); http_endpoint["range_endpoint"] = "http://api.pwnedpasswords.com/range/{PREFIX}"
      assert_special_rejected.call("non-HTTPS range endpoint", http_endpoint)
      lowercase_prefix = deep_copy(evidence); lowercase_prefix["transmitted_prefix"] = "abcde"
      assert_special_rejected.call("lowercase prefix", lowercase_prefix)
      long_prefix = deep_copy(evidence); long_prefix["transmitted_prefix"] = "ABCDEF"
      assert_special_rejected.call("non-five-character prefix", long_prefix)
      secret_user_agent = deep_copy(evidence); secret_user_agent["user_agent"] = "Bearer secret-token"
      assert_special_rejected.call("secret-shaped User-Agent", secret_user_agent)
      followed_redirect = deep_copy(evidence); followed_redirect["redirect_follow_count"] = 1
      assert_special_rejected.call("redirect followed", followed_redirect)
      padding_disabled = deep_copy(evidence); padding_disabled["add_padding_header"] = "false"
      assert_special_rejected.call("Add-Padding disabled", padding_disabled)
      unpadded = deep_copy(evidence); unpadded["padded_response_valid"] = false
      assert_special_rejected.call("response padding absent", unpadded)
      missing_compromised = deep_copy(evidence); missing_compromised["outcome_cases"].reject! { |row| row.fetch("outcome_id") == "known-compromised" }
      assert_special_rejected.call("missing known-compromised HIBP case", missing_compromised)
      missing_absent = deep_copy(evidence); missing_absent["outcome_cases"].reject! { |row| row.fetch("outcome_id") == "absent-no-match" }
      assert_special_rejected.call("missing absent/no-match HIBP case", missing_absent)
      swapped_outcomes = deep_copy(evidence); swapped_outcomes.fetch("outcome_cases").each { |row| row["outcome_id"] = row.fetch("outcome_id") == "known-compromised" ? "absent-no-match" : "known-compromised" }
      assert_special_rejected.call("swapped HIBP outcome evidence", swapped_outcomes)
      zero_match = deep_copy(evidence); zero_match["known_compromised_match_count"] = 0
      assert_special_rejected.call("zero known-compromised suffix matches", zero_match)
      false_absence = deep_copy(evidence); false_absence["absent_match_count"] = 1
      assert_special_rejected.call("nonzero absent/no-match suffix matches", false_absence)
      %w[password_transmission_count full_hash_transmission_count user_agent_secret_match_count redirect_follow_count].each do |field|
        privacy_bypass = deep_copy(evidence); privacy_bypass.fetch("outcome_cases").first[field] = 1
        assert_special_rejected.call("HIBP per-case #{field} privacy bypass", privacy_bypass)
      end
      timeout_allow = deep_copy(evidence); timeout_allow["timeout_status"] = "allow"
      assert_special_rejected.call("timeout allow", timeout_allow)
      %w[password_signup_denied password_set_denied password_change_denied password_reset_denied admin_password_set_denied].each do |field|
        bypass = deep_copy(evidence); bypass[field] = false
        assert_special_rejected.call("#{field} bypass", bypass)
      end
    when "oauth-oidc-conformance-report"
      omitted_endpoint = deep_copy(evidence); omitted_endpoint["operation_ids"] = omitted_endpoint.fetch("operation_ids").reject { |id| id == "identity.oauth-server.introspect" }
      assert_special_rejected.call("omitted OAuth endpoint execution", omitted_endpoint)
      %w[identity.oauth-server.continue identity.oauth-server.session-token].each do |operation_id|
        omitted = deep_copy(evidence); omitted["operation_ids"] = omitted.fetch("operation_ids").reject { |id| id == operation_id }
        assert_special_rejected.call("omitted #{operation_id} execution", omitted)
      end
      count_only = deep_copy(evidence); count_only["endpoint_operation_ids"] = []
      assert_special_rejected.call("count-only OAuth endpoint proof", count_only)
      omitted_claim = deep_copy(evidence); omitted_claim["interoperability_claim_ids"] = omitted_claim.fetch("interoperability_claim_ids")[0...-1]
      assert_special_rejected.call("omitted OAuth interoperability claim", omitted_claim)
      %w[continue_verified session_token_verified introspection_active_verified introspection_inactive_verified revocation_verified dynamic_registration_verified oauth_discovery_verified oidc_discovery_verified jwks_verified logout_verified protected_resource_metadata_verified].each do |field|
        bypass = deep_copy(evidence); bypass[field] = false
        assert_special_rejected.call("#{field} bypass", bypass)
      end
      missing_grant = deep_copy(evidence); missing_grant.fetch("grant_replay_results").pop
      assert_special_rejected.call("missing OAuth grant replay profile", missing_grant)
      unbound_replay = deep_copy(evidence); unbound_replay.fetch("grant_replay_results").first["replay_credential_sha256"] = "sha256:#{'f' * 64}"
      assert_special_rejected.call("OAuth replay used a different credential", unbound_replay)
    when "oauth-provider-matrix-report"
      missing_logout = deep_copy(evidence); missing_logout.fetch("social_logout_results").pop
      assert_special_rejected.call("missing social logout reconciliation case", missing_logout)
    when "mfa-recovery-report"
      missing_totp = deep_copy(evidence); missing_totp.fetch("totp_profile_results").pop
      assert_special_rejected.call("missing selected TOTP profile evidence", missing_totp)
      no_app = deep_copy(evidence); no_app.fetch("totp_profile_results").first["independent_authenticator_app"] = ""
      assert_special_rejected.call("missing independent authenticator app", no_app)
    when "sso-provider-lifecycle-report"
      omitted_endpoint = deep_copy(evidence); omitted_endpoint["operation_ids"] = omitted_endpoint.fetch("operation_ids").reject { |id| id == "identity.sso.saml-slo" }
      assert_special_rejected.call("omitted SAML endpoint execution", omitted_endpoint)
      %w[identity.sso.oauth-callback identity.sso.oidc-callback].each do |operation_id|
        omitted = deep_copy(evidence); omitted["operation_ids"] = omitted.fetch("operation_ids").reject { |id| id == operation_id }
        assert_special_rejected.call("omitted #{operation_id} execution", omitted)
      end
      count_only = deep_copy(evidence); count_only["saml_endpoint_ids"] = []
      assert_special_rejected.call("count-only SAML endpoint proof", count_only)
      %w[oauth_callback_verified oidc_callback_verified saml_metadata_verified saml_start_verified saml_acs_verified saml_idp_init_verified saml_logout_start_verified saml_slo_verified].each do |field|
        bypass = deep_copy(evidence); bypass[field] = false
        assert_special_rejected.call("#{field} bypass", bypass)
      end
    when "passkey-browser-ceremony-report"
      omitted_lifecycle = deep_copy(evidence); omitted_lifecycle["operation_ids"] = omitted_lifecycle.fetch("operation_ids").reject { |id| id == "identity.passkey.delete" }
      assert_special_rejected.call("omitted discoverable passkey lifecycle operation", omitted_lifecycle)
      mismatched_lifecycle = deep_copy(evidence); mismatched_lifecycle["listed_credential_id"] = "different-credential"
      assert_special_rejected.call("mismatched discoverable passkey lifecycle credential", mismatched_lifecycle)
      nondiscoverable = deep_copy(evidence); nondiscoverable["discoverable_required"] = false
      assert_special_rejected.call("non-discoverable passkey lifecycle", nondiscoverable)
      zero_lifecycle = deep_copy(evidence); zero_lifecycle["discoverable_lifecycle_step_count"] = 0
      assert_special_rejected.call("zero discoverable passkey lifecycle", zero_lifecycle)
      missing_action = deep_copy(evidence); missing_action["security_action_ids"].pop
      assert_special_rejected.call("missing passkey security action ID", missing_action)
      missing_transition = deep_copy(evidence); missing_transition["security_action_transitions"].pop
      assert_special_rejected.call("missing passkey security action transition", missing_transition)
      swapped_actions = deep_copy(evidence)
      creation = swapped_actions.fetch("security_action_transitions").find { |row| row.fetch("transition") == "committed-creation" }
      compromise = swapped_actions.fetch("security_action_transitions").find { |row| row.fetch("transition") == "confirmed-compromise" }
      creation["action_id"], compromise["action_id"] = compromise.fetch("action_id"), creation.fetch("action_id")
      assert_special_rejected.call("swapped passkey transition security actions", swapped_actions)
    when "webauthn-conformance-report"
      missing_backup_case = deep_copy(evidence); missing_backup_case["backup_state_matrix"].pop
      assert_special_rejected.call("missing BE/BS matrix case", missing_backup_case)
      invalid_accepted = deep_copy(evidence); invalid_accepted.fetch("backup_state_matrix").find { |row| row.fetch("case_id") == "invalid-bs-without-be" }["accepted"] = true
      assert_special_rejected.call("BE false BS true accepted", invalid_accepted)
      swapped_flags = deep_copy(evidence)
      valid = swapped_flags.fetch("backup_state_matrix").find { |row| row.fetch("case_id") == "non-backup-valid" }
      invalid = swapped_flags.fetch("backup_state_matrix").find { |row| row.fetch("case_id") == "invalid-bs-without-be" }
      valid["backup_state"], invalid["backup_state"] = invalid.fetch("backup_state"), valid.fetch("backup_state")
      assert_special_rejected.call("swapped BE/BS semantics", swapped_flags)
      missing_path = deep_copy(evidence); missing_path["authenticator_path_results"].reject! { |row| row.fetch("path_id") == "backup-eligible" }
      assert_special_rejected.call("missing backup authenticator path", missing_path)
      equal_allowed = deep_copy(evidence); equal_allowed.fetch("signature_counter_matrix").find { |row| row.fetch("case_id") == "non-backup-positive-equal" }["disposition"] = "accept-and-advance"
      assert_special_rejected.call("non-backup equal counter accepted", equal_allowed)
      decrease_rollback = deep_copy(evidence); decrease_rollback.fetch("signature_counter_matrix").find { |row| row.fetch("case_id") == "backup-positive-decrease" }["persisted_count"] = 1
      assert_special_rejected.call("backup counter rolled back", decrease_rollback)
      zero_denied = deep_copy(evidence); zero_denied.fetch("signature_counter_matrix").find { |row| row.fetch("case_id") == "non-backup-zero-zero" }["disposition"] = "deny-suspected-clone-or-reset"
      assert_special_rejected.call("unsupported zero counter denied", zero_denied)
      missed_advance = deep_copy(evidence); missed_advance.fetch("signature_counter_matrix").find { |row| row.fetch("case_id") == "non-backup-positive-increase" }["persisted_count"] = 1
      assert_special_rejected.call("positive counter increase not persisted", missed_advance)
      missing_profile = deep_copy(evidence); missing_profile.fetch("selected_profile_results").pop
      assert_special_rejected.call("missing selected WebAuthn profile evidence", missing_profile)
    when "browser-security-report"
      missing_operation = deep_copy(evidence); missing_operation.fetch("operation_security_results").pop
      assert_special_rejected.call("missing cookie or CSRF operation result", missing_operation)
    when "secret-redaction-report"
      missing_pair = deep_copy(evidence); missing_pair.fetch("class_surface_results").pop
      assert_special_rejected.call("missing redaction class/surface pair", missing_pair)
    when "risk-policy-report"
      missing_action = deep_copy(evidence); missing_action.fetch("action_results").pop
      assert_special_rejected.call("missing canonical risk action", missing_action)
      alias_action = deep_copy(evidence); alias_action.fetch("action_results").first["action"] = "challenge"
      assert_special_rejected.call("risk action alias accepted", alias_action)
    when "sso-enforcement-break-glass-report"
      weakened = deep_copy(evidence); weakened.fetch("safety_results").find { |row| row.fetch("case_id") == "consume-once" }["policy_weakened"] = true
      assert_special_rejected.call("break-glass weakened enforcement", weakened)
      different_grant = deep_copy(evidence); different_grant.fetch("safety_results").find { |row| row.fetch("case_id") == "consume-once" }["grant_id"] = "different-grant"
      assert_special_rejected.call("break-glass consume not bound to issued grant", different_grant)
    when "operations-runbook-index"
      missing_action = deep_copy(evidence); missing_action.fetch("action_results").pop
      assert_special_rejected.call("missing operational action row", missing_action)
      reused_reference = deep_copy(evidence); rows = reused_reference.fetch("action_results"); rows[1]["procedure_ref"] = rows[0].fetch("procedure_ref")
      assert_special_rejected.call("runbook reused another action reference", reused_reference)
    end

    if evidence.key?("provider_results") && %w[oauth-provider-matrix-report provider-evidence-index].include?(artifact.fetch("artifact_id"))
      missing = deep_copy(evidence); missing.fetch("provider_results").pop
      assert_special_rejected.call("missing canonical provider row", missing)
      duplicate = deep_copy(evidence); duplicate.fetch("provider_results")[-1] = deep_copy(duplicate.fetch("provider_results").first)
      assert_special_rejected.call("duplicate canonical provider row", duplicate)
      first = evidence.fetch("provider_results").first
      forged_verified = deep_copy(evidence); forged_verified.fetch("provider_results").first["verified"] = !first.fetch("verified")
      assert_special_rejected.call("provider verification differs from canonical catalog readiness", forged_verified)
      forged_outcome = deep_copy(evidence); forged_outcome.fetch("provider_results").first["execution_outcome"] = first.fetch("execution_outcome") == "passed" ? "not-run" : "passed"
      assert_special_rejected.call("provider execution differs from canonical catalog readiness", forged_outcome)
      forged_pin = deep_copy(evidence); forged_pin.fetch("provider_results").first["official_docs_status"] = "verified-attributable"
      assert_special_rejected.call("provider official pin differs from canonical catalog", forged_pin)
      forged_blocker = deep_copy(evidence); forged_blocker.fetch("provider_results").first["catalog_blocker_count"] = first.fetch("catalog_blocker_count") + 1
      assert_special_rejected.call("provider blocker count differs from canonical catalog", forged_blocker)
    end
    if evidence.key?("protocol_case_results")
      missing = deep_copy(evidence); missing.fetch("protocol_case_results").pop
      assert_special_rejected.call("missing selected protocol case", missing)
      substituted = deep_copy(evidence); substituted.fetch("protocol_case_results").first["requirement_id"] = "foreign.requirement"
      assert_special_rejected.call("substituted protocol requirement", substituted)
      wrong_outcome = deep_copy(evidence); wrong_outcome.fetch("protocol_case_results").first["outcome"] = "self-declared"
      assert_special_rejected.call("invalid protocol closure", wrong_outcome)
    end
    if evidence.key?("external_profile_results")
      missing = deep_copy(evidence); missing.fetch("external_profile_results").pop
      assert_special_rejected.call("missing exact external profile execution", missing)
      missing_consumer = deep_copy(evidence); missing_consumer.fetch("external_profile_results").first.fetch("consumer_ids").pop
      assert_special_rejected.call("missing external profile consumer claim", missing_consumer)
      first = evidence.fetch("external_profile_results").first
      forged_outcome = deep_copy(evidence); forged_outcome.fetch("external_profile_results").first["execution_outcome"] = first.fetch("execution_outcome") == "passed" ? "not-run" : "passed"
      assert_special_rejected.call("external profile execution differs from canonical readiness", forged_outcome)
    end
    %w[cascade_transitions delivery_outcome_cases reconciliation_cases otp_operation_cases bearer_transition_cases api_key_security_cases session_race_cases trusted_profile_cases issuer_variant_cases repeat_sync_cases redirect_link_cases authority_transition_cases wire_contract_cases grant_replay_results social_logout_results totp_profile_results selected_profile_results operation_security_results class_surface_results safety_results action_results permission_reduction_results command_id_integrity_results audit_stage_results scim_bulk_skip_results possibly_debited_results session_authority_cache_results privacy_lifecycle_results provider_cascade_results connection_cascade_results].each do |field|
      next unless evidence.key?(field)

      missing = deep_copy(evidence); missing.fetch(field).pop
      assert_special_rejected.call("missing artifact-specific #{field} case", missing)
      inconsistent = deep_copy(evidence); inconsistent.fetch(field).first["case_id"] = "coherent-forgery"
      assert_special_rejected.call("cross-field inconsistent #{field} case", inconsistent)
    end
    if artifact.fetch("artifact_id") == "operation-handler-openapi-bijection"
      %w[catalog_operation_ids handler_operation_ids openapi_operation_ids operation_bindings].each do |field|
        missing = deep_copy(evidence); missing.fetch(field).pop
        assert_special_rejected.call("missing #{field} member", missing)
      end
      substituted = deep_copy(evidence); substituted.fetch("catalog_operation_ids")[0] = "identity.foreign.operation"
      assert_special_rejected.call("substituted canonical operation", substituted)
      duplicate = deep_copy(evidence); duplicate.fetch("operation_bindings")[-1] = deep_copy(duplicate.fetch("operation_bindings").first)
      assert_special_rejected.call("duplicate handler binding", duplicate)
    end

    return unless artifact.fetch("artifact_id") == "coverage-mutation-report"

    incomplete_coverage = deep_copy(evidence)
    incomplete_coverage["statement_coverage_basis_points"] = 1
    incomplete_record, incomplete_bytes = record_for_evidence(record, incomplete_coverage)
    fail_check("coverage-mutation-report accepted less than exact statement coverage") if record_artifact_errors(incomplete_record, artifact, schema, incomplete_bytes, receipt_bytes).empty?
    missing_package = deep_copy(evidence)
    missing_package.fetch("affected_package_results").pop
    missing_record, missing_bytes = record_for_evidence(record, missing_package)
    fail_check("coverage-mutation-report accepted an incomplete affected-package inventory") if record_artifact_errors(missing_record, artifact, schema, missing_bytes, receipt_bytes).empty?
    duplicate_mutant_id = deep_copy(evidence)
    duplicate = deep_copy(duplicate_mutant_id.fetch("viable_mutant_results").first)
    duplicate["source_locator"] = "different-source-locator"
    duplicate_mutant_id.fetch("viable_mutant_results") << duplicate
    duplicate_mutant_id["viable_mutant_count"] += 1
    duplicate_mutant_id["killed_mutant_count"] += 1
    duplicate_record, duplicate_bytes = record_for_evidence(record, duplicate_mutant_id)
    fail_check("coverage-mutation-report accepted duplicate mutant identity") if record_artifact_errors(duplicate_record, artifact, schema, duplicate_bytes, receipt_bytes).empty?
  end

  def assert_runner_derivation_contract!(artifact, schema, evidence)
    raw = deep_copy(evidence)
    execution = raw.delete("execution")
    raw.each_value do |value|
      next unless value.is_a?(Array)

      value.each { |row| row.delete("transcript_sha256") if row.is_a?(Hash) }
    end
    assignments = []
    extract = lambda do |value, path|
      case value
      when Hash
        value.keys.each do |key|
          if AcceptanceExecutionRunner::FORBIDDEN_PRODUCER_KEY.match?(key)
            assigned = value.delete(key)
            if assigned.is_a?(Array)
              assigned.each { |row| row.delete("transcript_sha256") if row.is_a?(Hash) }
            end
            assignments << {"path" => path + [key], "value" => assigned}
          else
            extract.call(value.fetch(key), path + [key])
          end
        end
      when Array then value.each_with_index { |entry, index| extract.call(entry, path + [index]) }
      end
    end
    extract.call(raw, [])
    if artifact.fetch("artifact_id") == "coverage-mutation-report"
      raw.delete("affected_package_manifest_sha256")
      raw.delete("viable_mutant_manifest_sha256")
      package_manifest_bytes = JSON.pretty_generate(
        {"derivation" => "modules-and-packages-manifests-with-reverse-dependants",
         "source_manifest_sha256s" => {"modules.json" => "sha256:#{'a' * 64}", "packages.json" => "sha256:#{'b' * 64}"},
         "affected_module_directories" => [], "reverse_dependants" => {},
         "package_ids" => evidence.fetch("affected_package_results").map { |row| row.fetch("package_id") }}
      ) + "\n"
      mutant_manifest_bytes = JSON.pretty_generate(
        {"derivation" => "pinned-mutation-tool-output", "mutation_tool_path" => File.realpath(File.join(IdentityPlatformAcceptance::REPOSITORY_ROOT, ".ai/identity-platform/acceptance/schema_validation.rb")), "mutation_tool_sha256" => "sha256:#{Digest::SHA256.hexdigest(File.binread(File.join(IdentityPlatformAcceptance::REPOSITORY_ROOT, '.ai/identity-platform/acceptance/schema_validation.rb')))}",
         "mutation_output_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical({"mutants" => evidence.fetch('viable_mutant_results').map { |row| row.slice('mutant_id', 'package_id', 'operator', 'source_locator', 'outcome') }})))}",
         "mutation_output" => {"mutants" => evidence.fetch("viable_mutant_results").map { |row| row.slice("mutant_id", "package_id", "operator", "source_locator", "outcome") }},
         "mutants" => evidence.fetch("viable_mutant_results").map { |row| row.slice("mutant_id", "package_id", "operator", "source_locator", "outcome") }}
      ) + "\n"
      execution["package_manifest_sha256"] = "sha256:#{Digest::SHA256.hexdigest(package_manifest_bytes)}"
      execution["mutant_manifest_sha256"] = "sha256:#{Digest::SHA256.hexdigest(mutant_manifest_bytes)}"
      execution["package_discovery_argv"] = ["ruby", ".ai/identity-platform/acceptance/schema_validation.rb", "discover-packages", artifact.fetch("artifact_id")]
      execution["mutation_tool_argv"] = ["ruby", ".ai/identity-platform/acceptance/schema_validation.rb", "mutation-tool", "pkg/identity/reference"]
      execution["mutation_tool_output_sha256"] = "sha256:#{Digest::SHA256.hexdigest(JSON.pretty_generate({"mutants" => evidence.fetch("viable_mutant_results").map { |row| row.slice("mutant_id", "package_id", "operator", "source_locator", "outcome") }}) + "\n")}"
      AcceptanceSchemaValidation.bind_execution_proof!(execution)
    end
    raw_bytes = JSON.pretty_generate(raw) + "\n"
    execution["raw_capture_sha256"] = "sha256:#{Digest::SHA256.hexdigest(raw_bytes)}"
    AcceptanceSchemaValidation.bind_execution_proof!(execution)
    verifier_attestation = {
      "artifact_id" => artifact.fetch("artifact_id"), "raw_capture_sha256" => execution.fetch("raw_capture_sha256"),
      "assignments" => assignments,
      "case_receipts" => [{"case_id" => "artifact-verifier", "raw_capture_sha256" => execution.fetch("raw_capture_sha256"), "tool_native_result_sha256" => "sha256:#{'f' * 64}"}]
    }
    verifier_attestation["case_receipts"] = assignments.map.with_index do |assignment, index|
      native = {"assignment_index" => index, "observed_value" => assignment.fetch("value")}
      {"case_id" => "artifact-verifier-#{index}", "assignment_path" => assignment.fetch("path"),
       "assignment_value_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(assignment.fetch('value'))))}",
       "raw_capture_sha256" => execution.fetch("raw_capture_sha256"), "tool_native_row" => native,
       "tool_native_result_sha256" => "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(native)))}"}
    end
    incomplete_attestation = deep_copy(verifier_attestation) if artifact.fetch("artifact_id") == "coverage-mutation-report"
    final_bytes = AcceptanceExecutionRunner.derive_final_artifact(
      raw_bytes, execution, artifact_id: artifact.fetch("artifact_id"),
      verifier_attestation: verifier_attestation,
      package_manifest_bytes: package_manifest_bytes, mutant_manifest_bytes: mutant_manifest_bytes
    )
    final = AcceptanceSchemaValidation.parse_json(final_bytes, canonical: true)
    errors = schema_errors(final, schema.dig("$defs", "artifact_evidence"))
    fail_check("#{artifact.fetch('artifact_id')} runner-derived final evidence violates schema: #{errors.first}") unless errors.empty?
    fail_check("#{artifact.fetch('artifact_id')} runner did not exclusively own final execution evidence") unless final.fetch("execution") == execution
    if artifact.fetch("artifact_id") == "coverage-mutation-report"
      incomplete_raw = JSON.parse(raw_bytes)
      incomplete_raw.fetch("affected_package_results").pop
      begin
        AcceptanceExecutionRunner.derive_final_artifact(
          JSON.pretty_generate(incomplete_raw) + "\n", execution, artifact_id: artifact.fetch("artifact_id"),
          verifier_attestation: incomplete_attestation,
          package_manifest_bytes: package_manifest_bytes, mutant_manifest_bytes: mutant_manifest_bytes
        )
        fail_check("coverage runner accepted coherent omission from independently captured package manifest")
      rescue ArgumentError => e
        raise unless e.message.include?("independently captured package manifest")
      end
      surviving_manifest = deep_copy(AcceptanceSchemaValidation.parse_json(mutant_manifest_bytes, canonical: true))
      surviving_manifest.fetch("mutants").first["outcome"] = "survived"
      surviving_manifest.fetch("mutation_output").fetch("mutants").first["outcome"] = "survived"
      surviving_manifest["mutation_output_sha256"] = "sha256:#{Digest::SHA256.hexdigest(JSON.generate(canonical(surviving_manifest.fetch('mutation_output'))))}"
      begin
        surviving_raw = JSON.parse(raw_bytes)
        runner_transcript_fields = AcceptanceExecutionRunner.send(:runner_transcript_fields, artifact.fetch("artifact_id"), surviving_raw)
        runner_transcript_fields.each { |field| surviving_raw.fetch(field, []).each { |row| row.delete("transcript_sha256") } }
        AcceptanceExecutionRunner.derive_final_artifact(
          JSON.pretty_generate(surviving_raw) + "\n", execution, artifact_id: artifact.fetch("artifact_id"),
          verifier_attestation: deep_copy(verifier_attestation),
          package_manifest_bytes: package_manifest_bytes, mutant_manifest_bytes: JSON.pretty_generate(surviving_manifest) + "\n"
        )
        fail_check("coverage runner accepted a viable mutant survivor from the independent tool")
      rescue ArgumentError => e
        raise unless e.message.include?("viable survivor")
      end
      %w[omission invention].each do |mutation_kind|
        forged_manifest = deep_copy(AcceptanceSchemaValidation.parse_json(mutant_manifest_bytes, canonical: true))
        if mutation_kind == "omission"
          forged_manifest.fetch("mutants").pop
        else
          forged = deep_copy(forged_manifest.fetch("mutants").first)
          forged["mutant_id"] = "sha256:#{'9' * 64}"
          forged_manifest.fetch("mutants") << forged
        end
        begin
          AcceptanceExecutionRunner.send(:parse_inventory_manifest, JSON.pretty_generate(forged_manifest) + "\n", "mutant")
          fail_check("mutation wrapper accepted native-row #{mutation_kind}")
        rescue ArgumentError => e
          raise unless e.message.include?("native mutation output")
        end
      end
    end
  end

  def run
    assert_live_runner_contract!
    source = load_canonical(IdentityPlatformAcceptance::SOURCE)
    goals = load_canonical(IdentityPlatformAcceptance::GOALS)
    public_contracts = load_canonical(IdentityPlatformAcceptance::PUBLIC_CONTRACTS)
    operations = load_canonical(IdentityPlatformAcceptance::OPERATIONS)
    runbook_catalog = load_canonical(IdentityPlatformAcceptance::RUNBOOK_CATALOG)
    catalog = load_canonical(IdentityPlatformAcceptance::CATALOG)
    program_units = goals.fetch("goals").map { |goal| goal.fetch("unit") }
    product_units = public_contracts.fetch("units").map { |unit| unit.fetch("unit") }
    operation_rows = operations.fetch("operations")
    operation_by_id = operation_rows.to_h { |operation| [operation.fetch("id"), operation] }
    fail_check("expected current 67-unit program catalog, got #{program_units.length}") unless program_units.length == 67 && program_units.uniq == program_units
    fail_check("expected current 61-unit product-contract catalog, got #{product_units.length}") unless product_units.length == 61 && product_units.uniq == product_units
    primitive_units = IdentityPlatformAcceptance::PRIMITIVE_PREREQUISITES.map { |row| row.fetch("unit") }
    fail_check("program/product unit distinction drifted") unless (program_units - product_units).sort == primitive_units.sort && (product_units - program_units).empty?
    fail_check("operation catalog contains duplicate IDs") unless operation_rows.length == operation_by_id.length
    fail_check("OPERATIONS_RUNBOOK_CATALOG.json drifted from its canonical generator") unless runbook_catalog == IdentityPlatformAcceptance.runbook_catalog_document
    unless IdentityPlatformAcceptance.api_operation_ids.sort_by(&:b) == operation_rows.map { |operation| operation.fetch("id") }.sort_by(&:b)
      fail_check("API operation catalog differs from canonical operation semantics")
    end
    expected_catalog = IdentityPlatformAcceptance.catalog_document
    fail_check("ACCEPTANCE_ARTIFACTS.json drifted from its sources") unless catalog == expected_catalog
    executable_operation_ids = catalog.fetch("artifacts").flat_map do |artifact|
      artifact.fetch("covered_operations").filter_map { |operation| operation.fetch("id") if operation.fetch("kind") == "platform_operation" }
    end.uniq
    fail_check("executable artifact operation closure differs from API catalog") unless executable_operation_ids.sort_by(&:b) == IdentityPlatformAcceptance.api_operation_ids.sort_by(&:b)
    source_claims = (source.fetch("journeys") + source.fetch("cross_cutting")).map { |claim| claim.fetch("id") }
    source_artifacts = source.fetch("artifact_catalog").to_h { |artifact| [artifact.fetch("id"), artifact] }
    fail_check("expected exact 19-journey closure") unless source.fetch("journeys").map { |journey| journey.fetch("number") } == (1..19).to_a
    fail_check("audit-investigation journey artifact drifted") unless source.fetch("journeys").find { |journey| journey.fetch("id") == "journey.audit-investigation.v1" }&.fetch("artifacts") == ["operational-api-redaction-report"]
    REQUIRED_OPERATION_EVIDENCE.each do |artifact_id, required_operations|
      declared = source_artifacts.fetch(artifact_id).fetch("operation_claims", [])
      fail_check("#{artifact_id} omits required operation evidence") unless (required_operations - declared).empty?
    end
    captcha_claims = source_artifacts.fetch("captcha-four-provider-report").fetch("operation_claims")
    fail_check("captcha-four-provider-report target claims differ from canonical API attachment table") unless captcha_claims == IdentityPlatformAcceptance.captcha_protected_target_ids
    goal_by_unit = goals.fetch("goals").to_h { |goal| [goal.fetch("unit"), goal] }
    catalog.fetch("primitive_prerequisites").each do |prerequisite|
      unit = prerequisite.fetch("unit")
      goal = goal_by_unit.fetch(unit) { fail_check("missing primitive prerequisite goal #{unit}") }
      fail_check("#{unit} prerequisite goal path drifted") unless prerequisite.fetch("goal_path") == goal.fetch("planning_path")
      fail_check("#{unit} prerequisite gate list is empty") unless prerequisite.fetch("module_gates").is_a?(Array) && !prerequisite.fetch("module_gates").empty?
      fail_check("#{unit} is incorrectly declared as an artifact producer") unless prerequisite.fetch("produces_end_state_artifacts") == false
      fail_check("#{unit} does not gate artifact producers") unless prerequisite.fetch("required_before_artifact_producers") == true
    end
    fail_check("artifact profiles are incomplete or stale") unless IdentityPlatformAcceptance.profiles.keys.sort == source_artifacts.keys.sort
    evidence_contract_digests = []
    description_owners = {}
    catalog.fetch("artifacts").each do |artifact|
      id = artifact.fetch("artifact_id")
      upstream = source_artifacts.fetch(id) { fail_check("unknown artifact #{id}") }
      profile = IdentityPlatformAcceptance.profiles.fetch(id)
      fail_check("#{id} has unknown product-contract producer unit") unless product_units.include?(artifact.dig("producer", "unit"))
      fail_check("#{id} incorrectly assigns a primitive artifact producer") if primitive_units.include?(artifact.dig("producer", "unit"))
      fail_check("#{id} producer owner drifted") unless artifact.dig("producer", "unit") == upstream.fetch("producer_unit")
      fail_check("#{id} producer gate drifted") unless artifact.dig("producer", "operation") == upstream.fetch("gate")
      fail_check("#{id} output path drifted") unless artifact.fetch("canonical_output_path") == upstream.fetch("path")
      artifact.fetch("covered_operations").each do |covered|
        if covered.fetch("kind") == "repository_gate"
          fail_check("#{id} repository gate owner drifted") unless covered.fetch("owners") == [upstream.fetch("producer_unit")]
        else
          operation = operation_by_id.fetch(covered.fetch("id")) { fail_check("#{id} uses unknown operation #{covered.fetch('id')}") }
          fail_check("#{id} operation owner drifted for #{covered.fetch('id')}") unless covered.fetch("owners") == operation.fetch("owners")
        end
      end
      artifact.fetch("claim_ids").each do |claim_id|
        fail_check("#{id} references unknown claim #{claim_id}") unless source_claims.include?(claim_id) || operation_by_id.key?(claim_id)
      end
      required = artifact.fetch("required_observations")
      fail_check("#{id} authoritative observation IDs drifted") unless required.map { |row| row.fetch("observation_id") } == upstream.fetch("observation_ids")
      fail_check("#{id} observation count does not equal claims") unless required.length == artifact.fetch("claim_ids").length
      required.each do |observation|
        claim_id = observation.fetch("claim_id")
        expected_scenarios = IdentityPlatformAcceptance.scenario_names_for_claim(upstream, claim_id)
        fail_check("#{id} #{claim_id} scenario allocation drifted") unless observation.fetch("scenario").split(",") == expected_scenarios
        fail_check("#{id} contract reference is not schema ID") unless observation.fetch("contract_reference") == upstream.fetch("schema")
        text_fields = %w[scenario preconditions stimulus expected_outcome actual_outcome]
        text_fields.each do |field|
          text = observation.fetch(field)
          BANNED_TEXT.each { |phrase| fail_check("#{id} contains generic phrase #{phrase.inspect}") if text.downcase.include?(phrase) }
        end
        exact_operations = IdentityPlatformAcceptance.operation_ids_for_claim(upstream, claim_id)
        exact_inputs = IdentityPlatformAcceptance.input_fields_for_claim(upstream, claim_id)
        fail_check("#{id} preconditions omit exact operations") unless exact_operations.all? { |operation| observation.fetch("preconditions").include?(operation) }
        fail_check("#{id} preconditions omit exact inputs") unless exact_inputs.all? { |input| observation.fetch("preconditions").include?(input) }
        fail_check("#{id} expected outcome omits invariant") unless observation.fetch("expected_outcome").include?(profile.fetch("invariant"))
        fail_check("#{id} actual outcome is not exact expected outcome") unless observation.fetch("actual_outcome") == observation.fetch("expected_outcome")
        signature = %w[preconditions stimulus expected_outcome].map { |field| observation.fetch(field) }.join("\u0000")
        previous = description_owners[signature]
        fail_check("#{id} reuses another artifact observation template from #{previous}") if previous && previous != id
        description_owners[signature] = id
      end
      if id == "phone-reset-risk-evidence-report"
        exact = %w[identity.phone.password-reset-request identity.phone.password-reset-complete identity.risk.evaluate]
        fail_check("phone reset operation claims are incomplete") unless exact.all? { |operation| upstream.fetch("operation_claims", []).include?(operation) }
        per_operation = {
          "identity.phone.password-reset-request" => %w[replay concurrency rollback expiry cleanup unknown-outcome],
          "identity.phone.password-reset-complete" => %w[replay concurrency rollback expiry cleanup unknown-outcome],
          "identity.risk.evaluate" => %w[replay expiry unknown-outcome]
        }
        per_operation.each do |operation_id, required_scenarios|
          row = required.find { |observation| observation.fetch("claim_id") == operation_id }
          actual = row.fetch("scenario").split(",")
          fail_check("phone reset #{operation_id} scenarios are incomplete") unless (required_scenarios - actual).empty?
        end
      end
      schema_path = File.join(IdentityPlatformAcceptance::REPOSITORY_ROOT, artifact.fetch("schema_path"))
      schema = load_canonical(schema_path)
      fail_check("#{id} schema drifted from catalog") unless schema == IdentityPlatformAcceptance.schema_document(artifact)
      fail_check("#{id} record schema is not v2 closed shape") unless schema.fetch("required") == source.dig("evidence_record_contract", "required_payload_fields") && schema.fetch("additionalProperties") == false
      evidence_schema = schema.dig("$defs", "artifact_evidence") || fail_check("#{id} lacks artifact evidence object schema")
      fail_check("#{id} artifact evidence is not closed") unless evidence_schema.fetch("type") == "object" && evidence_schema.fetch("additionalProperties") == false
      fail_check("#{id} evidence fields are not exact") unless artifact.fetch("artifact_evidence_fields") == profile.fetch("evidence_fields").keys
      fail_check("#{id} evidence payload is too weak") unless evidence_schema.fetch("properties").length >= 7
      fail_check("#{id} lacks immutable command execution proof") unless evidence_schema.dig("properties", "execution", "x-execution-proof") == 2
      semantic_rules = evidence_schema.fetch("x-semantic-rules")
      fail_check("#{id} lacks a non-zero semantic work invariant") unless semantic_rules.any? { |rule| %w[positive true].include?(rule.fetch("kind")) }
      evidence_contract_digests << Digest::SHA256.hexdigest(JSON.generate(canonical(evidence_schema)))
      evidence = representative_evidence(artifact, schema)
      assert_runner_derivation_contract!(artifact, schema, evidence)
      preliminary_record = representative_record(artifact, "sha256:#{'0' * 64}", "sha256:#{'0' * 64}")
      execution = evidence.fetch("execution")
      execution["tested_revision"] = preliminary_record.fetch("tested_revision")
      execution["input_root"] = preliminary_record.fetch("input_root")
      AcceptanceSchemaValidation.bind_execution_proof!(execution)
      AcceptanceSchemaValidation.bind_runner_transcripts!(evidence, execution)
      receipt_bytes = AcceptanceSchemaValidation.execution_receipt_bytes(execution)
      receipt_digest = "sha256:#{Digest::SHA256.hexdigest(receipt_bytes)}"
      evidence_bytes = JSON.pretty_generate(evidence) + "\n"
      evidence_digest = "sha256:#{Digest::SHA256.hexdigest(evidence_bytes)}"
      record = representative_record(artifact, evidence_digest, receipt_digest)
      binding_errors = record_artifact_errors(record, artifact, schema, evidence_bytes, receipt_bytes)
      fail_check("#{id} representative record/artifact binding violates contract: #{binding_errors.first}") unless binding_errors.empty?
      assert_execution_semantic_negative_fixtures!(artifact, schema, record, evidence, receipt_bytes)
      assert_negative_fixtures!(artifact, schema, record, evidence, receipt_bytes) if id == "affected-gate-report"
    end
    fail_check("artifact evidence contracts are cloned") unless evidence_contract_digests.uniq.length == 70
    fail_check("artifact catalog is incomplete") unless source_artifacts.keys.sort == catalog.fetch("artifacts").map { |artifact| artifact.fetch("artifact_id") }.sort
    puts "acceptance catalog valid: #{program_units.length} program units, #{product_units.length} product-contract units, #{operation_rows.length} operations, #{source_artifacts.length} artifacts, zero generic templates"
  rescue KeyError => e
    fail_check("missing required field: #{e.message}")
  end
end

AcceptanceCatalogCheck.run
