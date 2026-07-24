#!/usr/bin/env bash
set -euo pipefail

required=(
  .gitattributes
  .gitignore
  .golangci.yml
  AGENTS.md
  CHANGELOG.md
  CLAUDE.md
  CODE_OF_CONDUCT.md
  CONTRIBUTING.md
  .ai/GOAL.md
  .ai/GOAL_HARDEN.md
  LICENSE
  Makefile
  NOTICE
  README.md
  ROADMAP.md
  SECURITY.md
  THIRD_PARTY_NOTICES.md
  llms.txt
  llms-full.txt
  docs/README.md
  docs/quickstart.md
  docs/adoption.md
  docs/api.md
  docs/architecture.md
  docs/go-safety-and-concurrency.md
  docs/examples.md
  docs/cookbook.md
  docs/faq.md
  docs/troubleshooting.md
  docs/migration.md
  docs/compatibility.md
  docs/performance.md
  docs/hardening.md
  docs/security.md
  docs/releasing.md
  docs/repository-standards.md
  docs/conformance.md
  docs/middleware.md
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
        if relative.startswith("<") and relative.endswith(">"):
            relative = relative[1:-1]
        resolved = (document.parent / relative).resolve()
        if not resolved.exists():
            raise SystemExit(f"broken relative link in {document}: {target}")

print("all required files exist and relative Markdown links resolve")
PY

python3 scripts/generate-llms.py --check
go test ./... -run '^Example'
