#!/usr/bin/env bash
set -euo pipefail

commit="26058a01fdbc8dad9ded0e97133190098ea8c5d8"
repository="https://github.com/tc39/test262.git"
target="${TEST262_ROOT:-/tmp/ecma-regexp-test262}"

if [[ -d "$target/.git" ]]; then
  test "$(git -C "$target" rev-parse HEAD)" = "$commit"
  exit 0
fi
if [[ -e "$target" ]]; then
  echo "Test262 target exists but is not a Git checkout: $target" >&2
  exit 1
fi

temporary="$(mktemp -d "${TMPDIR:-/tmp}/ecma-regexp-test262.XXXXXX")"
git clone --filter=blob:none --no-checkout "$repository" "$temporary"
git -C "$temporary" sparse-checkout init --cone
git -C "$temporary" sparse-checkout set \
  harness \
  test/built-ins/RegExp \
  test/language/literals/regexp
git -C "$temporary" fetch --depth=1 origin "$commit"
git -C "$temporary" checkout --detach "$commit"
mv "$temporary" "$target"
