#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mutation_path=${MUTATION_PATH:-./security}
gremlins_version=${GREMLINS_VERSION:-v0.6.0}
workers=${MUTATION_WORKERS:-4}
timeout_coefficient=${MUTATION_TIMEOUT_COEFFICIENT:-30}
integration=${MUTATION_INTEGRATION:-false}
exclude_files=${MUTATION_EXCLUDE_FILES:-}
temporary_root=$(mktemp -d)
trap 'rm -rf "$temporary_root"' EXIT HUP INT TERM
report="$temporary_root/report.json"

cd "$root_dir"
set -- unleash "$mutation_path" \
    --workers "$workers" \
    --timeout-coefficient "$timeout_coefficient" \
    --output-statuses l \
    --output "$report"
if [ "$integration" = true ]; then
    set -- "$@" --integration
fi
if [ -n "$exclude_files" ]; then
    set -- "$@" --exclude-files "$exclude_files"
fi
go run "github.com/go-gremlins/gremlins/cmd/gremlins@$gremlins_version" "$@"
go run ./internal/quality/cmd/mutationgate "$report"
