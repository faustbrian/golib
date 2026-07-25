#!/bin/sh

set -eu

version=${1:-}
root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-release-verify.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
first="$temporary/first"
second="$temporary/second"

"$root/scripts/release.sh" "$version" "$first"
"$root/scripts/release.sh" "$version" "$second"
if ! diff -rq "$first" "$second" >/dev/null; then
	printf 'repeated release packaging produced different artifacts\n' >&2
	exit 1
fi

archive_count=$(find "$first" -type f -name '*.zip' | wc -l | tr -d ' ')
if [ "$archive_count" -ne 6 ]; then
	printf 'release contains %s archives, want 6\n' "$archive_count" >&2
	exit 1
fi
(
	cd "$first"
	LC_ALL=C sort -c -k2 checksums.txt
	shasum -a 256 -c checksums.txt >/dev/null
)

platforms='linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64'
for platform in $platforms; do
	goos=${platform%/*}
	goarch=${platform#*/}
	name="golib-analysis_${version}_${goos}_${goarch}"
	program=golib-analysis
	if [ "$goos" = windows ]; then
		program=golib-analysis.exe
	fi
	archive="$first/$name.zip"
	entries="$temporary/$name.entries"
	expected="$temporary/$name.expected"
	unzip -Z1 "$archive" > "$entries"
	printf '%s\n' \
		"$name/$program" \
		"$name/README.md" \
		"$name/CHANGELOG.md" \
		"$name/SECURITY.md" > "$expected"
	if ! cmp -s "$entries" "$expected"; then
		printf 'release archive has unexpected contents: %s\n' "$name.zip" >&2
		exit 1
	fi
done

host_os=$(go env GOOS)
host_arch=$(go env GOARCH)
host_name="golib-analysis_${version}_${host_os}_${host_arch}"
host_program=golib-analysis
if [ "$host_os" = windows ]; then
	host_program=golib-analysis.exe
fi
mkdir -p "$temporary/host"
unzip -q "$first/$host_name.zip" -d "$temporary/host"
reported=$("$temporary/host/$host_name/$host_program" version)
if [ "$reported" != "$version" ]; then
	printf 'release binary reports version %s, want %s\n' "$reported" "$version" >&2
	exit 1
fi
report=$(
	cd "$root/testdata/coverage"
	GOWORK=off "$temporary/host/$host_name/$host_program" check \
		-config advisory.yml -format json ./...
)
case "$report" in
	*"\"tool_version\":\"$version\""*) ;;
	*)
		printf 'release report does not contain tool version %s\n' "$version" >&2
		exit 1
		;;
esac

printf 'release verified: %s (%s archives)\n' "$version" "$archive_count"
