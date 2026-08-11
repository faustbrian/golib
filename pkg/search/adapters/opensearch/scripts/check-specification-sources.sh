#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${root}/specification/manifest.tsv"

python3 - "${manifest}" <<'PY'
import csv
import hashlib
import sys
import urllib.request
from pathlib import Path

manifest = Path(sys.argv[1])
maximum_source_bytes = 128 << 20

expected_header = ["id", "version", "sections", "role", "url", "sha256", "status"]
required_sources = {"opensearch-2", "opensearch-3", "opensearch-go"}
with manifest.open(encoding="utf-8", newline="") as source:
    reader = csv.DictReader(source, delimiter="\t")
    if reader.fieldnames != expected_header:
        raise SystemExit("OpenSearch specification manifest has an invalid header")
    rows = list(reader)

if not rows:
    raise SystemExit("OpenSearch specification manifest is empty")

seen = set()
for row in rows:
    identifier = row["id"]
    if not identifier or identifier in seen:
        raise SystemExit(f"invalid or duplicate OpenSearch source identifier: {identifier!r}")
    seen.add(identifier)
    if row["status"] != "pinned" or not row["url"].startswith("https://"):
        raise SystemExit(f"OpenSearch source is not immutably pinned: {identifier}")
    expected = row["sha256"]
    if len(expected) != 64 or any(character not in "0123456789abcdef" for character in expected):
        raise SystemExit(f"OpenSearch source has an invalid SHA-256 digest: {identifier}")

    request = urllib.request.Request(row["url"], headers={"User-Agent": "golib-opensearch-conformance"})
    digest = hashlib.sha256()
    size = 0
    with urllib.request.urlopen(request, timeout=120) as response:
        while chunk := response.read(1 << 20):
            size += len(chunk)
            if size > maximum_source_bytes:
                raise SystemExit(f"OpenSearch source exceeds the download bound: {identifier}")
            digest.update(chunk)
    if digest.hexdigest() != expected:
        raise SystemExit(f"OpenSearch source digest mismatch: {identifier}")

if seen != required_sources:
    raise SystemExit(f"OpenSearch specification sources are incomplete: {sorted(seen)}")

print(f"verified {len(rows)} pinned OpenSearch sources")
PY
