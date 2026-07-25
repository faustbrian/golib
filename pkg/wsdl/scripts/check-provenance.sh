#!/bin/sh
set -eu

manifest=specification/manifest.tsv
interoperability=specification/interoperability.tsv
tooling=specification/tooling.tsv
assertions11=specification/assertions/wsdl-1.1.tsv
assertions20=specification/assertions/wsdl-2.0.tsv

awk -F '\t' '
NR == 1 {
    if ($0 != "id\tversion\trole\tstatus\tsha256\tbytes\turl") exit 1
    next
}
NF != 7 || seen[$1]++ || $5 !~ /^[0-9a-f]{64}$/ || $6 !~ /^[0-9]+$/ ||
    $7 !~ /^https:\/\// { exit 1 }
END { if (NR < 2) exit 1 }
' "$manifest"

for matrix in specification/requirements/wsdl-1.1.tsv specification/requirements/wsdl-2.0.tsv; do
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
done

awk -F '\t' '
NR == 1 {
    if ($0 != "assertion\tsection\tkeywords\trequirement\ttext_sha256\tsource") exit 1
    next
}
NF != 6 || seen[$1]++ || seen_digest[$5]++ ||
    $1 !~ /^WSDL11-NORM-[0-9][0-9][0-9]$/ || $2 !~ /^_/ ||
    $3 !~ /^(MUST|SHALL|REQUIRED|SHOULD|RECOMMENDED|MAY|OPTIONAL)/ ||
    $4 !~ /^WSDL11-/ || $5 !~ /^[0-9a-f]{64}$/ ||
    $6 !~ /^https:\/\/www.w3.org\/TR\/2001\/NOTE-wsdl-20010315.html#/ { exit 1 }
END { if (NR != 24) exit 1 }
' "$assertions11"

awk -F '\t' '
NR == 1 {
    if ($0 != "specification\tassertion\trequirement\tsource") exit 1
    next
}
NF != 4 || seen[$2]++ || $3 !~ /^WSDL20-/ ||
    $4 !~ /^https:\/\/www.w3.org\/TR\/2007\/REC-wsdl20/ { exit 1 }
$1 == "wsdl-2.0-core" { core++ }
$1 == "wsdl-2.0-adjuncts" { adjuncts++ }
$1 != "wsdl-2.0-core" && $1 != "wsdl-2.0-adjuncts" { exit 1 }
END { if (core != 84 || adjuncts != 110) exit 1 }
' "$assertions20"

awk -F '\t' '
NR == 1 {
    if ($0 != "id\tproducer\tlicense\trevision\tpath\tsha256\tbytes\turl") exit 1
    next
}
NF != 8 || seen[$1]++ || $5 !~ /^testdata\/interoperability\// ||
    $6 !~ /^[0-9a-f]{64}$/ || $7 !~ /^[0-9]+$/ || $8 !~ /^https:\/\// { exit 1 }
END { if (NR < 2) exit 1 }
' "$interoperability"

tail -n +2 "$interoperability" | while IFS="$(printf '\t')" read -r id producer license revision path digest bytes url; do
    test -f "$path"
    actual_digest=$(shasum -a 256 "$path" | awk '{print $1}')
    actual_bytes=$(wc -c < "$path" | tr -d ' ')
    test "$actual_digest" = "$digest"
    test "$actual_bytes" = "$bytes"
done

awk -F '\t' '
NR == 1 {
    if ($0 != "id\tversion\tlicense\tsha256\tbytes\turl") exit 1
    next
}
NF != 6 || seen[$1]++ || $4 !~ /^[0-9a-f]{64}$/ ||
    $5 !~ /^[0-9]+$/ || $6 !~ /^https:\/\// { exit 1 }
END { if (NR < 2) exit 1 }
' "$tooling"

if [ "${VERIFY_REMOTE:-0}" != 1 ]; then
    exit 0
fi

workdir=$(mktemp -d "${TMPDIR:-/tmp}/wsdl-provenance.XXXXXX")
trap 'rm -rf "$workdir"' EXIT HUP INT TERM

tail -n +2 "$manifest" | while IFS="$(printf '\t')" read -r id version role status digest bytes url; do
    destination="$workdir/$id"
    curl --fail --location --silent --show-error --output "$destination" "$url"
    actual_digest=$(shasum -a 256 "$destination" | awk '{print $1}')
    actual_bytes=$(wc -c < "$destination" | tr -d ' ')
    test "$actual_digest" = "$digest"
    test "$actual_bytes" = "$bytes"
done

tail -n +2 "$interoperability" | while IFS="$(printf '\t')" read -r id producer license revision path digest bytes url; do
    destination="$workdir/interop-$id"
    curl --fail --location --silent --show-error --output "$destination" "$url"
    actual_digest=$(shasum -a 256 "$destination" | awk '{print $1}')
    actual_bytes=$(wc -c < "$destination" | tr -d ' ')
    test "$actual_digest" = "$digest"
    test "$actual_bytes" = "$bytes"
done

tail -n +2 "$tooling" | while IFS="$(printf '\t')" read -r id version license digest bytes url; do
    destination="$workdir/tool-$id"
    curl --fail --location --silent --show-error --output "$destination" "$url"
    actual_digest=$(shasum -a 256 "$destination" | awk '{print $1}')
    actual_bytes=$(wc -c < "$destination" | tr -d ' ')
    test "$actual_digest" = "$digest"
    test "$actual_bytes" = "$bytes"
done
