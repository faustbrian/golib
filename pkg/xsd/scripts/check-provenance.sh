#!/bin/sh
set -eu

manifest=specification/manifest.tsv
matrix=specification/requirements/xsd-1.0.tsv

awk -F '\t' '
NR == 1 {
    if ($0 != "id\tversion\trole\tstatus\tsha256\tbytes\turl") exit 1
    next
}
NF != 7 || seen[$1]++ || $5 !~ /^[0-9a-f]{64}$/ || $6 !~ /^[0-9]+$/ ||
    $7 !~ /^https:\/\// { exit 1 }
END { if (NR < 2) exit 1 }
' "$manifest"

awk -F '\t' '
NR == 1 {
    if ($0 != "id\trequirement\tnormative_source\tstatus\tevidence") exit 1
    next
}
NF != 5 || seen[$1]++ ||
    ($4 != "missing" && $4 != "partial" && $4 != "implemented") { exit 1 }
$4 == "implemented" && $5 == "-" { exit 1 }
END { if (NR < 2) exit 1 }
' "$matrix"

if [ "${VERIFY_REMOTE:-0}" != 1 ]; then
    exit 0
fi

workdir=$(mktemp -d "${TMPDIR:-/tmp}/xsd-provenance.XXXXXX")
trap 'rm -rf "$workdir"' EXIT HUP INT TERM

tail -n +2 "$manifest" | while IFS="$(printf '\t')" read -r id version role status digest bytes url; do
    destination="$workdir/$id"
    curl --fail --location --silent --show-error --output "$destination" "$url"
    actual_digest=$(shasum -a 256 "$destination" | awk '{print $1}')
    actual_bytes=$(wc -c < "$destination" | tr -d ' ')
    test "$actual_digest" = "$digest"
    test "$actual_bytes" = "$bytes"
done
