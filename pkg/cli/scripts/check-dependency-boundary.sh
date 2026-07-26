#!/usr/bin/env bash
set -euo pipefail

modules="$(GOWORK=off go list -m all | awk '{print $1}' | sort)"
expected="$(printf '%s\n' \
  github.com/cpuguy83/go-md2man/v2 \
  github.com/faustbrian/golib/pkg/cli \
  github.com/inconshreveable/mousetrap \
  github.com/russross/blackfriday/v2 \
  github.com/spf13/cobra \
  github.com/spf13/pflag \
  go.yaml.in/yaml/v3 \
  gopkg.in/check.v1 | sort)"

if [[ "${modules}" != "${expected}" ]]; then
  echo "core dependency boundary changed:" >&2
  diff -u <(printf '%s\n' "${expected}") <(printf '%s\n' "${modules}") >&2 || true
  exit 1
fi

if GOWORK=off go list -deps ./... | rg -q '^github.com/(urfave/cli|alecthomas/kong)'; then
  echo "comparison-only frameworks leaked into the core dependency graph" >&2
  exit 1
fi
