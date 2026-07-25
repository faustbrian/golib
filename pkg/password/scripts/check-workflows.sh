#!/bin/sh
set -eu
# shellcheck disable=SC1091 # Repository-local pinned version manifest.
. ./tools/versions.env

actionlint_actual=$(actionlint -version | sed -n '1p')
if [ "${actionlint_actual#v}" != "${ACTIONLINT_VERSION#v}" ]; then
	printf 'actionlint version mismatch: expected %s, got %s\n' "$ACTIONLINT_VERSION" "${actionlint_actual:-unknown}" >&2
	exit 1
fi
gitleaks_actual=$(go version -m "$(command -v gitleaks)" | awk '$1 == "mod" && $2 == "github.com/zricethezav/gitleaks/v8" { print $3 }')
if [ "$gitleaks_actual" != "$GITLEAKS_VERSION" ]; then
	printf 'gitleaks version mismatch: expected %s, got %s\n' "$GITLEAKS_VERSION" "${gitleaks_actual:-unknown}" >&2
	exit 1
fi

actionlint
if [ "${SHELLCHECK_CONTAINER:-0}" = 1 ]; then
	shellcheck_actual=$(docker run --rm "koalaman/shellcheck:v${SHELLCHECK_VERSION}" --version | sed -n 's/^version: //p')
	if [ "$shellcheck_actual" != "$SHELLCHECK_VERSION" ]; then
		printf 'container ShellCheck version mismatch: expected %s, got %s\n' "$SHELLCHECK_VERSION" "${shellcheck_actual:-unknown}" >&2
		exit 1
	fi
	docker run --rm -v "$PWD:/mnt" -w /mnt "koalaman/shellcheck:v${SHELLCHECK_VERSION}" scripts/*.sh
else
	shellcheck_actual=$(shellcheck --version | sed -n 's/^version: //p')
	if [ "$shellcheck_actual" != "$SHELLCHECK_VERSION" ]; then
		printf 'shellcheck version mismatch: expected %s, got %s\n' "$SHELLCHECK_VERSION" "${shellcheck_actual:-unknown}" >&2
		exit 1
	fi
	shellcheck scripts/*.sh
fi
gitleaks detect --no-banner --redact --source .
