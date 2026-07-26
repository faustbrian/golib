#!/bin/sh
set -eu

reference="${1:-HEAD}"
temporary="$(mktemp -d)"
trap 'rm -r "$temporary"' EXIT HUP INT TERM

prefix="$(git rev-parse --show-prefix)"
repository="$(git rev-parse --show-toplevel)"
test -n "$prefix" || {
	printf 'module must be a subdirectory of its Git repository\n' >&2
	exit 1
}
module_path="${prefix%/}"
commit="$(git -C "$repository" rev-parse "${reference}^{commit}")"

for archive in first second; do
	git -C "$repository" archive --format=tar \
		"$commit" "$module_path" > "$temporary/$archive.tar"
	go run ./scripts/rewrite-archive.go "$temporary/$archive.tar" \
		"$temporary/$archive.tar.gz" "$module_path" prompts
done

cmp "$temporary/first.tar.gz" "$temporary/second.tar.gz"
gzip -t "$temporary/first.tar.gz"
