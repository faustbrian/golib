#!/usr/bin/env bash
set -euo pipefail

: "${CYCLONEDX_VERSION:?CYCLONEDX_VERSION is required}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace=$(mktemp -d)
go_cache=$(mktemp -d)
module_cache=$(mktemp -d)
go_path=$(mktemp -d)
cleanup() {
	chmod -R u+w "$workspace" "$go_cache" "$module_cache" "$go_path" 2>/dev/null || true
	find "$workspace" -depth -delete
	find "$go_cache" -depth -delete
	find "$module_cache" -depth -delete
	find "$go_path" -depth -delete
}
trap cleanup EXIT

export GOWORK=off GOCACHE="$go_cache" GOMODCACHE="$module_cache" GOPATH="$go_path"

generate() {
	local name=$1
	local module_root=$2
	local expected=$3
	shift 3
	local output="$workspace/${name}.json"
	local -a environment=(env)
	if (( $# > 0 )); then
		local modfile="$workspace/${name}.mod"
		local sumfile="$workspace/${name}.sum"
		cp "$module_root/go.mod" "$modfile"
		test ! -e "$module_root/go.sum" || cp "$module_root/go.sum" "$sumfile"
		go mod edit -modfile="$modfile" "$@"
		environment+=(GOFLAGS="-modfile=$modfile")
	fi
	(
		cd "$module_root"
		"${environment[@]}" go run "github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@${CYCLONEDX_VERSION}" \
			mod -json -licenses -type library -noserial -notimestamp -output "$output" .
	)
	test -s "$output"
	grep -F -- "\"name\": \"${expected}\"" "$output" >/dev/null || {
		echo "SBOM is missing ${expected}" >&2
		exit 1
	}
}

generate core "$root" github.com/faustbrian/golib/pkg/schema-registry
generate confluent "$root/providers/confluent" \
	github.com/faustbrian/golib/pkg/schema-registry/providers/confluent \
	-replace="github.com/faustbrian/golib/pkg/schema-registry=${root}"
generate glue "$root/providers/glue" \
	github.com/faustbrian/golib/pkg/schema-registry/providers/glue \
	-replace="github.com/faustbrian/golib/pkg/schema-registry=${root}"
