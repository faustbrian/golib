#!/usr/bin/env ruby
# frozen_string_literal: true

require_relative "shared_contract_applicability"

root = __dir__
units = File.readlines(File.join(root, "INVENTORY.md")).filter_map do |line|
  next unless line.start_with?("| `")
  line.split("|").map(&:strip)[1].delete("`")
end

begin
  document = SharedContractApplicability.load_and_validate!(root: root, units: units)
  if ARGV == ["--check"]
    unless SharedContractApplicability.canonical?(root: root, units: units)
      warn "shared-contract applicability: #{SharedContractApplicability::FILE} is not canonical JSON"
      exit 1
    end
    puts "shared-contract applicability: #{units.length} units valid and canonical"
  elsif ARGV.length == 1 && !ARGV.first.start_with?("-")
    puts SharedContractApplicability.render(document: document, root: root, unit: ARGV.first)
  else
    warn "usage: render_shared_contracts.rb --check | UNIT"
    exit 2
  end
rescue ArgumentError, KeyError => e
  warn e.message
  exit 1
end
