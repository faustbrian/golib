#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 1 ]]; then
    echo "usage: $0 vMAJOR.MINOR.PATCH" >&2
    exit 2
fi

version="${1#v}"
awk -v heading="## [${version}]" '
$0 == heading {
    found = 1
    next
}
found && /^## \[/ {
    exit
}
found {
    print
}
END {
    if (!found) {
        exit 1
    }
}
' CHANGELOG.md
