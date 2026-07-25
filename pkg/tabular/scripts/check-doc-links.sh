#!/usr/bin/env bash
set -euo pipefail

failed=0
while IFS= read -r match; do
    file="${match%%:*}"
    link="${match#*:}"
    target="${link#*](}"
    target="${target%)}"
    target="${target%%#*}"
    case "${target}" in
        ""|http://*|https://*|mailto:*) continue ;;
    esac
    if [[ ! -e "$(dirname "${file}")/${target}" ]]; then
        echo "${file}: broken local link ${target}" >&2
        failed=1
    fi
done < <(rg --with-filename --only-matching '\]\([^)]*\)' --glob '*.md')

exit "${failed}"
