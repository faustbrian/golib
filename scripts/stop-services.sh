#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    printf 'usage: %s <state-file>\n' "$0" >&2
    exit 2
fi

state_file="$1"
[[ -f "${state_file}" ]] || exit 0
cleanup_timeout="${GOLIB_DOCKER_CLEANUP_TIMEOUT_SECONDS:-30}"
if [[ ! "${cleanup_timeout}" =~ ^[1-9][0-9]*$ ]]; then
    printf 'GOLIB_DOCKER_CLEANUP_TIMEOUT_SECONDS must be a positive integer\n' >&2
    exit 2
fi

remove_container() {
    local container="$1"
    local started="${SECONDS}"

    docker rm --force "${container}" >/dev/null 2>&1 &
    local docker_pid=$!
    while kill -0 "${docker_pid}" >/dev/null 2>&1; do
        if ((SECONDS - started >= cleanup_timeout)); then
            kill -TERM "${docker_pid}" >/dev/null 2>&1 || true
            sleep 0.1
            kill -KILL "${docker_pid}" >/dev/null 2>&1 || true
            wait "${docker_pid}" >/dev/null 2>&1 || true
            printf 'timed out removing Docker container %s after %ss\n' \
                "${container}" "${cleanup_timeout}" >&2
            return
        fi
        sleep 0.1
    done
    wait "${docker_pid}" >/dev/null 2>&1 || true
}

while IFS= read -r container; do
    [[ -n "${container}" ]] || continue
    remove_container "${container}"
done <"${state_file}"
