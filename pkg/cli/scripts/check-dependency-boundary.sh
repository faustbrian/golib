#!/usr/bin/env bash
set -euo pipefail

modules="$(GOWORK=off go list -m all | awk '{print $1}' | sort)"
expected="$(printf '%s\n' \
  github.com/faustbrian/golib/pkg/cli | sort)"

if [[ "${modules}" != "${expected}" ]]; then
  echo "core dependency boundary changed:" >&2
  diff -u <(printf '%s\n' "${expected}") <(printf '%s\n' "${modules}") >&2 || true
  exit 1
fi

if GOWORK=off go list -deps ./... | rg -q '^github.com/(spf13|urfave/cli|alecthomas/kong)'; then
  echo "external parser frameworks leaked into the core dependency graph" >&2
  exit 1
fi
