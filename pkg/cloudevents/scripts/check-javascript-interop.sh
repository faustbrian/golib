#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_root="$(mktemp -d -t cloudevents-javascript-interop.XXXXXX)"

cleanup() {
    find "${temporary_root}" -type f -delete
    find "${temporary_root}" -type l -delete
    find "${temporary_root}" -depth -type d -empty -delete
}
trap cleanup EXIT

export npm_config_cache="${temporary_root}/npm-cache"
node_version="v24.19.0"
node_bin="$(command -v node || true)"
npm_bin="$(command -v npm || true)"
runtime_path=""
if [[ -z "${npm_bin}" || "$(${node_bin:-false} --version 2>/dev/null || true)" != "${node_version}" ]]; then
    case "$(uname -s)-$(uname -m)" in
        Darwin-arm64) platform="darwin-arm64" ;;
        Darwin-x86_64) platform="darwin-x64" ;;
        Linux-aarch64 | Linux-arm64) platform="linux-arm64" ;;
        Linux-x86_64) platform="linux-x64" ;;
        *)
            printf 'unsupported Node.js interoperability platform: %s-%s\n' "$(uname -s)" "$(uname -m)" >&2
            exit 1
            ;;
    esac
    archive_metadata="$(jq -cer --arg platform "${platform}" \
        '.sources[] | select(.name == "Node.js npm runtime") | .archives[$platform]' \
        "${module_root}/specification/manifest.json")"
    archive_filename="$(jq -r '.filename' <<<"${archive_metadata}")"
    expected_hash="$(jq -r '.sha256' <<<"${archive_metadata}")"
    archive="${temporary_root}/${archive_filename}"
    curl --fail --location --silent --show-error \
        "https://nodejs.org/dist/${node_version}/${archive_filename}" \
        --output "${archive}"
    if command -v shasum >/dev/null 2>&1; then
        actual_hash="$(shasum -a 256 "${archive}" | awk '{print $1}')"
    else
        actual_hash="$(sha256sum "${archive}" | awk '{print $1}')"
    fi
    if [[ "${actual_hash}" != "${expected_hash}" ]]; then
        printf 'Node.js archive digest mismatch for %s\n' "${platform}" >&2
        exit 1
    fi
    tar -xf "${archive}" -C "${temporary_root}"
    runtime_path="${temporary_root}/node-${node_version}-${platform}/bin"
    node_bin="${runtime_path}/node"
    npm_bin="${runtime_path}/npm"
fi

mkdir -p "${temporary_root}/project"
cp "${module_root}/scripts/javascript/package.json" "${temporary_root}/project/package.json"
cp "${module_root}/scripts/javascript/package-lock.json" "${temporary_root}/project/package-lock.json"
(
    cd "${temporary_root}/project"
    PATH="${runtime_path:+${runtime_path}:}${PATH}" "${npm_bin}" ci \
        --ignore-scripts \
        --no-audit \
        --no-fund \
        >/dev/null
)

NODE_PATH="${temporary_root}/project/node_modules" \
    "${node_bin}" \
    "${module_root}/scripts/javascript-interop.cjs" \
    "${module_root}/testdata/interoperability"

"${GO:-go}" test . -run '^TestJavaScriptSDK' -count=1
