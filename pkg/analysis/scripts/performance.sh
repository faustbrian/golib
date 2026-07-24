#!/bin/sh

set -eu

manifest=${1:-}
if [ -z "$manifest" ]; then
	printf 'usage: %s <manifest>\n' "$0" >&2
	exit 2
fi

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
binary=${PERFORMANCE_BINARY:-"$root/.build/golib-analysis"}
module_root=${CORPUS_MODULE_ROOT:-"$root"}
policy_root=${CORPUS_POLICY_ROOT:-}
report=${PERFORMANCE_REPORT:-"$root/.build/performance.tsv"}
case "$manifest" in
	/*) manifest_path=$manifest ;;
	*) manifest_path="$root/$manifest" ;;
esac
if [ ! -x "$binary" ]; then
	printf 'performance runner requires an executable analyzer: %s\n' "$binary" >&2
	exit 1
fi
if [ ! -f "$manifest_path" ]; then
	printf 'performance manifest not found: %s\n' "$manifest" >&2
	exit 1
fi

platform=$(uname -s)
case "$platform" in
	Darwin|Linux) ;;
	*)
		printf 'performance measurement is unsupported on %s\n' "$platform" >&2
		exit 1
		;;
esac
if [ ! -x /usr/bin/time ]; then
	printf 'performance measurement requires /usr/bin/time\n' >&2
	exit 1
fi

temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-performance.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
results="$temporary/results.tsv"
tab=$(printf '\t')
printf 'name%scold_ms%swarm_ms%speak_kib\n' "$tab" "$tab" "$tab" > "$results"
count=0

measure() {
	measurement_name=$1
	measurement_module=$2
	measurement_config=$3
	metrics="$temporary/$measurement_name.time"
	case "$platform" in
		Darwin)
			if ! (cd "$measurement_module" && /usr/bin/time -l -o "$metrics" \
				env GOWORK=off "$binary" check -config "$measurement_config" \
				-root "$measurement_module" -format json ./... > /dev/null); then
				return 1
			fi
			seconds=$(awk 'NR == 1 { print $1 }' "$metrics")
			peak=$(awk '/maximum resident set size/ { print $1; exit }' "$metrics")
			peak_kib=$(awk -v bytes="$peak" 'BEGIN { print int((bytes + 1023) / 1024) }')
			;;
		Linux)
			if ! (cd "$measurement_module" && /usr/bin/time -f "%e${tab}%M" \
				-o "$metrics" env GOWORK=off "$binary" check \
				-config "$measurement_config" -root "$measurement_module" \
				-format json ./... > /dev/null); then
				return 1
			fi
			seconds=$(awk -F "$tab" 'NR == 1 { print $1 }' "$metrics")
			peak_kib=$(awk -F "$tab" 'NR == 1 { print $2 }' "$metrics")
			;;
	esac
	case "$seconds:$peak_kib" in
		*[!0-9.:]*|:*|*:) return 1 ;;
	esac
	elapsed_ms=$(awk -v seconds="$seconds" \
		'BEGIN { print int((seconds * 1000) + 0.999999) }')
	printf '%s%s%s\n' "$elapsed_ms" "$tab" "$peak_kib"
}

while IFS="$tab" read -r name module policy cold_budget warm_budget peak_budget extra; do
	case "$name" in
		''|'#'*) continue ;;
	esac
	count=$((count + 1))
	if [ "$count" -gt 128 ]; then
		printf 'performance manifest exceeds 128 entries\n' >&2
		exit 1
	fi
	case "$name" in
		*[!a-z0-9._-]*)
			printf 'invalid performance name: %s\n' "$name" >&2
			exit 1
			;;
	esac
	if [ -n "$extra" ] || [ -z "$module" ] || [ -z "$policy" ] || \
		[ -z "$cold_budget" ] || [ -z "$warm_budget" ] || [ -z "$peak_budget" ]; then
		printf 'performance entry %s requires six tab-separated fields\n' "$name" >&2
		exit 1
	fi
	for path in "$module" "$policy"; do
		case "$path" in
			/*|..|../*|*/../*|*/..)
				printf 'performance entry %s contains an escaping path\n' "$name" >&2
				exit 1
				;;
		esac
	done
	for budget in "$cold_budget" "$warm_budget" "$peak_budget"; do
		case "$budget" in
			''|0|*[!0-9]*)
				printf 'performance entry %s has an invalid budget\n' "$name" >&2
				exit 1
				;;
		esac
	done
	if [ "$cold_budget" -gt 86400000 ] || [ "$warm_budget" -gt 86400000 ] || \
		[ "$peak_budget" -gt 1073741824 ]; then
		printf 'performance entry %s has an excessive budget\n' "$name" >&2
		exit 1
	fi

	module_dir="$module_root/$module"
	if [ -n "$policy_root" ]; then
		config_path="$policy_root/$policy"
	else
		config_path="$module_dir/$policy"
	fi
	if [ ! -d "$module_dir" ] || [ ! -f "$config_path" ]; then
		printf 'performance entry %s has a missing module or policy\n' "$name" >&2
		exit 1
	fi
	cold=$(measure "$name.cold" "$module_dir" "$config_path") || {
		printf 'cold performance run failed for %s\n' "$name" >&2
		exit 1
	}
	warm=$(measure "$name.warm" "$module_dir" "$config_path") || {
		printf 'warm performance run failed for %s\n' "$name" >&2
		exit 1
	}
	cold_ms=${cold%%"$tab"*}
	cold_peak=${cold#*"$tab"}
	warm_ms=${warm%%"$tab"*}
	warm_peak=${warm#*"$tab"}
	peak_kib=$cold_peak
	if [ "$warm_peak" -gt "$peak_kib" ]; then
		peak_kib=$warm_peak
	fi
	if [ "$cold_ms" -gt "$cold_budget" ] || [ "$warm_ms" -gt "$warm_budget" ] || \
		[ "$peak_kib" -gt "$peak_budget" ]; then
		printf '%s exceeded performance budget: cold=%sms/%sms warm=%sms/%sms peak=%sKiB/%sKiB\n' \
			"$name" "$cold_ms" "$cold_budget" "$warm_ms" "$warm_budget" \
			"$peak_kib" "$peak_budget" >&2
		exit 1
	fi
	printf '%s%s%s%s%s%s%s\n' "$name" "$tab" "$cold_ms" "$tab" \
		"$warm_ms" "$tab" "$peak_kib" >> "$results"
done < "$manifest_path"

if [ "$count" -eq 0 ]; then
	printf 'performance manifest contains no entries\n' >&2
	exit 1
fi
mkdir -p "$(dirname "$report")"
mv "$results" "$report"
printf 'performance verified: %d module(s)\n' "$count"
