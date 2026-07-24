#!/usr/bin/env bash
set -euo pipefail

required=(
	README.md CHANGELOG.md CONTRIBUTING.md CODE_OF_CONDUCT.md SECURITY.md
	docs/README.md docs/api.md docs/weekly-schedules.md docs/exceptions.md
	docs/overnight.md docs/timezones.md docs/queries.md docs/precedence.md
	docs/normalization.md docs/persistence.md docs/observability.md
	docs/integrations.md
	docs/service-points.md docs/storefronts.md docs/support-hours.md
	docs/legacy-migration.md docs/cookbook.md docs/security.md
	docs/performance.md docs/faq.md docs/troubleshooting.md
	docs/compatibility.md docs/architecture.md docs/roadmap.md
	docs/hardening.md
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

go test ./... -run '^Example'
