#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md
  CHANGELOG.md
  CODE_OF_CONDUCT.md
  CONTRIBUTING.md
  LICENSE
  NOTICE
  ROADMAP.md
  SECURITY.md
  THIRD_PARTY_NOTICES.md
  llms.txt
  llms-full.txt
  docs/README.md
  docs/quickstart.md
  docs/api.md
  docs/architecture.md
  docs/pool-and-lifecycle.md
  docs/tls.md
  docs/transactions.md
  docs/errors.md
  docs/sqlc.md
  docs/observability.md
  docs/migrations.md
  docs/testing.md
  docs/kubernetes.md
  docs/faq.md
  docs/compatibility.md
  docs/migration.md
  docs/security.md
  docs/hardening.md
  docs/performance.md
  docs/releasing.md
  docs/repository-standards.md
  examples/kubernetes/migration-job.yaml
	examples/migrations/README.md
	examples/migrations/main.go
	examples/migrations/migrations/000001_create_widgets.sql
	examples/migrations/migrations/000002_index_widgets.sql
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

python3 scripts/generate-llms.py --check
bash scripts/extract-release-notes.sh Unreleased > "${TMPDIR:-/tmp}/postgres-release-notes.md"
test -s "${TMPDIR:-/tmp}/postgres-release-notes.md"
! grep -q '^\[[^]]*\]:' "${TMPDIR:-/tmp}/postgres-release-notes.md"
go test ./... -run 'TestExportedSymbolsHaveGoDocumentation|^Example'
go build ./examples/...
(
  cd examples/migrations
  GOTOOLCHAIN=auto go mod tidy -diff
  GOTOOLCHAIN=auto go test .
)
