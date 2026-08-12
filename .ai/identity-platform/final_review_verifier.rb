#!/usr/bin/env ruby
# frozen_string_literal: true

require "digest"
require "json"

module IdentityPlatformFinalReviewVerifier
  module_function

  def verify(bytes)
    review = JSON.parse(bytes)
    expected = {"critical" => [], "important" => []}
    raise "review output is not canonical compact JSON" unless bytes == JSON.generate(review)
    raise "review output schema or finding closure drifted" unless review == expected && review.keys == %w[critical important]

    JSON.generate({
      "schema_version" => 1,
      "schema" => "identity-platform.final-review-verifier.v1",
      "review_sha256" => "sha256:#{Digest::SHA256.hexdigest(bytes)}",
      "critical_count" => 0,
      "important_count" => 0
    })
  end
end

if $PROGRAM_NAME == __FILE__
  input_path = ENV.fetch("IDENTITY_ACCEPTANCE_VERIFIER_INPUT_PATH")
  output_path = ENV.fetch("IDENTITY_ACCEPTANCE_VERIFIER_OUTPUT_PATH")
  File.binwrite(output_path, IdentityPlatformFinalReviewVerifier.verify(File.binread(input_path)))
end
