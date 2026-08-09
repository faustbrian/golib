#!/usr/bin/env bash
set -euo pipefail

mode="${1:-all}"
case "$mode" in
  all | provenance | test) ;;
  *)
    echo "unsupported Test262 mode: $mode" >&2
    exit 2
    ;;
esac

workspace="$(mktemp -d "${TMPDIR:-/tmp}/ecma-regexp-conformance.XXXXXX")"
cleanup() {
  chmod -R u+w "$workspace" 2>/dev/null || true
  find "$workspace" -depth -delete 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

export TEST262_ROOT="$workspace/test262"
./scripts/sync-test262.sh

if [[ "$mode" == "all" || "$mode" == "provenance" ]]; then
  ./scripts/check-provenance.sh
fi
if [[ "$mode" == "all" || "$mode" == "test" ]]; then
  go test . -run '^TestTest262' -count=1
fi
