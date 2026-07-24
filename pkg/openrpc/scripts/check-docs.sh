#!/usr/bin/env bash
set -euo pipefail

required=(
  CHANGELOG.md CODE_OF_CONDUCT.md CONTRIBUTING.md LICENSE NOTICE README.md
  ROADMAP.md SECURITY.md THIRD_PARTY_NOTICES.md
	llms.txt llms-full.txt api/README.md
  docs/README.md docs/adoption.md docs/api.md docs/architecture.md
  docs/compatibility.md docs/cookbook.md docs/dependencies.md docs/evidence.md
  docs/examples.md docs/faq.md docs/go-safety-and-concurrency.md
  docs/hardening.md docs/migration.md docs/performance.md docs/quickstart.md
  docs/releasing.md docs/repository-standards.md
  docs/resolver-threat-model.md docs/resource-budgets.md docs/security.md
  docs/specification-report.md benchmarks/baseline.txt
  docs/troubleshooting.md
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

documents = list(Path(".").glob("*.md")) + list(Path("docs").glob("*.md"))
for document in documents:
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
        if not (document.parent / relative).resolve().exists():
            raise SystemExit(f"broken relative link in {document}: {target}")

print("required documentation exists and relative links resolve")
PY

python3 scripts/generate-llms.py --check

go test ./... -run '^Example' -count=1
