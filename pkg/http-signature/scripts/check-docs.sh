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
    fence_marker = None
    for line in content.splitlines():
        if line.lstrip().startswith(('```', '~~~')):
            marker = line.lstrip()[:3]
            if not fenced:
                if not line.lstrip()[3:].strip():
                    raise SystemExit(f'untyped fenced block in {document}')
                fenced, fence_marker = True, marker
            elif marker == fence_marker:
                fenced, fence_marker = False, None
            continue
        if not fenced:
            prose.append(line)
    if fenced:
        raise SystemExit(f'unclosed fenced block in {document}')
    for target in re.findall(r'\[[^]]*\]\(([^)]+)\)', '\n'.join(prose)):
        if target.startswith(('http://', 'https://', 'mailto:', '#')):
            continue
        relative = target.split('#', 1)[0]
        if relative and not (document.parent / relative).resolve().exists():
            raise SystemExit(f'broken relative link in {document}: {target}')
print('documentation links resolve')
PY

./scripts/with-go-cache.sh env GOWORK=off go test -mod=readonly ./... -run '^Example' -count=1

documentation_build="$(mktemp -d)"
cleanup() {
    chmod -R u+w "$documentation_build" 2>/dev/null || true
    find "$documentation_build" -mindepth 1 -delete
    rmdir "$documentation_build"
}
trap cleanup EXIT

python3 - "$documentation_build/readme.go" <<'PY'
from pathlib import Path
import re
import sys

readme = Path('README.md').read_text(encoding='utf-8')
blocks = re.findall(r'^```go\n(.*?)^```$', readme, flags=re.MULTILINE | re.DOTALL)
if len(blocks) != 1:
    raise SystemExit(f'README must contain exactly one compiled Go quick start, found {len(blocks)}')

source = '''package readme

import (
    "context"
    "net/http"
    "time"

    httpsignature "github.com/faustbrian/golib/pkg/http-signature"
)

var provider docsProvider

type docsProvider struct{}

func (docsProvider) SigningKey(context.Context) (httpsignature.SigningKey, error) {
    return httpsignature.SigningKey{}, nil
}

func compileQuickStart() {
''' + blocks[0] + '''
    _ = client
}
'''
Path(sys.argv[1]).write_text(source, encoding='utf-8')
PY

module_root="$(pwd)"
(
    cd "$documentation_build"
    go mod init docs.example/http-signature
    go mod edit -require github.com/faustbrian/golib/pkg/http-signature@v0.0.0
    go mod edit -replace github.com/faustbrian/golib/pkg/http-signature="$module_root"
    "$module_root/scripts/with-go-cache.sh" env GOWORK=off go test -mod=mod ./...
)

api_reference="$documentation_build/api-reference.txt"
./scripts/with-go-cache.sh env GOWORK=off go doc -all . > "$api_reference"
test -s "$api_reference"
