#!/usr/bin/env bash
set -euo pipefail

root="${1:?repository root is required}"
ledger="${2:?history migration ledger is required}"
inventory="$(mktemp "${TMPDIR:-/tmp}/golib-history-scope.XXXXXX")"

cleanup() {
    rm -f "${inventory}"
}
trap cleanup EXIT INT TERM

allowed_paths=()
while IFS= read -r path; do
    allowed_paths+=("${path}")
done < <(jq -r '.allowed_changes[]' "${ledger}")

is_allowed() {
    local candidate="$1"
    local allowed_path
    for allowed_path in "${allowed_paths[@]}"; do
        if [[ "${candidate}" == "${allowed_path}" ]]; then
            return 0
        fi
    done

    return 1
}

append_worktree_entry() {
    local path="$1"
    local blob executable
    executable=0
    if [[ -x "${root}/${path}" ]]; then
        executable=1
    fi
    if [[ ! -e "${root}/${path}" && ! -L "${root}/${path}" ]]; then
        blob="missing"
    else
        blob="$(git -C "${root}" hash-object --no-filters -- "${path}")"
    fi
    printf 'worktree\0%s\0%s\0%s\0' \
        "${path}" "${executable}" "${blob}" >>"${inventory}"
}

while IFS= read -r -d '' entry; do
    path="${entry#*$'\t'}"
    if ! is_allowed "${path}"; then
        printf 'index\0%s\0' "${entry}" >>"${inventory}"
    fi
done < <(git -C "${root}" ls-files --stage -z)

while IFS= read -r -d '' path; do
    if ! is_allowed "${path}"; then
        append_worktree_entry "${path}"
    fi
done < <(git -C "${root}" diff --name-only -z --)

while IFS= read -r -d '' path; do
    if ! is_allowed "${path}"; then
        append_worktree_entry "${path}"
    fi
done < <(git -C "${root}" ls-files --others --exclude-standard -z)

shasum -a 256 "${inventory}" | awk '{print $1}'
