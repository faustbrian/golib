#!/usr/bin/env bash
set -euo pipefail

tag="${1:-}"
changelog="${2:-CHANGELOG.md}"
license="${3:-LICENSE}"

./scripts/check-release-tag.sh "$tag"

if [[ ! -f "$license" ]]; then
  echo "release license is missing: $license" >&2
  exit 1
fi

version="${tag#v}"
python3 - "$version" "$changelog" <<'PY'
from datetime import date
from pathlib import Path
import re
import sys

version, changelog_name = sys.argv[1:]
changelog = Path(changelog_name)
if not changelog.is_file():
    raise SystemExit(f"release changelog is missing: {changelog}")

pattern = re.compile(rf"^## \[{re.escape(version)}\] - (\d{{4}}-\d{{2}}-\d{{2}})$")
matches = [pattern.fullmatch(line) for line in changelog.read_text(encoding="utf-8").splitlines()]
dates = [match.group(1) for match in matches if match]
if len(dates) != 1:
    raise SystemExit(
        f"expected exactly one dated changelog heading for release {version}, found {len(dates)}"
    )

try:
    date.fromisoformat(dates[0])
except ValueError as error:
    raise SystemExit(f"invalid changelog date for release {version}: {dates[0]}") from error
PY
