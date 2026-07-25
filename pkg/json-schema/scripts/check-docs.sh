#!/usr/bin/env bash
set -euo pipefail

required=(
	README.md CHANGELOG.md CONTRIBUTING.md RELEASING.md LICENSE NOTICE
	docs/adoption.md docs/api.md docs/architecture.md docs/conformance.md
	docs/cookbook.md docs/dependencies.md docs/dialects.md docs/extensions.md
	docs/faq.md docs/hardening-report.md docs/limits.md docs/matrices.md docs/output.md
	docs/performance.md docs/quickstart.md docs/resolvers.md docs/security.md
	docs/troubleshooting.md docs/versioning.md bowtie/README.md
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
    if 'testdata' in document.parts:
        continue
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
go test ./... -run '^Example'
