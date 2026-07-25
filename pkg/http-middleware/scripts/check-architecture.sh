#!/usr/bin/env bash
set -euo pipefail

production=$(mktemp)
trap 'rm -f "$production"' EXIT
if [[ ${ARCHITECTURE_USE_GREP:-0} != 1 ]] && command -v rg >/dev/null 2>&1; then
	rg --files -g '*.go' -g '!**/*_test.go' >"$production"
else
	find . -type f -name '*.go' ! -name '*_test.go' -print >"$production"
fi

if if command -v rg >/dev/null 2>&1 && [[ ${ARCHITECTURE_USE_GREP:-0} != 1 ]]; then
	xargs rg -n '"(reflect|unsafe)"|//go:linkname|import[[:space:]]+"C"|func[[:space:]]+init[[:space:]]*\(' <"$production"
else
	xargs grep -En '"(reflect|unsafe)"|//go:linkname|import[[:space:]]+"C"|func[[:space:]]+init[[:space:]]*\(' <"$production"
fi; then
	echo 'forbidden production runtime mechanism found' >&2
	exit 1
fi

if find . -type f \( -name '*.c' -o -name '*.cc' -o -name '*.cpp' -o -name '*.m' -o -name '*.s' \) -print | grep -q .; then
	echo 'native production source found' >&2
	exit 1
fi

dependencies=$(go list -deps -f '{{.ImportPath}}' ./...)
if printf '%s\n' "$dependencies" | grep -E '^github\.com/faustbrian/go-(service|router|authentication|authorization|rate-limit|idempotency|log|telemetry)(/|$)'; then
	echo 'owning sibling package entered the dependency graph' >&2
	exit 1
fi

CGO_ENABLED=0 go test ./...
echo 'architecture constraints pass'
