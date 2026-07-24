#!/usr/bin/env bash
set -euo pipefail

profile="$(mktemp)"
report="$(mktemp)"
trap 'rm -f "${profile}" "${report}"' EXIT

packages=()
while IFS= read -r package; do
    if [[ "${package}" != */examples/* ]]; then
        packages+=("${package}")
    fi
done < <(go list ./...)

go test -covermode=atomic -coverprofile="${profile}" "${packages[@]}"
go tool cover -func="${profile}" | tee "${report}"

awk '
$1 == "total:" {
    gsub(/%/, "", $3)
    found = 1
    if (($3 + 0) != 100) {
        printf "statement coverage is %s%%, want 100%%\n", $3 > "/dev/stderr"
        exit 1
    }
}
END {
    if (!found) {
        print "coverage report has no total" > "/dev/stderr"
        exit 1
    }
}
' "${report}"
