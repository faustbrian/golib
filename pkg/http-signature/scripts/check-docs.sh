#!/usr/bin/env bash
set -euo pipefail

required=(
    README.md CHANGELOG.md SECURITY.md LICENSE NOTICE
    docs/benchmarks.md docs/conformance.md docs/faq.md docs/integration.md docs/security.md
    spec/errata-decisions.md spec/sources.lock.json
)
for file in "${required[@]}"; do
    if [[ ! -s "$file" ]]; then
        echo "required documentation is missing or empty: $file" >&2
        exit 1
    fi
done

python3 - <<'PY'
from pathlib import Path
import re

for document in Path('.').rglob('*.md'):
    content = document.read_text(encoding='utf-8')
    prose, fenced = [], False
    for line in content.splitlines():
        if line.lstrip().startswith(('```', '~~~')):
            fenced = not fenced
            continue
        if not fenced:
            prose.append(line)
    for target in re.findall(r'\[[^]]*\]\(([^)]+)\)', '\n'.join(prose)):
        if target.startswith(('http://', 'https://', 'mailto:', '#')):
            continue
        relative = target.split('#', 1)[0]
        if relative and not (document.parent / relative).resolve().exists():
            raise SystemExit(f'broken relative link in {document}: {target}')
print('documentation links resolve')
PY

./scripts/with-go-cache.sh env GOWORK=off go test -mod=readonly ./... -run '^Example' -count=1
