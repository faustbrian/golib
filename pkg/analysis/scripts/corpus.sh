#!/bin/sh

set -eu

mode=${1:-check}
manifest=${2:-}
case "$mode" in
	check|update) ;;
	*)
		printf 'usage: %s check|update <manifest>\n' "$0" >&2
		exit 2
		;;
esac
if [ -z "$manifest" ]; then
	printf 'usage: %s check|update <manifest>\n' "$0" >&2
	exit 2
fi

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
binary=${CORPUS_BINARY:-"$root/.build/golib-analysis"}
module_root=${CORPUS_MODULE_ROOT:-"$root"}
policy_root=${CORPUS_POLICY_ROOT:-}
baseline_root=${CORPUS_BASELINE_ROOT:-"$root"}
replace_root=${CORPUS_REPLACE_ROOT:-}
case "$manifest" in
	/*) manifest_path=$manifest ;;
	*) manifest_path="$root/$manifest" ;;
esac
if [ ! -x "$binary" ]; then
	printf 'corpus runner requires .build/golib-analysis; run make build\n' >&2
	exit 1
fi
if [ ! -f "$manifest_path" ]; then
	printf 'corpus manifest not found: %s\n' "$manifest" >&2
	exit 1
fi
if [ -n "$replace_root" ]; then
	case "$replace_root" in
		/*) ;;
		*)
			printf 'CORPUS_REPLACE_ROOT must be absolute\n' >&2
			exit 1
			;;
	esac
	if [ ! -d "$replace_root" ]; then
		printf 'CORPUS_REPLACE_ROOT is not a directory: %s\n' "$replace_root" >&2
		exit 1
	fi
fi

temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-corpus.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
tab=$(printf '\t')
count=0

while IFS="$tab" read -r name module policy baseline extra; do
	case "$name" in
		''|'#'*) continue ;;
	esac
	count=$((count + 1))
	if [ "$count" -gt 128 ]; then
		printf 'corpus manifest exceeds 128 entries\n' >&2
		exit 1
	fi
	case "$name" in
		*[!a-z0-9._-]*)
			printf 'invalid corpus name: %s\n' "$name" >&2
			exit 1
			;;
	esac
	if [ -n "$extra" ] || [ -z "$module" ] || [ -z "$policy" ] || [ -z "$baseline" ]; then
		printf 'corpus entry %s requires four tab-separated fields\n' "$name" >&2
		exit 1
	fi
	for path in "$module" "$policy" "$baseline"; do
		case "$path" in
			/*|..|../*|*/../*|*/..)
				printf 'corpus entry %s contains an escaping path\n' "$name" >&2
				exit 1
				;;
		esac
	done

	module_dir="$module_root/$module"
	if [ -n "$policy_root" ]; then
		config_path="$policy_root/$policy"
	else
		config_path="$module_dir/$policy"
	fi
	baseline_path="$baseline_root/$baseline"
	if [ ! -d "$module_dir" ] || [ ! -f "$config_path" ]; then
		printf 'corpus entry %s has a missing module or policy\n' "$name" >&2
		exit 1
	fi

	workspace=off
	if [ -n "$replace_root" ]; then
		workspace_dir="$temporary/$name.workspace"
		mkdir -p "$workspace_dir"
		if ! (cd "$workspace_dir" && GOWORK=off go work init "$module_dir"); then
			printf 'corpus entry %s could not initialize its workspace\n' "$name" >&2
			exit 1
		fi
		workspace="$workspace_dir/go.work"
		target_module=$(awk '$1 == "module" && NF == 2 { print $2; exit }' \
			"$module_dir/go.mod")
		if [ -z "$target_module" ]; then
			printf 'corpus entry %s has no module directive\n' "$name" >&2
			exit 1
		fi
		seen="$workspace_dir/modules"
		: > "$seen"
		replacement_count=0
		for module_file in "$replace_root"/*/go.mod; do
			[ -f "$module_file" ] || continue
			candidate=$(dirname "$module_file")
			module_path=$(awk '$1 == "module" && NF == 2 { print $2; exit }' \
				"$module_file")
			if [ -z "$module_path" ]; then
				printf 'replacement module has no module directive: %s\n' \
					"$module_file" >&2
				exit 1
			fi
			if grep -F -x "$module_path" "$seen" >/dev/null; then
				printf 'duplicate replacement module path: %s\n' "$module_path" >&2
				exit 1
			fi
			printf '%s\n' "$module_path" >> "$seen"
			[ "$module_path" = "$target_module" ] && continue
			replacement_count=$((replacement_count + 1))
			if [ "$replacement_count" -gt 128 ]; then
				printf 'local replacement corpus exceeds 128 modules\n' >&2
				exit 1
			fi
			if ! GOWORK="$workspace" go work edit \
				-replace="$module_path=$candidate"; then
				printf 'could not add local replacement %s\n' "$module_path" >&2
				exit 1
			fi
		done
	fi

	parallel="$temporary/$name.parallel.json"
	sequential="$temporary/$name.sequential.json"
	if ! (cd "$module_dir" && GOWORK="$workspace" "$binary" check \
		-config "$config_path" -root "$module_dir" -format json ./...) > "$parallel"; then
		printf 'corpus entry %s produced a blocking or analyzer failure\n' "$name" >&2
		exit 1
	fi
	if ! (cd "$module_dir" && GOWORK="$workspace" "$binary" check \
		-sequential -config "$config_path" -root "$module_dir" \
		-format json ./...) > "$sequential"; then
		printf 'sequential corpus entry %s failed\n' "$name" >&2
		exit 1
	fi
	if ! cmp -s "$parallel" "$sequential"; then
		printf 'corpus entry %s differs between parallel and sequential runs\n' "$name" >&2
		exit 1
	fi

	if [ "$mode" = update ]; then
		mkdir -p "$(dirname "$baseline_path")"
		cp "$parallel" "$baseline_path"
		continue
	fi
	if [ ! -f "$baseline_path" ]; then
		printf 'corpus baseline missing for %s: %s\n' "$name" "$baseline" >&2
		exit 1
	fi
	if ! cmp -s "$baseline_path" "$parallel"; then
		printf 'corpus report drifted for %s\n' "$name" >&2
		printf 'run make corpus-update only after classifying every change\n' >&2
		exit 1
	fi
done < "$manifest_path"

if [ "$count" -eq 0 ]; then
	printf 'corpus manifest contains no entries\n' >&2
	exit 1
fi
printf 'corpus verified: %d module(s)\n' "$count"
