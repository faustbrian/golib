#!/usr/bin/env bash
set -euo pipefail

syft="${SYFT:-go run github.com/anchore/syft/cmd/syft@v1.48.0}"
output="$(mktemp)"
trap 'rm -f "${output}"' EXIT

${syft} dir:. -o cyclonedx-json="${output}" --quiet
python3 -m json.tool "${output}" >/dev/null
python3 - "${output}" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    document = json.load(handle)

if document.get("bomFormat") != "CycloneDX":
    raise SystemExit("SBOM is not a CycloneDX document")
if not document.get("components"):
    raise SystemExit("SBOM contains no components")
PY
