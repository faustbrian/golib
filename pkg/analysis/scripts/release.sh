#!/bin/sh

set -eu

version=${1:-}
output=${2:-dist}
line_breaks=$(printf '%s' "$version" | wc -l | tr -d ' ')
if [ "${#version}" -gt 32 ] || [ "$line_breaks" -ne 0 ] || \
	! printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
	printf 'release version must be semantic version X.Y.Z\n' >&2
	exit 2
fi

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
case "$output" in
	/*) output_dir=$output ;;
	*) output_dir="$root/$output" ;;
esac
if [ -d "$output_dir" ] && [ -n "$(find "$output_dir" -mindepth 1 -print -quit)" ]; then
	printf 'release output directory is not empty: %s\n' "$output" >&2
	exit 1
fi

temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-release.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
mkdir -p "$temporary/stage" "$temporary/output"

platforms='linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64'
ldflags="-s -w -X github.com/faustbrian/golib/pkg/analysis/internal/version.Value=$version"

for platform in $platforms; do
	goos=${platform%/*}
	goarch=${platform#*/}
	name="golib-analysis_${version}_${goos}_${goarch}"
	stage="$temporary/stage/$name"
	program=golib-analysis
	if [ "$goos" = windows ]; then
		program=golib-analysis.exe
	fi
	mkdir -p "$stage"
	CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build \
		-trimpath -buildvcs=false -ldflags "$ldflags" \
		-o "$stage/$program" ./cmd/golib-analysis
	cp README.md CHANGELOG.md SECURITY.md "$stage/"
	chmod 0755 "$stage/$program"
	chmod 0644 "$stage/README.md" "$stage/CHANGELOG.md" "$stage/SECURITY.md"
	TZ=UTC touch -t 198001010000 \
		"$stage/$program" "$stage/README.md" "$stage/CHANGELOG.md" "$stage/SECURITY.md"
	(
		cd "$temporary/stage"
		TZ=UTC zip -X -q "$temporary/output/$name.zip" \
			"$name/$program" "$name/README.md" \
			"$name/CHANGELOG.md" "$name/SECURITY.md"
	)
done

(
	cd "$temporary/output"
	shasum -a 256 ./*.zip | awk '{name=$2; sub("^\\./", "", name); print $1 "  " name}' | \
		LC_ALL=C sort -k2 > checksums.txt
)

mkdir -p "$output_dir"
cp "$temporary/output"/* "$output_dir/"
printf 'release artifacts written: %s\n' "$output_dir"
