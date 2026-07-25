#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    printf 'usage: %s <module-directory>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
module="$1"
directory="${root}/${module}"
artifact="${root}/.artifacts/${module}"
profile="${artifact}/coverage.out"
packages="${artifact}/coverage-packages.txt"
report="${artifact}/coverage-report.txt"
mkdir -p "${artifact}"

cd "${directory}"
jq -r --arg directory "${module}" '
    .modules[]
    | select(.directory == $directory)
    | .packages[]
    | select(.coverage_required == true)
    | .import_path
' "${root}/modules.json" | sort -u >"${packages}"
[[ -s "${packages}" ]] || {
    printf 'coverage expected packages are empty for %s\n' "${module}" >&2
    exit 1
}

tags="$(jq -r --arg directory "${module}" \
    '.modules[] | select(.directory == $directory) | .test_tags | join(",")' \
    "${root}/modules.json")"
test_arguments=(./... -count=1 -covermode=atomic -coverpkg=./... -coverprofile="${profile}")
if [[ -n "${tags}" ]]; then
    test_arguments=(-tags="${tags}" "${test_arguments[@]}")
fi
if [[ "${module}" == "pkg/xsd" ]]; then
    xsts_work="${artifact}/xsts"
    mkdir -p "${xsts_work}"
    XSTS_ROOT="$(./scripts/prepare-xsts.sh "${xsts_work}")"
    export XSTS_ROOT
fi
GOWORK=off go test "${test_arguments[@]}"
[[ -s "${profile}" ]] || {
    printf 'coverage profile is missing for %s\n' "${module}" >&2
    exit 1
}

set +e
awk '
    FNR == NR { expected[$1] = 1; next }
    FNR == 1 { next }
    {
        split($1, location, ":")
        package = location[1]
        sub("/[^/]+$", "", package)
        block = $1
        blockPackage[block] = package
        blockStatements[block] = $(NF - 1) + 0
        if (($NF + 0) > 0) blockCovered[block] = 1
    }
    END {
        failed = 0
        for (block in blockStatements) {
            package = blockPackage[block]
            total[package] += blockStatements[block]
            if (blockCovered[block]) covered[package] += blockStatements[block]
        }
        for (package in expected) {
            if (!(package in total) || total[package] == 0) {
                printf "%s missing executable coverage evidence\n", package
                failed = 1
                continue
            }
            printf "%s %d/%d statements\n", package, covered[package], total[package]
            if (covered[package] != total[package]) failed = 1
        }
        exit failed
    }
' "${packages}" "${profile}" | sort >"${report}"
coverage_status="${PIPESTATUS[0]}"
set -e

cat "${report}"
if [[ "${coverage_status}" -eq 0 ]]; then
    printf 'all production packages have exact 100%% statement coverage\n'
else
    printf 'one or more production packages are below exact 100%% coverage\n' >&2
    exit 1
fi
