#!/usr/bin/env bash
set -euo pipefail

go test ./... -run 'Provenance|Dataset|Generated'
test -f LICENSE
test -f THIRD_PARTY_NOTICES.md
go mod verify

required_notices=(
  'Unicode License v3'
  'IANA terms of service'
  'BSD-3-Clause'
  'SIX ISO 4217 terms of use'
  'Apache-2.0'
)
for notice in "${required_notices[@]}"; do
  grep -Fq -- "$notice" THIRD_PARTY_NOTICES.md || {
    echo "missing third-party license notice: $notice" >&2
    exit 1
  }
done

expected_snapshot='a1e2d3de3b36bb5642b13f3340a81324240090cf700dc30bf7d43581bf938983'
actual_snapshot="$(shasum -a 256 data/dataset-snapshot.json | awk '{print $1}')"
test "$actual_snapshot" = "$expected_snapshot" || {
  echo 'dataset snapshot checksum changed without provenance review' >&2
  exit 1
}
