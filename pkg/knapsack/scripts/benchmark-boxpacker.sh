#!/bin/sh
set -eu

samples="${BOXPACKER_RUNTIME_SAMPLES:-10}"
output="${BOXPACKER_RUNTIME_RAW_OUTPUT:-}"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

case "$samples" in
	''|*[!0-9]*)
		printf 'BOXPACKER_RUNTIME_SAMPLES must be an integer\n' >&2
		exit 1
		;;
esac
test "$samples" -ge 10 && test "$samples" -le 100 || {
	printf 'BOXPACKER_RUNTIME_SAMPLES must be between 10 and 100\n' >&2
	exit 1
}

composer install --working-dir integration/boxpacker --no-dev --no-interaction \
	--prefer-dist --no-progress --no-scripts
revision="$(git log -1 --format=%H -- \
	boxpacker_integration_test.go \
	boxpacker_runtime_test.go \
	integration/boxpacker/compare.php \
	integration/references/adapter_test.go \
	integration/references/cmd/knapsack-compare/main.go \
	scripts/benchmark-boxpacker.sh)"
input_sha256="$(./scripts/benchmark-input-digest.sh boxpacker)"
(
	cd integration/references
	go build -trimpath \
		-ldflags "-X main.version=development -X main.revision=$revision" \
		-o "$temporary/knapsack-compare" ./cmd/knapsack-compare
)

case "$(uname -s)" in
	Darwin) processor="$(sysctl -n machdep.cpu.brand_string)" ;;
	Linux) processor="$(awk -F ': ' '/model name/ { print $2; exit }' /proc/cpuinfo)" ;;
	*)
		printf 'runtime comparison is unsupported on %s\n' "$(uname -s)" >&2
		exit 1
		;;
esac

current="$temporary/current.json"
BOXPACKER_RUNTIME_COMPARE=1 \
	BOXPACKER_GO_ADAPTER="$temporary/knapsack-compare" \
	BOXPACKER_RUNTIME_OUTPUT="$current" \
	BOXPACKER_PACKAGE_COMMIT="$revision" \
	BOXPACKER_INPUT_SHA256="$input_sha256" \
	BOXPACKER_PROCESSOR="$processor" \
	BOXPACKER_RUNTIME_SAMPLES="$samples" \
	go test . -run '^TestBoxPackerRuntimeComparison$' -count=1

php -r '
$evidence = json_decode(file_get_contents($argv[1]), true, 512, JSON_THROW_ON_ERROR);
foreach ($evidence["summaries"] as $summary) {
    printf(
        "%s wall_p50_ns=%d wall_p95_ns=%d solve_p50_ns=%d solve_p95_ns=%d peak_RSS_bytes=%d boxes=%d items=%d\n",
        $summary["implementation"],
        $summary["wall_p50_nanoseconds"],
        $summary["wall_p95_nanoseconds"],
        $summary["solve_p50_nanoseconds"],
        $summary["solve_p95_nanoseconds"],
        $summary["maximum_peak_rss_bytes"],
        $summary["container_count"],
        $summary["packed_items"],
    );
}
' "$current"

if [ -n "$output" ]; then
	cp "$current" "$output"
fi
