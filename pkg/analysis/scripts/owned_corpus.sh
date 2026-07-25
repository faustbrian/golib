#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
owned_root=${OWNED_CORPUS_ROOT:-$(dirname "$root")}
policy=${OWNED_CORPUS_POLICY:-"$root/testdata/coverage/advisory.yml"}
evidence=${OWNED_CORPUS_EVIDENCE_ROOT:-"$root/.build/owned-corpus"}
runner=${OWNED_CORPUS_RUNNER:-"$root/scripts/corpus.sh"}
source_mode=${OWNED_CORPUS_SOURCE:-worktree}
cold_budget=${OWNED_CORPUS_MAX_COLD_MS:-180000}
warm_budget=${OWNED_CORPUS_MAX_WARM_MS:-180000}
peak_budget=${OWNED_CORPUS_MAX_PEAK_KIB:-524288}

case "$owned_root" in
	/*) ;;
	*)
		printf 'OWNED_CORPUS_ROOT must be absolute\n' >&2
		exit 2
		;;
esac
case "$policy" in
	/*) ;;
	*)
		printf 'OWNED_CORPUS_POLICY must be absolute\n' >&2
		exit 2
		;;
esac
case "$evidence" in
	/*) ;;
	*)
		printf 'OWNED_CORPUS_EVIDENCE_ROOT must be absolute\n' >&2
		exit 2
		;;
esac
if [ ! -d "$owned_root" ] || [ ! -f "$policy" ] || [ ! -x "$runner" ]; then
	printf 'owned corpus requires an existing root, policy, and runner\n' >&2
	exit 2
fi
case "$source_mode" in
	worktree|head) ;;
	*)
		printf 'OWNED_CORPUS_SOURCE must be worktree or head\n' >&2
		exit 2
		;;
esac
for budget in "$cold_budget" "$warm_budget" "$peak_budget"; do
	case "$budget" in
		''|0|*[!0-9]*)
			printf 'owned corpus performance budgets must be positive integers\n' >&2
			exit 2
			;;
	esac
done
if [ "$cold_budget" -gt 86400000 ] || [ "$warm_budget" -gt 86400000 ] || \
	[ "$peak_budget" -gt 1073741824 ]; then
	printf 'owned corpus performance budget is excessive\n' >&2
	exit 2
fi

platform=$(uname -s)
case "$platform" in
	Darwin|Linux) ;;
	*)
		printf 'owned corpus performance measurement is unsupported on %s\n' \
			"$platform" >&2
		exit 2
		;;
esac
if [ ! -x /usr/bin/time ]; then
	printf 'owned corpus performance measurement requires /usr/bin/time\n' >&2
	exit 2
fi

temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-owned-corpus.XXXXXX")
cleanup() {
	chmod -R u+w "$temporary" 2>/dev/null || true
	rm -rf "$temporary"
}
trap cleanup EXIT HUP INT TERM
source_root=$owned_root
head_metadata="$temporary/head-metadata.tsv"
: > "$head_metadata"
if [ "$source_mode" = head ]; then
	source_root="$temporary/modules"
	mkdir -p "$source_root"
	for module_file in "$owned_root"/go-*/go.mod; do
		[ -f "$module_file" ] || continue
		repository=$(dirname "$module_file")
		name=${repository##*/}
		head=$(git -C "$repository" rev-parse HEAD)
		tree=$(git -C "$repository" rev-parse 'HEAD^{tree}')
		archive="$temporary/$name.tar"
		git -C "$repository" archive --format=tar HEAD > "$archive"
		archive_hash=$(shasum -a 256 "$archive" | awk '{print $1}')
		mkdir -p "$source_root/$name"
		tar -xf "$archive" -C "$source_root/$name"
		printf '%s\t%s\t%s\t%s\n' \
			"$name" "$head" "$tree" "$archive_hash" >> "$head_metadata"
	done
	chmod -R a-w "$source_root"
fi
mkdir -p "$evidence/reports"
cp "$policy" "$evidence/policy.yml"
manifest="$evidence/manifest.tsv"
: > "$manifest"

tab=$(printf '\t')
count=0
for module_file in "$owned_root"/go-*/go.mod; do
	[ -f "$module_file" ] || continue
	repository=$(dirname "$module_file")
	name=${repository##*/}
	case "$name" in
		go-*)
			suffix=${name#go-}
			case "$suffix" in
				''|*[!a-z0-9._-]*)
					printf 'invalid owned corpus repository: %s\n' "$name" >&2
					exit 1
					;;
			esac
			;;
		*)
			printf 'invalid owned corpus repository: %s\n' "$name" >&2
			exit 1
			;;
	esac
	count=$((count + 1))
	if [ "$count" -gt 128 ]; then
		printf 'owned corpus exceeds 128 repositories\n' >&2
		exit 1
	fi
	printf '%s%s%s%spolicy.yml%sreports/%s.json\n' \
		"$name" "$tab" "$name" "$tab" "$tab" "$name" >> "$manifest"
done
if [ "$count" -eq 0 ]; then
	printf 'owned corpus contains no direct go-* modules\n' >&2
	exit 1
fi

