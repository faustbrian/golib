#!/usr/bin/env bash
set -euo pipefail

required=(
	CHANGELOG.md
	CONTRIBUTING.md
	LICENSE
	README.md
	SECURITY.md
	docs/api.md
	docs/audit-evidence.md
	docs/conformance.md
	docs/discovery.md
	docs/examples.md
	docs/hardening.md
	docs/kubernetes.md
	docs/layering.md
	docs/migration.md
	docs/operations.md
	docs/package-authors.md
	docs/security.md
	docs/sources.md
)

for file in "${required[@]}"; do
	if [[ ! -s "$file" ]]; then
		echo "required repository file is missing or empty: $file" >&2
		exit 1
	fi
done

python3 - <<'PY'
from pathlib import Path
import re

for document in Path(".").rglob("*.md"):
    content = document.read_text(encoding="utf-8")
    prose = []
    in_fence = False
    for line in content.splitlines():
        if line.lstrip().startswith(("```", "~~~")):
            in_fence = not in_fence
            continue
        if not in_fence:
            prose.append(line)
    for target in re.findall(r"\[[^\]]*\]\(([^)]+)\)", "\n".join(prose)):
        if target.startswith(("http://", "https://", "mailto:", "#")):
            continue
        relative = target.split("#", 1)[0]
        resolved = (document.parent / relative).resolve()
        if not resolved.exists():
            raise SystemExit(f"broken relative link in {document}: {target}")

print("all required files exist and relative Markdown links resolve")
PY

go test ./... -run '^Example'
go run ./examples/quickstart >/dev/null
go run ./examples/discovery >/dev/null
go run ./examples/testing >/dev/null
