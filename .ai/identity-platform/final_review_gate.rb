#!/usr/bin/env ruby
# frozen_string_literal: true

require "json"
require "open3"

base = ARGV.fetch(0)
raise "invalid recorded base" unless base.match?(/\A[0-9a-f]{40}\z/)

prompt = 'Review the complete identity-platform diff for unresolved Critical or Important correctness, security, contract, dependency, evidence, or orchestration findings. Output only canonical compact JSON with exactly the ordered keys critical and important, each an array of finding objects; use {"critical":[],"important":[]} only when neither class has a finding.'
stdout, stderr, status = Open3.capture3("codex", "review", "--base", base, prompt, chdir: File.expand_path("../..", __dir__))
raise "independent final review failed: #{stderr}" unless status.success?

review = JSON.parse(stdout)
canonical = JSON.generate(review)
raise "independent final review is not canonical JSON" unless stdout.strip == canonical
path = ENV.fetch("IDENTITY_ACCEPTANCE_RAW_OUTPUT_PATH")
File.binwrite(path, canonical)
print canonical
