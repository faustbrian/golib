#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
parent="$(dirname "$repo_root")"

if [[ "${1:-}" == "make" && "${2:-}" == "mutation" ]]; then
    echo 'mutation copies the module and cannot use this workspace wrapper' >&2
    exit 1
fi

audit_root="$(mktemp -d "${TMPDIR:-/tmp}/localized-deps.XXXXXX")"

cleanup() {
    case "$(basename "$audit_root")" in
        localized-deps.*) rm -rf -- "$audit_root" ;;
    esac
}
trap cleanup EXIT

modules=(
    github.com/faustbrian/golib/pkg/api-query
    github.com/faustbrian/golib/pkg/international
    github.com/faustbrian/golib/pkg/validation
)
repos=(api-query international validation)

cd "$audit_root"
go work init "$repo_root"

for index in "${!modules[@]}"; do
    module="${modules[$index]}"
    repo="${repos[$index]}"
    checkout="$parent/$repo"
    version="$(awk -v module="$module" '$1 == module { print $2 }' "$repo_root/go.mod")"
    short="${version##*-}"
    if [[ -z "$version" || ${#short} -ne 12 ]]; then
        echo "$module does not have a commit-pinned pseudo-version" >&2
        exit 1
    fi
    if ! commit="$(git -C "$checkout" rev-parse --verify "$short^{commit}")"; then
        echo "$module pin $short is absent from the sibling repository" >&2
        exit 1
    fi

    archive="$audit_root/$repo"
    mkdir "$archive"
    git -C "$checkout" archive "$commit" | tar -x -C "$archive"
    GOWORK="$audit_root/go.work" go work edit \
        -replace "$module@$version=$archive"
done

cd "$repo_root"
if [[ "$#" -eq 0 ]]; then
    set -- go test ./...
fi
GOWORK="$audit_root/go.work" "$@"
echo 'declared clean dependency revisions passed'
