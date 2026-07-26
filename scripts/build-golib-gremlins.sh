#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"

# Resolved from the verified repository root rather than the caller's cwd.
# shellcheck disable=SC1091
source "${root}/.golib/versions.env"

semantic_patch="${root}/scripts/patches/gremlins-run-all-mutants.patch"
coverage_patch="${root}/scripts/patches/gremlins-shared-coverage.patch"
patch_digest="$(
    cat "${semantic_patch}" "${coverage_patch}" |
        shasum -a 256 |
        awk '{print $1}'
)"
artifact="${root}/.artifacts/tooling/gremlins-${GREMLINS_VERSION}-${patch_digest}"
binary="${artifact}/golib-gremlins"
lock="${artifact}.lock"

if [[ -x "${binary}" ]]; then
    printf '%s\n' "${binary}"
    exit 0
fi

mkdir -p "$(dirname "${artifact}")"
attempts=0
while ! mkdir "${lock}" 2>/dev/null; do
    if [[ -x "${binary}" ]]; then
        printf '%s\n' "${binary}"
        exit 0
    fi
    attempts=$((attempts + 1))
    if [[ "${attempts}" -ge 120 ]]; then
        printf 'timed out waiting for golib-gremlins build lock: %s\n' \
            "${lock}" >&2
        exit 1
    fi
    sleep 1
done
temporary="$(mktemp -d "${TMPDIR:-/tmp}/golib-gremlins.XXXXXX")"
cleanup() {
    rm -rf "${temporary}" "${lock}"
}
trap cleanup EXIT HUP INT TERM

download="$(GOWORK=off go mod download -json \
    "github.com/go-gremlins/gremlins@${GREMLINS_VERSION}")"
source_directory="$(jq -er '.Dir' <<<"${download}")"
jq -e '.Sum | type == "string" and length > 0' <<<"${download}" >/dev/null

cp -R "${source_directory}" "${temporary}/source"
chmod -R u+w "${temporary}/source"
patch --batch --forward -d "${temporary}/source" -p1 <"${semantic_patch}" >&2
patch --batch --forward -d "${temporary}/source" -p1 <"${coverage_patch}" >&2
(
    cd "${temporary}/source"
    GOWORK=off go build -trimpath -buildvcs=false \
        -o "${temporary}/golib-gremlins" ./cmd/gremlins
)

rm -rf "${artifact}"
mkdir "${artifact}"
mv "${temporary}/golib-gremlins" "${binary}"
chmod 0755 "${binary}"
printf '%s\n' "${binary}"
