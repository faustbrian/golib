#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    printf 'usage: %s <module-directory>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
# shellcheck disable=SC1091 # Canonical repository tool versions.
source "${root}/.golib/versions.env"
case "$1" in
    /*) directory="$1" ;;
    *) directory="${root}/$1" ;;
esac
if [[ ! -f "${directory}/go.mod" ]]; then
    printf 'module has no go.mod: %s\n' "$1" >&2
    exit 1
fi

module_path="$(
    cd "${directory}"
    GOWORK=off go mod edit -json | jq -er '.Module.Path'
)"
baseline="${directory}/api/baseline.txt"
temporary="$(mktemp "${TMPDIR:-/tmp}/golib-api-baseline.XXXXXX")"
cleanup() {
    rm -f "${temporary}"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$(dirname "${baseline}")"
(
    cd "${directory}"
    GOWORK=off go run \
        "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" \
        -m -w "${temporary}" "${module_path}"
)
test -s "${temporary}"
mv "${temporary}" "${baseline}"
trap - EXIT HUP INT TERM
