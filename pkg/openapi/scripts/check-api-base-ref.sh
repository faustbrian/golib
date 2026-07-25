#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
resolver="$root_dir/scripts/resolve-api-base-ref.sh"
temporary_root=$(mktemp -d)
trap 'rm -rf "$temporary_root"' EXIT HUP INT TERM
repository="$temporary_root/repository"

git init -q "$repository"
git -C "$repository" config user.email test@example.com
git -C "$repository" config user.name Test
touch "$repository/first"
git -C "$repository" add first
git -C "$repository" commit -qm first

actual=$($resolver "$repository")
test "$actual" = HEAD

touch "$repository/second"
git -C "$repository" add second
git -C "$repository" commit -qm second

actual=$($resolver "$repository")
test "$actual" = 'HEAD^'

actual=$(API_BASE_REF=HEAD "$resolver" "$repository")
test "$actual" = HEAD

if API_BASE_REF=missing "$resolver" "$repository" 2>/dev/null; then
	echo 'missing explicit API baseline unexpectedly succeeded' >&2
	exit 1
fi
