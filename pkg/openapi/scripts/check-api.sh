#!/bin/sh
set -eu

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

root_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
repository_root=$(git -C "$root_dir" rev-parse --show-toplevel)
module_prefix=$(git -C "$root_dir" rev-parse --show-prefix)
module_prefix=${module_prefix%/}
base_ref=$("$root_dir/scripts/resolve-api-base-ref.sh" "$repository_root")
apidiff_version=$APIDIFF_VERSION

temporary_root=$(mktemp -d)
trap 'rm -rf "$temporary_root"' EXIT HUP INT TERM
baseline_root="$temporary_root/baseline"
mkdir -p "$baseline_root"

if [ -n "$module_prefix" ]; then
    git -C "$repository_root" archive "$base_ref:$module_prefix" |
        tar -x -C "$baseline_root"
else
    git -C "$repository_root" archive "$base_ref" |
        tar -x -C "$baseline_root"
fi

module_path=$(sed -n 's/^module[[:space:]]\{1,\}//p' "$root_dir/go.mod")
if [ -z "$module_path" ]; then
    echo 'module path is unavailable' >&2
    exit 1
fi

(
    cd "$baseline_root"
    go run "golang.org/x/exp/cmd/apidiff@$apidiff_version" \
        -m -w "$temporary_root/baseline.api" "$module_path"
)
(
    cd "$root_dir"
    go run "golang.org/x/exp/cmd/apidiff@$apidiff_version" \
        -m -w "$temporary_root/current.api" "$module_path"
)
go run "golang.org/x/exp/cmd/apidiff@$apidiff_version" \
    -m -incompatible "$temporary_root/baseline.api" \
    "$temporary_root/current.api"
