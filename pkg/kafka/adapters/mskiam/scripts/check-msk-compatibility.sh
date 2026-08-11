#!/usr/bin/env bash

set -euo pipefail

report_path=${GOLIB_MSK_REPORT:-}
if [[ -z "$report_path" || "$report_path" != /* ]]; then
	printf '%s\n' 'GOLIB_MSK_REPORT must be an absolute persistent report path' >&2
	exit 2
fi
report_directory=$(dirname -- "$report_path")
if [[ ! -d "$report_directory" ]]; then
	printf '%s\n' 'GOLIB_MSK_REPORT parent directory must already exist' >&2
	exit 2
fi

task_cache=$(mktemp -d "${TMPDIR:-/tmp}/golib-msk-compat-gocache.XXXXXX")
task_report=$(mktemp "$report_directory/.msk-compatibility.XXXXXX")
# shellcheck disable=SC2329
cleanup() {
	find "$task_cache" -depth -delete
	if [[ -f "$task_report" ]]; then
		rm -f -- "$task_report"
	fi
}
trap cleanup EXIT HUP INT TERM
export GOCACHE="$task_cache"

go_command=${GO:-go}
set +e
"$go_command" test -count=1 -json -tags=msk \
	-run '^TestAmazonMSKCompatibility$' . | tee "$task_report"
pipeline_status=("${PIPESTATUS[@]}")
set -e
mv -- "$task_report" "$report_path"
if [[ ${pipeline_status[1]} -ne 0 ]]; then
	exit "${pipeline_status[1]}"
fi
exit "${pipeline_status[0]}"
