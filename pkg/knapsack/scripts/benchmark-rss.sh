#!/bin/sh
set -eu

parse_rss() {
	format="$1"
	input="$2"
	case "$format" in
		darwin)
			awk '/maximum resident set size$/ { print $1; found = 1 }
				END { if (!found) exit 1 }' "$input"
			;;
		linux)
			awk '/Maximum resident set size \(kbytes\):/ {
				printf "%.0f\n", $NF * 1024; found = 1
			}
			END { if (!found) exit 1 }' "$input"
			;;
		*)
			return 1
			;;
	esac
}

if [ "${1:-}" = "--parse" ]; then
	test "$#" -eq 3
	parse_rss "$2" "$3"
	exit
fi

duration="${1:-100ms}"
samples="${BENCH_RSS_SAMPLES:-5}"
thresholds="${BENCH_RSS_THRESHOLDS:-specification/benchmark-rss-thresholds.tsv}"
temporary="$(mktemp -d)"
published=""
trap 'rm -rf "$temporary"; test -z "$published" || rm -f "$published"' EXIT

case "$samples" in
	''|*[!0-9]*)
		printf 'BENCH_RSS_SAMPLES must be a positive integer\n' >&2
		exit 1
		;;
esac
test "$samples" -ge 5 || {
	printf 'BENCH_RSS_SAMPLES must be at least five\n' >&2
	exit 1
}
test -x /usr/bin/time || {
	printf '%s\n' '/usr/bin/time is required for peak RSS measurement' >&2
	exit 1
}
test -s "$thresholds" || {
	printf 'missing RSS threshold file: %s\n' "$thresholds" >&2
	exit 1
}

case "$(uname -s)" in
	Darwin) format=darwin ;;
	Linux) format=linux ;;
	*)
		printf 'peak RSS measurement is unsupported on %s\n' "$(uname -s)" >&2
		exit 1
		;;
esac

go test -c -o "$temporary/solver.test" ./solver
raw="$temporary/raw.tsv"
: > "$raw"

"$temporary/solver.test" -test.list '^Benchmark' | sort > "$temporary/available.txt"
tail -n +2 "$thresholds" | cut -f 1 | sort > "$temporary/configured.txt"
test -z "$(uniq -d "$temporary/configured.txt")" || {
	printf '%s\n' 'RSS threshold file contains duplicate benchmarks' >&2
	exit 1
}
cmp -s "$temporary/available.txt" "$temporary/configured.txt" || {
	printf '%s\n' 'RSS thresholds do not exactly cover solver benchmarks' >&2
	diff -u "$temporary/available.txt" "$temporary/configured.txt" >&2 || true
	exit 1
}

tail -n +2 "$thresholds" | while IFS="$(printf '\t')" read -r benchmark budget; do
	test -n "$benchmark" || continue
	case "$budget" in
	''|*[!0-9]*)
		printf 'invalid RSS budget for %s: %s\n' "$benchmark" "$budget" >&2
		exit 1
		;;
	esac
	sample=1
	while [ "$sample" -le "$samples" ]; do
		benchmark_output="$temporary/$benchmark.$sample.bench"
		time_output="$temporary/$benchmark.$sample.time"
		if [ "$format" = darwin ]; then
			/usr/bin/time -l "$temporary/solver.test" -test.run '^$' \
				-test.bench "^$benchmark$" -test.benchtime "$duration" \
				-test.count 1 > "$benchmark_output" 2> "$time_output"
		else
			/usr/bin/time -v "$temporary/solver.test" -test.run '^$' \
				-test.bench "^$benchmark$" -test.benchtime "$duration" \
				-test.count 1 > "$benchmark_output" 2> "$time_output"
		fi
		grep -q "^$benchmark-" "$benchmark_output" || {
			printf 'benchmark did not execute: %s\n' "$benchmark" >&2
			exit 1
		}
		rss="$(parse_rss "$format" "$time_output")"
		case "$rss" in
		''|*[!0-9]*)
			printf 'invalid peak RSS for %s: %s\n' "$benchmark" "$rss" >&2
			exit 1
			;;
		esac
		printf '%s\t%s\t%s\t%s\n' "$benchmark" "$sample" "$rss" "$budget" >> "$raw"
		sample=$((sample + 1))
	done
done

awk -F '\t' '
{
	name = $1
	count[name]++
	if ($3 > maximum[name]) maximum[name] = $3
	budget[name] = $4
	if (!(name in seen)) { names[++name_count] = name; seen[name] = 1 }
}
END {
	for (entry = 1; entry <= name_count; entry++) {
		name = names[entry]
		if (count[name] < 5) {
			printf "%s has fewer than five RSS samples\n", name > "/dev/stderr"
			failed = 1
		}
		if (maximum[name] > budget[name]) {
			printf "%s peak_RSS_bytes=%d budget=%d\n", name,
				maximum[name], budget[name] > "/dev/stderr"
			failed = 1
		}
		printf "%s peak_RSS_bytes=%d budget=%d samples=%d\n", name,
			maximum[name], budget[name], count[name]
	}
	exit failed
}' "$raw"

if [ -n "${BENCH_RSS_RAW_OUTPUT:-}" ]; then
	if [ "$format" = darwin ]; then
		processor="$(sysctl -n machdep.cpu.brand_string)"
	else
		processor="$(awk -F ': ' '/model name/ { print $2; exit }' /proc/cpuinfo)"
	fi
	production_paths="$(git ls-files '*.go' | awk '!/_test\.go$/')"
	# Repository-controlled Go paths contain no shell metacharacters or spaces.
	# shellcheck disable=SC2086
	package_commit="$(git log -1 --format=%H -- $production_paths solver/benchmark_test.go)"
	input_sha256="$(./scripts/benchmark-input-digest.sh rss)"
	published="$BENCH_RSS_RAW_OUTPUT.tmp.$$"
	{
		printf 'package_commit\t%s\n' "$package_commit"
		printf 'input_sha256\t%s\n' "$input_sha256"
		printf 'go_version\t%s\n' "$(go env GOVERSION)"
		printf 'environment\t%s/%s\n' "$(go env GOOS)" "$(go env GOARCH)"
		printf 'processor\t%s\n' "$processor"
		printf 'date\t%s\n' "$(date +%Y-%m-%d)"
		printf 'command\tBENCH_RSS_RAW_OUTPUT=%s ./scripts/benchmark-rss.sh %s\n' \
			"$BENCH_RSS_RAW_OUTPUT" "$duration"
		printf 'time_format\t%s\n' "$format"
		printf 'duration\t%s\n' "$duration"
		printf 'samples\t%s\n' "$samples"
		printf 'seed\t0\n'
		printf 'solver_parallelism\t1\n'
		printf 'gate_status\tpass\n'
		printf 'semantic_normalization\tverified integer lattice and exact constraints defined by each benchmark fixture\n'
		printf 'benchmark_source_sha256\t%s\n' \
			"$(shasum -a 256 solver/benchmark_test.go | awk '{ print $1 }')"
		printf 'generator_sha256\t%s\n' \
			"$(shasum -a 256 scripts/benchmark-rss.sh | awk '{ print $1 }')"
		printf 'threshold_sha256\t%s\n' \
			"$(shasum -a 256 "$thresholds" | awk '{ print $1 }')"
		printf 'benchmark\tsample\tpeak_rss_bytes\tbudget_bytes\n'
		cat "$raw"
	} > "$published"
	mv "$published" "$BENCH_RSS_RAW_OUTPUT"
	published=""
fi
