#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
directory=${1:-"$root/.github/workflows"}
if [ ! -d "$directory" ]; then
	printf 'workflow directory not found: %s\n' "$directory" >&2
	exit 1
fi

count=0
for workflow in "$directory"/*.yml "$directory"/*.yaml; do
	[ -f "$workflow" ] || continue
	count=$((count + 1))
	name=$(basename "$workflow")

	if ! awk '
		$0 == "permissions:" {
			if (getline > 0 && $0 == "  contents: read") {
				found = 1
			}
		}
		END { exit found ? 0 : 1 }
	' "$workflow"; then
		printf '%s: workflow permissions must default to contents read\n' \
			"$name" >&2
		exit 1
	fi
	if grep -E '^[[:space:]]*permissions:[[:space:]]*[^[:space:]]' \
		"$workflow" >/dev/null; then
		printf '%s: workflow permissions must default to contents read\n' \
			"$name" >&2
		exit 1
	fi

	grep -E '^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]*' "$workflow" | \
	while IFS= read -r line; do
		action=$(printf '%s\n' "$line" | sed -E \
			-e 's/^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]*//' \
			-e 's/[[:space:]]+#.*$//')
		case "$action" in
			./*) continue ;;
			docker://*@sha256:*)
				if printf '%s\n' "$action" | grep -E \
					'^docker://[^[:space:]]+@sha256:[0-9a-f]{64}$' >/dev/null; then
					continue
				fi
				;;
		esac
		if ! printf '%s\n' "$action" | grep -E \
			'^[^@[:space:]]+@[0-9a-f]{40}$' >/dev/null; then
			printf '%s: action is not pinned to a commit SHA: %s\n' \
				"$name" "$action" >&2
			exit 1
		fi
	done

	if [ "$name" = "ci.yml" ]; then
		for platform in ubuntu-latest macos-latest windows-latest; do
			platform_count=$(grep -Ec \
				"^[[:space:]]*-[[:space:]]+$platform$" \
				"$workflow" || true)
			if [ "$platform_count" -ne 1 ]; then
				printf '%s: CI must execute analyzer tests on Linux, macOS, and Windows\n' \
					"$name" >&2
				exit 1
			fi
		done
		if ! awk '
			$0 == "      - name: Run portable Windows gates" {
				gate++
				inside = 1
				next
			}
			inside && $0 ~ /^      - name:/ { inside = 0 }
			inside && $0 == "        if: runner.os == '\''Windows'\''" {
				condition = 1
			}
			inside && $0 == "          go vet ./..." { vet = 1 }
			inside && $0 == "          go test ./..." { test = 1 }
			inside && $0 == "          go test -race ./..." { race = 1 }
			inside && $0 == "          go build -trimpath ./cmd/golib-analysis" {
				build = 1
			}
			END {
				exit gate == 1 && condition && vet && test && race && build ? 0 : 1
			}
		' "$workflow"; then
			printf '%s: CI must run the portable Windows analyzer gate\n' \
				"$name" >&2
			exit 1
		fi

		init_count=$(grep -Ec \
			'uses:[[:space:]]+github/codeql-action/init@[0-9a-f]{40}' \
			"$workflow" || true)
		analyze_count=$(grep -Ec \
			'uses:[[:space:]]+github/codeql-action/analyze@[0-9a-f]{40}' \
			"$workflow" || true)
		init_revision=$(sed -n -E \
			's|.*uses:[[:space:]]+github/codeql-action/init@([0-9a-f]{40}).*|\1|p' \
			"$workflow")
		analyze_revision=$(sed -n -E \
			's|.*uses:[[:space:]]+github/codeql-action/analyze@([0-9a-f]{40}).*|\1|p' \
			"$workflow")
		if [ "$init_count" -ne 1 ] || [ "$analyze_count" -ne 1 ] ||
			[ "$init_revision" != "$analyze_revision" ]; then
			printf '%s: CI requires pinned CodeQL init and analyze actions on one revision\n' \
				"$name" >&2
			exit 1
		fi
		build_mode_count=$(grep -Ec \
			'^[[:space:]]+build-mode:[[:space:]]+manual$' "$workflow" || true)
		language_count=$(grep -Ec \
			'^[[:space:]]+languages:[[:space:]]+go$' "$workflow" || true)
		build_count=$(grep -Ec \
			'^[[:space:]]+run:[[:space:]]+go build -trimpath \.\/\.\.\.$' \
			"$workflow" || true)
		if [ "$build_mode_count" -ne 1 ] || [ "$language_count" -ne 1 ] || \
			[ "$build_count" -ne 1 ]; then
			printf '%s: CodeQL must use the reviewed manual Go build\n' \
				"$name" >&2
			exit 1
		fi
	fi

	grep -E '^[[:space:]]+[a-z][a-z-]*:[[:space:]]+write$' "$workflow" | \
	while IFS= read -r permission; do
		[ -n "$permission" ] || continue
		case "$name:$permission" in
			'ci.yml:      security-events: write'|\
			'release.yml:      contents: write') continue ;;
		esac
		printf '%s: unauthorized write permission: %s\n' \
			"$name" "$permission" >&2
		exit 1
	done
	setup_go_count=$(grep -Ec \
		'uses:[[:space:]]+actions/setup-go@[0-9a-f]{40}' \
		"$workflow" || true)
	version_file_count=$(grep -Ec \
		'^[[:space:]]+go-version-file:[[:space:]]+\.go-version$' \
		"$workflow" || true)
	if [ "$setup_go_count" -eq 0 ] || \
		[ "$setup_go_count" -ne "$version_file_count" ] || \
		grep -E '^[[:space:]]+go-version:' "$workflow" >/dev/null; then
		printf '%s: workflow must use the canonical .go-version toolchain\n' \
			"$name" >&2
		exit 1
	fi
done

if [ "$count" -eq 0 ]; then
	printf 'workflow directory contains no YAML files\n' >&2
	exit 1
fi

printf 'workflow policy verified: %d file(s)\n' "$count"