snapshot() {
	output=$1
	: > "$output"
	if [ "$source_mode" = head ]; then
		while IFS="$tab" read -r name head tree archive_hash; do
			content_hash=$(
				cd "$source_root/$name"
				find . -type f -print | LC_ALL=C sort | while IFS= read -r path; do
					printf '%s\t' "$path"
					shasum -a 256 "$path" | awk '{print $1}'
				done | shasum -a 256 | awk '{print $1}'
			)
			printf '%s\t%s\t%s\t%s\t%s\n' \
				"$name" "$head" "$tree" "$archive_hash" "$content_hash" >> "$output"
		done < "$head_metadata"
		return
	fi
	index=0
	for module_file in "$owned_root"/go-*/go.mod; do
		[ -f "$module_file" ] || continue
		repository=$(dirname "$module_file")
		name=${repository##*/}
		index=$((index + 1))
		if ! head=$(git -C "$repository" rev-parse HEAD 2>/dev/null); then
			printf 'owned corpus repository is not committed: %s\n' "$name" >&2
			exit 1
		fi
		diff_hash=$(git -C "$repository" diff --binary --no-ext-diff HEAD -- | \
			shasum -a 256 | awk '{print $1}')
		untracked="$temporary/untracked.$index"
		git -C "$repository" ls-files --others --exclude-standard -z > "$untracked"
		path_hash=$(shasum -a 256 "$untracked" | awk '{print $1}')
		if [ -s "$untracked" ]; then
			content_hash=$(
				cd "$repository"
				xargs -0 git hash-object -- < "$untracked" | \
					shasum -a 256 | awk '{print $1}'
			)
		else
			content_hash=$(shasum -a 256 /dev/null | awk '{print $1}')
		fi
		printf '%s\t%s\t%s\t%s\t%s\n' \
			"$name" "$head" "$diff_hash" "$path_hash" "$content_hash" >> "$output"
	done
}

report_changes() {
	before_snapshot=$1
	after_snapshot=$2
	awk '
		NR == FNR { before[$1] = $0; next }
		{
			seen[$1] = 1
			if (!($1 in before) || before[$1] != $0) print $1
		}
		END {
			for (name in before) if (!(name in seen)) print name
		}
	' "$before_snapshot" "$after_snapshot" | LC_ALL=C sort -u | \
		while IFS= read -r changed; do
			[ -n "$changed" ] && printf 'changed repository: %s\n' "$changed" >&2
		done
}

measure_corpus() {
	measurement_mode=$1
	metrics=$2
	case "$platform" in
		Darwin)
			/usr/bin/time -l -o "$metrics" env \
				CORPUS_MODULE_ROOT="$source_root" \
				CORPUS_POLICY_ROOT="$evidence" \
				CORPUS_BASELINE_ROOT="$evidence" \
				CORPUS_REPLACE_ROOT="$source_root" \
				"$runner" "$measurement_mode" "$manifest"
			;;
		Linux)
			/usr/bin/time -f '%e\t%M' -o "$metrics" env \
				CORPUS_MODULE_ROOT="$source_root" \
				CORPUS_POLICY_ROOT="$evidence" \
				CORPUS_BASELINE_ROOT="$evidence" \
				CORPUS_REPLACE_ROOT="$source_root" \
				"$runner" "$measurement_mode" "$manifest"
			;;
	esac
}

read_measurement() {
	metrics=$1
	case "$platform" in
		Darwin)
			seconds=$(awk 'NR == 1 { print $1 }' "$metrics")
			peak=$(awk '/maximum resident set size/ { print $1; exit }' \
				"$metrics")
			peak_kib=$(awk -v bytes="$peak" \
				'BEGIN { print int((bytes + 1023) / 1024) }')
			;;
		Linux)
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

before="$temporary/before.tsv"
after_update="$temporary/after-update.tsv"
after_check="$temporary/after-check.tsv"
snapshot "$before"

if ! measure_corpus update "$temporary/cold.time"; then
	printf 'owned corpus cold performance run failed\n' >&2
	exit 1
fi
cold=$(read_measurement "$temporary/cold.time") || {
	printf 'owned corpus cold performance metrics are malformed\n' >&2
	exit 1
}
snapshot "$after_update"
if ! cmp -s "$before" "$after_update"; then
	printf 'owned corpus changed during analysis update\n' >&2
	report_changes "$before" "$after_update"
	exit 1
fi

if ! measure_corpus check "$temporary/warm.time"; then
	printf 'owned corpus warm performance run failed\n' >&2
	exit 1
fi
warm=$(read_measurement "$temporary/warm.time") || {
	printf 'owned corpus warm performance metrics are malformed\n' >&2
	exit 1
}
snapshot "$after_check"
if ! cmp -s "$before" "$after_check"; then
	printf 'owned corpus changed during analysis check\n' >&2
	report_changes "$before" "$after_check"
	exit 1
fi

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
	printf 'owned corpus exceeded performance budget: cold=%sms/%sms warm=%sms/%sms peak=%sKiB/%sKiB\n' \
		"$cold_ms" "$cold_budget" "$warm_ms" "$warm_budget" \
		"$peak_kib" "$peak_budget" >&2
	exit 1
fi
performance="$evidence/performance.tsv"
printf 'name%scold_ms%swarm_ms%speak_kib%smax_cold_ms%smax_warm_ms%smax_peak_kib\n' \
	"$tab" "$tab" "$tab" "$tab" "$tab" "$tab" > "$performance"
printf 'full-corpus%s%s%s%s%s%s%s%s%s%s%s%s%s\n' \
	"$tab" "$cold_ms" "$tab" "$warm_ms" "$tab" "$peak_kib" \
	"$tab" "$cold_budget" "$tab" "$warm_budget" "$tab" "$peak_budget" \
	>> "$performance"
cp "$after_check" "$evidence/revisions.tsv"
printf 'owned corpus verified: %d module(s)\n' "$count"
