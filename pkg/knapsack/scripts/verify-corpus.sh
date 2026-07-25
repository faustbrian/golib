#!/bin/sh
set -eu

test -s specification/references.tsv
test -s specification/corpora.tsv
awk -F '\t' 'NR == 1 {next} NF != 6 || $1 == "" || $2 == "" || $3 == "" || $4 == "" || $5 == "" || $6 == "" {exit 1}' specification/references.tsv
awk -F '\t' 'NR == 1 {next} NF != 5 || $1 == "" || $2 == "" || $3 == "" || $4 == "" || $5 == "" {exit 1}' specification/corpora.tsv
grep -q 'c64e4974859fa4638588b4174d4c6bd31e0b91af' solver/testdata/corpus/dwave-sample-data-1.json
grep -q 'a1d2dcc3eb8424e25cd89d150bc5bc1ae7704c985d6fa19112913d9bb778d951' solver/testdata/corpus/dwave-sample-data-1.json
grep -q 'Apache-2.0' specification/corpora.tsv
test "$(shasum -a 256 third_party/licenses/dwave-3d-bin-packing-Apache-2.0.txt | awk '{print $1}')" = '58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd'
if grep -q 'terms-not-stated' specification/corpora.tsv; then
	printf '%s\n' 'unlicensed corpus entry is forbidden' >&2
	exit 1
fi
