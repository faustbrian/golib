#!/usr/bin/env bash
set -euo pipefail

required=(
  .gitattributes .gitignore .golangci.yml AGENTS.md CHANGELOG.md
  CODE_OF_CONDUCT.md CONTRIBUTING.md LICENSE Makefile NOTICE README.md ROADMAP.md
  SECURITY.md THIRD_PARTY_NOTICES.md llms.txt llms-full.txt
  docs/README.md docs/quickstart.md docs/semantics.md docs/api.md
  docs/adoption.md docs/architecture.md docs/cookbook.md docs/compatibility.md
  docs/dependencies.md docs/evidence.md docs/faq.md docs/hardening.md
  docs/migration.md docs/performance.md docs/releasing.md
  docs/repository-standards.md docs/security.md docs/troubleshooting.md
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

for document in Path('.').rglob('*.md'):
    content = document.read_text(encoding='utf-8')
    prose = []
    fenced = False
    for line in content.splitlines():
        if line.lstrip().startswith(('```', '~~~')):
            fenced = not fenced
            continue
        if not fenced:
            prose.append(line)
    for target in re.findall(r'\[[^\]]*\]\(([^)]+)\)', '\n'.join(prose)):
        if target.startswith(('http://', 'https://', 'mailto:', '#')):
            continue
        relative = target.split('#', 1)[0].strip('<>')
        if not (document.parent / relative).resolve().exists():
            raise SystemExit(f'broken relative link in {document}: {target}')

print('all required files exist and relative Markdown links resolve')
PY

go test ./... -run '^Example'
