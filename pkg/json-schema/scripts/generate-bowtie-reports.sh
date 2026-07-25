#!/usr/bin/env bash
set -euo pipefail

readonly bowtie_version=2026.6.1
readonly implementation=localhost/json-schema-bowtie
readonly report_directory=bowtie/reports
readonly suite_directory=testdata/official/JSON-Schema-Test-Suite/tests
readonly dialects=(draft3 draft4 draft6 draft7 draft2019-09 draft2020-12)

mkdir -p "$report_directory"
docker build -f bowtie/Dockerfile -t "$implementation" . >/dev/null

for dialect in "${dialects[@]}"; do
	raw_report="$report_directory/$dialect.json"
	statistics="$report_directory/$dialect-statistics.json"
	uvx --from "bowtie-json-schema==$bowtie_version" bowtie suite \
		"$suite_directory/$dialect" -i "$implementation" -V \
		> "$raw_report"
	uvx --from "bowtie-json-schema==$bowtie_version" bowtie statistics \
		"$raw_report" -f json > "$statistics"
	jq -e '.mean == 1 and .median == 1' "$statistics" >/dev/null
done

(
	cd "$report_directory"
	sha256sum -- *.json > SHA256SUMS
)
