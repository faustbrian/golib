#!/bin/sh
set -eu

duration="${1:-100ms}"
samples="${BENCH_SAMPLES:-10}"
temporary="$(mktemp -d)"
published=""
trap 'rm -rf "$temporary"; test -z "$published" || rm -f "$published"' EXIT

go test ./... -run '^$' -bench Benchmark -benchmem \
	-benchtime "$duration" -count "$samples" > "$temporary/current.txt"

awk '
/^Benchmark/ {
	name = $1
	sub(/-[0-9]+$/, "", name)
	ns = $3
	bytes = allocations = boxes = candidates = utilization = unpacked = "-"
	for (field = 4; field < NF; field++) {
		unit = $(field + 1)
		if (unit == "B/op") bytes = $field
		if (unit == "allocs/op") allocations = $field
		if (unit == "boxes/op") boxes = $field
		if (unit == "candidates/op") candidates = $field
		if (unit == "util_pct") utilization = $field
		if (unit == "unpacked/op") unpacked = $field
	}
	print name, ns, bytes, allocations, boxes, candidates, utilization, unpacked
}
' OFS='\t' "$temporary/current.txt" > "$temporary/samples.tsv"

awk -F '\t' '
function fail(message) { print message > "/dev/stderr"; failed = 1 }
function numeric(value) { return value == "-" ? -1 : value + 0 }
NR == FNR {
	if (FNR == 1) next
	p50[$1] = $2 + 0; p95[$1] = $3 + 0; max_bytes[$1] = $4 + 0
	max_allocs[$1] = $5 + 0; want_boxes[$1] = $6
	want_candidates[$1] = $7; min_util[$1] = $8
	want_unpacked[$1] = $9
	names[++name_count] = $1
	next
}
{
	name = $1
	if (!(name in p50)) { fail("benchmark has no reviewed threshold: " name); next }
	count[name]++
	times[name, count[name]] = $2 + 0
	if (numeric($3) > observed_bytes[name]) observed_bytes[name] = numeric($3)
	if (numeric($4) > observed_allocs[name]) observed_allocs[name] = numeric($4)
	if (want_boxes[name] != "-" && $5 != want_boxes[name]) fail(name " boxes/op=" $5 " want=" want_boxes[name])
	if (want_candidates[name] != "-" && $6 != want_candidates[name]) fail(name " candidates/op=" $6 " want=" want_candidates[name])
	if (min_util[name] != "-" && numeric($7) < numeric(min_util[name])) fail(name " util_pct=" $7 " minimum=" min_util[name])
	if (want_unpacked[name] != "-" && $8 != want_unpacked[name]) fail(name " unpacked/op=" $8 " want=" want_unpacked[name])
}
END {
	for (entry = 1; entry <= name_count; entry++) {
		name = names[entry]
		if (count[name] < 5) { fail(name " has fewer than five samples"); continue }
		for (left = 1; left <= count[name]; left++) {
			for (right = left + 1; right <= count[name]; right++) {
				if (times[name, left] > times[name, right]) {
					temporary = times[name, left]
					times[name, left] = times[name, right]
					times[name, right] = temporary
				}
			}
		}
		median = times[name, int((count[name] + 1) / 2)]
		p95_index = int((95 * count[name] + 99) / 100)
		tail = times[name, p95_index]
		if (median > p50[name]) fail(name " p50_ns=" median " budget=" p50[name])
		if (tail > p95[name]) fail(name " p95_ns=" tail " budget=" p95[name])
		if (observed_bytes[name] > max_bytes[name]) fail(name " B/op=" observed_bytes[name] " budget=" max_bytes[name])
		if (observed_allocs[name] > max_allocs[name]) fail(name " allocs/op=" observed_allocs[name] " budget=" max_allocs[name])
		printf "%s p50_ns=%d p95_ns=%d max_B/op=%d max_allocs/op=%d\n", name, median, tail, observed_bytes[name], observed_allocs[name]
	}
	exit failed
}
' specification/benchmark-thresholds.tsv "$temporary/samples.tsv"

if [ -n "${BENCH_RAW_OUTPUT:-}" ]; then
	case "$(uname -s)" in
	Darwin) processor="$(sysctl -n machdep.cpu.brand_string)" ;;
	Linux) processor="$(awk -F ': ' '/model name/ { print $2; exit }' /proc/cpuinfo)" ;;
	*) processor="unknown" ;;
	esac
	production_paths="$(git ls-files '*.go' | awk '!/_test\.go$/')"
	# Repository-controlled Go paths contain no shell metacharacters or spaces.
	# shellcheck disable=SC2086
	package_commit="$(git log -1 --format=%H -- $production_paths solver/benchmark_test.go)"
	input_sha256="$(./scripts/benchmark-input-digest.sh native)"
	published="$BENCH_RAW_OUTPUT.tmp.$$"
	{
		printf 'package_commit\t%s\n' "$package_commit"
		printf 'input_sha256\t%s\n' "$input_sha256"
		printf 'go_version\t%s\n' "$(go env GOVERSION)"
		printf 'environment\t%s/%s\n' "$(go env GOOS)" "$(go env GOARCH)"
		printf 'processor\t%s\n' "$processor"
		printf 'date\t%s\n' "$(date +%Y-%m-%d)"
		printf 'command\tBENCH_RAW_OUTPUT=%s ./scripts/benchmark-compare.sh %s\n' \
			"$BENCH_RAW_OUTPUT" "$duration"
		printf 'duration\t%s\n' "$duration"
		printf 'samples\t%s\n' "$samples"
		printf 'seed\t0\n'
		printf 'solver_parallelism\t1\n'
		printf 'gate_status\tpass\n'
		printf 'semantic_normalization\tverified integer lattice and exact constraints defined by each benchmark fixture\n'
		printf 'benchmark_source_sha256\t%s\n' \
			"$(shasum -a 256 solver/benchmark_test.go | awk '{ print $1 }')"
		printf 'generator_sha256\t%s\n' \
			"$(shasum -a 256 scripts/benchmark-compare.sh | awk '{ print $1 }')"
		printf 'threshold_sha256\t%s\n' \
			"$(shasum -a 256 specification/benchmark-thresholds.tsv | awk '{ print $1 }')"
		printf 'raw_format\tgo_test_benchmark\n'
		printf 'go_test_output\n'
		cat "$temporary/current.txt"
	} > "$published"
	mv "$published" "$BENCH_RAW_OUTPUT"
	published=""
fi
