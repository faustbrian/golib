#!/usr/bin/env bash
set -euo pipefail

tool_dir="${TMPDIR:-/tmp}/queue-gremlins-v0.6.0"
mkdir -p "${tool_dir}"
GOBIN="${tool_dir}" go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

run_scope() {
  local package="$1"
  local exclusions="$2"

  "${tool_dir}/gremlins" unleash "${package}" \
    --exclude-files "${exclusions}" \
    --workers "${MUTATION_WORKERS:-4}" \
    --test-cpu 1 \
    --timeout-coefficient 50 \
    --threshold-efficacy 100 \
    --threshold-mcover 100 \
    --output-statuses lctv
}

run_scope ./management \
  '^(control|desired|errors|lifecycle|protocol|reader|records|status)\.go$'
run_scope . \
  '^(cmd|examples|internal|job|management|managementhttp|nats|nsq|rabbitmq|redisdb|redisstream|valkeystream)/|^(errors|logger|management_lifecycle|metric|observer|options|pool|recovery|ring|thread)\.go$'
run_scope ./redisstream \
  '^(api_compat|benchmark|constructor|control|options|redis|settlement|status)\.go$'
run_scope ./redisstream \
  '^(api_compat|benchmark|constructor|options|records|redis|settlement|status)\.go$'
run_scope ./redisstream \
  '^(api_compat|benchmark|constructor|control|options|records|settlement|status)\.go$'
run_scope ./redisstream \
  '^(api_compat|benchmark|constructor|control|options|records|redis|settlement)\.go$'
run_scope ./valkeystream \
  '^(control|errors|native_transport|options|records|worker)\.go$'
run_scope ./valkeystream \
  '^(control|errors|options|records|status|worker)\.go$'
