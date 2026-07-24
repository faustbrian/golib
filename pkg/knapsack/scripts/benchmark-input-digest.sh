#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
	printf 'usage: %s <native|rss|boxpacker>\n' "$0" >&2
	exit 2
fi

profile="$1"
root="$(git rev-parse --show-toplevel)"
module="${root}/pkg/knapsack"
paths="$(mktemp "${TMPDIR:-/tmp}/knapsack-benchmark-paths.XXXXXX")"
manifest="$(mktemp "${TMPDIR:-/tmp}/knapsack-benchmark-inputs.XXXXXX")"
trap 'rm -f "${paths}" "${manifest}"' EXIT HUP INT TERM

add_file() {
	local path="$1"
	[[ -f "${root}/${path}" ]] || {
		printf 'benchmark input is missing: %s\n' "${path}" >&2
		exit 1
	}
	printf '%s\n' "${root}/${path}" >>"${paths}"
}

add_production_tree() {
	local directory="$1"
	find "${root}/${directory}" -type f \
		\( -name '*.go' ! -name '*_test.go' -o -name 'go.mod' -o -name 'go.sum' \) \
		! -path '*/.tools/*' -print >>"${paths}"
}

while IFS= read -r file; do
	case "${file}" in
		"${module}/integration/references/"* | "${module}/objective/gomoney/"*)
			continue
			;;
	esac
	printf '%s\n' "${file}" >>"${paths}"
done < <(
	find "${module}" -type f \
		\( -name '*.go' ! -name '*_test.go' -o -name 'go.mod' -o -name 'go.sum' \) \
		! -path '*/.tools/*' -print
)
while IFS= read -r directory; do
	add_production_tree "${directory}"
done < <(
	jq -r '
		. as $catalog
		| def closure($seen):
			([
				$catalog.modules[]
				| select(.module_path as $path | $seen | index($path))
				| .owned_dependencies[]
			] | unique) as $dependencies
			| ($seen + $dependencies | unique) as $next
			| if $next == $seen then $next else closure($next) end;
		(.modules[] | select(.directory == "pkg/knapsack").owned_dependencies) as $owned
		| closure($owned) as $paths
		| .modules[]
		| select(.module_path as $path | $paths | index($path))
		| .directory
	' "${root}/modules.json"
)
add_file "pkg/knapsack/scripts/benchmark-input-digest.sh"

case "${profile}" in
	native)
		add_file "pkg/knapsack/solver/benchmark_test.go"
		add_file "pkg/knapsack/scripts/benchmark-compare.sh"
		add_file "pkg/knapsack/specification/benchmark-thresholds.tsv"
		;;
	rss)
		add_file "pkg/knapsack/solver/benchmark_test.go"
		add_file "pkg/knapsack/scripts/benchmark-rss.sh"
		add_file "pkg/knapsack/specification/benchmark-rss-thresholds.tsv"
		;;
	boxpacker)
		add_file "pkg/knapsack/boxpacker_integration_test.go"
		add_file "pkg/knapsack/boxpacker_runtime_test.go"
		add_file "pkg/knapsack/integration/boxpacker/compare.php"
		add_file "pkg/knapsack/integration/boxpacker/composer.json"
		add_file "pkg/knapsack/integration/boxpacker/composer.lock"
		add_file "pkg/knapsack/scripts/benchmark-boxpacker.sh"
		find "${module}/integration/references" -type f \
			\( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) \
			-print >>"${paths}"
		;;
	*)
		printf 'unsupported benchmark profile: %s\n' "${profile}" >&2
		exit 2
		;;
esac

while IFS= read -r file; do
	relative="${file#"${root}/"}"
	if [[ "${relative}" == "pkg/knapsack/integration/references/go.sum" ]]; then
		# The parent module archive contains this benchmark evidence. Hashing its
		# own zip checksum would therefore create an unsatisfiable digest cycle.
		digest="$(
			sed '\|^github.com/faustbrian/golib/pkg/knapsack v[^ ]* h1:|d' \
				"${file}" |
				shasum -a 256 |
				awk '{print $1}'
		)"
	else
		digest="$(shasum -a 256 "${file}" | awk '{print $1}')"
	fi
	printf '%s  %s\n' "${digest}" "${relative}" >>"${manifest}"
done < <(LC_ALL=C sort -u "${paths}")

shasum -a 256 "${manifest}" | awk '{print $1}'
