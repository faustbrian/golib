#!/bin/sh
set -eu

directory="$(mktemp -d)"
trap 'rm -rf "$directory"' EXIT

git diff --quiet -- . || {
	printf '%s\n' 'reproducibility check requires a clean knapsack tree' >&2
	exit 1
}
git diff --cached --quiet -- . || {
	printf '%s\n' 'reproducibility check requires an unstaged knapsack index' >&2
	exit 1
}

repository="$(git rev-parse --show-toplevel)"
module_path="$(git rev-parse --show-prefix)"
module_path="${module_path%/}"

if [ -n "$module_path" ]; then
	source_commit="$(git -C "$repository" log -1 --format=%H -- "$module_path")"
	module_tree="$source_commit:$module_path/go.mod"
else
	source_commit="$(git -C "$repository" log -1 --format=%H -- .)"
	module_tree="$source_commit:go.mod"
fi
if [ -z "$source_commit" ] || ! git -C "$repository" cat-file -e "$module_tree" 2>/dev/null; then
	printf '%s\n' 'knapsack module is absent from HEAD' >&2
	exit 1
fi

archive_module() {
	output="$1"
	if [ -n "$module_path" ]; then
		git -C "$repository" archive --format=tar "$source_commit" "$module_path" |
			gzip -n -9 > "$output"
	else
		git -C "$repository" archive --format=tar --prefix=knapsack/ \
			"$source_commit" | gzip -n -9 > "$output"
	fi
}

if [ "${1:-}" = "--archive" ]; then
	test "$#" -eq 2
	archive_module "$2"
	exit
fi
test "$#" -eq 0

archive_module "$directory/first.tar.gz"
archive_module "$directory/second.tar.gz"
cmp "$directory/first.tar.gz" "$directory/second.tar.gz"
tar -tzf "$directory/first.tar.gz" | grep -q '^knapsack/go.mod$'
tar -tzf "$directory/first.tar.gz" | grep -q '^knapsack/LICENSE$'
shasum -a 256 "$directory/first.tar.gz"
